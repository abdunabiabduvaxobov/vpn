---
phase: 02-auth-sso-backend
plan: 02
subsystem: auth
tags: [apple-sso, jwt, jwks, rs256, keyfunc, verifier, identity, golang]

# Dependency graph
requires:
  - phase: 02-auth-sso-backend
    provides: "Plan 02-01 — User SSO columns (apple_user_id, email_verified, email_is_private_relay, auth_provider), env-var validator stubs, partial unique indexes."
provides:
  - "internal/auth/apple package — pure-library Apple identityToken verifier (no DB, no Fiber, no globals) (D-12)"
  - "AppleIdentity{Sub, Email, EmailVerified, IsPrivateRelay} value type for downstream handler composition"
  - "JWKSource interface enabling test-time injection of stub keys without network calls"
  - "Options{AllowedAudiences, AllowedIssuer, JWKsURL} startup-time DI contract (D-34)"
  - "RS256-only signature enforcement (rejects alg=none, HMAC forgeries)"
  - "Apple email_verified string-typing (`\"true\"` -> bool true) decoder — RESEARCH.md A1 footgun handled"
  - "Cold-start tolerance: apple.New() returns non-blocking even if Apple's JWKs URL unreachable at boot"
affects: [02-05-sso-handlers, 03-lava-payments, mobile-sso, landing-sso]

# Tech tracking
tech-stack:
  added:
    - "github.com/MicahParks/keyfunc/v3 v3.8.0 — Apple JWKs cache + jwt.Keyfunc bridge"
    - "github.com/MicahParks/jwkset v0.11.0 (transitive of keyfunc)"
    - "golang.org/x/time v0.9.0 (transitive of keyfunc)"
    - "github.com/golang-jwt/jwt/v5 v5.2.2 (semver-safe minor bump from v5.2.1)"
  patterns:
    - "Verifier package pattern: pure library exposes Verifier struct + value type + interface for test-time substitution; handler in another package composes."
    - "JWKSource interface defined in the verifier package; satisfied by both keyfunc.Keyfunc and a stub for tests — no special-casing in production code."
    - "TDD red-then-green via two atomic commits: failing tests first, implementation second (D-37 atomicity + GREEN pattern)."

key-files:
  created:
    - "server/api/internal/auth/apple/verifier.go (141 lines) — Verifier struct + New() + Verify() + AppleIdentity value type"
    - "server/api/internal/auth/apple/verifier_test.go (259 lines) — 8 test functions + 4 sub-tests, all using local RSA keypair + stub JWKSource"
  modified:
    - "server/api/go.mod — added direct dep github.com/MicahParks/keyfunc/v3 v3.8.0; promoted to direct after verifier.go imports it"
    - "server/api/go.sum — keyfunc + jwkset + x/time checksums; jwt/v5 upgraded v5.2.1 -> v5.2.2 (transitive)"

key-decisions:
  - "Constructor signature is `New(opts Options) (*Verifier, error)` (no ctx param) — context.Background() is created internally for the keyfunc refresh goroutine which is process-lifetime; future shutdown context can be wired via Options without API break."
  - "Stub JWKSource via interface (not concrete keyfunc.Keyfunc) — enables verifier tests to sign their own tokens with a local RSA keypair and bypass the network entirely. Production code wires keyfunc.NewDefaultCtx whose .Keyfunc method satisfies the interface."
  - "WithLeeway(30s) added to ParseWithClaims (RESEARCH.md §Race & Failure Modes #5) — absorbs minor clock skew between our server and Apple."
  - "Apple's email_verified is decoded as STRING == \"true\" (fail-safe): if Apple ever migrates to native bool, the decoder returns false rather than incorrectly auto-linking a stranger's account."

patterns-established:
  - "Verifier-package purity (D-12): zero imports from gofiber, gorm.io, internal/handler, internal/repository, internal/model. Future Google verifier (plan 03) follows the same shape."
  - "JWKs cold-start tolerance proved by regression test (TestVerify_JWKsColdStart points at http://127.0.0.1:1, asserts non-blocking return within 2s) — locks the RESEARCH.md non-blocking guarantee against accidental future change."
  - "Local RSA-2048 keypair per test (no shared keypair) — eliminates cross-test pollution and lets the signature-mismatch test use two distinct keys cleanly."

requirements-completed: [AUTH-01]

# Metrics
duration: ~12 min
completed: 2026-05-22
---

# Phase 02 Plan 02: Apple JWT Verifier Summary

