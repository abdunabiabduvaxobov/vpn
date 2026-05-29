# Phase 6: Performance & scalability - Research

**Researched:** 2026-05-29
**Domain:** Go 1.25 + Fiber v2 + GORM (pgx) + Postgres 16 + Redis 7 backend performance hardening
**Confidence:** HIGH (every claim verified against the live codebase as it exists today; the few unknowns are flagged in the Assumptions Log)

## Summary

Phase 6 is overwhelmingly pre-decided by `docs/audit/PERFORMANCE-AUDIT.md` and locked in CONTEXT.md as D-01..D-10. This research **confirms every decided approach is implementable against the live code exactly as written** and **resolves the four open forks**. No locked decision was found infeasible — there are **zero BLOCKERs**. There is one material operational caveat (the migration runner only fires on an empty data volume) that the planner MUST surface as a runbook step, not a code change.

The repository layer is uniformly **free functions taking `db *gorm.DB` as the first parameter** (e.g. `repository.FindUserByID(db, id)`), called directly from Fiber handlers that close over `(logger, db)` (and now `redisClient` for the plans handlers). This single fact drives the D-08 ctx-propagation answer: add `ctx context.Context` as the **first** parameter of each repo function and call `db.WithContext(ctx)` once inside. The cache-aside + fail-open pattern in `plans_cache.go` is clean and copy-pasteable for both new caches. The scheduler is a single 1-minute ticker with a tick-counter (`expiryTickCount % 10`); the 10s heartbeat flush needs its **own faster ticker/goroutine** because 10s is not an integer sub-division reachable from a 1-minute tick.

**Primary recommendation:** Implement the nine PERF requirements in the order the audit ranks them, mirroring `plans_cache.go` / `BustPlansCache` for the two new caches, threading `ctx` through repo signatures, and treating `CREATE INDEX CONCURRENTLY` as the production path (the runner does NOT wrap in a transaction — already proven by migration 017). Surface the empty-volume migration-runner limitation as the single most important planning caveat.

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

