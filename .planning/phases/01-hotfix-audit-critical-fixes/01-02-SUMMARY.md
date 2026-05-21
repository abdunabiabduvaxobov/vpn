---
phase: 01-hotfix-audit-critical-fixes
plan: "02"
subsystem: server/api/internal/config
tags: [hotfix, env-validation, fail-fast, security, HOTFIX-08]
requirements_addressed: [HOTFIX-08]
threat_refs: [T-1-08]
dependency_graph:
  requires:
    - "01-hotfix-audit-critical-fixes/01"  # HOTFIX-06 createadmin landed at base
  provides:
    - "config.RequireEnv() []string — single-pass required-env validator"
    - "config.OptionalEnvWarnings() []string — placeholder/empty Stripe var detector"
    - "cmd/main.go wiring: Fatal-on-miss before config.Load(), Warn-on-placeholder after"
  affects:
    - "API startup contract: empty/missing JWT_SECRET, DATABASE_URL, REDIS_URL, or TUNNEL_VLESS_UUID now exits(1) with one structured log line listing all missing keys"
tech_stack:
  added:
    - "none — stdlib `os` + existing go.uber.org/zap only"
  patterns:
    - "Single-pass aggregate validator (D-04): scan all required vars then emit ONE log line"
    - "logger.Fatal(...) for fail-fast — internally calls os.Exit(1)"
    - "Optional vars use a key→placeholder map so both empty AND known-placeholder values trip the warn path"
key_files:
  created:
    - server/api/internal/config/config_test.go
  modified:
    - server/api/internal/config/config.go
    - server/api/cmd/main.go
decisions:
  - "D-03 honored: required set is the v2.1.0 runtime core only — JWT_SECRET, DATABASE_URL, REDIS_URL, TUNNEL_VLESS_UUID. Stripe vars are optional-with-warn. LAVA_* NOT added (Phase 3 owns that)."
  - "D-04 honored: validator scans every var in one pass, emits a single structured log line listing all missing keys, then logger.Fatal calls os.Exit(1). No partial startups."
  - "Defense-in-depth A3 honored: existing in-Load empty checks at config.go:68-74 (JWT_SECRET and TUNNEL_VLESS_UUID) left in place — RequireEnv short-circuits in cmd/main.go but Load() remains safe to call from tests that don't go through cmd/main.go."
metrics:
  duration_seconds: 162
  duration_minutes: 2
  completed_date: "2026-05-21T23:33:07Z"
  tasks_completed: 2
  files_changed: 3
  lines_added: 158
  lines_removed: 0
  commits: 1
---

# Phase 01 Plan 02: Fail-Fast Aggregate Env Validator (HOTFIX-08) Summary

Implements the single-pass aggregate environment-variable validator from CODE-REVIEW HIGH-08 / SECURITY-AUDIT S3-4/S3-5 so a misconfigured deploy (`JWT_SECRET=""`, `DATABASE_URL` defaulting to localhost in prod, `STRIPE_PRICE_PREMIUM=price_PLACEHOLDER_PREMIUM`) is impossible to start silently — the API now exits with code 1 and a single structured JSON log line listing every missing required key.

## What Shipped

- **`config.RequireEnv() []string`** in `server/api/internal/config/config.go` — scans the four v2.1.0 runtime-core required keys (`JWT_SECRET`, `DATABASE_URL`, `REDIS_URL`, `TUNNEL_VLESS_UUID`) and returns every key that is unset or empty. Per D-03 the LAVA_* keys are intentionally NOT here; Phase 3 adds them when lava.top wires in.
- **`config.OptionalEnvWarnings() []string`** in the same file — flags the four Stripe vars (`STRIPE_KEY`, `STRIPE_WEBHOOK_SECRET`, `STRIPE_PRICE_PREMIUM`, `STRIPE_PRICE_ULTIMATE`) when they are empty OR equal to their known placeholder strings. Stripe leaves in Phase 8 so this list is transitional.
- **Wiring in `server/api/cmd/main.go`**:
  - `RequireEnv()` called immediately after `defer logger.Sync()` and BEFORE `config.Load()` — non-empty return becomes a `logger.Fatal("required environment variables missing", zap.Strings("missing", missing), …)` which calls `os.Exit(1)` internally (satisfies the D-04 single-pass + fail-fast contract).
  - `OptionalEnvWarnings()` called AFTER successful `config.Load()` but BEFORE `stripe.Key = cfg.StripeKey` — non-empty return becomes a `logger.Warn(…)` line so misconfigured Stripe vars are visible in log aggregation but do NOT block startup.
