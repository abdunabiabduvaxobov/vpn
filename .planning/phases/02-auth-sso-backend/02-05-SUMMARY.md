---
phase: 02-auth-sso-backend
plan: 05
subsystem: auth
tags: [apple-sso, google-sso, handlers, jwt, fiber, identity, account-linking, guest-promotion]

# Dependency graph
requires:
  - phase: 02-auth-sso-backend
    provides: "Plan 02-01 — User SSO columns + config env vars (cfg.AppleBundleID/ServiceID, cfg.GoogleClientIDIOS/Android/Web)"
  - phase: 02-auth-sso-backend
    provides: "Plan 02-02 — apple.New / apple.Verify with AppleIdentity{Sub, Email, EmailVerified, IsPrivateRelay}"
  - phase: 02-auth-sso-backend
    provides: "Plan 02-03 — google.New / google.Verify with GoogleIdentity{Sub, Email, EmailVerified, HostedDomain}"
  - phase: 02-auth-sso-backend
    provides: "Plan 02-04 — FindUserByAppleID/GoogleID, FindUserByVerifiedEmailForLink, PromoteGuestToSSO, ReassignDevicesByUserID, ErrDuplicate sentinel, DeleteOrphanGuestUser"
provides:
  - "POST /api/v1/auth/apple — AppleSignIn handler (D-19, D-20)"
  - "POST /api/v1/auth/google — GoogleSignIn handler (D-22)"
  - "resolveSSOUser shared composition helper (find → auto-link → promote → create with race-fallback)"
  - "parseGuestJWT helper — HS256-validates the optional Authorization: Bearer guest token (T-2-GuestJWTSpoof)"
  - "appleVerifier + googleVerifier handler-side interfaces enabling fake-verifier injection in tests"
  - "ssoResponseBody — canonical D-21 response envelope shared by both handlers"
  - "Verifier construction wired ONCE in cmd/main.go at server-init (D-34)"
affects: [02-06-logout, 03-lava-payments, mobile-sso, landing-sso]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Handler-side interfaces (appleVerifier, googleVerifier) duck-type production *apple.Verifier and *google.Verifier; tests pass fake structs. No vendor lock-in at the handler layer."
    - "Composition pattern: handler does verify → resolveSSOUser → generateTokens → storeRefreshSession → respond. Each step has a single canonical failure-to-status mapping (D-27)."
    - "Race-fallback via `errors.Is(err, repository.ErrDuplicate)` re-read in TWO places: CreateUser AND PromoteGuestToSSO. Together with the partial-unique index this guarantees every concurrent caller for the same sub gets HTTP 200 (W-4)."
    - "D-06 reassign-and-orphan branch implemented IN-LINE in resolveSSOUser Step A using `db.Transaction(ReassignDevicesByUserID + DeleteOrphanGuestUser)` — not deferred (B-3 fix)."
    - "T-2-EmailBodySpoof defense in handler: body Email is explicitly `_ = req.Email`'d with a security comment; only the verifier-derived `identity.Email` reaches `FindUserByVerifiedEmailForLink`."

key-files:
  created: []
  modified:
    - "server/api/internal/handler/auth.go (+356 lines) — appleVerifier+googleVerifier interfaces, appleSignInRequest+googleSignInRequest structs, parseGuestJWT, ssoResolveParams, findUserByProviderID, resolveSSOUser, AppleSignIn, GoogleSignIn, ssoResponseBody. All appended; existing functions untouched."
    - "server/api/internal/handler/auth_test.go (+563 lines) — fakeAppleVerifier, fakeGoogleVerifier, newAppleApp, newGoogleApp, mintGuestJWT, ssoResponse, 12 new test functions covering AUTH-01/02/04/05/06/07 + threat model rows. Includes B-3 conflict-branch reassignment test and W-4 concurrent-200-assertion test. SetMaxOpenConns(1) clamp in the concurrent test so SQLite :memory: connection-fanout doesn't masquerade as a race."
    - "server/api/cmd/main.go (+25 lines) — apple.New + google.New constructed once at startup with audience whitelists from cfg; api.Post for /auth/apple and /auth/google registered alongside /auth/guest. Logout NOT mounted (plan 02-06 owns)."

