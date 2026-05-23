---
phase: 03-lava-top-plans-catalog
reviewed: 2026-05-24T00:00:00Z
depth: standard
files_reviewed: 92
files_reviewed_list:
  - server/api/.env.example
  - server/api/cmd/main.go
  - server/api/cmd/createadmin/main_test.go
  - server/api/go.mod
  - server/api/go.sum
  - server/api/integration/lava_sandbox_test.go
  - server/api/internal/cache/plans_cache.go
  - server/api/internal/cache/plans_cache_test.go
  - server/api/internal/config/config.go
  - server/api/internal/config/config_test.go
  - server/api/internal/handler/admin.go
  - server/api/internal/handler/admin_lava.go
  - server/api/internal/handler/admin_lava_test.go
  - server/api/internal/handler/auth.go
  - server/api/internal/handler/auth_test.go
  - server/api/internal/handler/connection.go
  - server/api/internal/handler/connection_test.go
  - server/api/internal/handler/devices.go
  - server/api/internal/handler/devices_test.go
  - server/api/internal/handler/health.go
  - server/api/internal/handler/payment.go
  - server/api/internal/handler/payment_test.go
  - server/api/internal/handler/plans_admin.go
  - server/api/internal/handler/plans_admin_test.go
  - server/api/internal/handler/plans_public.go
  - server/api/internal/handler/plans_public_test.go
  - server/api/internal/handler/servers.go
  - server/api/internal/handler/servers_test.go
  - server/api/internal/handler/webhook_lava.go
  - server/api/internal/handler/webhook_lava_test.go
  - server/api/internal/lava/client.go
  - server/api/internal/lava/client_test.go
  - server/api/internal/lava/dto.go
  - server/api/internal/lava/invoice.go
  - server/api/internal/lava/invoice_test.go
  - server/api/internal/lava/products.go
  - server/api/internal/lava/products_test.go
  - server/api/internal/lava/subscription.go
  - server/api/internal/lava/subscription_test.go
  - server/api/internal/lava/webhook.go
  - server/api/internal/lava/webhook_test.go
  - server/api/internal/middleware/admin_test.go
  - server/api/internal/middleware/audit.go
  - server/api/internal/middleware/auth.go
  - server/api/internal/middleware/lava_ip_allowlist.go
  - server/api/internal/middleware/lava_ip_allowlist_test.go
  - server/api/internal/model/invoice.go
  - server/api/internal/model/lava_contract.go
  - server/api/internal/model/lava_webhook_event.go
  - server/api/internal/model/plan.go
  - server/api/internal/model/subscription.go
  - server/api/internal/model/user.go
  - server/api/internal/repository/expiry_repo.go
  - server/api/internal/repository/expiry_repo_test.go
  - server/api/internal/repository/invoice_repo.go
  - server/api/internal/repository/invoice_repo_test.go
  - server/api/internal/repository/plan_repo.go
  - server/api/internal/repository/plan_repo_test.go
  - server/api/internal/repository/subscription_repo.go
  - server/api/internal/repository/subscription_repo_test.go
  - server/api/internal/repository/user_repo_sso_test.go
  - server/api/internal/repository/user_repo_subscription_test.go
  - server/api/internal/repository/webhook_event_repo.go
  - server/api/internal/repository/webhook_event_repo_test.go
  - server/api/internal/scheduler/scheduler.go
  - server/api/migrations/019_plans_catalog.sql
  - server/api/migrations/020_lava_payments.sql
  - server/api/migrations/migrations_test.go
  - admin-web/package.json
  - admin-web/src/App.tsx
  - admin-web/src/api/lava.ts
  - admin-web/src/api/plans.ts
  - admin-web/src/components/layout/AdminLayout.tsx
  - admin-web/src/components/plans/DeletePlanDialog.tsx
  - admin-web/src/components/plans/LavaOfferPicker.tsx
  - admin-web/src/components/plans/PlanCodeBadge.tsx
  - admin-web/src/components/plans/PlanForm.tsx
  - admin-web/src/components/plans/PlanOffersGrid.tsx
  - admin-web/src/components/plans/PlanServersPicker.tsx
  - admin-web/src/components/plans/PlansTable.tsx
  - admin-web/src/components/plans/ReplaceOfferDialog.tsx
  - admin-web/src/components/ui/badge.tsx
  - admin-web/src/components/ui/checkbox.tsx
  - admin-web/src/components/ui/form.tsx
  - admin-web/src/components/ui/select.tsx
  - admin-web/src/components/ui/switch.tsx
  - admin-web/src/components/ui/tabs.tsx
  - admin-web/src/components/ui/textarea.tsx
  - admin-web/src/components/ui/tooltip.tsx
  - admin-web/src/pages/PlanDetail.tsx
  - admin-web/src/pages/Plans.tsx
  - docs/lava-payments-api.md
