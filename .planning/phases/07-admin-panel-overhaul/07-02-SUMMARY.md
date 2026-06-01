---
phase: 07-admin-panel-overhaul
plan: 02
subsystem: api
tags: [admin, dashboard, kpis, mrr, redis-cache, gorm, tdd]

# Dependency graph
requires:
  - phase: 07-admin-panel-overhaul
    provides: "migration 024 (lava_webhook_events.status, suspend/drain cols) + RED stub TestAdminGetStatsKPIs from plan 07-01"
  - phase: 03-lava-top-plans-catalog
    provides: "plans/plan_offers/lava_contracts/lava_webhook_events schema the KPI + MRR queries read"
provides:
  - "repository.GetDashboardKPIs — GetGlobalStats + paid_users/active_connections/signups_today/week/month/churn_30d/failed_payments_30d"
  - "repository.GetMRR(ctx, db, currency) — annualised monthly+yearly active-offer revenue per currency"
  - "AdminGetStats(logger, db, redisClient) returning the eight new KPIs with a 5-min Redis MRR cache (fail-open)"
affects: [07-09, 07-10]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Go-computed time bounds passed as bind parameters (not now()-interval literals) so KPI/MRR queries run on both Postgres and SQLite test DBs"
    - "Fail-open per-currency Redis cache (cache:admin:mrr:<currency>, 5-min TTL): cache miss/parse-fail/outage all fall through to a live compute"

key-files:
  created: []
  modified:
    - server/api/internal/repository/admin_repo.go
    - server/api/internal/handler/admin.go
    - server/api/cmd/main.go
    - server/api/internal/handler/admin_kpis_test.go
    - server/api/internal/handler/admin_test.go

key-decisions:
  - "System-plan code resolved via correlated subquery (subscription_tier != (SELECT code FROM plans WHERE is_system)) — no hardcoded free-tier code"
  - "MRR computed as a single aggregate query (MONTHLY=amount, PERIOD_YEAR=amount/12), cached 5 min so the dashboard's 60s poll collapses to one aggregate per window (T-07-05)"
  - "MRR cache is fail-open: nil redisClient, read error, miss, or unparseable value all recompute live; write errors are logged not fatal"

patterns-established:
  - "KPI time windows are bound parameters computed in Go, keeping the same query dialect-portable across Postgres and the SQLite handler tests"
  - "currency flows only into a cache-key suffix and a parameterised WHERE — never string-concatenated into SQL (T-07-07)"

requirements-completed: [ADMIN-01]

# Metrics
duration: 9 min
completed: 2026-06-01
---

# Phase 7 Plan 02: Dashboard KPI Bar Summary

**GET /admin/stats now returns eight live KPIs (paid_users, mrr, active_connections, signups_today/week/month, churn_30d, failed_payments_30d) alongside the four legacy counts, with MRR computed as one aggregate query and cached 5 minutes in Redis (fail-open).**

## Performance

- **Duration:** 9 min
- **Started:** 2026-06-01T11:31:55Z
- **Completed:** 2026-06-01T11:40:07Z
- **Tasks:** 2
- **Files modified:** 5

## Accomplishments

- **`repository.GetDashboardKPIs`** wraps `GetGlobalStats` (preserving its four keys) and adds the seven count-based KPIs. The system-plan code is resolved via a correlated subquery so the free tier is never hardcoded; all time windows are computed in Go and bound as parameters so the identical query runs on Postgres (production) and SQLite (handler tests). Every query threads `ctx` via `db.WithContext` (PERF-07).
- **`repository.GetMRR(ctx, db, currency)`** is a single aggregate: it joins active paid users → their plan's active offer in the requested currency, summing MONTHLY offers at full amount and PERIOD_YEAR offers at amount/12. Returns `(0, nil)` for an empty book.
- **`AdminGetStats`** gained a `*redis.Client` arg (one-line `cmd/main.go` call-site update) and now reports MRR via `cache:admin:mrr:<currency>` (currency from `?currency`, default USD) at a 5-min TTL. The cache path is fail-open at every step (T-07-05 / Redis-outage requirement).
- **The 07-01 RED stub `TestAdminGetStatsKPIs` is now GREEN** — it seeds a paid user, an active connection, a cancelled contract, and a failed webhook event against an in-memory SQLite + miniredis rig, then asserts all twelve response keys, the seeded numeric values, and that the MRR cache key is populated after the first call.