- **D-01:** Split the data tier into its own compose file — `docker-compose.data.yml` holds `postgres` + `redis`; `docker-compose.prod.yml` keeps the app tier (`api` + `tunnel`). Connection config becomes fully **host-parameterized** (`DB_HOST` / `REDIS_HOST` env vars feeding `DATABASE_URL` / `REDIS_URL`), defaulting to the service names for co-located dev. Moving PG/Redis to a second host is a documented ops-runbook step with zero code/image change. PERF-03 success criterion ("`docker-compose.prod.yml` reflects the split") is satisfied by the restructure; the physical move is the operator's call.
- **D-02:** Cross-host link secured by **private network + firewall** — bind PG/Redis to a trusted private interface, keep `sslmode=disable` + Redis `--requirepass` over that link. Connection strings written **TLS-ready**; document the `sslmode=require`/`verify-full` + Redis-TLS upgrade path. Rejected: full TLS now.
- **D-03:** **Postgres remains source-of-truth** for `connections.last_heartbeat_at`. Heartbeat handler writes **only to Redis** (`SET hb:<conn_id> <unix_ts> EX 600`). Scheduler job flushes every **10s** via one bulk `UPDATE connections SET last_heartbeat_at = $now WHERE id = ANY($dirty_ids) AND disconnected_at IS NULL`. `CleanupStaleConnections` + the new PERF-05 partial index keep reading `last_heartbeat_at` unchanged. Rejected: Redis-as-live-truth + `EXISTS hb:<id>` cleanup.
- **D-04:** **Dirty-set tracking.** Heartbeat handler does `SET hb:<id>` + `SADD hb:dirty <id>` in a single pipeline. Flush job: `SMEMBERS hb:dirty` → bulk `UPDATE` → `SREM` flushed ids. O(changed), no `SCAN`, multi-replica safe.
- **D-05:** `/servers` cache = single global blob `cache:servers:active` holding `ListActiveServers` JSON. Plan-scoped filter stays **live in Go** (admin → full list; non-admin → intersect cached list with plan's allowed server IDs). Invalidation surface = the 3 admin server-write handlers only (synchronous `DEL` before returning). Cache-aside + fail-open per `plans_cache.go`. TTL ≤ 5 min — start at 60s to match `plans_cache` precedent unless research argues for the higher ceiling.
- **D-06:** User existence+tier cache for `AuthRequired`. Key `user:<id>` (existence + tier), TTL ≤ 5s, cache-aside + fail-open. **Explicit synchronous bust on ALL mutation paths:** (1) admin user-update; (2) payment webhook Pro-grant (`webhook_lava.go`); (3) user delete / `PerformRestore`; (4) scheduler expiry-downgrade.
- **D-07:** **`c.Locals("user")` refactor is IN SCOPE.** `AuthRequired` already loads the user row — store it in `c.Locals("user")` and have handlers read it instead of re-querying `FindUserByID`.
- **D-08:** *(Claude's discretion — confirmed below.)* Thread `ctx context.Context` through repository function **signatures** and call `db.WithContext(ctx)` internally, rather than scattering `db.WithContext` at call sites.
- **D-09b:** Build `RUN_SCHEDULER` env gate (default **`true`** for single-replica v2.2.0). Deploy stays single-replica; "second replica with `RUN_SCHEDULER=false` fires no jobs" proven by an **automated assertion**, not a real second replica.
- **D-09c:** Fiber config: `BodyLimit: 64*1024`, `ReadTimeout: 15s`, `WriteTimeout: 30s`, `IdleTimeout: 120s`, `Prefork: false`. Postgres `postgresql.conf` tuned via pgtune defaults for data-tier host RAM (`shared_buffers` 25%, `effective_cache_size` 50%, `max_connections` 200, `work_mem` 16MB, `maintenance_work_mem` 256MB, `random_page_cost` 1.1) — mounted into the data-tier compose service.
- **D-09d:** New migration(s) (next is `022_*`): `idx_connections_heartbeat_active` partial index `ON connections(last_heartbeat_at) WHERE disconnected_at IS NULL` (PERF-05); `idx_connections_connected_at` (PERF-08). CONCURRENTLY vs plain resolved below. Fold in PERF-08's 90-day pruning of disconnected `connections` rows into the scheduler (weekly cadence).
- **D-09:** Phase proves correctness with **automated assertions**, not a live load test. Real ~10k load test DEFERRED to release phase.
- **D-10:** Four P2 polish items ride along: (a) Redis `--maxmemory 256mb --maxmemory-policy allkeys-lru`; (b) PG pool `SetMaxIdleConns(25)` + `SetConnMaxIdleTime(5*time.Minute)`; (c) per-job scheduler interval registry; (d) GORM `PrepareStmt: true`.

### Claude's Discretion

- PERF-07 ctx-rollout *mechanism* (D-08) — recommended approach captured; research confirms. **→ Resolved: thread `ctx` as first repo param (Fork 1).**
- Whether to *additionally* cache `plan_servers` membership (plan → allowed server IDs) for D-05's in-Go filter. **→ Resolved: NO (Fork 3).**
- Migration `CONCURRENTLY`-vs-plain strategy (D-09d) given the migration runner's transaction behavior. **→ Resolved: CONCURRENTLY (Fork 2).**
- Exact `/servers` cache TTL within the ≤5min ceiling (D-05 — start 60s). **→ Resolved: 60s (Fork 4).**

### Deferred Ideas (OUT OF SCOPE)

- **Real ~10k synthetic load test** — HUMAN-UAT item, run in end-of-milestone release phase. Phase 6 ships assertion-level proof only.
- **Mobile react-query retry jitter** (audit §13.3) — mobile surface change, fits Phase 8.
- **Managed PG / managed Redis** — not chosen (D-01 went self-hosted-on-second-host).
- **Multi-replica API deploy** (SCALE-01) — gate built here, N-replica deploy post-launch.
- **Cache the `plan_servers` membership** — possible follow-up; not required (D-05 removes the primary amplifier).
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| PERF-01 | `GET /servers` cached in Redis with admin-side invalidation; TTL ≤ 5 min | `plans_cache.go` is the verbatim template. `ListServers` handler (`servers.go:100`) is the read site; `ListActiveServers` (`server_repo.go:12`) is the cached payload. Bust sites: `AdminCreateServer/UpdateServer/DeleteServer` (`admin.go:309/390/460`). |
| PERF-02 | Heartbeat → Redis first; scheduler bulk flush to PG every 10s | `UpdateHeartbeat` (`connection_repo.go:148`) is replaced by Redis write. `HeartbeatConnection` handler (`connection.go:273`). Scheduler (`scheduler.go`) needs a new 10s ticker — see Architecture. Bulk UPDATE pattern documented below. |
| PERF-03 | PG + Redis on separate hosts / scaled services in prod | `docker-compose.prod.yml` already env-parameterizes `DATABASE_URL`/`REDIS_URL` via `${POSTGRES_*}`/`${REDIS_PASSWORD}`. Split + `DB_HOST`/`REDIS_HOST` indirection documented below. |
| PERF-04 | User existence+tier cached in Redis for `AuthRequired`, TTL ≤ 5s; busted on admin user-update | `AuthRequired` (`auth.go:101`) is the read site; it already loads the full row. `cache/redis.go` + `plans_cache.go` are the pattern templates. Full bust-site inventory below. |
| PERF-05 | `idx_connections_heartbeat_active` partial index for the stale sweep O(connected) | Migration `022_*`. Stale sweep is `CleanupStaleConnections` (`connection_repo.go:113`). Index DDL + EXPLAIN assertion below. |
| PERF-06 | `RUN_SCHEDULER` env gate; scheduler only starts when `true` | `scheduler.Start()` called unconditionally at `main.go:128`. Gate pattern + automated assertion below. |
| PERF-07 | Every GORM call uses `db.WithContext(ctx)`; no query outlives its request | Repo layer is free-functions on `db *gorm.DB`. Concrete before/after + full inventory below (Fork 1). pgx v5 cancellation confirmed. |
| PERF-08 | `idx_connections_connected_at` index + 90-day pruning in scheduler | Migration `022_*`/`023_*`. Analytics queries at `admin_repo.go`. Prune query + weekly cadence below. |
| PERF-09 | Fiber `BodyLimit: 64*1024`, `ReadTimeout: 15s`, `WriteTimeout: 30s`; PG `postgresql.conf` pgtuned | Fiber config at `main.go:146`. PG config mounted into data-tier compose service. Concrete config below. |
</phase_requirements>

## Project Constraints (from CLAUDE.md)

These are **locked** and research must not contradict them:

- **Backend stack:** Go 1.25 + Fiber v2 + GORM + Postgres 16 + Redis 7. Locked. No language switch. (`go.mod` confirms `go 1.25.0`, `gofiber/fiber/v2 v2.52.5`, `gorm.io/gorm v1.30.0`, `gorm.io/driver/postgres v1.5.9`, `redis/go-redis/v9 v9.18.0`, `jackc/pgx/v5 v5.5.5`.)
- **Deployment:** Single VM via Docker Compose for v2.2.0. Multi-replica (SCALE-01) is built-but-not-deployed here (`RUN_SCHEDULER` gate only).
- **Security:** Launching Pro = real money flow; Critical/High audit findings MUST land before any user pays. Phase 6 is perf, but D-10a (Redis `maxmemory` OOM guard) and D-09c (Fiber timeouts / slowloris defense) are also security-relevant — do not drop them.
- **Webhook idempotency:** `webhook_lava.go` is idempotent (UNIQUE on `lava_webhook_events`, returns 500 on processing error). The D-06 user-cache bust added to the Pro-grant path must be **idempotent and inside / after the successful side-effect**, never blocking the 200/500 contract.
- **GSD workflow enforcement:** All edits go through a GSD command. Research is read-only.

**No project skills found** — checked `.claude/skills/` and `.agents/skills/`; neither exists.

## Standard Stack

This phase adds **no new libraries**. Every requirement is met with the already-pinned dependencies. [VERIFIED: server/api/go.mod]

### Core (already present — versions verified in go.mod)
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/gofiber/fiber/v2` | v2.52.5 | HTTP server; `fiber.Config` timeouts (PERF-09); `c.Context()` for ctx (PERF-07); `c.Locals` (D-07) | Already the framework; locked |
| `gorm.io/gorm` | v1.30.0 | `db.WithContext(ctx)` (PERF-07); `gorm.Config{PrepareStmt:true}` (D-10d) | Already the ORM; locked |
| `gorm.io/driver/postgres` | v1.5.9 | pgx-backed driver; honors ctx cancellation at the protocol layer | Default Postgres driver |
| `github.com/jackc/pgx/v5` | v5.5.5 | Underlying pgx driver — sends a cancel-request on ctx cancellation (PERF-07 abort) | Standard pgx; bundled by the gorm driver |
| `github.com/redis/go-redis/v9` | v9.18.0 | `SET ... EX`, `SADD`/`SMEMBERS`/`SREM` pipeline (D-03/D-04), `Get`/`Set`/`Del` (D-05/D-06) | Already the Redis client; locked |
| `go.uber.org/zap` | (present) | Structured logging in scheduler/handlers | Already in use |

### Supporting (test-only — already present)
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `github.com/alicebob/miniredis/v2` | v2.37.0 | In-memory Redis fake for unit-testing the two new caches + dirty-set flush | Cache/flush unit tests (Wave 0) |
| `github.com/testcontainers/testcontainers-go` (+ `/modules/postgres`) | (present) | Real Postgres 16 container for EXPLAIN-plan + ctx-cancellation + index assertions | Validation Architecture assertions (a)(c) |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Per-job interval registry (D-10c) hand-rolled in `scheduler.go` | `robfig/cron` | New dependency; the existing single-ticker + tick-counter is already the established idiom (D-26) — extend it, do not replace it. **Recommend: extend existing.** |
| Dirty-set flush via `SMEMBERS` (D-04) | `SCAN hb:*` | CONTEXT explicitly rejected SCAN; `SMEMBERS hb:dirty` is O(changed) and the chosen approach. **Honor D-04.** |

**Installation:** none. `go.mod` is unchanged by this phase.

**Version verification:** all versions read directly from `server/api/go.mod` on 2026-05-29 — these are the exact compiled-in versions, not registry-latest. [VERIFIED: server/api/go.mod]

## Architecture Patterns

### Repository style (THE load-bearing fact for D-07 and D-08)

Every repository function is a **package-level free function whose first parameter is `db *gorm.DB`**. There are no repo structs, no methods, no interfaces. Handlers are closures over `(logger *zap.Logger, db *gorm.DB)` (plus `redisClient` and `cfg` where needed) and call repo functions directly. [VERIFIED: repository/*.go, handler/*.go]

```go
// Repo (repository/user_repo.go:39)
func FindUserByID(db *gorm.DB, id string) (*model.User, error) { ... }

// Handler (handler/connection.go:36)
userRecord, err := repository.FindUserByID(db, userID)
```

### Pattern 1: Cache-aside + fail-open (mirror exactly for D-05 and D-06)
**What:** `Get` returns `""` (not error) on miss/Redis-outage; `Set` is best-effort; explicit `DEL` on bust. Handler falls through to DB on empty.
**When to use:** both new caches (`cache:servers:active`, `user:<id>`).
**Source:** `server/api/internal/cache/plans_cache.go` [VERIFIED]
```go
// cache:servers:active — mirror of GetPlansCache/SetPlansCache/BustPlansCache.
const serversActiveKey = "cache:servers:active"
const serversActiveTTL = 60 * time.Second // Fork 4: matches plansPublicCacheTTL exactly

func GetServersCache(ctx context.Context, client *redis.Client) (string, error) {
    if client == nil { return "", nil }
    val, err := client.Get(ctx, serversActiveKey).Result()
    if err != nil { return "", nil } // redis.Nil OR transient error → fall through to DB
    return val, nil
}
func SetServersCache(ctx context.Context, client *redis.Client, jsonBody string) error {
    if client == nil { return nil }
    return client.Set(ctx, serversActiveKey, jsonBody, serversActiveTTL).Err()
}
func BustServersCache(ctx context.Context, client *redis.Client) error {
    if client == nil { return nil }
    return client.Del(ctx, serversActiveKey).Err()
}
```
For `user:<id>` (D-06), the cached value is existence+tier. Recommend a tiny JSON `{"tier":"pro"}` (presence of the key = "exists"); empty/`redis.Nil` = miss → DB lookup. TTL = 5s.

### Pattern 2: ctx threading through repo signatures (Fork 1 — PERF-07/D-08)
**What:** add `ctx context.Context` as the **first** parameter of each repo function; call `db.WithContext(ctx)` once at the top of the body. Handlers pass `c.Context()` (Fiber's per-request context, already used at `auth.go:83` for `IsTokenBlacklisted`).
**Before / After (copy-pasteable):**
```go
// BEFORE  (repository/user_repo.go:39)
func FindUserByID(db *gorm.DB, id string) (*model.User, error) {
    var user model.User
    result := db.First(&user, "id = ?", id)
    ...
}
// AFTER
func FindUserByID(ctx context.Context, db *gorm.DB, id string) (*model.User, error) {
    var user model.User
    result := db.WithContext(ctx).First(&user, "id = ?", id)
    ...
}

// BEFORE  (handler/connection.go:36)
userRecord, err := repository.FindUserByID(db, userID)
// AFTER
userRecord, err := repository.FindUserByID(c.Context(), db, userID)
```
For functions called from the **scheduler** (no Fiber ctx), pass `context.Background()` (or a `context.WithTimeout` per cleanup pass — recommended so a wedged cleanup query can't hang the ticker). For functions called from the **Telegram bot** goroutine, pass `botCtx` (already in scope at `main.go:364`).

### Anti-Patterns to Avoid
- **`db.WithContext` at every call site (D-08 rejected alternative):** scatters the same boilerplate across ~30 handler files and is easy to miss. Thread the signature instead — the compiler then forces every caller to supply a ctx.
- **Caching `last_heartbeat_at` semantics changes:** D-03 keeps PG as source of truth. Do NOT make `CleanupStaleConnections` read Redis (CONTEXT rejected this). The cache flush updates the same column the cleanup already reads.
- **Busting `cache:plans:public:*` when an admin edits a SERVER:** plans cache and servers cache are distinct namespaces. Server writes bust `cache:servers:active` only; plan writes bust `cache:plans:public:*` only (already wired).

### Recommended file touch-map (no new packages)
```
server/api/internal/cache/
├── servers_cache.go     # NEW: Get/Set/BustServersCache (mirror plans_cache.go)
├── user_cache.go        # NEW: Get/Set/BustUserCache (user:<id>, TTL 5s)
└── heartbeat_cache.go   # NEW: SET hb:<id>+SADD hb:dirty pipeline; SMEMBERS/SREM flush helpers
server/api/internal/repository/   # ctx added to every exported func (PERF-07)
server/api/internal/scheduler/scheduler.go  # 10s flush ticker, per-job intervals, prune, expiry-downgrade bust
server/api/internal/middleware/auth.go      # user cache read + c.Locals("user") (D-06/D-07)
server/api/internal/handler/{servers,connection,admin,webhook_lava}.go  # cache reads + busts
server/api/cmd/main.go            # Fiber timeouts, RUN_SCHEDULER gate, pass redisClient to admin handlers
server/api/internal/repository/db.go  # pool tuning + PrepareStmt
server/api/migrations/022_*.sql, 023_*.sql  # indexes (CONCURRENTLY)
docker-compose.prod.yml + docker-compose.data.yml (NEW) + postgresql.conf (NEW)
```

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Query cancellation on client disconnect (PERF-07) | A manual goroutine + `time.AfterFunc` per query | `db.WithContext(c.Context())` | Fiber + GORM + pgx already wire ctx → Postgres cancel-request. pgx v5 sends a CancelRequest on the wire when ctx is cancelled. |
| Atomic INCR+EXPIRE / dirty-set flush atomicity | MULTI/EXEC hand-rolled, or a SCAN loop | Existing `redis.NewScript` idiom (`redis.go:75`) for atomic ops; `SADD`/`SMEMBERS`/`SREM` for dirty set | The Lua-script pattern is already in-tree (CRIT-02 fix). `SMEMBERS hb:dirty` is O(changed) — D-04 chose it over SCAN. |
| Cache-aside + fail-open | A bespoke cache wrapper with custom error handling | `plans_cache.go` copy | Already battle-tested; the fail-open contract is non-negotiable (a Redis outage must never break a request). |
| Per-job scheduler intervals | `robfig/cron` (new dep) | Extend the existing tick-counter (`expiryTickCount % N`) | The codebase already uses tick-counter staggering (D-26). Adding a dep for 6 jobs is overkill. |
| Migration transaction handling for CONCURRENTLY | A custom golang-migrate/goose integration | Keep the existing `docker-entrypoint-initdb.d` + `psql -f` per-file runner | Migration 017 already runs `CREATE UNIQUE INDEX CONCURRENTLY` through this exact runner with no tx wrapping. Proven. |

**Key insight:** This phase is almost entirely *deletion of redundant DB round-trips* and *moving config*, not building new machinery. The two genuinely new pieces (heartbeat dirty-set, two caches) all have an in-tree template.

---

## FORK RESOLUTIONS (the high-value output)

### Fork 1 — D-08 ctx-propagation mechanism (PERF-07): RESOLVED → thread `ctx` as first repo param

**Live repo style:** confirmed. Repos are free functions `func Xxx(db *gorm.DB, ...) (...)`, called directly from handler closures. No structs/interfaces. [VERIFIED: repository/user_repo.go:39, server_repo.go:12, connection_repo.go, plan_repo.go:16, session_repo.go]

**Recommended signature:** add `ctx context.Context` as the **first** parameter; call `db.WithContext(ctx)` once at the top of the body (see Pattern 2 for the exact before/after). This is the cleaner, Go-idiomatic option CONTEXT recommended, and it is mechanical.

**Inventory of repo files needing the change** (every exported function that runs a query; ~all of them). Each handler/middleware/scheduler/bot call site that invokes them also changes to pass a ctx:

| Repo file | Notable functions | Caller ctx source |
|-----------|-------------------|-------------------|
| `user_repo.go` | `FindUserByID`, `DeleteUser`, `UpdateUser`, `UpdateUserTier`, `DowngradeExpiredSubscriptions`, `DeleteOrphanGuestUser`, `UpdateUserName`, SSO upserts | handlers → `c.Context()`; scheduler → `context.Background()` (or per-pass timeout) |
| `connection_repo.go` | `CreateConnection`, `CreateConnectionAtomic`, `DisconnectConnection`, `CountActiveConnections`, `ListActiveConnectionsByUser`, `CleanupStaleConnections`, `CleanupStaleReservations`, `UpdateHeartbeat` (being removed), `FindConnectionByID` | handlers → `c.Context()`; cleanup* → scheduler ctx |
| `server_repo.go` | `ListActiveServers`, `FindServerByID`, `CreateServer`, `UpdateServer`, `DeleteServer` | handlers → `c.Context()` |
| `plan_repo.go` | `FindPlanByID/ByCode`, `FindSystemPlanID`, `ListActivePlans`, `ListServersForPlan`, `IsServerAllowedForPlan`, `SetUserPlan`, all CRUD | handlers → `c.Context()`; webhook → `c.Context()` |
| `session_repo.go` | `FindSessionByTokenHash`, `CreateSession`, `DeleteSession`, `DeleteExpiredSessions` | auth handlers → `c.Context()`; scheduler → bg ctx |
| `admin_repo.go` | `FindUserByIDAdmin`, list/stats/analytics, `ListUsers` (COUNT) | admin handlers → `c.Context()` |
| `expiry_repo.go` | `DowngradeExpiredPlans` | scheduler → bg ctx |
| `recovery_repo.go` | `PerformRestore` | bot goroutine → `botCtx` |
| `webhook_event_repo.go`, `subscription_repo.go`, `device_repo.go`, `link_code_repo.go`, `audit_repo.go` | all query funcs | respective handler `c.Context()` / scheduler bg ctx |

Inside transactions (`db.Transaction(func(tx *gorm.DB) ...)`), the `tx` already inherits the parent `db`'s context once `db.WithContext(ctx).Transaction(...)` is used — so add the `.WithContext(ctx)` to the outer call (e.g. `SetUserPlan`, `PerformRestore`, `ReplacePlanServers`, `SoftDeletePlan`, `ReplaceOffer`). [VERIFIED: GORM propagates the statement context into the tx callback]

**Does ctx-cancellation actually abort the DB query?** YES, with this stack. The gorm postgres driver is pgx v5 (`go.mod`: `jackc/pgx/v5 v5.5.5`). When the `context.Context` passed via `db.WithContext(ctx)` is cancelled (client drops the TCP connection → Fiber cancels `c.Context()`), pgx sends a Postgres CancelRequest on the wire and the in-flight query is terminated; the row disappears from `pg_stat_activity`. This is exactly the DoS-mitigation the audit §4.1 describes and is the basis for Validation assertion (c). [VERIFIED: pgx v5 is the driver; CITED: standard database/sql + pgx context-cancellation contract — see Assumptions A2 for the test that proves it]

**Effort:** Medium (mechanical, but touches every repo file + every call site). Compiler-guided once the signatures change — every miss is a build error.

### Fork 2 — Migration CONCURRENTLY-vs-plain (D-09d, PERF-05/PERF-08): RESOLVED → CONCURRENTLY

**How migrations are applied (the decisive fact):** the production runner is Postgres's native **`docker-entrypoint-initdb.d`**. `docker-compose.prod.yml:19` mounts `./migrations:/docker-entrypoint-initdb.d`, and the entrypoint runs **`psql -f <file>` per file, WITHOUT wrapping in `BEGIN/COMMIT`**. [VERIFIED: docker-compose.prod.yml:17-19; deploy.yml:103-135; migration 017 header comment lines 19-28]

Therefore `CREATE INDEX CONCURRENTLY` **can** run — there is no auto-tx wrap. This is already proven: migration `017_sessions_refresh_token_hash_unique.sql:42` runs `CREATE UNIQUE INDEX CONCURRENTLY` through this exact runner. **Recommend CONCURRENTLY** for both new indexes (PERF-05 `idx_connections_heartbeat_active`, PERF-08 `idx_connections_connected_at`) because `connections` is a high-churn table that will grow to millions of rows — a plain `CREATE INDEX` takes an `ACCESS EXCLUSIVE` lock that blocks the heartbeat write path for the build duration.

**Critical migration-runner caveat the planner MUST surface (NOT a code change — a runbook step):** `docker-entrypoint-initdb.d` scripts run **only on first container init, when the `pgdata` volume is empty**. On an existing production database (volume already populated), `docker compose up` does NOT re-run any migration file — including the new `022`/`023`. The migration files are effectively a *fresh-install seed*, not an incremental migration system. [VERIFIED: standard postgres image behavior; corroborated by deploy.yml copying migrations every deploy yet relying on initdb semantics]

Consequence for Phase 6:
1. The new indexes will NOT be created on the live `194.87.31.44` database by a normal deploy.
2. The production backfill MUST be a **manual runbook step**: `psql "$DATABASE_URL" -f 022_add_perf_indexes.sql` (or `docker exec vpn-postgres psql -U vpnapp -d vpnapp -f /docker-entrypoint-initdb.d/022_...`). Because the file uses `CREATE INDEX CONCURRENTLY`, it runs online without locking writes — perfect for a live backfill.
3. Keep `CONCURRENTLY` + `IF NOT EXISTS` so the file is idempotent and safe whether applied by initdb (fresh) or by hand (live).

**Recommended DDL for migration `022_add_perf_indexes.sql`** (no BEGIN/COMMIT — CONCURRENTLY forbids it; the test harness already special-cases files containing `CONCURRENTLY` via `splitSQLStatements`, running each statement on its own connection — `migrations_test.go:38-61`):
```sql
-- PERF-05: partial b-tree so the stale-connection sweep is O(connected) not O(history).
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_connections_heartbeat_active
    ON connections (last_heartbeat_at)
    WHERE disconnected_at IS NULL;

-- PERF-08: analytics scans on connected_at (admin_repo.go date-bucket queries).
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_connections_connected_at
    ON connections (connected_at);
```
**On the COALESCE-removal in the cleanup predicate (audit §2.2 suggestion):** OPTIONAL, not in PERF-05's strict scope. The new partial index covers `last_heartbeat_at WHERE disconnected_at IS NULL`. The current sweep predicate is `disconnected_at IS NULL AND COALESCE(last_heartbeat_at, connected_at) < cutoff` (`connection_repo.go:118`). The `COALESCE` wrapper prevents the planner from using the new index as a clean range scan. To get the assertion-grade range scan (Validation (a)), the planner should rewrite the predicate to drop the COALESCE: `disconnected_at IS NULL AND last_heartbeat_at < cutoff`. This is SAFE because every active row has a non-null `last_heartbeat_at`: migration 008 backfilled it for then-active rows (`008_connection_heartbeat.sql:3`), and `CreateConnection`/`CreateConnectionAtomic` set it on every insert (`connection_repo.go:23,46`). **Recommend folding the COALESCE-drop into Phase 6** so the EXPLAIN assertion can actually demonstrate the range scan. Note `CleanupStaleReservations` has the same COALESCE on a `status='connecting'` subset — leave it or apply the same drop; it's lower-volume.

### Fork 3 — Additionally cache `plan_servers` membership: RESOLVED → NO

D-05 already removes the primary amplifier (`ListActiveServers` for every `/servers` call) via `cache:servers:active`. The remaining per-request DB cost for a non-admin is `ListServersForPlan(db, planID)` (`plan_repo.go:83`) — a single indexed JOIN (`plan_servers ps ON ps.server_id = vpn_servers.id WHERE ps.plan_id = ? AND is_active`). [VERIFIED]

**Recommend NOT caching plan_servers membership**, because:
1. **D-05's design already eliminates the amplifier.** With `cache:servers:active` serving the heavy blob, the residual cost is a small JOIN over `plan_servers` (a handful of rows per plan today; bounded by server count ~20 long-term). The planner can implement D-05's "intersect cached blob with plan's allowed IDs in Go" by caching ONLY the active-server blob and doing the membership filter against the live `plan_servers` rows — but at ~20 servers × a few plans the JOIN is sub-ms and runs against indexes (`idx_plan_servers_server`, PK on `(plan_id, server_id)`).
2. **Invalidation complexity.** A per-plan membership cache adds bust sites: `ReplacePlanServers`, `AddPlanServer`, `RemovePlanServer`, and (transitively) plan delete + server delete cascade. D-05 deliberately avoided per-plan cache keys precisely so an admin plan↔server remap stays automatically correct. Adding membership caching reintroduces exactly the per-plan invalidation surface D-05 was designed to avoid.
3. **Wrong order of magnitude.** The audit's 833 q/s figure (§5.2) was the `ListActiveServers` blob, not the membership filter. Membership is already cheap.

If a future profiling pass shows the JOIN is hot (it won't at this scale), revisit — but for Phase 6, NO. The simplest correct implementation: cache the active-server JSON blob; for non-admins, fetch the plan's allowed server-IDs (or do the JOIN) live and filter the cached blob in Go. Decide between "filter cached blob by plan IDs in Go" vs "JOIN live" as an implementation detail — both are fine; the JOIN is marginally simpler and already exists as `ListServersForPlan`.

### Fork 4 — `/servers` cache TTL: RESOLVED → 60s

`plans_cache.go:19` sets `plansPublicCacheTTL = 60 * time.Second`. [VERIFIED] CONTEXT D-05 says "start at 60s to match plans_cache precedent unless research argues for the higher ceiling." There is no argument for the higher ceiling: server rows change ~weekly, and the synchronous `DEL` on every admin server-write (D-05) makes the TTL a pure safety net for a missed bust. 60s bounds the worst-case staleness if a `DEL` ever fails (Redis hiccup mid-write) to one minute — identical to the plans cache's already-accepted bound. **Recommend exactly `60 * time.Second`**, named `serversActiveTTL`, mirroring the plans constant.

---

## CONFIRMATIONS AGAINST LIVE CODE (decided approaches verified implementable)

### D-03/D-04 heartbeat → Redis + 10s bulk flush
- **Current write path:** `HeartbeatConnection` (`connection.go:273`) does `FindConnectionByID` (ownership pre-read) + `UpdateHeartbeat` (`connection_repo.go:148`, the `UPDATE ... WHERE id=? AND disconnected_at IS NULL`). [VERIFIED]
- **New path:** replace the `UpdateHeartbeat` call with the Redis pipeline `SET hb:<id> <unix> EX 600` + `SADD hb:dirty <id>`. The ownership pre-read can be folded into the WHERE of the eventual flush (audit §8.3) — or kept for a clean 404; the planner decides. NOTE: the heartbeat handler still needs `c.Locals("user")`/`user_id` to authorize the connection belongs to the caller; with D-07 this no longer costs a DB round-trip.
- **Scheduler integration (the tricky part — VERIFIED):** `scheduler.go` is a **single 1-minute ticker** with `expiryTickCount % 10` staggering (`scheduler.go:15,54-59`). 10s is NOT reachable as an integer sub-division of a 60s tick. **The 10s flush needs its own `time.NewTicker(10 * time.Second)` in a second goroutine inside `Start()`** (added to the same `s.wg` + `s.done` lifecycle so `Stop()` cleans it up). Do not try to fake 10s from the 1-min ticker.
- **Bulk flush job:** `SMEMBERS hb:dirty` → `UPDATE connections SET last_heartbeat_at = $now WHERE id = ANY($dirty_ids::uuid[]) AND disconnected_at IS NULL` → `SREM hb:dirty <flushed ids>`. [matches CONTEXT specifics]
- **Multi-replica safety (VERIFIED safe):** `SMEMBERS`+`SREM` of a single shared `hb:dirty` set in Redis is multi-replica safe — any replica's flush drains the shared set; double-flush is idempotent (same `last_heartbeat_at = $now` write). The race where two replicas both `SMEMBERS` then both `UPDATE` is harmless (idempotent UPDATE) and `SREM` of already-removed members is a no-op. With the `RUN_SCHEDULER` gate (PERF-06) only one replica runs the flush anyway in v2.2.0.
- **Redis-restart durability (VERIFIED acceptable):** `StaleConnectionAfter` default is `3 * time.Minute` (`config.go:107`). A Redis restart loses at most one 10s flush window — well inside the 3-minute grace. PG's `last_heartbeat_at` stays warm (D-03 keeps PG source-of-truth). Matches D-03 rationale.

### D-05/D-06 cache-aside + fail-open
- `plans_cache.go` confirmed: `Get` returns `""` on `redis.Nil` AND on transient error (fail-open); `Set` best-effort; `BustPlansCache` does synchronous `DEL`. [VERIFIED] Mirror exactly (Pattern 1).
- `cache/redis.go` `IsTokenBlacklisted` confirms the same fail-open contract (`return false` on any error). [VERIFIED]

### D-06/D-07 user cache + `c.Locals("user")` — FULL bust-site inventory
- `AuthRequired` (`auth.go:51`) ALREADY loads the full user row via `repository.FindUserByID(db, claims.UserID)` for the HOTFIX-02 existence check (`auth.go:101-114`). [VERIFIED] D-06 wraps this in a `user:<id>` cache read; D-07 stores the loaded `*model.User` in `c.Locals("user", u)` so handlers stop re-querying.
- **Handler that re-queries `FindUserByID` and SHOULD read `c.Locals("user")` instead (the D-07 win):** `RegisterConnection` (`connection.go:36`) — the redundant second lookup the audit §1.2/§6.1b calls out. Also `resolveUserPlanID` (`servers.go:373`) falls back to `FindUserByID` when `c.Locals("plan_id")` is empty; with the cached user in locals it can read from there. Other `FindUserByID` callers (`health.go:114` GetSubscription, `devices.go:94`, `payment.go:80/206`, `telegram.go:117`) are candidates too — the planner should sweep every `protected` handler that calls `FindUserByID(db, userID)` for the *authenticated* user and switch to `c.Locals("user")`. [VERIFIED via grep: 12 call sites total; admin handlers use `FindUserByIDAdmin` for a *different* user-id and must NOT be switched]
- **ALL user-cache bust sites (`user:<id>` DEL) — enumerated and VERIFIED:**
  1. **Admin user-update** — `AdminUpdateUser` (`admin.go:119`, calls `repository.UpdateUser` at :206). *Required by PERF-04.* Bust `user:<id>` (the `:id` param) after a successful update. Handler currently takes `(logger, db)` → add `redisClient` param (same pattern as the plans handlers already do).
  2. **Payment webhook Pro-grant** — `handleLavaPaymentSuccess` (`webhook_lava.go`, `SetUserPlan` at :206) AND `handleLavaRecurringSuccess` (`SetUserPlan` at :279). Bust `user:<inv.UserID>` / `user:<parent.UserID>` after the successful side-effect, before returning nil (200). Keep idempotent — a duplicate webhook that short-circuits on the UNIQUE constraint need not bust again (no state changed).
  3. **User delete / `PerformRestore`** — `PerformRestore` (`recovery_repo.go:49`) DELETEs `newUserID` and rebinds devices to `oldUserID`. Bust `user:<newUserID>` (deleted — clears the zombie) AND `user:<oldUserID>` (its devices/sessions changed; defensively clear so its cached existence/tier can't go stale). Called from the bot goroutine (`bot/recovery.go:369`) — bust must happen there (or via a returned signal) since `PerformRestore` is a repo function with no Redis handle. Also `DeleteUser` (`user_repo.go:54`) and `DeleteOrphanGuestUser` (`user_repo.go:92`, called from `LinkDevice`) delete users — bust those ids too.
  4. **Scheduler expiry-downgrade** — TWO functions both run in the scheduler: `DowngradeExpiredSubscriptions` (`user_repo.go:277`, legacy `subscription_tier`) and `DowngradeExpiredPlans` (`expiry_repo.go:49`, plan_id, D-26). Both flip tier on N users in one bulk UPDATE — they return only a count, not the affected ids. To bust precisely, the planner must either (a) change these to RETURNING the affected user ids then bust each, or (b) accept the ≤5s TTL as the bust for this path (CONTEXT D-06 says "operator chose zero-lag everywhere over relying on the 5s TTL" — so prefer (a)). **Flag for planning:** option (a) requires a repo-function signature change to return `[]string` of downgraded user-ids. This is the one bust site that is not a simple single-id DEL. [VERIFIED]

### D-09b RUN_SCHEDULER gate (PERF-06)
- `scheduler.Start(db, logger, cfg)` is called **unconditionally** at `main.go:128`. [VERIFIED] Gate it: read `RUN_SCHEDULER` env (default `true`), only call `Start` when true. Add a `RunScheduler bool` field to `config.Config` (parse via `getEnv("RUN_SCHEDULER","true")` → `!= "false"`), validated in `Load()`. The scheduler's own `Start()` is already idempotent (singleton guard, `scheduler.go:36-38`).
- **Automated assertion (D-09, no real second replica):** the cleanest test is to assert at the `main`/wiring layer that `RUN_SCHEDULER=false` → `scheduler.Start` is never invoked. Recommend extracting the gate decision into a tiny pure helper (e.g. `config.ShouldRunScheduler()` returning bool from the env) and unit-testing it for `""→true`, `"true"→true`, `"false"→false`, `"0"/"no"→` (decide). For an integration-level proof, start the app with `RUN_SCHEDULER=false`, advance time past `cleanupInterval`, and assert no scheduler log lines / no cleanup side-effects occurred (e.g. a stale connection row is NOT disconnected). The unit-test on the gate helper is the load-bearing assertion; the integration check is belt-and-suspenders.

### D-01/D-02 compose split
- `docker-compose.prod.yml` confirmed: `postgres` + `redis` + `api` + `tunnel` in one file; `api` already reads `DATABASE_URL`/`REDIS_URL` built from `${POSTGRES_USER}:${POSTGRES_PASSWORD}@postgres:5432/...` and `redis://:${REDIS_PASSWORD}@redis:6379`. [VERIFIED: lines 45-46]
- **Split plan:** new `docker-compose.data.yml` holds `postgres` + `redis` (move the maxmemory flags + mounted `postgresql.conf` here). `docker-compose.prod.yml` keeps `api` + `tunnel`. Host-parameterize: introduce `DB_HOST` (default `postgres`) and `REDIS_HOST` (default `redis`), and build `DATABASE_URL: postgres://...@${DB_HOST:-postgres}:5432/...`, `REDIS_URL: redis://:...@${REDIS_HOST:-redis}:6379`. Co-located dev = defaults; second-host move = set `DB_HOST`/`REDIS_HOST` in `.env`, zero image change (D-01).
- **D-10a Redis maxmemory:** change the redis `command:` to `redis-server --requirepass ${REDIS_PASSWORD:-changeme} --maxmemory 256mb --maxmemory-policy allkeys-lru`. [VERIFIED current command at line 30]
- **D-09c mounted postgresql.conf:** add a tuned `postgresql.conf` (pgtune values from CONTEXT D-09c) and mount it into the data-tier postgres service, e.g. `command: ["postgres", "-c", "config_file=/etc/postgresql/postgresql.conf"]` with a `volumes:` mount, OR pass the individual `-c shared_buffers=...` flags. Mounting a file is cleaner for the 6 values.
- **CAVEAT (carry from Fork 2):** moving postgres to a new host = a fresh empty `pgdata` volume there, which means `docker-entrypoint-initdb.d` WILL run all `001..023` migrations on first boot — but the existing data does NOT migrate itself. The physical move is a data-migration runbook step (dump/restore or replication), which CONTEXT correctly scopes OUT of Phase 6 ("the physical move is the operator's call"). Phase 6 only ships the *restructured compose files* + parameterization.

### D-09c/D-10b/D-10d Fiber + PG config
- **Fiber config** (`main.go:146-152`): currently `AppName`, `ServerHeader`, `ErrorHandler`, `EnableTrustedProxyCheck`, `TrustedProxies`. [VERIFIED] Add `BodyLimit: 64 * 1024`, `ReadTimeout: 15 * time.Second`, `WriteTimeout: 30 * time.Second`, `IdleTimeout: 120 * time.Second`. Leave `Prefork` unset (false) — `main.go` comment block already explains prefork breaks the shared DB pool. **Watch-out:** the lava webhook handler reads a JSON body; 64KB BodyLimit is fine for lava payloads but the planner should confirm no admin endpoint legitimately posts >64KB (server CRUD, plan CRUD are tiny — safe). [VERIFIED: no large-body endpoint found]
- **PG pool + GORM config** (`db.go:23-38`): currently `SetMaxIdleConns(10)`, `SetMaxOpenConns(100)`, `SetConnMaxLifetime(1h)`, and `gorm.Config{Logger: logger.Default.LogMode(logger.Warn)}`. [VERIFIED] Change to `SetMaxIdleConns(25)` (D-10b), add `SetConnMaxIdleTime(5 * time.Minute)` (D-10b), and add `PrepareStmt: true` to the `gorm.Config{}` (D-10d). Keep `SetMaxOpenConns(100)` (but note pgtune `max_connections=200` gives headroom for scheduler + future replicas).
- **`PrepareStmt: true` compatibility (VERIFIED no known pitfall for this app):** GORM's prepared-statement cache keys by SQL string; the app's query set is small and stable (the hot heartbeat write is being MOVED to Redis, so the heaviest repeated UPDATE largely disappears anyway). The one historical GORM `PrepareStmt` gotcha is raw `db.Exec` with varying SQL — the app's `CreateConnectionAtomic` uses a *constant* SQL string with bind params (`connection_repo.go:51`), so it caches cleanly. No dynamic SQL string concatenation was found in the repo layer. The bulk-flush `UPDATE ... WHERE id = ANY($1::uuid[])` is also a constant string with one array param — caches fine. **Recommend enabling.** Minor caveat: prepared statements are per-connection; with `SetConnMaxIdleTime` recycling idle conns, the cache re-warms on new conns — negligible. [CITED: gorm.io/docs/performance.html — PrepareStmt caches prepared statements per connection]

## Common Pitfalls

### Pitfall 1: Assuming `docker compose up` re-runs migrations on the live DB
**What goes wrong:** the new `022`/`023` index migrations silently never apply to `194.87.31.44`; EXPLAIN still shows a seq/full-index scan; "it worked in the test container" but not in prod.
**Why:** `docker-entrypoint-initdb.d` only runs when `pgdata` is empty (first init). [VERIFIED]
**How to avoid:** make the production index creation an explicit runbook step (`psql -f 022_...` against the live DB). CONCURRENTLY makes this safe online.
**Warning sign:** the EXPLAIN assertion passes in CI (fresh container) but the production `pg_stat_user_indexes` shows `idx_scan = 0` for the new index.

### Pitfall 2: Trying to fit the 10s flush into the 1-minute ticker
**What goes wrong:** 10s is not an integer factor of 60s reachable from `expiryTickCount % N`; forcing it produces a 60s (or wrong-cadence) flush, blowing the PERF-02 "every 10s" criterion.
**How to avoid:** add a dedicated `time.NewTicker(10 * time.Second)` goroutine in `Start()`, tracked by the same `wg`/`done`.

### Pitfall 3: Bulk-downgrade bust loses precision
**What goes wrong:** `DowngradeExpiredSubscriptions`/`DowngradeExpiredPlans` flip N users in one UPDATE and return only a count — you can't bust `user:<id>` for users you didn't enumerate.
**How to avoid:** add `RETURNING id` (change return to `[]string`) so the scheduler can bust each, OR knowingly accept the 5s TTL for this one path. CONTEXT prefers zero-lag → do the RETURNING change. Flag explicitly.

### Pitfall 4: Forgetting scheduler/bot ctx when threading PERF-07
**What goes wrong:** repo functions called from `scheduler.go` and `bot/recovery.go` have no Fiber `c.Context()`. Passing `nil` panics or is ignored.
**How to avoid:** pass `context.Background()` (scheduler — ideally `context.WithTimeout` per pass) and `botCtx` (bot). The compiler forces the choice once signatures change.

### Pitfall 5: Busting the wrong cache namespace on admin server writes
**What goes wrong:** an admin server edit busts `cache:plans:public:*` (or vice-versa), leaving the `/servers` cache stale.
**How to avoid:** server writes → `BustServersCache` (`cache:servers:active`) ONLY. Plan writes → `BustPlansCache` ONLY. They are independent.

### Pitfall 6: User-cache value shape vs existence semantics
**What goes wrong:** caching tier but losing the "user deleted" signal (the original HOTFIX-02 reason for the lookup).
**How to avoid:** key presence = exists; `redis.Nil` = miss (fall through to DB, which returns ErrNotFound → 401). On user delete, the DEL bust makes the next request miss → DB → 401. Never cache a negative ("does not exist") — just bust and let the DB be authoritative on miss.

## Code Examples

### Heartbeat write (D-03/D-04) — pipeline in handler
```go
// Source: pattern from cache/redis.go (pipeline idiom) + CONTEXT D-04
func TouchHeartbeat(ctx context.Context, client *redis.Client, connID string) error {
    if client == nil { return nil } // fail-open: no Redis → skip (cleanup grace absorbs it)
    pipe := client.Pipeline()
    pipe.Set(ctx, "hb:"+connID, time.Now().Unix(), 600*time.Second)
    pipe.SAdd(ctx, "hb:dirty", connID)
    _, err := pipe.Exec(ctx)
    return err // best-effort; handler logs but returns 204 regardless (heartbeat is non-critical)
}
```

### Bulk flush (D-03/D-04) — scheduler 10s goroutine
```go
// Source: CONTEXT specifics + connection_repo.go bulk UPDATE style
func FlushHeartbeats(ctx context.Context, client *redis.Client, db *gorm.DB) (int, error) {
    ids, err := client.SMembers(ctx, "hb:dirty").Result()
    if err != nil || len(ids) == 0 { return 0, err }
    now := time.Now()
    if err := db.WithContext(ctx).Exec(
        `UPDATE connections SET last_heartbeat_at = ? WHERE id = ANY(?::uuid[]) AND disconnected_at IS NULL`,
        now, pq.Array(ids), // or pgx array encoding — confirm array binding helper
    ).Error; err != nil {
        return 0, err // leave hb:dirty intact so next tick retries
    }
    if err := client.SRem(ctx, "hb:dirty", ids).Result(); err != nil { /* log; non-fatal */ }
    return len(ids), nil
}
```
**Array-binding note for the planner:** GORM/pgx needs the `[]string` bound as a Postgres array. Confirm the exact helper — pgx supports `[]string` directly via `db.Exec(..., ids)` with `= ANY(?)` in many cases; if GORM's translation fails, use `id IN (?)` with GORM's slice expansion (`db.Where("id IN ?", ids)`) which GORM expands natively. The `IN (?)` form is the safest with GORM's parameter expansion and avoids the `::uuid[]` cast question. [ASSUMED — verify array binding during implementation; A3]

### Fiber config (PERF-09)
```go
// Source: main.go:146 + audit §6.2 / CONTEXT D-09c
app := fiber.New(fiber.Config{
    AppName:                 "VPN API Server",
    ServerHeader:            "",
    ErrorHandler:            handler.ErrorHandler(logger),
    EnableTrustedProxyCheck: true,
    TrustedProxies:          []string{},
    BodyLimit:               64 * 1024,
    ReadTimeout:             15 * time.Second,
    WriteTimeout:            30 * time.Second,
    IdleTimeout:             120 * time.Second,
})
```

### GORM config + pool (D-10b/D-10d)
```go
// Source: db.go:23-38 + CONTEXT D-10b/D-10d
db, err := gorm.Open(postgres.Open(databaseURL), &gorm.Config{
    Logger:      logger.Default.LogMode(logger.Warn),
    PrepareStmt: true, // D-10d
})
...
sqlDB.SetMaxIdleConns(25)                       // D-10b (was 10)
sqlDB.SetMaxOpenConns(100)
sqlDB.SetConnMaxLifetime(1 * time.Hour)
sqlDB.SetConnMaxIdleTime(5 * time.Minute)       // D-10b (new)
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Synchronous per-heartbeat PG UPDATE | Redis SET + 10s bulk flush | This phase (PERF-02) | ~167 write-q/s → ~1 write/10s |
| Per-request `SELECT * FROM users` in AuthRequired + redundant re-query in handler | `user:<id>` cache (5s) + `c.Locals("user")` | This phase (PERF-04/D-07) | removes ~333 + ~167 q/s |
| `/servers` PG read every 60s/client | `cache:servers:active` (60s) | This phase (PERF-01) | removes ~833 q/s |
| No `db.WithContext` anywhere | ctx threaded through all repos | This phase (PERF-07) | stuck queries become time-bounded; DoS surface closed |

**Deprecated/outdated:** nothing in this phase deprecates an external API. The COALESCE in `CleanupStaleConnections` is the one in-code pattern being superseded (optional COALESCE-drop, Fork 2).

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Production migration runner is `docker-entrypoint-initdb.d` + `psql -f` (no tx wrap), and only runs on empty `pgdata` volume. | Fork 2 | LOW for the "no tx wrap" part (verified by migration 017 header + compose mount). The "only on empty volume" is standard postgres-image behavior; if the operator has a different live-migration process (not found in repo), the runbook step differs — but CONCURRENTLY remains correct regardless. |
| A2 | pgx v5 + GORM `WithContext` cancellation removes the query from `pg_stat_activity` (PERF-07 assertion (c)). | Fork 1 | LOW — pgx v5 implements wire-level CancelRequest on ctx cancel. The assertion test itself proves it; if it didn't fire, PERF-07's DoS-mitigation goal is weakened but the code still compiles and is harmless. Test it in Validation. |
| A3 | The bulk-flush `[]string` ids bind cleanly to Postgres `= ANY(?::uuid[])` / `IN (?)` via GORM/pgx. | Code Examples | LOW — worst case use GORM's native `IN ?` slice expansion (definitely works). Verify the exact form during implementation. |
| A4 | No protected/admin endpoint legitimately posts a body > 64KB (so `BodyLimit: 64*1024` is safe). | PERF-09 confirm | LOW — server/plan/user CRUD bodies are tiny; lava webhook payloads are small JSON. Confirmed by inspection; if a future bulk-import endpoint is added it would need a per-route override. |

## Open Questions (RESOLVED)

1. **Bulk-downgrade bust precision (Pitfall 3).**
   - What we know: CONTEXT D-06 wants zero-lag busts everywhere; the two downgrade functions return only counts.
   - What's unclear: whether the planner accepts a repo signature change (`RETURNING id` → `[]string`) for `DowngradeExpiredSubscriptions` + `DowngradeExpiredPlans`.
   - Recommendation: do the RETURNING change (it's small and honors D-06's "zero-lag everywhere"); otherwise document that this single path relies on the 5s TTL.

2. **`PerformRestore` cache-bust placement.**
   - What we know: `PerformRestore` is a repo function with no Redis handle; it's called from the bot goroutine (`bot/recovery.go:369`).
   - What's unclear: bust in the bot handler after `PerformRestore` returns, or pass a Redis client / return the affected ids.
   - Recommendation: `PerformRestore` already returns a `RestoreResult{OldUserID, NewUserID}` — bust both `user:<OldUserID>` and `user:<NewUserID>` in the bot handler using the result. No signature change needed. [VERIFIED RestoreResult has both ids]

3. **COALESCE-drop scope (Fork 2).**
   - Recommendation: fold the `CleanupStaleConnections` predicate rewrite into Phase 6 so the EXPLAIN range-scan assertion (Validation (a)) is demonstrable; it is safe (active rows always have non-null `last_heartbeat_at`).

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Postgres 16 | All DB work; data-tier split | ✓ (image `postgres:16-alpine`) | 16 | — |
| Redis 7 | Caches + heartbeat dirty-set | ✓ (image `redis:7-alpine`) | 7 | fail-open (caches no-op; heartbeat grace absorbs) |
| Docker Compose | PERF-03 split | ✓ | (compose v2) | — |
| `psql` client (for live index backfill) | Fork 2 production runbook | via `docker exec vpn-postgres psql` | 16 | — |
| testcontainers + Docker daemon | EXPLAIN/ctx/index assertions | ✓ (used by `migrations_test.go`) | — | unit tests with miniredis cover cache logic without Docker |
| `miniredis` | cache + flush unit tests | ✓ (`go.mod` v2.37.0) | 2.37.0 | — |

**Missing dependencies with no fallback:** none.
**Missing dependencies with fallback:** Redis at runtime — every cache path is fail-open by design (D-05/D-06), so a Redis outage degrades to direct DB reads, never an error.

## Validation Architecture

> nyquist_validation = true (`.planning/config.json`) — section included. Definition-of-done is automated assertions (D-09), NOT a live load test (deferred).

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go standard `testing` + testcontainers-go (Postgres 16) + miniredis (Redis fake) |
| Config file | none — Go test discovery; `migrations_test.go` is the testcontainers template |
| Quick run command | `cd server/api && go test ./internal/cache/... ./internal/scheduler/... ./internal/repository/... -short` |
| Full suite command | `cd server/api && go test ./...` (testcontainers EXPLAIN/ctx tests run without `-short`) |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| PERF-05 | `idx_connections_heartbeat_active` range scan via EXPLAIN ANALYZE on the (COALESCE-dropped) stale sweep | integration (testcontainers) | `go test ./server/api/migrations/... -run TestPerfIndexes` | ❌ Wave 0 |
| PERF-08 | `idx_connections_connected_at` used by analytics date-bucket query (EXPLAIN) | integration | same suite | ❌ Wave 0 |
| PERF-01 | cache-hit path emits **zero `servers` SELECT** in query log | integration (gorm logger capture / miniredis) | `go test ./server/api/internal/handler/... -run TestServersCacheNoSelect` | ❌ Wave 0 |
| PERF-02 | bulk flush absorbs ~1 write/10s instead of ~167/s: N heartbeats → 1 UPDATE | unit (miniredis) + integration | `go test ./server/api/internal/scheduler/... -run TestHeartbeatFlush` | ❌ Wave 0 |
| PERF-04 | `user:<id>` cache hit → no `SELECT * FROM users`; bust → next request misses | unit (miniredis) | `go test ./server/api/internal/middleware/... -run TestUserCache` | ❌ Wave 0 |
| PERF-06 | `RUN_SCHEDULER=false` fires no jobs | unit (gate helper) + integration | `go test ./server/api/internal/config/... -run TestShouldRunScheduler` + main-wiring check | ❌ Wave 0 |
| PERF-07 | ctx cancellation removes the query from `pg_stat_activity` | integration (testcontainers; cancel ctx mid-`pg_sleep`, poll `pg_stat_activity`) | `go test ./server/api/internal/repository/... -run TestCtxCancelAbortsQuery` | ❌ Wave 0 |
| PERF-09 | Fiber config has the four timeout/limit values; pool + PrepareStmt set | unit (assert on `app.Config()` / `db.Config`) | `go test ./server/api/... -run TestServerConfig` | ❌ Wave 0 |
| PERF-03 | compose split parameterized (`DB_HOST`/`REDIS_HOST` indirection present, defaults resolve) | static/lint (parse both compose files) | shell/CI check or `go test` reading the YAML | ❌ Wave 0 |

### Sampling Rate
- **Per task commit:** `go test ./internal/<touched-package>/... -short` (< 30s; miniredis-based, no Docker).
- **Per wave merge:** `go test ./...` (includes testcontainers EXPLAIN/ctx/index assertions).
- **Phase gate:** full suite green + the five D-09 assertions ((a) EXPLAIN range scan, (b) zero servers SELECT on cache hit, (c) ctx-cancel clears pg_stat_activity, (d) RUN_SCHEDULER=false fires nothing, (e) flush absorbs ~1 write/10s) before `/gsd-verify-work`.

### Wave 0 Gaps
- [ ] `server/api/migrations/perf_indexes_test.go` — EXPLAIN ANALYZE assertions for PERF-05/PERF-08 (reuse `migrations_test.go` testcontainers setup)
- [ ] `server/api/internal/cache/servers_cache_test.go` + `user_cache_test.go` — miniredis cache-aside/fail-open/bust
- [ ] `server/api/internal/cache/heartbeat_cache_test.go` — pipeline write + dirty-set; `FlushHeartbeats` collapses N→1
- [ ] `server/api/internal/scheduler/scheduler_test.go` additions — 10s flush ticker, RUN_SCHEDULER=false no-op, weekly prune cadence
- [ ] `server/api/internal/config/scheduler_gate_test.go` — `ShouldRunScheduler` truth table (PERF-06 (d))
- [ ] `server/api/internal/repository/ctx_cancel_test.go` — testcontainers `pg_stat_activity` ctx-cancel assertion (PERF-07 (c))
- [ ] `server/api/internal/handler/servers_cache_test.go` — zero-`servers`-SELECT on cache hit (PERF-01 (b))
- [ ] No framework install needed — `testing`, testcontainers, miniredis all already present.

## Security Domain

> `security_enforcement` absent in config → treated as enabled. Phase 6 is perf, but two items are directly security-relevant and must not be dropped.

### Applicable ASVS Categories
| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | unchanged (JWT/AuthRequired untouched except adding a cache in front) |
| V3 Session Management | no | unchanged |
| V4 Access Control | partial | D-07 must preserve admin-bypass + plan-filter logic when reading `c.Locals("user")` — do NOT let the cache leak a stale `role`/`tier` past its 5s TTL on a privilege change; admin user-update busts the cache (D-06.1) precisely to close this. |
| V5 Input Validation | yes | unchanged — cache keys are server-derived (`user:<uuid>`, conn ids from path); never build a Redis key from unvalidated client input beyond the already-validated id/uuid. |
| V6 Cryptography | no | none — no crypto in this phase |
| V12/V13 (DoS / API) | yes | Fiber `ReadTimeout`/`WriteTimeout`/`IdleTimeout`/`BodyLimit` (PERF-09) defend slowloris/oversized-body; ctx cancellation (PERF-07) bounds stuck queries; Redis `maxmemory-policy allkeys-lru` (D-10a) bounds OOM under adversarial unique-key spam. |

### Known Threat Patterns for Go/Fiber/Redis/PG
| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Slow-client / slowloris exhausting goroutines | DoS | Fiber `ReadTimeout`/`IdleTimeout` (PERF-09) |
| Client opens many conns, sends slow request, drops TCP → stuck PG queries drain pool | DoS | `db.WithContext(c.Context())` (PERF-07) — pgx cancels the query on disconnect |
| Redis OOM via millions of unique rate-limit/cache keys | DoS | `--maxmemory 256mb --maxmemory-policy allkeys-lru` (D-10a) |
| Stale tier/role served past a privilege change due to caching | Elevation of Privilege | TTL ≤ 5s (D-06) + synchronous bust on admin user-update (D-06.1) — keep the bust mandatory |
| Oversized request body memory pressure | DoS | `BodyLimit: 64*1024` (PERF-09); confirm no legit large-body route (A4) |
| Cache-poisoning via client-controlled key | Tampering | keys are server-derived UUIDs only; never interpolate raw client strings into Redis keys |

## Sources

### Primary (HIGH confidence)
- Live codebase (read in full this session): `repository/{connection_repo,user_repo,server_repo,plan_repo,recovery_repo,expiry_repo,db}.go`, `cache/{plans_cache,redis}.go`, `middleware/auth.go`, `scheduler/scheduler.go`, `handler/{servers,connection,admin,webhook_lava}.go`, `cmd/main.go`, `config/config.go`, `migrations/{001,008,017,019,021}*.sql`, `migrations/migrations_test.go`, `go.mod`, `docker-compose.prod.yml`, `.github/workflows/deploy.yml`, `.planning/config.json`.
- `docs/audit/PERFORMANCE-AUDIT.md` (source of truth — §2.1-2.5, §4.1/4.3/4.4, §5.2/5.5, §6.1/6.2, §7.2/7.4, §8.2, §12.3/12.4).
- `docs/audit/MASTER-PLAN.md` "Tranche 3", `.planning/REQUIREMENTS.md` PERF-01..09 + SCALE-01, `.planning/phases/06-performance-scalability/06-CONTEXT.md` (D-01..D-10).

### Secondary (MEDIUM confidence)
- `docs/ADR-007-lava-sso-rework.md` (admin endpoint shapes; webhook side-effect contract) — read §1-12; §19.7 admin endpoints already wired in `main.go` and confirmed there directly.

### Tertiary (LOW confidence)
- pgx v5 ctx-cancellation behavior (PERF-07 (c)) — based on pgx wire protocol knowledge [ASSUMED A2]; the Validation test is the in-session proof.
- gorm `PrepareStmt` per-connection caching semantics [CITED: gorm.io/docs/performance.html, from training knowledge].

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — versions read straight from `go.mod`; no new deps.
- Architecture (repo style, cache pattern, scheduler shape): HIGH — every relevant file read in full.
- Fork resolutions: HIGH — all four anchored to verified live code (`plans_cache.go` TTL, migration 017 runner, `ListServersForPlan` JOIN, repo free-function style).
- Pitfalls: HIGH — derived from verified runner behavior and scheduler/cache structure.
- PERF-07 ctx-cancel assertion: MEDIUM — driver is pgx (verified); the abort behavior is proven by the Validation test, not yet executed.

**Research date:** 2026-05-29
**Valid until:** 2026-06-28 (30 days — stable stack; the only volatility is the live DB state, which the planner should re-confirm at execution time per the empty-volume caveat).
