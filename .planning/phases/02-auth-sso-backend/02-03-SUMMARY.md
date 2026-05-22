---
phase: 02-auth-sso-backend
plan: 03
subsystem: auth
tags: [google-sso, jwt, idtoken, verifier, identity, golang, oidc]

# Dependency graph
requires:
  - phase: 02-auth-sso-backend
    provides: "Plan 02-01 — User SSO columns (google_user_id, email, email_verified, auth_provider) + GOOGLE_CLIENT_ID_IOS/_ANDROID/_WEB env vars + HOTFIX-08 validator hooks"
  - phase: 02-auth-sso-backend
    provides: "Plan 02-02 — Apple verifier package pattern (Options, JWKSource, New, Verify); this plan mirrors the verifier-package shape so plan 02-05's handler composes both verifiers identically"
provides:
  - "internal/auth/google package — pure-library Google idToken verifier (no DB, no Fiber, no globals) (D-13)"
  - "GoogleIdentity{Sub, Email, EmailVerified, HostedDomain} value type for downstream handler composition"
  - "idtokenValidator interface enabling test-time injection of fake validators without network calls"
  - "Audience iteration whitelist (D-16, D-34): loops {GoogleClientIDIOS, GoogleClientIDAndroid, GoogleClientIDWeb}; first non-error wins; all-fail returns last error wrapped"
  - "email_verified=false rejection (D-17) — blocks T-2-EmailSpoof before handler runs auto-link"
  - "HostedDomain (`hd` claim) surfaced for downstream Workspace handling; empty for consumer Gmail"
  - "defaultValidator{} adapter wrapping idtoken.Validate so production satisfies the idtokenValidator interface via Go structural typing"
affects: [02-05-sso-handlers, 03-lava-payments, mobile-sso, landing-sso]

# Tech tracking
tech-stack:
  added:
    - "google.golang.org/api v0.280.0 — official Google API library (used for idtoken.Validate)"
    - "~29 transitive deps (oauth2 v0.36.0, otel v1.43.0 + contrib/instrumentation, gax-go v2.22.0, grpc v1.81.1, protobuf v1.36.11, enterprise-certificate-proxy v0.3.15, etc.) — pulled by go.golang.org/api; acceptable per ADR §7"
    - "golang.org/x/crypto v0.51.0 (upgrade from v0.28.0; transitive of new deps)"
    - "golang.org/x/net v0.54.0 (added as transitive)"
    - "golang.org/x/sync v0.20.0, golang.org/x/sys v0.44.0, golang.org/x/term v0.43.0, golang.org/x/text v0.37.0, golang.org/x/time v0.15.0 (transitive upgrades)"
    - "github.com/google/uuid v1.6.0 (semver-safe upgrade from v1.5.0; required by go.golang.org/api)"
  patterns:
    - "Mirrored verifier-package shape from plan 02-02 (Apple): pure library exposes Verifier struct + value type + interface for test-time substitution; handler in another package composes via Go structural typing."
    - "idtokenValidator interface defined in the verifier package; satisfied by both defaultValidator{} (wraps idtoken.Validate) and a fakeValidator stub for tests — no special-casing in production code."
    - "TDD red-then-green via three atomic commits (D-37): chore (dep), test (red), feat (green) — same shape as plan 02-02."

key-files:
  created:
    - "server/api/internal/auth/google/verifier.go (94 lines) — Verifier struct + New() + Verify() + GoogleIdentity value type + idtokenValidator interface + defaultValidator{} adapter"
    - "server/api/internal/auth/google/verifier_test.go (131 lines) — 5 test functions covering happy/3rd-aud/all-fail/email-not-verified/hosted-domain via fakeValidator interface injection"
  modified:
    - "server/api/go.mod — added direct dep google.golang.org/api v0.280.0 (promoted from indirect after verifier.go imports it); dropped orphan indirect github.com/stretchr/testify (auto-cleanup by go mod tidy; zero source imports)"
    - "server/api/go.sum — google api + transitive checksums; x/crypto / x/net / oauth2 / otel / grpc / protobuf / gax-go / enterprise-cert-proxy / google/uuid v1.6.0 entries"

