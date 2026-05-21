---
phase: 01-hotfix-audit-critical-fixes
plan: 03
subsystem: api
tags: [fiber, requestid, error-handling, zap, security, hotfix]

# Dependency graph
requires:
  - phase: 01-hotfix-audit-critical-fixes
    provides: "fail-fast env validator (plan 02) ensures API never starts with missing JWT_SECRET, so ErrorHandler always has a sane runtime"
provides:
  - "Global Fiber ErrorHandler that scrubs 5xx response bodies to {error: 'internal server error', request_id: '<uuid>'} (D-05)"
  - "4xx pass-through preserved (D-06) — client-UX surface keeps verbose err.Error() text"
  - "Fiber middleware/requestid wired BEFORE recover.New() so panic-recovery paths carry a request_id"
  - "Every error logged once via zap with request_id, status, path, method — correlate scrubbed client response to structured log line"
  - "Integration tests via Fiber app.Test() proving scrub + header echo + UUIDv4 generation + 4xx pass-through"
affects: [02-sso, 03-lava-payments, 08-cleanup-hardening]

# Tech tracking
tech-stack:
  added: ["github.com/gofiber/fiber/v2/middleware/requestid", "github.com/gofiber/fiber/v2/utils.UUIDv4"]
  patterns:
    - "X-Request-ID echo or UUIDv4 generation in middleware chain"
    - "Single zap.Error log line per request error, paired with request_id in the 5xx response body"
    - "Status-class-aware error body shape (>=500 scrubbed, <500 verbose)"

key-files:
  created:
    - "server/api/internal/handler/errorhandler_test.go"
  modified:
    - "server/api/internal/handler/health.go"
    - "server/api/cmd/main.go"

key-decisions:
  - "5xx body is exactly {error, request_id} — two keys, no err.Error() text"
  - "4xx body keeps err.Error() — client toasters render these (D-06)"
  - "requestid.New wired BEFORE recover.New so panic-recovery 5xx still carries an id"
  - "utils.UUIDv4 generator chosen over default utils.UUID — RFC 4122 random, no request-count leak"

patterns-established:
  - "Structured error logging with request_id field across every error path"
  - "Test-driven Fiber middleware integration: stub file (SKIP) → real bodies + green pass in same atomic commit"

requirements-completed: [HOTFIX-04]

# Metrics
duration: 11min
completed: 2026-05-22
---

# Phase 1 Plan 03: HOTFIX-04 ErrorHandler Scrub + X-Request-ID Summary

**Fiber ErrorHandler now returns a generic 5xx body with a UUIDv4 request_id (4xx pass-through preserved), with middleware/requestid wired before recover so panic-recovery paths also carry the id.**

## Performance

- **Duration:** ~11 min
- **Started:** 2026-05-21T23:27:00Z
- **Completed:** 2026-05-21T23:38:16Z
- **Tasks:** 2 (one TDD pair: RED stub → GREEN implementation+real-tests, committed atomically per plan D-01)
- **Files modified:** 3 (1 created, 2 edited)

## Accomplishments

- 5xx response bodies no longer leak `pq:`, `bcrypt:`, `gorm:`, or internal `err.Error()` text — verified by `TestErrorHandler_Scrubs5xxBody`.
- Every error response carries an `X-Request-ID` header and an `request_id` field in the JSON body; incoming `X-Request-ID` is echoed verbatim, missing headers get a fresh UUIDv4 (RFC 4122 random).
- 4xx responses still ship verbose `err.Error()` text per D-06, so client toasters keep rendering "email required" etc.
- Every 5xx is paired with one structured zap log line carrying the same `request_id`, status, path, method — operators can correlate scrubbed client response to log row.
- Closes CODE-REVIEW CRIT-04 and SECURITY-AUDIT S9-1.

## Task Commits

Per CONTEXT.md D-01 ("one atomic commit per hotfix") the plan groups its TDD RED stub and GREEN implementation into a single commit:

1. **Task 1 + Task 2: ErrorHandler 5xx scrub + X-Request-ID middleware + four integration tests** — `b54b727` (hotfix)

**Plan metadata (this SUMMARY):** docs commit follows.

## Files Created/Modified

