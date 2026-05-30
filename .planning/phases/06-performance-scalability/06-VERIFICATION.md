---
phase: 06-performance-scalability
verified: 2026-05-30T00:00:00Z
status: passed
score: 9/9
overrides_applied: 0
deferred:
  - truth: "Physical second-host PG/Redis provisioning + private-link/firewall"
    addressed_in: "06-HUMAN-UAT.md (DEFERRED — operator's call)"
    evidence: "Phase 6 delivers the parameterised compose split + DB_HOST/REDIS_HOST defaults; the physical move is an explicit operator runbook step documented in docs/runbooks/06-perf-deploy-runbook.md and tracked as DEFERRED in 06-HUMAN-UAT.md"
  - truth: "Real ~10k synthetic connection load test confirms PG write q/s drop"
    addressed_in: "06-HUMAN-UAT.md (DEFERRED to end-of-milestone release phase)"
    evidence: "Phase 6 ships D-09 assertion-level proof only (five automated test assertions). The live load test is a release-phase gate per D-09 and 06-VALIDATION.md"
  - truth: "Live-DB index backfill confirms idx_scan > 0 on prod"
    addressed_in: "06-HUMAN-UAT.md (PENDING-OPERATOR — next prod deploy)"
    evidence: "docker-entrypoint-initdb.d only fires on an empty pgdata volume; the backfill commands and caveat are documented in docs/runbooks/06-perf-deploy-runbook.md §2 and tracked in 06-HUMAN-UAT.md §1"
---

# Phase 06: Performance & Scalability — Verification Report

**Phase Goal:** Performance & scalability — /servers Redis cache, Redis heartbeat with bulk flush, off-host PG/Redis compose split, user-tier cache for AuthRequired, missing perf indexes, scheduler RUN_SCHEDULER gate, GORM context propagation.
**Verified:** 2026-05-30
**Status:** passed
**Re-verification:** No — initial verification

