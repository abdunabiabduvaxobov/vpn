---
phase: 02-auth-sso-backend
plan: 01
subsystem: auth
tags: [sso, apple-signin, google-signin, postgres-migration, gorm, env-validation, sqlite-test-helpers]

# Dependency graph
requires:
  - phase: 01-hotfix-audit-critical-fixes
    provides: HOTFIX-08 fail-fast aggregate env validator (config.RequireEnv / OptionalEnvWarnings), called from cmd/main.go before config.Load
  - phase: 01-hotfix-audit-critical-fixes
    provides: Migration runner pattern (docker-entrypoint-initdb.d, BEGIN/COMMIT-wrapped idempotent .sql files, sequential numbering — 017 was the last)
provides:
  - users.apple_user_id / google_user_id / email / email_verified / email_is_private_relay / auth_provider columns + three partial-unique/partial indexes (migration 018)
  - User struct SSO fields (AppleUserID, GoogleUserID, Email, EmailVerified, EmailIsPrivateRelay, AuthProvider) with correct nullability + json tags
  - Eight Apple/Google env vars on Config struct (5 Apple + 3 Google) populated from os.Getenv
  - RequireEnv() now enforces the six required SSO keys at boot (T-2-EnvBoot mitigation); OptionalEnvWarnings() carries the two Apple .p8 keys (warn-not-fail per D-30)
  - newAuthTestDB extended with the six SSO columns + two partial unique indexes mirroring migration 018
  - Five additional test-DB helpers across the codebase extended with the same six columns so GORM Create(&model.User{}) calls keep working
affects: [02-02 apple-verifier, 02-03 google-verifier, 02-04 repository-functions, 02-05 sso-handlers, 02-06 logout-handler, 02-07 cmd-main-wiring]

# Tech tracking
tech-stack:
  added: []  # No new libraries — verifier packages land in plans 02-02 / 02-03
  patterns:
    - "Soft enum + CHECK constraint at DB layer (auth_provider IN ('guest','apple','google','admin')) — typo guardrail per Discretion"
    - "Partial unique indexes (WHERE col IS NOT NULL) to enforce one-row-per-provider-sub while leaving guest rows unconstrained"
    - "Partial INDEX with compound predicate (WHERE email_verified=TRUE AND email_is_private_relay=FALSE) to exclude private-relay rows from auto-link search space — privacy/security defense in depth"
    - "json:\"-\" on AppleUserID/GoogleUserID/EmailIsPrivateRelay — provider subs and relay flag are never serialized to API clients"
    - "Test helpers parallelize the production schema — every test-DB CREATE TABLE must be updated in lockstep with model changes; documented for plan 02-05"

key-files:
  created:
    - server/api/migrations/018_add_sso_columns.sql
  modified:
    - server/api/internal/model/user.go
    - server/api/internal/config/config.go
    - server/api/internal/config/config_test.go
    - server/api/internal/handler/auth_test.go
    - server/api/internal/handler/payment_test.go
    - server/api/cmd/createadmin/main_test.go
    - server/api/internal/middleware/admin_test.go
    - server/api/internal/repository/user_repo_subscription_test.go
    - server/api/internal/repository/subscription_repo_test.go

key-decisions:
  - "Took CONTEXT.md Discretion to add CHECK (auth_provider IN ('guest','apple','google','admin')) to migration 018 — one extra line, prevents DB-layer typos."
  - "Updated Phase 1's TestRequireEnv_ReturnsAllMissingKeys / _ReturnsEmptyWhenAllSet to be forward-compatible (must-contain Phase 1 keys + sanity floor) so future phases adding required keys don't regress them — Rule 1 fix scoped to test code only."
  - "Extended five additional test-DB helpers (payment_test, createadmin/main_test, middleware/admin_test, user_repo_subscription_test, subscription_repo_test) with the same six SSO columns as part of Task 4 — strict reading of the plan only enumerated newAuthTestDB but the User model change cascaded to every GORM-Create test helper. Treated as Rule 1 fix; documented below."
  - "Did NOT update internal/handler/connection_test.go or internal/handler/admin_test.go schema helpers — they use raw SQL inserts, not GORM Create, so they pass without changes today. Flagged in commit message for plan 02-05 when SSO handlers will use them."