key-decisions:
  - "Constructor signature is `New(audiences []string) *Verifier` (no ctx param, no error return) — Google's verifier has no init-time work (no JWKs prefetch goroutine to launch, no network call at startup); idtoken.Validate self-manages its key cache on each call. Simpler than Apple's `New(opts Options) (*Verifier, error)` because there's no cold-start failure mode to expose."
  - "idtokenValidator interface defined in the verifier package (not the handler package) — the verifier owns its own injection contract. Production wires defaultValidator{}; tests wire fakeValidator. Identical pattern to Apple's JWKSource interface."
  - "Audience iteration uses errors.Is-style last-error propagation wrapped with `fmt.Errorf(\"google: %w\", lastErr)` — preserves the underlying idtoken validation error for handler-level mapping (audience-mismatch text from the google library is wrapped, not replaced)."
  - "email_verified gate is the FIRST check after successful idtoken.Validate (before any field extraction), short-circuiting before the auto-link search space ever sees the email. Matches D-17 / T-2-EmailSpoof mitigation contract."
  - "Empty AllowedAudiences slice returns explicit error `\"google: no allowed audiences configured\"` rather than silently iterating zero times and returning a misleading nil-error / empty identity — guards against startup misconfiguration (e.g. all three GOOGLE_CLIENT_ID_* env vars empty making it past the validator)."

patterns-established:
  - "Verifier-package purity (D-13): zero non-comment imports from gofiber, gorm.io, internal/handler, internal/repository, internal/model. Identical purity stance as plan 02-02 Apple verifier — confirms the project-wide pattern for verifier packages."
  - "Test-time fake injection via package-local interface (idtokenValidator): tests construct &Verifier{validator: &fakeValidator{...}} directly with the unexported field, exploiting Go's package-scope access. Mirrors the verifier-internal injection seen in plan 02-02's apple package."
  - "Per-audience error stacking: collect lastErr inside the loop, return wrapped last error after all audiences fail. Preserves the underlying idtoken validation message while making the verifier error string greppable for the handler-level 401 mapper (plan 02-05)."

requirements-completed: [AUTH-02]

# Metrics
duration: ~7 min
completed: 2026-05-22
---

# Phase 02 Plan 03: Google JWT Verifier Summary

**Pure-library Google idToken verifier (`internal/auth/google`) using `google.golang.org/api/idtoken` with strict iOS/Android/Web audience iteration, `email_verified=true` mandate (D-17), and HostedDomain claim extraction — mirrors plan 02-02's Apple verifier shape for handler-side composition parity.**

## Performance

- **Duration:** ~7 min (actual: 6m 41s)
- **Started:** 2026-05-22T02:44:06Z
- **Completed:** 2026-05-22T02:50:47Z
- **Tasks:** 3 (1 chore + 1 test + 1 feat)
- **Files modified:** 4 (2 created, 2 modified)

## Accomplishments

