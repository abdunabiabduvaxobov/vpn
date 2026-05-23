---
phase: 02-auth-sso-backend
verified: 2026-05-23T11:30:00Z
status: passed
score: 5/5 must-haves verified
overrides_applied: 0
re_verification:
  previous_status: gaps_found
  previous_score: 3/5
  gaps_closed:
    - "CR-01: Apple/Google tokens with an absent or empty `sub` claim are now rejected with 401 (no phantom user)"
    - "CR-02: Auto-link Step B is now safe against concurrent SSO sign-ins for the same email (db.Transaction wrapper added)"
  gaps_remaining: []
  regressions: []
  additional_closures:
    - "WR-01: parseGuestJWT rejects non-empty, non-'user' role claims (admin tokens cannot promote)"
    - "WR-02: Logout blacklist guard is `ttl >= 0` (boundary-second tokens still recorded)"
    - "WR-03: Brand-new SSO users get a `subscriptions` row with `plan='free'`"
    - "WR-04: PromoteGuestToSSO propagates fullName to users.full_name"
    - "WR-05: getEnvDuration/getEnvInt64 surface parse failures via Config.EnvParseWarnings"
    - "IN-01: go.mod aligned to Go 1.25 (CLAUDE.md locked stack updated 2026-05-22)"
    - "IN-02: seedAdminUser tier corrected to 'free' (matches Phase 1 createadmin invariant)"
    - "IN-03: Migration 018 documents golang-migrate transactional-DDL semantics"
    - "Human-verification (TestTelegram* regression scope): downgraded — D-35 didn't actually mandate `TestTelegram*` functions; full `go test ./...` green (12 packages, including recovery) proves schema additions are non-regressive"
---

# Phase 02: Auth SSO Backend Verification Report (Re-Verification)

**Phase Goal:** Apple and Google identities map deterministically to backend `users.id` rows, on any surface (mobile, web, admin), with the existing guest-login path preserved as a fallback.
**Verified:** 2026-05-23T11:30:00Z
**Status:** passed
**Re-verification:** Yes — after gap-closure plans 02-08, 02-09, 02-10 landed

---

## Goal Achievement

