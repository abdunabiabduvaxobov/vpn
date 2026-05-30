---
phase: 6
slug: performance-scalability
status: validated
nyquist_compliant: true
wave_0_complete: true
created: 2026-05-29
validated: 2026-05-30
---

# Phase 6 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Definition-of-done is **automated assertions** (D-09), NOT a live load test — the real ~10k synthetic load test is deferred to the end-of-milestone release phase.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go standard `testing` + testcontainers-go (Postgres 16) + miniredis (Redis fake) |
| **Config file** | none — Go test discovery; `server/api/migrations/migrations_test.go` is the testcontainers template |
| **Quick run command** | `cd server/api && go test ./internal/cache/... ./internal/scheduler/... ./internal/repository/... -short` |
| **Full suite command** | `cd server/api && go test ./...` (testcontainers EXPLAIN/ctx tests run without `-short`) |
| **Estimated runtime** | ~25s quick (miniredis, no Docker) · ~3–5 min full (testcontainers spins Postgres 16) |

---

## Sampling Rate

- **After every task commit:** Run `cd server/api && go test ./internal/<touched-package>/... -short` (< 30s; miniredis-based, no Docker)
- **After every plan wave:** Run `cd server/api && go test ./...` (includes testcontainers EXPLAIN/ctx/index assertions)
- **Before `/gsd-verify-work`:** Full suite green **plus** the five D-09 assertions below
- **Max feedback latency:** 30s per task / ~5 min per wave

---

## Per-Task Verification Map

> Phase requirements → test map. Plan/wave/task IDs are filled by the planner; the requirement→behavior→command mapping is fixed by research.

| Requirement | Behavior | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|-------------|----------|------------|-----------------|-----------|-------------------|-------------|--------|
| PERF-05 | `idx_connections_heartbeat_active` range scan via EXPLAIN ANALYZE on the stale-connection sweep | — | N/A | integration (testcontainers) | `go test ./migrations/... -run TestPerfIndexes` | ✅ `migrations/perf_indexes_test.go` | 🐳 docker-gated |
| PERF-08 | `idx_connections_connected_at` used by analytics date-bucket query (EXPLAIN) | — | N/A | integration | `go test ./migrations/... -run TestPerfIndexes` | ✅ `migrations/perf_indexes_test.go` | 🐳 docker-gated |
| PERF-01 | cache-hit path emits **zero `servers` SELECT** in query log; bust within one request on admin server-write | — | Fail-open: Redis outage must not break `/servers` | integration (gorm logger capture / miniredis) | `go test ./internal/handler/... -run TestServersCacheNoSelect` | ✅ `internal/handler/servers_cache_test.go` | ✅ green |
| PERF-02 | bulk flush absorbs ~1 write/10s instead of ~167/s: N heartbeats → 1 UPDATE | — | N/A | unit (miniredis) + integration | `go test ./internal/cache/... -run TestFlushHeartbeats` · `go test ./internal/scheduler/... -run TestHeartbeatFlush` | ✅ `internal/cache/heartbeat_cache_test.go` + `internal/scheduler/scheduler_perf_test.go` | 🐳 docker-gated (N→1 collapse + ticker need PG; fail-open/empty-set cases ✅ green) |
| PERF-04 | `user:<id>` cache hit → no `SELECT … FROM users`; bust → next request misses | T-06-USERCACHE | Stale Pro/tier never served past a mutation; fail-open on Redis outage | unit (miniredis) | `go test ./internal/cache/... -run TestUserCache` | ✅ `internal/cache/user_cache_test.go` | ✅ green |
| PERF-06 | `RUN_SCHEDULER=false` fires no jobs; only the primary runs periodic work | — | N/A | unit (gate helper) + integration (main-wiring) | `go test ./internal/config/... -run TestShouldRunScheduler` | ✅ `internal/config/scheduler_gate_test.go` | ✅ green |
| PERF-07 | ctx cancellation removes the query from `pg_stat_activity` | — | Request abort releases the DB connection (no leak under load) | integration (testcontainers; cancel ctx mid-`pg_sleep`, poll `pg_stat_activity`) | `go test ./internal/repository/... -run TestCtxCancelAbortsQuery` | ✅ `internal/repository/ctx_cancel_test.go` | 🐳 docker-gated |
| PERF-09 | Fiber config has the four timeout/limit values; PG pool + PrepareStmt set | — | BodyLimit 64KB caps request-body DoS surface | unit (assert on `app.Config()` / `db.Config`) | `go test ./cmd/... -run "TestServerConfig" ./internal/repository/... -run "TestGormConfig\|TestApplyPoolSettings"` | ✅ `cmd/server_config_test.go` + `internal/repository/db_test.go` | ✅ green |
| PERF-03 | compose split parameterized (`DB_HOST`/`REDIS_HOST` indirection present, defaults resolve) | T-06-DATALINK | PG/Redis bound to private interface; Redis `--requirepass`; TLS-ready strings | static (parse both compose YAMLs, no Docker) | `go test ./cmd/... -run TestComposeDataTierSplit` | ✅ `cmd/compose_topology_test.go` *(added by this audit)* | ✅ green |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky · 🐳 docker-gated (in the full `go test ./...` testcontainers suite; SKIPs under `-short`/no-Docker)*