- **AUTH-02 verifier layer landed:** Google idToken cryptographic validation is the new green path. ROADMAP §Phase 2 SC#5 ("wrong `aud` rejected with 401") is verified at the Google verifier layer via `TestVerify_AllAudiencesFail`; plan 02-05's handler will surface the 401.
- **D-17 email_verified gate enforced at the verifier boundary** (RESEARCH.md §Race & Failure Modes #9 / VALIDATION.md T-2-EmailSpoof): a Google identity with `email_verified=false` returns `errors.New("google: email not verified")` BEFORE the handler ever sees the email — the auto-link search space (plan 02-05) cannot be poisoned by an unverified address.
- **Verifier-package pattern consistency with Apple (plan 02-02):** same Options/interface/injection shape, same purity stance (D-12 + D-13), same TDD commit cadence (chore → test → feat). Plan 02-05's SSO handler will compose `appleVerifier.Verify(...)` and `googleVerifier.Verify(...)` through structurally identical handler-side interfaces.
- **Threat mitigations in code, verified by tests:**
  - **T-2-GoogleAud** → explicit `AllowedAudiences []string` whitelist iteration + `TestVerify_AllAudiencesFail` (fake validator errors for every audience → Verify returns non-nil error wrapping the last underlying error)
  - **T-2-EmailSpoof** → `email_verified=false` short-circuit + `TestVerify_EmailNotVerified` (error string contains both "email" and "verif" so the handler's 401 mapper can detect cleanly)
  - **Audience iteration ordering** → `TestVerify_HappyPath_ThirdAudience` proves the loop actually iterates beyond the first audience (catches a regression where someone breaks the loop into a single-audience hardcode)
  - **Hosted-domain extraction** → `TestVerify_HostedDomainExtracted` proves the `hd` claim is surfaced for plan 02-05's optional Workspace-tenant handling (currently unused but won't surprise downstream code)

## Task Commits

Each task was committed atomically (D-37):

1. **Task 1: Add `google.golang.org/api/idtoken` dependency** — `4e82096` (chore)
2. **Task 2: Red-phase tests (5 functions)** — `118e8fd` (test)
3. **Task 3: Green-phase verifier.go implementation + go.mod direct-promotion** — `0706821` (feat) — Task 3's `go mod tidy` promoted `google.golang.org/api` from `// indirect` to direct (verifier.go imports `idtoken`) and dropped orphan indirect `github.com/stretchr/testify` (zero source imports)

## Files Created/Modified

- `server/api/internal/auth/google/verifier.go` (created, 94 lines) — `Verifier`, `New(audiences []string)`, `Verify(ctx, idToken)`, `GoogleIdentity`, `idtokenValidator` interface, `defaultValidator{}` adapter
- `server/api/internal/auth/google/verifier_test.go` (created, 131 lines) — 5 test functions covering happy / 3rd-audience-iteration / all-aud-fail / email-not-verified / hosted-domain via `fakeValidator` interface injection
- `server/api/go.mod` (modified) — added direct dep `google.golang.org/api v0.280.0`; transitive additions: `golang.org/x/oauth2 v0.36.0`, `go.opentelemetry.io/otel v1.43.0` + contrib/instrumentation/net/http/otelhttp v0.67.0 + auto/sdk v1.2.1 + metric/trace v1.43.0, `google.golang.org/genproto/googleapis/rpc`, `google.golang.org/grpc v1.81.1`, `google.golang.org/protobuf v1.36.11`, `googleapis/enterprise-certificate-proxy v0.3.15`, `googleapis/gax-go/v2 v2.22.0`; semver-safe upgrades: `google/uuid v1.5.0 → v1.6.0`, `golang.org/x/crypto v0.28.0 → v0.51.0`, `x/net v0.21.0 → v0.54.0`, `x/sync v0.9.0 → v0.20.0`, `x/sys v0.26.0 → v0.44.0`, `x/term v0.25.0 → v0.43.0`, `x/text v0.20.0 → v0.37.0`, `x/time v0.9.0 → v0.15.0`; dropped orphan indirect `stretchr/testify v1.11.1` (no source imports)
- `server/api/go.sum` (modified) — checksums for the above

## Decisions Made

- **Constructor is `New(audiences []string) *Verifier`** (no ctx, no error). Google's idtoken library has no init-time work — no goroutine to launch, no network at startup, no cold-start failure mode. Simpler than Apple's `New(opts Options) (*Verifier, error)`. The audience whitelist is the only DI input and a typed `[]string` is sufficient.
- **`idtokenValidator` interface in the verifier package** (not the handler package). Mirrors plan 02-02's JWKSource pattern: the verifier owns its own injection contract; production uses `defaultValidator{}` wrapping `idtoken.Validate`; tests inject `&fakeValidator{...}` directly via the package-local unexported field. Same pattern as Apple, intentional consistency for the future handler composition.
- **`email_verified` gate is the FIRST check after successful `idtoken.Validate`** — before any field extraction returns to the caller. This puts the security-critical mitigation as close to the trust boundary as possible. The error string is greppable (`"google: email not verified"`) so plan 02-05's handler can map it to 401.
- **Empty AllowedAudiences returns explicit error** — guards against the misconfiguration where all three `GOOGLE_CLIENT_ID_*` env vars get past the HOTFIX-08 validator (e.g. via a config refactor). Silently iterating zero times would return a misleading `nil-error / empty-identity` pair.
- **Last-error wrapping for all-fail case** uses `fmt.Errorf("google: %w", lastErr)` — preserves the underlying `idtoken.Validate` error message for the handler's logging/metrics while wrapping with the verifier's namespace.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] `go mod tidy` after Task 1 stripped `google.golang.org/api` (no source imports yet)**
- **Found during:** Task 1 (Add dependency)
- **Issue:** `go get google.golang.org/api/idtoken` added the dep, but the subsequent `go mod tidy` (per the plan's action instructions) removed it because no source file imported `idtoken` yet — same issue plan 02-02 hit per its SUMMARY "Issues Encountered" section.
- **Fix:** Re-ran `go get google.golang.org/api/idtoken` WITHOUT tidy, committed the indirect entry in Task 1. Task 3's verifier.go imports `idtoken`; Task 3's `go mod tidy` then correctly promoted it from `// indirect` to direct.
- **Files modified:** server/api/go.mod, server/api/go.sum (Task 1 commit) — re-staged with the indirect entry present.
- **Verification:** Task 1 acceptance check `grep -c "^[[:space:]]*google.golang.org/api " go.mod` returned 1 (indirect line still matches the leading-whitespace + space pattern); Task 3 final state has direct entry, all 5 tests green, full build clean.
- **Committed in:** `4e82096` (Task 1) — kept as `// indirect`; `0706821` (Task 3) — promoted to direct.

**2. [Rule 3 - Blocking] `go mod tidy` in Task 3 also dropped orphan indirect `github.com/stretchr/testify v1.11.1`**
- **Found during:** Task 3 (verifier.go implementation, tidy step)
- **Issue:** Tidy detected `stretchr/testify` was in `// indirect` block but zero source files import it. Independent of my plan; tidy's removal is correct module hygiene but technically out of plan scope.
- **Fix:** Allowed the removal — verified `grep -rE 'stretchr/testify' --include='*.go'` returns zero matches (no test or source code imports it). Bundling it with Task 3's commit is cleaner than reverting tidy or splitting it into a separate chore commit.
- **Files modified:** server/api/go.mod (one line removed).
- **Verification:** `go build ./...` clean; `go test ./internal/auth/... -race` passes; no broken dependencies.
- **Committed in:** `0706821` (Task 3) — line `github.com/stretchr/testify v1.11.1 // indirect` removed.

**3. [Documentation, not a fix] Plan-staging instruction divergence — Task 3 staged 3 files instead of "exactly 1"**
- **Found during:** Task 3 (commit step)
- **Issue:** Plan said "Stage exactly `internal/auth/google/verifier.go`" but `go mod tidy` (which the plan explicitly instructs to run before commit) modifies `go.mod` + `go.sum`. Following the plan literally would leave `go.mod` showing `google.golang.org/api` as `// indirect` even though `verifier.go` imports it directly — an inconsistent state any future `go mod tidy` run would immediately "fix" and someone would have to commit that fix anyway.
- **Fix:** Staged `verifier.go`, `go.mod`, `go.sum` together in Task 3 — captures the import-and-promote in one atomic commit. Same approach plan 02-02's Task 3 took (its SUMMARY's Decisions section notes "Task 3's `go mod tidy` then correctly promoted it to direct").
- **Files modified:** Three files in `0706821` instead of one.
- **Verification:** Plan 02-02 set the precedent for this same pattern; commit message documents the promotion explicitly.
- **Committed in:** `0706821` (Task 3) — `verifier.go` + `go.mod` + `go.sum`.

