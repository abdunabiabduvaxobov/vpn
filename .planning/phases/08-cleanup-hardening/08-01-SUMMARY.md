---
phase: 08-cleanup-hardening
plan: 01
subsystem: test-infrastructure
tags: [wave-0, red-tests, vless, refresh-tokens, redaction, bcrypt, ratelimit, mobile, hardening]
dependency_graph:
  requires: []
  provides:
    - "Wave 0 RED/skip test suite pinning HARD-02..17 target behaviors"
    - "Wire-level VLESS rejection harness (HARD-02 SC#2)"
    - "SC#5 Keychain/AsyncStorage manual-verification procedure"
  affects:
    - "All Phase 8 Wave 1-3 implementation plans (these tests flip GREEN as each HARD-NN lands)"
tech_stack:
  added: []
  patterns:
    - "GORM DryRun session to assert generated SQL shape without a live DB"
    - "miniredis .Close() to simulate a Redis outage and assert fail-closed behavior"
    - "compiling t.Skip(\"GREEN when HARD-NN ...\") to pin behavior whose anchor does not exist yet"
    - "zaptest/observer in-memory core for log-redaction assertions"
    - "package-internal _test.go calling unexported buildXRayConfig() to assert tunnel config shape"
key_files:
  created:
    - server/api/internal/handler/health_test.go
    - server/api/internal/handler/auth_opaque_refresh_test.go
    - server/api/internal/handler/bcrypt_cost_test.go
    - server/api/internal/handler/admin_search_test.go
    - server/api/internal/handler/admin_audit_diff_test.go
    - server/api/internal/handler/debug_error_limit_test.go
    - server/api/internal/handler/servers_order_test.go
    - server/api/internal/handler/servers_vless_test.go
    - server/api/internal/handler/security_headers_test.go
    - server/api/internal/handler/auth_refresh_device_test.go
    - server/api/internal/middleware/ratelimit_failclosed_test.go
    - server/api/internal/bot/recovery_private_test.go
    - server/api/internal/logger/logger_redact_test.go
    - server/tunnel/internal/server_reload_test.go
    - app/src/stores/vpnStore.test.ts
    - test/wire-vless/docker-compose.yml
    - test/wire-vless/tunnel-config.json
    - test/wire-vless/client-good-uuid.json
    - test/wire-vless/client-foreign-uuid.json
    - test/wire-vless/README.md
    - docs/manual-verification/08-keychain-asyncstorage.md
    - .planning/phases/08-cleanup-hardening/deferred-items.md
  modified: []
decisions:
  - "Used GORM DryRun to capture the LIKE bound-arg for HARD-06 rather than a live ILIKE query (sqlite rejects ILIKE; DryRun never executes)."
  - "bcrypt cost test mirrors the production DefaultCost path (10) and asserts ==12, going RED now; flips to call the shared cost-12 constant when HARD-11 introduces it."
  - "HARD-05/07/13/14 + HARD-02 API/security-headers are compiling skips because their anchors (injectable bot reply sink, audit-diff capture, DebugErrorLimit, per-user ordering, per-user UUID storage, helmet mount) do not exist yet."
  - "Tunnel reload test adds a REAL buildXRayConfig assertion plus a ReloadClients skip-gate so the buildable invariant is enforced today."
metrics:
  duration: "~45m"
  completed: 2026-06-02
  tasks: 3
  files: 22
---

# Phase 8 Plan 01: Wave 0 Test Infrastructure Summary

RED-first test infrastructure for Phase 8: 13 Go test files, 1 jest test, a
wire-level VLESS rejection docker harness, and the SC#5 Keychain manual doc —
every HARD-NN target behavior is pinned by a failing or compiling-skip test
BEFORE any implementation, so later waves verify against independently-authored
tests rather than self-graded ones.

## What was built

### Task 1 — Go API RED tests (commit `bf08f2a`)
12 Go test files across `internal/handler`, `internal/middleware`, `internal/bot`:

| File | Req | Kind | Behavior pinned |
|------|-----|------|-----------------|
| `health_test.go` | HARD-17 | RED | `/health` must not expose `go_version` |
| `auth_opaque_refresh_test.go` | HARD-03 | RED | refresh token is 43-char base64url, no `.` (not a JWT) |
| `bcrypt_cost_test.go` | HARD-11 | RED | admin password hash is bcrypt cost 12 |
| `admin_search_test.go` | HARD-06 | RED | prefix LIKE (no leading `%`) + len<3 search → 400 |
| `ratelimit_failclosed_test.go` | HARD-12 | RED | `LinkAttemptLimit` returns 503 when Redis is down |
| `auth_refresh_device_test.go` | HARD-04 | SKIP | device-B refresh → 401, device-A → 200, IP-only mismatch OK |
| `admin_audit_diff_test.go` | HARD-07 | SKIP | role change writes `details.role={before,after}` |
| `debug_error_limit_test.go` | HARD-13 | SKIP | 6th `/debug/error` call/min/IP → 429 |
| `servers_order_test.go` | HARD-14 | SKIP | per-user stable permutation of servers |
| `servers_vless_test.go` | HARD-02 | SKIP | per-user UUID allocation/rotation + active-set endpoint |
| `security_headers_test.go` | HARD-08 | SKIP | admin responses carry HSTS/nosniff/CSP |
| `recovery_private_test.go` | HARD-05 | SKIP | group chat → no bot reply |

