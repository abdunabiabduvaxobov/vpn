---
phase: 8
slug: cleanup-hardening
status: verified
nyquist_compliant: true
wave_0_complete: true
created: 2026-06-02
validated: 2026-06-03
---

# Phase 8 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Derived from `08-RESEARCH.md` "## Validation Architecture".

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework (Go API)** | Go test (stdlib) + testcontainers-postgres + miniredis |
| **Framework (Go tunnel)** | Go test (stdlib) |
| **Framework (Mobile)** | jest 29 (`@react-native/babel-preset`) |
| **Framework (CI)** | GitHub Actions (validated by triggering a PR) |
| **Config file** | `server/api/go.mod`, `server/tunnel/go.mod`, `app/jest.config.js` (existing) |
| **Quick run command** | API: `go test ./internal/<touched>/... -x` (from `server/api`) · Mobile: `npm test -- <file>` (from `app`) |
| **Full suite command** | `go test ./...` (each of `server/api`, `server/tunnel`) · `npm test` (from `app`) |
| **Estimated runtime** | ~60–120s per Go module; ~30s mobile; wire-VLESS harness ~minutes |

---

## Sampling Rate

- **After every task commit:** Run the quick command for the touched surface (`go test ./internal/<touched>/... -x` or `npm test -- <touched>`)
- **After every plan wave:** Run `go test ./...` in both `server/api` and `server/tunnel`; `npm test` in `app`
- **Before `/gsd-verify-work`:** Full suites green + grep-stripe gate + wire-level VLESS integration check + manual SC#5 (Xcode Keychain)
- **Max feedback latency:** 120 seconds (per-surface quick run)

---

## Per-Task Verification Map