findings:
  critical: 2
  warning: 8
  info: 7
  total: 17
status: issues_found
---

# Phase 3: Code Review Report

**Reviewed:** 2026-05-24
**Depth:** standard
**Files Reviewed:** 92
**Status:** issues_found

## Summary

Phase 3 introduces real-money payment processing via lava.top — a high-stakes integration that is well thought out overall. The security-critical surface (constant-time API key compare, dedicated TCP-layer IP allowlist, UNIQUE-constraint idempotency, server-side tier derivation from offer_id, hardcoded BaseURL, no-follow redirects, 5s timeouts, fail-safe planIDFromContract) shows a high level of care and good defence-in-depth. Test coverage of the webhook handler is strong (5 event types + idempotency + bad-sig + processing-error + tier-derivation tests all present).

However, the review found two **Critical** issues that need attention before paying users hit the system:

1. **CR-01** — `fiber.Config.TrustedProxies` is set to `lavaCIDRs` (lava webhook source IPs) instead of the actual reverse-proxy IP set. This causes Fiber to honour `X-Forwarded-For` headers from lava's IPs, opening a request-IP-spoofing path that affects the rate limiter, the audit log IP column, and any other code using `c.IP()`.
2. **CR-02** — The webhook idempotency UNIQUE index uses `COALESCE(payload->>'timestamp', payload->>'cancelledAt')`, but **neither field is present on `payment.failed` retries when lava omits the `timestamp` on certain retry attempts**. More importantly, the local SQLite test schema uses `UNIQUE (event_type, contract_id, payload)` — the whole JSON payload — which gives different idempotency semantics than production: if lava retries an event with even a single varying field (like `lastModifiedAt`), production de-duplicates but the test passes through. This means the test suite does NOT verify the production idempotency rule.

The Warnings cover a handful of robust-against-edge-cases gaps (planIDFromContract may return empty string in a bad-DB state, AdminCreatePlan skips server validation that AdminReplacePlanServers performs, currency derivation falls through silently on invalid input, lock-acquired-but-not-checked branch in admin-web refresh, etc.). Info items flag dead Stripe code that should be removed, comment drift, and a few small leaks.

## Critical Issues

### CR-01: `TrustedProxies` misconfigured with lava CIDR set — allows X-Forwarded-For spoofing from lava's IP range

**File:** `server/api/cmd/main.go:135-141`

**Issue:**
```go
app := fiber.New(fiber.Config{
    EnableTrustedProxyCheck: true,
    TrustedProxies:          lavaCIDRs,  // ← WRONG
})
```
`fiber.Config.TrustedProxies` enumerates the IP/CIDR set of **trusted L7 intermediaries** (your reverse proxy, CDN, load balancer). When `EnableTrustedProxyCheck` is true and a request arrives from one of these CIDRs, Fiber **honours the request's `X-Forwarded-For` header** and `c.IP()` returns the spoofed value.

By setting `TrustedProxies = lavaCIDRs` (the lava webhook source IPs from `LAVA_WEBHOOK_ALLOWED_CIDRS`), the code grants lava.top's webhook origin servers permission to spoof the client IP on every request — but lava is NOT a trusted proxy, it's a remote third-party HTTP client. If lava (or anyone with the lava webhook source IP, e.g. via misrouted Yandex Cloud tenancy) sends `X-Forwarded-For: 127.0.0.1`, `c.IP()` returns `127.0.0.1` to:
- `middleware.RateLimit` (per-IP throttle bypass)
- `middleware.AuditLog` IP column (audit log poisoning)
- Any rate limit / abuse detection that keys on `c.IP()`

The webhook handler itself is fine — it uses `c.Context().RemoteIP()` via `LavaWebhookIPAllowlist` middleware (correctly). The bug is the *global* `c.IP()` behaviour everywhere else.

The comment block (`cmd/main.go:131-134`) misreads the Fiber contract — it claims `TrustedProxies = lavaCIDRs` ensures `c.IP()` returns RemoteIP for callers outside the lava set. Actually the opposite: inside the set, c.IP() returns the spoofed forwarded value; outside the set, c.IP() returns RemoteIP. So this misconfiguration *enables* spoofing for the very IP range we should trust the least.