## Task Commits

1. **Task 1: Add GetDashboardKPIs + GetMRR to admin_repo.go** - `23ba500` (feat)
2. **Task 2: Wire AdminGetStats to KPIs + 5-min MRR cache; turn RED KPI test GREEN** - `ae0a541` (feat)

## Files Created/Modified

- `server/api/internal/repository/admin_repo.go` - added `GetDashboardKPIs` and `GetMRR`
- `server/api/internal/handler/admin.go` - `AdminGetStats` now takes a redis client, adds MRR cache via new `adminResolveMRR` helper + `adminMRRCacheTTL`/`adminMRRCacheKeyPrefix` constants
- `server/api/cmd/main.go` - `admin.Get("/stats", ...)` call site passes `redisClient`
- `server/api/internal/handler/admin_kpis_test.go` - `TestAdminGetStatsKPIs` un-skipped with full SQLite+miniredis assertion; `TestServerDrainHidesFromPublic` RED stub preserved for 07-06
- `server/api/internal/handler/admin_test.go` - existing `TestAdminGetStats_NilDB_Returns500` updated for the new 3-arg signature

## Decisions Made

- **System-plan code via correlated subquery**, not a Go round-trip or hardcoded `'free'` — keeps `paid_users` correct even if the system plan code changes.
- **MRR as one cached aggregate** rather than per-user math in Go — minimises round-trips and, behind the 5-min cache, makes the 60s dashboard poll cost one aggregate per window (T-07-05 DoS mitigation).
- **Fail-open cache** — a nil client (used by the nil-DB unit test), a Redis outage, a miss, or a corrupt cached value all degrade to a live `GetMRR`; only a genuine query failure yields `mrr: 0`, and never an endpoint error.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Updated the second AdminGetStats call site in admin_test.go for the new signature**
- **Found during:** Task 2 (handler wiring)
- **Issue:** The plan named only the `cmd/main.go` call site, but `internal/handler/admin_test.go:57` (`TestAdminGetStats_NilDB_Returns500`) also calls `AdminGetStats` and broke the handler test build with "not enough arguments".
- **Fix:** Passed `nil` as the redis client there — the nil-DB path errors at `GetDashboardKPIs` and returns 500 before MRR is touched, so the test's expectation is unchanged.
- **Files modified:** server/api/internal/handler/admin_test.go
- **Verification:** `TestAdminGetStats_NilDB_Returns500` still passes (500 on nil DB); full handler package green.
- **Committed in:** ae0a541 (Task 2 commit)

---

**Total deviations:** 1 auto-fixed (1 blocking)
**Impact on plan:** Necessary to keep the handler package compiling; no scope creep. Both tasks otherwise executed exactly as written.

## Issues Encountered

- **`TestCtxCancelAbortsQuery` (repository package) fails without Docker.** This is a pre-existing testcontainers test (uses `t.Fatalf` on a missing Docker daemon, not `t.Skip`) unrelated to this plan's files — the same Docker-unavailable condition documented in the 07-01 SUMMARY. It is out of scope (not introduced or touched by 07-02). `go test ./internal/repository/ -short` and the full `go test ./internal/handler/` suite both pass cleanly; the Docker-backed run is the orchestrator's post-wave validation step.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- ADMIN-01 KPI bar is live and the 07-01 RED KPI stub is GREEN. `GetDashboardKPIs`/`GetMRR` are available for any later dashboard aggregation work.
- No blockers. The remaining RED stubs (07-03 `TestReadyzLivez`, 07-04 `TestMaintenanceMiddleware`, 07-05 `TestForceCancelWebhookRace`, 07-06 `TestServerDrainHidesFromPublic`, 07-07 `TestSuspendedMiddleware`, 07-08 `TestWebhookReplayIdempotent`) are untouched and ready for their plans.

## Self-Check: PASSED

All five modified files exist on disk; both task commits (`23ba500`, `ae0a541`) are present in git history; `TestAdminGetStatsKPIs` passes and `go build ./...` is green.

---
*Phase: 07-admin-panel-overhaul*
*Completed: 2026-06-01*