**Pure-library Apple identityToken verifier (`internal/auth/apple`) using `MicahParks/keyfunc/v3` with RS256-only signature + iss + aud whitelist + exp checks, 30s clock-skew leeway, Apple's stringly-typed `email_verified` decoded fail-safe, and non-blocking JWKs cold-start.**

## Performance

- **Duration:** ~12 min
- **Started:** 2026-05-22T07:36Z
- **Completed:** 2026-05-22T07:39Z
- **Tasks:** 3 (1 chore + 1 test + 1 feat)
- **Files modified:** 4 (2 created, 2 modified)

## Accomplishments

- **AUTH-01 verifier layer landed:** Apple identityToken cryptographic validation is the new green path. ROADMAP §Phase 2 SC#5 ("wrong `aud` rejected with 401") is verified at the verifier layer; plan 05's handler will surface the 401.
- **Apple's stringly-typed `email_verified`/`is_private_email` footgun handled explicitly** (RESEARCH.md A1) — decoder reads as string, compares `== "true"`. Documented in package comment so the next maintainer doesn't "fix" it.
- **JWKs cold-start non-blocking guarantee locked in a regression test** — points `apple.New()` at `http://127.0.0.1:1/auth/keys` (guaranteed connection refused), asserts it returns a non-nil `*Verifier` within 2 seconds. Covers VALIDATION.md `Operational | JWKsColdStart` row.
- **Threat mitigations in code, verified by tests:**
  - T-2-AppleAud → explicit `AllowedAudiences` whitelist check + `TestVerify_AudienceMismatch` (asserts error contains "audience")
  - T-2-AppleSig → `jwt.WithValidMethods(["RS256"])` + JWKs-bound Keyfunc + `TestVerify_SignatureMismatch` (two distinct RSA keys, verifier rejects)
  - T-2-JWKsMITM → keyfunc's default `http.Client` (system CA bundle); zero `InsecureSkipVerify` in code; zero custom `http.Transport`
  - T-2-AppleEmailType → `TestVerify_AppleEmailVerifiedStringType` covers `"true"` / `"false"` / `""` / `"yes"`, all decoded fail-safe

## Task Commits

1. **Task 1: Add `MicahParks/keyfunc/v3 v3.8.0` dependency** — `0746051` (chore)
2. **Task 2: Red-phase tests (8 functions, 4 sub-tests)** — `f3d4568` (test)
3. **Task 3: Green-phase verifier.go implementation** — `6997b01` (feat) — also tidies `go.mod` to promote keyfunc/v3 from indirect to direct (auto-result of the import landing)

## Files Created/Modified

- `server/api/internal/auth/apple/verifier.go` (created, 141 lines) — `Verifier`, `New(Options)`, `Verify(ctx, token)`, `AppleIdentity`, `Options`, `JWKSource` interface, `defaultAppleJWKsURL` const, `clockSkewLeeway` const
- `server/api/internal/auth/apple/verifier_test.go` (created, 259 lines) — 8 test functions covering happy path + 5 negative paths + email-verified-string-typing (4 sub-cases) + JWKs cold-start
- `server/api/go.mod` (modified) — added direct dep `github.com/MicahParks/keyfunc/v3 v3.8.0`; transitive `MicahParks/jwkset v0.11.0`, `golang.org/x/time v0.9.0`; semver-safe upgrade of `golang-jwt/jwt/v5` v5.2.1 -> v5.2.2
- `server/api/go.sum` (modified) — checksums for the above

## Decisions Made

