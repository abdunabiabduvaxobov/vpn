---
phase: 02-auth-sso-backend
plan: 04
subsystem: auth
tags: [sso, repository, gorm, sqlite-tests, account-linking, guest-promotion, logout, conflict-branch]

# Dependency graph
requires:
  - phase: 02-auth-sso-backend
    provides: User struct SSO fields (AppleUserID / GoogleUserID / Email / EmailVerified / EmailIsPrivateRelay / AuthProvider) + migration 018 partial unique indexes on apple_user_id and google_user_id + idx_users_email_verified partial index — established by plan 02-01
  - phase: 02-auth-sso-backend
    provides: API contract docs for /auth/apple, /auth/google, /auth/logout — established by plan 02-07 (Wave 1)
provides:
  - repository.FindUserByAppleID(db, sub) (*User, error) — provider-sub lookup
  - repository.FindUserByGoogleID(db, sub) (*User, error) — structural twin
  - repository.FindUserByVerifiedEmailForLink(db, email) (*User, error) — auto-link candidate with built-in private-relay exclusion
  - repository.PromoteGuestToSSO(db, guestID, sub, email, provider, isPrivateRelay) error — TX-wrapped guest-row promotion, surfaces UNIQUE collisions as ErrDuplicate
  - repository.DeleteUserSessions(db, userID) (int64, error) — multi-row sibling of DeleteSession for the Logout handler
  - repository.ReassignDevicesByUserID(db, oldUserID, newUserID) (int64, error) — multi-device sibling of ReassignDeviceUser for the D-06 conflict-branch transaction
affects: [02-05 sso-handlers (consumes all 6 functions), 02-06 logout-handler (consumes DeleteUserSessions)]

# Tech tracking
tech-stack:
  added: []  # No new libraries — repository functions only
  patterns:
    - "Passive readers + single TX-wrapped writer: D-29 boundary made explicit at the repository layer — only PromoteGuestToSSO is wrapped in db.Transaction; race detection (FindOrCreate-then-ReadOnConflict) lives in the handler, not the repo."
    - "Defense-in-depth provider whitelist inside PromoteGuestToSSO — refuses any provider that isn't 'apple' or 'google', even though the handler dispatches on verifier output. Prevents silent data corruption if a future caller misuses the function."
    - "Multi-row UPDATE as idempotency primitive — ReassignDevicesByUserID's WHERE matches zero rows on the second call, so the D-06 conflict-branch transaction is safely re-runnable."
    - "Parameterized GORM clauses throughout — `Where(\"col = ?\", val)` not string concat — T-2-SQLi defense applied even though provider subs come from trusted verifiers."
    - "Test-DB schema parity with production migration: SQLite CREATE TABLE + two partial unique indexes mirror migration 018 so duplicate-sub semantics exercise the same constraint as Postgres."

key-files:
  created:
    - server/api/internal/repository/user_repo_sso_test.go
  modified:
    - server/api/internal/repository/user_repo.go
    - server/api/internal/repository/session_repo.go
    - server/api/internal/repository/device_repo.go

key-decisions:
  - "Devices table test schema matches production shape (id PK + device_id UNIQUE) rather than the plan's stub (device_id as PK). The plan-text snippet would have made GORM Updates against model.Device{} mismatch column names; matching internal/handler/auth_test.go's proven schema kept the GORM mapping consistent and the seed/assertion helpers correct. Rule-1 fix scoped to test code."
  - "Test helper names suffixed with _SSO_ / 'ssoStrPtr' / 'seedGuestSSO' / 'seedSSOUser' / 'seedSSODevice' / 'seedSSOSession' to avoid colliding with helper names in sibling _test.go files in the same package_test (Go test files share package symbol space)."
  - "Did NOT introduce gorm.io/gorm/clause OnConflict semantics in PromoteGuestToSSO — keeping the simpler errors.Is(err, ErrDuplicate) pattern per RESEARCH.md §Account-Linking Race Condition recommendation. Plan 05's handler owns the race-detection composition."
  - "Provider validation done up-front (before opening the transaction) so invalid-provider failures never start a TX they immediately roll back. Cheaper and clearer in logs."

patterns-established:
  - "Repository functions return one of: nil, sentinel error (ErrNotFound / ErrDuplicate), or wrapped DB error. PromoteGuestToSSO documents all three exit paths so handler-side switch statements can be exhaustive."
  - "When a new helper name in a _test.go file might collide with an existing helper in another _test.go in the same package_test (e.g. seedSession, newTestDB, strPtr), suffix the new helper with a feature-scoped prefix/suffix (here: SSO) rather than renaming the existing one. Minimizes blast radius."