---

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | `/servers` is cached in Redis (`cache:servers:active`, TTL 60s, fail-open, busted synchronously on admin server-writes) | VERIFIED | `servers_cache.go` — `serversActiveKey = "cache:servers:active"`, `serversActiveTTL = 60 * time.Second`; `ListServersCached` in `servers.go` calls `GetServersCache`/`SetServersCache`; `BustServersCache` found 3 times in `admin.go` (AdminCreateServer/Update/Delete); `handler.ListServersCached(logger, db, redisClient)` wired in `main.go:311` |
| 2 | Heartbeat writes go to Redis only (hb:<id> + hb:dirty); a 10s bulk-flush ticker collapses N→1 bulk UPDATE per interval | VERIFIED | `heartbeat_cache.go` — `heartbeatDirtySet = "hb:dirty"`, `heartbeatTTL = 600s`; `TouchHeartbeat` pipelines SET+SADD; `FlushHeartbeats` SMEMBERS→bulk UPDATE→SREM; `connection.go HeartbeatConnection` calls `TouchHeartbeat` (zero `UpdateHeartbeat(db` calls); `scheduler.go` has two tickers — `time.NewTicker(cleanupInterval)` and `time.NewTicker(heartbeatFlushInterval)` (10s); `FlushHeartbeats` called in the 10s goroutine |
| 3 | Compose is split: postgres+redis live in `docker-compose.data.yml`; `docker-compose.prod.yml` has neither; `DATABASE_URL`/`REDIS_URL` parameterised via `${DB_HOST:-postgres}`/`${REDIS_HOST:-redis}` | VERIFIED | `docker-compose.data.yml` has exactly 1 `postgres:` and 1 `redis:` service; `docker-compose.prod.yml` has 0 `postgres:` and 0 `redis:` service blocks; `DATABASE_URL: ...@${DB_HOST:-postgres}:5432/...` and `REDIS_URL: ...@${REDIS_HOST:-redis}:6379` confirmed in `prod.yml:39` |
| 4 | User existence+tier is cached (`user:<id>`, TTL 5s, fail-open); `AuthRequired` hits cache on warm paths; cache busted synchronously on every mutation (admin update, webhook grant ×2, Telegram restore ×2, bulk downgrades via RETURNING id) | VERIFIED | `user_cache.go` — `userKeyPrefix = "user:"`, `userCacheTTL = 5s`; `auth.go` calls `GetUserCache`/`SetUserCache`; `BustUserCache` confirmed at 6 production call sites: `admin.go:229`, `webhook_lava.go:246` (payment success), `webhook_lava.go:329` (recurring success), `scheduler.go:301` (bustExpiredUsers iterating RETURNING ids), `bot/recovery.go:390` (OldUserID), `bot/recovery.go:394` (NewUserID); `DowngradeExpiredSubscriptions` and `DowngradeExpiredPlans` return `([]string, error)` with RETURNING id |
| 5 | `idx_connections_heartbeat_active` partial index (migration 022) and `idx_connections_connected_at` index (migration 023) exist; `CleanupStaleConnections` drops COALESCE so the planner range-scans the partial index | VERIFIED | `022_add_perf_indexes.sql` — `CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_connections_heartbeat_active ON connections(last_heartbeat_at) WHERE disconnected_at IS NULL`; `023_connections_connected_at_index.sql` — `CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_connections_connected_at ON connections(connected_at)`; 0 BEGIN/COMMIT lines in either file; `connection_repo.go CleanupStaleConnections` predicate is `disconnected_at IS NULL AND last_heartbeat_at < ?` (COALESCE absent from that function) |
| 6 | `RUN_SCHEDULER` env gate exists; scheduler only starts when not explicitly disabled; default is true (unset = run) | VERIFIED | `config.go:185` — `func ShouldRunScheduler(envValue string) bool` — falsey set `{false,0,no}` returns false, all else (including empty) returns true; `config.go:120` — `RunScheduler: ShouldRunScheduler(getEnv("RUN_SCHEDULER", "true"))`; `main.go:165` — `if cfg.RunScheduler { scheduler.Start(...) }` |
| 7 | Every exported repository function takes `ctx context.Context` as its first parameter and calls `db.WithContext(ctx)` internally; every call site passes a real ctx | VERIFIED | `grep -rn "func [A-Z].*db \*gorm.DB" server/api/internal/repository/` returns 0 (no exported func still takes db first); `db.WithContext(ctx)` found 108 times across all 14 repo files; handlers pass `c.Context()` at 128 call sites; scheduler uses `context.WithTimeout(context.Background(), 30*time.Second)` in both `runCleanup` and `runExpiryDowngrade`; `grep -rn "repository\.[A-Za-z]*(nil," server/api/internal/` returns 0 (no nil ctx in production) |
| 8 | Fiber config has `BodyLimit: 64KB`, `ReadTimeout: 15s`, `WriteTimeout: 30s`, `IdleTimeout: 120s`; PG pool has `MaxIdleConns: 25`, `ConnMaxIdleTime: 5m`, `PrepareStmt: true`; `postgresql.conf` pgtune-tuned | VERIFIED | `cmd/main.go:50-53` confirms all four Fiber fields; `db.go:31` — `PrepareStmt: true`; `db.go:42,45` — `SetMaxIdleConns(25)` and `SetConnMaxIdleTime(5 * time.Minute)`; `postgresql.conf` has `shared_buffers = 1GB` and `random_page_cost = 1.1`; `TestServerConfig` and `TestGormConfig_PrepareStmtEnabled` test files exist |
| 9 | Production runbook documents the manual live-DB index backfill, co-located two-file compose bring-up, and off-host data-tier move; `06-HUMAN-UAT.md` tracks deferred/operator-gated items | VERIFIED | `docs/runbooks/06-perf-deploy-runbook.md` exists with `CREATE INDEX CONCURRENTLY` backfill commands, `DB_HOST`/`REDIS_HOST` off-host instructions; `06-HUMAN-UAT.md` has 3 entries: live backfill (PENDING-OPERATOR), ~10k load test (DEFERRED), second-host provisioning (DEFERRED); human-verify checkpoint approved (Task 3 of plan 07) |

**Score:** 9/9 truths verified (3 operator/hardware-dependent items appropriately deferred in HUMAN-UAT per D-09 contract)

---

### Deferred Items

Items not yet met but explicitly tracked in HUMAN-UAT as operator/hardware-dependent — not code gaps.

| # | Item | Addressed In | Evidence |
|---|------|-------------|----------|
| 1 | Physical second-host PG/Redis provisioning + private-link/firewall | 06-HUMAN-UAT.md §3 (DEFERRED) | Phase 6 delivers the parameterised compose split; physical move is an explicit operator step in docs/runbooks/06-perf-deploy-runbook.md §4 |
| 2 | Real ~10k synthetic connection load test (PG write q/s drop, p99 latency) | 06-HUMAN-UAT.md §2 (DEFERRED to release phase) | Phase 6 ships D-09 assertion-level proof per 06-VALIDATION.md; live load test is a release-phase gate |
| 3 | Live-DB index backfill (idx_scan > 0 on prod) | 06-HUMAN-UAT.md §1 (PENDING-OPERATOR) | docker-entrypoint-initdb.d only fires on empty pgdata; backfill commands are in runbook §2; CI EXPLAIN assertions (TestPerfIndexes) prove the DDL is correct against fresh testcontainer DBs |

