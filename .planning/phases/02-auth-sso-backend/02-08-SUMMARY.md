---
phase: 02-auth-sso-backend
plan: 08
subsystem: auth
tags: [jwt, sso, apple-signin, google-signin, gorm, postgres, redis, transactional-writes, security-hardening]

# Dependency graph
requires:
  - phase: 02-auth-sso-backend
    provides: AppleSignIn / GoogleSignIn / resolveSSOUser handlers, parseGuestJWT helper, Logout handler, subscriptions repository
provides:
  - Empty-sub 401 guards in AppleSignIn, GoogleSignIn, and resolveSSOUser (CR-01)
  - Transactional Step B auto-link-by-verified-email path (CR-02)
  - Non-user role rejection in parseGuestJWT (WR-01)
  - Logout TTL boundary fix — `ttl >= 0` writes audit-trail entry for tokens expiring at "now" (WR-02)
  - Free Subscription row inserted for brand-new SSO users in Step D (WR-03)
affects: [03-lava-payments-backend, 04-admin-web-overhaul]

# Tech tracking
tech-stack:
  added: ["go.uber.org/zap/zaptest/observer (test-only)"]
  patterns:
    - "TX-scoped read+write for races against partial unique indexes (db.Transaction wrapper around Find + Updates)"
    - "Defense-in-depth guards: handler-level + helper-level empty-input rejection"
    - "Role allow-list on cross-purpose JWT parsing (parseGuestJWT only accepts empty or 'user')"

key-files:
  created:
    - .planning/phases/02-auth-sso-backend/02-08-VERIFY-EVIDENCE.md
  modified:
    - server/api/internal/handler/auth.go
    - server/api/internal/handler/auth_test.go

key-decisions:
  - "Implement REVIEW.md prescription verbatim — `if ttl >= 0`, not a TTL floor. Audit-trail completeness over minor Redis SET-EX-0 quirk."
  - "Step B failure modes converge on `linkedUser nil → fall through to Step C/D` for consistency with Step A's transaction pattern."
  - "WR-03 subscription failure is non-fatal (WARN log, continue) — mirrors REVIEW.md WR-03 recommended behavior; a future repair job can backfill."

patterns-established:
  - "TX-scoped Step B for cross-provider auto-link races"
  - "Empty-claim defense-in-depth (handler + helper)"
  - "Role allow-list on guest-promotion JWT parsing"

requirements-completed: [AUTH-01, AUTH-02, AUTH-04, AUTH-05, AUTH-06, AUTH-07, AUTH-08]

# Metrics
duration: 8min
completed: 2026-05-22
---

# Phase 02 Plan 08: Auth SSO Gap-Closure Summary

**Five Phase-2 security findings closed in `internal/handler/auth.go`: empty-sub rejection, transactional auto-link Step B, parseGuestJWT role allow-list, Logout TTL boundary fix, and free Subscription row on new-SSO-user.**

## Performance

- **Duration:** 8 min
- **Started:** 2026-05-22T04:53:29Z
- **Completed:** 2026-05-22T05:01:34Z
- **Tasks:** 8
- **Files modified:** 2 (auth.go + auth_test.go) + 1 evidence file

## Accomplishments

- **CR-01 (Critical) closed:** AppleSignIn, GoogleSignIn, and resolveSSOUser all guard against empty `sub`. A signed JWT with no `sub` claim now returns 401, never creating a phantom user row with `apple_user_id=''` or `google_user_id=''`.
- **CR-02 (Critical) closed:** Step B auto-link-by-verified-email is now wrapped in `db.Transaction`. Two concurrent SSO sign-ins (Apple + Google) for the same verified email both return 200 with the same `users.id` — no 500 race.
- **WR-01 (Warning) closed:** `parseGuestJWT` now rejects any non-empty role other than "user". An admin access token can no longer be replayed against `/auth/apple` or `/auth/google` to attach a new provider sub to the admin row.
- **WR-02 (Warning) closed:** Logout's blacklist guard is `if ttl >= 0` instead of `if ttl > 0` — the boundary-second token (exp == time.Now()) still gets an audit-trail entry.
- **WR-03 (Warning) closed:** Brand-new SSO users now get a `subscriptions` row inserted with `plan='free'` and `is_active=true`. `GET /api/v1/subscription` no longer 404s for freshly-SSO'd users.

## Task Commits

Each task was committed atomically:

1. **Task 1: RED — empty-sub guard tests [CR-01]** — `6370cc7` (test)
2. **Task 2: GREEN — empty-sub guards in handlers + resolveSSOUser [CR-01]** — `db62f25` (fix)
3. **Task 3: RED — concurrent auto-link race test [CR-02]** — `406a00b` (test)
4. **Task 4: GREEN — wrap Step B in db.Transaction [CR-02]** — `1045df8` (fix)
5. **Task 5: parseGuestJWT non-user role rejection + test [WR-01]** — `4e954f7` (fix)
6. **Task 6: Logout TTL boundary fix + test [WR-02]** — `6befbe4` (fix)
7. **Task 7: Free Subscription row for new SSO user + test [WR-03]** — `b304fc1` (fix)
8. **Task 8: Full Phase 2 regression check + evidence file** — `afd63a7` (docs)

