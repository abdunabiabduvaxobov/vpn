---
phase: 8
slug: cleanup-hardening
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-02
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
| HARD-01 | SC#1 | Go API | `grep -rn stripe server/ --include=*.go` == 0; `stripe_id` column absent | shell + Go test + integration | `grep -rn stripe server/ --include=*.go` (expect 0); `TestNoStripeReferences` (durable grep gate, 08-05); existing `migrations_test.go` asserts column absence | ✅ assert / ❌ Wave 2 grep-gate test (08-05) | ⬜ pending |
| HARD-02 | SC#2 | API + tunnel | two same-plan users get different UUIDs; rotate on plan change; **wire rejects revoked UUID** | unit (API) + integration (wire) | API `go test` UUIDs differ + rotate; wire harness: real VLESS handshake with revoked UUID FAILS | ❌ W0 (both) | ⬜ pending |
| HARD-03 | — | Go API | refresh token opaque, not JWT | unit | assert `generateTokens` refresh matches `^[A-Za-z0-9_-]{43}$`, no `.` segments | ❌ W0 | ⬜ pending |
| HARD-04 | SC#4 | Go API | device-B refresh → 401; device-A → 200 | unit | `go test` issue session device A, refresh device B → 401 | ❌ W0 | ⬜ pending |
| HARD-05 | — | Go API/bot | group chat → no reply | unit | feed `Update{Chat.Type:"group"}` → assert no send | ❌ W0 | ⬜ pending |
| HARD-06 | SC#7 | Go API | `len(search)<3` → 400; prefix query, no leading `%` | unit | assert short search rejected; generated SQL has no leading `%` | ❌ W0 | ⬜ pending |
| HARD-07 | — | Go API | audit row carries before→after diff | unit | change role; assert audit `details.role = {before,after}` | ❌ W0 | ⬜ pending |
| HARD-08 | SC#7 | Go API | admin responses carry HSTS/nosniff/CSP | unit | httptest on admin route; assert headers present | ❌ W0 | ⬜ pending |
| HARD-09 | SC#3 | CI | vuln-introducing PR unmergeable | CI check | PR adding known-vuln dep → `govulncheck-action` job red; merge-block via GitHub branch-protection (manual) | ❌ Wave 1 (08-08) | ⬜ pending |
| HARD-10 | SC#6 | Go API | `zap.String("token", jwt)` → `[REDACTED]` | unit | `zaptest/observer` core; JWT-shaped + base64url-32 → `[REDACTED]` | ❌ W0 | ⬜ pending |
| HARD-11 | — | Go API | new hashes are bcrypt cost 12 | unit | `bcrypt.Cost(hash) == 12` | ❌ W0 | ⬜ pending |
| HARD-12 | — | Go API | redis-down → 503 on link attempt | unit | stop miniredis; `LinkAttemptLimit` returns 503 | ❌ W0 | ⬜ pending |
| HARD-13 | — | Go API | 6th call/min/IP on `/debug/error` → 429 | unit | 6 rapid calls one IP | ❌ W0 | ⬜ pending |
| HARD-14 | — | Go API | per-user stable permutation of servers | unit | same user same order; two users differ; set equal | ❌ W0 | ⬜ pending |
| HARD-15 | — | Mobile | `waitForDisconnected` resolves on state change (no busy-wait) | unit (jest) | subscribe-resolves test | ❌ W0 (+manual smoke) | ⬜ pending |
| HARD-16 | SC#5 | Mobile | token in Keychain; absent from AsyncStorage plist | **manual (Xcode)** | sign in; Keychain Access shows entry; AsyncStorage manifest has no `auth-tokens` | ❌ manual-only | ⬜ pending |
| HARD-17 | — | Go API | `/health` has no `go_version` | unit | httptest GET /health; assert key absent | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `server/tunnel/internal/server_reload_test.go` — `ReloadClients`/regen rebuilds config + restarts instance (HARD-02 tunnel side)
- [ ] Wire-level VLESS harness (docker-compose or scripted xray client) — revoked-UUID rejection (HARD-02 SC#2). **Heaviest new infra.**
- [ ] `server/api/internal/handler/servers_vless_test.go` — per-user UUID allocation/rotation + active-set endpoint (HARD-02)
- [ ] `server/api/internal/handler/auth_refresh_device_test.go` — device-bind 401 (HARD-04)
- [ ] `server/api/internal/logger/logger_redact_test.go` — zap redaction (HARD-10)
- [ ] `app/src/stores/vpnStore.test.ts` — `waitForDisconnected` (HARD-15)
- [ ] Manual procedure doc for SC#5 (Xcode Keychain check) — see Manual-Only below

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

## Validation Sign-Off

- [ ] All tasks have automated verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 120s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
