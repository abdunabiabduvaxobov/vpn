---
phase: 03-lava-top-plans-catalog
fixed_at: 2026-05-24T00:00:00Z
review_path: .planning/phases/03-lava-top-plans-catalog/03-REVIEW.md
iteration: 1
findings_in_scope: 10
fixed: 9
skipped: 1
status: partial
---

# Phase 3: Code Review Fix Report

**Fixed at:** 2026-05-24
**Source review:** `.planning/phases/03-lava-top-plans-catalog/03-REVIEW.md`
**Iteration:** 1

**Summary:**
- Findings in scope: 10 (2 Critical + 8 Warning; Info excluded by scope)
- Fixed: 9
- Skipped: 1

After every fix the full `go build ./...` and `go test ./... -short -count=1 -timeout=120s` sweep passed cleanly. Each fix is its own atomic commit using the `fix(03): <ID> <summary>` convention.

## Fixed Issues

### CR-01: TrustedProxies misconfigured with lava CIDR set

**Files modified:** `server/api/cmd/main.go`
**Commit:** bb573df
**Applied fix:** Set `fiber.Config.TrustedProxies` to an empty slice (`[]string{}`) instead of `lavaCIDRs`. The v2.2.0 single-VM Docker Compose deploy has no L7 proxy in front of the API, so trusting nothing is correct. Rewrote the inline comment to flag the misuse: when a reverse proxy is introduced later, populate from a NEW config field, NOT `LAVA_WEBHOOK_ALLOWED_CIDRS`. The webhook route still uses its own `LavaWebhookIPAllowlist` middleware which correctly reads `c.Context().RemoteIP()` (TCP-layer, unspoofable) per PAY-06.

### CR-02: Webhook idempotency UNIQUE has a NULL hole + SQLite test schema diverges from production

**Files modified:** `server/api/migrations/021_lava_webhook_natural_key_no_null_hole.sql` (new), `server/api/migrations/migrations_test.go`, `server/api/internal/handler/webhook_lava_test.go`
**Commit:** e971689
**Applied fix:**
1. Added migration 021 that drops + re-creates `idx_lava_webhook_events_natural_key` with `COALESCE(payload->>'timestamp', payload->>'cancelledAt', 'no-timestamp')` so the third column is always non-NULL.
2. Aligned the SQLite test schema in `webhook_lava_test.go` to mirror the production rule using `json_extract` + the same sentinel — replaced the inline `UNIQUE (event_type, contract_id, payload)` (which had completely different idempotency semantics) with a separate `CREATE UNIQUE INDEX` matching production.
3. Added two regression tests against the SQLite-mirrored rule:
   - `TestHandleLavaWebhook_NaturalKey_NoTimestampCollision` — two no-timestamp deliveries produce exactly one row (NULL hole closed).
   - `TestHandleLavaWebhook_NaturalKey_DistinctTimestampsInsertBoth` — distinct timestamps produce two rows (natural key still discriminates real retries).
4. Extended `TestMigrations019_020` to apply 021 in order and assert the same two behaviours against real Postgres via testcontainers.

### WR-01: planIDFromContract returns empty string on double-failure

**Files modified:** `server/api/internal/handler/webhook_lava.go`
**Commit:** e6f1d4d
**Applied fix:** Changed `planIDFromContract` signature to `(string, error)`. Preserved fail-safe semantics (offer lookup first, fall back to system plan on `ErrNotFound`). Only when BOTH lookups hit `ErrNotFound` — or either returns a non-NotFound DB error — does the function surface a structured error. The `handleLavaRecurringSuccess` caller now propagates the error so the outer wrapper records it in `lava_webhook_events.error` for forensics, replacing the previous fail-stuck retry-storm behaviour.

### WR-02: AdminCreatePlan skips server_id validation that AdminReplacePlanServers performs

**Files modified:** `server/api/internal/handler/plans_admin.go`
**Commit:** b953e27
**Applied fix:** Extracted `validatePlanServerIDs(db, serverIDs) (badSID, error)` shared helper. `AdminCreatePlan` now validates BEFORE opening the create transaction (avoids wasted rollback on bad ids); `AdminReplacePlanServers` was rewired to call the same helper. Both endpoints now return identical 422 + offending `server_id` echoed back on miss.

### WR-03: deriveCurrencyFromAcceptLanguage silent fallback + no log on invalid currency

