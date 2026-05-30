---
phase: 6
slug: performance-scalability
status: verified
threats_open: 0
asvs_level: 1
created: 2026-05-30
---

# Phase 6 — Security

> Per-phase security contract: threat register, accepted risks, and audit trail.
> Covers all 8 plans (06-00 … 06-07): test harness, server timeouts/body limits,
> private-bind data tier, perf indexes, Redis cache-aside (/servers + user
> entitlement), Redis-backed heartbeats + scheduler flush, end-to-end request-
> context propagation, and the deploy runbook.

---

## Trust Boundaries

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| internet → Fiber API | untrusted client requests; body + connection lifetime are attacker-controlled | request payloads, JWT bearer |
| app tier (api) → data tier (postgres/redis) | crosses a network boundary after the split (same host today, private interface when split off-host — D-02) | DB queries, cache reads/writes |
| JWT bearer → AuthRequired | the user cache fronts an authorization-relevant existence/tier check | user id, role/tier claims |
| client → /servers | authenticated request; the plan filter must not leak servers outside the user's plan | server list (entitlement-sensitive) |
| admin → server-write | admin-authorized mutation; must invalidate the shared cache atomically vs concurrent reads | server records |
| mutation paths → user cache | admin/webhook/scheduler/bot writes must bust the entitlement cache atomically | user entitlement state |
| client heartbeat → Redis | authenticated; ownership enforced before the Redis write | connection liveness |
| scheduler → connections / users | internal background writes (flush, prune, downgrade); no untrusted input | bulk row updates/deletes |
| operator → live production DB | manual backfill runs DDL against the live DB; CONCURRENTLY keeps it online-safe | index DDL |
| client TCP → Fiber → GORM → pgx → Postgres | a client disconnect must propagate cancellation all the way to the DB query | query cancellation signal |

---

## Threat Register