> **Local run note (2026-05-30):** the 5 non-Docker assertions (PERF-01/03/04/06/09) were run GREEN on this machine (`go test ./... -short`, go 1.26.1, no Docker). The 4 docker-gated assertions (PERF-02 collapse, PERF-05, PERF-07, PERF-08) require Docker for testcontainers/PG and SKIP cleanly under `-short`; they were confirmed GREEN by the phase verifier against fresh testcontainer DBs (see 06-VERIFICATION.md) and by the orchestrator's post-merge full-suite run. Run them with Docker up: `cd server/api && go test ./migrations/... ./internal/repository/... ./internal/cache/... ./internal/scheduler/... -count=1`.

---

## The Five D-09 Phase-Gate Assertions

These are the canonical definition-of-done. All five MUST be green before `/gsd-verify-work`:

- **(a)** `EXPLAIN ANALYZE` shows `idx_connections_heartbeat_active` range/index scan on the stale sweep (and `idx_connections_connected_at` usage on analytics) — PERF-05/PERF-08.
- **(b)** the cache-hit path emits **zero `servers` SELECT** in the query log — PERF-01.
- **(c)** `ctx` cancellation **removes the query from `pg_stat_activity`** — PERF-07.
- **(d)** a `RUN_SCHEDULER=false` instance fires **no scheduler jobs** — PERF-06.
- **(e)** the heartbeat bulk flush absorbs **~1 write / 10s** instead of ~167 writes/s — PERF-02.

---

## Wave 0 Requirements

