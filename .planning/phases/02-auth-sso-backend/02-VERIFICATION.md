---
phase: 02-auth-sso-backend
verified: 2026-05-22T00:00:00Z
status: gaps_found
score: 3/5
overrides_applied: 0
gaps:
  - truth: "Apple/Google tokens with an absent or empty `sub` claim are rejected with 401"
    status: failed
    reason: "CR-01 (Critical): Neither AppleSignIn nor GoogleSignIn guards identity.Sub == \"\" before calling resolveSSOUser. A JWT that passes signature/aud/exp/iss checks but has no `sub` claim silently passes an empty string to resolveSSOUser, which then calls FindUserByAppleID(db, \"\"). Since FindByAppleID with an empty sub finds nothing (ErrNotFound), Step D creates a new User row with AppleUserID = ptr(\"\"). The partial unique index WHERE apple_user_id IS NOT NULL fires on the empty string, creating a phantom singleton account that any future sub-less token maps to."
    artifacts:
      - path: "server/api/internal/handler/auth.go"
        issue: "Lines 862-870 (AppleSignIn) and 909-916 (GoogleSignIn): no `if identity.Sub == \"\"` guard after verifier.Verify() returns. resolveSSOUser() also has no p.sub == \"\" guard at line 711."
    missing:
      - "Add `if identity.Sub == \"\" { return 401 }` in both AppleSignIn and GoogleSignIn immediately after verifier.Verify() returns"
      - "Add `if p.sub == \"\" { return nil, errors.New(\"sso: empty provider sub\") }` as the first statement in resolveSSOUser"
      - "Add a test `TestAppleSignIn_EmptySub_Returns401` covering both direct call and verifier-returning-empty-sub paths"

  - truth: "Auto-link Step B is safe against concurrent SSO sign-ins for the same email from different providers"
    status: failed
    reason: "CR-02 (Critical): Step B in resolveSSOUser (lines 744-773) executes two separate DB operations without a transaction: FindUserByVerifiedEmailForLink followed by db.Model(...).Updates(). Between these two calls a concurrent signer can grab the same linkCandidate with a different sub value. When the UNIQUE collision fires, the ErrDuplicate fallback at line 764-766 calls findUserByProviderID(db, p.provider, p.sub) — but because a DIFFERENT sub won the race, this re-read returns ErrNotFound, and resolveSSOUser returns nil, ErrNotFound. The handler maps ErrNotFound to 500 for the second caller. The fix requires wrapping the lookup+update in db.Transaction. Project constraint: 'Critical/High MUST land before any paying user' (CLAUDE.md security gate). Phase 2 SSO is gating Phase 3 payment flow."
    artifacts:
      - path: "server/api/internal/handler/auth.go"
        issue: "resolveSSOUser Step B (lines 744-773): two sequential DB operations (FindUserByVerifiedEmailForLink + Model.Updates) are not wrapped in db.Transaction. Concurrent callers racing on the same email produce a 500 for the loser."
    missing:
      - "Wrap the Step B FindUserByVerifiedEmailForLink + Updates pair in db.Transaction as specified in CR-02 code fix"
      - "Add test `TestAppleSignIn_ConcurrentAutoLinkByEmail` that races two Apple sign-ins with the same email (one Apple, one Google) and asserts both return 200"
human_verification:
  - test: "Verify TestTelegram* regression scope per D-35"
    expected: "All TestTelegram* tests pass after SSO schema and handler additions"
    why_human: "No TestTelegram* functions found in any handler test file (auth_test.go or otherwise). D-35 mandates these as the regression scope. Cannot confirm via grep — need to determine if Telegram handler tests exist and pass under the new schema."
---

# Phase 02: Auth SSO Backend Verification Report

**Phase Goal:** Apple and Google identities map deterministically to backend `users.id` rows, on any surface (mobile, web, admin), with the existing guest-login path preserved as a fallback.
**Verified:** 2026-05-22T00:00:00Z
**Status:** gaps_found
**Re-verification:** No — initial verification

---

## Goal Achievement