key-decisions:
  - "Handler-side interfaces (appleVerifier / googleVerifier) defined in the handler package rather than reusing types from the auth packages directly — keeps fake-verifier injection trivial and avoids importing the test seams from production-only packages."
  - "resolveSSOUser is the shared composition for both providers (instead of duplicating logic across AppleSignIn and GoogleSignIn). Provider is a string parameter; ssoResolveParams carries everything else. This keeps the security-critical race + auto-link + guest-promote logic in one place — exactly one diff site to review."
  - "D-06 conflict branch implemented in-line (B-3 fix). The plan was explicit that this is NOT deferred. ReassignDevicesByUserID + DeleteOrphanGuestUser are wrapped in `db.Transaction(...)` inside resolveSSOUser Step A. Test `TestAppleSignIn_GuestWithConflict_DevicesReassigned` proves it."
  - "B-2 fix: race-fallback uses `errors.Is(err, repository.ErrDuplicate)` exclusively. The package-private `repository.isDuplicateError` helper is never referenced from the handler package — verified by grep against non-comment lines."
  - "Concurrent test SQLite clamp (SetMaxOpenConns(1)): :memory: SQLite is per-connection, so without a single-connection pool the goroutines hit different empty databases and the partial-unique index never fires. Clamping ensures the test exercises the ErrDuplicate-fallback path the W-4 fix exists to protect."
  - "appleSignInRequest carries `authorizationCode` as a field but the handler does NOT exchange it (D-18). The field is documented inline and discarded; future Apple code-exchange work plugs in without an API-contract change."

patterns-established:
  - "Handler-package SSO composition: parse → verifier → guest-JWT-parse → resolveSSOUser → generateTokens → storeRefreshSession → ssoResponseBody. Future SSO providers slot in by extending the provider switch in resolveSSOUser + findUserByProviderID and adding a new handler that delegates to the same shared helper."
  - "Test pattern: fake verifier returning canned AppleIdentity/GoogleIdentity + fresh per-test SQLite + direct db.Exec seeds. Avoids the network, mocks the only nondeterministic input. Pattern proven across 12 tests."
  - "Race-test pattern (W-4): N goroutines fire the same request, mutex-protected result slice, assert ALL responses are 200 AND all user.id values match AND exactly one DB row exists. Reusable for any FindOrCreate handler."

requirements-completed: [AUTH-01, AUTH-02, AUTH-04, AUTH-05, AUTH-06, AUTH-07]

# Metrics
duration: ~18 min
completed: 2026-05-22
---

# Phase 02 Plan 05: SSO Handlers (Apple + Google) Summary

**POST /api/v1/auth/apple and /api/v1/auth/google land at the handler layer — composing the Apple/Google JWT verifiers (plans 02/03) + SSO repository functions (plan 04) + existing `generateTokens`/`storeRefreshSession` (AUTH-07 unchanged) — with the D-06 guest-conflict reassign-and-orphan branch implemented in-line (B-3 fix) and the W-4 concurrent-200 invariant enforced by an explicit race-fallback re-read.**

## Performance

- **Duration:** ~18 min
- **Started:** 2026-05-22T~08:18Z
- **Completed:** 2026-05-22T~08:36Z
- **Tasks:** 3 (1 test red + 1 feat green + 1 wiring)
- **Files modified:** 3 (auth.go, auth_test.go, cmd/main.go)

## Accomplishments

- **AUTH-01 / AUTH-02 handler layer landed:** valid Apple/Google identity tokens now produce a backend JWT pair via POST /api/v1/auth/apple and /api/v1/auth/google. Wrong-audience tokens map to 401 with a generic body (HOTFIX-04 contract). Cross-surface continuity: the SAME `users.id` is returned for the same Apple `sub` from any device.
- **AUTH-04 / ROADMAP SC#1 verified:** `TestAppleSignIn_CrossSurfaceSameSubSameID` exercises the second-sign-in path and asserts a single row in `users WHERE apple_user_id='AS-cross'`.
- **AUTH-05 / ROADMAP SC#3 verified — TWO branches:**
  - **In-place promotion:** `TestAppleSignIn_PromoteGuestInPlace` mints a guest JWT, signs in with Apple, asserts the returned `user.id` is the original guest id and the row's `auth_provider` is now `apple`.
  - **D-06 conflict branch (B-3 fix):** `TestAppleSignIn_GuestWithConflict_DevicesReassigned` — when an existing row already owns the Apple sub, the guest's device rows are moved to the existing owner and the guest user row is deleted, both inside a single `db.Transaction`. Test reads device ownership AND verifies the guest row count is 0.