| Threat ID | Category | Component | Disposition | Mitigation | Status |
|-----------|----------|-----------|-------------|------------|--------|
| T-06-W0-01 | Tampering | test files vs prod cache keys | accept | Tests read-only against ephemeral fakes; key constants match prod literals (`cache:servers:active`, `user:<id>`, `hb:dirty`) so they validate the real paths. | closed |
| T-06-W0-02 | DoS | testcontainer CI resource use | accept | `-short` skip guards keep the quick suite Docker-free; testcontainers run only on wave-merge/phase-gate. | closed |
| T-06-DATALINK | Information Disclosure / Spoofing | PG/Redis cross-host link (D-02) | mitigate | No public host-port publish for postgres/redis (`docker-compose.data.yml` — `networks:` only, no `ports:`); Redis `--requirepass ${REDIS_PASSWORD}` (data.yml:66), app `REDIS_URL` embeds the same `${REDIS_PASSWORD}` (prod.yml:40); `POSTGRES_PASSWORD` hard-required (data.yml:33 `:?`); TLS-ready `sslmode=require` / `rediss://` upgrade documented in-file. Acceptance (no public bind + requirepass present) met & verifiable in compose. | closed |
| T-06-SLOWLORIS | DoS | Fiber HTTP server | mitigate | `ReadTimeout: 15s` + `IdleTimeout: 120s` close slow/idle clients (main.go). Asserted by TestServerConfig. | closed |
| T-06-BODYDOS | DoS | Fiber request body parsing | mitigate | `BodyLimit: 64*1024` caps oversized-body memory (main.go); no legit >64KB route. | closed |
| T-06-REDISOOM | DoS | Redis memory | mitigate | `--maxmemory 256mb --maxmemory-policy allkeys-lru` bounds memory under unique-key spam (data.yml:66 / audit §5.5). | closed |
| T-06-IDX-01 | DoS (availability) | live-DB index build | mitigate | `CREATE INDEX CONCURRENTLY IF NOT EXISTS` on every index (migrations 022, 023) avoids the ACCESS EXCLUSIVE lock; heartbeat write path stays available during backfill. | closed |
| T-06-IDX-02 | Tampering (data correctness) | COALESCE-drop predicate | accept | Active rows always have non-null `last_heartbeat_at` (migration 008 backfill + every `CreateConnection*` sets it); the rewrite is provably equivalent. | closed |
| T-06-SRVCACHE-01 | Information Disclosure | non-admin /servers cache path | mitigate | Cached blob is the FULL active list, but the plan filter stays live in Go for non-admins (`ListServersForPlan`, post-cache, per-request, servers.go:158); admin path bypasses cache. A non-admin never receives out-of-plan servers. | closed |
| T-06-SRVCACHE-02 | Tampering (stale data) | shared cache vs admin write | mitigate | Synchronous `BustServersCache` before each admin server-write returns (admin.go) + 60s TTL safety net. | closed |
| T-06-SRVCACHE-03 | DoS / availability | Redis outage on /servers | mitigate | Fail-open: `GetServersCache` returns `""` on outage → handler falls through to DB. /servers never breaks. | closed |
| T-06-SRVCACHE-04 | Tampering | cache key from client input | accept | Key is the server-derived constant `cache:servers:active` — no client input interpolated. | closed |
| T-06-USERCACHE | **Elevation of Privilege** | `user:<id>` entitlement cache | mitigate | Synchronous `BustUserCache` on EVERY mutation path — admin update (admin.go:229), webhook grant (webhook_lava.go:246,329), bot recovery old+new (recovery.go:390,394), and BOTH bulk downgrades via the shared `bustExpiredUsers` helper (scheduler.go:219 `DowngradeExpiredSubscriptions`, :284 `DowngradeExpiredPlans` incl. the WR-03 self-guard candidate set) — plus a ≤5s TTL backstop. Authz role/tier still come from the JWT claim + the `AdminRequired` DB re-read (HOTFIX-02); the cache fronts the EXISTENCE gate only, so a stale cache cannot grant admin/Pro. | closed |
| T-06-USERCACHE-FAILOPEN | DoS / availability | Redis outage on AuthRequired | mitigate | `GetUserCache` returns `found=false` on outage → AuthRequired falls through to the DB existence check; the deleted-user 401 still fires from the DB. | closed |
| T-06-WEBHOOK-IDEMP | Tampering (replay) | webhook bust on Pro-grant | mitigate | Bust runs on the SUCCESS side-effect only; a duplicate event short-circuits on the `lava_webhook_events` UNIQUE and neither re-applies nor re-busts. The 200/500 retry contract is unchanged (bust error logs but still returns 200). | closed |
| T-06-USERKEY | Tampering | cache key from input | accept | Key is `user:<uuid>` where uuid is the validated JWT `sub` / path id — no raw client string interpolated. | closed |
| T-06-HB-FAILOPEN | DoS / availability | Redis outage on heartbeat | mitigate | `TouchHeartbeat` fails open (nil/err → 204); a missed flush window is bounded by the 3-min `StaleConnectionAfter` grace; PG stays source-of-truth. | closed |
| T-06-HB-OWNERSHIP | Spoofing / EoP | heartbeat for another user's conn | mitigate | The `FindConnectionByID` ownership pre-read is retained (per-request ownership check) before the Redis write — a client cannot refresh a connection it does not own. | closed |
| T-06-SCHED-DOUBLE | Tampering (double-run) | multi-replica scheduler | mitigate | `RUN_SCHEDULER` gate (config.go) ensures only the primary replica runs periodic jobs; the flush is multi-replica safe (shared dirty set, idempotent UPDATE) as defense-in-depth. | closed |
| T-06-PRUNE | Tampering (data loss) | 90-day connection prune | accept | Deletes only `disconnected_at`-non-null rows older than 90 days (historical); weekly cadence; bounded by `idx_connections_connected_at`. Active rows (`disconnected_at IS NULL`) never touched. | closed |
| T-06-CTXLEAK | DoS | unbounded queries outliving their request | mitigate | `db.WithContext(c.Context())` threaded through every request-path repo call incl. `server_repo.ListActiveServers` (server_repo.go:15) and the cached /servers path (servers.go:158,198); a client disconnect cancels the ctx → pgx v5 CancelRequest → query aborts, releasing its pool conn. Asserted by TestCtxCancelAbortsQuery. | closed |
| T-06-CTXNIL | Reliability | scheduler/bot passing nil ctx | mitigate | Scheduler passes `context.Background()`/`WithTimeout`; bot passes `botCtx`; no nil ctx reaches a DB call. | closed |
| T-06-BACKFILL-GAP | DoS (silent perf regression) | live DB missing the new indexes | mitigate | Runbook §D-01 + HUMAN-UAT make the manual backfill an explicit operator gate; the warning sign (CI green but `idx_scan=0`) is documented; CONCURRENTLY + IF NOT EXISTS make it safe + idempotent. | closed |
| T-06-OFFHOST-EXPOSURE | Information Disclosure | data tier on a public interface during the move | mitigate | Runbook §D-02 mandates binding PG/Redis to a private interface + firewall and documents the TLS-ready upgrade (D-02) before any cross-host link is exposed. | closed |

*Status: open · closed*
*Disposition: mitigate (implementation required) · accept (documented risk) · transfer (third-party)*

---

## Accepted Risks Log

