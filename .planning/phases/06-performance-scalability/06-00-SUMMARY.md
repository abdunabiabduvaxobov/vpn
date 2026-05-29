---
phase: 06-performance-scalability
plan: 00
subsystem: testing
tags: [go, testing, miniredis, testcontainers, postgres, redis, tdd, explain]

# Dependency graph
requires:
  - phase: 06-performance-scalability
    provides: existing migrations_test.go testcontainer bootstrap + plans_cache_test.go miniredis pattern
provides:
  - RED-first Wave 0 test scaffolds for every Phase 6 implementation wave
  - Unit tests (miniredis) for servers/user/heartbeat caches + ShouldRunScheduler gate
  - Integration tests (testcontainers PG16) for perf-index EXPLAIN, ctx-cancel, zero-SELECT cache-hit, scheduler flush/gate/prune
affects: [06-01, 06-02, 06-03, 06-04, 06-05, 06-06]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "RED-first TDD: tests reference not-yet-built symbols so they fail to compile until impl waves land them"
    - "miniredis for cache unit tests (no Docker); testcontainers PG16 for index/ctx EXPLAIN assertions guarded behind !testing.Short()"

key-files:
  created:
    - server/api/internal/cache/servers_cache_test.go
    - server/api/internal/cache/user_cache_test.go
    - server/api/internal/cache/heartbeat_cache_test.go
    - server/api/internal/config/scheduler_gate_test.go
    - server/api/migrations/perf_indexes_test.go
    - server/api/internal/repository/ctx_cancel_test.go
    - server/api/internal/handler/servers_cache_test.go
    - server/api/internal/scheduler/scheduler_perf_test.go
  modified: []

key-decisions:
  - "ShouldRunScheduler falsey set = {false, 0, no}; everything else (incl. empty) is true (default-on)"
  - "user cache signature (tier string, found bool, err error) — miss falls through to authoritative DB"
  - "DB-dependent flush/EXPLAIN assertions guarded behind !testing.Short() so the quick suite stays fast"

patterns-established:
  - "RED-first Wave 0: every downstream task has a concrete failing test to satisfy before it is considered done"
  - "Cache tests assert fail-open (closed Redis → empty/found=false, nil err) so caching never breaks the request path"

requirements-completed: [PERF-01, PERF-02, PERF-04, PERF-05, PERF-06, PERF-07, PERF-08]

# Metrics
duration: ~9min (crashed mid-Task-2 on API socket error; recovered by orchestrator)
completed: 2026-05-29
---

# Phase 06 Plan 00: Wave 0 Test Infrastructure Summary

**RED-first test scaffolds for every Phase 6 implementation wave — miniredis cache unit tests, ShouldRunScheduler truth table, and testcontainer EXPLAIN/ctx-cancel/zero-SELECT integration tests referencing not-yet-built symbols.**

## Performance

- **Duration:** ~9 min (agent run) + orchestrator recovery
- **Completed:** 2026-05-29
- **Tasks:** 2/2
- **Files created:** 8 test files

## Accomplishments
- Task 1 (committed by agent): 4 miniredis unit-test files — `servers_cache_test.go`, `user_cache_test.go`, `heartbeat_cache_test.go`, `scheduler_gate_test.go` (round-trip, miss, fail-open, bust, TTL, N→1 flush, ShouldRunScheduler truth table)
- Task 2 (recovered + committed by orchestrator): 4 integration-test files — `perf_indexes_test.go` (PERF-05/08 EXPLAIN JSON asserts `idx_connections_heartbeat_active` / `idx_connections_connected_at`), `ctx_cancel_test.go` (pgx v5 CancelRequest proof), `servers_cache_test.go` handler (zero-SELECT on cache-hit), `scheduler_perf_test.go` (flush ticker / RUN_SCHEDULER gate / weekly prune skeletons)
- All 8 files parse cleanly (gofmt) and are RED by construction — they reference `GetServersCache`, `GetUserCache`, `TouchHeartbeat`, `FlushHeartbeats`, `ShouldRunScheduler`, and the two new index names that land in later waves

## Task Commits

1. **Task 1: Cache + config unit-test scaffolds (miniredis)** - `dd6cb38` (test)
2. **Task 2: testcontainers integration-test scaffolds** - `7268433` (test)

## Files Created/Modified
- `server/api/internal/cache/servers_cache_test.go` - cache-aside/fail-open/bust for `cache:servers:active`
- `server/api/internal/cache/user_cache_test.go` - cache-aside/fail-open/bust/TTL≤5s for `user:<id>`
- `server/api/internal/cache/heartbeat_cache_test.go` - pipeline write + `hb:dirty` set; `FlushHeartbeats` N→1
- `server/api/internal/config/scheduler_gate_test.go` - `ShouldRunScheduler` truth table
- `server/api/migrations/perf_indexes_test.go` - EXPLAIN(FORMAT JSON) asserts on both new indexes
- `server/api/internal/repository/ctx_cancel_test.go` - cancelled `db.WithContext(ctx).Exec` aborts in-flight query
- `server/api/internal/handler/servers_cache_test.go` - zero `servers` SELECT on cache-hit (gorm logger capture)
- `server/api/internal/scheduler/scheduler_perf_test.go` - flush-ticker / gate / weekly-prune skeletons

## Decisions Made
- ShouldRunScheduler falsey set = `{false, 0, no}` (case-insensitive); empty/anything-else → true (default-on)
- user cache signature `(tier string, found bool, err error)`; cache miss falls through to the authoritative DB row
- DB-dependent assertions (`FlushHeartbeats` UPDATE, EXPLAIN) guarded behind `!testing.Short()`

## Deviations from Plan

### Recovery note
The executor agent crashed mid-Task-2 with an `API Error: socket connection was closed unexpectedly` **after** writing all four Task-2 files but **before** committing them or writing this SUMMARY. The orchestrator verified the four uncommitted files were complete (balanced braces, valid gofmt parse, all acceptance-criteria strings present), applied gofmt to one file with formatting drift (`handler/servers_cache_test.go`), committed them as the Task-2 atomic commit (`7268433`), and authored this SUMMARY. No file contents were changed beyond gofmt.

**Total deviations:** 1 (orchestrator crash-recovery; no functional change)
**Impact on plan:** None — all 8 planned files shipped exactly as specified.

## Issues Encountered
- Worktree was initially created from a stale base; the worktree branch base verified correct (`2f5a812`) before work.
- Mid-run API socket error killed the agent in the completion handler — recovered by orchestrator (see Deviations).

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- Every Phase 6 implementation wave now has a concrete failing test to satisfy.
- Tests remain RED (won't compile) until the cache packages, `ShouldRunScheduler`, `FlushHeartbeats`, and migrations 022/023 land in Waves 1–5. This is the intended Wave 0 state — `go build ./...` (production code) still passes; `go test`/`go vet` on the affected test packages fails ONLY on undefined symbols.

---
*Phase: 06-performance-scalability*
*Completed: 2026-05-29*
