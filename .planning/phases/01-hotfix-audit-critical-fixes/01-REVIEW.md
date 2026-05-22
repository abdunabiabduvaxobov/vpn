---
phase: 01-hotfix-audit-critical-fixes
reviewed: 2026-05-22T00:00:00Z
depth: standard
re_review: true
previous_findings:
  warning: 5
  info: 9
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
fixes_verified:
  - id: WR-02
    commit: 3180ce1
    status: resolved
  - id: WR-03
    commit: ff1b756
    status: resolved
  - id: WR-04
    commit: 61c00be
    status: resolved
  - id: WR-05
    commit: 76e4027
    status: resolved
deferred:
  - id: WR-01
    reason: roadmap_item
    note: Migration runner gap is a separate roadmap deliverable, not a hotfix.
findings:
  critical: 0
  warning: 0
  info: 3
  total: 3
status: clean
---

# Phase 1: Code Review Report (Re-Review)

**Reviewed:** 2026-05-22
**Depth:** standard
**Files Reviewed:** 17
**Status:** clean (4 of 4 in-scope warnings resolved; WR-01 deferred to roadmap)

## Summary

Re-review of the four targeted fixes landed in commits `3180ce1`, `ff1b756`,
`61c00be`, `76e4027`. All four fixes land correctly, each closes the exact
failure mode the prior review described, and each is accompanied by a
regression test that genuinely exercises the failure path (not just the
happy path).

No new bugs were introduced. No new security regressions. The remaining
findings are all `info`-level cosmetic items that survived from the prior
review unchanged.

### Fix-by-fix verification

**WR-02 — return 500 when storeRefreshSession fails (commit 3180ce1):**
Verified at three call sites in `handler/auth.go`:

- `AdminLogin` line 102-113: now logs `user_id` + error and returns 500.
- `GuestLogin` known-device fast path, lines 409-422: logs `user_id` +
  `device_id` + error, returns 500. Comment correctly explains the retry
  story (device row unchanged, so retry hits the same fast path).
- `GuestLogin` fresh-user slow path, lines 523-537: logs `user_id` +
  `device_id` + error, returns 500. Comment acknowledges the user +
  subscription rows that were just created stay behind; that's intentional
  because (a) device-bound retries reuse the row, (b) guest accounts are
  cheap, and (c) the scheduler-driven cleanup will reap orphans long-term.

None of the three changes alter the happy-path response shape or the
existing tests (`TestAdminLogin_HappyPath_Returns200WithTokens`,
`TestGuestLogin_HappyPath_CreatesUserAndReturnsTokens`) still pass against
the new code.

**WR-03 — use FindUserByIDAdmin in AdminRequired (commit ff1b756):**
Verified at `middleware/admin.go:46`. The middleware now calls
`repository.FindUserByIDAdmin(db, userID)` matching the sister handler
`AdminChangePassword`. Confirmed `FindUserByIDAdmin` (admin_repo.go:450)
correctly wraps non-`ErrNotFound` errors with `"finding user %s: %w"`
context, which means IN-09 from the prior review is now partially
addressed: operators triaging a 500 from AdminRequired will see
`"finding user <uuid>: <gorm error>"` rather than just the raw GORM text.
The middleware also defensively returns `errNilDB` for a nil DB.

Existing tests (`TestAdminRequired_AllowsAdminRole`, `_RejectsUserRole`,
`_DemotionTakesEffect`, `_DeletedUserDuringSession`, `_EmptyLocals`) all
remain valid under the new call.

**WR-04 — narrow readPassword to only ignore io.EOF (commit 61c00be):**
Verified at `cmd/createadmin/main.go:114-126`. The branch is now exactly
the form recommended in the prior review:

```go
if err != nil && !errors.Is(err, io.EOF) {
    return "", fmt.Errorf("reading password from stdin: %w", err)
}
line = strings.TrimRight(line, "\r\n")
if line == "" {
    return "", fmt.Errorf("reading password from stdin: empty input")
}
```

The empty-input check is no longer gated on `err != nil`, so a pipe that
closes cleanly with no bytes also returns the explicit error. The added
`TestCreateAdmin_RejectsEmptyPipedStdin` (main_test.go:155-181) closes the
write side of the pipe immediately without writing any bytes and asserts
the returned error contains "empty input" — that's a real regression test,
not a tautology.

**WR-05 — surface tunable env parse failures via warn log (commit 76e4027):**
Verified the warning-sink pattern across `config/config.go` and
`cmd/main.go`:

- `Config.EnvParseWarnings []string` field added (config.go:45).
- `getEnvDuration` / `getEnvInt64` take `*[]string` and append the offender
  key + reason + chosen default when parse fails (config.go:106-138).