- **Constructor signature is `New(opts Options) (*Verifier, error)`** (no ctx param). The keyfunc refresh goroutine is process-lifetime; injecting a request-scope ctx would tie the goroutine to a doomed scope. A `JWKsURL` field on `Options` is the swap point for tests (and for any future deployment that wants to point at a JWKs mirror).
- **`JWKSource` interface defined in the verifier package, not the handler package** — the verifier owns its own injection contract. Production code passes `keyfunc.Keyfunc` (whose `.Keyfunc` method satisfies the interface via Go structural typing); tests pass `stubJWKS{pub: &priv.PublicKey}`.
- **Audience whitelist checked manually after `jwt.ParseWithClaims`** (not via `jwt.WithAudience`) — produces a clearer "apple: audience mismatch" error string for the handler to detect and map to 401. `WithAudience` would have collapsed into a generic invalid-claim error.
- **`jwt.WithLeeway(30 * time.Second)`** (RESEARCH.md §Race & Failure Modes #5) — absorbs the few-second clock drift that's normal between two NTP-synced machines and would otherwise produce phantom token-expired errors at the exp boundary.

## Deviations from Plan

None - plan executed exactly as written.

(Task 1 commit had a slight commit-subject style upgrade: `chore(02-02): ...` instead of `chore(02): ...` — matches the phase-plan convention already established by plan 02-01's commits like `docs(02-01): ...`. The plan's prose said `chore(02): ...` but the convention in-repo is the more specific form. Not a deviation in substance; subject prefix only.)

## Issues Encountered

- **`go mod tidy` after Task 1 stripped keyfunc as unused** (since no source file imported it yet). Resolved by re-adding via `go get` without tidy; committed the indirect entry in Task 1; Task 3's verifier.go imports it; Task 3's `go mod tidy` then correctly promoted it to direct. The acceptance check (`grep -c "github.com/MicahParks/keyfunc/v3 v3.8.0"` returns 1) passed at every step.
- **Two acceptance-check `grep -c` results matched comments, not code** — `InsecureSkipVerify` returned 1 (the comment `// HTTPS-only — D-CD threat-model row T-2-JWKsMITM forbids InsecureSkipVerify`) and the impurity grep returned 1 (the comment `// The handler in internal/handler/auth.go composes this with repository`). Verified via `grep -vE '^\s*//'` that the code body has zero forbidden patterns. The comments are intentional — they document WHY the patterns are forbidden, so future maintainers don't accidentally introduce them. The substantive security/purity requirement is fully met; the gap is in the verification script's regex specificity, not in the implementation.

## Verification Run

Plan's `<verification>` section (`cd server/api && go test ./internal/auth/apple/... -count=1 -race -v`):

```
=== RUN   TestVerify_HappyPath
--- PASS: TestVerify_HappyPath (0.17s)
=== RUN   TestVerify_AudienceMismatch
--- PASS: TestVerify_AudienceMismatch (0.04s)
=== RUN   TestVerify_ExpiredToken
--- PASS: TestVerify_ExpiredToken (0.09s)
=== RUN   TestVerify_SignatureMismatch
--- PASS: TestVerify_SignatureMismatch (0.25s)
=== RUN   TestVerify_IssuerMismatch
--- PASS: TestVerify_IssuerMismatch (0.06s)
=== RUN   TestVerify_AppleEmailVerifiedStringType (4 sub-cases, all PASS)
=== RUN   TestVerify_PrivateRelayFlag
--- PASS: TestVerify_PrivateRelayFlag (0.17s)
=== RUN   TestVerify_JWKsColdStart
2026/05/22 07:39:33 ERROR Failed to refresh HTTP JWK Set ...connection refused
--- PASS: TestVerify_JWKsColdStart (0.00s)
PASS
ok  vpnapp/server/api/internal/auth/apple  2.857s
```

12 PASS lines total. Zero FAIL. Zero race-detector warnings. `TestVerify_JWKsColdStart` in PASS list (VALIDATION.md `Operational | JWKsColdStart` row satisfied). Full project build (`go build ./...`) clean.

## Manual-Only Verification Deferred

Per VALIDATION.md "Manual-Only Verifications": confirm Apple's real `email_verified` claim type (string vs bool) with a one-line dev capture spike before `/gsd-verify-work` — log `fmt.Sprintf("%T", claims["email_verified"])` from a real Apple identityToken captured in a dev iOS build. Not a release blocker for plan 02-02; lock the observed type assertion in `verifier.go` package comment once observed.

## Next Plan Readiness

- **Plan 02-03 (Google verifier) is unblocked** — same package-shape (`internal/auth/google`) and same handler-side interface pattern. Can mirror this plan's structure 1:1.
- **Plan 02-05 (SSO handlers) is unblocked** — the `apple.Verifier` exists; the handler can DI it via the `AppleVerifier` interface it defines on its own side (Go structural typing).
- **No new env vars required this plan** — `APPLE_BUNDLE_ID` and `APPLE_SERVICE_ID` are loaded in plan 02-01; the verifier consumes them via the `Options.AllowedAudiences` slice constructed in `cmd/main.go` (plan 02-05 Task wiring).
- **No carryover blockers.**

## Self-Check: PASSED

- File `server/api/internal/auth/apple/verifier.go` exists: FOUND
- File `server/api/internal/auth/apple/verifier_test.go` exists: FOUND
- Commit `0746051` exists: FOUND
- Commit `f3d4568` exists: FOUND
- Commit `6997b01` exists: FOUND
- Full apple package test suite green with -race: FOUND (12 PASS, 0 FAIL)
- Full project build clean: FOUND

---
*Phase: 02-auth-sso-backend*
*Plan: 02 (Apple JWT verifier)*
*Completed: 2026-05-22*
