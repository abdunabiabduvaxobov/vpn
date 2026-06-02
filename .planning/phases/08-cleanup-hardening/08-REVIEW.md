---
phase: 08-cleanup-hardening
reviewed: 2026-06-02T00:00:00Z
depth: standard
files_reviewed: 38
files_reviewed_list:
  - app/src/hooks/useConnectionStats.ts
  - app/src/hooks/useProtocolFallback.ts
  - app/src/hooks/useVpnConnection.ts
  - app/src/hooks/useVpnLifecycle.ts
  - app/src/hooks/vpnConnectionShared.ts
  - app/src/services/api.ts
  - app/src/services/secureTokenStore.ts
  - app/src/stores/authStore.ts
  - app/src/stores/vpnStore.ts
  - server/api/cmd/createadmin/main.go
  - server/api/cmd/main.go
  - server/api/internal/bot/recovery.go
  - server/api/internal/config/config.go
  - server/api/internal/handler/admin.go
  - server/api/internal/handler/auth.go
  - server/api/internal/handler/devices.go
  - server/api/internal/handler/health.go
  - server/api/internal/handler/payment.go
  - server/api/internal/handler/servers.go
  - server/api/internal/handler/webhook_lava.go
  - server/api/internal/logger/logger.go
  - server/api/internal/middleware/audit.go
  - server/api/internal/middleware/ratelimit.go
  - server/api/internal/middleware/security_headers.go
  - server/api/internal/middleware/version.go
  - server/api/internal/model/subscription.go
  - server/api/internal/model/user.go
  - server/api/internal/model/vless_identity.go
  - server/api/internal/repository/admin_repo.go
  - server/api/internal/repository/subscription_repo.go
  - server/api/internal/repository/user_repo.go
  - server/api/internal/repository/vless_repo.go
  - server/api/migrations/025_session_device_binding.sql
  - server/api/migrations/026_user_vless_identities.sql
  - server/api/migrations/027_admin_search_index.sql
  - server/tunnel/cmd/tunnel/main.go
  - server/tunnel/internal/heartbeat.go
  - server/tunnel/internal/server.go
findings:
  critical: 0
  warning: 5
  info: 9
  total: 14
status: issues_found
---

# Phase 8: Code Review Report

**Reviewed:** 2026-06-02
**Depth:** standard
**Files Reviewed:** 38
**Status:** issues_found

## Summary

Phase 8 is a security-critical hardening pass for a consumer VPN about to take real money. The audit-driven controls are, on the whole, well-implemented and carefully reasoned: opaque `crypto/rand` refresh tokens (auth.go:693), device/IP session binding with a hard device check and a soft IP check (auth.go:268-289), per-user VLESS UUID allocation/rotation under `WithUserLock` (vless_repo.go + admin.go:259 + webhook_lava.go:263), constant-time webhook `X-Api-Key` compare plus IP-allowlist plus `OnConflict DoNothing` idempotency returning 500 for retries (webhook_lava.go), fail-closed link rate limiting vs deliberately fail-open debug/log limiting (ratelimit.go), bcrypt cost 12 (config.go:17), the Telegram private-chat gate (recovery.go:165), Keychain token storage with a one-time AsyncStorage purge (secureTokenStore.ts + authStore.ts:91), and zap log redaction by key and by token shape (logger.go).

I found **no Critical issues**. The Stripe/legacy-provider removal is clean (single lava provider, `OptionalEnvWarnings` notes the residue intentionally). SQL is parameterized throughout; the admin search is anchored-prefix only with a supporting index; the advisory lock derivation is injection-safe.

The 5 Warnings are correctness/robustness gaps rather than exploitable holes: a session-rotation hard-check bypass window on empty `device_id`, a wire-enforcement reload that can leave the tunnel admitting NO users on a transient empty active-set, a `ParseUnverified`-before-verify ordering nit in the rate limiter, a `GetOrCreateActiveVlessUUID` insert race that can briefly create two active identities per user, and a `CancelSubscription` local-write that runs outside `WithUserLock` (unlike every webhook path). Info items are smaller hardening/observability notes.