---

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `server/api/internal/cache/servers_cache.go` | cache:servers:active get/set/bust | VERIFIED | Key `cache:servers:active`, TTL 60s, fail-open mirror of plans_cache.go |
| `server/api/internal/cache/user_cache.go` | user:<id> existence+tier cache, TTL 5s | VERIFIED | Key prefix `user:`, TTL 5s, fail-open; never caches a negative |
| `server/api/internal/cache/heartbeat_cache.go` | TouchHeartbeat pipeline + FlushHeartbeats bulk flush | VERIFIED | `hb:dirty` set; pipeline SET+SADD; SMEMBERS→UPDATE→SREM with retry-on-error |
| `server/api/internal/config/config.go` | ShouldRunScheduler gate helper + RunScheduler field | VERIFIED | Pure helper, falsey set {false,0,no}, default true |
| `server/api/migrations/022_add_perf_indexes.sql` | PERF-05 partial heartbeat index | VERIFIED | `CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_connections_heartbeat_active … WHERE disconnected_at IS NULL`; no BEGIN/COMMIT |
| `server/api/migrations/023_connections_connected_at_index.sql` | PERF-08 connected_at index | VERIFIED | `CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_connections_connected_at`; no BEGIN/COMMIT |
| `server/api/internal/repository/connection_repo.go` | COALESCE-dropped stale-sweep predicate | VERIFIED | `disconnected_at IS NULL AND last_heartbeat_at < ?` in CleanupStaleConnections; CleanupStaleReservations intentionally retains COALESCE |
| `docker-compose.data.yml` | data tier: postgres + redis | VERIFIED | Both services present; `--maxmemory 256mb --maxmemory-policy allkeys-lru`; `config_file=/etc/postgresql/postgresql.conf` mount |
| `server/api/postgresql.conf` | pgtune-tuned Postgres config | VERIFIED | `shared_buffers = 1GB`, `random_page_cost = 1.1`, `max_connections = 200` |
| `server/api/cmd/main.go` | Fiber timeouts + BodyLimit + scheduler gate | VERIFIED | `BodyLimit: 64*1024`, `ReadTimeout: 15s`, `WriteTimeout: 30s`, `IdleTimeout: 120s`; `ShouldRunScheduler` gate at :165 |
| `server/api/internal/repository/db.go` | pool tuning + PrepareStmt | VERIFIED | `PrepareStmt: true`, `SetMaxIdleConns(25)`, `SetConnMaxIdleTime(5 * time.Minute)` |
| `server/api/internal/repository/user_repo.go` (and all 13 repo files) | ctx-first signatures + db.WithContext(ctx) | VERIFIED | 0 exported funcs still take db first; 108 `db.WithContext(ctx)` calls across repo files; 7 transactional functions use `db.WithContext(ctx).Transaction` |
| `docs/runbooks/06-perf-deploy-runbook.md` | production deploy runbook | VERIFIED | Live-DB backfill commands, co-located bring-up, off-host move, rollback section |
| `server/api/migrations/perf_indexes_test.go` | D-09 (a) EXPLAIN assertion | VERIFIED | `EXPLAIN (FORMAT JSON)` assertions for both `idx_connections_heartbeat_active` and `idx_connections_connected_at`; Short() skip guard present |
| `server/api/internal/handler/servers_cache_test.go` | D-09 (b) zero-SELECT assertion | VERIFIED | `TestServersCacheNoSelect` present; gorm logger capture approach |
| `server/api/internal/repository/ctx_cancel_test.go` | D-09 (c) ctx-cancel assertion | VERIFIED | `pg_stat_activity` and `pg_sleep` patterns; Short() skip guard present |
| `server/api/internal/config/scheduler_gate_test.go` | D-09 (d) ShouldRunScheduler truth table | VERIFIED | `TestShouldRunScheduler` with cases `""=true`, `"true"=true`, `"false"=false`, `"0"=false`, `"no"=false` |
| `server/api/internal/cache/heartbeat_cache_test.go` | D-09 (e) N→1 flush assertion | VERIFIED | `FlushHeartbeats`, `hb:dirty` references; N-heartbeat→1-flush collapse test |

---

### Key Link Verification

