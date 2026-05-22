---
phase: 01-hotfix-audit-critical-fixes
reviewed: 2026-05-22T00:00:00Z
depth: standard
files_reviewed: 17
files_reviewed_list:
  - server/api/Dockerfile
  - server/api/cmd/createadmin/main.go
  - server/api/cmd/createadmin/main_test.go
  - server/api/cmd/main.go
  - server/api/internal/cache/redis.go
  - server/api/internal/cache/redis_test.go
  - server/api/internal/config/config.go
  - server/api/internal/config/config_test.go
  - server/api/internal/handler/auth.go
  - server/api/internal/handler/auth_test.go
  - server/api/internal/handler/errorhandler_test.go
  - server/api/internal/handler/health.go
  - server/api/internal/middleware/admin.go
  - server/api/internal/middleware/admin_test.go
  - server/api/internal/repository/user_repo_subscription_test.go
  - server/api/migrations/017_sessions_refresh_token_hash_unique.sql
  - server/api/scripts/smoke_test_session_index.sh
findings:
  critical: 0
  warning: 5
  info: 9
  total: 14
status: issues_found
---

# Phase 1: Code Review Report

**Reviewed:** 2026-05-22
**Depth:** standard
**Files Reviewed:** 17
**Status:** issues_found

## Summary

The HOTFIX-01..08 tranche delivers eight focused security/reliability fixes
with strong, on-point tests. All six security-critical fixes (refresh
transaction wrap, AdminRequired DB re-read, 5xx body scrubbing, atomic Lua
rate limit, env validator, password-via-stdin) are implemented correctly and
each has a regression test that genuinely exercises the failure mode the fix
closes. No critical security regressions were found.

The findings below are concerns about consistency, defense-in-depth, and
operational ergonomics:

- One latent infrastructure gap (Warning): migration 017 will not be picked up
  by `docker-entrypoint-initdb.d` against an existing production DB and the
  smoke test silently passes against a stale plan if rows were ever cached.
- Two consistency findings (Warning): only one of three "store refresh
  session" call sites is wrapped in a transaction post-HOTFIX-05; AdminRequired
  uses `FindUserByID` whereas the sister handler `AdminChangePassword` uses
  `FindUserByIDAdmin`.
- Two minor robustness findings (Warning): `readPassword` swallows non-EOF
  read errors when any bytes were read; `getEnvDuration` / `getEnvInt64` fall
  back silently on parse errors with no log line.
- Info-level items cover dedupe ordering against NULL `created_at`, the
  duplicate "required env" source of truth in `Load()` vs `RequireEnv()`,
  Dockerfile/go.mod Go-version mismatch, etc.

The two HOTFIX-05 regression tests (`TestRefreshToken_RollbackOnInsertFailure`
and `TestRefreshToken_UserDeletedDuringRotation`) deserve a specific call-out:
they assert the rollback invariant by re-counting session rows after the
failure path, which is the only way to prove the transaction actually rolled
back rather than ignoring the error. That is excellent test design.

## Warnings

### WR-01: Migration 017 will not auto-apply on an existing production database

**File:** `server/api/migrations/017_sessions_refresh_token_hash_unique.sql:19-28`
**Issue:** The migration's file-level comment correctly states that
`docker-entrypoint-initdb.d` runs the file without a wrapping transaction
(so `CREATE INDEX CONCURRENTLY` works), but it omits the more important
operational fact: `docker-entrypoint-initdb.d` runs **only on first DB init**.
The production Postgres `pgdata` volume in `docker-compose.prod.yml:17-18` is
persistent, so on every subsequent `docker compose up -d` the
`/docker-entrypoint-initdb.d/*.sql` files are silently ignored. There is no
in-Go migration runner (verified — `internal/repository/db.go` says "Schema
is managed by SQL migrations, not AutoMigrate" but nothing in `cmd/` invokes
them). The smoke test in `scripts/smoke_test_session_index.sh` will report
RED until an operator manually runs `psql -f migrations/017_*.sql` against
the live DB. This is a real risk because Phase 1's success criterion #3
("/auth/refresh hits the index") depends on the index being applied, and
nothing in the deployment pipeline guarantees that.