- **AUTH-06 / ROADMAP SC#2 verified — auto-link + private-relay exception:**
  - `TestAppleSignIn_AutoLinkByEmail` — Apple sign-in with email matching an existing google-bound row → response carries the seeded user.id AND `apple_user_id` is now populated on that row.
  - `TestAppleSignIn_PrivateRelaySkipsLink` — Apple sign-in with `IsPrivateRelay=true` → returns a NEW user id, the seeded verified-email row is untouched (T-2-RelaySkip).
- **AUTH-07 shape regression locked:** `TestAuth_JWTShapeUnchanged` decodes the access token after an Apple sign-in and asserts the claims set is exactly `{sub, tier, role, name, iat, exp}` — no extra `plan_id`, no missing claims.
- **W-4 concurrent-200 invariant proved under -race:** `TestAppleSignIn_ConcurrentSameSub` spawns 5 goroutines hitting the same sub, asserts every response is HTTP 200 (not 500), every returned user.id matches, and the partial-unique index enforces exactly one row in the database.
- **Threat mitigations verified in code + tests:**
  - T-2-AppleAud / T-2-GoogleAud → `TestAppleSignIn_AudienceMismatch_Returns401` confirms verifier errors map to 401 with the canonical body "invalid identity token"
  - T-2-EmailBodySpoof → `TestAppleSignIn_BodyEmailNeverUsedForAutoLink` — body-spoofed email does NOT auto-link to the victim row; the handler ignores `req.Email` for any trust-bearing lookup (explicit `_ = req.Email` + security comment)
  - T-2-GuestJWTSpoof → `TestAppleSignIn_InvalidGuestJWT_Returns403` — tampered guest JWT returns 403, not silent fall-through
  - T-2-RaceLink → covered by `TestAppleSignIn_ConcurrentSameSub` AND the explicit `errors.Is(err, repository.ErrDuplicate)` re-read in resolveSSOUser
  - T-2-RelaySkip → covered by `TestAppleSignIn_PrivateRelaySkipsLink` and by code-gated `if p.email != "" && p.emailVerified && !p.isPrivateRelay`
  - T-2-Promotion → D-06 conflict branch is `db.Transaction(...)`-wrapped; partial state is impossible

## Task Commits

1. **Task 1: Twelve RED-phase tests + fake verifiers + helpers** — `093b62b` (test) — package fails to compile as designed (`undefined: AppleSignIn / GoogleSignIn`); Task 2 makes it green.
2. **Task 2: AppleSignIn + GoogleSignIn + resolveSSOUser + parseGuestJWT + ssoResponseBody implementation** — `d35fd29` (feat) — also clamps the concurrent test SQLite to `SetMaxOpenConns(1)` (auto-fixed during execution; documented under Deviations).
3. **Task 3: Verifier construction + route registration in cmd/main.go** — `fc9ac4f` (feat) — apple.New + google.New constructed once at startup with audience whitelists from cfg; /auth/apple and /auth/google mounted as public routes.

## Files Created/Modified

- `server/api/internal/handler/auth.go` (modified, +356 lines, ends at ~line 970) — appleVerifier+googleVerifier interfaces; appleSignInRequest+googleSignInRequest; parseGuestJWT; ssoResolveParams; findUserByProviderID; resolveSSOUser (4-step composition with race-fallback + D-06 conflict branch); AppleSignIn; GoogleSignIn; ssoResponseBody. All existing functions untouched (`grep -c '^func generateTokens' ` returns 1, not 2 — reuse, not duplication).
- `server/api/internal/handler/auth_test.go` (modified, +563 lines, ends at ~line 1026) — 12 new test functions + 2 fake verifier types + 2 newApp helpers + mintGuestJWT helper + ssoResponse decoder type. New imports: context, errors, io, sync, time, apple, google, jwt/v5, uuid.
- `server/api/cmd/main.go` (modified, +25 lines) — apple+google package imports; verifier construction block after `stripe.Key = cfg.StripeKey`; two `api.Post` lines for /auth/apple and /auth/google in the public route block.

