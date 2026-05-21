---
phase: 01-hotfix-audit-critical-fixes
plan: 04
subsystem: api/middleware
tags: [hotfix, security, authorization, middleware, gorm, fiber]
requires:
  - 01-03 (ErrorHandler scrub + X-Request-ID — `return err` from AdminRequired propagates here)
provides:
  - "middleware.AdminRequired(db *gorm.DB) fiber.Handler that re-reads role from DB per admin request"
affects:
  - server/api/cmd/main.go (admin route group signature)
tech_stack:
  added: []
  patterns:
    - "Per-request DB PK lookup in security-critical middleware (no cache) when correctness > latency"
    - "Sqlite in-memory test DB for middleware behavior (mirrors subscription_repo_test.go)"
key_files:
  created: []
  modified:
    - server/api/internal/middleware/admin.go
    - server/api/internal/middleware/admin_test.go
    - server/api/cmd/main.go
decisions:
  - "Pure DB read per admin request (no Redis cache, no TTL). Criterion #1 demands 'very next request, not five minutes later' — any TTL > 0 would violate it. Admin traffic is bounded (< tens of req/min); PERF-04 in Phase 6 will revisit caching once the system grows."
  - "Deleted-user-during-session returns 401 (not 403) so the client's refresh path fires and the client recovers via /auth/guest fallback rather than getting stuck on a permanent 403."
  - "Dropped TestAdminRequired_RejectsEmptyRole. Under the new architecture role comes from the live DB row, not c.Locals, so an 'empty role' input no longer exists. The semantic replacement is TestAdminRequired_EmptyLocals (asserts 401 when c.Locals('user_id') is empty)."
metrics:
  duration: ~20 minutes
  completed: "2026-05-22"
  tasks: 2
  commit_count: 1
requirements_completed: [HOTFIX-02]
threats_mitigated: [T-1-02]
---

# Phase 01 Plan 04: HOTFIX-02 — AdminRequired re-reads role from DB Summary

**One-liner:** AdminRequired middleware now performs `repository.FindUserByID` on every admin request and authorizes against `model.User.Role` from the live DB row, so admin demotion takes effect on the very next admin request instead of waiting for JWT expiry.

## What landed

A single atomic commit on `main`:

| SHA       | Subject |
| --------- | ------- |
| `204b80f` | `hotfix(01): AdminRequired re-reads role from DB per request [HOTFIX-02]` |

### Files modified (3)

- `server/api/internal/middleware/admin.go` — full rewrite. Signature changed from `AdminRequired() fiber.Handler` to `AdminRequired(db *gorm.DB) fiber.Handler`. Behavior:
  1. `userID, _ := c.Locals("user_id").(string)`; empty → 401 `{"error":"unauthorized"}`.
  2. `user, err := repository.FindUserByID(db, userID)`.
  3. `errors.Is(err, repository.ErrNotFound)` → 401 `{"error":"user no longer exists"}`.
  4. Other err → `return err` (propagates to HOTFIX-04 ErrorHandler → scrubbed 500 + X-Request-ID).
  5. `user.Role != "admin"` → 403 `{"error":"forbidden"}`.
  6. Else → `c.Next()`.
- `server/api/internal/middleware/admin_test.go` — rewritten to use sqlite in-memory DB. Existing tests (`AllowsAdminRole`, `RejectsUserRole`, `RejectsArbitraryRole`, `ResponseBodyContainsErrorKey`) updated to seed real user rows with the desired role and inject `c.Locals("user_id", user.ID)` via a faux-auth middleware. Three new HOTFIX-02 tests added: `DemotionTakesEffect`, `DeletedUserDuringSession`, `EmptyLocals`. (`RejectsEmptyRole` from the previous file was dropped — see "Deviations" below.)
- `server/api/cmd/main.go:218` — admin route group now passes `db`: `middleware.AdminRequired(db)`.

## Test output

`cd server/api && go test ./internal/middleware/... -v -count=1 -run TestAdminRequired_`:

```
=== RUN   TestAdminRequired_AllowsAdminRole
--- PASS: TestAdminRequired_AllowsAdminRole (0.00s)
=== RUN   TestAdminRequired_RejectsUserRole
--- PASS: TestAdminRequired_RejectsUserRole (0.00s)
=== RUN   TestAdminRequired_RejectsArbitraryRole
--- PASS: TestAdminRequired_RejectsArbitraryRole (0.00s)
=== RUN   TestAdminRequired_ResponseBodyContainsErrorKey
--- PASS: TestAdminRequired_ResponseBodyContainsErrorKey (0.00s)
=== RUN   TestAdminRequired_DemotionTakesEffect
--- PASS: TestAdminRequired_DemotionTakesEffect (0.00s)
=== RUN   TestAdminRequired_DeletedUserDuringSession
--- PASS: TestAdminRequired_DeletedUserDuringSession (0.00s)
=== RUN   TestAdminRequired_EmptyLocals
--- PASS: TestAdminRequired_EmptyLocals (0.00s)
PASS
ok  	vpnapp/server/api/internal/middleware	0.840s
```