## Warnings

### WR-01: Session rotation preserves an empty `device_id`, permanently disabling the HARD-04 hard check for that session lineage

**File:** `server/api/internal/handler/auth.go:268`, `auth.go:331`
**Issue:** The refresh hard check is skipped whenever the bound `device_id` is empty (`if session.DeviceID != "" && session.DeviceID != req.DeviceID`). On rotation the new row deliberately carries the SAME `device_id` forward (auth.go:331). This is correct for admin sessions (never device-bound by design). But any guest/SSO/link session that was issued with an empty `device_id` — legacy mobile clients that omit it, web SSO sign-in (`req.DeviceID` empty per the AppleSignIn/GoogleSignIn comments), and `/auth/link` when the body omits `device_id` — produces a long-lived refresh-token lineage (30-day TTL) on which a stolen token can be replayed from any device forever, because the empty binding is preserved across every rotation. The audit control (S1-7 / T-08-04) is silently void for that population.
**Fix:** This is partly by-design for the web/admin surface, but the mobile surface should not be able to mint an empty-`device_id` session. Consider requiring a non-empty `device_id` for the mobile guest/SSO paths (the RN client always sends one via `getDeviceFingerprint()`), and only allowing the empty-binding skip for the explicitly browser-originated flows (admin login, web SSO). At minimum, log a security signal when a mobile-shaped request (has `X-App-Version`) establishes a session with empty `device_id`, so the gap is observable:
```go
if req.DeviceID == "" && c.Get("X-App-Version") != "" {
    logger.Warn("mobile session issued without device binding — refresh hard check will be skipped",
        zap.String("user_id", user.ID))
}
```

### WR-02: `RotateVlessUUID` reload can transiently admit ZERO clients, dropping every live VPN connection on a server-wide empty active-set read

**File:** `server/tunnel/internal/server.go:210`, `server/tunnel/internal/heartbeat.go:136`
**Issue:** `StartClientSync` calls `server.ReloadClients(resp.UUIDs)` with whatever the API returned. If `ListActiveVlessUUIDs` ever returns an empty slice (e.g. a migration window, a bad mass-revoke, or a transient query that the handler turns into `uuids = []string{}` at servers.go:522), the tunnel rebuilds its xray config with zero clients and Closes the old instance — dropping ALL live connections and admitting nobody until the next non-empty pull. The ETag for an empty set is stable and distinct, so the debounce does not protect against it; `ReloadClients` has no "refuse to apply an empty set" guard. For a VPN this is a full-server outage triggered by one bad read.
**Fix:** Treat an empty active-set as suspicious and refuse to reload to zero unless explicitly intended:
```go
if len(resp.UUIDs) == 0 {
    logger.Warn("vless-sync: refusing to reload to an EMPTY active set — keeping previous client set",
        zap.String("etag", etag))
    continue // do NOT advance lastETag; retry next tick
}
```
If a deliberate "admit nobody" state is ever needed, gate it behind an explicit signal rather than an empty list.

### WR-03: `GetOrCreateActiveVlessUUID` has a check-then-insert race that can create two active identities for one user