requirements-completed: [AUTH-04, AUTH-05, AUTH-06, AUTH-08]

# Metrics
duration: 5m 42s
completed: 2026-05-22
---

# Phase 2 Plan 04: Repository Layer for AUTH-04 / AUTH-05 / AUTH-06 + AUTH-08 Summary

**Six repository functions across three files + 16 SQLite-:memory: unit tests. The data-layer primitives plan 05's SSO handler and plan 06's Logout handler will compose. Pure data layer — no Fiber, no business logic, no JWT minting. D-06 conflict-branch transaction (ReassignDevicesByUserID + DeleteOrphanGuestUser) now unblocked at the repo layer.**

## Performance

- **Duration:** 5m 42s
- **Started:** 2026-05-22T03:08:39Z
- **Completed:** 2026-05-22T03:14:21Z
- **Tasks:** 4 (all atomic per D-37)
- **Files modified/created:** 4 (3 modified Go files + 1 new test file)
- **Tests added:** 16 (all green, all green under `-race`)

## Accomplishments

- **AUTH-04** (provider-id lookup): `FindUserByAppleID` + `FindUserByGoogleID` query the partial-unique-index-backed `apple_user_id` / `google_user_id` columns; both use `errors.Is(err, gorm.ErrRecordNotFound)` translation to `repository.ErrNotFound`.
- **AUTH-05** (guest-promotion): `PromoteGuestToSSO` is the single TX-wrapped writer per D-29; surfaces UNIQUE collisions as `ErrDuplicate` for plan-05's handler-side race-detection composition; refuses invalid providers up-front.
- **AUTH-05** (D-06 conflict branch): `ReassignDevicesByUserID` is the multi-row sibling of `ReassignDeviceUser`. Plan 05's `resolveSSOUser` Step A wraps it in `db.Transaction({ReassignDevicesByUserID + DeleteOrphanGuestUser})` for atomic guest-row cleanup when a different row already owns the provider sub.
- **AUTH-06** (auto-link by verified email): `FindUserByVerifiedEmailForLink` enforces `email_verified=TRUE AND email_is_private_relay=FALSE` in its WHERE — the single point of D-03 / D-04 enforcement. Index-supported by `idx_users_email_verified` from migration 018.
- **AUTH-08** (logout repo dependency): `DeleteUserSessions(db, userID) (int64, error)` is the multi-row sibling of the existing `DeleteSession`. Plan 06 owns the user-facing Logout endpoint behaviour; plan 04 contributes the repo dependency (W-5 contribution split).
- **T-2-RelaySkip mitigation verified**: `TestFindUserByVerifiedEmailForLink_PrivateRelayExcluded` seeds a relay row and asserts `ErrNotFound` — defends against the auto-link hijack via private-relay address described in the threat model.
- **T-2-Promotion mitigation verified**: `TestPromoteGuestToSSO_HappyPath_Apple` reads back all four columns to confirm atomic four-column update inside the TX.
- **T-2-RaceLink mitigation verified**: `TestPromoteGuestToSSO_DuplicateSub_ReturnsErrDuplicate` exercises the partial-unique-index collision path through the `isDuplicateError` helper.
- **T-2-SQLi mitigation verified**: all four new repo functions use parameterized GORM WHERE clauses; grep proves zero string-concatenated WHERE.
- Full `server/api/internal/repository` suite green under `-count=1 -race`.

## Task Commits

Each task committed atomically per D-37:

1. **Task 1: SSO functions in user_repo.go** — `0fccdf8` (feat) — `feat(02-04): user_repo SSO functions [AUTH-04,05,06]`
2. **Task 2: DeleteUserSessions in session_repo.go** — `cfc0e2f` (feat) — `feat(02-04): session_repo.DeleteUserSessions [AUTH-08]`
3. **Task 3: ReassignDevicesByUserID in device_repo.go** — `d253bd9` (feat) — `feat(02-04): device_repo.ReassignDevicesByUserID — multi-device variant for D-06 conflict branch [AUTH-05]`
4. **Task 4: Sixteen-test SSO unit suite** — `e334403` (test) — `test(02-04): user_repo SSO unit tests [AUTH-04,05,06,08]`

## Files Created/Modified

### Created

- `server/api/internal/repository/user_repo_sso_test.go` — 16 tests + helpers (`newSSOTestDB`, `seedGuestSSO`, `seedSSOUser`, `seedSSODevice`, `seedSSOSession`, `ssoStrPtr`). Schema mirrors migration 018 (six SSO columns + two partial unique indexes); devices table shape matches `internal/handler/auth_test.go` (id PK + device_id UNIQUE).

