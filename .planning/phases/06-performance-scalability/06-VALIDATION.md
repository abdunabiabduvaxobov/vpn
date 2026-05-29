---
phase: 6
slug: performance-scalability
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-05-29
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
| PERF-05 | `idx_connections_heartbeat_active` range scan via EXPLAIN ANALYZE on the stale-connection sweep | — | N/A | integration (testcontainers) | `go test ./migrations/... -run TestPerfIndexes` | ❌ W0 | ⬜ pending |
| PERF-08 | `idx_connections_connected_at` used by analytics date-bucket query (EXPLAIN) | — | N/A | integration | `go test ./migrations/... -run TestPerfIndexes` | ❌ W0 | ⬜ pending |
| PERF-01 | cache-hit path emits **zero `servers` SELECT** in query log; bust within one request on admin server-write | — | Fail-open: Redis outage must not break `/servers` | integration (gorm logger capture / miniredis) | `go test ./internal/handler/... -run TestServersCacheNoSelect` | ❌ W0 | ⬜ pending |
| PERF-02 | bulk flush absorbs ~1 write/10s instead of ~167/s: N heartbeats → 1 UPDATE | — | N/A | unit (miniredis) + integration | `go test ./internal/scheduler/... -run TestHeartbeatFlush` | ❌ W0 | ⬜ pending |
| PERF-04 | `user:<id>` cache hit → no `SELECT … FROM users`; bust → next request misses | T-06-USERCACHE | Stale Pro/tier never served past a mutation; fail-open on Redis outage | unit (miniredis) | `go test ./internal/middleware/... -run TestUserCache` | ❌ W0 | ⬜ pending |
| PERF-06 | `RUN_SCHEDULER=false` fires no jobs; only the primary runs periodic work | — | N/A | unit (gate helper) + integration (main-wiring) | `go test ./internal/config/... -run TestShouldRunScheduler` | ❌ W0 | ⬜ pending |
| PERF-07 | ctx cancellation removes the query from `pg_stat_activity` | — | Request abort releases the DB connection (no leak under load) | integration (testcontainers; cancel ctx mid-`pg_sleep`, poll `pg_stat_activity`) | `go test ./internal/repository/... -run TestCtxCancelAbortsQuery` | ❌ W0 | ⬜ pending |
| PERF-09 | Fiber config has the four timeout/limit values; PG pool + PrepareStmt set | — | BodyLimit 64KB caps request-body DoS surface | unit (assert on `app.Config()` / `db.Config`) | `go test ./... -run TestServerConfig` | ❌ W0 | ⬜ pending |
| PERF-03 | compose split parameterized (`DB_HOST`/`REDIS_HOST` indirection present, defaults resolve) | T-06-DATALINK | PG/Redis bound to private interface; Redis `--requirepass`; TLS-ready strings | static/lint (parse both compose files) | shell/CI check or `go test` reading the YAML | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

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

- [ ] `server/api/migrations/perf_indexes_test.go` — EXPLAIN ANALYZE assertions for PERF-05/PERF-08 (reuse `migrations_test.go` testcontainers setup)
- [ ] `server/api/internal/cache/servers_cache_test.go` + `user_cache_test.go` — miniredis cache-aside / fail-open / bust
- [ ] `server/api/internal/cache/heartbeat_cache_test.go` — pipeline write + dirty-set; `FlushHeartbeats` collapses N→1
- [ ] `server/api/internal/scheduler/scheduler_test.go` additions — 10s flush ticker, `RUN_SCHEDULER=false` no-op, weekly prune cadence
- [ ] `server/api/internal/config/scheduler_gate_test.go` — `ShouldRunScheduler` truth table (PERF-06 (d))
- [ ] `server/api/internal/repository/ctx_cancel_test.go` — testcontainers `pg_stat_activity` ctx-cancel assertion (PERF-07 (c))
- [ ] `server/api/internal/handler/servers_cache_test.go` — zero-`servers`-SELECT on cache hit (PERF-01 (b))
- [ ] No framework install needed — `testing`, testcontainers-go, miniredis all already present in go.mod.

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Real ~10k synthetic connection load test (PG write q/s drop, `/auth/refresh` p99, cleanup latency) | PERF-02 / phase goal | Requires staging hardware + k6/vegeta load driver; not reproducible in CI | **DEFERRED to end-of-milestone release phase** (HUMAN-UAT). Phase 6 ships with assertion-level proof only (D-09). |
| Physical second-host PG/Redis provisioning + private-link/firewall | PERF-03 | Operator/ops-runbook action on real infra; no code change | Documented runbook step. Phase 6 only proves the compose split + host parameterization is correct. |
| Production live-DB index backfill via `psql -f` `CREATE INDEX CONCURRENTLY` | PERF-05 / PERF-08 | Migration runner (`docker-entrypoint-initdb.d`) only fires on an **empty** pgdata volume; live backfill is a manual online step | Documented runbook step. Tests prove the index DDL is correct + used by EXPLAIN against a fresh testcontainer DB. |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 30s (quick) / < 5 min (wave)
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