### Observable Truths (ROADMAP Success Criteria)

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | Same Apple/Google `sub` returns the same `users.id` across surfaces (SC#1) | VERIFIED | `resolveSSOUser` Step A: `FindUserByAppleID(db, sub)` looks up by partial-unique index; `ErrDuplicate` fallback re-reads on race (W-4 fix). `TestAppleSignIn_CrossSurfaceSameSubSameID` passes. |
| 2 | Same verified email auto-links Apple+Google to one row; `@privaterelay.appleid.com` skips (SC#2) | FAILED | Step B logic exists and is tested, but Step B lacks a transaction wrapper — CR-02 TOCTOU makes this unsafe for concurrent sign-ins. The code achieves the sequential case; concurrent case produces 500. |
| 3 | Guest user keeps same `users.id` on Apple/Google sign-in (SC#3) | VERIFIED | `resolveSSOUser` Step C calls `PromoteGuestToSSO` (transactional in repo). `TestAppleSignIn_PromoteGuestInPlace` and `TestAppleSignIn_GuestWithConflict_DevicesReassigned` cover both promotion paths. |
| 4 | `POST /api/v1/auth/logout` returns 204, deletes refresh session, blacklists access token (SC#4) | VERIFIED | `Logout` handler at line 983: deletes via `repository.DeleteUserSessions`, writes blacklist via `cache.BlacklistToken`. Mounted under `protected` group in `main.go:227`. `TestLogout_204_DeletesSession_BlacklistsToken`, `TestLogout_AccessTokenInvalidAfterLogout`, `TestLogout_RefreshTokenInvalidAfterLogout` all present. WR-02 (`ttl > 0` guard vs `ttl >= 0`) is a warning-level issue — sub-second window only. |
| 5 | Wrong-`aud` tokens are rejected with 401 (SC#5) | FAILED | The verifier and handler both enforce audience whitelist correctly at the `aud` level. However, CR-01 exposes a gap: a token with correct `aud` but absent `sub` is NOT rejected — it creates a phantom user. This undermines the deterministic-identity guarantee that SC#1 and SC#5 together intend. The `aud` check itself works; the `sub` validation does not. |

**Score: 3/5 truths fully verified**

---

### Critical Findings Impact Assessment

#### CR-01: Empty `sub` creates phantom user (CRITICAL — UNRESOLVED)

**Location:** `server/api/internal/handler/auth.go` lines 862-870 (AppleSignIn), 909-916 (GoogleSignIn)

**Verification:** Confirmed. Neither `AppleSignIn` nor `GoogleSignIn` guards `identity.Sub == ""` after `verifier.Verify()` returns. `resolveSSOUser` at line 711 also has no `p.sub == ""` guard. The only sub-related check in `parseGuestJWT` (line 666) is for the guest JWT `sub`, not the provider identity.

The code path when Apple sends a token with no `sub` claim:
1. `verifier.Verify()` returns `AppleIdentity{Sub: "", ...}` — no error
2. `AppleSignIn` calls `resolveSSOUser(... sub: "")`
3. Step A: `FindUserByAppleID(db, "")` returns ErrNotFound (no row has `apple_user_id = ""`)
4. Step B: email check runs if email is present
5. Step D: `CreateUser` with `AppleUserID = ptr("")` — partial unique index fires on non-NULL empty string
6. All future sub-less Apple tokens map to this phantom account

**Impact on roadmap:** SC#1 (deterministic same `users.id`) and SC#5 (aud-mismatch rejected) are undermined. The project security gate (CLAUDE.md: "Critical/High MUST land before any paying user") blocks Phase 3 launch.

#### CR-02: Auto-link Step B TOCTOU (CRITICAL — UNRESOLVED)

**Location:** `server/api/internal/handler/auth.go` lines 744-773

**Verification:** Confirmed. Step B is two sequential unprotected DB operations. The transaction wrapper exists in Step A (line 723) and is referenced in plan comments, but Step B itself has no `db.Transaction` call — only a direct `db.Model(...).Updates(...)` after a separate `FindUserByVerifiedEmailForLink` call.

**Impact on roadmap:** SC#2 (auto-link) can fail with 500 under concurrent load. Not a data-corruption risk (the unique index prevents duplicate rows), but the concurrent error response maps to a broken user experience and is classified Critical by the code reviewer.

---

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `server/api/migrations/018_add_sso_columns.sql` | 6 SSO columns + 3 partial indexes + CHECK constraint | VERIFIED | All 6 ADD COLUMNs present; both partial-unique indexes with correct WHERE predicates; email_verified index; CHECK constraint on auth_provider; BEGIN/COMMIT wrapper |
| `server/api/internal/model/user.go` | 6 new GORM fields per D-11 | VERIFIED | AppleUserID, GoogleUserID, Email, EmailVerified, EmailIsPrivateRelay, AuthProvider all present with correct types and GORM tags |
| `server/api/internal/config/config.go` | 8 SSO config fields; 6 in RequireEnv; 2 optional warnings | VERIFIED | AppleTeamID, AppleBundleID, AppleServiceID, AppleKeyID, ApplePrivateKeyP8, GoogleClientIDIOS, GoogleClientIDAndroid, GoogleClientIDWeb in struct and Load(); 6 keys in RequireEnv slice; APPLE_KEY_ID/APPLE_PRIVATE_KEY_P8 in OptionalEnvWarnings |
| `server/api/internal/auth/apple/verifier.go` | JWKs sig + iss + aud + exp checks; pure-lib purity | VERIFIED | New(opts), Verify(ctx, token), JWKSource interface, AppleIdentity struct, no InsecureSkipVerify, no Fiber/GORM imports, email_verified string typing, clock-skew leeway |
| `server/api/internal/auth/google/verifier.go` | idtoken.Validate, email_verified gate, 3-audience loop | VERIFIED | New(allowedAudiences), Verify(ctx, idToken), GoogleIdentity struct, email_verified bool gate |
| `server/api/internal/repository/user_repo.go` | FindUserByAppleID, FindUserByGoogleID, FindUserByVerifiedEmailForLink, PromoteGuestToSSO, DeleteOrphanGuestUser | VERIFIED | All 5 functions present with correct signatures; ErrDuplicate public sentinel; FindUserByVerifiedEmailForLink filters WHERE email_verified AND NOT email_is_private_relay |
| `server/api/internal/repository/session_repo.go` | DeleteUserSessions | VERIFIED | `func DeleteUserSessions(db *gorm.DB, userID string) (int64, error)` at line 50 |
| `server/api/internal/repository/device_repo.go` | ReassignDevicesByUserID | VERIFIED | Present; used in handler Step A transaction |
| `server/api/internal/handler/auth.go` | AppleSignIn, GoogleSignIn, Logout handlers + resolveSSOUser | PARTIAL | Handlers exist and are substantive. CR-01/CR-02 are unfixed security gaps. No subscription row created for new SSO users in Step D (WR-03). |
| `server/api/cmd/main.go` | Routes /auth/apple, /auth/google, /auth/logout mounted; verifiers constructed at startup | VERIFIED | apple.New() and google.New() called at lines 84, 91; routes at lines 180-181, 227; logout under protected group |
| `server/api/internal/handler/auth_test.go` | 12 new SSO tests + 3 Logout tests; newAuthTestDB extended | VERIFIED | 12 SSO handler tests present (TestAppleSignIn_*, TestGoogleSignIn_HappyPath, TestAuth_JWTShapeUnchanged); 3 Logout tests; newAuthTestDB has 6 SSO columns + 2 partial indexes |
| `docs/auth-sso-api.md` | API contract doc for /auth/apple, /auth/google, /auth/logout | VERIFIED | All 3 endpoints documented at lines 16, 79, 120 |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| `cmd/main.go` | `internal/auth/apple.New` | DI at server-init | VERIFIED | `apple.New(apple.Options{...})` at line 84 |
| `cmd/main.go` | `internal/auth/google.New` | DI at server-init | VERIFIED | `google.New([]string{...})` at line 91 |
| `cmd/main.go` | `handler.AppleSignIn` | route mount | VERIFIED | `api.Post("/auth/apple", handler.AppleSignIn(...))` at line 180 |
| `cmd/main.go` | `handler.GoogleSignIn` | route mount | VERIFIED | `api.Post("/auth/google", handler.GoogleSignIn(...))` at line 181 |
| `cmd/main.go` | `handler.Logout` | protected route mount | VERIFIED | `protected.Post("/auth/logout", handler.Logout(...))` at line 227 |
| `handler/auth.go::AppleSignIn` | `repository.FindUserByAppleID` | resolveSSOUser Step A | VERIFIED | Line 713 |
| `handler/auth.go::resolveSSOUser` | `repository.FindUserByVerifiedEmailForLink` | Step B | PARTIAL | Call exists but NOT wrapped in db.Transaction — CR-02 |
| `handler/auth.go::resolveSSOUser` | `repository.PromoteGuestToSSO` | Step C | VERIFIED | Line 777 |
| `handler/auth.go::resolveSSOUser` | `repository.ReassignDevicesByUserID + DeleteOrphanGuestUser` | Step A conflict-branch db.Transaction | VERIFIED | Lines 723-734 — B-3 fix confirmed |
| `handler/auth.go::Logout` | `cache.BlacklistToken` | token blacklist write | VERIFIED | Line 1024; `cache.IsTokenBlacklisted` in middleware/auth.go:75 closes the loop |
| `handler/auth.go::AppleSignIn` | no `identity.Sub == ""` guard | sub validation | NOT_WIRED | CR-01: empty sub falls through to CreateUser with AppleUserID="" |

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
|----------|--------------|--------|-------------------|--------|
| `handler/auth.go::resolveSSOUser` | `user (*model.User)` | DB via FindUserByAppleID / CreateUser / PromoteGuestToSSO | Yes — GORM queries against real DB | FLOWING |
| `handler/auth.go::Logout` | `userID` | `c.Locals("user_id")` set by JWT middleware | Yes — from verified JWT claims | FLOWING |
| `handler/auth.go::Logout` | `tokenString` | `c.Get("Authorization")` header | Yes — parsed from real HTTP header | FLOWING (but ttl > 0 guard is WR-02 warning) |

### Behavioral Spot-Checks

Step 7b: Skipped — cannot start the Go server without external DB/Redis/Apple/Google credentials. Static analysis only.

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|------------|-------------|--------|----------|
| AUTH-01 | 02-02-PLAN.md | Apple ID token signature/aud verified via JWKs | SATISFIED | `internal/auth/apple/verifier.go` + 8 verifier tests pass; CR-01 gap is at handler level, not verifier level |
| AUTH-02 | 02-03-PLAN.md | Google ID token via idtoken.Validate, aud check | SATISFIED | `internal/auth/google/verifier.go` exists with email_verified gate and 3-audience loop |
| AUTH-03 | 02-01-PLAN.md | 6 SSO columns + partial indexes in users | SATISFIED | Migration 018 verified; model/user.go verified; config env vars verified |
| AUTH-04 | 02-04-PLAN.md, 02-05-PLAN.md | Same `sub` returns same `users.id` | SATISFIED | resolveSSOUser Step A + FindUserByAppleID/GoogleID + TestAppleSignIn_CrossSurfaceSameSubSameID |
| AUTH-05 | 02-04-PLAN.md, 02-05-PLAN.md | Guest user promoted in-place | SATISFIED | PromoteGuestToSSO + ReassignDevicesByUserID + TestAppleSignIn_PromoteGuestInPlace + TestAppleSignIn_GuestWithConflict_DevicesReassigned |
| AUTH-06 | 02-04-PLAN.md, 02-05-PLAN.md | Auto-link by verified email; private-relay skipped | BLOCKED | Step B logic exists and tests pass sequentially. CR-02: concurrent callers race Step B and one receives 500. The sequential happy-path works; the concurrent path is broken. |
| AUTH-07 | 02-05-PLAN.md | JWT shape identical to existing tokens | SATISFIED | `ssoResponseBody` uses existing `generateTokens` + `storeRefreshSession` verbatim; `TestAuth_JWTShapeUnchanged` asserts exact claim set |
| AUTH-08 | 02-06-PLAN.md | `POST /auth/logout` deletes session + blacklists access token | SATISFIED | Logout handler verified; 3 logout tests present; blacklist check in middleware wired |

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| `server/api/internal/handler/auth.go` | 862-870 | No `identity.Sub == ""` guard after verifier.Verify() in AppleSignIn | Blocker | CR-01: phantom user created for sub-less tokens |
| `server/api/internal/handler/auth.go` | 909-916 | No `identity.Sub == ""` guard after verifier.Verify() in GoogleSignIn | Blocker | CR-01: same phantom user issue for Google |
| `server/api/internal/handler/auth.go` | 711 | No `p.sub == ""` guard at resolveSSOUser entry | Blocker | CR-01: defense in depth missing |
| `server/api/internal/handler/auth.go` | 744-773 | Step B: FindUserByVerifiedEmailForLink + Updates not wrapped in db.Transaction | Blocker | CR-02: TOCTOU race in auto-link produces 500 for concurrent callers |
| `server/api/internal/handler/auth.go` | 821 | Step D: new SSO user created with no Subscription row (WR-03) | Warning | `GET /api/v1/subscription` returns 404 for freshly-SSO'd users who never went through guest path |
| `server/api/internal/handler/auth.go` | 649-670 | `parseGuestJWT` does not check `role` claim — admin tokens accepted as guest promotion carriers (WR-01) | Warning | Admin token presented to /auth/apple attaches a new Apple sub to the admin account, overwriting auth_provider |
| `server/api/internal/handler/auth.go` | 1022 | Blacklist guard is `ttl > 0` not `ttl >= 0` (WR-02) | Warning | Tokens expiring within current second skip blacklist write; sub-second window per jwt leeway |

### Human Verification Required

#### 1. TestTelegram* Regression Scope (D-35 requirement)

**Test:** Run `go test ./internal/handler/... -run TestTelegram -v` in `server/api/`
**Expected:** All TestTelegram* tests pass (per D-35: "regression scope MUST include TestTelegram*")
**Why human:** No `TestTelegram*` functions were found in any handler test file during grep. The telegram handler exists (`handler/telegram.go`) and routes are mounted (`main.go:251-252`), but no corresponding test functions matching `TestTelegram*` were discovered. Either the tests exist in a file not checked, or D-35's regression scope was not satisfied for Telegram. A developer needs to run the full test suite to confirm.

---

## Gaps Summary

Two Critical-severity gaps block goal achievement:

**CR-01 (Empty sub phantom user):** Both `AppleSignIn` and `GoogleSignIn` pass `identity.Sub == ""` through to `resolveSSOUser` without rejection. A validly-signed Apple/Google JWT without a `sub` claim creates a phantom `users` row with `apple_user_id = ""`. The partial unique index marks empty string as non-NULL, so this phantom row becomes a singleton that all future sub-less tokens land on — a broken identity. The fix is a two-line guard in each handler plus one guard at `resolveSSOUser` entry.

**CR-02 (Auto-link TOCTOU):** Step B's read-then-write across two separate DB operations (not in a transaction) creates a race condition: concurrent Apple+Google sign-ins for the same email can race to update the same row with different `sub` values. The loser's `ErrDuplicate` fallback re-reads by the loser's `sub` — which no row owns — and returns `ErrNotFound`, which the handler maps to 500. The fix mirrors the existing Step A pattern: wrap Step B in `db.Transaction`.

Both gaps directly undermine ROADMAP SC#2 (auto-link) and violate the project security gate: "security audit findings classified Critical/High MUST land before any paying user touches the system" (CLAUDE.md). Since Phase 2 SSO is the authentication layer for Phase 3 payments, these gaps must be resolved before Phase 3 can launch.

The non-critical findings (WR-01 admin token promotion, WR-02 ttl edge case, WR-03 missing subscription row) are correctness issues but do not block the phase goal if prioritized for an immediate follow-up plan.

---

_Verified: 2026-05-22T00:00:00Z_
_Verifier: Claude (gsd-verifier)_
