# Phase 8: Cleanup & hardening - Research

**Researched:** 2026-06-02
**Domain:** Multi-surface security hardening (Go API + Xray tunnel + RN mobile + GitHub Actions CI)
**Confidence:** HIGH (codebase anchors verified line-by-line; library currency confirmed via npm + web)

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions (D-01 … D-24)
- **D-01 (HARD-01):** `subscriptions.stripe_id` already dropped by migration `020_lava_payments.sql:85`. Phase 8 **verifies** the column is absent (assertion/test), does not assume. Adding a redundant `DROP COLUMN IF EXISTS` migration is discretion — verify-only acceptable.
- **D-02 (HARD-01):** Remove `github.com/stripe/stripe-go/v81 v81.4.0` from `go.mod` + `go.sum`. After Phase 8, `grep -rn stripe server/ --include=*.go` returns zero.
- **D-03 (HARD-01):** Delete Stripe test fixtures outright (do NOT port to lava). No test imports `stripe-go` after Phase 8.
- **D-04 (HARD-02):** **Full Xray enforcement** — tunnel must REJECT a UUID not provisioned for the presenting user. API-only is explicitly rejected. SC#2 must hold at the wire.
- **D-05 (HARD-02):** Sync = **full Xray config regeneration + reload** (NOT gRPC HandlerService). API is source of truth; tunnel already heartbeats. Propagation target: seconds, not minutes.
- **D-06 (HARD-02 — discretion):** UUID derivation/storage scheme. Locked: per-user UUID, rotates on plan change, two same-plan users get **different** UUIDs.
- **D-07 (HARD-02 — discretion):** Rotation timing on plan change. Locked: rotation happens, old UUIDs eventually revoked at tunnel, `GET /servers/:id/config` returns current UUID.
- **D-08 (HARD-03):** Refresh tokens = 32-byte opaque URL-safe base64, NOT JWTs. Reuse `recovery/start_token.go` pattern. Lookup by token hash in `sessions`.
- **D-09 (HARD-03):** **Clean-break cutover — force re-login.** No dual-read/JWT-refresh fallback. All existing sessions invalidated at deploy.
- **D-10 (HARD-04):** Refresh session bound to `device_id` (hard reject 401 on mismatch) + issue-IP (soft, log-only — mobile roams, no IP reject).
- **D-11 (HARD-16 — discretion lib):** Tokens end in iOS Keychain + Android EncryptedSharedPreferences. Xcode-verifiable Keychain entry; absent from AsyncStorage plist. MMKV-with-encryption does NOT satisfy the check.
- **D-12 (HARD-16 — discretion path):** Migration path (migrate-then-wipe vs force re-login). Lock: AsyncStorage ends with no auth tokens. Coordinate with D-09 single re-login.
- **D-13 (HARD-05):** Telegram bot gates on `msg.Chat.Type != "private"`.
- **D-14 (HARD-06):** Admin search requires `len(search) >= 3`, prefix-match indexed columns only.
- **D-15 (HARD-07):** Role-change audit records before→after diff.
- **D-16 (HARD-08 — discretion CSP):** Security headers on admin route group. CSP policy is discretion; consider report-only first.
- **D-17 (HARD-09):** `govulncheck` blocking on every PR.
- **D-18 (HARD-10):** zap regex redactor for JWT-shaped + base64url{32} → `[REDACTED]`.
- **D-19 (HARD-11):** bcrypt cost 10 → 12 for `createadmin` + admin password-change.
- **D-20 (HARD-12):** `LinkAttemptLimit` fails CLOSED (503) on Redis outage. Scope to link-attempt limiter only.
- **D-21 (HARD-13):** `/api/v1/debug/error` gets dedicated 5/min/IP bucket.
- **D-22 (HARD-14):** `ListServers` deterministic order rotated per-user via `HMAC(user_id)`, applied per-request in Go (NOT cached).
- **D-23 (HARD-15):** Split `useVpnConnection` (591 lines) + replace `vpnStore.connect` busy-wait with event-driven wait. Behavior-preserving; decomposition shape is discretion.
- **D-24 (HARD-17):** `/health` no longer returns `runtime.Version()` to unauthenticated callers.

### Claude's Discretion
- VLESS UUID derivation/storage (D-06) + rotation timing (D-07) — **recommendations below**.
- Mobile secure-storage library (D-11) + migration path (D-12) — **recommendations below**.
- Stripe verify-only vs redundant migration (D-01) — **recommend verify-only**.
- Admin CSP exact policy (D-16) — **recommendation below**.
- `useVpnConnection` hook decomposition shape (D-23) — **recommendation below**.

### Deferred Ideas (OUT OF SCOPE)
- HARD-02 as a separate phase (full enforcement stays in Phase 8).
- Xray gRPC HandlerService live AddUser/RemoveUser (revisit at v2 SCALE-03).
- Sentry / external error sink (v2 MUX-01).
- `/auth/logout` (S1-6 — already shipped, see note), CORS origin (S8-1), BodyLimit (S6-1) — not mapped to a HARD-NN. Only close if a report demands it; otherwise leave for a future pass.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| HARD-01 | Remove all Stripe code + `stripe-go` dep; verify `stripe_id` dropped | §1 — only 5 residual stripe hits; column already dropped (mig 020), verified by `migrations_test.go:203-207` |
| HARD-02 | Per-user VLESS UUID, tunnel enforcement, rotated on plan change | §2 — full design: schema, sync path, reload semantics, discretion recs |
| HARD-03 | Opaque 32-byte refresh tokens (not JWT), reuse start_token pattern | §3 — token already looked up by hash; only `generateTokens` changes |
| HARD-04 | Refresh sessions bound to device_id (hard) + IP (soft) | §3 — new migration 025 adds columns; `RefreshToken` handler binds |
| HARD-05 | Telegram bot refuses non-private chats | §4.1 — gate in `handleUpdate` (recovery.go:159-160) |
| HARD-06 | Admin search ≥3 chars, prefix on indexed cols | §4.2 — `admin_repo.go:21-46` ILIKE %x% rewrite |
| HARD-07 | Role-change audit before→after diff | §4.3 — `admin.go:144-160` + `audit.go:70-90` |
| HARD-08 | Security headers on admin route group | §4.4 — admin group at `main.go:399-403`; Fiber Helmet |
| HARD-09 | govulncheck blocking on every PR | §4.5 — no CI test workflow exists; new `.github/workflows/ci.yml` |
| HARD-10 | zap regex redactor for token-shaped strings | §4.6 — custom zapcore.Core wrap; logger at `main.go:61` |
| HARD-11 | bcrypt cost 10→12 | §4.7 — 2 prod sites: `createadmin/main.go:77`, `auth.go:201` |
| HARD-12 | LinkAttemptLimit fails CLOSED (503) | §4.8 — `ratelimit.go:102-108` |
| HARD-13 | /debug/error dedicated 5/min/IP bucket | §4.9 — `main.go:328-340` |
| HARD-14 | ListServers per-user HMAC ordering | §4.10 — `servers.go:122` ListServersCached |
| HARD-15 | Split useVpnConnection + event-driven connect wait | §5 — `useVpnConnection.ts` 591 lines; `vpnStore.ts:78-93` busy-wait |
| HARD-16 | Mobile tokens → platform secure storage | §6 — react-native-keychain; coordinate with D-09 |
| HARD-17 | /health drops runtime.Version() | §4.11 — `health.go:24-30` |
</phase_requirements>

## Summary