patterns-established:
  - "DB Discretion gate: when CONTEXT.md offers a Discretion, prefer the safer choice if cost is one extra DB line (here: enum CHECK constraint)."
  - "Forward-compatible required-key tests: assert must-contain + sanity floor rather than exact count, so adding required keys in later phases is additive not breaking."
  - "When User struct schema changes, every test-DB helper file across the repo must be updated in the same commit-set; document files using raw SQL inserts that don't currently break but will when those code paths invoke GORM Create."

requirements-completed: [AUTH-03]

# Metrics
duration: 10m 12s
completed: 2026-05-22
---

# Phase 2 Plan 01: SSO Foundation Schema Summary

**Migration 018 + User model + HOTFIX-08 env validator wiring + extended test-DB schemas across 6 helpers — every Wave 1 foundation that later Phase 2 plans depend on.**

## Performance

- **Duration:** 10m 12s
- **Started:** 2026-05-22T02:20:15Z
- **Completed:** 2026-05-22T02:30:27Z
- **Tasks:** 4
- **Files modified:** 9 (1 new migration + 1 model + 2 config + 5 test schemas)

## Accomplishments

- DB migration 018 adds six SSO identity columns + three partial indexes + CHECK enum guard — destruction-free per D-32.
- GORM User struct gains six SSO fields with correct nullability, gorm tags, and `json:"-"` on sensitive provider subs.
- Config struct exposes eight SSO env vars (5 Apple + 3 Google); RequireEnv() now boot-blocks on the six required keys (T-2-EnvBoot mitigation); two optional .p8 keys live in OptionalEnvWarnings (D-30 warn-not-fail until Apple authorizationCode exchange lands).
- newAuthTestDB + five additional GORM-Create-aware test-DB helpers extended with the same six columns + two partial unique indexes so every existing handler/middleware/repository test passes against the extended User model.
- 23 baseline test failures resolved (caused by the User model change rippling through five test-DB helpers that the plan didn't enumerate); zero scope creep beyond restoring green.

## Task Commits

Each task was committed atomically per D-37:

1. **Task 1: Migration 018** — `d8c0682` (feat) — `feat(02-01): migration 018 — add SSO columns to users [AUTH-03]`
2. **Task 2: User model SSO fields** — `dce3ac4` (feat) — `feat(02-01): extend User model with SSO fields [AUTH-03]`
3. **Task 3: Env validator wiring + new test** — `21774ed` (feat) — `feat(02-01): register Apple/Google env vars with HOTFIX-08 validator [AUTH-03]`
4. **Task 4: Extended test-DB helpers (1 in-plan + 5 cascaded)** — `c9d34a2` (test) — `test(02-01): extend newAuthTestDB with SSO columns + partial indexes [AUTH-03]`

## Files Created/Modified

### Created

- `server/api/migrations/018_add_sso_columns.sql` — six ALTER TABLE columns, three indexes (two partial unique, one partial non-unique), one CHECK constraint, BEGIN/COMMIT wrapped.

### Modified

- `server/api/internal/model/user.go` — six new GORM fields per D-11 between TelegramFirstName and CreatedAt; gofmt re-aligned existing Telegram field column widths (no behavior change).
- `server/api/internal/config/config.go` — Config struct +8 SSO fields; Load() populates them; RequireEnv() +6 required keys; OptionalEnvWarnings() +2 optional keys.
- `server/api/internal/config/config_test.go` — added TestRequireEnv_MissingSSOKeys_Reported (exact-set assertion on SSO keys); updated TestRequireEnv_ReturnsAllMissingKeys / _ReturnsEmptyWhenAllSet to be forward-compatible with future required-key additions.
- `server/api/internal/handler/auth_test.go::newAuthTestDB` — six SSO columns in users CREATE TABLE + two partial unique indexes mirroring migration 018.
- `server/api/internal/handler/payment_test.go::newTestDB` — six SSO columns in users CREATE TABLE (Rule 1 cascade fix).
- `server/api/cmd/createadmin/main_test.go::openTestDB` — same (Rule 1 cascade).
- `server/api/internal/middleware/admin_test.go::openAdminTestDB` — same (Rule 1 cascade).
- `server/api/internal/repository/user_repo_subscription_test.go::openTestDB` — same (Rule 1 cascade).
- `server/api/internal/repository/subscription_repo_test.go` — same (Rule 1 cascade).

## Decisions Made

- **CHECK constraint added** (CONTEXT.md Discretion): `auth_provider IN ('guest','apple','google','admin')` — one extra SQL line, prevents typos at the DB layer (Discretion "auth_provider enum enforcement"). Documented in the migration comment.
- **Forward-compatible Phase 1 env tests** (Rule 1 fix): updated to assert must-contain + sanity floor rather than exact count, so adding required keys in plans 02-02 through 02-07 is additive not breaking.
- **Plan scope cascade** (Rule 1 fixes): the plan only enumerated `newAuthTestDB` but the User model change broke 5 other GORM-Create test helpers (23 failing tests). Fixed all 5 in the Task 4 commit to keep the commit cohesive — extending one test schema and leaving five broken would be misleading.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Phase 1 env validator tests had stale exact-count assertions**

- **Found during:** Task 3 (after adding 6 required keys to RequireEnv())
- **Issue:** `TestRequireEnv_ReturnsAllMissingKeys` asserted exactly 4 missing keys; `TestRequireEnv_ReturnsEmptyWhenAllSet` set only the 4 Phase 1 keys and expected 0 missing. After my change, both regressed (10 missing / 6 missing respectively).
- **Fix:** Rewrote both tests to be forward-compatible — `_ReturnsAllMissingKeys` now asserts must-contain the 4 Phase 1 keys + sanity floor `len >= 4`; `_ReturnsEmptyWhenAllSet` now sets all required keys (Phase 1 + Phase 2). The new TestRequireEnv_MissingSSOKeys_Reported handles the exact-set assertion for the SSO subset.
- **Files modified:** `server/api/internal/config/config_test.go`
- **Verification:** All 6 tests in `internal/config` package pass; `go test ./internal/config/... -count=1 -v` shows PASS for each.
- **Committed in:** `21774ed` (Task 3 commit)

**2. [Rule 1 - Bug] User model change broke 5 GORM-Create test helpers (23 test failures)**

- **Found during:** Task 4 (full test suite run after extending newAuthTestDB)
- **Issue:** Adding the 6 SSO fields to the User struct (Task 2) made GORM emit `INSERT INTO users (..., apple_user_id, ..., auth_provider) VALUES (...)` for every `db.Create(&model.User{...})` call. Five test-DB helpers across the codebase have their own duplicated users CREATE TABLE statements that didn't have the new columns, producing 23 baseline test failures: `payment_test.go::newTestDB` (6 fails), `createadmin/main_test.go::openTestDB` (1), `middleware/admin_test.go::openAdminTestDB` (6), `repository/user_repo_subscription_test.go` (4), `repository/subscription_repo_test.go` (6). Error: `table users has no column named apple_user_id`.
- **Fix:** Added the same six SSO columns to all five test-DB CREATE TABLE statements. No partial unique indexes (only needed where tests will exercise duplicate-sub semantics — that's plan 02-05 territory).
- **Files modified:** `server/api/internal/handler/payment_test.go`, `server/api/cmd/createadmin/main_test.go`, `server/api/internal/middleware/admin_test.go`, `server/api/internal/repository/user_repo_subscription_test.go`, `server/api/internal/repository/subscription_repo_test.go`
- **Verification:** `cd server/api && go test ./... -count=1` — all 8 test packages green (`ok`), zero `FAIL`.
- **Committed in:** `c9d34a2` (Task 4 commit, kept cohesive)

### Acceptance Criteria Deviation (documented, not auto-fixed)

**3. Task 4 AC referenced TestTelegram* tests that do not exist in the baseline**

- **Plan said:** "TestTelegram* explicitly executed (not silently skipped)" per D-35 regression scope.
- **Reality:** `grep -rln "func TestTelegram" --include="*_test.go" .` returns empty — no Telegram handler tests exist in the codebase yet. `/auth/telegram/*` handlers have zero test coverage at baseline.
- **Outcome:** Other AC rows (TestGuestLogin / TestAdminLogin / TestRefreshToken / TestLinkDevice) all pass. The Telegram regression intent is satisfied vacuously (no tests to break); when Telegram tests are added (separate plan), they will run against the SSO-extended schema since the columns are nullable with defaults.
- **No code change made.** Documented here so the verifier doesn't flag.

---

**Total deviations:** 2 Rule-1 auto-fixes + 1 documented AC deviation
**Impact on plan:** Both auto-fixes were the direct downstream consequence of the User struct change. They had to land for the test suite to remain green; otherwise the four AUTH-03 commits would have left the repo in a broken state. No scope creep beyond the immediate cascade.

## Issues Encountered

- **gofmt re-aligned Telegram field column widths** in `user.go` after the SSO fields landed — purely cosmetic, no behavior change. Captured in Task 2 commit message.

## User Setup Required

None — this plan is pure backend foundation (DB columns + Go struct fields + env validator wiring + test helpers). The six new required env vars (`APPLE_TEAM_ID`, `APPLE_BUNDLE_ID`, `APPLE_SERVICE_ID`, `GOOGLE_CLIENT_ID_IOS/_ANDROID/_WEB`) MUST be present in any deploy that runs `cmd/main.go` after this plan lands — operators will see a single fail-fast aggregate error listing every missing key (HOTFIX-08 behavior). Operational rollout is tracked in `.planning/STATE.md` "Phase 2 blockers" section.

## Next Phase Readiness

**Unblocked for the rest of Phase 2:**
- Plan 02-02 (Apple verifier): can now read `cfg.AppleBundleID`, `cfg.AppleServiceID` at construction time (D-34 DI pattern).
- Plan 02-03 (Google verifier): can now read `cfg.GoogleClientIDIOS/Android/Web`.
- Plan 02-04 (repository functions): can now query `WHERE apple_user_id = ?` and `WHERE google_user_id = ?` against the partial unique indexes; `WHERE email_verified=TRUE AND email_is_private_relay=FALSE` against the email auto-link index.
- Plan 02-05 (SSO handlers): `User` struct exposes the fields needed for `users.email`, `users.auth_provider`, `users.apple_user_id` reads/writes.
- Plan 02-06 (Logout handler): no new dependency from this plan — already had blacklist plumbing from Phase 1 HOTFIX-04/AUTH-02.

**Carry-over notes for plan 02-05 (SSO handler tests):**
- `internal/handler/connection_test.go` (line 73) and `internal/handler/admin_test.go` (line 318) have their own users CREATE TABLE statements that still lack the six SSO columns. They pass today because their tests use raw SQL inserts (not GORM Create). They WILL need extension when SSO handler tests touch them (or when a future test there switches to GORM Create).

## Self-Check: PASSED

- File created `server/api/migrations/018_add_sso_columns.sql`: FOUND
- File modified `server/api/internal/model/user.go`: FOUND (committed in dce3ac4)
- File modified `server/api/internal/config/config.go`: FOUND (committed in 21774ed)
- File modified `server/api/internal/config/config_test.go`: FOUND (committed in 21774ed)
- File modified `server/api/internal/handler/auth_test.go`: FOUND (committed in c9d34a2)
- File modified `server/api/internal/handler/payment_test.go`: FOUND (committed in c9d34a2)
- File modified `server/api/cmd/createadmin/main_test.go`: FOUND (committed in c9d34a2)
- File modified `server/api/internal/middleware/admin_test.go`: FOUND (committed in c9d34a2)
- File modified `server/api/internal/repository/user_repo_subscription_test.go`: FOUND (committed in c9d34a2)
- File modified `server/api/internal/repository/subscription_repo_test.go`: FOUND (committed in c9d34a2)
- Commit d8c0682: FOUND
- Commit dce3ac4: FOUND
- Commit 21774ed: FOUND
- Commit c9d34a2: FOUND
- Full server/api test suite: green (8/8 packages `ok`, 0 `FAIL`)

---
*Phase: 02-auth-sso-backend*
*Plan: 01 (AUTH-03 — SSO Foundation Schema)*
*Completed: 2026-05-22*