**Fix:**
Decide what trust model you actually want and set it explicitly. Three options:

```go
// Option A (simplest, recommended for v2.2.0 single-VM Docker Compose deploy):
// No L7 proxy in front of the API → never trust any X-Forwarded-For.
app := fiber.New(fiber.Config{
    AppName:                 "VPN API Server",
    ServerHeader:            "",
    ErrorHandler:            handler.ErrorHandler(logger),
    EnableTrustedProxyCheck: true,
    TrustedProxies:          []string{}, // empty: trust nothing
})

// Option B (when nginx/Caddy/Cloudflare fronts the API):
// Trust ONLY your reverse proxy. Source this from a NEW env var, NOT LAVA_*.
trustedProxyCIDRs := strings.Split(cfg.TrustedProxyCIDRs, ",") // e.g. "127.0.0.1/32,10.0.0.0/8"

// Option C (Cloudflare): use Cloudflare's published IP ranges.
```

Plus: rename the misleading comment, and add a config_test.go assertion that `TrustedProxies` is NOT derived from `LavaWebhookAllowedCIDRs`.

---

### CR-02: Webhook idempotency UNIQUE — production index covers a tuple that lava can submit without `timestamp` OR `cancelledAt`; SQLite test uses a different rule entirely so this gap is invisible to the test suite

**File:** `server/api/migrations/020_lava_payments.sql:76-81`, `server/api/internal/handler/webhook_lava_test.go:131-141`

**Issue:**

Production migration 020 creates:
```sql
CREATE UNIQUE INDEX idx_lava_webhook_events_natural_key
    ON lava_webhook_events (
        event_type,
        contract_id,
        COALESCE((payload->>'timestamp')::text, (payload->>'cancelledAt')::text)
    );
```

This means the dedup key is `(event_type, contract_id, timestamp_or_cancelledAt)`. Two distinct problems:

**Problem A: `payment.failed` retries may omit `timestamp` on later retries.**

`InsertWebhookEventIfNew` is called with `event.ContractID` as the contract_id and the raw JSON body as payload. If `payment.failed` arrives with `{"eventType":"payment.failed","contractId":"X","timestamp":"T"}` first, and lava's second retry strips or changes the timestamp (some providers do — RESEARCH §1.5 is not explicit), the third COALESCE value differs and BOTH inserts succeed — idempotency is lost. The `errorMessage` field can also drift between retries (lava's stack trace changes), but it's not in the natural key — that's correct.

The bigger concern: the third column resolves to NULL when BOTH `timestamp` AND `cancelledAt` are absent. Postgres treats `NULL <> NULL` for unique indexes, so two events both lacking these fields collide as DISTINCT — meaning a hostile payload `{"eventType":"X","contractId":"Y"}` with NO timestamp can be replayed unbounded times before any dedup kicks in.

**Problem B: SQLite test schema diverges from production.**

The test schema at `handler/webhook_lava_test.go:131-141` declares:
```sql
CREATE TABLE lava_webhook_events (
    ...
    UNIQUE (event_type, contract_id, payload)  -- ← entire JSON blob
)
```
This is a fundamentally different idempotency rule (whole-body equality vs. natural-key-COALESCE). Even if production has the bug, the test suite cannot exercise it — `TestHandleLavaWebhook_DuplicateNoop` passes because the body is byte-identical, but the production CONFLICT path executes a different SQL expression entirely. A lava retry with a single varying ms-precision field would pass the SQLite UNIQUE but fail the production COALESCE-UNIQUE.

Risk: **a lava webhook retry storm that genuinely succeeds idempotently in production cannot be regression-tested**, because the test schema is structurally different. Conversely, a real-world replay attack that should be rejected by production may slip through if `timestamp` is absent and `cancelledAt` is absent.

**Fix:**

1. Add `invoice_id` to the natural key and use a coalesce-with-fallback-string so NULLs never participate:
   ```sql
   DROP INDEX IF EXISTS idx_lava_webhook_events_natural_key;
   CREATE UNIQUE INDEX idx_lava_webhook_events_natural_key
       ON lava_webhook_events (
           event_type,
           contract_id,
           COALESCE(
               payload->>'timestamp',
               payload->>'cancelledAt',
               payload->>'eventId',  -- if lava sends one
               'no-timestamp'        -- explicit non-NULL sentinel
           )
       );
   ```