- [x] `server/api/migrations/perf_indexes_test.go` — EXPLAIN ANALYZE assertions for PERF-05/PERF-08 (`TestPerfIndexes`, testcontainers) 🐳
- [x] `server/api/internal/cache/servers_cache_test.go` + `user_cache_test.go` — miniredis cache-aside / fail-open / bust ✅ green
- [x] `server/api/internal/cache/heartbeat_cache_test.go` — pipeline write + dirty-set (`TestTouchHeartbeat*` ✅ green); `FlushHeartbeats` collapses N→1 (`TestFlushHeartbeatsCollapsesNto1` 🐳 needs PG)
- [x] `server/api/internal/scheduler/scheduler_perf_test.go` — 10s flush ticker (`TestHeartbeatFlushTicker` 🐳), `RUN_SCHEDULER=false` no-op (`TestSchedulerGateNoJobs`), weekly prune cadence (`TestWeeklyPruneCadence`)
- [x] `server/api/internal/config/scheduler_gate_test.go` — `ShouldRunScheduler` truth table (PERF-06 (d), `TestShouldRunScheduler`) ✅ green
- [x] `server/api/internal/repository/ctx_cancel_test.go` — testcontainers `pg_stat_activity` ctx-cancel assertion (PERF-07 (c), `TestCtxCancelAbortsQuery`) 🐳
- [x] `server/api/internal/handler/servers_cache_test.go` — zero-`servers`-SELECT on cache hit (PERF-01 (b), `TestServersCacheNoSelect`) ✅ green
- [x] `server/api/cmd/server_config_test.go` + `internal/repository/db_test.go` — PERF-09 Fiber/pool/PrepareStmt config locks ✅ green
- [x] **PERF-03 gap filled by this audit:** `server/api/cmd/compose_topology_test.go` (`TestComposeDataTierSplit`) — static parse of both compose YAMLs (data tier owns postgres+redis, app tier owns neither, `${DB_HOST}`/`${REDIS_HOST}` parameterized) ✅ green, no Docker
- [x] No framework install needed — `testing`, testcontainers-go, miniredis, `gopkg.in/yaml.v3` all already present in go.mod (yaml.v3 promoted indirect→direct for the compose test).

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Real ~10k synthetic connection load test (PG write q/s drop, `/auth/refresh` p99, cleanup latency) | PERF-02 / phase goal | Requires staging hardware + k6/vegeta load driver; not reproducible in CI | **DEFERRED to end-of-milestone release phase** (HUMAN-UAT). Phase 6 ships with assertion-level proof only (D-09). |
| Physical second-host PG/Redis provisioning + private-link/firewall | PERF-03 | Operator/ops-runbook action on real infra; no code change | Documented runbook step. Phase 6 only proves the compose split + host parameterization is correct. |
| Production live-DB index backfill via `psql -f` `CREATE INDEX CONCURRENTLY` | PERF-05 / PERF-08 | Migration runner (`docker-entrypoint-initdb.d`) only fires on an **empty** pgdata volume; live backfill is a manual online step | Documented runbook step. Tests prove the index DDL is correct + used by EXPLAIN against a fresh testcontainer DB. |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references (PERF-03 gap closed by this audit)
- [x] No watch-mode flags
- [x] Feedback latency < 30s (quick) / < 5 min (wave)
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** validated 2026-05-30 — all 9 PERF requirements have committed, behavior-targeting automated tests. 5 run green without Docker; 4 are docker-gated (full testcontainers suite, verified green by the phase verifier + orchestrator). Genuinely manual items remain in the Manual-Only table.

---

## Validation Audit 2026-05-30

| Metric | Count |
|--------|-------|
| Requirements audited | 9 |
| Already covered (test existed, green) | 8 |
| Gaps found | 1 (PERF-03 — no committed automated test) |
| Resolved (test written + green) | 1 (PERF-03 → `TestComposeDataTierSplit`) |
| Map corrections | 1 (PERF-04 command: `./internal/middleware/...` → `./internal/cache/...`; the old path matched **zero** tests / vacuous pass) |
| Escalated to manual-only | 0 |

**Actions taken:**
- **Filled PERF-03 gap:** added `server/api/cmd/compose_topology_test.go` (`TestComposeDataTierSplit`) — a Docker-free static YAML parse asserting the data-tier split + `${DB_HOST:-postgres}`/`${REDIS_HOST:-redis}` parameterization. Promoted `gopkg.in/yaml.v3` from indirect→direct in `go.mod` (surgical; `go.sum` untouched).
- **Corrected the PERF-04 map row:** the existing command pointed at `./internal/middleware/...`, which has no `TestUserCache` (vacuous pass, flagged in 06-04-SUMMARY); the real tests live in `./internal/cache/user_cache_test.go` (5 cases, green).
- **Corrected the PERF-02 / PERF-09 commands** to the packages where the tests actually live (cache + scheduler; cmd + repository).
- **Re-ran the suite locally** (go 1.26.1, no Docker): `go vet ./...` clean, `go test ./... -short` all green. The 4 docker-gated assertions SKIP cleanly under `-short` and were confirmed green by the verifier (06-VERIFICATION.md, 9/9) against fresh testcontainer DBs.