| From | To | Via | Status | Details |
|------|-----|-----|--------|---------|
| `handler/servers.go ListServersCached` | `cache:servers:active` | `GetServersCache → miss → ListActiveServers → SetServersCache` | WIRED | `servers.go:198` calls `GetServersCache`; `servers.go:216` calls `SetServersCache` |
| `handler/admin.go AdminCreate/Update/DeleteServer` | `cache:servers:active` | synchronous `BustServersCache` before return | WIRED | 3 occurrences in `admin.go` |
| `handler/connection.go HeartbeatConnection` | `hb:<id>` + `hb:dirty` | `cache.TouchHeartbeat` pipeline (no PG write) | WIRED | `connection.go:325`; zero `UpdateHeartbeat(db` calls in handler |
| `scheduler.go 10s flushTicker goroutine` | `connections` bulk UPDATE | `cache.FlushHeartbeats` | WIRED | `scheduler.go:118` in 10s goroutine; `flushTicker: time.NewTicker(heartbeatFlushInterval)` |
| `cmd/main.go` | `scheduler.Start` | `config.ShouldRunScheduler` gate | WIRED | `main.go:165` — `if cfg.RunScheduler { scheduler.Start(db, logger, cfg, redisClient) }` |
| `middleware/auth.go AuthRequired` | `user:<id>` cache | `GetUserCache → on miss FindUserByID → SetUserCache + c.Locals("user")` | WIRED | `auth.go:135` (GetUserCache), `auth.go:159` (SetUserCache) |
| All mutation paths | `user:<id>` bust | synchronous `BustUserCache` | WIRED | 6 production bust sites across admin.go, webhook_lava.go, scheduler.go, bot/recovery.go |
| `handler call sites` | `repo functions` | `c.Context()` threaded as first arg | WIRED | 128 `repository.Xxx(c.Context()` calls in handler package |
| `docker-compose.prod.yml api service` | `DB_HOST`/`REDIS_HOST` env | `DATABASE_URL`/`REDIS_URL` string interpolation | WIRED | `prod.yml:39` — `@${DB_HOST:-postgres}:5432/...` confirmed |
| `docker-compose.data.yml postgres service` | `postgresql.conf` | volume mount + config_file flag | WIRED | `data.yml:43-44` — read-only mount + `command: ["postgres", "-c", "config_file=..."]` |

---

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
|----------|---------------|--------|-------------------|--------|
| `servers_cache.go GetServersCache` | `val` | Redis GET on `cache:servers:active` | Yes — populated by `ListActiveServers` DB query on cache miss | FLOWING |
| `user_cache.go GetUserCache` | `tier string, found bool` | Redis GET on `user:<id>` | Yes — populated by `FindUserByID` DB query on cache miss | FLOWING |
| `heartbeat_cache.go FlushHeartbeats` | `ids []string` | `SMEMBERS hb:dirty` → bulk UPDATE | Yes — real GORM UPDATE to `connections` table | FLOWING |
| `connection_repo.go CleanupStaleConnections` | query predicate | `last_heartbeat_at < cutoff` WHERE clause | Yes — range-scans `idx_connections_heartbeat_active` | FLOWING |

---

### Behavioral Spot-Checks

Step 7b: SKIPPED for live-server behaviors (API must be running). The D-09 assertions (automated Go tests) serve as the behavioral proof for this phase's code-level behaviors. The orchestrator confirmed `cd server/api && go test ./... -short` is GREEN.

Key fix commits confirm the test suite was actively run and green:
- `98ea89e` — "Full server/api short suite GREEN" (post-wave fix for scheduler regression + NULL-safe gate test)
- `9bc8ce1` — "Full server/api build+vet+short suite now GREEN" (post-merge ctx fix for devices.go)

---

### Requirements Coverage