**File:** `server/api/internal/repository/vless_repo.go:24`
**Issue:** The read path runs `First(... is_active=true)` then, on `ErrRecordNotFound`, `Create()`s a new active row — outside any transaction or lock (the doc comment explicitly notes it runs on the shared `*gorm.DB`). Two concurrent `GET /servers/:id/config` requests for the same brand-new user (the mobile client commonly fires config fetches in parallel across servers, and the protocol-fallback hook re-fetches) both miss and both insert, yielding two `is_active=true` rows. The schema's UNIQUE is on `vless_uuid` (always distinct), and `idx_uvi_user_active` is a non-unique partial index, so nothing prevents this. The model doc asserts "at most one ACTIVE identity per user" — that invariant is not actually enforced. Downstream effects: the tunnel admits both UUIDs (harmless-ish), but `RotateVlessUUID` retiring "every active" then issuing one new row partially self-heals only on the next plan change, and the user's config could flip between the two UUIDs across requests (`Order created_at DESC` picks the newest, but a same-millisecond tie is unordered).
**Fix:** Make the active-per-user invariant enforceable and the allocation atomic. Either add a partial UNIQUE index (`CREATE UNIQUE INDEX ... ON user_vless_identities(user_id) WHERE is_active = TRUE`) and handle the duplicate by re-reading, or wrap the get-or-create in `WithUserLock(ctx, db, userID, ...)` so concurrent first-fetches serialize:
```go
// On Create() duplicate-active, re-read the winner instead of inserting a second active row.
```
A partial unique index is the durable fix; the lazy read path can then re-`First` on conflict.

### WR-04: `CancelSubscription` mutates the contract OUTSIDE `WithUserLock`, unlike every webhook path it races with

**File:** `server/api/internal/handler/payment.go:245`
**Issue:** Every server-side contract/subscription mutation in the lava webhook (`handleLavaPaymentSuccess`, `handleLavaRecurringSuccess`, `handleLavaRecurringFailed`, `handleLavaSubscriptionCancelled`) and the admin force-cancel take `repository.WithUserLock(ctx, db, userID, ...)` to prevent hybrid states. The user-initiated `CancelSubscription` handler does its local `lava_contracts` update (payment.go:245-251) with a plain `db.Model(...).Updates(...)` — no advisory lock. A user tapping "Cancel" while a `recurring.payment.success` webhook for the same contract is in flight can interleave: the webhook sets `is_active=true` + extends `expires_at` while the cancel sets `is_active=false` + `cancelled_at`, and the final state depends on commit ordering — exactly the hybrid state `WithUserLock` exists to prevent.
**Fix:** Wrap the local contract write in the same per-user lock the webhook uses:
```go
if err := repository.WithUserLock(c.Context(), db, userID, func(tx *gorm.DB) error {
    return tx.Model(&model.LavaContract{}).Where("id = ?", contract.ID).Updates(map[string]interface{}{
        "is_active": false, "cancelled_at": &now,
    }).Error
}); err != nil { ... }
```

### WR-05: Rate-limit user-key selection does `ParseUnverified` before signature verification and ignores token expiry

**File:** `server/api/internal/middleware/ratelimit.go:166`
**Issue:** `extractUserIDFromToken` first `ParseUnverified`s to grab claims, then verifies the signature with `jwt.WithoutClaimsValidation()` and returns the `sub` only if `verifiedToken.Valid`. The signature check is correct (it does reject forged tokens), but: (1) `WithoutClaimsValidation()` means an EXPIRED but validly-signed token still selects the authenticated 200/min bucket — a user with a long-dead access token keeps the higher limit for rate-limiting purposes, which is a minor abuse-budget inflation, not an auth bypass (AuthRequired still rejects the request). (2) Reading `claims` from the unverified parse and then trusting them after a separate verify is a fragile pattern — the two parses could diverge if the library internals change. Functionally safe today because a forged token fails `verifiedToken.Valid`, but the expiry-ignored bucket selection is a real (small) gap.
**Fix:** Read the `sub` from the VERIFIED token's claims, and drop `WithoutClaimsValidation()` so expired tokens fall back to the IP bucket:
```go
verified, err := jwt.Parse(tokenStr, keyFunc) // no WithoutClaimsValidation
if err != nil || !verified.Valid { return "" }
claims, _ := verified.Claims.(jwt.MapClaims)
sub, _ := claims["sub"].(string)
return sub
```

## Info

### IN-01: `ListActiveVlessUUIDs` is an unbounded full-table scan with no limit and no server scoping