Full middleware suite (regression check, `go test ./internal/middleware/... -count=1`): `ok vpnapp/server/api/internal/middleware 2.517s`.

`cd server/api && go build -o /tmp/vpnapi-build ./cmd` → exits 0.

## Verification gates (from plan's `<verification>`)

| # | Gate | Result |
|---|------|--------|
| 1 | `go test ./internal/middleware/... -v -count=1` green | PASS |
| 2 | `go build ./cmd` builds cleanly | PASS |
| 3 | `grep -q 'AdminRequired(db' server/api/cmd/main.go` | PASS |
| 4 | `grep -q 'FindUserByID' server/api/internal/middleware/admin.go && ! grep -qE 'c\.Locals\(.role.\)' server/api/internal/middleware/admin.go` | PASS |
| 5 | `! grep -q 'redis' server/api/internal/middleware/admin.go` (discretion: no caching) | PASS |
| 6 | `git log -1 --format=%s` matches `^hotfix\(01\): .*HOTFIX-02` | PASS |

## Confirmation of plan invariants

- **Pre-existing admin_test.go callers updated.** The four previously-existing tests in admin_test.go (`AllowsAdminRole`, `RejectsUserRole`, `RejectsArbitraryRole`, `ResponseBodyContainsErrorKey`) were each rewritten to construct an in-memory `*gorm.DB`, seed a row with the desired role, and call `middleware.AdminRequired(db)`. Diff confirms no `middleware.AdminRequired()` no-arg call sites remain:

  ```
  $ grep -nE 'middleware\.AdminRequired\(\)' server/api/internal/middleware/admin_test.go
  (no matches)
  $ grep -nE 'middleware\.AdminRequired\(' server/api/internal/middleware/admin_test.go
  85:	}, middleware.AdminRequired(db), func(c *fiber.Ctx) error {
  ```

  (Only one call site because all tests share `newAdminApp(db, userID)`.)

- **No Redis import added to admin.go.** Confirmed via `! grep -q 'redis' server/api/internal/middleware/admin.go`. Imports are limited to `errors`, the project repository package, `github.com/gofiber/fiber/v2`, and `gorm.io/gorm`. `go.uber.org/zap` is not imported (RESEARCH noted it was optional and the implementation doesn't use the logger directly — errors propagate to the global ErrorHandler from HOTFIX-04, which has its own zap logger).

- **Threat T-1-02 (Elevation of Privilege) mitigated.** `TestAdminRequired_DemotionTakesEffect` is the on-disk proof: status flips 200 → 403 with the same `c.Locals("user_id")` after `UPDATE users SET role='user'`. Staging smoke (plan 09 step 4) will repeat this against the live DB.

## Deviations from Plan

### Rule 1 — Bug-style adjustment (tightened test set)

**1. [Rule 1 — drop dead test] Removed `TestAdminRequired_RejectsEmptyRole`**

- **Found during:** Task 2 Step C (updating existing tests to new signature).
- **Issue:** The pre-existing `TestAdminRequired_RejectsEmptyRole` injected `c.Locals("role", "")` and asserted 403. After HOTFIX-02 there is no such code path — AdminRequired no longer reads `c.Locals("role")` at all. The only way to construct an "empty role" scenario is to seed a user with `Role=""`, which is impossible in production (GORM `default:user` ensures every row has a role).
- **Fix:** Deleted that test and added `TestAdminRequired_EmptyLocals` instead, which exercises the truly empty input the new code can see: `c.Locals("user_id")` not set (AuthRequired didn't run / token malformed). This asserts 401 (not 403), matching the new behavior and the plan's acceptance criteria.
- **Files modified:** `server/api/internal/middleware/admin_test.go`
- **Commit:** `204b80f` (same atomic commit per D-01)

This stayed inside D-01 (single atomic commit per hotfix) and inside `files_modified`. No new packages touched.

### Auth gates

None — this plan is pure-code; no external auth or secrets needed.

## Self-Check: PASSED

- File `server/api/internal/middleware/admin.go` exists: FOUND
- File `server/api/internal/middleware/admin_test.go` exists: FOUND
- File `server/api/cmd/main.go` exists: FOUND
- Commit `204b80f` exists in `git log --oneline --all`: FOUND
- Commit subject matches `^hotfix\(01\): .*HOTFIX-02`: FOUND
- All 7 `TestAdminRequired_*` tests PASS (4 pre-existing + 3 new): FOUND
- `cd server/api && go build -o /tmp/vpnapi-build ./cmd`: exits 0
- No Redis import in admin.go: confirmed via grep
- No `c.Locals("role")` reads in admin.go: confirmed via grep
