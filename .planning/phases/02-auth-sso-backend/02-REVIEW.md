---
phase: 02-auth-sso-backend
reviewed: 2026-05-23T10:15:00Z
depth: standard
files_reviewed: 16
files_reviewed_list:
  - server/api/cmd/createadmin/main_test.go
  - server/api/go.mod
  - server/api/internal/config/config.go
  - server/api/internal/config/config_test.go
  - server/api/internal/handler/auth.go
  - server/api/internal/handler/auth_test.go
  - server/api/internal/handler/payment_test.go
  - server/api/internal/middleware/admin_test.go
  - server/api/internal/model/user.go
  - server/api/internal/repository/device_repo.go
  - server/api/internal/repository/session_repo.go
  - server/api/internal/repository/subscription_repo_test.go
  - server/api/internal/repository/user_repo.go
  - server/api/internal/repository/user_repo_sso_test.go
  - server/api/internal/repository/user_repo_subscription_test.go
  - server/api/migrations/018_add_sso_columns.sql
findings:
  critical: 0
  warning: 0
  info: 0
  total: 0
status: clean
---

# Phase 2: Code Review Report (Post-Gap-Closure)

**Reviewed:** 2026-05-23T10:15:00Z
**Depth:** standard
**Files Reviewed:** 16
**Status:** clean

## Summary

This is the second pass over Phase 2 (`02-auth-sso-backend`) — a re-review against the
current source state after gap-closure plans **02-08**, **02-09**, and **02-10** landed
on `main`. The prior `02-REVIEW.md` reported nine findings (CR-01, CR-02, WR-01..WR-04,
IN-01..IN-03); this re-review verifies every one of them is closed in the source and
that no new issues were introduced by the fix work.

**All reviewed files meet quality standards. No new issues found.**

### What was checked

Standard-depth language-aware review of all 16 listed files, covering:

- **Correctness:** error propagation in `resolveSSOUser`, transactional boundaries in
  `RefreshToken` and Step B (email auto-link), nil-pointer guards on `*string`
  identity fields (`AppleUserID`, `GoogleUserID`, `Email`, `PasswordHash`), and the
  `goto freshUser` control flow in `GuestLogin`.
- **Security:** empty-`sub` rejection in both SSO handlers, body-email spoof refusal
  (`T-2-EmailBodySpoof`), constant-time `subtle.ConstantTimeCompare` for device-secret
  matching, private-relay exclusion from auto-link, parseGuestJWT role-claim
  enforcement (`WR-01`), and the migration-018 partial unique indexes.
- **Quality:** test-data consistency between `createadmin` seeding and `seedAdminUser`
  test helper, env-var parse warnings surface (`EnvParseWarnings`), migration-runner
  rollback semantics doc, and the explicit blacklist-key-prefix divergence rationale
  in `Logout`.

### Verification of prior findings