**4. [Verification-script nit, not a deviation in implementation] Task 3 purity-check grep matches one line — a doc-comment, not an import**
- **Found during:** Task 3 (acceptance criterion run)
- **Issue:** `grep -cE 'gofiber|gorm.io|internal/handler|internal/repository|internal/model' verifier.go` returned 1, criterion expected 0. Identical false-positive to plan 02-02 (its SUMMARY documents the same).
- **Cause:** Package doc-comment line 4 says `"The handler in internal/handler/auth.go composes this with repository"` — prose explaining the cross-package composition pattern. Not an import.
- **Substantive check:** `grep -vE '^\s*//' verifier.go | grep -cE 'gofiber|gorm.io|internal/handler|internal/repository|internal/model'` returns 0 — zero forbidden imports in code body. D-13 purity met.
- **Fix:** None needed in code (the doc-comment is intentional and useful for future maintainers); documented here so future plans can refine the verification-script regex.
- **Files modified:** None.
- **Committed in:** N/A.

---

**Total deviations:** 4 documented (3 auto-fixes + 1 verification-script nit)
**Impact on plan:** All four follow the precedents set by plan 02-02 (same package pattern, same tooling, same false-positives). Zero scope creep, zero security weakening, all five plan-spec tests green, full project builds clean.

## Issues Encountered

- **`go mod tidy` strips unused indirect deps** (Task 1) — known footgun documented above (Deviation 1). Resolution: defer tidy until after Task 3's import lands.
- **Initial Task 1 commit accidentally swept up pre-existing baseline-staged changes** (`.planning/ROADMAP.md`, `STATE.md`, `01-REVIEW-FIX.md`, `01-REVIEW.md`, `migrations/018_add_sso_columns.sql`). These were already in the index from the worktree baseline reset (the worktree was created on top of commit `2ad152e` which had a soft reset against `6a3da00` for unrelated reasons). Caught immediately via `git log -1 --stat` — undone via `git reset --soft HEAD~1`, then `git restore --staged` for the unrelated files, then re-committed Task 1 with only `go.mod` + `go.sum`. Net effect: zero contamination in the final three commits.