## Decisions Made

- **resolveSSOUser is the single composition site for both providers.** The alternative — duplicating the 4-step pipeline in AppleSignIn and GoogleSignIn — would have produced two diff sites for every future security-critical change (the race-fallback, the conflict branch, the auto-link guard). One site = one place to audit. Provider is dispatched via a string field on ssoResolveParams + a small `findUserByProviderID` switch.
- **B-3 D-06 conflict branch is in-line.** The plan was explicit that this is NOT deferred. resolveSSOUser Step A contains the literal `db.Transaction(func(tx *gorm.DB) error { ... ReassignDevicesByUserID ... DeleteOrphanGuestUser ... return nil })`. Test `TestAppleSignIn_GuestWithConflict_DevicesReassigned` asserts BOTH the device-reassignment AND the orphan-row-deletion.
- **B-2 fix:** the auto-link path and the create path both use `errors.Is(err, repository.ErrDuplicate)` exclusively. The private `repository.isDuplicateError` helper is not referenced from `internal/handler/...` — verified by `grep -vE '^\s*//' ... | grep -c isDuplicateError` returning 0 (the one match in the file is a comment explaining WHY the package-private helper is not reachable).
- **Email-body trust boundary documented in code.** `_ = req.Email` followed by a security comment block makes the intent unmistakable to future readers — the body email is captured by the request struct so client typings stay stable, but it is intentionally never reachable from the auto-link lookup.
- **Concurrent test SQLite clamp** (`sqlDB.SetMaxOpenConns(1)` in `TestAppleSignIn_ConcurrentSameSub`): SQLite `:memory:` is per-connection, so a multi-connection pool gives each goroutine a different empty database. Without the clamp the test fails with "5/5 goroutines got 500" not because the handler is broken but because the partial-unique index can't fire across separate databases. The clamp forces the goroutines through a single shared connection where the index does fire, exercising the W-4 ErrDuplicate-fallback path the test exists to verify.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 — Blocking] Concurrent test fails on SQLite `:memory:` connection fanout**
- **Found during:** Task 2 (running `go test ./internal/handler/... -run TestAppleSignIn_ConcurrentSameSub`)
- **Issue:** All 5 goroutines returned HTTP 500 because GORM's connection pool was opening multiple connections to `:memory:`, and each `:memory:` connection is a separate empty database — so the partial-unique index never matched, every goroutine's CreateUser succeeded against its own private DB, the post-loop `SELECT COUNT(*) WHERE apple_user_id='AS-1'` saw 0 rows (the seeded data was on a different connection).
- **Fix:** In `TestAppleSignIn_ConcurrentSameSub` only, call `db.DB()` to get the underlying `*sql.DB` and `SetMaxOpenConns(1)`. All goroutines now serialize through a single shared SQLite connection. The handler's ErrDuplicate-fallback path is now what's being tested — exactly the W-4 invariant the test was added to verify.
- **Files modified:** server/api/internal/handler/auth_test.go (test-only, not the handler).
- **Commit:** Bundled with `d35fd29` (Task 2 feat commit) since the test was added in the previous commit (Task 1) and the clamp is a test-infrastructure fix needed for the new handler to be properly verified.
- **Verification:** `go test ./internal/handler/... -count=1 -race -run TestAppleSignIn_ConcurrentSameSub -v` passes; the post-loop assertion `users WHERE apple_user_id='AS-1' want 1, got 1` now succeeds; all 5 goroutines return 200.