**Fix:** Either (a) add a one-shot Go migration runner invoked from
`cmd/main.go` before `app.Listen`, gated on a `MIGRATE=true` env flag; or
(b) document the manual apply step in the deployment runbook and add the
smoke test to a post-deploy CI gate so a missing index fails the deploy
instead of silently passing. The migration file's header comment should
also call out "this file is NOT auto-applied on production upgrades —
operators must `psql -f` it manually for existing DBs."

### WR-02: Inconsistent transaction wrap — only RefreshToken rotates atomically

**File:** `server/api/internal/handler/auth.go:400, 501`
**Issue:** HOTFIX-05 wrapped `RefreshToken`'s rotate-session path in
`db.Transaction(...)` so a failed `CreateSession` after `DeleteSession`
rolls back (lines 253-280). However, the two other call sites of
`storeRefreshSession` — `GuestLogin` (line 400 for the known-device fast
path, line 501 for the fresh-user slow path) and `AdminLogin` (line 102) —
still call it with the outer `db` and discard the error:

```go
_ = storeRefreshSession(db, user.ID, tokens.RefreshToken)  // line 400
```

These call sites do not need rotation atomicity (there's no prior
DeleteSession to undo), so the security severity is lower. But the
inconsistency means a partial Redis-down / DB-down scenario can silently
issue access tokens to a user who has no session row, and `GuestLogin`'s
slow path additionally creates a user + subscription before this step —
if `storeRefreshSession` fails, the user account exists but the user
cannot refresh.

**Fix:** At minimum, log a structured `Warn` line (currently `_ = ...`
silently discards) so on-call sees these failures in production logs.
Better: stop dropping the error and return 500 — the user can retry, the
account will be reused on retry (guest path looks up by device_id), and
the user is not handed a soon-to-be-broken token.

```go
if err := storeRefreshSession(db, user.ID, tokens.RefreshToken); err != nil {
    logger.Error("guest login: failed to store refresh session",
        zap.String("user_id", user.ID),
        zap.Error(err),
    )
    return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
        "error": "internal server error",
    })
}
```

### WR-03: AdminRequired uses FindUserByID (not FindUserByIDAdmin) — inconsistency with AdminChangePassword

**File:** `server/api/internal/middleware/admin.go:41`
**Issue:** `AdminRequired` calls `repository.FindUserByID(db, userID)`, while
the admin-scoped handler `AdminChangePassword` in `handler/auth.go:156` calls
`repository.FindUserByIDAdmin(db, adminID)`. There are two repo functions
(verified: `user_repo.go:39` and `admin_repo.go:450`). If the two diverge in
behavior — e.g. `FindUserByIDAdmin` filters out soft-deleted rows differently
or pre-loads associations the admin path expects — the middleware and the
handler will disagree about what "this admin user exists" means. Today both
likely return the same row, but the divergence is a future footgun and
already breaks the "consistent admin-data path" expectation that a code
reviewer or new contributor would form.

**Fix:** Either switch `AdminRequired` to `FindUserByIDAdmin` for consistency,
or rename one of the two functions and document why two reads exist. If the
distinction is genuinely meaningful, add a Godoc comment on each explaining
when to use which.

### WR-04: readPassword swallows non-EOF errors when any byte was read

**File:** `server/api/cmd/createadmin/main.go:113-122`
**Issue:** In the piped-stdin branch, the error handling is:

```go
line, err := bufio.NewReader(in).ReadString('\n')
if err != nil {
    if line == "" {
        return "", fmt.Errorf("reading password from stdin: %w", err)
    }
}
```

The intent (per the comment) is "EOF without trailing newline is acceptable."
That's correct for `io.EOF`. But the branch swallows **any** error — including
`ErrUnexpectedEOF`, a closed pipe mid-read, a context cancellation, etc. — as
long as at least one byte was buffered. An operator running
`printf 'partial' | ./createadmin` with a write-side crash would get a
truncated password silently accepted as the real one, and then `bcrypt`
would store the truncated hash. The CLI is one-shot so the practical risk is
low (an attentive operator would notice an unfamiliar password), but
defense-in-depth says don't blanket-ignore non-`io.EOF` errors.

**Fix:** Tighten the branch to only ignore `io.EOF`:

```go
line, err := bufio.NewReader(in).ReadString('\n')
if err != nil && !errors.Is(err, io.EOF) {
    return "", fmt.Errorf("reading password from stdin: %w", err)
}
if line == "" {
    return "", fmt.Errorf("reading password from stdin: empty input")
}
```

