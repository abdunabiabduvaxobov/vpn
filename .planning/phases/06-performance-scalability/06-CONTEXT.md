# Phase 6: Performance & scalability - Context

**Gathered:** 2026-05-29
**Status:** Ready for planning

<domain>
## Phase Boundary

Backend performance & scalability work so the single VM handles roughly **5–8k concurrent active connections** without the API becoming the bottleneck — by caching hot reads, batching hot writes, splitting DB/Redis off the API host, adding missing indexes, gating the scheduler, and propagating request context.

**Fixed scope = PERF-01 through PERF-09** (REQUIREMENTS.md lines 75–83). This phase clarifies *how* to implement those nine requirements; it adds no new product capability. The work is overwhelmingly pre-decided by `docs/audit/PERFORMANCE-AUDIT.md` (every fix anchored to a file:line) — the decisions below are the forks the audit deliberately left open because they carry operational, cost, durability, or scope consequences.

**Not in this phase:** the actual physical second-host provisioning (ops runbook), the real 10k synthetic load test (deferred to release), and multi-replica API deploy (SCALE-01 — the gate is built here, the deploy is post-launch).

</domain>

<decisions>
## Implementation Decisions

### Deployment topology (PERF-03)

- **D-01:** Split the data tier into its own compose file — `docker-compose.data.yml` holds `postgres` + `redis`; `docker-compose.prod.yml` keeps the app tier (`api` + `tunnel`). Connection config becomes fully **host-parameterized** (`DB_HOST` / `REDIS_HOST` env vars feeding `DATABASE_URL` / `REDIS_URL`), defaulting to the service names for co-located dev. Moving PG/Redis to a second host is then a **documented ops-runbook step with zero code/image change** — exactly what audit §12.3 promises. The PERF-03 success criterion ("`docker-compose.prod.yml` reflects the split") is satisfied by this restructure; the physical move is the operator's call at their own pace.
- **D-02:** The cross-host link is secured by **private network + firewall** — bind PG/Redis to a trusted private interface (provider VPC / WireGuard / locked subnet), keep `sslmode=disable` + Redis `--requirepass` over that link. Connection strings are written **TLS-ready**; document the `sslmode=require` (or `verify-full`) + Redis-TLS upgrade path so it can be turned on later without code change. Rejected: full TLS now (unnecessary latency/cert overhead at this scale on a trusted private link).

### Heartbeat scalability (PERF-02)

