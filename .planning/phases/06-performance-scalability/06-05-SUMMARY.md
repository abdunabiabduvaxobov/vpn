---
phase: 06-performance-scalability
plan: 05
subsystem: api
tags: [redis, heartbeat, scheduler, write-coalescing, go, gorm, postgres, run_scheduler, pruning]

# Dependency graph
requires:
  - phase: 06-performance-scalability (Wave 0)
    provides: RED tests — heartbeat_cache_test.go (TouchHeartbeat/FlushHeartbeats), scheduler_gate_test.go (ShouldRunScheduler truth table), scheduler_perf_test.go (flush ticker / gate / weekly prune skeletons)
  - phase: 06-performance-scalability (06-04, Wave 3)
    provides: redisClient threaded into scheduler.Start; user:<id> entitlement cache + bustExpiredUsers pattern
  - phase: 06-performance-scalability (06-01)
    provides: cache-aside + fail-open contract (servers_cache.go / user_cache.go) mirrored here
provides:
  - "PERF-02: heartbeat path writes ONLY to Redis (SET hb:<id> EX 600 + SADD hb:dirty), a dedicated 10s ticker collapses N dirty ids into ONE bulk Postgres UPDATE (write-coalescing buffer, PG stays source-of-truth)"
  - "PERF-06: config.ShouldRunScheduler gate — RUN_SCHEDULER=false fires no scheduler jobs; default true for single-replica v2.2.0"
  - "PERF-08: weekly 90-day connection prune (repository.PruneOldConnections) bounding connections table growth"
  - "D-10c: per-job interval registry — sessions every 5 ticks, stale devices daily, expiry downgrade every 10, weekly prune; reservations/stale-conn/link-codes stay 1-min"
affects: [horizontal-scaling, multi-replica-api, performance-validation, tranche-3]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Redis write-coalescing buffer: hot per-call writes pipelined into Redis (SET key + SADD dirty-set), drained on a fixed ticker into ONE bulk UPDATE; source-of-truth DB unchanged"
    - "At-least-once flush: on UPDATE error the dirty set is NOT drained, so the next tick retries; idempotent same-value UPDATE makes re-flush harmless (multi-replica safe)"
    - "Pure env-gate helper (ShouldRunScheduler) so the main.go branch is unit-testable without a real second replica"
    - "Per-job modulo interval registry inside one ticker loop (tickCount % N) instead of N separate tickers"

key-files:
  created:
    - server/api/internal/cache/heartbeat_cache.go
  modified:
    - server/api/internal/handler/connection.go
    - server/api/internal/scheduler/scheduler.go
    - server/api/internal/config/config.go
    - server/api/internal/repository/connection_repo.go
    - server/api/cmd/main.go
    - server/api/internal/handler/connection_test.go

key-decisions:
  - "Kept scheduler.Start 4-arg signature (db, logger, cfg, redisClient) unchanged — the 10s flush goroutine reuses the existing redisClient; existing scheduler_test.go callers stay valid"
  - "Retained the FindConnectionByID ownership pre-read in the heartbeat handler (cheap indexed PK read) — the PERF-02 win is removing the per-call WRITE, not the read; ownership/404 enforcement preserved (T-06-HB-OWNERSHIP)"
  - "UpdateHeartbeat repo fn retained with a deprecation comment (no longer called by the handler) rather than deleted — avoids breaking connection_repo tests; removal was optional per plan"
  - "Per-job intervals implemented as a tickCount modulo registry in one goroutine; first immediate run = tick 1 so no weekly/daily/5-min job fires spuriously on startup"

patterns-established:
  - "Heartbeat fail-open: nil client / Redis error → handler still returns 204 (heartbeat non-critical, 3-min stale grace + next beat cover it)"
  - "Weekly/daily/5-min cadences keyed off a single 1-min ticker via tickCount % interval"

requirements-completed: [PERF-02, PERF-06, PERF-08]

# Metrics
duration: 7min
completed: 2026-05-29
---

# Phase 6 Plan 5: Heartbeat Redis Write-Coalescing + RUN_SCHEDULER Gate + Weekly Prune Summary

**Heartbeat hot path moved entirely off Postgres into a Redis pipeline drained by a dedicated 10s ticker into one bulk UPDATE (~167 write-q/s collapsed to ~1 write/10s), plus a unit-testable RUN_SCHEDULER gate and a weekly 90-day connection prune.**

## Performance

- **Duration:** 7 min
- **Started:** 2026-05-29T17:52:53Z
- **Completed:** 2026-05-29T17:59:30Z
- **Tasks:** 3
- **Files modified:** 7 (1 created, 6 modified)