| Prior finding | File / line | Closure evidence in current source |
|---|---|---|
| **CR-01** empty sub silently creating phantom rows | `handler/auth.go:932` (Apple), `:992` (Google), `:725` (inner backstop) | All three call sites now reject empty `Sub` before any DB write. Regression tests at `auth_test.go:1152` (`TestAppleSignIn_EmptySub_Returns401`) and `:1189` (`TestGoogleSignIn_EmptySub_Returns401`) assert no row with empty `apple_user_id` / `google_user_id` exists. |
| **CR-02** race in Step B email auto-link | `handler/auth.go:774-816` | The read-then-update pair now runs inside `db.Transaction(...)` with a `findUserByProviderID`-by-this-caller's-sub re-read on `ErrDuplicate`. Regression test at `auth_test.go:957` (`TestAppleSignIn_ConcurrentAutoLinkByEmail`) asserts every concurrent goroutine sees 200 with a single email-owning row. |
| **WR-01** parseGuestJWT accepts admin tokens | `handler/auth.go:675-677` | Non-empty, non-"user" role claims now return `errors.New("guest jwt: non-user role not allowed for promotion")`. Regression test at `auth_test.go:1230` (`TestParseGuestJWT_RejectsAdminRole`) covers admin / operator / user / empty / backwards-compat cases. |
| **WR-02** `ttl > 0` skipped boundary-second blacklist | `handler/auth.go:1114` | Guard is now `ttl >= 0`. Regression test at `auth_test.go:1516` (`TestLogout_BlacklistsTokenExpiringNow`) sleeps past the token's `exp` and asserts the blacklist branch is taken. |
| **WR-03** fresh SSO user missing free subscription row | `handler/auth.go:881-891` | `resolveSSOUser` Step D now inserts a `{Plan: "free", IsActive: true}` row after `CreateUser`; failures are logged at WARN (non-fatal). Regression test at `auth_test.go:1596` (`TestAppleSignIn_NewUser_HasSubscriptionRow`) asserts the row exists. |
| **WR-04** `fullName` not propagated to `PromoteGuestToSSO` | `handler/auth.go:831` (caller) + `repository/user_repo.go:377,397-399` (callee guard) | Handler passes `p.fullName`; repository guards empty value (preserves existing). Regression tests at `user_repo_sso_test.go:274` (`TestPromoteGuestToSSO_UpdatesFullName`) and `:299` (`TestPromoteGuestToSSO_EmptyFullName_PreservesExisting`). |
| **WR-05** silent env-var parse failure | `config/config.go:45,77,91-93,97,135-148,154-167` | `getEnvDuration` / `getEnvInt64` now append parse failures to a `*[]string` warnings sink surfaced via `Config.EnvParseWarnings`. Regression tests at `config_test.go:80` (`TestLoad_RecordsParseWarnings`) and `:122` (`TestLoad_NoParseWarningsForValidOrUnset`). |
| **IN-01** unused `apple.AppleIdentity` import / contract gap | `handler/auth.go:618-624` | `appleVerifier` / `googleVerifier` interfaces are exercised by the production `*apple.Verifier` / `*google.Verifier` via structural typing and by `fakeAppleVerifier` / `fakeGoogleVerifier` in tests. Both `apple` and `google` packages are imported and consumed by the interface signatures. |
| **IN-02** `seedAdminUser` test helper used `'ultimate'` tier | `handler/auth_test.go:166-179` | Now seeds `subscription_tier='free'`, matching the createadmin Phase-1 SC#8 invariant. Comment at `:170-172` documents the divergence rationale. |
| **IN-03** migration-runner rollback semantics undocumented | `migrations/018_add_sso_columns.sql:14-31` | Header comment now documents `golang-migrate`'s implicit per-file transaction wrap and the safe-to-re-run property of `IF NOT EXISTS` indexes + transactional `ADD COLUMN`. |

### Additional checks (Phase 2 scope, defensive re-scan)

- **`Logout` blacklist key prefix:** divergence from CONTEXT.md D-24 is explicitly
  acknowledged at `handler/auth.go:1064-1070`; the writer (`cache.BlacklistToken`)
  and reader (`middleware/auth.go IsTokenBlacklisted`) share one constant, so drift
  is impossible. Acceptable.
- **`GuestLogin` `goto freshUser`:** unusual but well-scoped and heavily commented
  (lines 379-395, 439). Single forward jump within one function; reads cleanly.
  Not a finding.
- **`AdminChangePassword` 8..72 plaintext bound:** matches bcrypt's 72-byte input
  cap (line 162-167). Correct.
- **`subtle.ConstantTimeCompare` on `device.DeviceSecretHash`:** invoked only when
  both sides are non-empty (case at line 380-381). The length-mismatch case returns
  0 from `subtle.ConstantTimeCompare` per documented semantics — handled by the
  other switch arms. Correct.
- **`migration 018` partial unique indexes:** `WHERE col IS NOT NULL` is the
  standard Postgres pattern and matches the in-memory test schema in
  `auth_test.go:120-121` and `user_repo_sso_test.go:53-54`. Correct.
- **`go.mod`:** `go 1.25.0` directive matches the CLAUDE.md updated locked stack
  (per task additional context). Not flagged.

### Test coverage observations (non-findings)

The Phase 2 SSO test suite has a notable depth-of-evidence pattern worth preserving:
several tests not only assert HTTP status codes but also verify the **absence** of
side-effect rows that an incorrect implementation would have created — e.g.
`TestAppleSignIn_EmptySub_Returns401` asserts both 401 and `COUNT(*) ... WHERE
apple_user_id = ''` returning 0. This belt-and-suspenders style is good defensive
practice; preserve it in future SSO test additions.

---

_Reviewed: 2026-05-23T10:15:00Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
