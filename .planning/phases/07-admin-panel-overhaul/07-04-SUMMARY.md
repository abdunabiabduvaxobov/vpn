---
phase: 07-admin-panel-overhaul
plan: 04
subsystem: api
tags: [fiber, gorm, redis, audit-log, rate-limit, suspension, admin, tdd]

# Dependency graph
requires:
  - phase: 07-admin-panel-overhaul
    provides: "migration 024 users.suspended_at/suspended_reason columns + RED TestSuspendedMiddleware stub (plan 07-01)"
provides:
  - "model.User.SuspendedAt/SuspendedReason struct fields (migration 024 columns)"
  - "middleware.SuspendedRequired(db): per-request DB read, 403s suspended users / 401s deleted/unauthed; mounted on the protected group only"
  - "handler.AdminSuspendUser/AdminUnsuspendUser/AdminDisconnectUser: reason-carrying mutations writing reason to audit_log.details (Pitfall 4)"
  - "handler.AdminGetUserAuditLog (by target_id) + AdminListUserSessions read endpoints"
  - "repository.DisconnectConnectionsByUser (Option-B mark-disconnected by user_id), ListSessionsByUser, ListAuditEntriesByTarget"
  - "describeAction labels: suspend_user / unsuspend_user / disconnect_user"
affects: [07-05, 07-10]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Reason-carrying admin mutations write an explicit CreateAuditEntry with Details[reason] because the AuditLog middleware records only method/path/params (Pitfall 4 / T-07-15)"
    - "Per-request suspended_at DB read in SuspendedRequired (mirrors AdminRequired) — correctness over caching a suspended flag (T-07-13)"
    - "Atomic IncrRateLimit throttle gate (1/user/30s → 429) before the mutation (T-07-14)"

key-files:
  created:
    - server/api/internal/middleware/suspended.go
    - server/api/internal/handler/admin_user_controls.go
    - server/api/internal/handler/admin_user_controls_test.go
  modified:
    - server/api/internal/model/user.go
    - server/api/internal/repository/connection_repo.go
    - server/api/internal/repository/session_repo.go
    - server/api/internal/repository/audit_repo.go
    - server/api/internal/middleware/audit.go
    - server/api/internal/middleware/suspended_test.go
    - server/api/cmd/main.go

key-decisions:
  - "SuspendedRequired does its own FindUserByIDAdmin per request rather than caching a suspended flag — a stale entitlement cache cannot bypass the gate (T-07-13)"
  - "force-disconnect is Option-B mark-disconnected by user_id; no Redis tunnel:kill — live tunnels die on the existing stale sweep (LOCKED)"
  - "reason is required (trimmed, 500-char capped) and written to audit_log.details via an explicit CreateAuditEntry; the middleware's method/path row is insufficient (Pitfall 4)"
  - "SuspendedRequired mounted on the protected group only, never the admin group — admins cannot self-lockout and are not suspendable in v1 (T-07-17)"
  - "disconnect throttle skips only when redisClient is nil (test convenience); production always passes a real client"

patterns-established:
  - "writeUserControlAudit helper: explicit reason-carrying audit row keyed by actor (c.Locals user_id) + target_id, best-effort persist"
  - "adminUserSessionResponse DTO: session reads never serialize refresh_token_hash"

requirements-completed: [ADMIN-02]

# Metrics
duration: 10min
completed: 2026-06-01
---

# Phase 7 Plan 04: Per-User Controls (suspend / disconnect / history) Summary

**ADMIN-02 per-user action surface: suspend (revoke sessions + 403 on next request via SuspendedRequired), force-disconnect-all-devices (Option-B mark-disconnected by user_id, throttled 1/30s→429), unsuspend, and audit-log/sessions history reads — all reason-carrying mutations persist the operator's reason to audit_log.details.**

## Performance

- **Duration:** 10 min
- **Started:** 2026-06-01T11:52:29Z
- **Completed:** 2026-06-01T12:02:45Z
- **Tasks:** 2 (both TDD)
- **Files created:** 3
- **Files modified:** 17 (7 production/test for the feature + 10 sibling test DDLs — see Deviations)

## Accomplishments

- **SuspendedRequired middleware** mounted on the protected user group (never admin): a suspended user's still-valid JWT is rejected with 403/"account suspended" on the very next request via a per-request `suspended_at` DB read; deleted/unauthed users get 401. Turns the 07-01 RED `TestSuspendedMiddleware` GREEN.
- **suspend/unsuspend/disconnect handlers** on the audited admin group. Suspend sets `suspended_at`, revokes every refresh session (`DeleteUserSessions`), busts `user:<id>` cache, and writes an explicit audit row carrying the reason (T-07-13 close: sessions + cache + DB-read gate). Disconnect marks all live connections disconnected by `user_id` (Option-B) and is throttled to ≤1/user/30s (429 on the second call within the window, T-07-14).
- **Reason persistence (Pitfall 4 / T-07-15):** every reason-carrying mutation requires a trimmed, 500-char-capped reason and writes it into `audit_log.details` via `repository.CreateAuditEntry` — the AuditLog middleware's method/path-only row is insufficient for a repudiation-proof trail.
- **History reads:** `AdminGetUserAuditLog` (filtered by `target_id`) and `AdminListUserSessions` (never leaks the refresh-token hash). Added `repository.DisconnectConnectionsByUser`, `ListSessionsByUser`, `ListAuditEntriesByTarget`.
- **Readable audit labels:** `describeAction` now resolves `suspend_user`/`unsuspend_user`/`disconnect_user` instead of the `post_admin_users_<uuid>_<action>` fallback.