## Accomplishments
- **PERF-02:** `cache.TouchHeartbeat` pipelines `SET hb:<id> EX 600` + `SADD hb:dirty` in one round-trip; `cache.FlushHeartbeats` SMEMBERS the dirty set, runs ONE `UPDATE connections SET last_heartbeat_at=now WHERE id IN (...) AND disconnected_at IS NULL` (GORM native IN expansion, A3), then SREMs the flushed ids. A dedicated 10s scheduler goroutine drives it. The heartbeat handler no longer calls `repository.UpdateHeartbeat` — the per-call Postgres write (the hottest write path at 10k connections) is eliminated. Postgres stays source-of-truth.
- **PERF-06:** `config.ShouldRunScheduler(env)` pure helper — falsey set `{false,0,no}` (case-insensitive, trimmed), default true (including unset). `main.go` gates the entire scheduler behind `cfg.RunScheduler`; `RUN_SCHEDULER=false` fires no jobs and logs "scheduler disabled".
- **PERF-08:** `repository.PruneOldConnections(db, cutoff)` deletes disconnected rows older than 90 days; the scheduler runs it weekly (every 10080 ticks). Active rows (`disconnected_at IS NULL`) are never touched.
- **D-10c:** Per-job interval registry — `DeleteExpiredSessions` every 5 ticks, `DeleteStaleDevices` daily (1440), expiry-plan downgrade every 10; reservations/stale-connection/link-code cleanup + subscription downgrade stay 1-min.
- Both tickers (1-min cleanup + 10s flush) are lifecycle-managed: tracked by the same `s.wg`/`s.done`, both stopped and awaited in `Stop()`.

## Task Commits

Each task was committed atomically:

1. **Task 1: heartbeat_cache.go — TouchHeartbeat pipeline + FlushHeartbeats bulk flush** - `edb0701` (feat)
2. **Task 2: Heartbeat handler writes only to Redis (remove per-call PG write)** - `90e0b8c` (feat)
3. **Task 3: 10s flush goroutine + per-job interval registry + weekly prune + RUN_SCHEDULER gate** - `d97c66b` (feat)

_Note: Task 1 was a TDD GREEN task — the RED test (`heartbeat_cache_test.go`) landed in Wave 0; this plan supplied the implementation, so it is a single feat commit rather than a test→feat pair._

## Files Created/Modified
- `server/api/internal/cache/heartbeat_cache.go` (created) - `TouchHeartbeat` pipeline (SET hb:<id> EX 600 + SADD hb:dirty) and `FlushHeartbeats` bulk flush (SMEMBERS → one IN-clause UPDATE → SREM); fail-open on nil client; UPDATE error leaves hb:dirty intact for retry.
- `server/api/internal/handler/connection.go` (modified) - `HeartbeatConnection` now takes `redisClient *redis.Client`, calls `cache.TouchHeartbeat`, drops `repository.UpdateHeartbeat`; ownership pre-read retained; TouchHeartbeat error logged but still returns 204.
- `server/api/internal/scheduler/scheduler.go` (modified) - second 10s `flushTicker` goroutine calling `cache.FlushHeartbeats`; per-job modulo interval registry in `runCleanup`; weekly 90-day prune; `Stop()` stops both tickers and waits both goroutines; `cleanupTickCount` subsumes the old `expiryTickCount`.
- `server/api/internal/config/config.go` (modified) - `ShouldRunScheduler` pure helper + `RunScheduler` field computed from `getEnv("RUN_SCHEDULER","true")` in `Load()`.
- `server/api/internal/repository/connection_repo.go` (modified) - new `PruneOldConnections(db, cutoff)`; `UpdateHeartbeat` kept with a deprecation comment.
- `server/api/cmd/main.go` (modified) - gates `scheduler.Start` behind `cfg.RunScheduler`; passes `redisClient` to the heartbeat route.
- `server/api/internal/handler/connection_test.go` (modified) - `buildApp` passes `nil` redis client to `HeartbeatConnection` (fail-open keeps the route returning 204 in DB-only handler tests).