2. Update the SQLite test schema in `webhook_lava_test.go:131-141` to mirror the production COALESCE rule (SQLite supports `json_extract(payload, '$.timestamp')`):
   ```sql
   CREATE UNIQUE INDEX idx_lava_webhook_events_natural_key
       ON lava_webhook_events (
           event_type,
           contract_id,
           COALESCE(
               json_extract(payload, '$.timestamp'),
               json_extract(payload, '$.cancelledAt'),
               'no-timestamp'
           )
       );
   ```

3. Add a new test that delivers two events with the same `(event_type, contract_id)` but different `timestamp` values — assert TWO rows insert (proves the natural key includes timestamp). And another delivering two events with no `timestamp`/`cancelledAt` at all — assert exactly ONE row inserts (proves NULL collisions are caught).

## Warnings

### WR-01: `planIDFromContract` returns empty string when both offer and system plan lookups fail — SetUserPlan then fails with a `record not found` error rolling back the whole renewal

**File:** `server/api/internal/handler/webhook_lava.go:396-404`

**Issue:**
```go
func planIDFromContract(db *gorm.DB, contract *model.LavaContract) string {
    if offer, err := repository.FindOfferByLavaOfferID(db, contract.OfferID); err == nil {
        return offer.PlanID
    }
    if sid, err := repository.FindSystemPlanID(db); err == nil {
        return sid
    }
    return ""  // ← empty string is silently accepted by caller
}
```

The function returns `""` on double-failure (offer gone AND system plan gone — exceedingly rare but possible during a migration in-flight). The caller at line 246 unconditionally passes that empty string to `repository.SetUserPlan(db, parent.UserID, planIDFromContract(db, parent), &contractID, &newExp)`. `SetUserPlan` then executes `tx.Where("id = ?", planID).First(&plan)` which returns `gorm.ErrRecordNotFound`. The whole transaction rolls back; the handler returns 500 (good — triggers lava retry); the next retry hits the same condition.

This is *fail-stuck* under a degraded DB. The fail-safe comment ("never elevate") is honoured in the offer-found path, but the empty-string fallback is a worst-of-both-worlds: it doesn't grant Pro, but it also doesn't downgrade safely — it just keeps retrying forever and burning lava's 20-retry budget.

**Fix:**
Return a sentinel error and propagate so the caller can log loudly:
```go
func planIDFromContract(db *gorm.DB, contract *model.LavaContract) (string, error) {
    if offer, err := repository.FindOfferByLavaOfferID(db, contract.OfferID); err == nil {
        return offer.PlanID, nil
    } else if !errors.Is(err, repository.ErrNotFound) {
        return "", fmt.Errorf("planIDFromContract: lookup offer: %w", err)
    }
    if sid, err := repository.FindSystemPlanID(db); err == nil {
        return sid, nil
    } else {
        return "", fmt.Errorf("planIDFromContract: lookup system plan: %w", err)
    }
}
```
Then at the call site (line 246), check the error and return it from `handleLavaRecurringSuccess` so the outer wrapper records it in `lava_webhook_events.error` for forensics, and the operator sees the failed retries in the logs.

---

### WR-02: `AdminCreatePlan` does not validate that supplied `server_ids` exist or are active — `AdminReplacePlanServers` does. Inconsistent enforcement on the same join table

**File:** `server/api/internal/handler/plans_admin.go:206-228` vs `:416-425`

**Issue:**

`AdminCreatePlan` iterates `req.ServerIDs` and calls `repository.AddPlanServer(tx, plan.ID, sid)` directly. There's no check that `sid` corresponds to a real, active VPN server. A malformed or stale ID will silently insert a `plan_servers` row pointing nowhere (then later cause subtle 404s when a paying user tries to connect).

`AdminReplacePlanServers` (line 416) DOES validate every server_id exists with `is_active=true`, returning 422 with the offending `server_id` echoed back. The two endpoints should behave identically.

The composite FK constraint in migration 019 (`server_id UUID NOT NULL REFERENCES vpn_servers(id) ON DELETE CASCADE`) prevents the worst case (FK error rolls back the tx), so this is "noisy 500" not "data corruption" — but the UX is wrong (admin sees 500 instead of a friendly 422 naming the bad server_id) and the rollback wastes work the admin already provided.