Result: **6 FAIL (real RED), 6 SKIP** — no silent PASS; api module builds clean.

### Task 2 — cross-surface RED tests (commit `1c6235e`)
- `server/api/internal/logger/logger_redact_test.go` (HARD-10): new package, uses
  `zaptest/observer`, pins the JWT + base64url-32 contract regexes and the
  `[REDACTED]` marker; compiling skip until `logger.NewRedactingLogger` lands.
- `server/tunnel/internal/server_reload_test.go` (HARD-02 tunnel): REAL assertion
  that `buildXRayConfig` admits exactly `Config.Clients`, plus a skip-gate for the
  `(*TunnelServer).ReloadClients` hot-swap.
- `app/src/stores/vpnStore.test.ts` (HARD-15): jest RED for `waitForDisconnected`
  (resolves on `disconnecting→disconnected`; resolves at the timeout cap).

### Task 3 — wire harness + SC#5 doc (commit `43b63e9`)
- `test/wire-vless/` — docker-compose runs the real tunnel (active set `{U1}`)
  plus a scripted xray VLESS client; the good/foreign client configs differ in
  exactly one field (the VLESS `id`). README gives concrete commands and marks
  **Step 4** (foreign UUID → REALITY fallback rejection) as the SC#2 proof.
- `docs/manual-verification/08-keychain-asyncstorage.md` — iOS (Keychain service
  `com.vpnapp` + AsyncStorage `RCTAsyncLocalStorage` manifest) and Android
  (encrypted prefs XML + `RKStorage` sqlite) steps proving the token is in the OS
  secure store and `auth-tokens` is absent from AsyncStorage.

## Verification

- `cd server/api && go build ./...` → exit 0.
- New Go tests run: 6 FAIL + 6 SKIP (handler/middleware/bot); `bot` package passes
  overall because its sole test is a skip. No silent PASS.
- `grep -rl "GREEN when HARD"` across both `internal` trees → 11 files (≥4 required).
- No new test imports `stripe-go`; no `stripe` references in new files.
- `cd server/tunnel && go build ./internal/...` → clean; new tunnel test vets clean.
- Wire harness JSON files all parse; required-content greps (`xray`, `keychain`,
  `asyncstorage`) pass.

## Deviations from Plan

None of Rules 1-4 fired (this is a test-seeding plan; no production code changed).

## Deferred Issues

**Pre-existing tunnel test-binary linker failure** (logged in
`deferred-items.md`): `go test ./internal/...` and `go build ./...` in
`server/tunnel` fail at the LINK stage —
`link: github.com/sagernet/sing/common/control: invalid reference to
net.errNoSuchInterface`. This reproduces on an **untouched** pre-existing test
(`TestAWGServerConfigValidateHappyPath`) and is a toolchain/dep-version
incompatibility in the `sagernet/sing` transitive dep, NOT caused by this plan.
The tunnel `internal` library and the new test source both compile/vet clean; the
two tunnel-side tests are committed and will execute once the linker issue is
fixed (separate task: bump/pin the Go toolchain or sing/xray-core versions).

## Environment Limitation (not a code issue)

The jest test (`app/src/stores/vpnStore.test.ts`) could not be **executed** here:
the git worktree has no `app/node_modules` (worktrees don't share installs) and
the sandbox blocks invoking the node/jest binary. The file is committed, imports
`waitForDisconnected`, and is structured to be RED (first spec asserts
`typeof === 'function'`; behavioural specs throw explicitly when the export is
absent). It will run RED under `npm test -- src/stores/vpnStore.test.ts` once
dependencies are installed.

## Known Stubs

None. All deliverables are RED tests / harness scaffolds / manual docs by design
(this plan deliberately implements no HARD-NN behavior — that is Waves 1-3).

## Self-Check: PASSED

- All 22 created files verified present on disk (plus this SUMMARY = 23).
- All three task commits verified in `git log`: `bf08f2a`, `1c6235e`, `43b63e9`
  (atop the correct base `2122b84`).
