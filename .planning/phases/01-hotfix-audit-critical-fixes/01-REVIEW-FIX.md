---
phase: 01-hotfix-audit-critical-fixes
fixed_at: 2026-05-22T00:00:00Z
review_path: .planning/phases/01-hotfix-audit-critical-fixes/01-REVIEW.md
iteration: 1
findings_in_scope: 5
fixed: 4
skipped: 1
status: partial
---

# Phase 1: Code Review Fix Report

**Fixed at:** 2026-05-22
**Source review:** .planning/phases/01-hotfix-audit-critical-fixes/01-REVIEW.md
**Iteration:** 1

**Summary:**
- Findings in scope: 5 (Warning-tier; Info-tier excluded by `fix_scope=critical_warning`)
- Fixed: 4 (WR-02, WR-03, WR-04, WR-05)
- Skipped: 1 (WR-01 — deferred per orchestrator guidance)

Phase 1 was already tagged as `v2.2.0-hotfix`. These four fixes land on
`main` after the tag, so the tagged release is unchanged — the fixes
flow into v2.2.1 (or the next dot release that ships off `main`).

All four fixes were verified by running `go test ./...` against the
server/api module after each commit; the full suite stays green.

## Fixed Issues

### WR-02: Inconsistent transaction wrap — only RefreshToken rotates atomically

**Files modified:** `server/api/internal/handler/auth.go`
**Commit:** 3180ce1
**Applied fix:** Three call sites (`AdminLogin` line 102, `GuestLogin`
known-device fast path line 400, `GuestLogin` fresh-user slow path line
501) previously discarded `storeRefreshSession`'s error with `_ = ...`.
All three now log the error with `user_id` / `device_id` context and
return HTTP 500. Rationale matches the reviewer's recommendation: there
is no prior `DeleteSession` to undo at these sites, so a full
transaction wrap is unnecessary — but handing the client a token with
no backing session row is a worse user experience than a clean 500 +
client retry (the next `/auth/refresh` would 401 and silently sign the
user out). The fresh-user path retains its existing user/subscription
rollback ordering — the new failure point fires AFTER both are
persisted, and the device row (when present) is reused on retry so
accounts are not duplicated.

### WR-03: AdminRequired uses FindUserByID — inconsistent with AdminChangePassword

**Files modified:** `server/api/internal/middleware/admin.go`
**Commit:** ff1b756
**Applied fix:** Switched `repository.FindUserByID(db, userID)` to
`repository.FindUserByIDAdmin(db, userID)` so the middleware and
`AdminChangePassword` (handler/auth.go:156) read users through the
same code path. Added a Godoc comment explaining the choice. As a
secondary benefit, `FindUserByIDAdmin` wraps non-`ErrNotFound` DB
errors with `fmt.Errorf("finding user %s: %w", ...)`, which gives
operators a self-describing log line at the ErrorHandler boundary —
this addresses the spirit of IN-09 for the AdminRequired path
without needing the separate `fmt.Errorf` wrap suggested there.

### WR-04: readPassword swallows non-EOF errors when any byte was read

**Files modified:** `server/api/cmd/createadmin/main.go`,
`server/api/cmd/createadmin/main_test.go`
**Commit:** 61c00be
**Applied fix:** Narrowed the piped-stdin error branch from "ignore
any err if at least one byte was buffered" to
`if err != nil && !errors.Is(err, io.EOF) { return err }`. Added an
explicit empty-input rejection so a closed-immediately stdin can no
longer produce a successful read of `""`. Added regression test
`TestCreateAdmin_RejectsEmptyPipedStdin` that closes the write side of
a pipe with no bytes written and asserts the explicit "empty input"
error. The existing `TestCreateAdmin_AcceptsPipedStdin` continues to
pass (io.EOF after the trailing newline is still accepted).

### WR-05: getEnvDuration / getEnvInt64 silently swallow parse errors

**Files modified:** `server/api/internal/config/config.go`,
`server/api/internal/config/config_test.go`, `server/api/cmd/main.go`
**Commit:** 76e4027
**Applied fix:** Both helpers now accept a `*[]string` warnings sink.
When the env var is set but unparseable, the key (with the offending
value and parse error) is appended to the slice and the helper still
returns the fallback. `Load()` collects the warnings into
`Config.EnvParseWarnings`. `cmd/main.go` emits a single
`logger.Warn("tunable environment variables failed to parse — falling
back to defaults", zap.Strings("offenders", ...))` immediately after
`config.Load()` returns. The deferred-emit pattern is required because
the logger does not exist at parse time. Added two regression tests:
`TestLoad_RecordsParseWarnings` confirms invalid duration + invalid
int64 both appear in the warnings; `TestLoad_NoParseWarningsForValidOrUnset`
guards against false positives for valid values and unset vars
(operator chose the default, no warning).

## Skipped Issues

### WR-01: Migration 017 will not auto-apply on an existing production database

**File:** `server/api/migrations/017_sessions_refresh_token_hash_unique.sql:19-28`
**Reason:** Deferred — needs design discussion per orchestrator
guidance. The two proposed fix options diverge significantly:
- Option (a) "in-Go migration runner gated on `MIGRATE=true` env flag"
  is a non-trivial new code path (migration tracking table, file
  ordering contract, transaction-vs-no-transaction handling for
  `CREATE INDEX CONCURRENTLY`, rollback policy). This belongs in its
  own phase with an architect pass.
- Option (b) "document manual `psql -f` step in the runbook + add the
  smoke test to a post-deploy CI gate" is mostly process work
  (runbook, CI workflow) that lives outside the API source tree.

Either approach affects deployment story for the whole project, not
just Phase 1. The risk surfaced by this finding (smoke test silently
green when the index is missing) is real but does not block what is
already tagged as `v2.2.0-hotfix` — operators applied the migration
manually for this release. Should be raised in the roadmap as its own
discussion item rather than rolled into this fix iteration.

**Original issue:** The migration's file-level comment notes that
`docker-entrypoint-initdb.d` runs the file without a wrapping
transaction (so `CREATE INDEX CONCURRENTLY` works) but omits the more
important operational fact that `docker-entrypoint-initdb.d` runs
**only on first DB init**. The production Postgres `pgdata` volume in
`docker-compose.prod.yml:17-18` is persistent, so on every subsequent
`docker compose up -d` the `/docker-entrypoint-initdb.d/*.sql` files
are silently ignored. There is no in-Go migration runner. The smoke
test in `scripts/smoke_test_session_index.sh` will report RED until
an operator manually runs `psql -f migrations/017_*.sql` against the
live DB.

---

_Fixed: 2026-05-22_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 1_
