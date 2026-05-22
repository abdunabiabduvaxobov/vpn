# 02-08 Verify Evidence

Generated after plan 02-08 (Phase 2 gap-closure: CR-01, CR-02, WR-01, WR-02, WR-03) completed all seven atomic commits.

## Test counts (per package, after all fixes landed)

- handler tests passing: 110
- repository tests passing: 43
- auth/apple tests passing: 8
- auth/google tests passing: 5
- middleware tests passing: 29

## Quality gates

- `go test ./internal/auth/... ./internal/repository/... ./internal/handler/... ./internal/middleware/... -count=1` — PASS (5/5 packages ok)
- `go test ./internal/handler/ -race -count=1` — PASS (no data races)
- `go vet ./...` — clean
- `go build ./...` — clean

## Commits in plan 02-08

| SHA       | Title                                                     | Closes |
|-----------|-----------------------------------------------------------|--------|
| 6370cc7   | test(02-08): red-phase empty-sub guards                   | CR-01  |
| db62f25   | fix(02-08): reject empty-sub SSO tokens with 401          | CR-01  |
| 406a00b   | test(02-08): red-phase concurrent auto-link race          | CR-02  |
| 1045df8   | fix(02-08): wrap auto-link Step B in db.Transaction       | CR-02  |
| 4e954f7   | fix(02-08): reject non-user roles in parseGuestJWT        | WR-01  |
| 6befbe4   | fix(02-08): blacklist token on ttl boundary               | WR-02  |
| b304fc1   | fix(02-08): create free subscription row for new SSO users | WR-03 |

## Findings closed (traceability)

- **CR-01 — Empty `sub` claim creates phantom user row**: VERIFICATION.md truth #1 now verifies. Guards land in AppleSignIn, GoogleSignIn, and resolveSSOUser. Two new tests (`TestAppleSignIn_EmptySub_Returns401`, `TestGoogleSignIn_EmptySub_Returns401`) prove the fix.
- **CR-02 — Auto-link Step B TOCTOU race**: VERIFICATION.md truth #2 now verifies. Step B runs inside `db.Transaction`. `TestAppleSignIn_ConcurrentAutoLinkByEmail` exercises the race with 5 concurrent goroutines.
- **WR-01 — `parseGuestJWT` accepts admin role**: `parseGuestJWT` now rejects any non-empty role other than "user". `TestParseGuestJWT_RejectsAdminRole` proves it.
- **WR-02 — Logout TTL boundary case**: `if ttl >= 0` replaces `if ttl > 0`. `TestLogout_BlacklistsTokenExpiringNow` proves the audit-trail entry is written even when the token's exp == time.Now().
- **WR-03 — New SSO user has no subscription row**: Step D now calls `CreateSubscription` after `CreateUser`. `TestAppleSignIn_NewUser_HasSubscriptionRow` proves the free-tier row is inserted.

Project security gate from CLAUDE.md ("Critical/High MUST land before any paying user touches the system") is unblocked for Phase 3 launch.
