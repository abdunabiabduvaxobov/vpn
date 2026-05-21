---
phase: 01-hotfix-audit-critical-fixes
plan: 06
subsystem: server/api/auth
tags: [hotfix, security, gorm, transactions, refresh-token, audit-s1-1]
dependency_graph:
  requires: [01-hotfix-audit-critical-fixes/05]
  provides: [transactional-refresh-rotation]
  affects: [server/api/internal/handler/auth.go]
tech_stack:
  added: []
  patterns: ["db.Transaction(func(tx *gorm.DB) error)"]
key_files:
  created: []
  modified:
    - server/api/internal/handler/auth.go
    - server/api/internal/handler/auth_test.go
decisions:
  - "Reuse existing storeRefreshSession helper (already takes *gorm.DB; tx satisfies the interface). No body duplication."
  - "Distinguish repository.ErrNotFound (401, client re-authenticates) from other errors (500, client retries) after the closure returns."
  - "Tests inject a GORM before-create callback on the sessions table to force CreateSession failure mid-tx (deterministic, no UNIQUE-collision math)."
  - "Test for the user-deleted-during-rotation case deletes the user row up front; FindUserByID(tx, ...) inside the closure returns ErrNotFound naturally — closer to the real race than a synthetic callback hook."
metrics:
  duration_seconds: 480
  tasks_completed: 2
  files_modified: 2
  commit_hash: 2f6d86b
  completed_date: 2026-05-22
requirements: [HOTFIX-05]
threat_refs: [T-1-05]
---

# Phase 1 Plan 6: HOTFIX-05 — Transactional refresh-token rotation Summary

Wrap the refresh-token rotation in `handler/auth.go` (DeleteSession → FindUserByID → generateTokens → storeRefreshSession) inside a single `db.Transaction(func(tx *gorm.DB) error)` closure so a failed insert never leaves the user with a valid refresh token whose backing session row was deleted. Closes SECURITY-AUDIT S1-1.

## What Changed

### `server/api/internal/handler/auth.go` (RefreshToken handler, lines ~240–298)

**Before** — four sequential ops on the global `db` handle, with the final `storeRefreshSession` failure swallowed by a `logger.Error` call:

```go
repository.DeleteSession(db, session.ID)                    // return value IGNORED
user, err := repository.FindUserByID(db, session.UserID)    // 401 on miss
tokens, err := generateTokens(...)                          // 500 on err
if err := storeRefreshSession(db, ...); err != nil {        // logged but TOKENS STILL RETURNED
    logger.Error("failed to store session", zap.Error(err))
}
return c.JSON(fiber.Map{"data": tokens})
```

The bug: on `storeRefreshSession` failure the original session row was already deleted AND the client received new tokens whose refresh half had no backing row → the next `/auth/refresh` would 401 → silent log-out from any transient DB blip.

**After** — single `db.Transaction` closure; all four ops use `tx`; tokens populate an outer-scoped variable only after the closure returns nil; ErrNotFound is distinguished from other errors after the transaction returns:

```go
var tokens *authResponse
err = db.Transaction(func(tx *gorm.DB) error {
    if err := repository.DeleteSession(tx, session.ID); err != nil {
        return fmt.Errorf("deleting old session: %w", err)
    }
    user, err := repository.FindUserByID(tx, session.UserID)
    if err != nil {
        return fmt.Errorf("loading user: %w", err)
    }
    newTokens, err := generateTokens(user.ID, user.SubscriptionTier, user.Role, user.FullName, cfg.JWTSecret)
    if err != nil {
        return fmt.Errorf("generating tokens: %w", err)
    }
    if err := storeRefreshSession(tx, user.ID, newTokens.RefreshToken); err != nil {
        return fmt.Errorf("storing new session: %w", err)
    }
    tokens = newTokens
    return nil
})
if err != nil {
    if errors.Is(err, repository.ErrNotFound) {
        return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "user not found"})
    }
    logger.Error("refresh rotation failed", zap.Error(err))
    return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
}
return c.JSON(fiber.Map{"data": tokens})
```

### `server/api/internal/handler/auth_test.go`

Added three integration tests (sqlite in-memory, reusing the existing `newAuthTestDB` helper):