- **Three regression tests** in `server/api/internal/config/config_test.go` (new file):
  - `TestRequireEnv_ReturnsAllMissingKeys` — every required var set to empty via `t.Setenv`, asserts the returned slice contains all four keys.
  - `TestRequireEnv_ReturnsEmptyWhenAllSet` — every required var set to a non-empty value, asserts empty slice.
  - `TestOptionalEnvWarnings_FlagsPlaceholders` — `STRIPE_PRICE_PREMIUM=price_PLACEHOLDER_PREMIUM` (other vars real), asserts the placeholder key appears in `warned` and the real-value keys do not.

## Commit

| Hash | Message |
| --- | --- |
| `af92b63` | `hotfix(01): fail-fast aggregate env validator [HOTFIX-08]` |

## Verification

### Test output

```text
=== RUN   TestRequireEnv_ReturnsAllMissingKeys
--- PASS: TestRequireEnv_ReturnsAllMissingKeys (0.00s)
=== RUN   TestRequireEnv_ReturnsEmptyWhenAllSet
--- PASS: TestRequireEnv_ReturnsEmptyWhenAllSet (0.00s)
=== RUN   TestOptionalEnvWarnings_FlagsPlaceholders
--- PASS: TestOptionalEnvWarnings_FlagsPlaceholders (0.00s)
PASS
ok  	vpnapp/server/api/internal/config	0.350s
```

### Build output

```text
$ cd server/api && go build -o /tmp/vpn-api-final ./cmd
build exit: 0
```

### Acceptance-criteria grep results

| Check | Command | Result |
| --- | --- | --- |
| `RequireEnv` defined | `grep -c '^func RequireEnv()' server/api/internal/config/config.go` | `1` |
| `OptionalEnvWarnings` defined | `grep -c '^func OptionalEnvWarnings()' server/api/internal/config/config.go` | `1` |
| Required keys present (D-03 invariant) | `grep -cE '"(JWT_SECRET\|DATABASE_URL\|REDIS_URL\|TUNNEL_VLESS_UUID)"' server/api/internal/config/config.go` | `8` (4 in impl + 4 in test) |
| **LAVA_ absent (D-03 invariant)** | `grep -c '"LAVA_' server/api/internal/config/config.go` | `0` ✓ |
| `RequireEnv` wired in main.go | `grep -c 'config\.RequireEnv()' server/api/cmd/main.go` | `1` |
| `OptionalEnvWarnings` wired in main.go | `grep -c 'config\.OptionalEnvWarnings()' server/api/cmd/main.go` | `1` |
| `logger.Fatal` on missing env | `grep -c 'logger\.Fatal.*required environment' server/api/cmd/main.go` | `1` |
| Commit message format | `git log -1 --format=%s \| grep -qE '^hotfix\(01\): .*HOTFIX-08'` | PASS |

### Confirmation that no LAVA_* was added (D-03 invariant)

```bash
$ grep -c '"LAVA_' server/api/internal/config/config.go
0
```

Required set in `config.go` is exactly the four runtime-core keys:

```go
required := []string{
    "JWT_SECRET",
    "DATABASE_URL",
    "REDIS_URL",
    "TUNNEL_VLESS_UUID",
}
```

LAVA_* will move into this slice in Phase 3 when the lava.top integration lands.

## Threat Model Coverage

- **T-1-08 (Tampering — silent misconfigured deploy)** — MITIGATED. `RequireEnv()` runs BEFORE `config.Load()` so an empty `JWT_SECRET`, missing `DATABASE_URL`, etc. trips a fail-fast `logger.Fatal` with a single structured log line listing every missing key. Placeholder Stripe vars are surfaced via `OptionalEnvWarnings()` so a "looks fine but checkout will 500" deploy is still visible in log aggregation. Existing in-Load checks at `config.go:68-74` retained as defense-in-depth (A3) so direct callers of `config.Load()` from tests are still safe.

## Deviations from Plan

None — plan executed exactly as written.

- Task 1 created the test scaffold from scratch (file did not exist) per the planned conditional in `<action>`.
- Task 2 implemented all three functions, wired into `cmd/main.go`, replaced the SKIPs with real assertions, and committed atomically with the exact commit message specified.

No auto-fixes were required (no bugs, missing functionality, or blocking issues encountered).

## Authentication Gates

None — pure code change, no external services touched.

## Self-Check

- File `server/api/internal/config/config_test.go` exists: FOUND
- File `server/api/internal/config/config.go` modified (functions added): FOUND (`func RequireEnv()` and `func OptionalEnvWarnings()` present)
- File `server/api/cmd/main.go` modified (calls wired): FOUND (`config.RequireEnv()` and `config.OptionalEnvWarnings()` present)
- Commit `af92b63` exists in git log: FOUND
- Build passes: PASS (`go build ./cmd` exits 0)
- All three tests PASS: PASS (no SKIPs remain)
- D-03 invariant (no LAVA_*): PASS
- D-04 invariant (single log line, fail-fast): PASS (one `logger.Fatal` call with `zap.Strings("missing", missing)`)

## Self-Check: PASSED