**2. [Documentation, not a fix] Plan-staging instruction divergence — Task 2 staged 2 files instead of 1**
- **Found during:** Task 2 commit step
- **Issue:** Plan said "Stage exactly `internal/handler/auth.go`" but the SQLite clamp fix (Deviation #1) lives in `internal/handler/auth_test.go`. Following the plan literally would commit broken tests (concurrent test fails) with the new handler, then commit the test fix separately — confusing the bisect history.
- **Fix:** Staged `auth.go` + `auth_test.go` together in the Task 2 commit. Same approach plan 02-03 documented as Deviation #3 (bundling the test-side adjustment with the implementation commit when both are needed for the implementation to be properly verified).
- **Files modified:** Two files in `d35fd29` instead of one. Commit message documents the test-infra clamp.
- **Verification:** Plan 02-03 set the precedent; the bundle keeps the bisect clean (every commit either passes its own tests cleanly or — for Task 1 RED — fails as designed).

**3. [Verification-script nit, not a deviation in implementation] Acceptance grep for `req.Email` near `FindUserByVerifiedEmailForLink` matches a comment, not code**
- **Found during:** Task 2 (acceptance criterion run)
- **Issue:** `grep -A2 'FindUserByVerifiedEmailForLink' server/api/internal/handler/auth.go | grep -c 'req.Email'` returned 1, criterion expected 0. Same false-positive class as plan 02-02 / 02-03 (verification regex matches doc-comments).
- **Cause:** The security comment block in AppleSignIn says "The body `Email` is intentionally ignored ... it is never passed to FindUserByVerifiedEmailForLink" with `_ = req.Email` on the next line — the grep window picks up that line.
- **Substantive check:** `grep -B1 -A4 'FindUserByVerifiedEmailForLink(' ... | grep -v '^\s*//' | grep -c 'req\.Email'` returns 0 — zero non-comment occurrences. T-2-EmailBodySpoof mitigation is fully in code.
- **Fix:** None needed in code (the security comment is intentional and load-bearing for future readers); documented here so future plans can refine the verification-script regex.
- **Files modified:** None.
- **Commit:** N/A.

---

**Total deviations:** 3 documented (1 auto-fix in test infra + 1 staging-instruction divergence + 1 verification-script nit)
**Impact on plan:** All three follow precedents set by earlier Wave-1 plans (02-02 / 02-03). Zero security weakening, all 12 new tests pass under `-race`, full project test suite green, AUTH-01 through AUTH-07 + all five threat-model rows from the plan's `<threat_model>` covered.

## Issues Encountered

- **SQLite `:memory:` connection fanout under concurrent goroutines** — documented above (Deviation #1). The fix is a one-line `SetMaxOpenConns(1)` clamp inside the single test that exercises concurrent access. Plan 02-04's repository tests already work with `:memory:` because they don't go concurrent; this is the first phase-2 test that DOES need it.
- **Worktree base required a soft reset** — the worktree HEAD was on commit `6a3da00` (phase-01 only), but the plan's stated base was `e33440313e4608108a023496ef87094f7e8b0fc9` (which has Wave 1 + 2). Reset HEAD to the base via `git reset --soft`, then `git checkout HEAD -- .` to bring the working tree in line. Documented in the initial worktree_branch_check step. Net: zero impact on the three plan commits — they all branch cleanly from the stated base.

## Verification Run

Full plan `<verification>` section ran from the worktree:

```
$ cd server/api && go test ./... -count=1 -race
?       vpnapp/server/api/cmd                          [no test files]
ok      vpnapp/server/api/cmd/createadmin              5.467s
ok      vpnapp/server/api/internal/auth/apple          4.524s
ok      vpnapp/server/api/internal/auth/google         2.307s
?       vpnapp/server/api/internal/bot                 [no test files]
ok      vpnapp/server/api/internal/cache              11.544s
ok      vpnapp/server/api/internal/config              3.453s
ok      vpnapp/server/api/internal/handler             9.749s
ok      vpnapp/server/api/internal/middleware          6.399s
?       vpnapp/server/api/internal/model               [no test files]
ok      vpnapp/server/api/internal/recovery            4.712s
ok      vpnapp/server/api/internal/repository          3.693s
ok      vpnapp/server/api/internal/scheduler           3.080s
```

Zero FAIL. Zero race-detector warnings. Per-row VALIDATION.md status:

| Validation row | Status | Test |
|----------------|--------|------|
| AUTH-04 cross-surface same sub → same id | ✅ green | TestAppleSignIn_CrossSurfaceSameSubSameID |
| AUTH-05 promote-in-place | ✅ green | TestAppleSignIn_PromoteGuestInPlace |
| AUTH-05 conflict branch (B-3) | ✅ green | TestAppleSignIn_GuestWithConflict_DevicesReassigned |
| AUTH-06 auto-link by verified email | ✅ green | TestAppleSignIn_AutoLinkByEmail |
| AUTH-06 private-relay skipped | ✅ green | TestAppleSignIn_PrivateRelaySkipsLink |
| AUTH-07 JWT shape regression | ✅ green | TestAuth_JWTShapeUnchanged |
| Concurrency same-sub → all 200, one row (W-4) | ✅ green under -race | TestAppleSignIn_ConcurrentSameSub |
| Audience mismatch → 401 | ✅ green | TestAppleSignIn_AudienceMismatch_Returns401 |
| Invalid guest JWT → 403 | ✅ green | TestAppleSignIn_InvalidGuestJWT_Returns403 |
| Body-email never auto-links (T-2-EmailBodySpoof) | ✅ green | TestAppleSignIn_BodyEmailNeverUsedForAutoLink |
| Google happy path | ✅ green | TestGoogleSignIn_HappyPath |
| Backcompat (TestGuestLogin / TestAdminLogin / TestRefreshToken / TestLinkDevice / TestTelegram) | ✅ green | unchanged suite |

Twelve new tests + complete handler regression suite all green.

## Manual-Only Verification Deferred

Per VALIDATION.md "Manual-Only Verifications":
- **Real Apple `email_verified` claim type capture** — still required before `/gsd-verify-work`. The verifier's STRING-typed handling (plan 02-02) is the load-bearing path; this plan's tests use the verifier's output directly so a one-line spike in dev is sufficient.
- **Real Google idToken `hd` claim capture** — still required before `/gsd-verify-work`. Not blocking — this plan's handler does not branch on HostedDomain (surfaced for future Workspace-tenant work).

## Next Plan Readiness

- **Plan 02-06 (Logout endpoint) unblocked.** This plan deliberately did NOT mount `/auth/logout` — the route registration block in cmd/main.go has a comment marker pointing at plan 02-06 ("Logout (AUTH-08) is owned by plan 02-06 and will mount under the protected group"). The repository helper `DeleteUserSessions` from plan 02-04 is ready to be composed into the Logout handler.
- **Plan 02-07 (API contract doc) already shipped at base** — `docs/auth-sso-api.md` exists; this plan's response bodies (D-21 shape with `data.{access_token, refresh_token, expires_in, user{id, auth_provider, email, full_name, subscription_tier}}`) match it.
- **Mobile + landing SSO clients (Phase 5/4)** can code against these endpoints — the contract is locked, the response shape is stable, all error codes are documented in code comments + tests.
- **No carryover blockers.**

## Self-Check: PASSED

- File `server/api/internal/handler/auth.go` exists and contains AppleSignIn/GoogleSignIn: FOUND (2/2 function declarations)
- File `server/api/internal/handler/auth_test.go` exists and contains 12 new test functions: FOUND (12/12)
- File `server/api/cmd/main.go` exists and contains apple.New + google.New + /auth/apple + /auth/google: FOUND (all 4)
- Commit `093b62b` exists: FOUND (`git log --oneline | grep 093b62b`)
- Commit `d35fd29` exists: FOUND
- Commit `fc9ac4f` exists: FOUND
- Full project test suite green under -race: FOUND (12 packages PASS, 0 FAIL)
- AUTH-01/02/04/05/06/07 acceptance criteria met: FOUND (all 12 SSO tests + regression suite green)
- B-3 conflict-branch device reassignment + orphan in `db.Transaction`: FOUND (test asserts both device.user_id and guest user count=0)
- W-4 concurrent-200 invariant proved under -race: FOUND (5 goroutines, all 200, all same user.id, 1 row)
- B-2 fix (errors.Is sentinel only, no `isDuplicateError` reference outside comments): FOUND (`grep -vE '^\s*//' | grep -c isDuplicateError` returns 0)

---
*Phase: 02-auth-sso-backend*
*Plan: 05 (Apple + Google SSO handlers)*
*Completed: 2026-05-22*