### WR-05: getEnvDuration / getEnvInt64 silently swallow parse errors

**File:** `server/api/internal/config/config.go:93-97, 107-111`
**Issue:** Both helpers fall back to the default on `time.ParseDuration` /
`strconv.ParseInt` failure with no log line emitted:

```go
d, err := time.ParseDuration(val)
if err != nil {
    return fallback   // no warning, no record that the operator's intent was discarded
}
```

If an operator sets `STALE_CONNECTION_AFTER=3min` (invalid; should be `3m`)
or `LINK_CODE_TTL=5min`, the server starts cleanly with the hard-coded
default and the operator never finds out their tuning value was rejected.
This is exactly the "fix one, restart, fix the next" experience HOTFIX-08
was meant to avoid for the *required* set — the *tunable* set has the
same problem at a smaller scale.

**Fix:** When the env var is set but unparseable, surface it. Either:
- Promote both helpers to return `(T, error)` and have `Load()` aggregate
  parse errors alongside the missing-key check; or
- At minimum, write a one-line stderr or zap warning at parse time. Since
  these helpers run before the logger exists, the simplest fix is to track
  the offenders in a slice and emit one `logger.Warn` line from `cmd/main.go`
  right after `config.Load()` returns.

## Info

### IN-01: Migration 017 dedupe ordering is non-deterministic for NULL created_at

**File:** `server/api/migrations/017_sessions_refresh_token_hash_unique.sql:31-39`
**Issue:** `ORDER BY created_at DESC` is well-defined when all rows have a
timestamp, but Postgres treats NULL as larger-than-any-value by default
(`NULLS FIRST` for `DESC`). If any session row in staging/dev has
`created_at IS NULL` (the CREATE statement in `auth_test.go` doesn't set a
default and the production schema may or may not — worth grepping
`migrations/001_initial.sql` to confirm), those rows will be retained over
rows with real timestamps. Practically harmless for v2.1.0 since there are
no paying users, but the dedupe semantics deserve clarity.

**Fix:** Add `NULLS LAST` to the window ordering: `ORDER BY created_at DESC
NULLS LAST`. Then the newest *known* row wins, and NULL-timestamped rows
are deleted as the duplicates.

### IN-02: Config.Load() and RequireEnv() duplicate the "required" check

**File:** `server/api/internal/config/config.go:68-74, 125-138`
**Issue:** `RequireEnv()` (called from `cmd/main.go:40`) checks four keys.
`Load()` (called immediately after) also re-checks `JWT_SECRET` and
`TUNNEL_VLESS_UUID`. The pre-Load aggregate is the new design; the
post-Load check is leftover. If RequireEnv's list ever drifts (e.g.
someone adds `STRIPE_KEY` to required but not to RequireEnv), the two
sources of truth disagree.

**Fix:** Remove the two `if cfg.JWTSecret == "" / TunnelVLESSUUID == ""`
guards from `Load()` now that `RequireEnv` is the canonical pre-check.
Or, alternatively, drive both from a shared slice constant so they
cannot drift.

### IN-03: Dockerfile Go version mismatches go.mod

**File:** `server/api/Dockerfile:1`
**Issue:** `FROM golang:1.24-alpine` builds against Go 1.24 toolchain, but
`go.mod` declares `go 1.22.0`. This works because Go's toolchain is
forward-compatible, but it means CI/dev/prod builds with Go 1.24 features
implicitly (e.g. `for range int`, new stdlib APIs) and any local dev
running `go build` with Go 1.22 will succeed while the container build
might silently use a newer feature. Lock one or the other.

**Fix:** Either bump go.mod to `go 1.24` (if the team is comfortable
requiring 1.24 locally) or pin the Dockerfile to `golang:1.22-alpine`.
The current state is "works today, will bite us when someone uses a 1.23+
stdlib API."

### IN-04: Dockerfile runs `go mod tidy` twice

**File:** `server/api/Dockerfile:9, 15`
**Issue:** `go mod tidy` runs before sources are copied (line 9, when only
`go.mod` exists, which is unusual — normally needs sources to compute
the import graph), then again after `COPY . .` (line 15). The first
invocation will pull a different set of dependencies than the second
because the import graph is empty pre-copy. The second `tidy` will
re-resolve everything, making the first one wasted work plus a
correctness hazard if the first one happens to record a stale `go.sum`.