| Req | SC | Surface | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|-----|----|---------|-----------------|-----------|-------------------|-------------|--------|
| HARD-01 | SC#1 | Go API | `grep -rn stripe server/ --include=*.go` == 0; `stripe_id` column absent | Go test (durable fence) | `handler/stripe_removal_test.go` `TestNoStripeReferences` | ✅ stripe_removal_test.go | ✅ green |
| HARD-02 | SC#2 | API + tunnel | two same-plan users get different UUIDs; rotate on plan change; **wire rejects revoked UUID** | unit (API) + integration (wire) | `handler/servers_vless_test.go` (isolation+rotation+gated active-set), `repository/vless_repo_active_test.go` (WR-03 partial-unique), `tunnel/internal/server_reload_test.go` (compiles/vets; runs once tunnel linker fixed) + `test/wire-vless/` harness | ✅ all present | ✅ green (API) · 🔶 wire harness manual |
| HARD-03 | — | Go API | refresh token opaque, not JWT | unit | `handler/auth_opaque_refresh_test.go` `TestGenerateTokens_RefreshIsOpaque` | ✅ auth_opaque_refresh_test.go | ✅ green |
| HARD-04 | SC#4 | Go API | device-B refresh → 401; device-A → 200 | unit | `handler/auth_refresh_device_test.go` `TestRefreshToken_DeviceBinding` (4 subtests) + WR-01 `TestWarnIfMobileSessionUnbound` | ✅ auth_refresh_device_test.go | ✅ green |
| HARD-05 | — | Go API/bot | group chat → no reply | unit | `bot/recovery_private_test.go` `TestHandleUpdate_GroupChat_NoReply` (group/supergroup/channel → 0 sends) + `TestHandleUpdate_PrivateChat_Replies` control | ✅ recovery_private_test.go | ✅ green |
| HARD-06 | SC#7 | Go API | `len(search)<3` → 400; prefix query, no leading `%` | unit | `handler/admin_search_test.go` `TestAdminListUsers_ShortSearchRejected` + `TestListUsers_SearchUsesPrefixNoLeadingWildcard` | ✅ admin_search_test.go | ✅ green |
| HARD-07 | — | Go API | audit row carries before→after diff | integration (sqlite) | `handler/admin_audit_diff_test.go` `TestAdminUpdateUser_RoleChange_WritesBeforeAfterDiff` (asserts persisted `details.role.before/after`) | ✅ admin_audit_diff_test.go | ✅ green |
| HARD-08 | SC#7 | Go API | admin responses carry HSTS/nosniff/CSP | unit | `middleware/security_headers_test.go` `TestAdminSecurityHeaders` | ✅ security_headers_test.go | ✅ green |
| HARD-09 | SC#3 | CI | vuln-introducing PR unmergeable | CI check | `.github/workflows/ci.yml` govulncheck job present; merge-block via GitHub branch-protection | ✅ ci.yml | ✅ CI job green · 🔶 branch-protection toggle = OP-1 (HUMAN-UAT) |
| HARD-10 | SC#6 | Go API | `zap.String("token", jwt)` → `[REDACTED]` | unit | `logger/logger_redact_test.go` `TestRedactJWTShaped`/`TestRedactBase64URL32`/`TestRedactByKey` (+2) | ✅ logger_redact_test.go | ✅ green |
| HARD-11 | — | Go API | new hashes are bcrypt cost 12 | unit | `handler/bcrypt_cost_test.go` `TestBcryptCostIs12` | ✅ bcrypt_cost_test.go | ✅ green |
| HARD-12 | — | Go API | redis-down → 503 on link attempt | unit | `middleware/ratelimit_failclosed_test.go` `TestLinkAttemptLimit_RedisDown_FailsClosed` | ✅ ratelimit_failclosed_test.go | ✅ green |
| HARD-13 | — | Go API | 6th call/min/IP on `/debug/error` → 429 | unit | `middleware/debug_error_limit_test.go` `TestDebugErrorLimit_SixthCall429` + `_RedisDown_FailsOpen` | ✅ debug_error_limit_test.go | ✅ green |
| HARD-14 | — | Go API | per-user stable permutation of servers | unit | `handler/servers_order_test.go` `TestServerOrderStablePerUser`/`DiffersBetweenUsers`/`PreservesSet` | ✅ servers_order_test.go | ✅ green |
| HARD-15 | — | Mobile | `waitForDisconnected` resolves on state change (no busy-wait) | unit (jest) | `app/src/stores/vpnStore.test.ts` | ✅ vpnStore.test.ts | ✅ authored (jest blocked in sandbox; CI runs) · 🔶 device smoke |
| HARD-16 | SC#5 | Mobile | token in Keychain; absent from AsyncStorage plist | **manual (Xcode)** + jest | `app/src/services/__tests__/secureTokenStore.test.ts`, `stores/__tests__/authStore.test.ts`, `services/__tests__/api.test.ts` (device_id) | ✅ all present | 🔶 manual-only on-device (SC#5) = OP-2/OP-3 (HUMAN-UAT) |
| HARD-17 | — | Go API | `/health` has no `go_version` | unit | `handler/health_test.go` `TestHealth_NoGoVersion` | ✅ health_test.go | ✅ green |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [x] `server/tunnel/internal/server_reload_test.go` — `ReloadClients`/regen rebuilds config + restarts instance (HARD-02 tunnel side). Compiles+vets; execution blocked by the pre-existing tunnel test-binary linker issue (DEF in `deferred-items.md`).
- [x] Wire-level VLESS harness (`test/wire-vless/` docker-compose + good/foreign UUID client configs) — revoked-UUID rejection (HARD-02 SC#2). Present; run is manual (see Manual-Only).
- [x] `server/api/internal/handler/servers_vless_test.go` — per-user UUID allocation/rotation + active-set endpoint (HARD-02). GREEN.
- [x] `server/api/internal/handler/auth_refresh_device_test.go` — device-bind 401 (HARD-04). GREEN.
- [x] `server/api/internal/logger/logger_redact_test.go` — zap redaction (HARD-10). GREEN.
- [x] `app/src/stores/vpnStore.test.ts` — `waitForDisconnected` (HARD-15). Authored; jest blocked in sandbox, runs in CI.
- [x] Manual procedure doc for SC#5 (Xcode Keychain check) — `docs/manual-verification/08-keychain-asyncstorage.md`

> **Note (Wave 1, not a Go-test Wave-0 dependency):** `.github/workflows/ci.yml` (govulncheck PR job, HARD-09) is *built* in Wave 1 by plan **08-08** (`depends_on: []`), not pre-seeded in Wave 0. It is a CI-config artifact (no Go test depends on it), and its blocking proof is the deliberate-vuln PR + branch-protection step in 08-08 Task 2. Listed here only for cross-reference.

> **Note (SC#1 durable grep gate, Wave 2):** the grep-stripe literal gate is made a *persistent* regression check by `TestNoStripeReferences` in `server/api/internal/handler/stripe_removal_test.go`, created in plan **08-05** (Wave 2). It runs in `go test ./internal/handler/...` and fails if any case-insensitive `stripe` reference appears in a `.go` file under `server/` (excluding the test file itself). This survives as a durable fence rather than a one-shot execution-time grep.

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Auth token present in iOS Keychain, absent from AsyncStorage plist | HARD-16 / SC#5 | Keychain entries are only inspectable through Xcode/Keychain Access on a real device/simulator; not assertable from jest | 1. Build app to iOS simulator. 2. Sign in. 3. Keychain Access (or Xcode device log) shows the `react-native-keychain` service entry. 4. Inspect the app's `RCTAsyncLocalStorage` manifest — confirm no `auth-tokens` key. 5. Android: verify `EncryptedSharedPreferences` xml exists and AsyncStorage has no token. |
| Full VPN connect/disconnect flow after `useVpnConnection` refactor | HARD-15 | Native VPN tunnel lifecycle requires a device | Smoke test connect → connected → disconnect on a physical device; confirm no regression vs pre-refactor behavior |
| govulncheck PR is actually merge-blocked | HARD-09 / SC#3 | GitHub branch-protection "required status check" is a repo-settings toggle outside the codebase | After the workflow lands (08-08, Wave 1), enable branch protection requiring the govulncheck check on `main`; open a deliberate-vuln PR and confirm the merge button is blocked |

---

## Validation Audit 2026-06-03

| Metric | Count |
|--------|-------|
| Requirements | 17 |
| Automated-covered (green) | 15 |
| Mobile (authored; jest sandbox-blocked, runs in CI) | 1 (HARD-15) |
| Manual-only (on-device SC#5) | 1 (HARD-16) |
| Gaps found this audit | 2 (HARD-05, HARD-07 — skip-gated stubs) |
| Gaps resolved | 2 |
| Escalated to manual-only | 0 |

Both gaps were skip-gated RED stubs from 08-01 whose implementations had landed but were never un-skipped:
- **HARD-05** filled in `bot/recovery_private_test.go` — a recording stub `*tgbotapi.BotAPI` (package-internal, no network) asserts group/supergroup/channel `/help` → 0 sends, with a private-chat positive control proving the gate (not a dead handler) suppresses the reply.
- **HARD-07** filled in `handler/admin_audit_diff_test.go` — a SQLite-backed `AuditLog` middleware integration test asserts the role before/after diff persists into the audit row's `details` JSONB (the security-critical persistence seam), avoiding the testcontainer-Postgres infra the handler package lacks.

Remaining non-automated items are genuine HUMAN-UAT (08-HUMAN-UAT.md): the wire-VLESS harness run, the iOS/Android on-device Keychain inspection (SC#5), and the govulncheck branch-protection toggle. The tunnel `server_reload_test.go` compiles/vets but is blocked from executing by a pre-existing toolchain linker issue (deferred-items.md).

---

## Validation Sign-Off

- [x] All tasks have automated verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references
- [x] No watch-mode flags
- [x] Feedback latency < 120s
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** verified 2026-06-03