## Decisions Made
- **scheduler.Start signature unchanged (4 args):** the 10s flush goroutine reuses the existing `redisClient` parameter (added in 06-04), so `scheduler_test.go`'s `Start(db, log, testCfg(), nil)` callers compile unchanged and the gate test path stays valid.
- **Ownership pre-read retained in heartbeat handler:** a cheap indexed PK read that enforces per-request 404/403; the PERF-02 win is removing the per-call WRITE, not the read (T-06-HB-OWNERSHIP).
- **UpdateHeartbeat repo fn kept (deprecated, not deleted):** the handler was its only caller, but deleting it would risk repo tests; left in place with a deprecation comment per plan guidance.
- **Per-job registry via one ticker + modulo:** simpler lifecycle than N separate tickers; the immediate first run is tick 1, so weekly/daily/5-min jobs never fire spuriously at startup.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Updated connection_test.go buildApp for the new HeartbeatConnection signature**
- **Found during:** Task 2 (heartbeat handler signature change)
- **Issue:** `internal/handler/connection_test.go:233` called `HeartbeatConnection(log, db)`; the new 3-arg signature (`+ redisClient`) would break the handler test package compilation — blocking the whole `internal/handler` test build.
- **Fix:** Passed `nil` as the redis client (TouchHeartbeat is fail-open on nil, so the route still returns 204 without standing up miniredis) and added a comment explaining the fail-open rationale.
- **Files modified:** server/api/internal/handler/connection_test.go
- **Verification:** grep confirms the only `HeartbeatConnection(` call sites are main.go (3-arg with redisClient) and the test (3-arg with nil); manual review (go toolchain denied in sandbox — see "Action needed from the orchestrator").
- **Committed in:** 90e0b8c (Task 2 commit)

---

**Total deviations:** 1 auto-fixed (1 blocking).
**Impact on plan:** Necessary to keep the handler test package compiling after the signature change. No scope creep — the fix is a one-line test-wiring update consistent with the fail-open contract.

## Issues Encountered
- **Go/gofmt toolchain denied in the worktree sandbox:** `go build`, `go test`, and `gofmt` were all denied by the sandbox (per the plan's `<validation_note>`, an expected restriction). Verification relied on careful manual review + `grep` acceptance-criteria checks. All Wave 0 RED test symbols are now defined (`TouchHeartbeat`, `FlushHeartbeats`, `ShouldRunScheduler`), so the previously non-compiling `internal/cache`, `internal/config`, and `internal/scheduler` test packages should now compile and the suite should run for the orchestrator.

## Action needed from the orchestrator
Run post-merge validation with the (now-available) Go toolchain:
```bash
cd server/api && go build ./...
cd server/api && go test ./internal/cache/... -run "TestTouchHeartbeat|TestFlushHeartbeats"
cd server/api && go test ./internal/config/... -run TestShouldRunScheduler -short
cd server/api && go test ./internal/scheduler/... -run "TestHeartbeatFlush|TestSchedulerGate|TestWeeklyPrune"
cd server/api && go test ./internal/handler/... -run "TestHeartbeat"   # no Heartbeat-named cases; build-compiles check
cd server/api && gofmt -l internal/cache/heartbeat_cache.go internal/config/config.go internal/scheduler/scheduler.go internal/handler/connection.go internal/repository/connection_repo.go cmd/main.go
```
Expectation: build green; the three Wave 0 perf/cache/config test packages compile (all referenced symbols now exist) and pass — including the N→1 collapse (D-09 (e)) and the ShouldRunScheduler truth table (D-09 (d)). `TestWeeklyPruneCadence` remains a `t.Skip` skeleton (plan exposes the weekly cadence as the `pruneConnectionEveryTicks = 10080` constant rather than an exported tunable).

## User Setup Required
Optional operator env var (defaults preserve current behavior):
- `RUN_SCHEDULER` — unset/`true` keeps the scheduler running (single-replica v2.2.0 default). Set to `false`/`0`/`no` on non-primary replicas in a future multi-replica (Tranche 3) deploy so only the primary fires periodic jobs.

No external service configuration required.

## Next Phase Readiness
- The heartbeat hot write path is eliminated and the scheduler is gateable — both prerequisites for horizontal API scaling (Tranche 3) are in place.
- Postgres remains the source of truth; a Redis restart loses at most one ~10s flush window, well inside the 3-min stale-sweep grace.
- No blockers introduced. Awaiting orchestrator post-merge `go build`/`go test`/`gofmt` validation (toolchain denied in this worktree sandbox).

## Self-Check: PASSED

- `server/api/internal/cache/heartbeat_cache.go` — FOUND
- `.planning/phases/06-performance-scalability/06-05-SUMMARY.md` — FOUND
- Commit `edb0701` (Task 1) — FOUND
- Commit `90e0b8c` (Task 2) — FOUND
- Commit `d97c66b` (Task 3) — FOUND

---
*Phase: 06-performance-scalability*
*Completed: 2026-05-29*