**File:** `server/api/internal/repository/vless_repo.go:114`, `server/api/internal/handler/servers.go:506`
**Issue:** The tunnel pulls the ENTIRE fleet's active UUID set every heartbeat tick. `serverID` is accepted but unused (documented as forward-compat). As the user base grows this response grows linearly and is rebuilt into xray config on every membership change. This is an intentional architectural choice (shared-UUID model, partial index on `is_active`), and out of the v1 performance scope, but it is worth a roadmap note: the xray `clients` array and the per-tick JSON payload both scale with total active users, and each reload is connection-dropping.
**Fix:** Roadmap item — add `plan_servers`-scoped filtering keyed on `serverID` so each tunnel admits only the UUIDs entitled to it, bounding both payload size and reload cost.

### IN-02: `GetServerConfig` uses an unchecked `c.Locals("user_id").(string)` assertion

**File:** `server/api/internal/handler/servers.go:271`, `handler/payment.go:62`, `handler/payment.go:203`, `handler/payment.go:280`, `handler/devices.go:77`
**Issue:** Several protected handlers do `userID := c.Locals("user_id").(string)` (single-return assertion) which panics if the local is ever unset. These are all mounted under the `protected` group where `AuthRequired` sets it, so it is safe today, and `recover.New()` would catch a panic. But `ListServersCached` (servers.go:129) and `Logout` (auth.go:1198) use the safe two-value form for the same value — the codebase is inconsistent, and a future re-mount of one of these handlers under a non-auth group would turn a misconfiguration into a panic instead of a clean 401.
**Fix:** Use the two-value form everywhere and 401 on empty, matching `Logout`/`ListServersCached`:
```go
userID, _ := c.Locals("user_id").(string)
if userID == "" { return c.Status(fiber.StatusUnauthorized).JSON(...) }
```

### IN-03: `GuestLogin` known-device fast path is not transactional — TouchDevice/token/session run as separate writes

**File:** `server/api/internal/handler/auth.go:440-475`
**Issue:** On the known-device path, `TouchDevice`, `FindUserByID`, `generateTokens`, and `storeRefreshSession` run as independent statements on the shared `db`. The fresh-user path also creates user → subscription → device → session as separate writes with a manual rollback for the subscription step only (auth.go:525-539). The session-store failure is handled (returns 500), but the device-bind failure in the fresh-user path is non-fatal-and-logged (auth.go:565), so a user can be created with a session but no device row, which then re-mints on next call. Not a correctness bug given the documented reasoning, but the multi-write guest flow is the kind of place a single `db.Transaction` would remove a class of partial-state edge cases.
**Fix:** Consider wrapping the fresh-user create chain (user + subscription + device) in one transaction; the manual user-rollback at auth.go:530 is a hint that a transaction is the cleaner primitive here.

### IN-04: `orderServersForUser` reuses `JWTSecret` as the HMAC key for server-ordering

**File:** `server/api/internal/handler/servers.go:195`, `servers.go:255`
**Issue:** The per-user server permutation keys its HMAC on `cfg.JWTSecret`. Reusing the JWT signing secret as a second-purpose HMAC key is cryptographic key reuse: it does not leak the secret (HMAC is one-way and only 8 bytes of output are compared), but it couples two unrelated security functions to one secret and means a future change to either (rotation cadence, exposure surface) affects both. Low risk, flagged for hygiene.
**Fix:** Derive a dedicated key (e.g. `HKDF(JWTSecret, "server-order")` or a separate `SERVER_ORDER_HMAC_KEY` env) so the JWT secret has a single responsibility.

### IN-05: `applyLavaEventImpl` re-rotates the VLESS UUID on webhook REPLAY, churning the active identity