| Risk ID | Threat Ref | Rationale | Accepted By | Date |
|---------|------------|-----------|-------------|------|
| AR-06-01 | T-06-W0-01 | Test code only; reads ephemeral fakes (testcontainer PG / miniredis), never prod Redis/DB. Key constants must match prod literals so the tests validate the real paths — checked in acceptance criteria. | Phase owner | 2026-05-30 |
| AR-06-02 | T-06-W0-02 | Testcontainer CI resource use is bounded by `-short` skip guards (quick suite is Docker-free; full suite only on wave-merge/phase-gate). | Phase owner | 2026-05-30 |
| AR-06-03 | T-06-IDX-02 | Dropping COALESCE from the cleanup predicate is provably equivalent: every active (`disconnected_at IS NULL`) row has a non-null `last_heartbeat_at` (migration 008 backfill + `CreateConnection*` insert). No change to which rows are swept. | Phase owner | 2026-05-30 |
| AR-06-04 | T-06-SRVCACHE-04 | The /servers cache key is the server-derived constant `cache:servers:active`; no client input is interpolated, so there is no key-injection surface. | Phase owner | 2026-05-30 |
| AR-06-05 | T-06-USERKEY | The user cache key is `user:<uuid>` where uuid is the validated JWT `sub` / UUID-validated path id; no raw client string is interpolated. | Phase owner | 2026-05-30 |
| AR-06-06 | T-06-PRUNE | The 90-day prune deletes only `disconnected_at`-non-null rows older than 90 days (historical, weekly cadence, index-bounded). Active rows (`disconnected_at IS NULL`) are never touched. | Phase owner | 2026-05-30 |

*Accepted risks do not resurface in future audit runs.*

---

## Residual Notes (non-blocking hardening)

| Note ID | Ref | Observation | Recommendation |
|---------|-----|-------------|----------------|
| HN-06-01 | T-06-DATALINK | `REDIS_PASSWORD:-changeme` silently defaults in compose, whereas `POSTGRES_PASSWORD:?` hard-fails if unset. On the current single-VM deployment Redis is not publicly bound, so this is defense-in-depth only — but a `changeme` Redis password is a weak default if an operator forgets to set it. | Switch the Redis password to `${REDIS_PASSWORD:?Set REDIS_PASSWORD in .env}` (match the POSTGRES pattern) before any off-host move. Tracked alongside T-06-OFFHOST-EXPOSURE. |

---

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-05-30 | 24 | 21 | 3 | gsd-security-auditor (sonnet) |
| 2026-05-30 | 24 | 24 | 0 | orchestrator reconciliation (direct code verification) |

**Reconciliation of the 3 auditor-flagged open threats** (all resolved to *closed* on direct
inspection — the auditor's findings were grep/tracing artifacts, two of which it explicitly hedged):

- **T-06-USERCACHE** (EoP, HIGH-watch) — Auditor suspected the WR-03 self-guard bulk-downgrade
  path lacked a `BustUserCache` loop and asked for confirmation. Verified: WR-03 (commit d241d1f)
  only added an eligibility re-assertion to the *existing* `DowngradeExpiredPlans` UPDATE; it is
  not a new path. The function returns the Pluck'd candidate IDs, which `runExpiryDowngrade` busts
  via the shared `bustExpiredUsers` helper (scheduler.go:284). The other bulk path
  (`DowngradeExpiredSubscriptions`) busts via the same helper at scheduler.go:219. Both bulk
  downgrades, plus admin/webhook/bot paths, bust synchronously. Defense-in-depth: the cache stores
  existence only; tier/role authz derive from the JWT claim + `AdminRequired` DB re-read (HOTFIX-02).
  → **closed**.
- **T-06-CTXLEAK** (DoS) — Auditor claimed `server_repo.go` has 0 `WithContext` occurrences (citing
  file "encoding issues"). Verified: `server_repo.go:15` (`ListActiveServers`) and `:22`
  (`FindServerByID`) both use `db.WithContext(ctx)`; the /servers handler threads `c.Context()`
  (servers.go:158,198). The only repo file with 0 `WithContext` is `db.go`, which is pool setup
  (no request queries). → **closed**.
- **T-06-DATALINK** (Info Disclosure) — Auditor applied a stricter bar (code-level `sslmode`/password
  enforcement) than the mitigation's stated acceptance: "no public bind in compose; requirepass
  present." Both are met and verifiable in `docker-compose.data.yml` (no `ports:` on postgres/redis;
  `--requirepass ${REDIS_PASSWORD}`; `POSTGRES_PASSWORD` hard-required). The TLS-ready upgrade was the
  intended disposition for the off-host move (out of Phase 6 scope per D-01/D-02; separately tracked
  as T-06-OFFHOST-EXPOSURE). Logged HN-06-01 for the weak `changeme` default. → **closed**.

---

## Sign-Off

- [x] All threats have a disposition (mitigate / accept / transfer)
- [x] Accepted risks documented in Accepted Risks Log
- [x] `threats_open: 0` confirmed
- [x] `status: verified` set in frontmatter

**Approval:** verified 2026-05-30