Phase 8 is a 17-item hardening sweep. **11 items are mechanically small and tightly specified** — all canonical file:line anchors were verified against the working tree (with two drift corrections noted below). **Three items carry real design weight:** HARD-02 (per-user VLESS with wire enforcement), HARD-16 (mobile Keychain migration), and HARD-03/04 (opaque device-bound refresh) — and these three have a hidden coupling: D-09's backend refresh cutover and D-12's mobile token wipe must be sequenced so the user re-logs in exactly **once**.

The single largest finding: **xray-core's `core.Instance` has no in-place hot-reload.** The codebase builds a static JSON config once at startup (`server.go:64 buildXRayConfig`) and calls `core.New(...).Start()`. Applying a new UUID set means `instance.Close()` + `core.New()` + `Start()` — which **drops all live VLESS connections** on that tunnel. This is the central constraint shaping the HARD-02 design (D-05 reload coarseness, D-07 rotation timing).

**Primary recommendation:** Sequence the phase so the device-bound-opaque-refresh cutover (HARD-03/04) and the mobile Keychain move (HARD-16) deploy together as one coordinated "re-login wave," and make HARD-02's reload a **debounced, batched, drain-aware** regeneration so a plan change doesn't kill every other user's tunnel. Everything else is independent and parallelizable.

## Anchor Verification (drift report)

All `canonical_refs` paths were checked against the working tree. Results:

| CONTEXT cited | Actual (verified 2026-06-02) | Status |
|---------------|------------------------------|--------|
| `auth.go:212-269` refresh rotation | `RefreshToken` handler at **`auth.go:227-323`**; rotation tx at 264-303 | DRIFT (±line) |
| `auth.go:488-541` JWT-refresh lookup "to be replaced" | **No such path exists.** Refresh token is never JWT-parsed — `RefreshToken` hashes the inbound string (`auth.go:240`) and looks it up. Token *generation* (`generateTokens` at **`auth.go:601`**) signs a JWT. | DRIFT — see §3 (changes the plan materially) |
| `servers.go:170-172` shared UUID | `GetServerConfig` UUID assignment at **`servers.go:291-293`** (`UserID: cfg.TunnelVLESSUUID`) | DRIFT (±line), confirmed |
| `servers.go:99-130` ListServers ordering | `ListServersCached` at **`servers.go:122`** | confirmed (renamed in Phase 6) |
| `ratelimit.go:102-108` LinkAttemptLimit | **`ratelimit.go:98-119`** fail-open `return c.Next()` at line 108 | confirmed |
| `audit.go:79-90` | audit entry built at **`audit.go:83-90`** | confirmed |
| `handler/admin.go:144-150` | `AdminUpdateUser` tier branch at **`admin.go:144-160`** | confirmed |
| `cmd/main.go:81-84` (S2-5 headers) | admin group at **`main.go:399-403`**; no headers middleware exists | confirmed (no helmet today) |
| `cmd/main.go:134-146` (/debug/error) | **`main.go:328-340`** | confirmed |
| `health.go:20-29` | `Health()` returns `runtime.Version()` at **`health.go:24-30`** | confirmed |
| `repository/admin_repo.go:20-46` | `ListUsers` ILIKE at **`admin_repo.go:27-33`** | confirmed |
| `bot/recovery.go:179-323` | dispatch in `handleUpdate` at **`recovery.go:144-176`** (gate belongs at 159-160, before `msg.IsCommand()`); `handleStart` at 180 | DRIFT — gate location refined, see §4.1 |
| `createadmin/main.go:61` | `bcrypt.DefaultCost` at **`createadmin/main.go:77`** | DRIFT (±line) |
| `auth.go:187` admin pw-change bcrypt | `bcrypt.DefaultCost` at **`auth.go:201`** | DRIFT (±line) |
| `auth.go:337`, `devices.go:284` ConstantTimeCompare | pattern present; mirror for new secret checks | confirmed |

---

## §1 — HARD-01: Stripe removal (verify-only + dep drop)

**State today.** `grep -rn stripe server/ --include=*.go` returns exactly **5 hits**, none of them live handler code:
- `cmd/main.go:102` — a WARN string mentioning stripe in `OptionalEnvWarnings` log (cosmetic).
- `migrations/migrations_test.go:203-207` — the **D-01 assertion that already exists** (`WHERE column_name='stripe_id' … must be dropped`).
- `internal/handler/admin_test.go:345` — `stripe_id TEXT` in a test-fixture `CREATE TABLE` (D-03: delete).
- `internal/middleware/version.go:84` — a code comment referencing `/webhook/stripe` (stale comment).
- `internal/model/subscription.go:14` — a doc comment ("dropped stripe_id (migration 020)").

`go.mod:15` still has `github.com/stripe/stripe-go/v81 v81.4.0` **[VERIFIED: read go.mod]**. The column is dropped by `020_lava_payments.sql:85` per ADR-007 §8.3, and `migrations_test.go:203-207` already verifies absence **[VERIFIED]**.

**Plan.**
1. Delete `go.mod:15` line + run `go mod tidy` to purge `go.sum`. The package is genuinely unimported in `.go` files now, so tidy removes it cleanly.
2. Delete the `stripe_id TEXT` line from `admin_test.go:345` fixture (D-03) and any stripe-named test cases in `payment_test.go` (the lava rewrite already replaced them; confirm `webhook_lava_test.go` is the live coverage).
3. Fix the two stale comments (`version.go:84`, `subscription.go:14`) and the `main.go:102` WARN string for cleanliness (optional but closes the grep).
4. **D-01 discretion: recommend verify-only** — the column is already dropped and asserted; a redundant `025_drop_stripe_id.sql` adds a no-op migration purely for literal-SC mapping. Keep `migrations_test.go:203-207` as the SC#1 evidence; do not add a migration.

Confidence: HIGH.

---

## §2 — HARD-02: Per-user VLESS UUID with wire enforcement (HEAVIEST item)

### 2.1 How the tunnel works today

`server/tunnel/internal/server.go` **[VERIFIED: full read]**:
- `Config.Clients []string` (`config.go:18-21`) is the list of allowed VLESS UUIDs, loaded from a static `config.json` at startup.
- `Start()` (`server.go:50`) calls `buildXRayConfig()` (`server.go:185`), marshals to JSON, `serial.LoadJSONConfig` → `core.New(pb)` → `instance.Start()`.
- `buildXRayConfig()` iterates `s.config.Clients` building one VLESS client `{id, flow: "xtls-rprx-vision"}` per UUID (`server.go:188-194`). The WS inbound (`buildWebSocketConfig`, `server.go:248`) builds a parallel client list with empty flow.
- `Stop()` (`server.go:159`) calls `instance.Close()`.

The API side returns the **shared** UUID: `GetServerConfig` (`servers.go:226`) sets `UserID: cfg.TunnelVLESSUUID` at **`servers.go:291-293`** with the comment "Per-user UUIDs will be implemented when tunnel supports dynamic client management." `TUNNEL_VLESS_UUID` is a required env var (`config.go:121,170-171`). This is audit **S4-2/S5-1**.

The only existing API↔tunnel channel is the heartbeat: the tunnel POSTs to `/api/v1/internal/servers/:id/heartbeat` (`heartbeat.go:35`, authed by `X-Internal-Secret`), handled by `HeartbeatServer` (`health.go:169`). It is one-directional (tunnel→API) and currently carries only load.

### 2.2 CRITICAL constraint: xray-core has no hot reload