| Requirement | Source Plan(s) | Description | Status | Evidence |
|-------------|---------------|-------------|--------|----------|
| PERF-01 | Plan 03 | `/servers` cached in Redis, cache TTL ≤ 5 min, bust on admin server-write | SATISFIED | `cache:servers:active` (60s TTL); `ListServersCached`; 3× `BustServersCache` in admin.go; D-09 (b) test exists |
| PERF-02 | Plans 00, 05 | Heartbeat writes → Redis first; 10s bulk flush to Postgres | SATISFIED | `TouchHeartbeat` pipeline; `FlushHeartbeats`; 10s `flushTicker` goroutine; D-09 (e) test exists |
| PERF-03 | Plans 00, 01, 07 | PG + Redis on separate compose file; host-parameterised URLs | SATISFIED | `docker-compose.data.yml` (both services); `${DB_HOST:-postgres}` / `${REDIS_HOST:-redis}` in prod.yml; physical move documented as operator step |
| PERF-04 | Plans 00, 04 | User existence+tier cached for AuthRequired, TTL ≤ 5s, busted on admin user-update | SATISFIED | `user_cache.go` (5s TTL); `AuthRequired` cache-fronted; 6 bust sites including admin, webhook, scheduler, bot |
| PERF-05 | Plans 00, 02, 07 | `idx_connections_heartbeat_active` partial index; stale sweep O(connected) | SATISFIED | Migration 022 DDL verified; COALESCE dropped from `CleanupStaleConnections`; D-09 (a) test exists |
| PERF-06 | Plans 00, 05 | `RUN_SCHEDULER` env gate; scheduler only starts on primary replica | SATISFIED | `ShouldRunScheduler` helper; `cfg.RunScheduler` gate in `main.go`; D-09 (d) truth-table test |
| PERF-07 | Plans 00, 06 | Every GORM call uses `db.WithContext(ctx)`; no unbounded query outlives request | SATISFIED | 0 exported repo funcs with db-first signature; 108 `db.WithContext(ctx)` calls; 128 `c.Context()` call sites in handlers; D-09 (c) test exists |
| PERF-08 | Plans 00, 02, 05, 07 | `idx_connections_connected_at` index + 90-day pruning in scheduler | SATISFIED | Migration 023 DDL verified; `PruneOldConnections` runs weekly (every 10080 ticks) in `scheduler.go` |
| PERF-09 | Plans 00, 01 | Fiber BodyLimit 64KB, ReadTimeout 15s, WriteTimeout 30s; pgtune postgresql.conf | SATISFIED | All four Fiber config fields confirmed in `cmd/main.go:50-53`; `postgresql.conf` has `shared_buffers = 1GB`, `random_page_cost = 1.1` |

**All 9 PERF requirements (PERF-01 through PERF-09) are SATISFIED.** No orphaned requirements.

---

### Anti-Patterns Found

No stub anti-patterns found in the key implementation files. Systematic checks:

- `grep -n "TODO|FIXME|PLACEHOLDER" server/api/internal/cache/*.go` — 0 matches
- `grep -n "return null|return \{\}|return \[\]" server/api/internal/cache/*.go` — 0 (fail-open returns `"", nil` or `false, nil` which is correct design, not a stub)
- `grep -rn "UpdateHeartbeat(db" server/api/internal/handler/connection.go` — 0 matches (per-heartbeat PG write successfully removed)
- `grep -rn "repository\.[A-Za-z]*(nil," server/api/internal/` (production code) — 0 matches (no nil ctx)

One notable design confirmation: `UpdateHeartbeat` repo function is retained (deprecated, not deleted) to avoid breaking connection_repo tests. This is correctly documented in 06-05-SUMMARY.md and is not a stub — the handler no longer calls it.

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| (none found) | — | — | — | — |

---

### Human Verification Required

No additional human verification is required beyond what is already tracked in `06-HUMAN-UAT.md`. The three deferred items (live backfill, load test, second-host move) are correctly scoped as operator/hardware-dependent and are not code gaps:

1. **Live-DB index backfill** — PENDING-OPERATOR. The CI EXPLAIN assertions (`TestPerfIndexes`) prove the DDL and query-plan are correct against a fresh testcontainer DB. The backfill is a one-time manual step on the existing production database. Tracked in `06-HUMAN-UAT.md §1`.

2. **~10k synthetic load test** — DEFERRED to release phase per D-09. The five D-09 automated assertions are the in-phase definition-of-done. Tracked in `06-HUMAN-UAT.md §2`.

3. **Physical second-host provisioning** — DEFERRED (operator's call). The compose split and host parameterisation are code-complete. Tracked in `06-HUMAN-UAT.md §3`.

The human-verify checkpoint (Plan 07, Task 3) was executed and approved. No further human testing is required to call Phase 6 code-complete.

---

### Gaps Summary

No gaps. All nine PERF requirements are satisfied by code in the repository. The three operator/hardware-dependent items are correctly deferred in `06-HUMAN-UAT.md` and documented in `docs/runbooks/06-perf-deploy-runbook.md` — consistent with D-09 and with how Phases 4 and 5 handled hardware-dependent UAT.

The orchestrator confirmed `cd server/api && go test ./... -short` is GREEN. Two post-merge fix commits (`98ea89e`, `9bc8ce1`) were applied to address a scheduler regression and a missed call site, both with confirmed GREEN suite status.

---

_Verified: 2026-05-30T00:00:00Z_
_Verifier: Claude (gsd-verifier)_
