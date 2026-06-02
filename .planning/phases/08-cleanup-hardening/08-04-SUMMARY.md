---
phase: 08-cleanup-hardening
plan: 04
subsystem: auth
tags: [security, refresh-token, session, device-binding, HARD-03, HARD-04]
requires: ["08-01", "08-03"]
provides:
  - "opaque 32-byte refresh tokens (no JWT refresh envelope)"
  - "refresh sessions bound to device_id (hard) + issue_ip (soft)"
  - "migration 025 (sessions.device_id, sessions.issue_ip, clean-break DELETE)"
affects:
  - "every refresh-token issuer (admin/guest/apple/google/link/rotation)"
  - "all live sessions invalidated once at deploy (D-09 clean-break)"
tech-stack:
  added: []
  patterns:
    - "opaque crypto/rand base64url token (mirrors recovery/start_token.go, D-08)"
    - "hard device-bind + soft IP-log on refresh"
key-files:
  created:
    - server/api/migrations/025_session_device_binding.sql
  modified:
    - server/api/internal/model/user.go
    - server/api/internal/handler/auth.go
    - server/api/internal/handler/devices.go
    - server/api/internal/handler/auth_refresh_device_test.go
    - server/api/internal/handler/auth_test.go
    - server/api/internal/handler/admin_user_controls_test.go
    - server/api/internal/repository/user_repo_sso_test.go
decisions:
  - "Refresh token is opaque (43-char base64url of 32 random bytes), not a JWT — JWT_SECRET can no longer mint a valid refresh token (S1-2)"
  - "device_id hard-checked (401 on mismatch); empty bound device_id skips the check so admin/web sessions keep refreshing"
  - "issue_ip soft-checked (warn-only) — mobile roaming would cause false logouts if hard-enforced (D-10)"
  - "rotation carries device_id forward and preserves the ORIGINAL issue_ip as the anomaly baseline"
  - "migration 025 DELETEs all sessions for a single coordinated re-login at deploy (D-09)"
metrics:
  duration: ~35m
  completed: 2026-06-02
  tasks: 2
  files: 8
---

# Phase 8 Plan 04: Opaque Refresh Tokens + Device Binding Summary

Replaced the forgeable JWT refresh-token envelope with an opaque 32-byte crypto/rand base64url string (HARD-03 / S1-2) and bound every refresh session to its issuing device_id (hard 401 on mismatch) and issue_ip (soft, log-only) (HARD-04 / S1-7), with a migration-025 clean-break DELETE that forces exactly one coordinated re-login at deploy.

## What Was Built

### Task 1 — Migration 025 + model.Session fields (commit 374bc6e)
- `server/api/migrations/025_session_device_binding.sql`: adds `sessions.device_id VARCHAR(255)` (hard-checked) and `sessions.issue_ip VARCHAR(45)` (IPv6-safe, soft-checked), `idx_sessions_device_id`, and a `DELETE FROM sessions` clean-break cutover (D-09). Wrapped in BEGIN/COMMIT, idempotent ADD COLUMN/INDEX IF NOT EXISTS.
- `model.Session` gains `DeviceID` and `IssueIP` fields (nullable, additive — pre-existing reads stay safe).

### Task 2 — Opaque mint + device/IP binding (commit 6178e56)
- `generateTokens`: the refresh JWT mint (`refreshClaims` + `SignedString`) is replaced with `base64.RawURLEncoding.EncodeToString(rand 32)` → a 43-char opaque token with no `.`. The access-token JWT is untouched. The `auth.go:201` bcrypt line (owned by 08-03) is untouched.
- `storeRefreshSession` signature extended to `(ctx, db, userID, refreshToken, deviceID, issueIP)`; persists the binding on the new row.
- All **7** call sites updated (plan listed 4; SSO + link handlers were added post-Wave-1):
  - `AdminLogin` (deviceID `""`, IP recorded — admin not device-bound)
  - `RefreshToken` rotation tx (carries `session.DeviceID` + original `session.IssueIP`)
  - `GuestLogin` known-device + fresh-user paths (`req.DeviceID`)
  - `AppleSignIn`, `GoogleSignIn` (`req.DeviceID`)
  - `LinkDevice` in `devices.go` (`req.DeviceID`)