- **D-03:** **Postgres remains source-of-truth** for `connections.last_heartbeat_at`. The heartbeat handler writes **only to Redis** (`SET hb:<conn_id> <unix_ts> EX 600`). A scheduler job flushes every **10s** (per the PERF-02 success criterion) via one bulk `UPDATE connections SET last_heartbeat_at = $now WHERE id = ANY($dirty_ids) AND disconnected_at IS NULL`. `CleanupStaleConnections` and the new PERF-05 partial index keep reading `last_heartbeat_at` **unchanged**. A Redis restart costs at most one missed flush window — well inside the 3-min `StaleConnectionAfter` grace. Rejected: Redis-as-live-truth + `EXISTS hb:<id>` cleanup (a Redis flush would make every connection look stale at once and leave PG's `last_heartbeat_at` cold).
- **D-04:** **Dirty-set tracking.** The heartbeat handler does `SET hb:<id>` + `SADD hb:dirty <id>` in a single pipeline. The flush job: `SMEMBERS hb:dirty` → bulk `UPDATE` → `SREM` the flushed ids. O(changed), no `SCAN`, multi-replica safe. Costs one extra `SADD` per heartbeat — cheap vs. re-writing unchanged rows.

### Caching & invalidation (PERF-01, PERF-04)

- **D-05:** `/servers` cache = a **single global blob** `cache:servers:active` holding the `ListActiveServers` JSON (the read that actually amplifies — identical for everyone, changes ~weekly). The **plan-scoped filter stays live in Go**: admin → full list; non-admin → intersect the cached list with the plan's allowed server IDs. **Invalidation surface = the 3 admin server-write handlers only** (`AdminCreateServer`, `AdminUpdateServer`, `AdminDeleteServer` — synchronous `DEL` before returning). This keeps the per-plan filter automatically correct when an admin remaps a plan↔server (no per-plan cache keys to bust). Follows the cache-aside + fail-open pattern from `plans_cache.go`. TTL ≤ 5 min — start at 60s to match `plans_cache` precedent unless research argues for the higher ceiling.
- **D-06:** User existence+tier cache for `AuthRequired`. Key `user:<id>` (existence + tier), **TTL ≤ 5s**, cache-aside + fail-open. **Explicit synchronous bust on ALL mutation paths** (operator chose zero-lag everywhere over relying on the 5s TTL): (1) admin user-update — *required by PERF-04*; (2) payment webhook Pro-grant (`webhook_lava.go`); (3) user delete / `PerformRestore` (Telegram recovery + admin delete); (4) scheduler expiry-downgrade.
- **D-07:** **`c.Locals("user")` refactor is IN SCOPE.** `AuthRequired` already loads the user row — store it in `c.Locals("user")` and have handlers (`connection.go` register path, etc.) read it instead of re-querying `FindUserByID`. Removes the redundant ~167 q/s second lookup (audit §6.1b). The other half of the PERF-04 win.

### Context propagation (PERF-07)

- **D-08:** *(Claude's discretion — recommendation captured for research.)* Thread `ctx context.Context` through repository function **signatures** and call `db.WithContext(ctx)` internally, rather than scattering `db.WithContext` at every call site. Cleaner, Go-idiomatic, enables true request-cancellation. Mechanical across all repo files. Research to confirm the exact signature pattern against the existing repo style.

### RUN_SCHEDULER gate (PERF-06)

- **D-09b:** Build the `RUN_SCHEDULER` env gate (default **`true`** for the single-replica v2.2.0 deploy). Deploy stays single-replica per the PROJECT.md constraint; the "second replica with `RUN_SCHEDULER=false` fires no jobs" success criterion is proven by an **automated assertion**, not a real second replica (see D-09).

### Fiber + Postgres config (PERF-09)

- **D-09c:** Fiber config: `BodyLimit: 64*1024`, `ReadTimeout: 15s`, `WriteTimeout: 30s` (per the requirement; audit §6.2 also suggests `IdleTimeout: 120s`, leave `Prefork: false`). Postgres `postgresql.conf` tuned via pgtune defaults for the **data-tier host RAM** (audit §12.4: `shared_buffers` 25%, `effective_cache_size` 50%, `max_connections` 200, `work_mem` 16MB, `maintenance_work_mem` 256MB, `random_page_cost` 1.1) — mounted into the data-tier compose service. Mechanical; values come straight from the audit.

### Indexes (PERF-05, PERF-08)

- **D-09d:** New migration(s) (next is `022_*`): `idx_connections_heartbeat_active` partial index `ON connections(last_heartbeat_at) WHERE disconnected_at IS NULL` (PERF-05), and `idx_connections_connected_at` (PERF-08) for analytics. Audit recommends `CREATE INDEX CONCURRENTLY` — *(Claude's discretion)*: the migration runner must handle that `CONCURRENTLY` cannot run inside a transaction; if the runner wraps every migration in a tx, fall back to a plain `CREATE INDEX` (table is small enough today that the brief lock is acceptable) and note the CONCURRENTLY path for the production backfill. Also fold in PERF-08's 90-day pruning of disconnected `connections` rows into the scheduler (weekly cadence, not per-minute).

### Verification / definition of done

- **D-09:** Phase 6 proves correctness with **automated assertions**, not a live load test: `EXPLAIN ANALYZE` shows `idx_connections_heartbeat_active` range scan (and `idx_connections_connected_at` usage); the cache-hit path emits **zero `servers` SELECT** in the query log; `ctx` cancellation **removes the query from `pg_stat_activity`**; a `RUN_SCHEDULER=false` instance fires no scheduler jobs. The real synthetic **~10k-connection load test** (measure PG write q/s drop, `/auth/refresh` p99, cleanup latency) is captured as a **HUMAN-UAT item and DEFERRED to the end-of-milestone release phase** — consistent with how Phases 4 & 5 deferred hardware-dependent UAT.

### P2 polish folded into scope

- **D-10:** All four audit P2 "nice-to-haves" ride along in Phase 6:
  - **(a)** Redis `--maxmemory 256mb --maxmemory-policy allkeys-lru` on the data-tier Redis service (audit §5.5 — OOM guard; natural fit while editing the data tier for PERF-03).
  - **(b)** PG pool tuning: `SetMaxIdleConns(25)` + `SetConnMaxIdleTime(5 * time.Minute)` in `db.go` (audit §4.3 — complements PERF-09 pgtune).
  - **(c)** Per-job scheduler interval registry (audit §7.4): defer `DeleteExpiredSessions` / `DeleteStaleDevices` to less-frequent ticks; keep expiry-downgrade + connection/reservation cleanup at 1-min cadence.
  - **(d)** GORM `PrepareStmt: true` in `gorm.Config{}` (audit §4.4 — 10–20% lower latency on the hot heartbeat write).

### Claude's Discretion

- PERF-07 ctx-rollout *mechanism* (D-08) — recommended approach captured; research confirms.
- Whether to *additionally* cache the `plan_servers` membership (plan → allowed server IDs) used by D-05's in-Go filter. It changes rarely (admin plan-server writes) so it's cacheable, but the primary amplifier is already removed by D-05. Research/planning decides.
- Migration `CONCURRENTLY`-vs-plain strategy (D-09d) given the migration runner's transaction behavior.
- Exact `/servers` cache TTL within the ≤5min ceiling (D-05 — start 60s).

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Performance audit (SOURCE OF TRUTH — every fix anchored to file:line)
- `docs/audit/PERFORMANCE-AUDIT.md` — the authoritative spec for all nine PERF requirements. Key sections:
  - §2.1–2.5 — missing indexes (PERF-05 §2.2, PERF-08 §2.3 + pruning §2.5)
  - §5.2 — `/servers` Redis cache + invalidation (PERF-01)
  - §6.1 — `AuthRequired` user existence+tier cache **and** the `c.Locals` redundant-lookup fix (PERF-04 + D-07)
  - §7.2 — `RUN_SCHEDULER` env gate (PERF-06); §7.4 — per-job intervals (D-10c)
  - §8.2 — heartbeat → Redis + bulk flush (PERF-02)
  - §4.1 — `db.WithContext(ctx)` everywhere (PERF-07); §4.3 — pool tuning (D-10b); §4.4 — `PrepareStmt` (D-10d)
  - §12.3 — off-host PG/Redis split (PERF-03); §12.4 — Postgres tuning (PERF-09)
  - §5.5 — Redis `maxmemory-policy` (D-10a); §6.2 — Fiber timeouts (PERF-09)
  - §13.3 — mobile react-query retry jitter (**deferred** — see Deferred Ideas)
- `docs/audit/MASTER-PLAN.md` "Tranche 3 — Performance & scalability" — the 9 wins ranked by impact/effort.

### Requirements
- `.planning/REQUIREMENTS.md` PERF-01..PERF-09 (lines 75–83) + SCALE-01 (line 121 — multi-replica, depends on PERF-06 + plan_id cache).

### Architecture
- `docs/ADR-007-lava-sso-rework.md` — admin endpoint shapes that the cache-invalidation hooks attach to (server-write + plans + webhook).

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `server/api/internal/cache/plans_cache.go` — the **cache-aside + fail-open** pattern (Get returns "" on miss/outage, Set is best-effort, explicit `DEL` on bust). Mirror this exactly for `cache:servers:active` (D-05) and `user:<id>` (D-06).
- `server/api/internal/scheduler/scheduler.go` — single ticker + **tick-counter staggering** (D-26; `expiryTickCount % 10`). The 10s heartbeat flush (D-03), per-job intervals (D-10c), and `RUN_SCHEDULER` gate (D-09b) land here. Note: current `cleanupInterval` is 1 min — the 10s flush needs either a faster sub-ticker or its own goroutine.
- `server/api/internal/middleware/auth.go` — `AuthRequired` **already loads the full user row** (HOTFIX-02 existence check + D-29 plan_id fallback). The user cache (D-06), `c.Locals("user")` (D-07), and `WithContext` (D-08) all attach here.
- `server/api/internal/cache/redis.go` — existing `IsTokenBlacklisted` / rate-limit helpers; the fail-open contract to follow.

### Established Patterns
- Cache-aside + **fail-open**: a Redis outage must never break a request path (plans_cache.go contract). Non-negotiable for D-05/D-06.
- Schema via **SQL migrations**, not GORM AutoMigrate (`db.go` comment; migrations run from `001` → `021`, next is `022`).
- Synchronous cache bust **before the write handler returns** (plans_cache `BustPlansCache` called by admin handlers — the model for D-05/D-06 invalidation).

### Integration Points
- `server/api/internal/handler/servers.go` — `/servers` cache read + plan filter (`ListServersForPlan(db, planID)` for users, `ListActiveServers(db)` for admins). D-05.
- `server/api/internal/handler/admin.go` — `AdminCreateServer` (:309), `AdminUpdateServer` (:390), `AdminDeleteServer` (:460) → servers-cache bust (D-05) + user-cache bust on user-update (D-06).
- `server/api/internal/handler/webhook_lava.go` — Pro-grant → user-cache bust (D-06).
- `server/api/internal/repository/connection_repo.go` — heartbeat write, `CleanupStaleConnections`, the new bulk-flush UPDATE (D-03/D-04).
- `server/api/internal/repository/db.go` — pool tuning (D-10b) + `PrepareStmt` (D-10d) in `NewDB`.
- `server/api/cmd/main.go` — Fiber config (PERF-09/D-09c) + `RUN_SCHEDULER` wiring (D-09b).
- `docker-compose.prod.yml` → split into prod (app) + new `docker-compose.data.yml` (PG/Redis), host-parameterized URLs (D-01/D-02), Redis maxmemory (D-10a), mounted `postgresql.conf` (D-09c).
- `server/api/migrations/022_*` (and 023) — `idx_connections_heartbeat_active` (PERF-05) + `idx_connections_connected_at` (PERF-08).

</code_context>

<specifics>
## Specific Ideas

- Heartbeat key shape is fixed by the audit: `hb:<conn_id>` with `EX 600`. Dirty set is `hb:dirty` (D-04).
- `/servers` cache key: `cache:servers:active` (single global blob — mirrors the `cache:plans:public:*` namespace convention).
- User cache key: `user:<id>`; TTL ≤ 5s.
- Heartbeat flush bulk UPDATE: `UPDATE connections SET last_heartbeat_at = $now WHERE id = ANY($dirty_ids::uuid[]) AND disconnected_at IS NULL`.
- Postgres tuning values are pgtune-for-host-RAM; the audit §12.4 lists the concrete starting numbers.

</specifics>

<deferred>
## Deferred Ideas

- **Real ~10k synthetic load test** (k6/vegeta drive + measure PG write q/s drop, `/auth/refresh` p99, cleanup latency) — captured as a HUMAN-UAT item, run against staging in the **end-of-milestone release phase**. Phase 6 ships with assertion-level proof only (D-09).
- **Mobile react-query retry jitter** (audit §13.3 — exponential backoff + jitter to prevent thundering-herd on API recovery) — a *mobile* surface change; fits Phase 8 cleanup, not folded into this backend-focused phase.
- **Managed PG / managed Redis** — not chosen (D-01 went self-hosted-on-second-host); revisit only if scaling past a single second VM.
- **Multi-replica API deploy** (SCALE-01) — the `RUN_SCHEDULER` gate + stateless auth are built here, but the actual N-replica deploy is post-launch (PROJECT.md: "not required for launch").
- **Cache the `plan_servers` membership** for the D-05 in-Go filter — possible follow-up; not required since D-05 already removes the primary amplifier. Left to research/planning discretion.

*No pending todos matched this phase (todo match-phase returned 0).*

</deferred>

---

*Phase: 06-performance-scalability*
*Context gathered: 2026-05-29*
