---
phase: 08-cleanup-hardening
plan: 03
subsystem: server/api hardening
tags: [security, logging, rate-limiting, bcrypt, headers, enumeration]
requires:
  - "08-01 Wave 0 RED tests (not present in this worktree base — see Deviations)"
provides:
  - "internal/logger.NewRedactingLogger — JWT + base64url-32 + sensitive-key redaction (HARD-10)"
  - "middleware.AdminSecurityHeaders — helmet + unconditional HSTS on admin group (HARD-08)"
  - "config.BcryptCost = 12 used by createadmin + admin password-change (HARD-11)"
  - "middleware.LinkAttemptLimit fail-closed 503 on Redis outage (HARD-12)"
  - "middleware.DebugErrorLimit — 5/min/IP /debug/error bucket, fail-open (HARD-13)"
  - "handler.orderServersForUser — per-user HMAC server ordering (HARD-14)"
affects:
  - server/api/cmd/main.go
  - server/api/internal/middleware/ratelimit.go
  - server/api/internal/handler/servers.go
  - server/api/internal/handler/auth.go
  - server/api/cmd/createadmin/main.go
  - server/api/internal/config/config.go
tech-stack:
  added: []
  patterns:
    - "zapcore.Core wrapper (WrapCore) for cross-cutting field redaction"
    - "HMAC-SHA256 keyed by JWTSecret for deterministic per-user permutation"
    - "fail-closed vs fail-open rate-limit asymmetry by threat type"
key-files:
  created:
    - server/api/internal/logger/logger.go
    - server/api/internal/logger/logger_redact_test.go
    - server/api/internal/middleware/security_headers.go
    - server/api/internal/middleware/security_headers_test.go
    - server/api/internal/middleware/ratelimit_failclosed_test.go
    - server/api/internal/middleware/debug_error_limit_test.go
    - server/api/internal/handler/bcrypt_cost_test.go
    - server/api/internal/handler/servers_order_test.go
  modified:
    - server/api/cmd/main.go
    - server/api/internal/middleware/ratelimit.go
    - server/api/internal/handler/servers.go
    - server/api/internal/handler/auth.go
    - server/api/cmd/createadmin/main.go
    - server/api/internal/config/config.go
    - server/api/internal/handler/servers_test.go
    - server/api/internal/handler/servers_cache_test.go
decisions:
  - "HSTS set unconditionally (custom middleware) because helmet only emits it over https and the API runs behind a TLS-terminating proxy on plain http:3000"
  - "BcryptCost constant placed in config package (zero-dep, imported by both handler and createadmin) rather than handler/auth.go to avoid createadmin->handler import"
  - "Wave 0 RED tests authored in-plan since 08-01 output is absent from this worktree base"
metrics:
  duration: ~50m
  tasks: 3
  files_created: 8
  files_modified: 8
  commits: 4
  completed: 2026-06-02
---

# Phase 8 Plan 03: API Hardening Bundle (main.go / ratelimit.go owners) Summary

Closed six API hardening items sharing `main.go`/`ratelimit.go` ownership: a redacting zap core that scrubs JWT/base64url-32/sensitive-key fields before any sink, helmet+unconditional-HSTS security headers on the admin group, bcrypt cost 10→12 on both production hash sites, a fail-closed link-attempt limiter, a dedicated fail-open `/debug/error` 5/min/IP bucket, and per-user HMAC server ordering that defeats fleet enumeration.

## What Was Built

### Task 1 — Redacting zap logger (HARD-10 / S4-4) — commit 78d9823 (+ 79d55e4)
- New `internal/logger` package. `NewRedactingLogger(base)` wraps the production core via `zap.WrapCore`; a `redactCore` overrides `With`/`Check`/`Write` so that any `StringType` field whose value matches `^[A-Za-z0-9_-]{10,}(\.[...]){2}$` (JWT) or `^[A-Za-z0-9_-]{32,}$` (base64url-32+ opaque token/key), or whose KEY is in `{token, refresh_token, access_token, secret, authorization}`, is replaced with `[REDACTED]` before serialisation. Regexes compiled once at package init. D-18 false-positive tradeoff documented in the package doc.
- `main.go:61` now builds `baseLogger := zap.NewProduction()` then `logger := applog.NewRedactingLogger(baseLogger)`, so every downstream handler/middleware receives the scrubbing logger unchanged.
- TDD: `logger_redact_test.go` (RED→GREEN) asserts JWT + base64url-32 → `[REDACTED]`, short strings + Int/Bool fields untouched, sensitive-key redaction.

### Task 2 — Admin headers + fail-closed link limiter + /debug/error bucket (HARD-08/12/13) — commit ed62a73
- `middleware.AdminSecurityHeaders()` returns helmet (nosniff, CSP `default-src 'none'; frame-ancestors 'none'`, X-Frame-Options DENY) plus an unconditional `Strict-Transport-Security: max-age=31536000; includeSubDomains` setter. Mounted FIRST on the admin group (before auth, so even a 401 carries the headers).
- `LinkAttemptLimit` now FAILS CLOSED: returns 503 on `IncrRateLimit` error so link-code brute force cannot bypass the limiter during a Redis outage. Global `RateLimit` untouched (D-20).
- `DebugErrorLimit(redisClient, logger)`: dedicated bucket keyed `ratelimit:debug:<ip>`, window 60s, limit 5 — 6th call/min/IP → 429. FAILS OPEN on Redis error (a logging endpoint must not break) — deliberate asymmetry vs HARD-12, commented inline. Mounted on `api.Post("/debug/error", middleware.DebugErrorLimit(...), handler)`.
- Tests: `security_headers_test.go`, `ratelimit_failclosed_test.go`, `debug_error_limit_test.go` (miniredis up + dead-port down).