**Fix:**
Extract a shared helper and call it from both places:
```go
func validateServerIDs(db *gorm.DB, serverIDs []string) (string, error) {
    for _, sid := range serverIDs {
        var n int64
        if err := db.Table("vpn_servers").Where("id = ? AND is_active = ?", sid, true).Count(&n).Error; err != nil {
            return sid, err
        }
        if n == 0 {
            return sid, fmt.Errorf("server not found or inactive")
        }
    }
    return "", nil
}
```
In `AdminCreatePlan`, call it BEFORE `db.Transaction(...)` so the 422 path doesn't open and immediately roll back a transaction.

---

### WR-03: `deriveCurrencyFromAcceptLanguage` silently picks USD on any non-`ru` header — including hostile `?currency=BTC` falls back to AcceptLanguage which falls back to USD instead of rejecting

**File:** `server/api/internal/handler/plans_public.go:54-67, 138-143`

**Issue:**

```go
currency := strings.ToUpper(strings.TrimSpace(c.Query("currency")))
if currency == "" {
    currency = deriveCurrencyFromAcceptLanguage(c.Get("Accept-Language"))
}
if _, ok := allowedPublicCurrencies[currency]; !ok {
    return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid currency"})
}
```

This is correct on the surface, but `deriveCurrencyFromAcceptLanguage` (line 138-143) silently returns `"USD"` for any header that isn't `ru*` — including empty headers, `xx-INVALID`, garbage. That means a Russian-locale browser that omits the header (some embedded webviews do) gets RUB pricing but a sloppy parser branch is hard to spot.

More important: when `?currency=` is supplied but it's not in `{USD,EUR,RUB}`, the 400 response is good. But there's no logging — admin can't tell whether a hostile client is probing for currency support.

Also: the `allowedPublicCurrencies` map at line 18-20 is a duplicate of `allowedCurrencies` at `payment.go:20-22`. Drift risk: if someone adds `RUB` or removes one in only one file, behaviour diverges silently.

**Fix:**
1. Consolidate the allowedCurrencies map to a single package-level constant (e.g. in a shared `currencies.go`).
2. Add `logger.Debug("/plans: invalid currency rejected", zap.String("currency", currency))` before the 400 return so abuse is detectable.
3. In `deriveCurrencyFromAcceptLanguage`, prefer explicit `q=` quality-value parsing over `HasPrefix` for correctness on Accept-Language: `en-US,en;q=0.9,ru;q=0.8` would silently get USD even though the user can read Russian.

---

### WR-04: `mapLavaStatusToLocal` returns `""` for unknown statuses but the caller's nil check at payment.go:297 misses the empty case via short-circuit

**File:** `server/api/internal/handler/payment.go:297-304, 328-341`

**Issue:**
```go
localStatus := mapLavaStatusToLocal(lavaInv.Status)
if localStatus != inv.Status && localStatus != "" {
    if uerr := repository.UpdateInvoiceStatus(db, inv.ID, localStatus); uerr != nil {
        ...
    } else {
        inv.Status = localStatus
    }
}
```

The empty-string guard at line 297 correctly handles unknown lava statuses (e.g. lava adds a new enum value in future). But this is paired with NO logging — if lava sends a value `mapLavaStatusToLocal` doesn't understand, the escalate path silently returns the stale `pending` status to the user. The user's `/pay/success` page will keep polling forever.

**Fix:**
Add a Warn log when the mapping yields `""` so the operator catches lava API changes early:
```go
localStatus := mapLavaStatusToLocal(lavaInv.Status)
if localStatus == "" {
    logger.Warn("invoice: lava status not mapped — keeping local status",
        zap.String("lava_status", lavaInv.Status),
        zap.String("local_status", inv.Status),
        zap.String("lava_invoice_id", inv.LavaInvoiceID))
}
if localStatus != inv.Status && localStatus != "" {
    ...
}
```

---

### WR-05: `admin-web/src/api/client.ts` lock-acquired branch reads `preLockTokens` but the comment says it should refresh anyway — the captured-but-unused variable is misleading dead code

**File:** `admin-web/src/api/client.ts:126-144`

**Issue:**
```typescript
return locks.request("vpn-admin-refresh", { mode: "exclusive" }, async () => {
    const preLockTokens = authSelectors.getTokens();
    const latest = preLockTokens; // captured after lock acquisition
    if (latest && latest.accessToken && latest.refreshToken) {
        // The store write from a sibling tab is picked up via Zustand's
        // persist middleware listening on `storage` events; by the time
        // we get here latest reflects whatever the winner wrote. The
        // heuristic "refresh only if we still hold the same access
        // token we had before" can't be expressed cleanly here, so
        // just issue the refresh once under the lock. The race window
        // is closed by the exclusivity — at most one /refresh per
        // tab-cluster.
    }
    return performRefresh();
});
```

