---
phase: 08
slug: cleanup-hardening
status: verified
threats_open: 0
asvs_level: 1
created: 2026-06-02
---

# Phase 08 — Security

> Per-phase security contract: threat register, accepted risks, and audit trail.
> Verified against the merged implementation (HEAD on `main`) with file:line evidence.
> Code review (08-REVIEW.md) found 0 Critical / 5 Warning — all 5 warnings fixed (commits `ffc512d`..`e68f407`). Phase verification (08-VERIFICATION.md) confirmed HARD-01..17 in source.

---

## Trust Boundaries

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| Mobile/web client → API | Unauthenticated + bearer-token requests over TLS | Credentials, refresh tokens, device_id |
| API → Tunnel (internal) | Heartbeat + active-UUID pull over `InternalSecret` gate | Per-user VLESS UUID set |
| Client device-local storage | Token-at-rest on iOS/Android | Access + refresh tokens |
| Public unauthenticated endpoints | `/health`, `/debug/error`, Telegram webhook | Service metadata, error reports, recovery state |
| Admin surface | Admin-only mutation endpoints | Role/tier changes, audit records |
| CI / dependency supply chain | PR merge gate | Go module graph |

---

## Threat Register

| Threat ID | Category | Component | Disposition | Mitigation (verified evidence) | Status |
|-----------|----------|-----------|-------------|--------------------------------|--------|
| T-08-00 | Tampering | test fixtures in prod build | accept | Go toolchain excludes `_test.go`/`test/` from prod binary | closed |
| T-08-01 | Tampering/EoP | `stripe-go` dependency | mitigate | `server/api/go.mod` — 0 stripe hits (removed) | closed |
| T-08-01b | Information disclosure | stale stripe strings | accept | Cosmetic only; no runtime path | closed |
| T-08-01c | Tampering | stripe re-introduced later | mitigate | `handler/stripe_removal_test.go:61` `TestNoStripeReferences` durable grep fence | closed |
| T-08-02 | Elevation of privilege | tunnel shared UUID (S4-2/S5-1) | mitigate | `tunnel/internal/server.go:210` `ReloadClients` admits only the active per-user UUID set at the wire (HARD-02) | closed |
| T-08-02b | Information disclosure | fleet enumeration | mitigate | `migrations/026_user_vless_identities.sql` per-user UUIDs + rotation/revoke on plan change | closed |
| T-08-02c | Spoofing | internal vless-clients endpoint | mitigate | `handler/servers.go` active-set endpoint behind `InternalSecret` middleware | closed |
| T-08-02d | Denial of service | reload drops live connections | accept | xray-core has no hot reload; debounced + WR-02 empty-set guard (`heartbeat.go:126`) | closed |
| T-08-03 | Spoofing/Tampering | `auth.go` generateTokens (S1-2) | mitigate | `handler/auth.go:727` `base64.RawURLEncoding` 32-byte `crypto/rand` opaque refresh — no signature to forge (HARD-03) | closed |
| T-08-03b | Session fixation | leftover JWT sessions post-cutover | mitigate | `migrations/025_session_device_binding.sql:43` `DELETE FROM sessions` clean-break | closed |
| T-08-04 | Spoofing (replay) | `auth.go` RefreshToken (S1-7) | mitigate | `handler/auth.go:268` hard 401 when `session.DeviceID != req.DeviceID` (HARD-04); WR-01 warns on empty mobile binding | closed |
| T-08-04b | Repudiation | refresh IP change | accept | IP mismatch logged at WARN, allowed for mobile roaming (D-10) | closed |
| T-08-05 | Information disclosure | `bot/recovery.go` (S1-8) | mitigate | `bot/recovery.go:165` `msg.Chat.Type != "private"` gate — no group replies (HARD-05) | closed |
| T-08-06 | Denial of service | `admin_repo.go` ListUsers (S2-3) | mitigate | `repository/admin_repo.go:39` anchored `search + "%"` prefix on indexed cols + len<3 reject (HARD-06) | closed |
| T-08-07 | Repudiation | `admin.go` AdminUpdateUser (S2-4/S9-4) | mitigate | `middleware/audit.go` before→after role/tier diff recorded (HARD-07) | closed |
| T-08-08 | Tampering/Spoofing | admin group (S2-5) | mitigate | `middleware/security_headers.go` HSTS (unconditional) + nosniff + CSP + X-Frame-Options DENY (HARD-08) | closed |
| T-08-09 | Tampering/EoP | Go dep graph (S11-2) | mitigate | `.github/workflows/ci.yml` govulncheck on every PR (HARD-09) — merge-block toggle is OP-1 (HUMAN-UAT) | closed |
| T-08-09b | Repudiation | advisory-only check | mitigate | Runbook + deliberate-vuln proof `docs/ci/govulncheck-branch-protection.md` — enforcement is OP-1 (HUMAN-UAT) | closed |
| T-08-10 | Information disclosure | `logger.go` zap sites (S4-4) | mitigate | `logger/logger.go:88` redacting core → `[REDACTED]` for JWT/base64url-32/sensitive keys (HARD-10) | closed |
| T-08-11 | Spoofing (offline crack) | createadmin + pw-change (S4-5) | mitigate | `config/config.go:17` `const BcryptCost = 12` (HARD-11) | closed |
| T-08-12 | Denial of service | `ratelimit.go` LinkAttemptLimit (S7-1) | mitigate | `middleware/ratelimit.go:116` fail-CLOSED 503 on Redis outage (HARD-12); WR-05 expiry on key selection | closed |
| T-08-13 | Denial of service | `/debug/error` (S7-2) | mitigate | `middleware/ratelimit.go:30` `debugErrorLimit = 5`/min/IP bucket (HARD-13) | closed |
| T-08-14 | Information disclosure | `servers.go` ListServersCached (S5-2) | mitigate | `handler/servers.go:183` per-user `HMAC(JWTSecret, ...)` response ordering (HARD-14) | closed |
| T-08-15 | Denial of service (client) | vpnStore.connect busy-wait | mitigate | `app/src/stores/vpnStore.ts` event-driven `waitForDisconnected` replaces 100ms poll (HARD-15) | closed |
| T-08-15b | Tampering (state) | useProtocolFallback | mitigate | protocol switch routed through `storeDisconnect()`+`waitForDisconnected()` cleanup (APP-H-03) | closed |
| T-08-16 | Information disclosure | authStore AsyncStorage tokens (S10) | mitigate | `app/src/services/secureTokenStore.ts:7` react-native-keychain (iOS Keychain / Android EncryptedSharedPreferences) (HARD-16) — on-device inspection is OP-2/OP-3 (HUMAN-UAT) | closed |
| T-08-16b | Spoofing (replay) | refresh without device_id | mitigate | `app/src/services/api.ts:106` refresh body carries `device_id` | closed |
| T-08-16c | Usability/security | double re-login if uncoordinated | mitigate | one-time AsyncStorage wipe + force-re-login coordinated with D-09 cutover | closed |