1. **`TestRefreshToken_RollbackOnInsertFailure`** — registers a `before:gorm:create` callback that fails on `sessions` inserts. After the failed refresh the **original** session row count is still 1 and its hash is unchanged. Without the transactional wrap this would be 0 → user silently locked out.
2. **`TestRefreshToken_HappyPath`** — no failure injected. Refresh returns 200 with a token pair; exactly one session row remains and its hash differs from the original (proving rotation actually happened, not a silent no-op).
3. **`TestRefreshToken_UserDeletedDuringRotation`** — deletes the user row before refresh. Closure's `FindUserByID(tx, ...)` returns `ErrNotFound`; handler responds 401 with `"user not found"` and the prior DeleteSession is rolled back so the session row count stays at 1.

Two helpers added: `seedRefreshSessionForUser`, `countSessions`, `sessionHashFor` — all package-private to the test file.

## Verification Evidence

**Test output:**
```
=== RUN   TestRefreshToken_RollbackOnInsertFailure
--- PASS: TestRefreshToken_RollbackOnInsertFailure (0.00s)
=== RUN   TestRefreshToken_HappyPath
--- PASS: TestRefreshToken_HappyPath (0.00s)
=== RUN   TestRefreshToken_UserDeletedDuringRotation
--- PASS: TestRefreshToken_UserDeletedDuringRotation (0.00s)
PASS
ok      vpnapp/server/api/internal/handler      0.675s
```

**Closure uses `tx` everywhere — grep evidence:**
```
253:    err = db.Transaction(func(tx *gorm.DB) error {
254:        if err := repository.DeleteSession(tx, session.ID); err != nil {
260:        user, err := repository.FindUserByID(tx, session.UserID)
274:        if err := storeRefreshSession(tx, user.ID, newTokens.RefreshToken); err != nil {
```

No occurrences of `repository.DeleteSession(db,` / `repository.FindUserByID(db,` / `storeRefreshSession(db,` remain inside the `RefreshToken` handler — only `tx`. (The `db` form does appear in the `AdminLogin` and `GuestLogin` handlers; those are intentionally untouched per plan scope.)

**Full Go test suite green:**
```
ok      vpnapp/server/api/cmd/createadmin       3.926s
ok      vpnapp/server/api/internal/cache        9.528s
ok      vpnapp/server/api/internal/config       1.272s
ok      vpnapp/server/api/internal/handler      2.426s
ok      vpnapp/server/api/internal/middleware   4.044s
ok      vpnapp/server/api/internal/recovery     2.361s
ok      vpnapp/server/api/internal/repository   2.833s
ok      vpnapp/server/api/internal/scheduler    3.426s
```

**Binary builds:** `go build -o /dev/null ./cmd` exits 0.

**Commit:** `2f6d86b` — `hotfix(01): transactional refresh-token rotation [HOTFIX-05]`

## Threat Model Status

| Threat | Disposition | Evidence |
|--------|-------------|----------|
| T-1-05 (silent session loss from non-atomic rotation) | **Mitigated** | Closure wraps DeleteSession+CreateSession; `TestRefreshToken_RollbackOnInsertFailure` proves rollback; `TestRefreshToken_UserDeletedDuringRotation` proves rollback on the lookup-failure path |

No new threat surface introduced — the refresh endpoint contract (200/400/401/500 shapes) is preserved; the only externally observable behaviour change is that a previously-silent 500-after-stale-tokens path is now a clean 500 with the session intact.

## Deviations from Plan

None — plan executed exactly as written. Choice (b) from RESEARCH §HOTFIX-05 "Notice" was taken (reuse `storeRefreshSession(tx, ...)`) per the plan's recommendation in Task 2 Step A.

## Self-Check: PASSED

- Files exist:
  - FOUND: `/Users/abdunabi/Desktop/vpn/server/api/internal/handler/auth.go`
  - FOUND: `/Users/abdunabi/Desktop/vpn/server/api/internal/handler/auth_test.go`
- Commit exists:
  - FOUND: `2f6d86b` — `hotfix(01): transactional refresh-token rotation [HOTFIX-05]`
- All five `TestRefreshToken_*` tests pass (2 pre-existing + 3 new); full suite green; binary builds.
- Closure uses `tx` for DeleteSession, FindUserByID, and storeRefreshSession; `db.Transaction(` and `errors.Is(err, repository.ErrNotFound)` both present.