The whole `preLockTokens`/`latest`/`if` block is dead code — it captures variables that are never read, then unconditionally calls `performRefresh()`. The comment correctly identifies the gap (no way to know "did the winner refresh while I was waiting"), but the dead code is misleading: a future maintainer will think there's an optimisation that just doesn't fire. Also, even though the per-tab `refreshInFlight` debounce avoids in-tab thundering herd, a multi-tab cluster will issue N sequential refreshes (one per waiter) and each waiter discards the previous tab's now-valid token. The previous tab's `performRefresh()` deleted the old session row; the second tab's `performRefresh()` will succeed (the just-rotated token is now the "old"); the third will fail because the second's rotation deleted IT.

End result: under N concurrent tabs that all 401 within a few ms of each other, only one tab ends up authenticated; the rest hit the catch and `authSelectors.clear()` — exactly the bug the cross-tab lock was supposed to prevent.

**Fix:**
Track the access-token value captured at the moment the original 401 fired, then inside the lock compare to the current store value:
```typescript
// At interceptor scope, capture the failing token:
const tokenAtFailure = authSelectors.getTokens()?.accessToken;
// ...inside the lock callback:
return locks.request("vpn-admin-refresh", { mode: "exclusive" }, async () => {
    const latest = authSelectors.getTokens();
    // If the sibling tab already rotated, we're holding a fresh token now.
    if (latest && latest.accessToken && latest.accessToken !== tokenAtFailure) {
        return latest; // skip the refresh call entirely
    }
    return performRefresh();
});
```
This requires plumbing `tokenAtFailure` from the interceptor down to `refreshAccessToken()`.

---

### WR-06: Webhook handler's `MarkWebhookProcessed` error is discarded with `_ =` even though the comment claims it's logged loudly

**File:** `server/api/internal/handler/webhook_lava.go:118, 125, 135`

**Issue:**
Three call sites:
```go
_ = repository.MarkWebhookProcessed(db, rec.ID, nil)               // line 118 (unknown event branch)
_ = repository.MarkWebhookProcessed(db, rec.ID, &errStr)           // line 125 (error branch)
_ = repository.MarkWebhookProcessed(db, rec.ID, nil)               // line 135 (success branch)
```

`MarkWebhookProcessed`'s contract (line 35-46 of `webhook_event_repo.go`) says "Best-effort — caller does NOT propagate error from this call (the side effect of failing here is a stale forensic record; the 500 returned to lava ensures retry handles the real work)." Fair enough — but the comment also says the failure has a side effect (stale forensic data), and the caller silently drops the error without even a `logger.Warn`. The forensic record will show `processed_at IS NULL` after a successful handler — operator looking at the row later will conclude the event failed mid-flight, which is wrong.

**Fix:**
Log the failure at Warn level so the forensic record's apparent inconsistency is correlatable to an actual error:
```go
if err := repository.MarkWebhookProcessed(db, rec.ID, nil); err != nil {
    logger.Warn("webhook: MarkWebhookProcessed failed (forensic record will be stale)",
        zap.String("event_id", rec.ID),
        zap.String("event_type", event.EventType),
        zap.Error(err))
}
```

---

### WR-07: `CancelSubscription` queries by `is_active = true` ordered by `started_at DESC` — but `is_active` was flipped to `false` by `handleLavaRecurringFailed` on payment failure, so a user who manually cancels post-failure gets a 404 instead of a successful no-op cancel-the-already-failed-contract

**File:** `server/api/internal/handler/payment.go:215-224`

**Issue:**

```go
var contract model.LavaContract
findErr := db.Where("user_id = ? AND is_active = ?", userID, true).Order("started_at DESC").First(&contract).Error
if findErr != nil {
    if errors.Is(findErr, gorm.ErrRecordNotFound) {
        return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "no active subscription"})
    }
    ...
}
```