### Modified

- `server/api/internal/repository/user_repo.go` — appended four SSO functions after `UpdateUserName`. Existing `fmt` import is reused (already in the file via `DowngradeExpiredSubscriptions`). +98 lines.
- `server/api/internal/repository/session_repo.go` — appended `DeleteUserSessions` after `DeleteExpiredSessions`. +17 lines.
- `server/api/internal/repository/device_repo.go` — appended `ReassignDevicesByUserID` after `DeleteStaleDevices`. Reuses existing `time` import. +24 lines.

## Decisions Made

- **Devices test-DB schema follows production shape** (id PK + device_id UNIQUE), not the plan stub (device_id as PK). The stub would have confused GORM's `Updates(&model.Device{})` column mapping; matching `internal/handler/auth_test.go` keeps every helper aligned with the live model. Documented above; treated as a Rule-1 fix scoped to test code only.
- **Test helper names scoped with `SSO` suffix** (`newSSOTestDB`, `seedSSOUser`, `ssoStrPtr`, …) to avoid colliding with helpers in sibling `_test.go` files inside the same `repository_test` package (e.g., `user_repo_subscription_test.go` already declares helpers in the same package scope). Cheaper than renaming existing helpers.
- **Provider validation before TX open in `PromoteGuestToSSO`** — invalid-provider errors never start a transaction they immediately roll back. Clearer logs, no wasted connection round-trip.
- **Did not adopt `gorm.io/gorm/clause.OnConflict` semantics** — the simpler `errors.Is(err, ErrDuplicate)` pattern per RESEARCH.md §Account-Linking Race Condition keeps the plan-05 handler readable and avoids importing the GORM clause package at this layer. Race-detection composition lives in the handler, not the repo.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Plan-stub devices test schema mismatched production shape**

- **Found during:** Task 4 (writing the test file)
- **Issue:** The plan's verbatim test snippet declared `devices` with `device_id TEXT PRIMARY KEY` and inserted by `device_id`. The production GORM model has `ID string \`gorm:"primaryKey"\`` and `DeviceID string \`gorm:"uniqueIndex"\`` as two distinct columns. Using the plan's stub would have made `ReassignDevicesByUserID` (which calls `Updates(map[string]interface{}{"user_id": …, "last_seen_at": …})` against `model.Device{}`) emit SQL referencing columns that didn't exist in the test schema, and the assertion query `SELECT … WHERE device_id = ?` would have hit the PK column (correct by accident) but mismatched the real production behaviour we're contracting.
- **Fix:** Aligned the devices CREATE TABLE in `newSSOTestDB` with the proven schema from `internal/handler/auth_test.go` (id PK + device_id UNIQUE + the other columns the model uses). Updated `seedSSODevice` to insert both `id` and `device_id`. Adjusted the "device exists exactly once" sanity assertion to query by `id` (the PK) instead of `device_id`.
- **Files modified:** `server/api/internal/repository/user_repo_sso_test.go` (the new file — schema deviation never reached disk)
- **Verification:** All 16 tests pass including `TestReassignDevicesByUserID_MovesAllDevices` (rows-affected = 2; sanity by-id sweep matches).
- **Committed in:** `e334403` (Task 4 commit)

**Total deviations:** 1 Rule-1 fix scoped to test code.
**Impact on plan:** Zero scope creep — same number of tests, same behaviours covered, just a schema definition that wouldn't lie about the production model.

## Acceptance-Criteria Snapshot

| Task | Key checks | Result |
|------|------------|--------|
| 1    | Four function names declared; build clean; `db.Transaction` ≥ 1; `email_is_private_relay = ?` = 1; parameterized WHERE ≥ 4; concat WHERE = 0; `isDuplicateError` ≥ 1; `.First(&user).Error` ≥ 3 | 4 / OK / 1 / 1 / 8 / 0 / 3 / 4 — **all green** |
| 2    | `DeleteUserSessions` declared exactly once; exact signature; parameterized WHERE ≥ 1; build clean; gofmt clean | 1 / 1 / 1 / OK / OK — **all green** |
| 3    | `ReassignDevicesByUserID` declared exactly once; exact signature; parameterized WHERE ≥ 1; `ReassignDeviceUser` untouched; build clean; gofmt clean | 1 / 1 / 3 / 1 / OK / OK — **all green** |
| 4    | Test file exists; 16 test functions declared; all 16 PASS; private-relay test PASS; duplicate-sub test PASS; reassign happy-path PASS; both partial unique indexes in schema; devices table in schema; gofmt clean | OK / 16 / 16 / PASS / PASS / PASS / 1 / 1 / 1 / OK — **all green** |