**[VERIFIED: web — XTLS/Xray-core Discussion #1060, #2596, Issue #1981, Marzban #1981]** xray-core's embedded `core.Instance` (the `core.New().Start()` we use) provides **no in-process config reload**. Two facts:
- Restarting the instance to apply a new client set **drops all live connections** on that tunnel. There is no graceful socket-handoff (the haproxy-style reload is an open feature request, #2596).
- The gRPC `HandlerService` (Add/RemoveUser) *can* mutate users live without restart — **but D-05 explicitly rejected it**, and newly-added users via the API have shown connection-timeout bugs until restart (Marzban #1981). D-05's "regen + reload" therefore means: rebuild the JSON, `Close()` the old instance, `core.New()` a fresh one, `Start()`. **This is a connection-dropping operation.**

Implication for design: **reloads must be rare and batched, never per-user-per-event.** A naive "rotate UUID on plan change → reload" would drop every connected user on that tunnel each time any one user upgrades.

### 2.3 Recommended sync architecture (designs the D-05 trigger path)

API is source of truth. Add a **pull-based config sync** layered on the existing heartbeat:

1. **Schema (D-06 storage):** store per-user UUID in the DB. Recommend a new table rather than a `users` column, because UUIDs are *per (user, rotation-epoch)* and you want revocation history:
   ```sql
   -- migration 025 (or 026 if 025 is the sessions migration — see §3)
   CREATE TABLE user_vless_identities (
       id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
       user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
       vless_uuid   UUID NOT NULL UNIQUE,          -- the actual VLESS client id
       is_active    BOOLEAN NOT NULL DEFAULT TRUE, -- false = revoked, kept for grace window
       created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
       revoked_at   TIMESTAMPTZ
   );
   CREATE INDEX idx_uvi_user_active ON user_vless_identities(user_id) WHERE is_active = TRUE;
   CREATE INDEX idx_uvi_active ON user_vless_identities(is_active) WHERE is_active = TRUE;
   ```

2. **D-06 derivation — recommend random UUIDv4 stored in DB, NOT deterministic HMAC.** Rationale:
   - **Revocation:** rotation must *invalidate* the old UUID. With deterministic `HMAC(secret, user_id+epoch)` you can recompute it but you still need a stored epoch + a revocation list — so you end up storing state anyway, with the added burden of a global secret whose rotation re-keys *every* user at once. Random-in-DB gives free per-row revocation (`is_active=false`).
   - **Collision:** UUIDv4 (122 bits random) collision is negligible; the `UNIQUE` constraint is a belt-and-suspenders catch. `github.com/google/uuid v1.6.0` is already a dep.
   - **Two same-plan users differ automatically** (D-06 lock) because each gets an independent random row.
   - **No secret-management surface** (the HMAC secret would be a new Critical secret to protect/rotate).

3. **Active-set endpoint (the pull):** add `GET /api/v1/internal/servers/:id/vless-clients` to the existing `internalGroup` (`main.go:302`), authed by the same `InternalSecret` middleware. Returns the current `is_active=TRUE` UUID set the tunnel should admit. The tunnel polls this on its heartbeat tick (it already has the base URL, server id, secret, and a 30s ticker — `heartbeat.go`, `tunnel/cmd/tunnel/main.go`). On a changed set (compare a hash/etag), the tunnel regenerates config and reloads.
   - **Why pull, not push:** the API is multi-replica-capable (PERF-06 `RUN_SCHEDULER`); a push would need the API to hold tunnel connections. Pull reuses the heartbeat channel and keeps the tunnel the only party that restarts xray.

4. **Reload coarseness controls (mandatory given §2.2):**
   - **Debounce:** on the tunnel, coalesce set-changes — wait e.g. 5–10s after a detected change before reloading, so a burst of plan changes triggers one reload.
   - **Propagation floor:** worst case = heartbeat interval (30s default, `main.go` enforces min 30s) + debounce. **Realistic target: 30–60s**, not "seconds." If a tighter SLA is needed, lower the heartbeat interval *for the config-poll specifically* (a separate, faster ticker) — but every reload still drops live connections, so there is a hard floor on how aggressive you want to be. Document the 30–60s number as the SC#2 propagation expectation.
   - **Drain awareness:** consider reloading only when the tunnel is in a low-traffic window, or accept the connection drop as the cost (free-tier, pre-launch, acceptable per REQUIREMENTS Out-of-Scope "free hand to break things"). Recommend: accept the drop, document it, debounce hard.

5. **D-07 rotation timing — recommend immediate revoke + short overlap, NOT a long grace window.** Because reload is coarse and connection-dropping anyway: on plan change, INSERT the new active UUID and mark the old `is_active=false` in the same transaction, but let the **next batched reload** actually evict it. Net effect: the new UUID works as soon as the client re-fetches `/servers/:id/config`; the old one keeps working only until the next reload (the natural ~30–60s window IS the grace period — no separate timer needed). This is simpler than an explicit grace timer and exploits the reload coarseness rather than fighting it.

6. **API write paths that must allocate/rotate UUIDs:**
   - First config fetch with no active identity → lazily allocate (in `GetServerConfig`, `servers.go:291`): replace `cfg.TunnelVLESSUUID` with `repository.GetOrCreateActiveVlessUUID(ctx, db, userID)`.
   - Plan change → rotate. The plan-change write already funnels through `SetUserPlan` (ADR-007 §19.4) / `AdminUpdateUser` (`admin.go:144`); add a UUID rotation in the same transaction. The lava webhook `payment.success` path also changes plan — rotate there too (it holds the per-user advisory lock, ADMIN-03).
   - Revoke-all (admin force-cancel / suspend) → mark all the user's identities inactive.

7. **Tunnel-side reload:** refactor `TunnelServer` to expose `ReloadClients(uuids []string) error` that rebuilds both the REALITY and WS configs, `Close()`s the old instance(s), and `Start()`s new ones under `s.mu`. Note: the WS instance and REALITY instance both consume `s.config.Clients`, so both reload together. Health-server and heartbeat goroutines are independent and survive.

### 2.4 HARD-02 validation (SC#2 at the wire)

- **Different UUIDs same plan:** create two users on plan `pro`, fetch `/servers/:id/config` for each, assert the returned `UserID` differs.
- **Rotation on plan change:** capture user's UUID, change plan, assert `/servers/:id/config` returns a new UUID and the old row is `is_active=false`.
- **Wire rejection (the hard one):** after a reload, attempt a real VLESS handshake using a revoked/foreign UUID against the tunnel and assert it is REJECTED (REALITY falls back to the impersonated dest, connection does not establish a VLESS session). This needs an integration harness with the actual xray tunnel — likely a docker-compose test or a manual scripted check. Flag as the heaviest validation; see §Validation Architecture.

Confidence: HIGH on mechanism, MEDIUM on the exact reload API surface (depends on how cleanly `TunnelServer` can be refactored — verify no global state assumes single-start).

---

## §3 — HARD-03/04: Opaque device-bound refresh tokens

### 3.1 The reality (important — corrects CONTEXT drift)

The refresh token is **already opaque at the lookup layer.** `RefreshToken` (`auth.go:227`) does `tokenHash := sha256(req.RefreshToken)` then `FindSessionByTokenHash` (`auth.go:240-241`). The token is **never JWT-parsed.** There is no `auth.go:488-541` "JWT-refresh lookup to replace" — that path does not exist. **[VERIFIED: full read of auth.go + session_repo.go]**

The *only* S1-2 violation is that `generateTokens` (`auth.go:601`, specifically **`auth.go:621-633`**) currently *mints* the refresh token as a signed HS256 JWT (`refreshClaims{sub,type:"refresh",iat,exp}`). The JWT envelope is pure dead weight — nothing verifies it. **HARD-03 is therefore a localized change to `generateTokens`:** replace the JWT mint with a 32-byte opaque random string.

### 3.2 HARD-03 plan (reuse start_token.go pattern)

`recovery/start_token.go` **[VERIFIED]** uses `crypto/rand` + `base64.RawURLEncoding` (24 bytes → 32 chars). D-08 wants 32 bytes:
```go
// in generateTokens, replace the refreshClaims/jwt.NewWithClaims block (auth.go:621-633):
raw := make([]byte, 32)
if _, err := rand.Read(raw); err != nil {
    return nil, fmt.Errorf("generating refresh token: %w", err)
}
refreshString := base64.RawURLEncoding.EncodeToString(raw) // 43 chars, opaque
```
`storeRefreshSession` (`auth.go:579`) already SHA-256-hashes whatever string it's given and stores it — **no change needed there.** The hash column is `VARCHAR(64)` (SHA-256 hex), unaffected by the token format change. All 7 `storeRefreshSession` call sites keep working.

**D-09 clean-break:** because the format changes and there is no dual-read, every existing session row's stored hash corresponds to a JWT the client holds; after deploy the client sends its old JWT, server hashes it, finds the row (it still matches!), and... here's the subtlety: **the old JWT-format tokens would still validate** since lookup is by hash of the raw string. To force the clean break per D-09, the migration must **`DELETE FROM sessions`** (or the deploy truncates sessions) so every client is forced to re-login. This is the single re-login event that must coordinate with HARD-16 (see §Open Risks).

### 3.3 HARD-04 plan (device + IP binding)

`model.Session` (`user.go:60-67`) today: `ID, UserID, RefreshTokenHash, DeviceInfo, CreatedAt, ExpiresAt`. The `sessions` table (`001_initial.sql:18-25`) has no `device_id`/`issue_ip`.

**New migration (next free number — 025):**
```sql
ALTER TABLE sessions
    ADD COLUMN device_id VARCHAR(255),   -- bound at issue; hard-checked on refresh (D-10)
    ADD COLUMN issue_ip  VARCHAR(45);    -- bound at issue; soft-checked, log-only (D-10)
CREATE INDEX idx_sessions_device_id ON sessions(device_id);
```
(IPv6-safe 45 chars.) Add the fields to `model.Session` and to `storeRefreshSession` (which must now accept `deviceID, issueIP` — thread them from each call site; `device_id` comes from the request body / fingerprint, `issue_ip` from `c.IP()`).

**Refresh enforcement** in `RefreshToken` (`auth.go:227`):
- Parse `device_id` from the refresh request body (the mobile client must send it — see §6 client change).
- After `FindSessionByTokenHash`, compare `session.DeviceID` vs request `device_id`: **hard reject 401** on mismatch (D-10). Use a plain string compare (these are not secrets; `device_id` is an OS identifier — constant-time not required, but harmless).
- Compare `session.IssueIP` vs `c.IP()`: on mismatch **log a security event** (`logger.Warn` with both IPs + user_id) and **continue** (D-10 — mobile roams cell↔wifi). The new rotated session carries the *original* issue_ip preserved (or the new IP — recommend preserving original so roaming doesn't reset the anomaly baseline; document the choice).
- Rotation already happens in a transaction (`auth.go:264-303`); the new session row must carry forward `device_id` and `issue_ip`.

**Backfill:** the D-09 `DELETE FROM sessions` means no NULL-device legacy rows survive — every post-cutover session is created with `device_id` populated. Clean.

Confidence: HIGH.

---

## §4 — Tightly-specified API items (confirmed anchors)

### 4.1 HARD-05 — Telegram private-chat gate (D-13)
`handleUpdate` (`recovery.go:144`) **[VERIFIED]**. The `msg` is obtained at `recovery.go:158`; the command dispatch is at 164-175. **Gate location:** insert immediately after the `msg == nil || msg.From == nil` guard (`recovery.go:159-160`) and before `msg.IsCommand()`:
```go
if msg.Chat == nil || msg.Chat.Type != "private" {
    return // S1-8: refuse group/supergroup/channel chats silently
}
```
`tgbotapi.Chat.Type` is `"private"|"group"|"supergroup"|"channel"` in `go-telegram-bot-api/v5`. CONTEXT cited `recovery.go:179-323` (the `handleStart`/`handleRestore` body) but the correct, cheapest gate is at the `handleUpdate` ingress so it covers `/status` and `/help` replies too (which also leak via `msg.Chat.ID`). Test: feed an `Update` with `Chat.Type="group"` and assert no reply is sent.

### 4.2 HARD-06 — Admin search hardening (D-14)
`ListUsers` (`admin_repo.go:21`) **[VERIFIED]**: line 27-33 builds `like := "%"+search+"%"` and `CAST(id AS TEXT) ILIKE ? OR email_hash ILIKE ? OR full_name ILIKE ?` — the S2-3 sequential-scan pattern. Rewrite:
- Reject `len(search) < 3` at the handler (`AdminListUsers`) → 400 (or treat as empty filter — recommend 400 with a clear message so the UI knows).
- Use **prefix** match (`search+"%"`, no leading `%`) so the `idx` on `full_name`/`email_hash` can be used. `CAST(id AS TEXT) ILIKE` cannot use an index — drop the id-cast branch, or match `id::text LIKE search||'%'` only when `search` looks like a UUID prefix (recommend: prefix-match `id::text` is still a scan; **drop the id branch entirely** per D-14 "indexed columns only," and document that exact-id lookup is a separate code path if needed). `email_hash` is a SHA-256 hex — prefix match is meaningless there (the user types an email, not a hash); recommend matching `full_name` prefix only, plus an exact `email_hash = sha256(search)` equality when the input is a full email. Confirm the indexes exist (`idx` on `full_name`?) — if not, add them in the same migration.

### 4.3 HARD-07 — Role-change audit diff (D-15)
`AdminUpdateUser` (`admin.go:144-160`) builds an `updates` map **[VERIFIED]**. `AuditLog` middleware (`audit.go:70-90`) records `method/path/params/query` but **no body diff** (S9-4). Plan: capture the user's `role`/`subscription_tier` **before** the update, compute the changed-field diff, and write it into `model.AuditDetails` (the `details` JSONB at `audit.go:72`). Cleanest: do the before→after capture inside the handler (it has both states) and attach via `c.Locals` for the middleware to merge, OR write a dedicated audit entry in the handler for role changes. Recommend handler-side diff (the middleware is generic and shouldn't re-query). Test: change a user's role, assert the audit row's `details` contains `{role: {before:"user", after:"admin"}}`.

### 4.4 HARD-08 — Admin security headers + CSP (D-16)
Admin group at `main.go:399-403` **[VERIFIED]** (`api.Group("/admin", authMiddleware, AdminRequired, AuditLog)`). No security-headers middleware exists anywhere (`grep` for HSTS/nosniff/CSP returned nothing). **Use Fiber v2's built-in Helmet** (`github.com/gofiber/fiber/v2/middleware/helmet`) — already in the Fiber module (v2.52.5), no new dep. Add as the first middleware on the admin group:
```go
admin := api.Group("/admin",
    helmet.New(helmet.Config{
        HSTSMaxAge:            31536000,
        HSTSIncludeSubdomains: true,
        ContentSecurityPolicy: "<see below>",
        // ContentTypeNosniff defaults to "nosniff"
    }),
    authMiddleware, middleware.AdminRequired(db), middleware.AuditLog(db, logger),
)
```
**D-16 CSP recommendation (the admin is a Vite+React SPA served from `vpnadmin.mydayai.uz:9443`, API at `vpnapi.mydayai.uz:9443`):** the admin SPA is served by its **own** host (deploy-admin.yml), so these headers on the *API* admin routes guard the JSON API responses, not the SPA HTML. The high-value headers here are HSTS + `X-Content-Type-Options: nosniff` + `X-Frame-Options: DENY`. CSP on a JSON API has limited effect but set a tight `default-src 'none'; frame-ancestors 'none'` for the API responses. **Start report-only is unnecessary for a JSON API** (no inline scripts to break) — recommend enforcing `default-src 'none'` directly. Note the SPA's own CSP (allowing `connect-src` to the API) belongs in the admin-web host config, which is **out of scope** here (Phase 8 is API/tunnel/mobile/CI). Document that the SPA-side CSP is a separate admin-web concern.

### 4.5 HARD-09 — govulncheck blocking in CI (D-17)
**No CI test/lint workflow exists** — `.github/workflows/` has only `deploy.yml`, `deploy-admin.yml`, `deploy-landing.yml`, all `push: branches:[main]` build-and-deploy jobs **[VERIFIED]**. So HARD-09 creates a **new `pull_request`-triggered workflow** (e.g. `.github/workflows/ci.yml`). Use the official `golang/govulncheck-action` **[CITED: github.com/golang/govulncheck-action]**:
```yaml
name: CI
on:
  pull_request:
    paths: ['server/**']
jobs:
  govulncheck:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: golang/govulncheck-action@v1
        with:
          go-version-file: server/api/go.mod
          work-dir: server/api
```
**Blocking semantics:** the official action **fails the job (non-zero exit) when a vulnerability is found by default** — this is the behavior D-17 wants (a vuln-introducing PR is unmergeable). **[CITED: govulncheck-action README]** Note: some third-party actions default to non-failing; the *official* `golang/govulncheck-action` exits non-zero on findings. To make it actually *block merge*, the repo must add a **branch-protection required status check** named after this job — that config is **GitHub-side (repo settings → Branches), not in-repo.** Flag this as a manual GitHub step the plan must call out (a YAML file alone does not block merge until the check is marked required).
**Suppression:** `golang/govulncheck-action` has **no built-in silencing** **[CITED: README]**. For an unfixable advisory, the standard approach is govulncheck's own mechanism — run `govulncheck` directly in the step and post-process, OR pin/replace the dep. Recommend: document that suppression = upgrade the dep; if truly unfixable, switch that one job to `govulncheck ... ; echo` with an explicit allowlist comment (rare). Also run the tunnel module (`server/tunnel`) as a second job — it imports `xtls/xray-core`, a high-churn dep.

### 4.6 HARD-10 — zap token redactor (D-18)
Logger constructed at `main.go:61` via `zap.NewProduction()` **[VERIFIED]** (returns a `*zap.Logger` with a JSON encoder). zap version **v1.27.0** (`go.mod`). D-18 must catch even `zap.String("token", x)` — meaning redaction must happen at the **field-value** level, after fields are resolved but before they hit the output. The two viable approaches:
- **(A) Custom `zapcore.Core` wrapper** (recommend): wrap the production core so its `Write(ent, fields)` walks `fields []zapcore.Field`, and for any `Field.Type == zapcore.StringType` whose `Field.String` matches the JWT-shaped or `base64url{32,}` regex, replace with `[REDACTED]` before delegating to the inner core. Build via `zap.New(zapcore.NewCore(...))` or `logger.WithOptions(zap.WrapCore(func(c zapcore.Core) zapcore.Core { return &redactCore{c} }))`. This catches every call site, including `zap.String("token", x)`, because it inspects resolved fields.
- **(B) Custom encoder wrapper** — wrap `zapcore.Encoder.EncodeEntry`/`AddString`. More invasive (must wrap every `Add*` method) and brittle. Reject in favor of (A).
**Regexes** (compile once):
  - JWT-shaped: `^[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}$` (three base64url segments).
  - opaque/base64url-32: `^[A-Za-z0-9_-]{32,}$` (catches the new refresh tokens AND start-tokens). Be careful: this also matches some long IDs — acceptable for a security-conservative redactor; document the false-positive tradeoff.
  Apply to the field *value*; also consider redacting by *key* (field key in {`token`,`refresh_token`,`access_token`,`secret`,`authorization`}) as a cheap belt-and-suspenders. Recommend value-regex (D-18's literal requirement) + key-name as bonus.
**Test:** log `zap.String("token", "<jwt>")` and `zap.String("x", "<43-char-base64url>")` through the wrapped logger to an in-memory sink (`zaptest/observer` or a buffer core) and assert the output contains `[REDACTED]`, not the secret.

### 4.7 HARD-11 — bcrypt cost 10→12 (D-19)
**Two prod sites** **[VERIFIED]**: `createadmin/main.go:77` and `auth.go:201` (admin password-change), both `bcrypt.GenerateFromPassword(..., bcrypt.DefaultCost)`. Replace `bcrypt.DefaultCost` (=10) with a constant `const bcryptCost = 12` (or `bcrypt.DefaultCost+2`). The two test sites (`auth_test.go:198,226`) can keep DefaultCost (faster tests) or be bumped — recommend leaving tests at low cost for speed and adding one test asserting the production path uses 12 (decode the hash's cost prefix with `bcrypt.Cost(hash)`). Note: existing admin hashes at cost 10 still verify fine (bcrypt embeds cost); new/changed passwords get 12. No migration needed.

### 4.8 HARD-12 — LinkAttemptLimit fail-CLOSED (D-20)
`LinkAttemptLimit` (`ratelimit.go:98`) **[VERIFIED]**: on `IncrRateLimit` error it logs Warn and `return c.Next()` (`ratelimit.go:108`) — fail-OPEN (S7-1). Flip to fail-CLOSED:
```go
if err != nil {
    logger.Error("link rate limit check failed — failing closed", zap.String("ip", c.IP()), zap.Error(err))
    return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "service temporarily unavailable"})
}
```
**Scope: ONLY this limiter** (D-20). The global `RateLimit` (`ratelimit.go:40`) keeps its current behavior — do not touch it. Test: inject a failing redis client (miniredis stopped, already a dep) and assert 503.

### 4.9 HARD-13 — /debug/error dedicated bucket (D-21)
`/debug/error` registered at `main.go:328-340` **[VERIFIED]** with only the global `RateLimit` applied (mounted on `api` at `main.go:251`). Add a dedicated 5/min/IP limiter as route middleware. Build a small `DebugErrorLimit(redisClient, logger)` mirroring `LinkAttemptLimit`'s shape but with `key := "debug:"+c.IP()`, window 60s, limit 5. Mount: `api.Post("/debug/error", debugErrorLimit, handler...)`. **Decide fail-open vs closed:** this is a logging endpoint, not auth — recommend fail-OPEN here (a redis outage shouldn't break client error reporting), unlike HARD-12. Document the asymmetry. Test: 6 rapid calls from one IP → 6th returns 429.

### 4.10 HARD-14 — Per-user HMAC server ordering (D-22)
`ListServersCached` (`servers.go:122`) **[VERIFIED]** — the final `servers` slice (built at `servers.go:171-184` for non-admins, or `fullServers` for admins) is returned at the JSON marshal. D-22: apply a deterministic per-user rotation **in Go, per-request, AFTER the cache read** (never cache the ordering — the cached blob is the shared full list). Implementation:
```go
// after `servers` is assembled, before c.JSON:
order := func(i, j int) bool {
    hi := hmacSort(userID, servers[i].ID, cfg.JWTSecret)
    hj := hmacSort(userID, servers[j].ID, cfg.JWTSecret)
    return bytes.Compare(hi, hj) < 0
}
sort.SliceStable(servers, order)
// hmacSort = hmac-sha256(key=userID-or-secret, msg=serverID) → first 8 bytes
```
Use `crypto/hmac` + `crypto/sha256` (stdlib). Key choice: HMAC keyed by a server secret (e.g. `cfg.JWTSecret`) over message `userID+":"+serverID` so two users get different but stable orderings and the order isn't externally predictable (defeats S5-2 fleet enumeration). Stable across requests for the same user. Test: same user → same order across calls; two users → different orders; all servers still present (it's a permutation, not a filter).

### 4.11 HARD-17 — /health drops Go version (D-24)
`Health()` (`health.go:24-30`) **[VERIFIED]** returns `"go_version": runtime.Version()`. Delete that line. Keep `status/uptime/timestamp`. The `runtime` import may become unused — check (it's only used here in this file region; `health.go` may use it elsewhere — verify and drop the import if orphaned). Test: GET /health, assert no `go_version` key.

Confidence: HIGH for all of §4.

---

## §5 — HARD-15: useVpnConnection refactor + event-driven connect (D-23)

**Sources** **[VERIFIED]**: `app/src/hooks/useVpnConnection.ts` = **591 lines** (CODE-REVIEW APP-M-04). The busy-wait is in `app/src/stores/vpnStore.ts:78-93` (CODE-REVIEW APP-H-04): `connect()` polls `get().connectionState === 'disconnecting'` every 100ms for up to 3s via `await new Promise(r => setTimeout(r, 100))`, blocking the flow. Also APP-H-03: `tryNextProtocol` at `useVpnConnection.ts:161-225` uses direct setState bypassing disconnect cleanup.

**D-23 is a behavior-preserving refactor** — no functional change to the connect flow. Recommendations:
- **Event-driven wait (replaces the 100ms poll):** instead of polling `connectionState`, have `disconnect()` resolve a promise / fire an event when it reaches `'disconnected'`, and have `connect()` `await` that. Zustand has no built-in event bus, but `useVpnStore.subscribe((state) => state.connectionState)` can resolve a one-shot promise:
  ```ts
  function waitForDisconnected(timeoutMs = 3000): Promise<void> {
    return new Promise((resolve) => {
      if (useVpnStore.getState().connectionState !== 'disconnecting') return resolve();
      const unsub = useVpnStore.subscribe((s) => {
        if (s.connectionState !== 'disconnecting') { unsub(); resolve(); }
      });
      setTimeout(() => { unsub(); resolve(); }, timeoutMs); // safety cap
    });
  }
  ```
  This removes the busy-wait without changing observable behavior (same 3s cap, same force-to-disconnected fallback).
- **Hook decomposition (discretion shape) — recommend** splitting `useVpnConnection` along its ~10 effects into cohesive hooks: `useVpnLifecycle` (connect/disconnect orchestration), `useProtocolFallback` (the `tryNextProtocol` chain, fixing APP-H-03 to route through proper disconnect cleanup), `useConnectionStats` (timers/byte counters), `useReconnect` (auto-reconnect/backoff). Keep `useVpnConnection` as a thin composition that calls the sub-hooks so call sites are unchanged. This is shape-discretion; the constraint is behavior-preservation + the busy-wait removal.
- **Validation:** this is the one item with **no automated test harness** in the RN app (jest is configured but these are native-bridge integration flows). Validation is **manual smoke test** (connect/disconnect/reconnect/protocol-fallback on device) + a unit test for `waitForDisconnected` resolving on state change. Flag as manual-heavy.

Confidence: HIGH on the busy-wait fix, MEDIUM on decomposition (shape is judgment; keep behavior frozen).

---

## §6 — HARD-16: Mobile secure storage (D-11/D-12)

### 6.1 Current state
`authStore.ts` **[VERIFIED]** stores the token pair via `AsyncStorage.setItem(TOKENS_KEY, JSON.stringify(tokens))` at **7 sites** (`TOKENS_KEY = 'auth-tokens'`): `initialize` (read+write), `linkWithCode`, `signInWithApple`, `signInWithGoogle`, `updateTokens`, `logout` (remove). App is **bare RN 0.84.1, New Architecture** (deps: `react-native-nitro-modules`, `reanimated@4`, `worklets` — all New-Arch/JSI). Already ships `react-native-mmkv@^4.3.0` (but D-11: MMKV-with-encryption does NOT satisfy the Xcode-Keychain success criterion).

### 6.2 Library recommendation (D-11 discretion): react-native-keychain
**Recommend `react-native-keychain`** **[VERIFIED: npm — latest 10.0.0, published 2025-03-23]**. It writes to **iOS Keychain** (`SecItem` API) and **Android EncryptedSharedPreferences / Keystore-backed** storage — exactly what D-11's success criterion checks (Xcode → Keychain Access shows the entry; the AsyncStorage plist contains no token). MMKV cannot satisfy this because it stores in an app-sandbox file, not the OS Keychain.

**RN 0.84 / New Architecture compatibility — caveat (MEDIUM confidence):**
- RN 0.84 is the **last release with the Bridge interop layer**; 0.85 (April 2026) removed it **[CITED: ninetwothree.co, reactwg discussions]**. So on 0.84, react-native-keychain works via interop even if it hasn't fully migrated to TurboModules.
- react-native-keychain's New-Arch support has been tracked in oblador/react-native-keychain#706 **[CITED: GitHub]**; v10.x (2025) is recent enough to expect New-Arch support, but **the plan MUST verify** at install time: build the iOS + Android apps with New Arch enabled and confirm no interop warnings. If a New-Arch issue surfaces, the fallback is `expo-secure-store` (works in bare RN via `expo-modules-core`) or `react-native-sensitive-info`. Recommend react-native-keychain first; keep `expo-secure-store` as the documented fallback.
- **Autolinking:** bare RN 0.84 autolinks native modules from package.json — `pod install` (iOS) + Gradle sync (Android) after `npm install react-native-keychain`. No manual linking. iOS needs no extra entitlement for default (generic password) Keychain access.

### 6.3 Implementation
Introduce a thin `secureTokenStore.ts` wrapping keychain (`setGenericPassword` / `getGenericPassword` / `resetGenericPassword`) with the same get/set/remove shape AsyncStorage exposes, and swap the 7 call sites in `authStore.ts`. Tokens are stored as the JSON pair under one keychain service key. The axios refresh interceptor (`services/api.ts`) reads tokens from zustand state (not storage directly per the comment in `signInWithApple`), so the storage swap is contained to `authStore.ts`.

**Also add `device_id` to the refresh request** (HARD-04 client side): the refresh call must now send `device_id` so the backend can hard-check it. The fingerprint is already available via `getDeviceFingerprint()` (used in `initialize`/`signInWith*`). Thread `device_id` into the refresh interceptor's `/auth/refresh` body.

### 6.4 Migration path (D-12 discretion) — recommend FORCE RE-LOGIN, coordinated with D-09
Two options:
- **migrate-then-wipe:** read existing AsyncStorage token, write to keychain, delete from AsyncStorage. Problem: **D-09 invalidates that token server-side** (the sessions table is cleared at backend cutover). Migrating it carries forward a **dead token** — the next API call 401s and the refresh interceptor kicks in. So migrate-then-wipe gains nothing.
- **force re-login (recommend):** on first launch of the new build, ignore/clear any AsyncStorage token and route to the login/guest flow. Because D-09 already forces a server-side re-auth, this is the *same* re-login the user faces anyway — **D-12 + D-09 collapse into ONE re-login** if the mobile build ships at/after the backend cutover.

**The coordination (CRITICAL — see §Open Risks):** ship the HARD-03/04 backend cutover and the HARD-16 mobile build as one wave. On launch the new app finds no valid session (cleared server-side), the user re-authenticates once (Apple/Google/guest), and the *new* tokens are written **straight to keychain** (never to AsyncStorage). The wipe is implicit (the new app never writes to AsyncStorage; add a one-time `AsyncStorage.removeItem('auth-tokens')` on boot to satisfy D-12's "AsyncStorage ends with no auth tokens" literal lock for users upgrading in place).

**Validation (SC#5):** on a device, sign in with the new build, open Xcode → Devices → app container OR Keychain, confirm the token entry exists in Keychain and that the AsyncStorage backing (`RCTAsyncLocalStorage` manifest/plist) contains **no** `auth-tokens` value.

Confidence: HIGH on approach; MEDIUM on react-native-keychain New-Arch (verify at install).

---

## Validation Architecture

**nyquist_validation = true** (`.planning/config.json`) — this section is required.

### Test Framework
| Surface | Framework | Quick command | Full command |
|---------|-----------|---------------|--------------|
| Go API | Go test (stdlib) + testcontainers-postgres + miniredis | `go test ./internal/handler/... -run <T> -x` (from `server/api`) | `go test ./...` (from `server/api`) |
| Go tunnel | Go test | `go test ./...` (from `server/tunnel`) | same |
| Mobile RN | jest 29 (`@react-native/babel-preset`) | `npm test -- <file>` (from `app`) | `npm test` |
| CI | GitHub Actions | n/a (validated by triggering a PR) | n/a |

### Requirement → Test Map (per success criterion)
| SC / Req | Behavior | Test type | Command / method | Exists? |
|----------|----------|-----------|------------------|---------|
| SC#1 / HARD-01 | grep-stripe == 0 in `.go`; `stripe_id` absent | shell + integration | `grep -rn stripe server/ --include=*.go` (expect 0); `migrations_test.go:203-207` already asserts column absence | ✅ assert exists; ❌ Wave 0 grep gate |
| SC#2 / HARD-02 | two same-plan users differ; rotate on plan change; **wire rejects revoked UUID** | unit (API) + **integration (wire)** | API: `go test` asserting `/servers/:id/config` UUIDs differ + rotate. Wire: docker-compose harness — real VLESS handshake with revoked UUID must FAIL | ❌ Wave 0 (both) — wire test is the heaviest new harness |
| SC#3 / HARD-09 | vuln-introducing PR is unmergeable | CI check | open a PR adding a known-vuln dep; assert `golang/govulncheck-action` job fails red | ❌ Wave 0 (new workflow); merge-block needs GitHub branch-protection (manual) |
| SC#4 / HARD-04 | device-B refresh rejected (401) | unit (API) | `go test` — issue session with device A, refresh with device B → 401; refresh with device A → 200 | ❌ Wave 0 |
| SC#5 / HARD-16 | token in Keychain, absent from AsyncStorage plist | **manual (Xcode)** | sign in on device; Keychain Access shows entry; AsyncStorage manifest has no `auth-tokens` | ❌ manual-only — document procedure |
| SC#6 / HARD-10 | `zap.String("token", jwt)` → `[REDACTED]` | unit (Go) | `zaptest/observer` core; log a JWT-shaped + base64url-32 string; assert output `[REDACTED]` | ❌ Wave 0 |
| HARD-03 | refresh token is opaque (no `.` segments) | unit | assert `generateTokens` refresh field matches `^[A-Za-z0-9_-]{43}$`, not 3 JWT segments | ❌ Wave 0 |
| HARD-05 | group chat → no reply | unit | feed `Update{Chat.Type:"group"}` → assert bot sends nothing | ❌ Wave 0 |
| HARD-06 | `len(search)<3` → 400; query uses prefix | unit | assert short search rejected; assert generated SQL has no leading `%` | ❌ Wave 0 |
| HARD-07 | audit details carry before→after | unit | change role; assert audit row `details.role = {before,after}` | ❌ Wave 0 |
| HARD-08 | admin responses carry HSTS/nosniff/CSP | unit | `go test` httptest on an admin route; assert headers present | ❌ Wave 0 |
| HARD-11 | new hashes are cost 12 | unit | `bcrypt.Cost(hash) == 12` | ❌ Wave 0 |
| HARD-12 | redis-down → 503 on link | unit | stop miniredis; assert `LinkAttemptLimit` returns 503 | ❌ Wave 0 |
| HARD-13 | 6th call/min/IP → 429 | unit | 6 rapid calls one IP | ❌ Wave 0 |
| HARD-14 | per-user stable permutation | unit | same user same order; two users differ; set equal | ❌ Wave 0 |
| HARD-15 | `waitForDisconnected` resolves on state change (no busy-wait) | unit (jest) | subscribe-resolves test; manual device smoke for full flow | ❌ Wave 0 + manual |
| HARD-17 | /health has no `go_version` | unit | httptest GET /health; assert key absent | ❌ Wave 0 |

### Sampling Rate
- **Per task commit:** `go test ./internal/<touched>/... -x` (API), `npm test -- <touched>` (mobile).
- **Per wave merge:** `go test ./...` in both `server/api` and `server/tunnel`; `npm test` in `app`.
- **Phase gate:** full suites green + the grep-stripe gate + the wire-level VLESS integration check + manual SC#5 (Xcode) before `/gsd-verify-work`.

### Wave 0 Gaps
- [ ] `server/tunnel/.../server_reload_test.go` — `ReloadClients` rebuilds config + restarts instance (HARD-02 tunnel side).
- [ ] Wire-level VLESS harness (docker-compose or scripted xray client) — revoked-UUID rejection (HARD-02 SC#2). **Heaviest new infra.**
- [ ] `server/api/.../servers_vless_test.go` — per-user UUID allocation/rotation + active-set endpoint.
- [ ] `server/api/.../auth_refresh_device_test.go` — device-bind 401 (HARD-04).
- [ ] `server/api/.../logger_redact_test.go` — zap redaction (HARD-10).
- [ ] `.github/workflows/ci.yml` — govulncheck PR job (HARD-09) + a deliberate-vuln PR to prove it blocks.
- [ ] `app/src/stores/vpnStore.test.ts` — `waitForDisconnected` (HARD-15).
- [ ] Manual procedure doc for SC#5 (Xcode Keychain check).
- [ ] grep-stripe shell assertion in CI or a `go:generate`/test (SC#1 literal gate).

---

## Security Domain

`security_enforcement` not set in config → enabled. This entire phase IS security hardening; the per-item mitigations are above. ASVS mapping:

| ASVS Category | Applies | Standard control (this phase) |
|---------------|---------|-------------------------------|
| V2 Authentication | yes | HARD-03/04 opaque device-bound refresh; HARD-11 bcrypt 12 |
| V3 Session Management | yes | HARD-03/04 session binding + clean-break invalidation |
| V4 Access Control | yes | HARD-02 per-user VLESS wire enforcement; HARD-06 admin search |
| V5 Input Validation | yes | HARD-06 (search length/shape), HARD-13 (rate-limit) |
| V6 Cryptography | yes | `crypto/rand`+base64url (reuse start_token); `crypto/hmac` (HARD-14); bcrypt (HARD-11) — never hand-roll |
| V7 Error/Logging | yes | HARD-10 redaction; HARD-13 log-spend; HARD-17 info leak |
| V8 Data Protection | yes | HARD-16 Keychain at-rest token storage |
| V14 Config | yes | HARD-08 security headers; HARD-09 dependency scanning |

| Threat (STRIDE) | Closed by |
|-----------------|-----------|
| Spoofing — stolen refresh reused on new device | HARD-04 device-bind hard-reject |
| Tampering — JWT_SECRET-holder mints refresh | HARD-03 opaque tokens (no signature to forge) |
| Info disclosure — tokens in logs / Go version / shared UUID enumeration | HARD-10, HARD-17, HARD-02, HARD-14 |
| Elevation — guest gets Pro tunnel access via shared UUID | HARD-02 wire enforcement |
| DoS — Redis-outage brute force / log-spend | HARD-12 fail-closed, HARD-13 bucket |

---

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | react-native-keychain 10.x has working RN 0.84 New-Arch support (via interop at minimum) | §6.2 | Mobile build breaks; fall back to expo-secure-store (documented) |
| A2 | `golang/govulncheck-action@v1` exits non-zero on findings by default (true blocking) | §4.5 | If not, set the failing flag explicitly; verify in the action's current README at install |
| A3 | Fiber v2.52.5 bundles `middleware/helmet` with HSTS/CSP/nosniff support | §4.4 | If absent, hand-roll a tiny headers middleware (trivial) |
| A4 | xray-core `core.Instance` reload = Close+New+Start drops live connections (no graceful handoff) | §2.2 | If a newer xray-core adds graceful reload, propagation can tighten — verify the pinned xray-core version's capabilities |
| A5 | base64url{32,} redaction regex false-positives on some long non-secret IDs are acceptable | §4.6 | Over-redaction in logs; tune regex if it hides needed diagnostic IDs |
| A6 | The `full_name`/`email_hash` columns have/can-get indexes usable by prefix match | §4.2 | Prefix match still scans; add the index in the same migration |

---

## Open Risks / Coordination

1. **D-09 ↔ D-12 single re-login (CRITICAL).** The backend opaque-refresh cutover (HARD-03/04) clears `sessions` server-side, AND the mobile Keychain move (HARD-16) wipes AsyncStorage tokens. If shipped on different days the user re-logs in **twice** (once when the server kills their session, once when the new app forces login). **Mitigation:** ship the backend cutover and the mobile build as one coordinated release; the new app force-routes to login on first launch and writes the fresh tokens straight to Keychain. Sequence: deploy backend (sessions cleared) → release app build at the same time. Document as a release-runbook step, not just code.

2. **HARD-02 propagation latency & connection drops.** Because xray-core reload drops live connections (§2.2), an aggressive per-event reload would repeatedly kick every connected user on a tunnel. **Mitigation (designed in §2.3):** pull-based active-set on the heartbeat channel + hard debounce (5–10s) + accept a 30–60s propagation floor. SC#2 says "seconds" — clarify with the planner that **30–60s is the honest floor** for a regen+reload mechanism (D-05's explicit choice); "seconds" is achievable for the *API response* (new UUID returned immediately) but the *wire enforcement* lags one reload cycle. This gap is inherent to D-05 and should be stated, not hidden.

3. **HARD-02 schema/migration numbering.** Both HARD-04 (sessions columns) and HARD-02 (user_vless_identities) need migrations. Next free number is **025**. Assign 025 = sessions device/IP, 026 = vless identities (or combine; recommend separate for independent rollback). Confirm no other in-flight phase claimed 025.

4. **HARD-09 merge-blocking is half-in-repo.** The workflow YAML is in-repo, but making the check **required** (so a red check actually blocks merge) is a GitHub branch-protection setting (repo Settings → Branches → require status check). The plan must include this as an explicit manual GitHub step or it silently ships as advisory-only — failing SC#3's "unmergeable" wording.

5. **HARD-02 wire test infra.** SC#2's wire-rejection check needs a running xray tunnel + a VLESS client that can present an arbitrary UUID. No such harness exists. This is the single biggest validation build-out — likely a docker-compose integration test or a scripted check run manually at the phase gate. Budget for it.

6. **CSP scope mismatch (HARD-08).** The admin *SPA* is served from its own host (deploy-admin.yml), so CSP on the *API*'s admin routes guards JSON responses, not the SPA HTML. The SPA-side CSP (which is what actually prevents admin-panel XSS, S10-3) lives in admin-web hosting config — **out of scope** for Phase 8 (API/tunnel/mobile/CI). Don't let the plan assume HARD-08 hardens the SPA; it hardens the API admin responses. Note for a future admin-web pass.

7. **S1-6 `/auth/logout` already exists.** CONTEXT lists logout as a deferred/unmapped audit item, but `auth.go:1112+` and `DeleteUserSessions` (`session_repo.go`) show logout shipped in an earlier phase. No action; just don't re-implement.

---

## Sources

### Primary (HIGH confidence)
- Codebase (read directly, 2026-06-02): `server/api/internal/handler/{auth.go,servers.go,health.go,admin.go}`, `server/api/internal/middleware/{ratelimit.go,audit.go}`, `server/api/internal/repository/{session_repo.go,admin_repo.go}`, `server/api/internal/model/user.go`, `server/api/internal/recovery/start_token.go`, `server/api/internal/bot/recovery.go`, `server/api/cmd/{main.go,createadmin/main.go}`, `server/api/go.mod`, `server/api/migrations/{001_initial.sql,020_lava_payments.sql}` + `migrations_test.go`, `server/tunnel/internal/{server.go,config.go,heartbeat.go}`, `server/tunnel/cmd/tunnel/main.go`, `app/src/stores/{authStore.ts,vpnStore.ts}`, `app/src/hooks/useVpnConnection.ts`, `app/package.json`, `.github/workflows/*.yml`, `.planning/config.json`.
- ADR-007 §8.3/§10.8/§12.6/§19; SECURITY-AUDIT.md (S1-2,S1-7,S1-8,S2-3,S2-4,S2-5,S4-2,S4-4,S4-5,S5-1,S5-2,S7-1,S7-2,S9-2,S9-4,S11-2); CODE-REVIEW.md (APP-H-03,APP-H-04,APP-M-04,APP-L-03); REQUIREMENTS.md (HARD-01..17).
- npm registry: `react-native-keychain` latest **10.0.0** (2025-03-23) [VERIFIED via `npm view`].

### Secondary (MEDIUM — verified against official source)
- XTLS/Xray-core Discussions #1060, #2596, #4456; Issues #1981, #5332; Marzban #1981 — no in-process hot reload; restart drops connections; HandlerService exists but rejected by D-05. <https://github.com/XTLS/Xray-core/discussions/1060>
- `golang/govulncheck-action` README — official action, no silencing, exits non-zero on findings. <https://github.com/golang/govulncheck-action>
- RN 0.84/0.85 bridge removal + New Architecture status. <https://www.ninetwothree.co/blog/react-native-0-85-bridge-removal>, <https://github.com/oblador/react-native-keychain/issues/706>

### Tertiary (LOW — verify at implementation)
- react-native-keychain exact New-Arch/bridgeless status on 0.84 — confirm at install build (A1).

## Metadata
- Standard stack: HIGH (all deps read from go.mod/package.json + npm-verified).
- Architecture: HIGH for §3/§4/§5; MEDIUM for §2 reload-API surface (depends on TunnelServer refactor) and §6 keychain New-Arch.
- Pitfalls: HIGH (xray reload + D-09/D-12 coupling + govulncheck merge-block are the load-bearing risks, all surfaced).
- Research date: 2026-06-02. Valid until: ~2026-07-02 (stable; re-check react-native-keychain New-Arch and govulncheck-action defaults at implementation).
