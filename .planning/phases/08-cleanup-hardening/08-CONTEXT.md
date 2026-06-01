# Phase 8: Cleanup & hardening - Context

**Gathered:** 2026-06-02
**Status:** Ready for planning

<domain>
## Phase Boundary

Close every remaining observation from the four audit reports that wasn't fixed in an earlier phase, across four surfaces:

- **Go API** — Stripe dead-code/dep removal, opaque device-bound refresh tokens, admin search hardening + security headers + role-change audit diff, log redaction, bcrypt cost bump, fail-closed rate limiting, `/debug/error` bucket, per-user server ordering, `/health` version removal.
- **Tunnel server (Xray/VLESS+REALITY)** — per-user VLESS UUIDs with real tunnel-side enforcement (reject non-provisioned/revoked UUIDs), rotated on plan change.
- **Mobile app (RN 0.84.1)** — auth tokens moved from AsyncStorage to platform secure storage; `useVpnConnection` refactor + event-driven connect.
- **CI** — `govulncheck` on every PR, blocking.

This is the 17-requirement (HARD-01…HARD-17) hardening sweep. It depends on Phase 3 (lava.top must have been the sole payment path for a stable period before Stripe code is deleted). Independent of Phases 4, 5, 6, 7.

**Not in scope:** new features; Sentry/external error sink (deferred to v2 MUX-01); MMKV adoption beyond what HARD-16 needs; multi-region/scale (v2 SCALE-*); any v2/IDX/MUX item.

</domain>

<decisions>
## Implementation Decisions

### Stripe removal (HARD-01)
- **D-01:** `subscriptions.stripe_id` is **already dropped** by Phase 3's `migrations/020_lava_payments.sql:85` (`ALTER TABLE subscriptions DROP COLUMN IF EXISTS stripe_id`). Success Criterion #1 is therefore already satisfied at the schema level. Phase 8 must **verify the column is absent** (assertion/test) rather than assume. Whether to also add a redundant idempotent `DROP COLUMN IF EXISTS` migration in Phase 8 (so the literal SC wording maps to a Phase 8 file) is **Claude's discretion** — verify-only is acceptable.
- **D-02:** Remove `github.com/stripe/stripe-go/v81 v81.4.0` from `server/api/go.mod` (still present at line 15) and `go.sum`. After Phase 8, `grep -rn stripe server/` returns zero hits in `.go` files.
- **D-03:** **Delete Stripe test fixtures outright.** The Stripe handlers were rewritten to lava in Phase 3 (D-01/D-02), so their tests (`handler/payment_test.go` Stripe cases, any `stripe-go` imports/fixtures in `admin_test.go` or elsewhere) test nothing real. Delete them; do NOT port to lava (the lava path already has `webhook_lava_test.go`). Lock: no test imports `stripe-go` after Phase 8.

### Per-user VLESS UUID (HARD-02)
- **D-04:** **Full Xray enforcement.** The tunnel must actually REJECT a UUID that isn't provisioned for the presenting user (closes audit S4-2 + S5-1). API-only "return a per-user UUID but Xray still accepts the shared one" is explicitly **rejected** — SC #2 must hold at the wire, not just in the API response.
- **D-05:** **Sync mechanism = full Xray config regeneration + reload.** When the active per-user UUID set changes (new user, plan change, revoke), regenerate the Xray JSON config with the current active-UUID list and reload the instance. Xray gRPC `HandlerService` (live AddUser/RemoveUser) was considered and **not chosen** — config regen+reload was selected for simplicity over the existing static-config startup (`server/tunnel/internal/server.go:64` `buildXRayConfig()`). Planner/researcher must design the regen+reload trigger path (API is source of truth; tunnel already heartbeats to API) and the propagation latency target (seconds, not minutes).
- **D-06:** UUID **derivation & tracking is Claude's discretion** (random-UUIDv4-stored-in-DB vs deterministic `HMAC(secret, user_id+epoch)`). Locked behavior: per-user UUID, rotates on plan change, two users on the same plan get **different** UUIDs for the same server.
- **D-07:** Rotation **timing on plan change is Claude's discretion** (immediate revoke vs short grace window). Locked behavior: rotation happens on plan change, old UUIDs are eventually revoked at the tunnel, `GET /servers/:id/config` returns the user's current UUID.