## Validation Map Coverage

Per `.planning/phases/02-auth-sso-backend/02-VALIDATION.md`:

- **Wave 0 row: `internal/repository/user_repo_test.go`** — covered by `user_repo_sso_test.go` (single new file rather than extending an existing one; the planner's "new or extend" wording explicitly allowed this).
- **W-1 fix row: `ReassignDevicesByUserID` multi-device variant** — covered by `TestReassignDevicesByUserID_MovesAllDevices` and `TestReassignDevicesByUserID_NoDevicesIsNoop` (idempotency).
- **Threat-model rows T-2-RaceLink / T-2-Promotion / T-2-RelaySkip / T-2-EmailLink / T-2-SQLi** — all five mitigations verified via the acceptance-criteria grep checks AND the corresponding behavioural tests (`_DuplicateSub_ReturnsErrDuplicate`, `_HappyPath_Apple` four-column readback, `_PrivateRelayExcluded`, `_HappyPath` with `EmailVerified/EmailIsPrivateRelay` assertions, and parameterized-WHERE grep).

## Issues Encountered

- None substantive. The plan was tight and well-specified; the only deviation was the devices test-schema shape (auto-fixed inline).

## User Setup Required

None — pure backend repository code. No env vars, no migrations, no operational rollout. Plan 02-01 (the migration this work depends on) has its own operational notes about the six required SSO env vars.

## Next Phase Readiness

**Unblocked for plan 02-05 (SSO handlers):**
- `resolveSSOUser` (Apple/Google handlers) can now compose:
  - `FindUserByAppleID(db, sub)` / `FindUserByGoogleID(db, sub)` for the "row already owns this sub" branch.
  - `FindUserByVerifiedEmailForLink(db, email)` for the auto-link branch (relay-excluded by the function's own WHERE, no caller-side flag check needed).
  - `PromoteGuestToSSO(db, guestID, sub, email, provider, isPrivateRelay)` for the guest-row promotion branch.
  - `db.Transaction(func(tx *gorm.DB) error { ReassignDevicesByUserID(tx, guestID, existingID); return DeleteOrphanGuestUser(tx, guestID) })` for the **D-06 conflict-branch** (B-3 fix) when promotion collides with an existing SSO-bound row.
  - `errors.Is(err, ErrDuplicate)` on the `CreateUser` race path per RESEARCH.md §Account-Linking Race Condition.

**Unblocked for plan 02-06 (Logout handler):**
- `DeleteUserSessions(db, userID)` for the "delete all sessions for this user" Discretion default.

**Carry-over notes:**
- Existing single-device `ReassignDeviceUser(db, deviceID, newUserID, platform, model, secretHash)` continues to power the share-code link flow; the new multi-device variant is purely additive.
- Test schema in `user_repo_sso_test.go` does NOT include all production users columns (omits the foreign keys and a few subscription-side columns) because the functions under test don't touch them. If plan 05's handler tests reuse this helper, they may need to extend the schema — but plan 05 will likely extend `internal/handler/auth_test.go::newAuthTestDB` (which is already migration-018-aware) instead.

## Self-Check: PASSED

- File created `server/api/internal/repository/user_repo_sso_test.go`: FOUND
- File modified `server/api/internal/repository/user_repo.go`: FOUND (committed in 0fccdf8)
- File modified `server/api/internal/repository/session_repo.go`: FOUND (committed in cfc0e2f)
- File modified `server/api/internal/repository/device_repo.go`: FOUND (committed in d253bd9)
- Commit 0fccdf8: FOUND
- Commit cfc0e2f: FOUND
- Commit d253bd9: FOUND
- Commit e334403: FOUND
- All 16 SSO tests pass (`go test ./internal/repository/... -count=1 -v -run 'TestFindUserByAppleID|TestFindUserByGoogleID|TestFindUserByVerifiedEmailForLink|TestPromoteGuestToSSO|TestDeleteUserSessions|TestReassignDevicesByUserID' | grep -c '\-\-\- PASS:'` = 16)
- Full repository suite green under `-race`: `ok vpnapp/server/api/internal/repository`

---
*Phase: 02-auth-sso-backend*
*Plan: 04 (AUTH-04 / AUTH-05 / AUTH-06 / AUTH-08 — Repository Layer)*
*Completed: 2026-05-22*
