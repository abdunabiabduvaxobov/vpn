---
phase: 06-performance-scalability
plan: 02
subsystem: database
tags: [postgres, indexes, migrations, gorm, performance, connections]

# Dependency graph
requires:
  - phase: 06-performance-scalability (plan 00, Wave 0)
    provides: TestPerfIndexes EXPLAIN harness that asserts both new indexes are used (merged by orchestrator; not present in this isolated worktree)
provides:
  - "PERF-05: idx_connections_heartbeat_active partial index (migration 022) + COALESCE-dropped stale-sweep predicate so the planner range-scans it"
  - "PERF-08: idx_connections_connected_at index (migration 023) for admin analytics date-bucket queries"
  - "Online-safe, idempotent CONCURRENTLY migration pattern with an in-file empty-volume manual-backfill caveat (input to plan 06's runbook)"
affects: [06-performance-scalability plan 06 (deploy/runbook — manual index backfill on the live DB), admin analytics, scheduler stale-connection sweep]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "CREATE INDEX CONCURRENTLY IF NOT EXISTS in a transaction-free migration file (mirrors migration 017), idempotent and online-safe for live backfill"
    - "Partial b-tree index matching a WHERE-narrowed hot predicate so the planner range-scans live rows only"

key-files:
  created:
    - server/api/migrations/022_add_perf_indexes.sql
    - server/api/migrations/023_connections_connected_at_index.sql
  modified:
    - server/api/internal/repository/connection_repo.go

key-decisions:
  - "Kept PERF-05 (022) and PERF-08 (023) in separate migration files so the runbook can backfill each independently and the test harness runs each cleanly."
  - "Dropped COALESCE only from CleanupStaleConnections (high-volume sweep); intentionally retained it in CleanupStaleReservations (tiny status='connecting' subset, out of PERF-05 scope) to minimize surface."

patterns-established:
  - "Transaction-free CONCURRENTLY migration with in-file empty-volume manual-backfill caveat pointing to the deploy runbook."
  - "Predicate rewrite justified by a non-null invariant (migration 008 backfill + every CreateConnection* sets last_heartbeat_at) so a partial index range-scan replaces a COALESCE-blocked seq scan."

requirements-completed: [PERF-05, PERF-08]

# Metrics
duration: ~15min
completed: 2026-05-29
---

# Phase 6 Plan 02: Connections Performance Indexes Summary

**Partial heartbeat index (idx_connections_heartbeat_active) + connected_at index added as online-safe CONCURRENTLY migrations 022/023, plus a COALESCE-drop on the stale-connection sweep so the planner range-scans the partial index instead of sequential-scanning the full connections history.**

## Performance

- **Duration:** ~15 min
- **Started:** 2026-05-29T16:55Z (approx)
- **Completed:** 2026-05-29T17:10Z
- **Tasks:** 2
- **Files modified:** 3 (2 created, 1 modified)

## Accomplishments
- PERF-05: `idx_connections_heartbeat_active` partial index — `ON connections(last_heartbeat_at) WHERE disconnected_at IS NULL` — keeps the stale-connection sweep O(connected) not O(history).
- PERF-08: `idx_connections_connected_at` index for the admin analytics date-bucket queries (admin_repo.go) so they stay fast as the table grows to millions of rows.
- Dropped the `COALESCE(last_heartbeat_at, connected_at)` wrapper from `CleanupStaleConnections` so the predicate is a clean `last_heartbeat_at < cutoff` range — the form the partial index can serve.
- Both migrations are transaction-free, `CONCURRENTLY`, and `IF NOT EXISTS` (online-safe for a live backfill, idempotent whether run by initdb on a fresh volume or by hand on the live DB), with the empty-volume manual-backfill caveat documented in-file for plan 06's runbook.

## Task Commits

Each task was committed atomically:

1. **Task 1: Migration 022 (partial heartbeat index) + 023 (connected_at index)** - `076d4f5` (feat)
2. **Task 2: Drop COALESCE from CleanupStaleConnections so the partial index range-scans** - `5333eac` (refactor)

_Plan metadata commit (SUMMARY) is created by the orchestrator after the wave merges; STATE.md/ROADMAP.md are owned by the orchestrator per the objective._