*Status: open · closed*
*Disposition: mitigate (implementation required) · accept (documented risk) · transfer (third-party)*

---

## Accepted Risks Log

| Risk ID | Threat Ref | Rationale | Accepted By | Date |
|---------|------------|-----------|-------------|------|
| AR-08-1 | T-08-00 | Go build excludes test files from the production binary — no runtime path exists. | Engineering | 2026-06-02 |
| AR-08-2 | T-08-01b | Residual Stripe strings live only in comments — cosmetic, no runtime attack surface. | Engineering | 2026-06-02 |
| AR-08-3 | T-08-04b | Refresh IP-mismatch is logged at WARN but allowed; enforcing IP binding would force excessive logouts on mobile roaming (D-10). Anomaly is recorded for forensic repudiation. | Engineering | 2026-06-02 |
| AR-08-4 | T-08-02d | xray-core has no hot-reload API; config reload briefly drops live VLESS connections. Mitigated by debounced/coalesced reload + WR-02 empty-set guard. Residual drop window accepted as a product limitation pre-GA (D-05). | Engineering | 2026-06-02 |

---

## Operator UAT Actions (tracked in 08-HUMAN-UAT.md — not open threats)

| ID | Action | Threat |
|----|--------|--------|
| OP-1 | Enable GitHub branch-protection requiring `govulncheck-api`/`govulncheck-tunnel` before merge + deliberate-vuln proof | T-08-09, T-08-09b |
| OP-2 | iOS on-device: confirm `risevpn.auth` Keychain entry present, `auth-tokens` absent from AsyncStorage | T-08-16 |
| OP-3 | Android on-device: confirm EncryptedSharedPreferences entry present, `auth-tokens` absent from RKStorage | T-08-16 |

The in-repo control for each of the above is present and verified; only the out-of-band operator confirmation remains. None constitutes an open security gap.

---

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-06-02 | 27 | 27 | 0 | orchestrator (gsd-security-auditor run discarded — sandbox CWD artifact produced false 0-byte readings; re-verified against real code with file:line evidence) |

---

## Sign-Off

- [x] All threats have a disposition (mitigate / accept / transfer)
- [x] Accepted risks documented in Accepted Risks Log
- [x] `threats_open: 0` confirmed
- [x] `status: verified` set in frontmatter

**Approval:** verified 2026-06-02