### Refresh tokens (HARD-03 / HARD-04)
- **D-08:** Refresh tokens become **32-byte opaque URL-safe base64 random strings**, NOT JWTs (closes S1-2). **Reuse the `server/api/internal/recovery/start_token.go` pattern** (already does opaque-token generation correctly). Session lookup stays by hash of the token in the `sessions` table.
- **D-09:** **Clean-break cutover — force re-login.** At deploy, all existing JWT refresh sessions become invalid; every user re-authenticates once. **No dual-read / JWT-refresh fallback path** is kept (keeping it would preserve the very code S1-2 wants gone). Justified: zero paying users + "free hand to break things" (REQUIREMENTS Out-of-Scope).
- **D-10:** Refresh session is **bound to `device_id` (hard) + issue-IP (soft)**. `device_id` mismatch on refresh → hard **reject** (401, full re-login). The issue-IP is recorded; on a current-IP ≠ issue-IP mismatch, **log a security event** for anomaly detection but **still allow** the refresh (mobile clients roam cell↔wifi constantly — IP-reject would cause excessive false logouts). Matches the audit's literal wording ("rejects if device_id changes").

### Mobile secure storage (HARD-16)
- **D-11:** Auth tokens must end up in **iOS Keychain + Android EncryptedSharedPreferences** (Xcode-verifiable Keychain entry; tokens absent from the `AsyncStorage` plist). **Library choice is Claude's discretion** (`react-native-keychain` is the leading candidate and satisfies the SC; MMKV-with-encryption does NOT satisfy the Xcode-Keychain check on its own). App is **bare RN 0.84.1** (no Expo) and already ships `react-native-mmkv ^4.3.0`.
- **D-12:** **Migration path (migrate-then-wipe vs force re-login) is Claude's discretion.** Lock: AsyncStorage ends with **no auth tokens**. **Planner note:** D-09's clean-break cutover already invalidates server-side refresh sessions, so a mobile re-login happens at the backend cutover regardless — coordinate the two so a user isn't forced through two separate re-logins, and so migrate-then-wipe doesn't carry forward a token that's already dead server-side.