## Files Created/Modified
- `server/api/migrations/022_add_perf_indexes.sql` - PERF-05 partial index `idx_connections_heartbeat_active` (CONCURRENTLY IF NOT EXISTS, WHERE disconnected_at IS NULL); header documents the no-tx rationale + empty-volume backfill caveat.
- `server/api/migrations/023_connections_connected_at_index.sql` - PERF-08 index `idx_connections_connected_at` (CONCURRENTLY IF NOT EXISTS); same no-tx + backfill-caveat header; kept separate from 022.
- `server/api/internal/repository/connection_repo.go` - `CleanupStaleConnections` predicate now `disconnected_at IS NULL AND last_heartbeat_at < ?` (COALESCE dropped); doc comment updated to reference the safety invariant + index; `CleanupStaleReservations` annotated as intentionally retaining COALESCE.

## Decisions Made
- **Separate migration files for 022/023** — independent runbook backfill and clean per-file harness runs, per the plan.
- **COALESCE-drop scoped to `CleanupStaleConnections` only** — `CleanupStaleReservations` filters the low-volume `status='connecting'` subset where COALESCE is harmless and out of PERF-05's strict scope; left as-is with an explanatory comment to minimize surface (research Fork 2 explicitly permitted "leave it").
- **Safety basis for the drop** — every active (`disconnected_at IS NULL`) row has a non-null `last_heartbeat_at`: migration 008 backfilled then-active rows and both `CreateConnection`/`CreateConnectionAtomic` set `LastHeartbeatAt = &now` on every insert. So the COALESCE was a no-op for live rows and the rewrite is behaviorally equivalent (threat T-06-IDX-02 = accept).

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered
- The plan's `verify` blocks reference `go test ./migrations/... -run TestPerfIndexes` (the Wave 0 EXPLAIN assertion). That test file (`perf_indexes_test.go` / `TestPerfIndexes`) is owned by plan 00 and is NOT present in this isolated parallel worktree (which was reset to base `2f5a812`), so the run reports `ok ... [no tests to run]` rather than executing the EXPLAIN assertion. This is expected for parallel-worktree execution — the orchestrator merges Wave 0's test with these artifacts, at which point `TestPerfIndexes` exercises the EXPLAIN plan against the new indexes + the rewritten predicate. The artifacts this plan owns satisfy that test by construction: the partial index matches the rewritten predicate exactly, and the connected_at index matches the analytics query.
- Behavioral equivalence of the COALESCE-drop was independently confirmed in this worktree: existing `TestCleanupStaleConnections_MarksOldConnections` and `TestCleanupStaleConnections_DoesNotTouchRecentConnections` both pass unchanged with the new predicate (the marks-old test backdates both `connected_at` and `last_heartbeat_at`; the fresh test relies on `CreateConnection` setting `last_heartbeat_at`).
- Worktree base correction: this worktree initially pointed at a stale phase-01 commit (`6a3da00`); reset (soft then hard) onto the correct base `2f5a812` before any work, matching the prescribed worktree-base-fix procedure.

## User Setup Required
None - no external service configuration required by this plan directly.

**Operator action carried to plan 06's runbook:** because `docker-entrypoint-initdb.d` runs only on an empty `pgdata` volume, the two new indexes will NOT be created on the existing live database by a normal `docker compose up`. The live backfill is a manual, online (no write-lock) step:
```
docker exec vpn-postgres psql -U vpnapp -d vpnapp -f /docker-entrypoint-initdb.d/022_add_perf_indexes.sql
docker exec vpn-postgres psql -U vpnapp -d vpnapp -f /docker-entrypoint-initdb.d/023_connections_connected_at_index.sql
```
`CONCURRENTLY` keeps the write path available; `IF NOT EXISTS` keeps it idempotent.

## Next Phase Readiness
- PERF-05 + PERF-08 indexes and the range-scan-friendly predicate are in place; ready for the Wave 0 `TestPerfIndexes` EXPLAIN assertion (D-09 (a)) to confirm both indexes are used once the wave merges.
- Plan 06 (deploy/runbook) must include the manual index-backfill step above and a HUMAN-UAT note covering the empty-volume caveat.

## Self-Check: PASSED

- FOUND: server/api/migrations/022_add_perf_indexes.sql
- FOUND: server/api/migrations/023_connections_connected_at_index.sql
- FOUND: server/api/internal/repository/connection_repo.go
- FOUND: .planning/phases/06-performance-scalability/06-02-SUMMARY.md
- FOUND commit: 076d4f5 (Task 1)
- FOUND commit: 5333eac (Task 2)

---
*Phase: 06-performance-scalability*
*Completed: 2026-05-29*