After plan 03-06 T03 (D-19 BLOCKER #1 fix), `subscription.recurring.payment.failed` flips `lava_contracts.is_active = false` immediately. The user still has Pro until `expires_at` lapses (correct per the comment), and the mobile app's Account screen still shows "active" until then. If they then tap "Cancel subscription", this handler 404s because no `is_active=true` row exists.

The user is confused: they see Pro, they tap cancel, they get an opaque "no active subscription" error. The lava-side state may also still be "active" (lava hasn't yet sent `subscription.cancelled`), so a real DELETE to lava is still needed to prevent the next retry from re-attempting the failed payment.

**Fix:**
Either:
- (A) Match the most recent contract regardless of `is_active`, as long as `expires_at` is in the future or NULL:
  ```go
  findErr := db.Where("user_id = ? AND (expires_at IS NULL OR expires_at > ?)", userID, time.Now()).
      Order("started_at DESC").First(&contract).Error
  ```
- (B) Return 200 with `{"already_inactive": true}` when the row exists but `is_active=false`, so the UX is "you're already cancelled".

Option A is preferred — it matches the user's mental model ("I still have Pro, let me cancel").

---

### WR-08: `escapeQuery(contractID)` in lava.CancelSubscription pre-escapes the contract id but the URL is built via string concat — risk of double-escaping if a lava-supplied contract id ever contains `%`

**File:** `server/api/internal/lava/subscription.go:14-25`

**Issue:**
```go
path := "/api/v1/subscriptions?contractId=" + escapeQuery(contractID) + "&email=" + escapeQuery(email)
```

This builds the URL via string concatenation rather than `url.Values.Encode()`. It works for ASCII-safe contract ids and emails, but if `contractID` or `email` ever contains a literal `%` (perhaps in a future lava format), the manual escape will double-encode.

A more concerning issue: `email` is the user's stored email (`*user.Email`). Most emails are safe, but emails like `user+tag@example.com` are valid; `escapeQuery` correctly encodes `+` → `%2B` → that's fine. But this is brittle for the same reason — any future change that introduces a `&` literal into the email path WILL break.

**Fix:**
Use `url.Values`:
```go
func (c *Client) CancelSubscription(ctx context.Context, contractID, email string) error {
    q := url.Values{}
    q.Set("contractId", contractID)
    q.Set("email", email)
    path := "/api/v1/subscriptions?" + q.Encode()
    ...
}
```
Same risk profile applies to `products.go:18` (`?nextPage=`). Cleaner overall.

## Info

### IN-01: `STRIPE_*` config fields, env keys, and the `stripe-go v81.4.0` dependency are still in the codebase even though no production code path imports the package

**File:** `server/api/go.mod:15`, `server/api/internal/config/config.go:16-19, 100-103, 268-272`, `server/api/.env.example:76-83`

**Issue:**
Phase 3 plan says "Full Stripe removal — Stripe code/refs MUST be gone" but:
- `go.mod:15` still declares `github.com/stripe/stripe-go/v81 v81.4.0`
- `go.sum` retains the corresponding hashes
- `config.go` declares `StripeKey`, `StripeWebhookSecret`, `StripePricePremium`, `StripePriceUltimate` fields
- `OptionalEnvWarnings()` (line 266-284) still flags Stripe env vars
- `.env.example:76-83` still documents them as "DEPRECATED — Phase 8"

This is intentional per the env-example comment ("Phase 8 HARD-01 removes the module"), but the phase context explicitly demanded Stripe removal in Phase 3. Either:
- Confirm the phase boundary is correct (this is Phase 3, Stripe leaves in Phase 8) and update the context to reflect that, OR
- Actually remove now: `go mod tidy` after removing the references, delete the four `Stripe*` Config fields, drop the `.env.example` block, drop the OptionalEnvWarnings entries.

**Fix:** Confirm intent with the planner. If the Phase 8 boundary is correct, just leave a `TODO(phase-8): remove stripe-go module` comment at `go.mod:15` so future readers don't think this is dead code that's been forgotten.

---

### IN-02: `admin_test.go:336` still has `stripe_id TEXT` in the SQLite test schema even though migration 020 drops the column

**File:** `server/api/internal/handler/admin_test.go:336` (not in scope but referenced by the Stripe grep)

**Issue:** The SQLite test for admin handlers includes `stripe_id TEXT` in its `subscriptions` table DDL. Migration 020 drops this column in production. The test still compiles and passes but is testing against a different schema than production.

**Fix:** Remove `stripe_id` from the SQLite test schema and confirm `admin_test.go` doesn't reference `Subscription.StripeID` anywhere.

---

### IN-03: `middleware/version.go:84` doc comment still references `POST /webhook/stripe` as an example of skip-rule target — but the route was removed

**File:** `server/api/internal/middleware/version.go:84`

**Issue:**
```go
//   - POST /webhook/stripe (called by Stripe servers, not the app)
```
The route was removed; the example is now misleading. Should be `POST /webhook/lava`.

**Fix:** Update the comment to:
```go
//   - POST /webhook/lava (called by lava.top servers, not the app)
```

---

### IN-04: `payment.go:346` has a `var _ = fmt.Sprintf` workaround for an "unused import" — suggests the import is actually unused and should be removed

**File:** `server/api/internal/handler/payment.go:343-346`

**Issue:**
```go
// Ensure the package compiles when imported by tests that previously
// referenced Stripe helpers. The compile-time fmt usage prevents an
// "imported and not used" error if no other reference remains.
var _ = fmt.Sprintf
```

`fmt` IS used in this file at line 145, 161, 183, etc. (`fmt.Errorf`-style returns and `zap.Error(err)`). The workaround is unnecessary and the comment is misleading.

**Fix:** Delete lines 343-346. If `fmt` ends up actually unused after some future refactor, the compiler will tell you.

---

### IN-05: `escapeQuery` and `pathEscape` thin wrappers in `lava/client.go:111-114` add zero value

**File:** `server/api/internal/lava/client.go:111-114`

**Issue:**
```go
func escapeQuery(v string) string { return url.QueryEscape(v) }
func pathEscape(v string) string { return url.PathEscape(v) }
```
These add nothing — call sites can use `url.QueryEscape` / `url.PathEscape` directly. The "reads cleanly" comment is contradicted by the indirection cost.

**Fix:** Inline the calls. Drop the wrappers. (Optional — pure stylistic, no behavioural impact.)

---

### IN-06: `auth.go:1093` reference to `audit-trail entry in Redis` in the `WR-02` boundary case comment — but the prior REVIEW.md numbering is now stale relative to the current code

**File:** `server/api/internal/handler/auth.go:1161-1164`

**Issue:**
```go
// WR-02: use `ttl >= 0` so the boundary case (token expiring this
// exact second) still produces an audit-trail entry in Redis. Per
// REVIEW.md WR-02: even a near-zero TTL keeps the keyspace
// observer's record complete.
```
The reference to "REVIEW.md WR-02" is to a previous phase's review document (probably plan 02-06). Future maintainers won't have that context. Inline the rationale.

**Fix:**
```go
// Use `ttl >= 0` so the boundary case (token expiring this exact
// second) still produces a blacklist entry. A near-zero TTL costs
// nothing in Redis and keeps the audit-trail complete — without
// this, every minute-of-day-boundary token expiry would silently
// skip the blacklist write.
```

---

### IN-07: `LavaOfferPicker.tsx` uses `key={`${r.offerId}-${r.currency}`}` but `offerId` is meant to be unique — the extra `-currency` suffix is harmless but suggests the key should already be unique by `offerId`

**File:** `admin-web/src/components/plans/LavaOfferPicker.tsx:109`

**Issue:**
```tsx
{rows.map((r) => (
    <SelectItem key={`${r.offerId}-${r.currency}`} value={r.offerId}>
```
The composite key works around a hypothetical collision where the same `offerId` appears in two currency rows — but the backend `lavaProductRow` is built from `(product, offer, price)` tuples; a given lava offer DOES have multiple prices (one per currency). So the key is correct — but the `value` field is `offerId` alone, which means picking "USD $5" vs "EUR €5" for the same offer is ambiguous (Radix Select keys items by `value`, and identical `value`s collapse to one). The `Select` will not let you select between the two currencies.

The `filterCurrency` prop at line 64-72 partly hides this by filtering to one currency at a time. As long as the parent always passes `filterCurrency`, the bug is masked. But if a parent ever calls `<LavaOfferPicker value={...} onChange={...} />` without a filter, the user sees two rows for "Pro MONTHLY $5" and "Pro MONTHLY €5" but can only pick one (whichever Radix de-duplicates last).

**Fix:** Make `value` composite too, then parse it in `onValueChange`:
```tsx
<SelectItem key={`${r.offerId}-${r.currency}`} value={`${r.offerId}|${r.currency}`}>
// ...
onValueChange={(next) => {
    const [offerId, currency] = next.split("|");
    const row = rows.find((r) => r.offerId === offerId && r.currency === currency);
    if (row) onChange(row);
}}
```
Or document that `filterCurrency` is mandatory and assert it.

---

_Reviewed: 2026-05-24_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