**Fix:** Drop the first `go mod tidy` and rely solely on the post-COPY one
(or, better, commit `go.sum` to the repo and use `go mod download` only,
no tidy). The current ordering also defeats Docker layer caching for
dependency installation, which is the canonical reason to copy `go.mod`
before the rest of the source.

### IN-05: zap.Logger.Sync() error suppressed by `defer` without acknowledgement

**File:** `server/api/cmd/main.go:34`
**Issue:** `defer logger.Sync()` discards Sync's return value, which on
Linux+stderr famously returns `sync /dev/stderr: invalid argument`. The
common idiom is `defer func() { _ = logger.Sync() }()` to make the intent
explicit and to satisfy `errcheck`-style linters. Minor stylistic point.

**Fix:** `defer func() { _ = logger.Sync() }()`

### IN-06: `goto freshUser` in GuestLogin is unusual Go style

**File:** `server/api/internal/handler/auth.go:380, 412`
**Issue:** The fast-path-to-slow-path fallthrough uses a `goto` label. Go
permits this and the code is correct, but Go style strongly prefers
restructuring control flow over `goto`. The current shape is also a hazard
for future edits: the label sits between two big blocks and a future
contributor adding a variable declaration before the label could break the
build with `goto jumps over declaration`.

**Fix:** Extract the slow path into a closure or helper function and call
it from each fall-through site explicitly. Or invert the fast path into a
single `if knownDevice { return ... }` block so the slow path becomes the
default. Not urgent — current code is correct.

### IN-07: TestCreateAdmin_RejectsPasswordFlag depends on `go run`

**File:** `server/api/cmd/createadmin/main_test.go:41`
**Issue:** The test shells out to `go run .` to assert that the
flag.Parse rejection happens in main(). This is the *only* way to assert
on a flag-package error since the package's main() does the parsing.
Acceptable, but worth flagging because:
- It requires `go` in PATH at test time (CI is fine, some sandboxes are not).
- It's much slower than the other tests (each invocation re-compiles).
- It will fail mysteriously if the test runs from a different working
  directory than the package — the `cmd.Dir = wd` line handles this, but
  CI subtleties (e.g. `go test ./...` from repo root) can still bite.

**Fix:** No action required for this PR. Long-term, consider extracting
flag definitions into a tested helper that can be exercised without a
subprocess — same pattern as `createAdminUser` was extracted for the
free-tier test.

### IN-08: Smoke test EXPLAIN query uses 32-char hex, but production uses 64-char SHA-256

**File:** `server/api/scripts/smoke_test_session_index.sh:33`
**Issue:** The literal `'deadbeefdeadbeefdeadbeefdeadbeef'` is 32 hex chars
(128 bits) while the real `refresh_token_hash` column stores 64 hex chars
(SHA-256 = 256 bits). Postgres's planner picks the index regardless of
value length for a varchar/text column, so the test works — but a
reviewer reading the smoke test could reasonably assume the literal
matches production shape, and an alert engineer at 3am debugging an
EXPLAIN regression would lose minutes confirming this.

**Fix:** Use a 64-char literal so the EXPLAIN matches the real query
exactly. Trivially cosmetic, but the script is operational tooling and
clarity matters there.

### IN-09: AdminRequired propagates raw DB error to ErrorHandler without context

**File:** `server/api/internal/middleware/admin.go:52`
**Issue:** When `FindUserByID` returns a non-`ErrNotFound` error, the
middleware does `return err`. Fiber's ErrorHandler then logs it with the
generic "request error" message and scrubs the body — that's the right
*response* behavior post-HOTFIX-04. But the log line carries no context
about *which* middleware it came from, so operators triaging a 500 see
"request error" / `path=/api/v1/admin/users` / raw GORM text, with no
way to know whether the failure was AdminRequired's DB lookup or
something further down the chain.

**Fix:** Wrap with context before propagating:

```go
return fmt.Errorf("admin middleware: looking up user %s: %w", userID, err)
```

Cheap and makes one-shot triage faster. The `request_id` already
correlates the log to the client; this just makes the log self-describing.

---

_Reviewed: 2026-05-22_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