### Audit-derived defaults (remaining requirements — locked by audit/REQUIREMENTS, planner implements per cited finding)
These were NOT gray-area discussion items; they are specified tightly enough by the audit and REQUIREMENTS that the planner implements them directly.
- **D-13:** HARD-05 — Telegram bot refuses non-private chats: gate on `msg.Chat.Type != "private"` (`bot/recovery.go:179-323`, audit S1-8).
- **D-14:** HARD-06 — Admin user-search requires `len(search) >= 3` and prefix-match on **indexed columns only**; no `CAST(id AS TEXT) ILIKE %x%` sequential scans (`repository/admin_repo.go:20-46`, audit S2-3).
- **D-15:** HARD-07 — Admin role-change audit log records **before→after diff** of the changed fields (`handler/admin.go:144-150`, `middleware/audit.go:79-90`, audit S2-4 / S9-4).
- **D-16:** HARD-08 — Security-headers middleware (HSTS / `X-Content-Type-Options: nosniff` / CSP) applied to the **admin route group** (`cmd/main.go:81-84`, audit S2-5). **CSP exact policy is Claude's discretion** — the admin is a Vite+React SPA (shadcn); the planner should choose a policy that doesn't break it (consider report-only first, allow the necessary `connect-src` to the API).
- **D-17:** HARD-09 — `govulncheck` runs in CI on **every PR**, **blocking** (a PR adding a vulnerable dep fails and is unmergeable, per SC #3). Planner decides where (GitHub Actions) and any suppression mechanism for unfixable advisories.
- **D-18:** HARD-10 — zap encoder gets a **regex redactor** for JWT-shaped and `base64url{32}` strings so a stray `zap.String("token", x)` renders `"token":"[REDACTED]"` (audit S4-4). Redact in-place to `[REDACTED]`.
- **D-19:** HARD-11 — bcrypt cost **10 → 12** for `createadmin` and admin password-change (`createadmin/main.go:61`, `auth.go:187`, audit S4-5).
- **D-20:** HARD-12 — `LinkAttemptLimit` **fails CLOSED** (returns 503) on Redis outage instead of fail-open (`middleware/ratelimit.go:102-108`, audit S7-1). Scope this change to the link-attempt limiter only.
- **D-21:** HARD-13 — `/api/v1/debug/error` gets a dedicated **5/min/IP** rate-limit bucket (`cmd/main.go:134-146`, audit S7-2).
- **D-22:** HARD-14 — `ListServers` returns servers in a **deterministic order rotated per-user via `HMAC(user_id)`** to defeat fleet enumeration via repeated free signups (`handler/servers.go:99-130`, audit S5-2). Apply within the existing cached `ListServersCached` flow (ordering is applied per-request in Go, not cached).
- **D-23:** HARD-15 — Mobile: split `useVpnConnection` (590 lines) into smaller hooks and replace the `vpnStore.connect` busy-wait with an **event-driven wait**. **Behavior-preserving refactor** — no functional change to the connect flow; decomposition shape is Claude's discretion.
- **D-24:** HARD-17 — `/health` no longer returns `runtime.Version()` to unauthenticated callers (`health.go:20-29`, audit S9-2).

### Claude's Discretion
- VLESS UUID derivation/storage scheme (D-06) and rotation timing (D-07).
- Mobile secure-storage library (D-11) and migration path (D-12).
- Stripe verify-only vs redundant migration (D-01).
- Admin CSP exact policy (D-16).
- `useVpnConnection` hook decomposition shape (D-23).

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Audit reports (the four reports this phase closes — phase goal)
- `docs/audit/SECURITY-AUDIT.md` — primary driver. HARD-02→S4-2/S5-1, HARD-03/04→S1-2/S1-7, HARD-05→S1-8, HARD-06→S2-3, HARD-07→S2-4/S9-4, HARD-08→S2-5, HARD-09→S11-2, HARD-10→S4-4, HARD-11→S4-5, HARD-12→S7-1, HARD-13→S7-2, HARD-14→S5-2, HARD-16→S10/§10, HARD-17→S9-2.
- `docs/audit/CODE-REVIEW.md` — code-quality observations (HARD-15 refactor; any unfixed items).
- `docs/audit/PERFORMANCE-AUDIT.md` — perf observations not closed in Phase 6.
- `docs/audit/ADMIN-IMPROVEMENTS.md` — admin observations not closed in Phase 7.
- `docs/audit/MASTER-PLAN.md` — cross-report rollup / triage.

### Specs / ADRs
- `docs/ADR-007-lava-sso-rework.md` — lava.top + SSO design; §8.3 (`lava_webhook_events`), §10.8 (Stripe routes removed), §12.6 (mobile token-storage note that HARD-16 now reverses).

### Reference implementations to reuse / mirror
- `server/api/internal/recovery/start_token.go` — **opaque-token pattern to reuse for HARD-03** (32-byte random, URL-safe base64).
- `server/api/migrations/020_lava_payments.sql` — already drops `subscriptions.stripe_id` (line 85); confirm before adding any new migration (HARD-01).
- `server/api/internal/handler/servers.go` — `ListServersCached` (HARD-14 ordering) and `GET /servers/:id/config` (HARD-02 per-user UUID return; currently shared at the old `:170-172`).
- `server/api/internal/handler/auth.go` — refresh rotation (`:212-269`), JWT-refresh lookup (`:488-541`) to be replaced (HARD-03/04).
- `server/tunnel/internal/server.go` (`buildXRayConfig()` `:64`) + `server/tunnel/internal/config.go` — static Xray config startup; HARD-02 regen+reload hooks here.
- `server/api/internal/middleware/ratelimit.go:102-108` — `LinkAttemptLimit` fail-open to flip CLOSED (HARD-12).
- `server/api/internal/middleware/audit.go:79-90` — audit log to extend with before→after diff (HARD-07).
- `app/src/stores/authStore.ts` — RN token persistence (AsyncStorage today) to move to secure storage (HARD-16); refresh-rotation client logic (HARD-03/04 client side).

### Prior-phase context (cross-phase coordination)
- `.planning/phases/03-lava-top-plans-catalog/03-CONTEXT.md` — D-01/D-03/D-11 (Stripe disposition, stripe_id drop), D-29 (`plan_id` JWT claim).
- `.planning/phases/02-auth-sso-backend/02-CONTEXT.md` — D-24 (logout blacklist), D-25 (JWT HS256 shape).
- `.planning/phases/05-mobile-sso-pro-cta/05-CONTEXT.md` — D-CD (MMKV deferred / AsyncStorage today), Phase-8 conflict warnings on Stripe consumer removal.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `recovery/start_token.go`: opaque random-token generator — directly reusable for HARD-03 refresh tokens.
- `ListServersCached` (`handler/servers.go`): existing cache-first server list; HARD-14 per-user HMAC ordering layers onto its per-request Go filter (ordering must NOT be cached).
- `middleware/audit.go`: existing admin audit middleware — HARD-07 extends it with a field diff.
- `react-native-mmkv ^4.3.0`: already a mobile dep (but not the platform Keychain — see D-11).

### Established Patterns
- `crypto/subtle.ConstantTimeCompare` already used for secret comparison (`auth.go:337`, `devices.go:284`) — mirror for any new secret checks.
- `*jwt.SigningMethodHMAC` algorithm assertion already correct in `ratelimit.go:138-140` — the pattern S1-3 wants applied in `middleware/auth.go`.
- Tunnel Xray config is **built statically at startup** (`server.go:64`) and the instance is started from marshaled JSON — there is **no dynamic Xray user API wired today**; HARD-02 D-05 adds regen+reload.
- API ↔ tunnel: tunnel sends heartbeats to API (`server/tunnel/internal/heartbeat.go`); API is source of truth for users/plans.

### Integration Points
- `cmd/main.go` — route registration, middleware wiring (admin security-headers group HARD-08; `/debug/error` bucket HARD-13).
- `sessions` table — refresh-session rows; HARD-04 adds `device_id` + issue-IP binding columns (new migration likely).
- Tunnel config regen path — new trigger from plan-change / UUID-set change to Xray reload (HARD-02).

</code_context>

<specifics>
## Specific Ideas

- HARD-02 must demonstrably satisfy SC #2 at the wire: two users on the same plan get **different** UUIDs for the same server, and a plan change **rotates** the UUID; the tunnel **rejects** a revoked/foreign UUID.
- HARD-10 redaction must catch even a literal `zap.String("token", x)` → `[REDACTED]` in the aggregator (test with a JWT-shaped and a base64url-32 string).
- HARD-09 must make a vuln-introducing PR **unmergeable** (blocking check), not advisory.
- Coordinate D-09 (backend clean-break) with D-12 (mobile token migration) so users face a **single** re-login, not two.

</specifics>

<deferred>
## Deferred Ideas

- **Full enforcement as a separate phase** — considered for HARD-02 (do API-only now, split the heavy Xray work later) but **rejected**; user chose full enforcement within Phase 8.
- **Xray gRPC HandlerService live AddUser/RemoveUser** — considered as the HARD-02 sync mechanism; deferred in favor of config regen+reload (D-05). Could revisit if regen+reload proves too coarse at scale (relates to v2 SCALE-03 multi-region tunnels).
- **Sentry / external error sink** — Phase 1 D-04 pointed here; actually deferred to v2 (MUX-01). Not in Phase 8.
- **`/auth/logout` endpoint (S1-6), CORS `risevpn.com` origin (S8-1), BodyLimit (S6-1)** — audit items not mapped to a HARD-NN requirement; only close them if the planner finds they're in-scope per the four reports, otherwise leave for a future hardening pass.

</deferred>

---

*Phase: 08-cleanup-hardening*
*Context gathered: 2026-06-02*