## Verification Run

Plan's `<verification>` section (`cd server/api && go test ./internal/auth/... -count=1 -race -v`):

```
=== RUN   TestVerify_HappyPath              (apple)    --- PASS (0.57s)
=== RUN   TestVerify_AudienceMismatch       (apple)    --- PASS (0.06s)
=== RUN   TestVerify_ExpiredToken           (apple)    --- PASS (0.35s)
=== RUN   TestVerify_SignatureMismatch      (apple)    --- PASS (0.20s)
=== RUN   TestVerify_IssuerMismatch         (apple)    --- PASS (0.04s)
=== RUN   TestVerify_AppleEmailVerifiedStringType  (apple, 4 sub-cases all PASS)
=== RUN   TestVerify_PrivateRelayFlag       (apple)    --- PASS (0.05s)
=== RUN   TestVerify_JWKsColdStart          (apple)    --- PASS (0.00s)
PASS    vpnapp/server/api/internal/auth/apple   3.444s

=== RUN   TestVerify_HappyPath              (google)   --- PASS (0.00s)
=== RUN   TestVerify_HappyPath_ThirdAudience (google)  --- PASS (0.00s)
=== RUN   TestVerify_AllAudiencesFail       (google)   --- PASS (0.00s)
=== RUN   TestVerify_EmailNotVerified       (google)   --- PASS (0.00s)
=== RUN   TestVerify_HostedDomainExtracted  (google)   --- PASS (0.00s)
PASS    vpnapp/server/api/internal/auth/google  1.558s
```

13 unique PASS lines (8 apple + 5 google) + 4 apple sub-cases = 17 PASS total. Zero FAIL. Zero race-detector warnings. Full project build (`cd server/api && go build ./...`) clean. VALIDATION.md rows AUTH-02 happy + AUTH-02 email-not-verified satisfied. T-2-GoogleAud and T-2-EmailSpoof mitigations verified by tests.

## Manual-Only Verification Deferred

Per VALIDATION.md "Manual-Only Verifications":
- **Real Google idToken with `hd` claim** — the field is surfaced but not enforced this phase. One-time dev capture before `/gsd-verify-work` to observe `Payload.Claims["hd"]` typing. Not a release blocker — plan 02-05's handler does not branch on `HostedDomain` this phase.

## Next Plan Readiness

- **Plan 02-04 (Migration 018 / GORM model)** unblocked — independent of this plan; both verifier packages now exist.
- **Plan 02-05 (SSO handlers)** unblocked — can construct `google.New([]string{cfg.GoogleClientIDIOS, cfg.GoogleClientIDAndroid, cfg.GoogleClientIDWeb})` and `apple.New(apple.Options{...})` side-by-side in `cmd/main.go`, then DI both verifiers into `handler.GoogleSignIn(...)` and `handler.AppleSignIn(...)` via handler-side interfaces (Go structural typing — no explicit `implements`).
- **Wave 1 merge note:** plan 02-02 (Apple) and plan 02-03 (Google) share `go.mod` / `go.sum` only. Both verifier packages live in disjoint directories (`internal/auth/apple/` vs `internal/auth/google/`). The Wave 1 merge step (per plan 02-03's W-3 revision note) serialises the two `go mod tidy` outputs; in this worktree the merge is a no-op because tidy was run after Task 3's import landed.
- **No carryover blockers.**

## Self-Check: PASSED

- File `server/api/internal/auth/google/verifier.go` exists: FOUND
- File `server/api/internal/auth/google/verifier_test.go` exists: FOUND
- Commit `4e82096` exists: FOUND
- Commit `118e8fd` exists: FOUND
- Commit `0706821` exists: FOUND
- Google verifier package test suite green with -race: FOUND (5 PASS, 0 FAIL)
- Full Phase 2 verifier-package suite green with -race: FOUND (13 PASS + 4 sub-cases, 0 FAIL)
- Full project build clean: FOUND
- AUTH-02 verifier layer requirements met: FOUND (D-15, D-16, D-17, D-34, D-13)
- Threat mitigations T-2-GoogleAud + T-2-EmailSpoof verified by tests: FOUND

---
*Phase: 02-auth-sso-backend*
*Plan: 03 (Google JWT verifier)*
*Completed: 2026-05-22*