**File:** `server/api/internal/handler/webhook_lava.go:276`
**Issue:** The code comment acknowledges that an admin replay funnels through the same `payment.success` path and therefore calls `RotateVlessUUID` again, "issuing another active UUID." Because `RotateVlessUUID` retires ALL active rows and inserts one new one, a replay of a long-past payment will revoke the user's CURRENT working UUID and issue a fresh one — dropping that user's live connection at the next tunnel reload, for an event that granted nothing new (the tier grant is idempotent, but the rotation is not idempotent in its wire effect). The set converges, but the user eats a connection drop from a replay that was supposed to be a no-op side-effect-wise.
**Fix:** Make rotation-on-replay conditional: only rotate when the grant actually changed the tier/contract state (compare before/after inside the lock), or skip rotation entirely on the replay path (the active UUID is already valid). Document the chosen behavior in the replay handler.

### IN-06: Migration 025 `DELETE FROM sessions` is unconditional and re-runs on every apply

**File:** `server/api/migrations/025_session_device_binding.sql:43`
**Issue:** The clean-break `DELETE FROM sessions` is intentionally unconditional, but the file header notes the runner applies migrations idempotently via `IF NOT EXISTS` DDL — the DELETE has no such guard, so any re-run of this migration file (a re-applied migration loop, a manual psql replay) silently logs out every active user again. The comment calls this "harmless," and pre-launch it is, but post-launch a re-run is a fleet-wide forced re-login. Acceptable for this phase given no paying users, flagged so it is not forgotten.
**Fix:** Gate the DELETE behind a one-shot marker (e.g. a `schema_migrations` row check) before launch so a re-applied migration cannot mass-invalidate live sessions.

### IN-07: Webhook `payment.failed`/`subscription.cancelled` "not found" branches return nil (ack) — correct, but mask resolution-chain bugs

**File:** `server/api/internal/handler/webhook_lava.go:424`, `webhook_lava.go:510`
**Issue:** When the invoice/contract lookup returns `ErrNotFound`, these handlers log a Warn and return nil so lava stops retrying. This is the right idempotency posture (don't 500-loop on an event for a deleted row), but it also means a genuine resolution-chain bug (e.g. `ContractID` vs `parentContractId` mismatch from a lava API change) is indistinguishable from a benign deleted-invoice, surfacing only as a steady trickle of Warn lines. Given the webhook is THE Pro-grant path, a silent mis-resolution on the success side is caught (it 500s and retries), but the failed/cancelled side fails silent.
**Fix:** Emit these specific not-found acks at a level/metric an operator alerts on (or increment a counter), so a non-zero rate of "no matching invoice/contract" is visible rather than buried in Warn.

### IN-08: `api.ts` hardcodes the dev API base URL (private LAN IP) in shipped source

**File:** `app/src/services/api.ts:17-19`
**Issue:** `API_BASE_URL` switches on `__DEV__`; the dev branch hardcodes `http://192.168.10.175:3000` (plaintext HTTP, a developer's LAN IP). `__DEV__` is false in release builds so the production HTTPS URL is used, but the LAN IP and the cleartext dev endpoint ship in the bundle as dead strings. Minor info leak (internal network addressing) and a cleartext-HTTP default that only `__DEV__` gates.
**Fix:** Source both URLs from env/`.env`-style config (react-native-config) rather than literals, so the dev address is not baked into the shipped JS bundle.

### IN-09: `secureTokenStore.setTokens` ignores the `react-native-keychain` accessible/biometry options

**File:** `app/src/services/secureTokenStore.ts:38`
**Issue:** `Keychain.setGenericPassword(...)` is called with only `{service}` — no `accessible` option. The default iOS accessibility is `AccessibleWhenUnlocked`, which is reasonable, but it is implicit; without an explicit `ACCESSIBLE` setting the at-rest protection class is unstated and could change with a library default bump. Since SC#5 is specifically about tokens at rest in OS secure storage, the protection class should be pinned.
**Fix:** Pass an explicit accessibility class, e.g. `{service: SERVICE, accessible: Keychain.ACCESSIBLE.WHEN_UNLOCKED_THIS_DEVICE_ONLY}`, so the at-rest class is deliberate and non-exportable to other devices via backup.

---

_Reviewed: 2026-06-02_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