- `server/api/internal/handler/health.go` — `ErrorHandler` rewritten: reads `c.Locals("requestid")`, logs once via zap with `request_id`/`status`/`path`/`method`/`zap.Error(err)`, returns `{error, request_id}` for `code >= 500` and `{error}` (verbose) for `code < 500`.
- `server/api/cmd/main.go` — added imports `github.com/gofiber/fiber/v2/middleware/requestid` and `github.com/gofiber/fiber/v2/utils`; inserted `app.Use(requestid.New(requestid.Config{Header: fiber.HeaderXRequestID, Generator: utils.UUIDv4}))` IMMEDIATELY BEFORE `app.Use(recover.New())`. Ordering invariant verified: `requestid.New` at line 98, `recover.New` at line 102.
- `server/api/internal/handler/errorhandler_test.go` (NEW, 178 lines) — four `TestErrorHandler_*` integration tests using `httptest.NewRequest` + `app.Test`, with a private `newErrorHandlerApp()` helper that wires `handler.ErrorHandler(zap.NewNop())` + `requestid.New(...)` exactly as `cmd/main.go` does.

## Test Output

```
=== RUN   TestErrorHandler_Scrubs5xxBody
--- PASS: TestErrorHandler_Scrubs5xxBody (0.00s)
=== RUN   TestErrorHandler_EchoesIncomingRequestID
--- PASS: TestErrorHandler_EchoesIncomingRequestID (0.00s)
=== RUN   TestErrorHandler_GeneratesUUIDv4WhenMissing
--- PASS: TestErrorHandler_GeneratesUUIDv4WhenMissing (0.00s)
=== RUN   TestErrorHandler_PassesThrough4xx
--- PASS: TestErrorHandler_PassesThrough4xx (0.00s)
PASS
ok  	vpnapp/server/api/internal/handler	0.843s
```

Full handler suite (`go test ./internal/handler/... -count=1`): **PASS** (no regressions in adjacent test files: auth, payment, admin, connection, devices, servers).

Build (`go build -o /tmp/vpn-api-build ./cmd`): **OK**.

## Middleware Ordering Verification

```
$ awk '/requestid\.New\(/{r=NR} /recover\.New\(/{c=NR} END{print "requestid.New at line", r, "; recover.New at line", c}' server/api/cmd/main.go
requestid.New at line 98 ; recover.New at line 102
```

Order is correct (requestid BEFORE recover). This means if a downstream handler panics, Fiber's recover converts it into an error that flows to `ErrorHandler`, and `c.Locals("requestid")` is already populated — the scrubbed 5xx body still carries a real `request_id` instead of an empty string.

## Decisions Made

- **5xx body shape locked at exactly two keys** (`error` + `request_id`) — `len(body) != 2` is asserted in the test so any future drift (e.g., somebody adding a `code` field) breaks CI.
- **Helper `newErrorHandlerApp()` lives inside the test file**, not promoted to a shared fixture. Future hotfix tests touching the error handler chain can copy-and-localize rather than couple via a shared helper that drifts.
- **`zap.NewNop()` in tests** rather than asserting on log output. Log shape is covered by manual smoke (plan 09); tests stay focused on the contract that ships to clients.

## Deviations from Plan

None — plan 03 executed exactly as written.

## Issues Encountered

- `go build ./cmd` reported `build output "cmd" already exists and is a directory`. Switched to `go build -o /tmp/vpn-api-build ./cmd` (output to a temp file, then delete) — same compile, no working-tree pollution. Not a real deviation, just an invocation tweak for the verification step.

## User Setup Required

None — change is internal middleware + error-handling logic; no env vars, no dashboard config.

## Next Phase Readiness

- ErrorHandler contract is now stable for plans 04-08 (AdminRequired DB re-read, Lua INCR+EXPIRE, transactional refresh, migrations). Those plans can return raw errors (`return fmt.Errorf(...)`) from handlers and trust `ErrorHandler` to scrub the body.
- Smoke test in plan 09 (Phase 1 deploy + tag) will re-verify on staging: `curl -i -H 'X-Request-ID: smoke-1' .../api/v1/__force-500` → 5xx body matches D-05 shape, header echoed.
- No blockers for plan 04 (HOTFIX-02 AdminRequired DB re-read).

## Self-Check: PASSED

- File exists: `server/api/internal/handler/errorhandler_test.go` — FOUND
- File modified: `server/api/internal/handler/health.go` — present, contains `"internal server error"` and `"request_id"` literals
- File modified: `server/api/cmd/main.go` — present, contains `requestid.New(` and `utils.UUIDv4`
- Commit `b54b727` exists in `git log` — FOUND
- Subject matches `^hotfix\(01\): .*HOTFIX-04` — OK
- All four `TestErrorHandler_*` tests PASS (no SKIP)
- `go build -o /tmp/vpn-api-build ./cmd` exits 0
- Ordering invariant: `requestid.New` (line 98) appears BEFORE `recover.New` (line 102) — OK

---
*Phase: 01-hotfix-audit-critical-fixes*
*Completed: 2026-05-22*