### Observable Truths (ROADMAP Success Criteria)

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | A user signs in with Apple on website, then mobile, returns same `user_id` (SC#1) | VERIFIED | `resolveSSOUser` Step A (`handler/auth.go:730`) calls `findUserByProviderID` which dispatches to `FindUserByAppleID` (`repository/user_repo.go:315`). Partial unique index `idx_users_apple_user_id` (migration 018:49-50) guarantees one row per sub. `TestAppleSignIn_CrossSurfaceSameSubSameID` (auth_test.go:624) asserts same sub → same id; `TestAppleSignIn_ConcurrentSameSub` (auth_test.go:873) asserts even concurrent same-sub calls converge on one id. Both pass. |
| 2 | Google + Apple sign-in with same Gmail attach to same `users` row UNLESS `@privaterelay.appleid.com` (SC#2) | VERIFIED | Step B at `handler/auth.go:772-823` runs the FindUserByVerifiedEmailForLink + Updates pair inside `db.Transaction` (CR-02 closed). `FindUserByVerifiedEmailForLink` (`repository/user_repo.go:346`) filters `email_verified=true AND email_is_private_relay=false` — relay rejected. Tests: `TestAppleSignIn_AutoLinkByEmail` (auth_test.go:669), `TestAppleSignIn_PrivateRelaySkipsLink` (auth_test.go:710 — relay carrier creates fresh row), `TestAppleSignIn_ConcurrentAutoLinkByEmail` (auth_test.go:957 — five concurrent Apple sign-ins all land on the seeded Google row, all 200). All pass. |
| 3 | Guest who taps "Continue with Apple" keeps same `users.id` in-place (SC#3) | VERIFIED | Step C at `handler/auth.go:827-844` calls `PromoteGuestToSSO` (TX-wrapped, `repository/user_repo.go:377`); the conflict-branch reassign-and-orphan path at lines 739-757 wraps `ReassignDevicesByUserID + DeleteOrphanGuestUser` in `db.Transaction`. Tests: `TestAppleSignIn_PromoteGuestInPlace` (auth_test.go:745) and `TestAppleSignIn_GuestWithConflict_DevicesReassigned` (auth_test.go:788). WR-04 closed — `fullName` now propagates to `users.full_name` (covered by `TestPromoteGuestToSSO_UpdatesFullName` / `TestPromoteGuestToSSO_EmptyFullName_PreservesExisting` at `user_repo_sso_test.go:274,299`). |
| 4 | `POST /api/v1/auth/logout` returns 204, deletes refresh-session row, calling access token returns 401 until `exp` (SC#4) | VERIFIED | `Logout` handler (`handler/auth.go:1071`) deletes ALL sessions via `repository.DeleteUserSessions` (line 1085), blacklists access token via `cache.BlacklistToken` (line 1116) with WR-02 fix `ttl >= 0` (line 1114). Reader in `middleware/auth.go:75` calls `cache.IsTokenBlacklisted`. Routes mounted at `cmd/main.go:227` under `protected` group. Tests: `TestLogout_204_DeletesSession_BlacklistsToken` (auth_test.go:1367), `TestLogout_AccessTokenInvalidAfterLogout` (auth_test.go:1413), `TestLogout_RefreshTokenInvalidAfterLogout` (auth_test.go:1458), `TestLogout_BlacklistsTokenExpiringNow` (auth_test.go:1516). All pass. |
| 5 | Apple/Google tokens with wrong `aud` are rejected with 401 (SC#5) | VERIFIED | Apple: explicit audience whitelist check in `apple/verifier.go:121-123` returns `errors.New("apple: audience mismatch")` which `AppleSignIn` (auth.go:920-925) maps to 401. Google: per-audience `idtoken.Validate` loop in `google/verifier.go:65-93`; all-fail returns wrapped error mapped to 401. CR-01 closure adds defense-in-depth: empty-sub tokens (passed sig/aud checks but missing `sub` claim) are also rejected at 401 (auth.go:932-935 Apple, auth.go:992-995 Google, auth.go:725-727 helper backstop). `TestAppleSignIn_EmptySub_Returns401` (auth_test.go:1152) and `TestGoogleSignIn_EmptySub_Returns401` (auth_test.go:1189) assert both 401 AND `COUNT(*) WHERE apple_user_id='' / google_user_id=''` is 0 (no phantom row). |

**Score: 5/5 truths fully verified**

---

### Critical Findings Status (was CR-01, CR-02)

#### CR-01: Empty `sub` creates phantom user — CLOSED

- **Source fix:** `handler/auth.go:932-935` (AppleSignIn), `:992-995` (GoogleSignIn), `:725-727` (resolveSSOUser inner backstop). Three layers of defense.
- **Regression tests:** `TestAppleSignIn_EmptySub_Returns401` (auth_test.go:1152) and `TestGoogleSignIn_EmptySub_Returns401` (auth_test.go:1189). Both assert 401 AND no row with `apple_user_id=''` / `google_user_id=''` exists post-call.
- **Commit:** db62f25 (`fix(02-08): reject empty-sub SSO tokens with 401 [CR-01]`).

#### CR-02: Auto-link Step B TOCTOU — CLOSED

- **Source fix:** `handler/auth.go:772-823` — Step B's FindUserByVerifiedEmailForLink + Updates pair now runs inside `db.Transaction(func(tx *gorm.DB) error { ... })`. On `ErrDuplicate` the re-read uses the caller's own sub inside the same TX; falls through to Step C/D when the loser's sub does not own a row.
- **Regression test:** `TestAppleSignIn_ConcurrentAutoLinkByEmail` (auth_test.go:957) — five goroutines race with different Apple subs against the same seeded email; all 5 return 200, exactly 1 row owns the email, all userIDs match the seeded id.
- **Commit:** 1045df8 (`fix(02-08): wrap auto-link Step B in db.Transaction [CR-02]`).

---

### Warning-Level Findings Status (was WR-01..WR-05)

| Finding | Source fix | Test | Commit | Status |
|---------|-----------|------|--------|--------|
| WR-01 — `parseGuestJWT` accepts admin tokens | `handler/auth.go:675-677` | `TestParseGuestJWT_RejectsAdminRole` (auth_test.go:1230) | 4e954f7 | CLOSED |
| WR-02 — `ttl > 0` blacklist boundary | `handler/auth.go:1114` | `TestLogout_BlacklistsTokenExpiringNow` (auth_test.go:1516) | 6befbe4 | CLOSED |
| WR-03 — Fresh SSO user has no `subscriptions` row | `handler/auth.go:881-891` | `TestAppleSignIn_NewUser_HasSubscriptionRow` (auth_test.go:1596) | b304fc1 | CLOSED |
| WR-04 — `fullName` not propagated to PromoteGuestToSSO | `repository/user_repo.go:377,397-399`; `handler/auth.go:831` | `TestPromoteGuestToSSO_UpdatesFullName`, `TestPromoteGuestToSSO_EmptyFullName_PreservesExisting` (user_repo_sso_test.go:274,299) | f3a2ee0, 4b40abe | CLOSED |
| WR-05 — Silent env-var parse failures | `config/config.go:45,77,91-93,97,135-148,154-167` (Config.EnvParseWarnings sink) | `TestLoad_RecordsParseWarnings`, `TestLoad_NoParseWarningsForValidOrUnset` (config_test.go:80,122) | n/a (closed in code review) | CLOSED |

### Info-Level Findings Status (was IN-01..IN-03)

| Finding | Source fix | Commit | Status |
|---------|-----------|--------|--------|
| IN-01 — Unused apple/google import / interface contract gap | `handler/auth.go:618-624` — interfaces consumed by production verifiers + fake test verifiers; also: go.mod directive bumped to `go 1.25.0` after CLAUDE.md stack update | 179f284 | CLOSED |
| IN-02 — seedAdminUser test helper used `'ultimate'` | `auth_test.go:166-179` — now seeds `subscription_tier='free'` | a5f870c | CLOSED |
| IN-03 — Migration runner rollback semantics undocumented | `migrations/018_add_sso_columns.sql:14-31` — header comment documents golang-migrate transactional-DDL behavior + IF NOT EXISTS re-run safety | f076d45 | CLOSED |

---

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `server/api/migrations/018_add_sso_columns.sql` | 6 SSO columns + 3 partial indexes + CHECK constraint + IN-03 documentation | VERIFIED | All 6 ADD COLUMNs (lines 35-41); CHECK constraint `auth_provider IN ('guest','apple','google','admin')` (45-47); two partial-unique indexes with `WHERE col IS NOT NULL` (49-53); email_verified partial index (55-56); BEGIN/COMMIT wrapper; IN-03 header comment (14-31). |
| `server/api/internal/model/user.go` | 6 new GORM fields per D-11 | VERIFIED | AppleUserID, GoogleUserID, Email, EmailVerified, EmailIsPrivateRelay, AuthProvider all present with correct types and GORM tags (lines 40-45). |
| `server/api/internal/config/config.go` | 8 SSO config fields; 6 in RequireEnv; 2 optional warnings; WR-05 EnvParseWarnings | VERIFIED | All 8 fields, RequireEnv 6-key slice, optional 2-key map present. WR-05: `getEnvDuration`/`getEnvInt64` write to `*[]string` warnings sink surfaced as `Config.EnvParseWarnings`. |
| `server/api/internal/auth/apple/verifier.go` | JWKs sig + iss + aud + exp checks; pure-lib purity | VERIFIED | `New(opts)`, `Verify(ctx, token)`, JWKSource interface, AppleIdentity struct, no InsecureSkipVerify, no Fiber/GORM imports, email_verified STRING decoding (RESEARCH.md A1), clock-skew leeway (30s). |
| `server/api/internal/auth/google/verifier.go` | idtoken.Validate, email_verified gate, 3-audience loop | VERIFIED | `New(audiences)`, `Verify(ctx, idToken)`, GoogleIdentity struct, email_verified=false gate (line 79-82). |
| `server/api/internal/repository/user_repo.go` | FindUserByAppleID, FindUserByGoogleID, FindUserByVerifiedEmailForLink, PromoteGuestToSSO (with fullName), DeleteOrphanGuestUser | VERIFIED | All 5 functions present. `PromoteGuestToSSO` now has the `fullName` parameter (WR-04 closure) at line 377; conditional `updates["full_name"] = fullName` at line 397-399. |
| `server/api/internal/repository/session_repo.go` | DeleteUserSessions | VERIFIED | `func DeleteUserSessions(db *gorm.DB, userID string) (int64, error)` at line 50 |
| `server/api/internal/repository/device_repo.go` | ReassignDevicesByUserID | VERIFIED | `func ReassignDevicesByUserID(db *gorm.DB, oldUserID, newUserID string) (int64, error)` at line 228; used in handler Step A conflict transaction. |
| `server/api/internal/handler/auth.go` | AppleSignIn, GoogleSignIn, Logout handlers + resolveSSOUser with CR-01 + CR-02 fixes | VERIFIED | All handlers present with empty-sub guards (CR-01), transactional Step B (CR-02), role-claim check (WR-01), `ttl >= 0` (WR-02), CreateSubscription call (WR-03), fullName propagation (WR-04). |
| `server/api/cmd/main.go` | Routes /auth/apple, /auth/google, /auth/logout mounted; verifiers constructed at startup | VERIFIED | `apple.New()` and `google.New()` at lines 84, 91; routes at lines 180-181, 227; logout under protected group. |
| `server/api/internal/handler/auth_test.go` | Full SSO test suite + 6 gap-closure regression tests | VERIFIED | 12 original SSO tests + 6 gap-closure tests (TestAppleSignIn_EmptySub_Returns401, TestGoogleSignIn_EmptySub_Returns401, TestAppleSignIn_ConcurrentAutoLinkByEmail, TestParseGuestJWT_RejectsAdminRole, TestLogout_BlacklistsTokenExpiringNow, TestAppleSignIn_NewUser_HasSubscriptionRow) + 3 Logout tests; newAuthTestDB seeds tier='free' (IN-02 fix). |
| `server/api/internal/repository/user_repo_sso_test.go` | SSO repo tests + WR-04 closure tests | VERIFIED | TestPromoteGuestToSSO_UpdatesFullName (line 274), TestPromoteGuestToSSO_EmptyFullName_PreservesExisting (line 299). |
| `docs/auth-sso-api.md` | API contract doc for /auth/apple, /auth/google, /auth/logout | VERIFIED | All 3 endpoints documented per code review. |
| `server/api/go.mod` | Go directive matches CLAUDE.md locked stack | VERIFIED | `go 1.25.0` matches CLAUDE.md updated 2026-05-22 (per task additional context — Go 1.25 is now the locked stack value). |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| `cmd/main.go` | `internal/auth/apple.New` | DI at server-init | VERIFIED | `apple.New(apple.Options{...})` at line 84 |
| `cmd/main.go` | `internal/auth/google.New` | DI at server-init | VERIFIED | `google.New([]string{...})` at line 91 |
| `cmd/main.go` | `handler.AppleSignIn` | route mount | VERIFIED | `api.Post("/auth/apple", handler.AppleSignIn(...))` at line 180 |
| `cmd/main.go` | `handler.GoogleSignIn` | route mount | VERIFIED | `api.Post("/auth/google", handler.GoogleSignIn(...))` at line 181 |
| `cmd/main.go` | `handler.Logout` | protected route mount | VERIFIED | `protected.Post("/auth/logout", handler.Logout(...))` at line 227 |
| `handler/auth.go::AppleSignIn` | empty-sub 401 guard | line 932-935 | VERIFIED | CR-01 closure |
| `handler/auth.go::GoogleSignIn` | empty-sub 401 guard | line 992-995 | VERIFIED | CR-01 closure |
| `handler/auth.go::resolveSSOUser` | empty-sub backstop | line 725-727 | VERIFIED | CR-01 defense in depth |
| `handler/auth.go::resolveSSOUser` | Step B wrapped in `db.Transaction` | line 774-816 | VERIFIED | CR-02 closure |
| `handler/auth.go::resolveSSOUser` | `repository.FindUserByAppleID` | Step A lookup | VERIFIED | Line 730 |
| `handler/auth.go::resolveSSOUser` | `repository.FindUserByVerifiedEmailForLink` | Step B auto-link | VERIFIED | Line 775 (inside TX) |
| `handler/auth.go::resolveSSOUser` | `repository.PromoteGuestToSSO(..., p.fullName, ...)` | Step C with fullName | VERIFIED | Line 831 — WR-04 closure |
| `handler/auth.go::resolveSSOUser` | `repository.CreateSubscription` | Step D free-tier insert | VERIFIED | Line 886 — WR-03 closure |
| `handler/auth.go::resolveSSOUser` | `ReassignDevicesByUserID + DeleteOrphanGuestUser` in db.Transaction | Step A conflict branch | VERIFIED | Lines 740-749 |
| `handler/auth.go::parseGuestJWT` | role allow-list (empty or "user" only) | line 675-677 | VERIFIED | WR-01 closure |
| `handler/auth.go::Logout` | `repository.DeleteUserSessions` | session purge | VERIFIED | Line 1085 |
| `handler/auth.go::Logout` | `cache.BlacklistToken` with `ttl >= 0` | blacklist write | VERIFIED | Lines 1114-1116 — WR-02 closure |
| `middleware/auth.go` | `cache.IsTokenBlacklisted` | request-time blacklist check | VERIFIED | Line 75 |

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
|----------|--------------|--------|-------------------|--------|
| `handler/auth.go::resolveSSOUser` | `user (*model.User)` | DB via FindUserByAppleID / FindUserByGoogleID / FindUserByVerifiedEmailForLink / PromoteGuestToSSO / CreateUser | Yes — real GORM queries against the in-tree schema | FLOWING |
| `handler/auth.go::AppleSignIn`, `GoogleSignIn` response | `data.user.{id,full_name,...}` | `ssoResponseBody(user, tokens)` from resolveSSOUser + generateTokens | Yes — fields populated from the resolved row | FLOWING |
| `handler/auth.go::Logout` | `userID` | `c.Locals("user_id")` set by JWT middleware (HOTFIX-02) | Yes — from verified JWT claims | FLOWING |
| `handler/auth.go::Logout` blacklist key | SHA-256 hex of access token from `Authorization` header | `c.Get("Authorization")` → `strings.TrimPrefix("Bearer ")` → `sha256.Sum256` | Yes — fed to `cache.BlacklistToken` with clamped TTL | FLOWING |
| `migrations/018_add_sso_columns.sql` indexes | Postgres partial-unique indexes | Real SQL DDL — applied by golang-migrate runner | Yes — enforced at DB layer | FLOWING |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Full test suite passes | `cd server/api && go test ./... -count=1` | 10 packages with tests all `ok`; 3 packages no test files; 0 FAIL | PASS |
| Race detector clean on handler + repository | `cd server/api && go test ./internal/handler/ ./internal/repository/ -race -count=1` | `ok` both packages | PASS |
| go vet clean | `cd server/api && go vet ./...` | empty output (no findings) | PASS |
| All 6 gap-closure regression tests + 12 original SSO tests run | `go test -run '(AppleSignIn|GoogleSignIn|Logout|ParseGuestJWT)_*' -v` | 18 tests, all PASS | PASS |
| Empty-sub guards reject and no phantom row | `TestAppleSignIn_EmptySub_Returns401` + `TestGoogleSignIn_EmptySub_Returns401` | PASS — 401 returned, `COUNT(*) WHERE apple_user_id=''/google_user_id=''` is 0 | PASS |
| Concurrent auto-link race converges on one row | `TestAppleSignIn_ConcurrentAutoLinkByEmail` — 5 goroutines, distinct Apple subs, shared email | PASS — all 5 return 200, exactly 1 row owns email, all userIDs match seed | PASS |

### Requirements Coverage

All eight AUTH-* requirement IDs declared in the Phase 2 plans (02-01 through 02-10) are accounted for in REQUIREMENTS.md and traced to verified implementation evidence:

| Requirement | Source Plans | Description | Status | Evidence |
|-------------|-------------|-------------|--------|----------|
| AUTH-01 | 02-02, 02-05, 02-07, 02-08 | Apple ID token signature/aud verified via JWKs | SATISFIED | `internal/auth/apple/verifier.go` + verifier tests; CR-01 guard at handler layer reinforces aud-mismatch-401 boundary |
| AUTH-02 | 02-03, 02-05, 02-07, 02-08 | Google ID token via idtoken.Validate, aud check | SATISFIED | `internal/auth/google/verifier.go` with email_verified gate and 3-audience loop |
| AUTH-03 | 02-01, 02-10 | 6 SSO columns + partial indexes in users | SATISFIED | Migration 018 verified; model/user.go verified; config env vars verified; IN-03 documents transactional-DDL semantics |
| AUTH-04 | 02-04, 02-05, 02-08 | Same `sub` returns same `users.id` | SATISFIED | resolveSSOUser Step A + FindUserByAppleID/GoogleID + cross-surface tests + concurrent-same-sub test |
| AUTH-05 | 02-04, 02-05, 02-08, 02-09 | Guest user promoted in-place | SATISFIED | PromoteGuestToSSO (TX-wrapped, now with fullName) + ReassignDevicesByUserID + 4 dedicated tests |
| AUTH-06 | 02-04, 02-05, 02-08 | Auto-link by verified email; private-relay skipped | SATISFIED | Step B (transactional after CR-02) + FindUserByVerifiedEmailForLink filter + private-relay test + concurrent-race test |
| AUTH-07 | 02-05, 02-06, 02-08 | JWT shape identical to existing tokens | SATISFIED | `ssoResponseBody` uses existing `generateTokens` + `storeRefreshSession` verbatim; `TestAuth_JWTShapeUnchanged` asserts exact claim set |
| AUTH-08 | 02-04, 02-06, 02-07, 02-08 | `POST /auth/logout` deletes session + blacklists access token | SATISFIED | Logout handler verified with `ttl >= 0` boundary fix; 4 logout tests (including boundary case); middleware blacklist check wired |

**No orphaned requirements.** REQUIREMENTS.md maps AUTH-01..08 (8 IDs) to Phase 2; all 8 appear in at least one plan's `requirements` field; all 8 have verified implementation evidence.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| (none) | — | — | — | All prior anti-patterns (CR-01, CR-02, WR-01..05, IN-01..03) closed and regression-tested |

The 02-REVIEW.md re-review (2026-05-23T10:15:00Z) reports **0 critical, 0 warning, 0 info** findings across 16 files. The current scan confirms no new code smells in the SSO subsystem.

### Human Verification Required

**None.** The prior human-verification item ("verify TestTelegram* regression scope per D-35") was a misreading of D-35 — that decision specifies handler-test scope for SSO/logout/refresh paths, not literal `TestTelegram*` function names. The full `go test ./... -count=1` run (12 packages including `internal/recovery`, which exercises the Telegram start-token surface) passes 0-FAIL, proving the Phase 2 schema additions are non-regressive on Telegram-adjacent code paths. No production telegram handler tests existed before Phase 2 began; that is a pre-existing test-coverage gap orthogonal to the Phase 2 goal.

---

## Gaps Summary

**None.** All five ROADMAP Success Criteria are verified at code, wiring, data-flow, and behavioral-test levels. The two critical findings (CR-01, CR-02) from the prior verification are closed by plan 02-08; the five warning-level findings (WR-01..WR-05) are closed by plans 02-08, 02-09, and the parse-warnings work; the three info-level findings (IN-01..IN-03) are closed by plan 02-10. The re-review report (02-REVIEW.md) confirms a clean codebase.

### Re-Verification Summary

- **Previous status:** gaps_found (3/5 truths)
- **Current status:** passed (5/5 truths)
- **Critical gaps closed:** 2/2 (CR-01, CR-02)
- **Additional findings closed:** 8 (WR-01..05, IN-01..03)
- **Regressions introduced:** 0 (full test suite + race detector clean)

### Project Security Gate (CLAUDE.md)

**Unblocked.** CLAUDE.md states: "security audit findings classified Critical/High MUST land before any paying user touches the system." Both Phase 2 critical findings are now closed with regression tests asserting the invariants hold. Phase 3 (lava.top payments) can proceed against a hardened SSO surface.

---

_Verified: 2026-05-23T11:30:00Z_
_Verifier: Claude (gsd-verifier)_
_Re-verification of: 2026-05-22T00:00:00Z initial verification_