## Task Commits

Each task was committed atomically (TDD — RED test and GREEN impl landed together per task):

1. **Task 1: model fields + repo helper + SuspendedRequired (RED→GREEN)** - `33c9590` (feat)
2. **Task 2: suspend/unsuspend/disconnect handlers + history + audit reason + throttle** - `0d87d81` (feat)

_Plan metadata commit is made by the orchestrator after the wave._

## Files Created/Modified

- `server/api/internal/middleware/suspended.go` - SuspendedRequired(db): per-request suspended_at gate (403/401)
- `server/api/internal/handler/admin_user_controls.go` - 5 handlers (suspend/unsuspend/disconnect/audit-log/sessions) + writeUserControlAudit
- `server/api/internal/handler/admin_user_controls_test.go` - TestAdminUserControls (suspend+reason+audit, disconnect 429, force-grant regression, history reads)
- `server/api/internal/model/user.go` - SuspendedAt/SuspendedReason struct fields
- `server/api/internal/repository/connection_repo.go` - DisconnectConnectionsByUser
- `server/api/internal/repository/session_repo.go` - ListSessionsByUser
- `server/api/internal/repository/audit_repo.go` - ListAuditEntriesByTarget
- `server/api/internal/middleware/audit.go` - describeAction cases for the 3 new actions
- `server/api/internal/middleware/suspended_test.go` - 07-01 RED stub → GREEN
- `server/api/cmd/main.go` - mount SuspendedRequired on protected group; wire 5 admin routes

## Decisions Made

- **Per-request DB read in SuspendedRequired** rather than caching a suspended flag — the AuthRequired `user:<id>` cache stores only the tier, so a stale cache could otherwise let a just-suspended JWT through. Single warm PK query; correctness over the micro-optimization (mirrors AdminRequired).
- **Option-B force-disconnect** (LOCKED): mark connections disconnected by `user_id`; no Redis tunnel:kill channel. Live VLESS/REALITY tunnels die on the existing ~3-min stale sweep; flipping the timestamp removes them from "active" immediately.
- **Throttle gate runs before the work** and only skips when `redisClient == nil` (test convenience) — production always wires a real client.
- **SuspendedRequired is NOT on the admin group** (T-07-17): admins can't self-lockout and are not suspendable in v1.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Backfilled suspended_at/suspended_reason into sibling test users-table DDLs**
- **Found during:** Task 2 (after adding model.User.SuspendedAt/SuspendedReason in Task 1)
- **Issue:** Adding the two struct fields to `model.User` made GORM reference `suspended_at`/`suspended_reason` on every users-table SELECT/INSERT. The repository, handler, middleware, and createadmin unit tests build their own explicit SQLite `CREATE TABLE users` DDLs (no AutoMigrate, because of PG-specific defaults). Those hardcoded schemas predated migration 024, so `model.User` operations failed with "table users has no column named suspended_at" across ~30 tests in 4 packages.
- **Fix:** Added `suspended_at`/`suspended_reason` columns to every users-table test DDL: `subscription_repo_test.go`, `user_repo_subscription_test.go`, `user_repo_sso_test.go`, `plan_repo_test.go`, `middleware/admin_test.go`, `cmd/createadmin/main_test.go`, and the handler tests `admin_kpis_test.go`, `auth_test.go`, `connection_test.go`, `payment_test.go`, `plans_admin_test.go`, `servers_test.go`, `webhook_lava_test.go`. Also simplified `suspended_test.go`'s `openSuspendedTestDB` to reuse the now-complete `openAdminTestDB` (removed the redundant ALTER TABLE that would have hit a duplicate-column error).
- **Files modified:** 10 sibling test files (listed above) + suspended_test.go
- **Verification:** `go test ./... -short -count=1` green across all packages (only Docker/testcontainers and one redis-pool-dial test remain skipped/failing for lack of Docker — pre-existing, documented in 07-01).
- **Committed in:** `0d87d81` (Task 2 commit)

---

**Total deviations:** 1 auto-fixed (1 blocking)
**Impact on plan:** The fix was a mechanical prerequisite — the model field change is correct and intended; the sibling test schemas simply had to track it. No production behavior changed beyond the plan; no scope creep.

## Issues Encountered

- **Docker unavailable in the execution environment.** Three testcontainers-backed tests (`TestCtxCancelAbortsQuery`, `TestMigrations019_020`, `TestPerfIndexes`) and one redis-pool-dial test require Docker/a live Redis and cannot run here; they skip/fail without it exactly as in 07-01. All SQLite + miniredis unit tests (including this plan's two suites) pass. The Docker-backed run is the orchestrator's post-wave validation step.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- ADMIN-02 mutation + read surface is complete and audited. 07-05 (force-cancel-subscription) can build on the same admin group and the `users.suspended_at` / connection-disconnect primitives; it adds the advisory-lock path this plan deliberately left out.
- 07-10 (admin UI) can wire the suspend/unsuspend/disconnect actions (with confirm dialog for disconnect per T-07-14) and the audit-log/sessions cards.
- No blockers.

## Self-Check: PASSED

All 3 created files exist on disk (`middleware/suspended.go`, `handler/admin_user_controls.go`, `handler/admin_user_controls_test.go`); both task commits (`33c9590`, `0d87d81`) are present in git history. `go test ./internal/middleware/ -run TestSuspendedMiddleware ./internal/handler/ -run TestAdminUserControls -count=1` and `go build ./...` both green.

---
*Phase: 07-admin-panel-overhaul*
*Completed: 2026-06-01*