### Task 3 — bcrypt 12 + per-user HMAC server ordering (HARD-11/14) — commit 0e2ff60
- `config.BcryptCost = 12`; both `createadmin/main.go:77` and `auth.go:201` now use it. `bcrypt.DefaultCost` removed from both (0 hits). Existing cost-10 hashes still verify (cost embedded) — no migration.
- `ListServersCached` applies `orderServersForUser(servers, userID, cfg.JWTSecret)` per request, after the response slice is assembled and before `c.JSON`. Sort key = first 8 bytes of `HMAC-SHA256(JWTSecret, userID + ":" + serverID)`, `sort.SliceStable`. Stable per user, differs between users, pure permutation. The cached `cache:servers:active` blob is NEVER reordered — ordering is response-only, in Go, per request.
- Signature threaded `cfg *config.Config` into the handler + the single `main.go` registration.
- Tests: `bcrypt_cost_test.go` (constant + hash cost == 12 + > DefaultCost), `servers_order_test.go` (stable / differs / set-preserved / degenerate).

## Verification

- `go build ./...` exits 0.
- `go vet ./...` (whole api module) clean.
- `go test ./internal/logger/... ./internal/middleware/... ./internal/handler/...` all GREEN (Redact, SecurityHeaders, FailClosed, DebugError, BcryptCost, ServerOrder + all pre-existing tests in those packages).
- Acceptance greps: `NewRedactingLogger|WrapCore` present in main.go (2); `AdminSecurityHeaders` first on admin group; `helmet.New` in security_headers.go; `StatusServiceUnavailable` appears once, inside LinkAttemptLimit only; `DebugErrorLimit` in both ratelimit.go and main.go; `hmac` in servers.go; `bcrypt.DefaultCost` = 0 hits in both target files.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Wave 0 RED tests absent — authored in-plan**
- **Found during:** Tasks 1–3 (read_first steps)
- **Issue:** The plan's `read_first` references Wave 0 RED tests from dependency 08-01 (`logger_redact_test.go`, `security_headers_test.go`, `ratelimit_failclosed_test.go`, `debug_error_limit_test.go`, `bcrypt_cost_test.go`, `servers_order_test.go`). None exist in this worktree — 08-01 runs in a separate parallel worktree and this base (`2122b84`) predates its output.
- **Fix:** Authored each test directly from the plan's `<behavior>` / `<acceptance_criteria>` contracts (TDD RED→GREEN for the logger; behavior-pinning tests for the rest). They live at the exact paths the plan names, so when waves merge they satisfy the same contracts.
- **Files modified:** all 6 `*_test.go` files created.
- **Commits:** 78d9823, ed62a73, 0e2ff60.

**2. [Rule 1 - Bug] helmet does not emit HSTS over plain HTTP — added unconditional HSTS**
- **Found during:** Task 2
- **Issue:** Fiber's helmet only sets `Strict-Transport-Security` when `c.Protocol() == "https"` (fiber/middleware/helmet/helmet.go:67). The API binds plain HTTP on localhost:3000 behind a TLS-terminating proxy, so `c.Protocol()` is `http` and helmet would silently drop HSTS — failing the must-have "Admin API responses carry Strict-Transport-Security".
- **Fix:** `AdminSecurityHeaders()` composes helmet (for nosniff/CSP/X-Frame-Options) with a tiny middleware that sets HSTS unconditionally. The proxy terminates TLS, so advertising HSTS to clients is correct.
- **Files modified:** server/api/internal/middleware/security_headers.go.
- **Commit:** ed62a73.

**3. [Rule 3 - Blocking] ListServersCached signature change broke two existing call sites**
- **Found during:** Task 3
- **Issue:** Threading `cfg *config.Config` into `ListServersCached` (needed for the HMAC key + matching the plan's guidance) broke `servers_test.go:208` and `servers_cache_test.go:96`, which still called the 3-arg form.
- **Fix:** Updated both call sites; added the `config` import to `servers_cache_test.go` and passed `&config.Config{JWTSecret:"test-secret"}`. `servers_test.go` already had `cfg` in scope.
- **Files modified:** servers_test.go, servers_cache_test.go.
- **Commit:** 0e2ff60.

### Note on `VPNServer.ID` type
The plan's HMAC pseudocode used `servers[i].ID.String()` (assuming `uuid.UUID`). The model field is a plain `string`, so the implementation uses `servers[i].ID` directly. Behaviour identical.

### Note on `BcryptCost` placement
The plan suggested `auth.go` or a config const. Placed in the `config` package (zero-dep, already imported by `handler`) so `createadmin` could import it without pulling in the `handler` package (which would be a heavier/awkward dependency for a bootstrap CLI).

## Threat Surface Scan

No new security-relevant surface beyond the plan's `<threat_model>` (T-08-08/10/11/12/13/14, all `mitigate`). All six mitigations implemented. No threat flags.

## Known Stubs

None.

## Self-Check: PASSED

All 8 created files verified present on disk:
- internal/logger/logger.go, internal/logger/logger_redact_test.go
- internal/middleware/security_headers.go, internal/middleware/security_headers_test.go
- internal/middleware/ratelimit_failclosed_test.go, internal/middleware/debug_error_limit_test.go
- internal/handler/bcrypt_cost_test.go, internal/handler/servers_order_test.go

All 4 commits verified in git log:
- 78d9823 (HARD-10 logger), ed62a73 (HARD-08/12/13), 0e2ff60 (HARD-11/14), 79d55e4 (HARD-10 explicit wiring)

Build green, full-module vet clean, logger+middleware+handler test packages all GREEN.
