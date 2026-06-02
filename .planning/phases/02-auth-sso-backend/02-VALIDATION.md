---
phase: 2
slug: auth-sso-backend
status: verified
nyquist_compliant: true
wave_0_complete: true
created: 2026-05-22
validated: 2026-06-03
revised: 2026-05-22 (W-2 / B-1 — TestTelegram added to Backcompat row, TestVerify_JWKsColdStart confirmed in plan 02)
---

# Phase 2 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Sourced from `02-RESEARCH.md` §Validation Architecture (Nyquist 8-dimension coverage).

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go `testing` (stdlib) + Fiber `app.Test(httptest.NewRequest(...))` + GORM SQLite `:memory:` + `miniredis/v2` |
| **Config file** | none — `server/api/go.mod` is sole config |
| **Quick run command** | `cd server/api && go test ./internal/auth/... ./internal/handler/... -run "Apple\|Google\|Logout\|SSO" -count=1` |
| **Full suite command** | `cd server/api && go test ./... -count=1 -race` |
| **Estimated runtime** | ~30s quick / ~90s full (race-detector pass) |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/auth/... ./internal/handler/... -run <relevant> -count=1` (single-package, <10s)
- **After every plan wave:** Run `cd server/api && go test ./internal/auth/... ./internal/handler/... ./internal/repository/... ./internal/config/... -count=1 -race`
- **Before `/gsd-verify-work`:** `cd server/api && go test ./... -count=1 -race` (full suite green, no skips)
- **Max feedback latency:** 10 seconds per task; 90 seconds per wave; 90 seconds for full suite

---

## Per-Task Verification Map

> Tasks are not yet defined — the planner produces them. Below is the **requirement-level** verification map; the planner MUST map each task to one or more of these rows (or to a Wave 0 setup row) via `<acceptance_criteria>` in PLAN.md.

| Req ID | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|--------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| AUTH-01 | 1 | Apple sig verified via JWKs; aud whitelist accepts BUNDLE or SERVICE | T-2-AppleAud | Reject wrong-aud Apple token with 401 | unit (verifier) | `go test ./internal/auth/apple/... -run TestVerify_HappyPath` | ❌ W0 | ✅ green |
| AUTH-01 | 1 | Apple wrong-aud rejection | T-2-AppleAud | Wrong audience → 401, no JWT minted | unit + integration | `go test ./internal/auth/apple/... -run TestVerify_AudienceMismatch` | ❌ W0 | ✅ green |
| AUTH-02 | 1 | Google verified via `idtoken.Validate`; iterates iOS+Android+Web audiences | T-2-GoogleAud | First-success audience match, else 401 | unit (verifier) | `go test ./internal/auth/google/... -run TestVerify_HappyPath` | ❌ W0 | ✅ green |
| AUTH-02 | 1 | Google `email_verified=false` rejected | T-2-EmailSpoof | Reject any Google identity with unverified email | unit (verifier) | `go test ./internal/auth/google/... -run TestVerify_EmailNotVerified` | ❌ W0 | ✅ green |
| AUTH-03 | 1 | Migration `018_add_sso_columns.sql` adds six columns + partial unique indexes | T-2-Schema | Idempotent migration, `IF NOT EXISTS` on indexes | manual smoke + integration | `psql -f migrations/018_add_sso_columns.sql && psql -c '\d users'`; columns exercised transitively by every passing SSO test (apple_user_id/google_user_id/email/etc.) | ✅ via SSO tests | 🔶 schema smoke manual |
| AUTH-04 | 2 | Same Apple sub returns same `users.id` on second sign-in (cross-surface) | T-2-RaceLink | Partial unique index enforces single row per provider sub | integration (handler) | `go test ./internal/handler -run TestAppleSignIn_CrossSurfaceSameSubSameID` | ❌ W0 | ✅ green |
| AUTH-05 | 2 | Guest with valid guest JWT signs in with Apple → `users.id` unchanged | T-2-Promotion | TX-wrapped UPDATE; no orphan device row | integration (handler) | `go test ./internal/handler -run TestAppleSignIn_PromoteGuestInPlace` | ❌ W0 | ✅ green |
| AUTH-05 | 2 | Guest with conflicting Apple sub → devices reassigned, guest row orphaned (D-06 reassign branch — **B-3 fix**) | T-2-Promotion | `db.Transaction(ReassignDevicesByUserID + DeleteOrphanGuestUser)` | integration (handler) | `go test ./internal/handler -run TestAppleSignIn_GuestWithConflict_DevicesReassigned` | ❌ W0 | ✅ green |
| AUTH-06 | 2 | Apple + Google with same verified email auto-link to same row | T-2-EmailLink | Only when `email_verified=TRUE AND email_is_private_relay=FALSE` | integration (handler) | `go test ./internal/handler -run TestAppleSignIn_AutoLinkByEmail` | ❌ W0 | ✅ green |
| AUTH-06 | 2 | `@privaterelay.appleid.com` does NOT auto-link | T-2-RelaySkip | Relay address always creates new row | integration (handler) | `go test ./internal/handler -run TestAppleSignIn_PrivateRelaySkipsLink` | ❌ W0 | ✅ green |
| AUTH-07 | 2 | JWT shape (sub, tier, role, name, iat, exp) identical to GuestLogin / AdminLogin output | — | HS256, 5min access / 30day refresh unchanged | regression | `go test ./internal/handler -run TestAuth_JWTShapeUnchanged` | ⚠️ extend existing | ✅ green |
| AUTH-08 | 2 | `POST /auth/logout` returns 204, deletes refresh session row, blacklists access token | T-2-Logout | Blacklist key TTL clamped to access-token remaining life | integration | `go test ./internal/handler -run TestLogout_204_DeletesSession_BlacklistsToken` | ❌ W0 | ✅ green |
| AUTH-08 | 2 | After logout, calling access token → 401 (until exp) | T-2-LogoutAT | Middleware checks `IsTokenBlacklisted` and returns 401 | integration (full Fiber app + miniredis) | `go test ./internal/handler -run TestLogout_AccessTokenInvalidAfterLogout` | ❌ W0 | ✅ green |
| AUTH-08 | 2 | After logout, refresh token → 401 | T-2-LogoutRT | Session row deleted; `/auth/refresh` cannot find it | integration | `go test ./internal/handler -run TestLogout_RefreshTokenInvalidAfterLogout` | ❌ W0 | ✅ green |
| Concurrency | 2 | Two simultaneous `/auth/apple` with same sub → same `users.id`, one row, **every response HTTP 200 (W-4)** | T-2-RaceLink | INSERT then read-on-conflict via `errors.Is(err, ErrDuplicate)` translation | integration | `go test ./internal/handler -run TestAppleSignIn_ConcurrentSameSub -race` | ❌ W0 | ✅ green |
| Backcompat | 1 | Existing `/auth/guest`, `/auth/refresh`, `/auth/admin-login`, `/auth/link`, **`/auth/telegram/*`** (D-35 / W-2) still pass | — | New columns are nullable, `auth_provider` defaults to `'guest'` | regression | `cd server/api && go test ./internal/handler -run "TestGuestLogin\|TestAdminLogin\|TestRefreshToken\|TestLinkDevice\|TestTelegram" -count=1` | ✅ existing | ✅ green |
| Operational | 1 | `RequireEnv()` reports missing Apple/Google keys at startup | — | Aggregate error, single log line, exit non-zero | unit | `go test ./internal/config -run TestRequireEnv_MissingSSOKeys_Reported` | ❌ W0 | ✅ green |
| Operational | 1 | JWKs cold-start (Apple endpoint unreachable on boot) logs but does not panic — **B-1 fix: covered by plan 02 Task 2 `TestVerify_JWKsColdStart`** | — | `keyfunc.NewDefaultCtx` non-blocking by default; `apple.New` returns non-nil `*Verifier` even when JWKs URL unreachable | unit | `go test ./internal/auth/apple -run TestVerify_JWKsColdStart` | ❌ W0 | ✅ green |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `cd server/api && go get github.com/MicahParks/keyfunc/v3@v3.8.0 && go get google.golang.org/api/idtoken` — module fetches MUST land before verifier packages will compile
- [x] `server/api/migrations/018_add_sso_columns.sql` — six columns + three indexes; idempotent
- [x] `server/api/internal/auth/apple/verifier_test.go` — covers AUTH-01 happy/wrong-aud/expired/sig-mismatch using a test RSA keypair + stub JWKs source **+ TestVerify_JWKsColdStart for the `Operational | JWKsColdStart` row (B-1 fix)**
- [x] `server/api/internal/auth/google/verifier_test.go` — covers AUTH-02 happy/wrong-aud/email-not-verified using injected `idtokenValidator` interface
- [x] `server/api/internal/handler/auth_test.go` — extend `newAuthTestDB` to add the six SSO columns + three partial unique indexes so existing SQLite-based tests continue to pass and new SSO/logout tests can run
- [x] `server/api/internal/handler/auth_test.go` — add SSO + logout tests (AUTH-04 through AUTH-08, concurrency, JWT-shape regression, **B-3 `TestAppleSignIn_GuestWithConflict_DevicesReassigned`**)
- [x] `server/api/internal/repository/user_repo_test.go` (new or extend existing) — covers `FindUserByAppleID`, `FindUserByGoogleID`, `FindUserByVerifiedEmailForLink` (incl. private-relay exclusion), `PromoteGuestToSSO`, **`ReassignDevicesByUserID` multi-device variant (W-1 fix)**
- [x] `server/api/internal/config/config_test.go` — extend to assert the new env keys (`APPLE_TEAM_ID`, `APPLE_BUNDLE_ID`, `APPLE_SERVICE_ID`, `GOOGLE_CLIENT_ID_IOS`, `GOOGLE_CLIENT_ID_ANDROID`, `GOOGLE_CLIENT_ID_WEB`) are in `RequireEnv()` output when unset, matching the existing HOTFIX-08 (Phase 1) test pattern

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Real Apple `email_verified` claim type ("true"/"false" string vs bool) | AUTH-01 / Research A1 | Apple does not publish a stable test-token endpoint; CI cannot mint a real Apple identityToken | One-line spike in dev: run `POST /api/v1/auth/apple` with a real Apple token captured from a dev iOS build; log `fmt.Sprintf("%T", claims["email_verified"])`. Lock the assertion once observed. Document the observed type in `internal/auth/apple/verifier.go` package comment. |
| Real Google idtoken with `hd` (hosted domain) claim | AUTH-02 | Same — Google does not expose a deterministic test idToken endpoint | One-time dev capture; observe `Payload.Claims["hd"]` typing. Not a release blocker — the field is not enforced this phase. |
| Production smoke against gate.lava.top/lava.top JWKs endpoints | — | n/a — Phase 2 does not call lava.top | (Skip — Phase 3.) |
| Migration 018 against a populated staging Postgres (not just SQLite `:memory:`) | AUTH-03 | Partial-unique index semantics differ subtly between SQLite and Postgres 16 | Before merge: `psql staging < migrations/018_add_sso_columns.sql && psql -c '\d users' && psql -c "INSERT INTO users(id, apple_user_id) VALUES (gen_random_uuid(), 'TEST_SUB');" && psql -c "INSERT INTO users(id, apple_user_id) VALUES (gen_random_uuid(), 'TEST_SUB');"` — second insert MUST fail with `duplicate key value violates unique constraint`. Roll back the test inserts. |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify command pointing at one of the rows above, OR a Wave 0 dependency
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all rows marked `❌ W0` above
- [x] No `-tags=integration` or `-skip` flags hide test cases
- [x] No watch-mode flags (`-watch`, `--watch`) appear in CI
- [x] Feedback latency < 10s per task, < 90s per wave
- [x] All four manual-only verifications scheduled before `/gsd-verify-work`
- [x] `nyquist_compliant: true` set in frontmatter

## Validation Audit 2026-06-03

All AUTH-01..08 covered automated (run green this audit): Apple verifier 8 tests (sig/aud/issuer/expired/private-relay), Google verifier 5 tests (multi-audience/email-verified), handler SSO tests (cross-surface same-id, guest promotion, auto-link, private-relay-skip), 4 logout tests. AUTH-03 schema columns verified transitively by the passing SSO tests + documented psql smoke. The SSO snake_case contract fix (v2.2.0 audit, commit 55e5d3e) is reflected — SSO tests pass against the live mobile body shape. No automatable gaps. `nyquist_compliant: true`.

**Approval:** pending