**Files modified:** `server/api/internal/handler/plans_public.go`
**Commit:** 76b33a7
**Applied fix (minimal targeted):** Added `logger.Debug("/plans: invalid currency rejected", ...)` before the 400 return so abuse / probing is detectable. The two other suggestions in the review (consolidate `allowedPublicCurrencies` and `allowedCurrencies` into a shared module; rewrite `deriveCurrencyFromAcceptLanguage` to parse `q=` quality values per RFC) are deferred — both are design decisions about shared-constants module placement and parser semantics that go beyond the minimal targeted fix. Documented in commit message.

### WR-04: mapLavaStatusToLocal returns "" silently when lava adds a new enum value

**Files modified:** `server/api/internal/handler/payment.go`
**Commit:** 4317470
**Applied fix:** Added `logger.Warn("invoice: lava status not mapped — keeping local status", ...)` with the lava status, kept local status, and `lava_invoice_id` so log scrapers can alert on unmapped values and the operator can update the mapping promptly. Behaviour unchanged — the escalate path still skips the status flip on `""`.

### WR-06: MarkWebhookProcessed errors discarded with `_ =` at three call sites

**Files modified:** `server/api/internal/handler/webhook_lava.go`
**Commit:** b5bd0af
**Applied fix:** Each of the three call sites (unknown-event, error-branch, success-branch) now logs failures at Warn with `event_id`, `event_type`, `contract_id`, and the underlying error. Behaviour preserved — the caller still does not propagate the error, and the 200/500 response to lava is still authoritative — but the apparent inconsistency in the forensic row (`processed_at IS NULL` on a successful path) is now correlatable to the actual cause.

### WR-07: CancelSubscription 404s after recurring.payment.failed flips is_active=false

**Status:** fixed: requires human verification
**Files modified:** `server/api/internal/handler/payment.go`
**Commit:** f969a1a
**Applied fix:** Replaced the WHERE clause from `user_id = ? AND is_active = ?` to `user_id = ? AND (expires_at IS NULL OR expires_at > now())` ordered by `started_at DESC`. After D-19 BLOCKER #1, `subscription.recurring.payment.failed` flips `lava_contracts.is_active=false` immediately while the user keeps Pro until `expires_at` lapses — so a user who sees "Pro active" in the app and taps Cancel was getting 404 with no lava-side DELETE fired. This change matches the user's mental model and ensures the lava-side DELETE is still attempted.

**Why human verification required:** this is a deliberate behaviour shift (404→200 path for the recurring-failed-but-still-Pro window) — verifies as a logic change per `<verification_strategy>`. The existing `TestCancelSubscription_KeepsProUntilExpiry` covers the `active+future-expires_at` case (still passes). A new test exercising the `is_active=false` but `expires_at > now()` case is recommended before this ships.

### WR-08: lava CancelSubscription builds URL via string concat with manual escapeQuery

**Files modified:** `server/api/internal/lava/subscription.go`
**Commit:** c658b5c
**Applied fix:** Replaced the `"path?contractId=" + escapeQuery(contractID) + "&email=" + escapeQuery(email)` concatenation with `url.Values{}` + `q.Encode()`. Encoded output is byte-identical for the values the existing tests exercise, so tests pass unchanged. The same pattern in `products.go` (`?nextPage=` cursor) is noted in the commit message but not modified here — that surface is lower-risk (opaque lava-supplied cursors) and is left for a follow-up sweep.

## Skipped Issues

### WR-05: admin-web client.ts lock-acquired branch reads preLockTokens but does nothing with it

**File:** `admin-web/src/api/client.ts:126-144`
**Reason:** skipped: requires_design_decision

The reviewer's suggested fix requires plumbing a new `tokenAtFailure` value from the axios interceptor down through `refreshAccessToken()` and into the lock callback, then conditionally returning the existing tokens (skipping `performRefresh()`) when the store value differs from `tokenAtFailure`. This is an architectural change:
- Changes the contract of `refreshAccessToken()` (must accept a token-at-failure argument).
- Requires deciding how to handle the per-tab `refreshInFlight` debounce when the interceptor's snapshot differs from the in-flight value (concurrent 401s within the same tab).
- Touches the multi-tab race-window semantics that the existing comment explicitly punts on.

Per fix-scope guidance ("Skip any Warning that requires architectural changes — document the reason"), this is deferred. The current behaviour is correct under N=1 tab (the per-tab `refreshInFlight` debounce handles it). The multi-tab degradation the reviewer describes is real but the fix is non-trivial and warrants a focused frontend design pass.

---

_Fixed: 2026-05-24_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 1_