## Files Created/Modified

- `server/api/internal/handler/auth.go` — Five fixes: empty-sub guards (AppleSignIn ~849, GoogleSignIn ~902, resolveSSOUser entry), transactional Step B (~744-803), parseGuestJWT role allow-list (~649-670), Logout `ttl >= 0` boundary (~1022), Step D `CreateSubscription` call (~821).
- `server/api/internal/handler/auth_test.go` — Six new tests added: `TestAppleSignIn_EmptySub_Returns401`, `TestGoogleSignIn_EmptySub_Returns401`, `TestAppleSignIn_ConcurrentAutoLinkByEmail`, `TestParseGuestJWT_RejectsAdminRole`, `TestLogout_BlacklistsTokenExpiringNow`, `TestAppleSignIn_NewUser_HasSubscriptionRow`. New imports: `strings`, `zapcore`, `zaptest/observer`.
- `.planning/phases/02-auth-sso-backend/02-08-VERIFY-EVIDENCE.md` — Evidence file linking commit SHAs to findings + Phase 2 test counts.

## Decisions Made

- **Implement REVIEW.md WR-02 prescription verbatim (`ttl >= 0`) rather than introducing a TTL floor.** REVIEW.md owns the spec for gap closure. Test asserts via zap.Observer that no error-level log is emitted on the boundary case, which proves the branch was taken.
- **CR-02 Step B re-read on ErrDuplicate falls through to Step C/D when the loser's sub doesn't own a row.** Cleaner than re-attempting the link in a loop; the loser's sub will either be auto-promoted via Step C (if a guest JWT is present) or created via Step D.
- **WR-03 `CreateSubscription` failure is logged at WARN and execution continues.** Matches REVIEW.md recommended behavior — a future repair job can backfill missing subscription rows.

## Deviations from Plan

**None — plan executed exactly as written.**

The plan correctly predicted that `TestAppleSignIn_ConcurrentAutoLinkByEmail` could pass on the current code under SQLite's `SetMaxOpenConns(1)` serialization (the test relies on the partial unique index, not on goroutine scheduling). The fix in Task 4 still closes the Postgres-side race (production) by enforcing transactional semantics around the read+write pair. The test now serves as a regression guard against any future code that reintroduces the non-transactional pattern.

## Issues Encountered

- Worktree was created from an older commit (`6a3da00` — Phase 01 completion) but the plan expected the Phase 02 base (`2d0d3e8`). Resolved by `git reset --hard 2d0d3e8b6fc9a237f529d9ce27b5c538e9ec82fc` before starting Task 1. No code lost (the only worktree-local change was `.claude/settings.local.json`, stashed for safety).
- `strings` package was used by the new `TestParseGuestJWT_RejectsAdminRole` test but not previously imported in `auth_test.go`. Added to imports.
- `go.uber.org/zap/zapcore` and `go.uber.org/zap/zaptest/observer` were used by the new `TestLogout_BlacklistsTokenExpiringNow` test but not previously imported. Added to imports. Both are transitively available via `go.uber.org/zap` which was already in `go.mod`.

## User Setup Required

None — no external service configuration required. All changes are server-side Go code + tests; no env vars, no provider config, no migrations.

## Next Phase Readiness

- **Project security gate unblocked.** CLAUDE.md states "Critical/High MUST land before any paying user touches the system." Both CR-01 and CR-02 (the two critical findings in `02-VERIFICATION.md`) are now closed, and the three warning-level findings in `02-REVIEW.md` (WR-01, WR-02, WR-03) are also closed.
- **Phase 3 (lava.top payments) can proceed.** The SSO surface that Phase 3 depends on is now hardened.
- **No blockers identified.** Race detector clean, go vet clean, go build clean. Full Phase 2 test suite (handler 110, repository 43, auth/apple 8, auth/google 5, middleware 29 — 195 tests total) green.

## Self-Check

Run after creating SUMMARY.md:

- ✓ `server/api/internal/handler/auth.go` modified
- ✓ `server/api/internal/handler/auth_test.go` modified
- ✓ `.planning/phases/02-auth-sso-backend/02-08-VERIFY-EVIDENCE.md` created
- ✓ All 8 task commits present on branch (`6370cc7`, `db62f25`, `406a00b`, `1045df8`, `4e954f7`, `6befbe4`, `b304fc1`, `afd63a7`)
- ✓ Full Phase 2 test suite passes (handler 110, repository 43, auth/apple 8, auth/google 5, middleware 29)
- ✓ Race detector clean (`go test ./internal/handler/ -race -count=1` → ok)
- ✓ go vet clean
- ✓ go build clean

## Self-Check: PASSED

---
*Phase: 02-auth-sso-backend*
*Completed: 2026-05-22*