- `RefreshToken` now parses `device_id` from the body: `session.DeviceID != "" && session.DeviceID != req.DeviceID` → 401 (hard); `session.IssueIP != c.IP()` → warn + continue (soft).
- Wave-0 `auth_refresh_device_test.go` flipped from `t.Skip` to a real 4-subtest device-binding test on the in-memory SQLite DB.

## Verification

| Check | Result |
|-------|--------|
| `go test -run 'TestGenerateTokens_RefreshIsOpaque\|TestRefreshToken_DeviceBinding' ./internal/handler/` | ok (both GREEN) |
| `go test ./internal/handler/` | ok |
| `go test ./internal/repository/ ./internal/scheduler/ ./internal/model/` | ok |
| `go test ./cmd/...` | ok (compiles + passes) |
| `grep base64.RawURLEncoding auth.go` | present (line 697) |
| `grep refreshClaims auth.go` | 0 (JWT refresh mint gone) |
| old-arity `storeRefreshSession` calls | 0 |

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] storeRefreshSession had 7 call sites, not 4**
- **Found during:** Task 2
- **Issue:** The plan's `<interfaces>` listed 4 call sites (auth.go:107/288/414/550). After Wave-1 merges the SSO handlers (`AppleSignIn` auth.go:1006, `GoogleSignIn` auth.go:1071) and `LinkDevice` (devices.go:424) also call `storeRefreshSession`. Leaving them on the old 4-arg signature would not compile.
- **Fix:** Updated all 7 call sites to the new 6-arg signature. SSO + link handlers pass `req.DeviceID` + `c.IP()`.
- **Files modified:** server/api/internal/handler/auth.go, server/api/internal/handler/devices.go
- **Commit:** 6178e56

**2. [Rule 3 - Blocking] Two extra SQLite test schemas needed the new columns**
- **Found during:** Task 2
- **Issue:** Adding `DeviceID`/`IssueIP` to `model.Session` makes GORM `SELECT *` (ListSessionsByUser) and `db.Create(&Session{})` reference the new columns. `admin_user_controls_test.go` (calls the sessions-history endpoint → `Find`) and `user_repo_sso_test.go` (calls `db.Create(&model.Session{})`) define their own `sessions` SQLite schemas without these columns, so they would fail with "no such column".
- **Fix:** Added `device_id`/`issue_ip` columns to both test schemas (and the primary `auth_test.go` schema). `scheduler_test.go` was checked and needs no change — `DeleteExpiredSessions` is a `Delete(&Session{})` (table-name + WHERE only, no column selection).
- **Files modified:** server/api/internal/handler/auth_test.go, server/api/internal/handler/admin_user_controls_test.go, server/api/internal/repository/user_repo_sso_test.go
- **Commit:** 6178e56

## Environment Note

`go build ./...` and multi-package `go vet`/`go build` were not runnable in this worktree (blocked at the harness level). Verification was performed with per-package `go test <pkg>` (which compiles the package under test plus its non-test code) across handler, repository, scheduler, model, and cmd — all GREEN — plus static grep checks. The plan's exact `go build ./...` line could not be executed; equivalent compile coverage was achieved via the test runs.

## Known Stubs

None. The refresh path is fully wired end-to-end.

## Threat Flags

None. No new network endpoints, auth paths, or trust-boundary surface beyond what the plan's `<threat_model>` already covers (T-08-03, T-08-04, T-08-04b, T-08-03b — all mitigated/accepted as planned).

## Self-Check: PASSED

- FOUND: server/api/migrations/025_session_device_binding.sql
- FOUND: server/api/internal/model/user.go (DeviceID/IssueIP)
- FOUND: commit 374bc6e (migration + model)
- FOUND: commit 6178e56 (opaque mint + binding)
- GREEN: TestGenerateTokens_RefreshIsOpaque
- GREEN: TestRefreshToken_DeviceBinding