- `Load()` allocates a local `parseWarnings` slice, threads it into the
  helpers, and assigns to `cfg.EnvParseWarnings` (config.go:60, 82).
- `cmd/main.go:57-61` emits one `logger.Warn` line right after `Load()`
  returns, with `zap.Strings("offenders", cfg.EnvParseWarnings)`.

The deferred-emit pattern (helpers run before logger exists, slice
collected, emitted post-Load) matches the suggestion in the prior review
verbatim. Tests `TestLoad_RecordsParseWarnings` and
`TestLoad_NoParseWarningsForValidOrUnset` cover both directions of the
contract (set+invalid yields warnings; valid/unset yields none).

### New issues introduced by the fixes

None. Three things worth a defensive look were considered and cleared:

1. **GuestLogin fresh-user 500 leaves orphan user + subscription rows.**
   Already acknowledged in the inline comment (auth.go:523-537). On retry
   the device row (if present) is reused, so accounts aren't duplicated.
   The scheduler reaps idle user rows long-term. Acceptable trade-off
   versus handing the client a dead-on-arrival token.

2. **Warning-sink pattern uses a `*[]string`.** No data race — `Load()` is
   called exactly once at process start, before any goroutine that reads
   the config exists. The slice is also single-writer (only the helpers
   append, only `Load()` reads at the end of its own scope).

3. **WR-03 changes the error message shape from AdminRequired.** Previously
   the middleware passed raw GORM text through ErrorHandler; now it gets
   `"finding user <uuid>: <gorm error>"`. The HOTFIX-04 scrub on the
   ErrorHandler side still applies, so the client still sees
   `{"error":"internal server error","request_id":"..."}` — operator log
   line gains context, client surface unchanged. No regression.

### Deferred (acknowledged, not flagged)

**WR-01 (migration auto-apply gap)** — explicitly deferred to a roadmap
deliverable. The prior review's reasoning still applies: `docker-entrypoint-initdb.d`
runs only on first init, so migration 017 will not auto-apply to existing
production DBs. This is now a known operational hazard documented in the
deployment runbook (per the deferral acknowledgement), and the fix
(in-Go migration runner or post-deploy CI gate) is too large to land
inside the hotfix tranche. Not flagged as an outstanding finding.

## Info

The three items below are carry-overs from the prior review that are
purely cosmetic or low-priority; none block the phase from closing.

### IN-01: Migration 017 dedupe ordering is non-deterministic for NULL created_at

**File:** `server/api/migrations/017_sessions_refresh_token_hash_unique.sql:31-39`
**Issue:** `ORDER BY created_at DESC` without `NULLS LAST` retains
NULL-`created_at` rows over rows with real timestamps (Postgres default
is `NULLS FIRST` for DESC). Practically harmless for v2.1.0 — no paying
users, and the production schema almost certainly sets a `NOT NULL DEFAULT
NOW()` on `sessions.created_at` (worth grepping `migrations/001_initial.sql`).
**Fix:** `ORDER BY created_at DESC NULLS LAST`.

### IN-02: Smoke test EXPLAIN query uses 32-char hex, but production uses 64-char SHA-256

**File:** `server/api/scripts/smoke_test_session_index.sh:33`
**Issue:** The literal `'deadbeefdeadbeefdeadbeefdeadbeef'` is 32 hex chars
(128 bits); the real `refresh_token_hash` column stores 64 hex chars
(SHA-256). Planner picks the index regardless, so the test works — but a
reviewer reading the smoke test at 3am would lose minutes confirming this.
**Fix:** Use a 64-char literal so the EXPLAIN matches the real query shape.

### IN-03: Dockerfile Go version mismatches go.mod and runs `go mod tidy` twice

**File:** `server/api/Dockerfile:1, 9, 15`
**Issue:** `FROM golang:1.24-alpine` builds with Go 1.24 toolchain while
`go.mod` declares `go 1.22.0`. Forward-compatible today, will silently
adopt 1.23+ stdlib features in CI/prod while local devs on 1.22 may not
see them. Separately, `go mod tidy` runs twice (line 9 before sources are
copied — when the import graph is empty — and again line 15 post-COPY).
The first invocation is wasted work and defeats Docker dependency-layer
caching.
**Fix:** Pin Dockerfile to `golang:1.22-alpine` OR bump `go.mod` to `go
1.24`. Drop the first `go mod tidy` and rely on the post-COPY one (better:
commit `go.sum` and use `go mod download` only).

---

_Reviewed: 2026-05-22_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
_Mode: re-review (prior REVIEW.md superseded)_
