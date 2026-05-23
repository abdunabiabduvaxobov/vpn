# Phase 3: Lava.top + plans catalog — Research

**Researched:** 2026-05-23
**Status:** Ready for planning
**Sources:** CONTEXT.md (32 D-XX decisions), ADR-007 §19, lava.top OpenAPI (`gate.lava.top/docs/documentation.yaml`), Fiber v2 source (`app.go`), GORM v2 docs, Phase 1/2 code anchors.
**Confidence:** HIGH for backend code shape and lava.top API; MEDIUM for some webhook payload field names (lava docs are sparse on Russian-text descriptions — planner should run a one-shot sandbox probe before locking field bindings).

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions (32 total)

Verbatim from CONTEXT.md `<decisions>`:

**Stripe disposition (this phase):**
- **D-01:** Rewrite `internal/handler/payment.go` in-place this phase: delete the four Stripe handlers, replace with lava-bound implementations of `CreateCheckoutSession`, `HandleLavaWebhook`, `CancelSubscription`, plus new `GetInvoice`.
- **D-02:** Remove `POST /webhook/stripe` and `POST /subscription/checkout` from `cmd/main.go`. Add `POST /checkout`, `POST /webhook/lava`, `POST /subscription/cancel`, `GET /invoices/:id`, `GET /plans`, `/admin/plans/*`, `/admin/lava/products`.
- **D-03:** Keep `stripe-go` in `go.mod` for now. `payment_test.go`, `admin_test.go`, `auth_test.go`, `subscription_repo_test.go` still reference `stripe_id` in their test-local DDL. **Phase 3 does not touch `payment_test.go` Stripe tests.**

**Chunking & shipping:**
- **D-04:** ~7-9 plans across 4-5 waves (W1 migrations + lava client → W2 plan_repo + server-access enforcement → W3 checkout + webhook + public /plans → W4 admin CRUD + expiry cron → W5 admin-web UI + sandbox integration test + docs).
- **D-05:** Branching matches Phase 1/2 — working branch, atomic per-plan commits. Tag `v2.2.0-pay` after Wave 5 integration test passes.
- **D-06:** Integration test runs against **lava.top sandbox** with `LAVA_API_KEY_SANDBOX`. Production smoke deferred to Phase 5.

**Schema, migrations, seeds:**
- **D-07:** Migration filenames `019_plans_catalog.sql` + `020_lava_payments.sql` (Phase 2 took 018).
- **D-08:** `019` content per ADR §19.3 with overrides: Pro `max_devices=3` (not 5), free `max_devices=1, max_servers=3, speed_limit_mbps=50, is_system=TRUE`. Free seeded with 3 lowest-load active servers; Pro gets every active server. Coerce `premium`/`ultimate` → `pro`.
- **D-09:** `019` seeds **6 placeholder `plan_offers`** for Pro: `{MONTHLY, PERIOD_YEAR} × {USD, EUR, RUB}` with `lava_offer_id=NULL`. `/checkout` returns `409 {"error":"offer_not_configured"}` until non-NULL.
- **D-10:** `020` per ADR §8.3 + §19.6. `lava_webhook_events.UNIQUE (event_type, contract_id, (payload->>'timestamp'))` casts as text.
- **D-11:** `subscriptions.stripe_id` dropped (`DROP COLUMN IF EXISTS stripe_id`). `subscriptions.lava_contract_id` added. Test-local DDL with `stripe_id` is unaffected (test tables built fresh per test).

**Lava client & offer-ID sourcing (Option B):**
- **D-12:** `lava_offer_id` sourcing is **Option B only** — synced dropdown. `GET /api/v1/admin/lava/products` proxies `GET https://gate.lava.top/api/v2/products`. No paste path.
- **D-13:** Admin UI for plan-offer editing is **in scope for Phase 3** (overrides ADR §19.13's "Phase 3.5").
- **D-14:** Lava client package layout `server/api/internal/lava/`: `client.go`, `invoice.go`, `products.go`, `subscription.go`, `webhook.go`, `dto.go`. Pure package — no Fiber, no GORM.
- **D-15:** Hardcoded `const BaseURL = "https://gate.lava.top"`. No env override.

**Webhook security & failure semantics:**
- **D-16:** IP allowlist via `LAVA_WEBHOOK_ALLOWED_CIDRS` (CSV, default `158.160.60.174/32`). Fiber `EnableTrustedProxyCheck=true` + `TrustedProxies=<list>`. Never read `X-Forwarded-For` / `X-Real-IP` directly.
- **D-17:** `X-Api-Key` check: `LAVA_WEBHOOK_SECRET` + optional `LAVA_WEBHOOK_SECRET_PREVIOUS`. Both via `crypto/subtle.ConstantTimeCompare`. Zero-downtime rotation.
- **D-18:** UNIQUE `(event_type, contract_id, (payload->>'timestamp'))` text cast. Duplicate → 200 OK no-op. Processing error → 500 (lava retries).
- **D-19:** Event semantics (5 event types):
  - `payment.success` → mark paid, upsert `lava_contracts`, call `SetUserPlan(user, plan)` transactional.
  - `subscription.recurring.payment.success` → extend `subscriptions.expires_at` + `lava_contracts.expires_at` by one period.
  - `payment.failed` → mark invoice failed, no tier change.
  - `subscription.recurring.payment.failed` → set `is_active=false` immediately on both rows. Tier downgrade waits for `expires_at` cron.
  - `subscription.cancelled` → set `cancelled_at=now()`, `is_active=false`. Tier untouched until cron.
- **D-20:** No additional rate-limiting on webhook beyond global per-IP (HOTFIX-03).

**Server access enforcement:**
- **D-21:** `ListServersForPlan(planID)` + `IsServerAllowedForPlan(planID, serverID)` in `repository/plan_repo.go`. Admin bypass (handler-level role check).
- **D-22:** `GET /servers/:id/config` for non-allowed server returns **404 Not Found** (don't leak existence).
- **D-23:** `DELETE /admin/plans/:id/servers/:server_id` does NOT force-disconnect connected users. Next reconnect fails.
- **D-24:** `connection.go` reads device limits from `FindPlanByID(planID)`. `admin.go` validates against `FindPlanByCode`. `health.go` reads system-plan via `is_system=true` filter.

**Polling endpoint & expiry cron:**
- **D-25:** `GET /api/v1/invoices/:id` — DB-only by default. `?escalate=true` (Phase 4 sends after ~5 polls / 10s) proxies `GET /api/v2/invoices/{lava_invoice_id}` and reconciles.
- **D-26:** Expiry cron `runExpiryDowngrade` every 10 minutes in `internal/scheduler/scheduler.go`. SQL idempotent.

**Public plans endpoint & JWT:**
- **D-27:** `GET /api/v1/plans` (no auth). `?currency=USD|EUR|RUB`, default from `Accept-Language` (RU → RUB else USD). Filters `is_active=TRUE`. Excludes `id`, `lava_offer_id`, `active_user_count`.
- **D-28:** Redis cache `cache:plans:public:{currency}`, TTL 60s. Admin writes bust via `DEL cache:plans:public:*`.
- **D-29:** JWT mint includes `plan_id` (UUID) alongside `tier`. Middleware extracts to `c.Locals("plan_id")`. Old JWTs without claim → middleware falls back to DB read.

**Config additions (D-30):**
- `LAVA_API_KEY` (required for production).
- `LAVA_API_KEY_SANDBOX` (optional; dev/test).
- `LAVA_WEBHOOK_SECRET` (required).
- `LAVA_WEBHOOK_SECRET_PREVIOUS` (optional during rotation).
- `LAVA_WEBHOOK_ALLOWED_CIDRS` (required OR default `158.160.60.174/32` — planner picks).
- `LAVA_SUCCESS_URL` (required, `https://risevpn.com/pay/success`).
- `LAVA_FAIL_URL` (required, `https://risevpn.com/pay/fail`).
- **No** `LAVA_OFFER_PRO_*` env vars (live in `plan_offers.lava_offer_id`).

**Threat model (D-31, D-32, D-33):**
- ASVS **L2** on payment paths (`payment.go`, `webhook_lava.go`, `plans_admin.go`, `admin_lava.go`, `internal/lava/`). ASVS **L1** elsewhere.
- Every PLAN.md gets an inline `<threat_model>` block.
- No webhook rate-limit beyond global per-IP.

### Claude's Discretion (6 entries — planner picks)
1. Wave 5 plan split (`03-10` admin-web UI + `03-11` docs + sandbox test) — may collapse or split further.
2. `LAVA_WEBHOOK_ALLOWED_CIDRS` strict-required vs default `158.160.60.174/32` (recommendation: strict-required).
3. `LAVA_API_KEY` vs `LAVA_API_KEY_SANDBOX` selection logic (recommendation: explicit `LAVA_ENV=sandbox|production` env flag).
4. Invoice polling escalate threshold (D-25 says ~5 polls; planner can adjust).
5. Public `/plans` cache key shape (D-28 default; planner may extend).
6. GORM `OnConflict` clause shape (D-19 + D-18 leave to planner — see §3 below).
7. Admin-web admin-login flow during Phase 3 — confirm planner doesn't pull admin SSO into scope (it's deferred).

### Deferred Ideas (OUT OF SCOPE)
- Lava product auto-creation API (Option C).
- Email reminders on failed recurring payment.
- Force-disconnect on plan-server removal as opt-in admin checkbox.
- Per-user advisory lock between admin actions and webhook (ADMIN-03 — Phase 7).
- Stripe code/dep removal (Phase 8 HARD-01).
- Apple `authorizationCode` exchange (Phase 2 D-18, still deferred).
- Admin SSO (admin password login stays).
- Admin UI sub-pages beyond plan-offer picker (Phase 7).
- PERF-06 `RUN_SCHEDULER` env gate (Phase 6).
- Webhook event log UI + replay button (Phase 7 ADMIN-06).
- KPI dashboard with MRR (Phase 7 ADMIN-01).
- Mid-cycle plan upgrade with proration (PROJECT.md out of scope).
- Email magic-link SSO (IDX-01, v2).
- Multi-region / horizontal scale (v2).
</user_constraints>

<phase_requirements>
## Phase Requirements (PAY-01..PAY-16)

| ID | Description | Research Support |
|----|-------------|------------------|
| PAY-01 | `plans`, `plan_servers`, `plan_offers` tables exist; `users.plan_id` FK; legacy tiers seeded as rows | §8 (GORM models), §9 (migration test harness), §15 row 1 |
| PAY-02 | `POST /api/v1/checkout` accepts `{plan_code, periodicity, currency}` returns `{payment_url, invoice_id}` | §1 (lava `/api/v3/invoice`), §12 (`internal/lava/invoice.go`) |
| PAY-03 | `POST /api/v1/webhook/lava` handles all 5 event types | §1 (webhook payload shapes), §12 (`internal/lava/webhook.go`) |
| PAY-04 | Webhook idempotent — UNIQUE rejects duplicates, handler returns 200 without re-applying | §3 (GORM `OnConflict{DoNothing:true}`), §4 (expression index) |
| PAY-05 | Handler returns 500 on processing errors so lava retries (20-attempt policy) | §3 (transactional handler), §15 row 2 |
| PAY-06 | IP allowlist via Fiber `EnableTrustedProxyCheck` + `TrustedProxies`; never read `X-Forwarded-For` directly | §2 (Fiber v2 trusted-proxy behavior) |
| PAY-07 | `X-Api-Key` uses `crypto/subtle.ConstantTimeCompare` | §12 (`internal/lava/webhook.go` VerifySignature) |
| PAY-08 | Plan tier derived from `offerId` in payload via `plan_offers` lookup, NEVER from client metadata | §1 (lava payload has `contractId` + product/offer), §3 (transactional `SetUserPlan`) |
| PAY-09 | `subscription_expires_at` set from webhook's `period_end` (lava: `subscriptionDetails.expiredAt` / `willExpireAt`) | §1 (`InvoiceResponseV3.subscriptionDetails.expiredAt`) |
| PAY-10 | `POST /api/v1/subscription/cancel` calls `DELETE /api/v1/subscriptions`; user keeps Pro until period end | §1 (lava DELETE endpoint), §12 (`subscription.go`) |
| PAY-11 | Server access enforced at repo layer via `ListServersForPlan` + `IsServerAllowedForPlan`; admins bypass | §8 (plan_repo functions), §5 (Fiber middleware ordering — admin bypass) |
| PAY-12 | `GET /api/v1/plans` (public, no auth) returns active plans with offers in caller's currency | §6 (Redis cache pattern) |
| PAY-13 | Admin CRUD plans via `GET/POST/PATCH/DELETE /api/v1/admin/plans`; refuses `is_system=true` soft-delete | §5 (admin route group inheritance), §10 (admin-web UI) |
| PAY-14 | Admin manages plan-servers via `PUT/POST/DELETE /admin/plans/:id/servers/...` | §10 (PlanServersPicker) |
| PAY-15 | Admin manages offers + price versioning via `.../offers/replace` | §10 (PlanOffersGrid + ReplaceOfferDialog), §3 (GORM tx for replace) |
| PAY-16 | Lava client lives in `internal/lava/`, hardcoded base URL, 5s timeout, no SSRF surface | §12 (package layout), §13 (D-15 verification) |
</phase_requirements>

## Project Constraints (from CLAUDE.md)

| Constraint | Source | Impact on Phase 3 |
|------------|--------|-------------------|
| Backend stack locked: Go 1.25 + Fiber v2 + GORM + Postgres 16 + Redis 7 | CLAUDE.md | All code uses existing stack; `go.mod` is at 1.25.0 (verified) |
| Mobile stack locked: RN 0.84, TypeScript, Zustand, axios | CLAUDE.md | N/A — Phase 3 backend + admin-web only |
| Admin-web stack: Vite + React 19 + TanStack Query + shadcn/ui | CLAUDE.md | §10 follows established patterns from `Servers.tsx` |
| Payment provider: lava.top exclusively | CLAUDE.md | No alternative provider research |
| App-store compliance: No IAP buttons in mobile app | CLAUDE.md | N/A this phase |
| Single VM Docker Compose for v2.2.0 | CLAUDE.md | Expiry cron runs in every replica is acceptable (Phase 6 adds PERF-06 gate) |
| Webhook reliability: lava.top retries 20×; handler MUST be idempotent and return 500 on processing error | CLAUDE.md | PAY-04 + PAY-05 (D-18, D-19) |
| Lava.top constraints: 8% commission, $5/€5 minimum, payment URL TTL ~24h, contracts identified by UUID | CLAUDE.md | Admin UI shows warning when offer amount < $5/€5 (per ADR §19.13 Tab 3) |
| GSD workflow enforcement: every change through GSD | CLAUDE.md | Every plan in Phase 3 lands via `/gsd-execute-phase` |
| Security audit findings classified Critical/High MUST land before any paying user | CLAUDE.md + docs/audit/SECURITY-AUDIT.md | **Phase 3 IS the launch security gate** — ASVS L2 on payment paths (D-31) |

## Executive Summary

Phase 3 is fully scoped by 32 locked decisions in CONTEXT.md. The planner's job is mechanical translation, not design — but five technical specifics need to be researched before plan-writing so PLAN.md actions are precise:

1. **lava.top webhook field names are not what ADR-007 implies.** The OpenAPI spec uses `contractId` (consistent) but the timestamp lives at the top of the payload as `timestamp` (ISO 8601 string, NOT number — so `(payload->>'timestamp')::text` cast in D-10 is redundant but harmless). The "subscription period end" is `subscriptionDetails.expiredAt` on the invoice-detail endpoint and `willExpireAt` on the `subscription.cancelled` webhook. Recurring renewal events carry `parentContractId` — the planner should reuse this as the `lava_contracts.parent_contract_id` foreign key.

2. **Fiber v2 `EnableTrustedProxyCheck` does NOT reject untrusted requests** — it silently ignores their proxy headers and falls back to `RemoteIP()`. To enforce hard IP rejection (success criterion #4: "rejected at the Fiber TrustedProxies layer"), Phase 3 needs a **route-scoped middleware** that calls `c.Context().RemoteIP()` and compares against the parsed CIDR list, returning 403 on mismatch. The Fiber config alone is insufficient for the stated goal — this is a critical gap the planner must close with a dedicated `LavaWebhookIPAllowlist` middleware.

3. **GORM v2 `OnConflict{DoNothing:true}` returns `RowsAffected=0` on conflict on PostgreSQL** — the planner can use this to distinguish "first delivery, do the work" from "duplicate, skip side effects". Combined with `db.Transaction(func(tx *gorm.DB) error { ... })`, the entire webhook handler is one TX: insert event log row → on `RowsAffected==0` return 200 immediately → otherwise process side effects → return 500 (lava retries) on any error.

4. **`PlanLimits` is referenced in 5 source files** (`servers.go:114`, `health.go:45,55`, `connection.go:97,100`, `admin.go:137`, `model/subscription.go:29`) PLUS 4 test files that mention `stripe_id` in their test-local `CREATE TABLE` DDL strings. The test files (`payment_test.go`, `admin_test.go`, `auth_test.go`, `subscription_repo_test.go`) build their own SQLite test schema fresh per test — dropping `subscriptions.stripe_id` in migration 020 does NOT break them. D-11 is safe.

5. **Admin-web has 8 shadcn/ui components installed** (`badge, button, card, dialog, dropdown-menu, input, label, separator, skeleton, table`). For Phase 3 the planner MUST add: `Form` (for PlanForm validation), `Select`/`Combobox` (for lava offer dropdown picker per D-12), `Checkbox` (for PlanServersPicker), `Switch` (for is_active toggle), `Tabs` (for PlanDetail's three tabs), `Tooltip` (for warnings), and `Textarea` (for description). The planner should treat shadcn component install as a Wave 5 Wave 0 prerequisite.

**Primary recommendation:** Write Phase 3 as 9 plans across 5 waves per D-04, but split Wave 5 into `03-10` (admin-web shadcn-install + plans API client + Plans table page), `03-11` (PlanDetail with all three tabs + ReplaceOfferDialog), and `03-12` (API contract doc + sandbox integration test + `grep -r PlanLimits` smoke). The admin-web work is meatier than D-04 acknowledges because of the dropdown picker + price-versioning modal.

---

## 1. lava.top API surface

**Confidence:** HIGH for endpoint paths, request/response shapes, webhook event types. MEDIUM for the exact JSON field name of `subscriptionDetails.expiredAt` on the webhook payloads (the OpenAPI spec only enumerates the `InvoiceResponseV3` shape for `GET /api/v2/invoices/{id}`; the webhook payload shapes are described but field-name overlap with the invoice DTO is not 100% documented). **Recommendation: planner adds a one-shot sandbox probe as part of Wave 1 (03-02 lava client) — record the first 5 webhook event payloads and pin the DTO struct tags from real data.**

### 1.1 `POST /api/v3/invoice` (CreateInvoice)

**Request body** (`CreateInvoiceV3Request`):
```go
type CreateInvoiceRequest struct {
    Email           string         `json:"email"`                     // required, taken from users.email
    OfferID         string         `json:"offerId"`                   // required, uuid — from plan_offers.lava_offer_id
    Currency        string         `json:"currency"`                  // required, RUB|USD|EUR
    Periodicity     string         `json:"periodicity,omitempty"`     // optional: ONE_TIME|MONTHLY|PERIOD_90_DAYS|PERIOD_180_DAYS|PERIOD_YEAR
    BuyerLanguage   string         `json:"buyerLanguage,omitempty"`   // optional: EN|RU|ES
    PaymentProvider string         `json:"paymentProvider,omitempty"` // optional: SMART_GLOCAL|UNLIMINT|PAYPAL|STRIPE|PAY2ME
    PaymentMethod   string         `json:"paymentMethod,omitempty"`   // optional: CARD|SBP|PAYPAL|STRIPE|PIX|APPLE_PAY
    ClientUtm       map[string]string `json:"clientUtm,omitempty"`    // optional, pass-through UTM params
    Amount          *float64       `json:"amount,omitempty"`          // optional, for dynamic pricing — leave unset for offer-driven price
}
```

**Response 200** (`InvoicePaymentParamsResponse`):
```go
type InvoiceResponse struct {
    ID         string `json:"id"`         // uuid — the invoice/contract identifier (NOT renamed lavaInvoiceID — just `id`)
    Status     string `json:"status"`     // new|in-progress|completed|failed|cancelled|subscription-active|subscription-expired|subscription-cancelled|subscription-failed
    AmountTotal struct {
        Amount   float64 `json:"amount"`
        Currency string  `json:"currency"`
    } `json:"amountTotal"`
    PaymentURL *string `json:"paymentUrl"` // nullable — "Ссылка на виджет оплаты продукта"
}
```

**Mapping to invoices table:**
- `invoices.lava_invoice_id` ← `id`
- `invoices.payment_url` ← `paymentUrl`
- `invoices.status` ← map: `new`/`in-progress` → `pending`; `completed` → `paid`; `failed` → `failed`; `cancelled`/`subscription-cancelled`/`subscription-expired`/`subscription-failed` → `cancelled` or `failed` (planner decides; recommendation: keep DB enum to `pending|paid|failed|cancelled` and map; lava's status values map cleanly).
- `invoices.amount` ← `amountTotal.amount`
- `invoices.currency` ← `amountTotal.currency`

**Notes:**
- Response does NOT contain `contractId`. The `id` IS the invoice/contract identifier from lava's perspective. `contractId` arrives on webhook events.
- `paymentUrl` TTL ~24h per CLAUDE.md → the `409 Conflict {"error":"offer_not_configured"}` (D-09) and 60s checkout-idempotency window (ADR §9.2) are the relevant client-side guards. A stale 24h+ URL is not the planner's concern.

### 1.2 `GET /api/v2/invoices/{id}` (GetInvoice — escalate path D-25)

**Response 200** (`InvoiceResponseV3`):
```go
type InvoiceDetailResponse struct {
    ID                  string    `json:"id"`
    Type                string    `json:"type"`        // INVOICE | SUBSCRIPTION_FIRST_INVOICE | SUBSCRIPTION_RENEWAL
    Datetime            string    `json:"datetime"`    // ISO 8601
    Status              string    `json:"status"`      // NEW | IN_PROGRESS | COMPLETED | FAILED  (NOTE: caps differ from POST /invoice — confirm in sandbox)
    Receipt struct {
        Amount   float64 `json:"amount"`
        Currency string  `json:"currency"`
        Fee      float64 `json:"fee"`
    } `json:"receipt"`
    Buyer struct {
        Email    string `json:"email"`
        CardMask string `json:"cardMask"`
    } `json:"buyer"`
    Product struct {
        Name  string `json:"name"`
        Offer string `json:"offer"`  // offer name as a string, NOT the offer UUID
    } `json:"product"`
    ParentInvoice *struct {
        ID string `json:"id"`
    } `json:"parentInvoice,omitempty"`
    SubscriptionStatus *string `json:"subscriptionStatus,omitempty"` // ACTIVE | CANCELLED | FAILED
    SubscriptionDetails *struct {
        ExpiredAt    *string `json:"expiredAt"`    // ISO 8601 — POPULATES subscription_expires_at (PAY-09)
        TerminatedAt *string `json:"terminatedAt"`
        CancelledAt  *string `json:"cancelledAt"`
    } `json:"subscriptionDetails,omitempty"`
    ClientUtm map[string]*string `json:"clientUtm,omitempty"`
}
```

**Critical for PAY-09:** `subscriptionDetails.expiredAt` is the "period end" date. The webhook for `payment.success` does NOT include this field directly — it must be fetched via `GET /api/v2/invoices/{id}` OR computed by the handler from `started_at + periodicity` math. **Recommendation: compute locally from `lava_contracts.started_at + periodicity` for the FIRST `payment.success` (faster, no extra API call), and on `subscription.recurring.payment.success` advance by one period. The escalate path D-25 uses this endpoint only when DB still shows `pending` after polling.**

### 1.3 `GET /api/v2/products` (ListProducts — admin dropdown D-12)

**Response 200:**
```go
type ProductsResponse struct {
    Items []struct {
        Type string                 `json:"type"`     // POST | PRODUCT
        Data ProductItemResponse    `json:"data"`     // when type=PRODUCT
    } `json:"items"`
    NextPage *string `json:"nextPage,omitempty"` // cursor for pagination
}

type ProductItemResponse struct {
    ID          string  `json:"id"`           // product UUID
    Title       *string `json:"title"`
    Description *string `json:"description"`
    Type        string  `json:"type"`         // COURSE | DIGITAL_PRODUCT | BOOK | GUIDE | SUBSCRIPTION | AUDIO | MODS | CONSULTATION
    Offers      []struct {
        ID          string  `json:"id"`           // offer UUID — THIS is plan_offers.lava_offer_id
        Name        string  `json:"name"`
        Description *string `json:"description"`
        Prices      []struct {
            Amount      float64 `json:"amount"`
            Currency    string  `json:"currency"`
            Periodicity string  `json:"periodicity"`
        } `json:"prices"`
        Recurrent bool `json:"recurrent"`  // deprecated
    } `json:"offers"`
}
```

**For the admin dropdown (D-12 + D-13):** the proxy endpoint `GET /api/v1/admin/lava/products` normalizes this into a flat array:
```json
[
  { "productId": "...", "productName": "Pro", "offerId": "uuid1", "offerName": "Monthly USD",
    "periodicity": "MONTHLY", "currency": "USD", "amount": 5.00 },
  { "productId": "...", "productName": "Pro", "offerId": "uuid2", "offerName": "Yearly USD",
    "periodicity": "PERIOD_YEAR", "currency": "USD", "amount": 49.99 },
  ...
]
```

**Pagination:** if `nextPage` is set, the proxy MUST follow it server-side and concatenate. The dropdown should never paginate (admin UX). For a single-product (Pro) lava catalog this is non-issue, but the proxy should still drain the cursor before responding.

### 1.4 `DELETE /api/v1/subscriptions` (CancelSubscription)

**Query parameters (BOTH required):**
- `contractId` — uuid, the parent contract ID
- `email` — the user's email (taken from `users.email`)

**Response:** 200 on success.

**Implementation note:** the query parameters carry the secrets — `crypto/subtle.ConstantTimeCompare` is not relevant for outbound. The base URL is the hardcoded `https://gate.lava.top` (D-15). The 5s context timeout (D-14) applies.

### 1.5 Webhook payloads — all 5 event types

**Common envelope fields (present on all events EXCEPT `subscription.cancelled`):**

```go
type WebhookEvent struct {
    EventType    string `json:"eventType"`    // payment.success | payment.failed | subscription.recurring.payment.success | subscription.recurring.payment.failed | subscription.cancelled
    ContractID   string `json:"contractId"`   // uuid — the lava-side contract id
    Amount       float64 `json:"amount"`      // omitted on subscription.cancelled
    Currency     string  `json:"currency"`    // omitted on subscription.cancelled
    Timestamp    string  `json:"timestamp"`   // ISO 8601 date-time STRING — NOT a unix int
    Status       string  `json:"status"`      // completed | failed | subscription-active | subscription-failed
    ErrorMessage string  `json:"errorMessage"`// empty on success, populated on failure
    Product struct {
        ID    string `json:"id"`
        Title string `json:"title"`
    } `json:"product"`
    Buyer struct {
        Email string `json:"email"`
    } `json:"buyer"`
}
```

**Per-event additions:**

| Event | Extra Fields | Handler Action (D-19) |
|-------|--------------|------------------------|
| `payment.success` | (none — base envelope) | Mark invoice paid; upsert `lava_contracts`; `SetUserPlan(user, plan)` transactional |
| `payment.failed` | `errorMessage` populated | Mark invoice failed; no tier change |
| `subscription.recurring.payment.success` | `parentContractId string` (uuid) — links the renewal invoice to the original | Extend `subscriptions.expires_at` + `lava_contracts.expires_at` by one period |
| `subscription.recurring.payment.failed` | `parentContractId string`; `errorMessage` populated | Set `subscriptions.is_active=false` and `lava_contracts.is_active=false` immediately. Tier waits for cron. |
| `subscription.cancelled` | `cancelledAt string` (ISO 8601); `willExpireAt string` (ISO 8601) — **NO `amount`, `currency`, `timestamp`, `errorMessage`** | Set `lava_contracts.cancelled_at=now()`, `is_active=false`. Tier untouched until cron. |

**`offerId` resolution (PAY-08):** the webhook payload does NOT carry `offerId` directly — it carries `product.id` and `contractId`. The `offerId` must be resolved by reverse-looking-up `invoices.lava_invoice_id` joined to the contractId (since `invoices.plan_offer_id` was populated on `/checkout`). **PAY-08 wording is satisfied because the resolution chain is: `webhook.contractId → invoices (joined by lava_invoice_id) → invoices.plan_offer_id → plan_offers.plan_id`. The webhook handler NEVER reads anything tier-bearing from the request body — only the contractId is used as a key.**

**Critical caveat for the planner:** for `subscription.recurring.payment.success`, the `contractId` is the RENEWAL contract id, and `parentContractId` is the ORIGINAL. The `invoices` row we wrote on `/checkout` matches the original; we need to look up `lava_contracts.contract_id = parentContractId` to find the right user. The D-10 UNIQUE on `(event_type, contract_id, (payload->>'timestamp'))` uses the `contractId` from the payload — different per renewal — so two renewals on the same parent contract are distinct rows. 

**`subscription.cancelled` has NO `timestamp` field per the OpenAPI shape.** This breaks the D-10 idempotency UNIQUE unless we either:
- Use `cancelledAt` in place of `timestamp` for cancellation events: `UNIQUE (event_type, contract_id, COALESCE((payload->>'timestamp'), (payload->>'cancelledAt')))`
- Or accept that cancellation events fall through the idempotency net (they're sent once per cancellation, not retried — lava docs don't promise this).

**Recommendation:** planner adds a fallback expression in migration 020: `UNIQUE (event_type, contract_id, COALESCE((payload->>'timestamp')::text, (payload->>'cancelledAt')::text))`. Both `timestamp` and `cancelledAt` are ISO 8601 strings; `->>'` produces text in both cases.

### 1.6 Sandbox details (D-06)

The OpenAPI spec at `gate.lava.top/docs/documentation.yaml` makes NO mention of a separate sandbox base URL. **Per available public docs, lava.top uses the same base URL `https://gate.lava.top` for production and sandbox; the distinction is by API key only.** Test transactions in sandbox cost actual money but at small denominations (planner should confirm with the operator).

**Recommendation for D-30 sub-choice (LAVA_API_KEY vs LAVA_API_KEY_SANDBOX selection):** explicit `LAVA_ENV=sandbox|production` env (defaults to `production` if unset). When `sandbox`, the client uses `LAVA_API_KEY_SANDBOX` and refuses to start if it's empty. When `production`, the client uses `LAVA_API_KEY` and refuses to start if it's empty. This matches the HOTFIX-08 fail-fast pattern.

---

## 2. Fiber TrustedProxies + IP allowlist

**Confidence:** HIGH (verified directly from fiber/v2 source `app.go` doc comments).

### 2.1 What Fiber v2's `EnableTrustedProxyCheck` actually does

From Fiber v2 source documentation:

> "But if request ip NOT in Trusted Proxies whitelist then:
> 1. `c.Protocol()` WON't get value from X-Forwarded-Proto, X-Forwarded-Protocol, X-Forwarded-Ssl or X-Url-Scheme header...
> 2. `c.IP()` WON'T get value from ProxyHeader header, will return RemoteIP() from fasthttp context
> 3. `c.Hostname()` WON'T get value from X-Forwarded-Host header..."

**The request is NOT rejected.** Fiber silently ignores spoofed headers and falls back to the TCP-connection IP. This is a security-positive default for proxy-header-trust questions, but it does NOT satisfy success criterion #4 (PAY-06's spirit) which says "rejected at the Fiber `TrustedProxies` layer".

### 2.2 Required architecture for PAY-06

The webhook handler needs HARD rejection of out-of-allowlist IPs (not just header-ignoring). The planner must wire two layers:

**Layer 1 — App-level config (one-time, in `cmd/main.go`):**
```go
// Parse LAVA_WEBHOOK_ALLOWED_CIDRS into a []string for Fiber's TrustedProxies.
// Fiber v2 accepts both individual IPs (e.g. "158.160.60.174") and CIDR ranges (e.g. "158.160.60.0/24") as strings.
trustedProxies := strings.Split(cfg.LavaWebhookAllowedCIDRs, ",")
app := fiber.New(fiber.Config{
    AppName:                 "VPN API Server",
    EnableTrustedProxyCheck: true,
    TrustedProxies:          trustedProxies,
    ErrorHandler:            handler.ErrorHandler(logger),
})
```

This makes Fiber ignore `X-Forwarded-For` from non-allowlisted IPs everywhere, so `c.IP()` is always the TCP RemoteIP for non-allowlist callers — defending the rest of the app from header spoofing.

**Layer 2 — Route-scoped middleware (rejects, doesn't just ignore):**
```go
// internal/middleware/lava_ip_allowlist.go (NEW)
// Returns a Fiber middleware that 403s any request whose RemoteIP (NOT c.IP() —
// the TCP-connection IP, immune to forwarded-for spoofing) is outside the parsed
// CIDR list. Mounted ONLY on the /webhook/lava route, not globally.
func LavaWebhookIPAllowlist(cidrs []string, logger *zap.Logger) (fiber.Handler, error) {
    nets := make([]*net.IPNet, 0, len(cidrs))
    for _, s := range cidrs {
        s = strings.TrimSpace(s)
        // Handle bare IPs by converting to /32 (v4) or /128 (v6) CIDR.
        if !strings.Contains(s, "/") {
            if strings.Contains(s, ":") {
                s += "/128"
            } else {
                s += "/32"
            }
        }
        _, ipNet, err := net.ParseCIDR(s)
        if err != nil {
            return nil, fmt.Errorf("LavaWebhookIPAllowlist: parse %q: %w", s, err)
        }
        nets = append(nets, ipNet)
    }
    return func(c *fiber.Ctx) error {
        remote := c.Context().RemoteIP() // TCP-layer IP, NOT c.IP() — c.IP() can be influenced by TrustedProxies
        for _, n := range nets {
            if n.Contains(remote) {
                return c.Next()
            }
        }
        logger.Warn("lava webhook: IP allowlist reject",
            zap.String("remote_ip", remote.String()),
            zap.String("path", c.Path()))
        return c.SendStatus(fiber.StatusForbidden)
    }, nil
}
```

**Route mount:**
```go
ipAllowlist, err := middleware.LavaWebhookIPAllowlist(strings.Split(cfg.LavaWebhookAllowedCIDRs, ","), logger)
if err != nil { logger.Fatal("lava webhook ip allowlist init", zap.Error(err)) }
api.Post("/webhook/lava", ipAllowlist, handler.HandleLavaWebhook(logger, cfg, db, redisClient))
```

**CIDR parsing in Go:** `net.ParseCIDR("158.160.60.174/32")` works directly. Bare IPs need normalization to `/32` (v4) or `/128` (v6) — handled above.

### 2.3 Verification

**Test approach:** spin up a `fasthttp.Server` in a test, set RemoteAddr via the fasthttp ctx, assert middleware returns 403 for non-allowlist IPs and `c.Next()` for allowlist IPs. Fiber's testing utilities (`app.Test(req)`) don't easily let you spoof the TCP RemoteIP — testcontainers or a real TCP loopback test may be needed. **Recommendation: planner uses `httptest.NewServer` with a stdlib `http.Handler` shim around Fiber, or just tests `LavaWebhookIPAllowlist` as a pure function on a fake `fiber.Ctx` (build a minimal mock).**

### 2.4 `c.IP()` vs `c.Context().RemoteIP()` — which to use

| When | Use | Why |
|------|-----|-----|
| Logging request source for non-webhook routes | `c.IP()` | Honors `TrustedProxies` config — gives the real client IP when behind a trusted proxy, the TCP IP otherwise |
| The webhook IP allowlist | `c.Context().RemoteIP()` | Bypasses ALL proxy-header logic — the raw TCP source. Immune to spoofing via header injection even if config is misconfigured. |
| Rate-limiter keys (current `middleware/ratelimit.go`) | `c.IP()` | Already in place; Phase 1 audit cleared this. Webhook is exempt from global rate-limit per D-20. |

---

## 3. GORM transactional patterns

**Confidence:** HIGH (verified from GORM v2 docs + GitHub issues for `OnConflict{DoNothing:true}` PostgreSQL behavior + existing repo patterns in `internal/repository/`).

### 3.1 `lava_webhook_events` insert (idempotency detection — PAY-04)

```go
import (
    "gorm.io/gorm"
    "gorm.io/gorm/clause"
)

type LavaWebhookEvent struct {
    ID          string    `gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
    EventType   string    `gorm:"type:varchar(64);not null"`
    ContractID  *string   `gorm:"type:varchar(64)"`
    InvoiceID   *string   `gorm:"type:varchar(64)"`
    Payload     datatypes.JSON `gorm:"type:jsonb;not null"`
    ReceivedAt  time.Time `gorm:"autoCreateTime"`
    ProcessedAt *time.Time
    Error       *string   `gorm:"type:text"`
}

// Insert with DoNothing-on-conflict. If RowsAffected==0, it's a duplicate.
func InsertWebhookEventIfNew(db *gorm.DB, event *LavaWebhookEvent) (isNew bool, err error) {
    result := db.Clauses(clause.OnConflict{DoNothing: true}).Create(event)
    if result.Error != nil {
        return false, result.Error
    }
    return result.RowsAffected > 0, nil
}
```

**Behavior on PostgreSQL:** GORM emits `INSERT ... ON CONFLICT DO NOTHING`. Postgres returns 0 rows affected on conflict. **This is what we want — distinguishes first delivery from retry without raising an error.**

⚠️ **Caveat noted in GORM issue #6554:** on MySQL, `DoNothing:true` generates `ON DUPLICATE KEY UPDATE id=id` instead of true no-op. We're on **PostgreSQL** (locked stack), so this caveat doesn't apply. The planner should still write a test asserting `RowsAffected == 0` on duplicate insert to lock the contract.

### 3.2 `lava_contracts` upsert

```go
func UpsertLavaContract(db *gorm.DB, c *LavaContract) error {
    return db.Clauses(clause.OnConflict{
        Columns: []clause.Column{{Name: "contract_id"}}, // matches UNIQUE on lava_contracts.contract_id
        DoUpdates: clause.AssignmentColumns([]string{
            "is_active",
            "expires_at",
            "cancelled_at",
            "parent_contract_id", // only set on renewal events
        }),
    }).Create(c).Error
}
```

**Notes:**
- The `Columns` field references the UNIQUE constraint columns (set by the migration: `UNIQUE (contract_id)`).
- `AssignmentColumns` lists fields to update on conflict — only the mutable lifecycle fields. `user_id`, `offer_id`, `plan`, `periodicity`, `currency`, `started_at` are write-once.
- For PostgreSQL, this generates `INSERT ... ON CONFLICT (contract_id) DO UPDATE SET is_active=EXCLUDED.is_active, expires_at=EXCLUDED.expires_at, cancelled_at=EXCLUDED.cancelled_at, parent_contract_id=EXCLUDED.parent_contract_id`.

### 3.3 `SetUserPlan` — transactional update of `users` + `subscriptions`

Per D-19 `payment.success` handling and ADR §19.4 (denormalized `subscription_tier` kept):

```go
// SetUserPlan updates users.plan_id, users.subscription_tier, and the active
// subscriptions row, all in one transaction. Failing any one rolls back all.
//
// Called from the webhook handler's payment.success branch (D-19).
func SetUserPlan(db *gorm.DB, userID, planID, lavaContractID string, expiresAt time.Time) error {
    return db.Transaction(func(tx *gorm.DB) error {
        // 1. Resolve the plan code (for the denormalized subscription_tier write).
        var plan model.Plan
        if err := tx.First(&plan, "id = ?", planID).Error; err != nil {
            return fmt.Errorf("SetUserPlan: find plan: %w", err)
        }

        // 2. Update users.plan_id + users.subscription_tier in one write.
        if err := tx.Model(&model.User{}).Where("id = ?", userID).Updates(map[string]interface{}{
            "plan_id":           planID,
            "subscription_tier": plan.Code,
            "subscription_expires_at": expiresAt,
        }).Error; err != nil {
            return fmt.Errorf("SetUserPlan: update user: %w", err)
        }

        // 3. Upsert the subscriptions row.
        sub := model.Subscription{
            UserID:         userID,
            Plan:           plan.Code,
            LavaContractID: lavaContractID,
            IsActive:       true,
            ExpiresAt:      &expiresAt,
        }
        if err := tx.Clauses(clause.OnConflict{
            Columns: []clause.Column{{Name: "user_id"}, {Name: "is_active"}}, // partial unique on (user_id) WHERE is_active=true
            DoUpdates: clause.AssignmentColumns([]string{"plan", "lava_contract_id", "expires_at"}),
        }).Create(&sub).Error; err != nil {
            return fmt.Errorf("SetUserPlan: upsert subscription: %w", err)
        }

        return nil
    })
}
```

**Caveat — partial UNIQUE on subscriptions:** the existing `subscriptions` table has no UNIQUE constraint on `user_id` (multiple historical rows are allowed; `FindSubscriptionByUserID` returns the most recent active one). The planner has two choices:

- **(a) Keep current shape, do a manual find+update vs insert** instead of OnConflict (matches existing `CreateOrUpdateSubscription` in `subscription_repo.go:48-68`).
- **(b) Add a partial unique index** `CREATE UNIQUE INDEX subscriptions_user_active ON subscriptions(user_id) WHERE is_active=true` in migration 020 alongside `lava_contract_id`, enabling clause.OnConflict above.

**Recommendation: option (a)** — minimizes migration surface and matches the existing `subscription_repo.go` pattern. Planner-discretion call.

### 3.4 Idempotent webhook handler skeleton (combines 3.1 + 3.3)

```go
func HandleLavaWebhook(logger, cfg, db) fiber.Handler {
    return func(c *fiber.Ctx) error {
        // Step 1: X-Api-Key check (PAY-07).
        apiKey := c.Get("X-Api-Key")
        if !ctConstantTimeCompareEitherSecret(apiKey, cfg.LavaWebhookSecret, cfg.LavaWebhookSecretPrevious) {
            return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "invalid api key"})
        }

        // Step 2: parse payload (lava.WebhookEvent DTO from §1.5).
        var event lava.WebhookEvent
        if err := c.BodyParser(&event); err != nil {
            return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid payload"})
        }

        // Step 3: idempotency UPSERT — record event + detect duplicate.
        rawBody := c.Body() // for the jsonb payload column
        rec := &model.LavaWebhookEvent{
            EventType:  event.EventType,
            ContractID: &event.ContractID,
            Payload:    datatypes.JSON(rawBody),
        }
        isNew, err := repository.InsertWebhookEventIfNew(db, rec)
        if err != nil {
            logger.Error("webhook: idempotency insert failed", zap.Error(err))
            return c.SendStatus(fiber.StatusInternalServerError) // lava retries (PAY-05)
        }
        if !isNew {
            // Duplicate — return 200 without re-applying (PAY-04).
            return c.SendStatus(fiber.StatusOK)
        }

        // Step 4: dispatch on eventType. Each branch is its own tx.
        switch event.EventType {
        case "payment.success":
            err = handlePaymentSuccess(db, event, rawBody)
        case "subscription.recurring.payment.success":
            err = handleRecurringSuccess(db, event)
        case "payment.failed":
            err = handlePaymentFailed(db, event)
        case "subscription.recurring.payment.failed":
            err = handleRecurringFailed(db, event)
        case "subscription.cancelled":
            err = handleSubscriptionCancelled(db, event)
        default:
            logger.Warn("webhook: unknown event type", zap.String("type", event.EventType))
            // Still record processed_at — we received it, we just don't act on it.
        }

        // Step 5: mark processed or store error.
        now := time.Now()
        if err != nil {
            errStr := err.Error()
            db.Model(&model.LavaWebhookEvent{}).Where("id = ?", rec.ID).Updates(map[string]interface{}{
                "error": errStr,
            })
            logger.Error("webhook: processing failed", zap.String("event_type", event.EventType), zap.Error(err))
            return c.SendStatus(fiber.StatusInternalServerError) // lava retries
        }
        db.Model(&model.LavaWebhookEvent{}).Where("id = ?", rec.ID).Update("processed_at", &now)
        return c.SendStatus(fiber.StatusOK)
    }
}
```

**Why one-tx-per-event-type** instead of one big tx for all of Step 3+4: GORM's `db.Transaction` rolls back on returned-error. Wrapping Steps 3 and 4 in one tx would mean a Step-4 failure rolls back the Step-3 idempotency record, which then allows the retry to bypass dedup. **Steps 3 and 4 MUST be in separate transactions** — Step 3 commits the dedup record always, Step 4 fails open (500 → lava retries) but leaves the event recorded so forensics can trace the retry.

---

## 4. Postgres expression index for idempotency

**Confidence:** HIGH (Postgres `->>` semantics are stable; expression indexes are a 20-year-old feature).

### 4.1 Syntax

`(payload->>'timestamp')` returns the JSON value as **text**, regardless of whether the underlying value is a JSON string, number, or boolean. Per PostgreSQL docs:

- `payload->'timestamp'` returns the value as JSONB (with quotes if string, etc.).
- `payload->>'timestamp'` returns the value as text (string unwrapped; numbers stringified).

So `(payload->>'timestamp')` is always-text. No `::text` cast needed, though `(payload->>'timestamp')::text` is a no-op and harmless (D-10 includes the cast — fine to keep).

### 4.2 In `CREATE TABLE` vs separate `CREATE INDEX`

Postgres allows expression UNIQUE constraints directly inside `CREATE TABLE` via the table constraint syntax — but you cannot use the inline `UNIQUE` per-column shorthand for expressions. You must use either:

```sql
-- Option A: separate CREATE INDEX after the table
CREATE TABLE lava_webhook_events (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type   VARCHAR(64) NOT NULL,
    contract_id  VARCHAR(64),
    invoice_id   VARCHAR(64),
    payload      JSONB NOT NULL,
    received_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at TIMESTAMPTZ,
    error        TEXT
);

CREATE UNIQUE INDEX lava_webhook_events_natural_key
    ON lava_webhook_events (
        event_type,
        contract_id,
        COALESCE((payload->>'timestamp'), (payload->>'cancelledAt'))
    );
```

**Recommendation:** Option A — separate `CREATE UNIQUE INDEX` after the table. This is what ADR §8.3 already does (`UNIQUE (event_type, contract_id, (payload->>'timestamp'))` in inline syntax is **wrong** for expression columns; the planner must rewrite it as a separate `CREATE UNIQUE INDEX`). Use `COALESCE` per §1.5 caveat (cancellation events have no `timestamp`, only `cancelledAt`).

### 4.3 Verification test approach

In a Postgres testcontainer or test DB:
```go
// Insert event with fixed timestamp twice — second insert must fail with
// pq error code 23505 (unique_violation) OR (if using OnConflict{DoNothing})
// RowsAffected must be 0 on second call.
event := &model.LavaWebhookEvent{
    EventType:  "payment.success",
    ContractID: ptr("contract-123"),
    Payload:    json.RawMessage(`{"timestamp":"2026-05-23T10:00:00Z","contractId":"contract-123"}`),
}
isNew1, err := InsertWebhookEventIfNew(db, event)
assert.NoError(t, err)
assert.True(t, isNew1) // first insert succeeds

// Re-insert with same payload-timestamp
event2 := *event
event2.ID = "" // GORM regenerates UUID, but the natural key collides
isNew2, err := InsertWebhookEventIfNew(db, &event2)
assert.NoError(t, err)
assert.False(t, isNew2) // ON CONFLICT DO NOTHING → RowsAffected=0
```

**Caveat:** if the planner uses testcontainers with Postgres 16 (matches CLAUDE.md locked version), the test passes. If they use SQLite for unit tests (existing pattern in `internal/handler/*_test.go`), **SQLite does NOT support `->>'` expression indexes the same way** — the JSON1 extension is available but expression-index syntax differs. **Recommendation: migration test for migrations 019/020 MUST run against Postgres (testcontainers), not SQLite.** Existing repository tests can stay on SQLite for non-JSON paths.

---

## 5. Fiber route registration + middleware ordering

**Confidence:** HIGH (verified directly from `cmd/main.go` current source).

### 5.1 Current route registration order in `cmd/main.go`

Lines 170-281 (after global middleware: requestid, recover, CORS, AppVersion, RateLimit):

```
api := app.Group("/api/v1")

// PUBLIC ROUTES (no auth)
api.Post("/auth/refresh", ...)          // line 174
api.Post("/auth/guest", ...)            // line 175
api.Post("/auth/admin-login", ...)      // line 176
api.Post("/auth/apple", ...)            // line 180 (Phase 2)
api.Post("/auth/google", ...)           // line 181
api.Post("/auth/link", linkLimiter, ...) // line 188
api.Get("/health", ...)                  // line 192
api.Post("/webhook/stripe", ...)         // line 195  *** REMOVE D-02 ***
api.Post("/debug/error", ...)            // line 203

// PROTECTED ROUTES (authMiddleware = AuthRequired)
authMiddleware := middleware.AuthRequired(cfg.JWTSecret, redisClient, db)
protected := api.Group("", authMiddleware)
protected.Post("/auth/logout", ...)                        // line 227
protected.Get("/servers", ...)                             // line 228 — AMEND for plan_id branch
protected.Get("/servers/:id/config", ...)                  // line 229 — AMEND for IsServerAllowedForPlan
protected.Get("/subscription", ...)                        // line 230
protected.Get("/account", ...)
protected.Patch("/account", ...)
protected.Post("/connections", ...)                        // AMEND — read plan_id, not tier
... (rest)
protected.Post("/subscription/checkout", ...)              // line 237  *** REMOVE D-02, replace with /checkout ***
protected.Post("/subscription/cancel", ...)                // line 238  *** AMEND — call lava client ***

// ADMIN ROUTES (authMiddleware + AdminRequired + AuditLog)
admin := api.Group("/admin",
    authMiddleware,
    middleware.AdminRequired(db),
    middleware.AuditLog(db, logger),
)
admin.Get("/users", ...)                  // line 264
admin.Get("/users/:id", ...)
... (rest)
```

### 5.2 New routes — where each mounts

**Public (no auth):**
```go
api.Post("/checkout", handler.CreateCheckoutSession(logger, cfg, db, lavaClient))   // BUT — wait, /checkout is AUTH'd per ADR §10.3
api.Get("/plans", handler.ListPlansPublic(logger, db, redisClient))                 // D-27 PUBLIC, no auth
api.Post("/webhook/lava", ipAllowlistMiddleware, handler.HandleLavaWebhook(logger, cfg, db, redisClient))
                                                                                     // route-scoped IP allowlist (§2.2)
```

Correction: `/checkout` is AUTHENTICATED per ADR §10.3 (`{"plan_code, periodicity, currency}` body, must be a signed-in SSO user). So:

**Protected:**
```go
protected.Post("/checkout", handler.CreateCheckoutSession(logger, cfg, db, lavaClient))   // PAY-02
protected.Post("/subscription/cancel", handler.CancelSubscription(logger, cfg, db, lavaClient))  // PAY-10
protected.Get("/invoices/:id", handler.GetInvoice(logger, cfg, db, lavaClient))           // PAY-09 + D-25 (?escalate=true)
```

**Admin (inherits AuthRequired + AdminRequired + AuditLog automatically — D-32 admin-abuse mitigations are free):**
```go
admin.Get("/plans", handler.AdminListPlans(logger, db))
admin.Post("/plans", handler.AdminCreatePlan(logger, db))
admin.Get("/plans/:id", handler.AdminGetPlan(logger, db))
admin.Patch("/plans/:id", handler.AdminUpdatePlan(logger, db))
admin.Delete("/plans/:id", handler.AdminDeletePlan(logger, db))
admin.Put("/plans/:id/servers", handler.AdminReplacePlanServers(logger, db))
admin.Post("/plans/:id/servers/:server_id", handler.AdminAddPlanServer(logger, db))
admin.Delete("/plans/:id/servers/:server_id", handler.AdminRemovePlanServer(logger, db))
admin.Get("/plans/:id/offers", handler.AdminListPlanOffers(logger, db))
admin.Post("/plans/:id/offers", handler.AdminCreatePlanOffer(logger, db))
admin.Patch("/plans/:id/offers/:offer_id", handler.AdminUpdatePlanOffer(logger, db))
admin.Delete("/plans/:id/offers/:offer_id", handler.AdminDeletePlanOffer(logger, db))
admin.Post("/plans/:id/offers/:offer_id/replace", handler.AdminReplacePlanOffer(logger, db))
admin.Get("/lava/products", handler.AdminListLavaProducts(logger, cfg, lavaClient))   // D-12
```

### 5.3 Audit middleware coverage check

The current `AuditLog` middleware (`internal/middleware/audit.go`):
- Only audits non-GET/HEAD methods (matches D-32 "every state-changing endpoint writes audit_log").
- Reads `c.Locals("user_id")` (matches existing `AdminRequired` + `AuthRequired` pre-population).
- Calls `describeAction(method, path)` to map URL → action name.

**Gap:** `describeAction` (audit.go:105-140) has explicit branches for `/admin/users`, `/admin/servers`, `/admin/change-password`. New `/admin/plans/*` URLs will fall through to the fallback path-sanitization branch (line 132), which produces actions like `post_admin_plans` or `delete_admin_plans_{uuid}_servers_{uuid}`. **Recommendation: planner adds explicit `describeAction` cases for plan-CRUD verbs** (e.g., `create_plan`, `update_plan`, `delete_plan`, `replace_plan_servers`, `add_plan_server`, `remove_plan_server`, `create_offer`, `update_offer`, `delete_offer`, `replace_offer`). One commit's worth of clean-up; matches the existing pattern.

### 5.4 IP allowlist scoping — only on webhook route

**Per-route middleware syntax in Fiber v2:**
```go
api.Post("/webhook/lava", ipAllowlistMiddleware, handler.HandleLavaWebhook(...))
// IP allowlist runs FIRST (before the global per-IP rate limiter, which is OK
// — rate limiter is global on `app.Use` but doesn't drop the request, it
// counts then continues; for the webhook route the allowlist's 403 short-circuits
// before any business logic).
```

**Verified pattern from `cmd/main.go:188-191`:** the existing `api.Post("/auth/link", middleware.LinkAttemptLimit(...), handler.LinkDevice(...))` shows route-scoped middleware works inline as the second argument before the handler.

---

## 6. Redis cache pattern for /plans

**Confidence:** HIGH (current `internal/cache/redis.go` API is small; D-28 caching is a straightforward cache-aside).

### 6.1 Existing cache helper surface

`internal/cache/redis.go` exposes:
- `NewRedisClient(url) (*redis.Client, error)` — used in `cmd/main.go`.
- `IsTokenBlacklisted`, `BlacklistToken` — JWT blacklist (Phase 2).
- `IncrRateLimit` — atomic INCR+EXPIRE Lua script (HOTFIX-03).

**Nothing for cache-aside (`Get`/`Set` with TTL) yet.** Phase 3 adds a tiny wrapper:

```go
// internal/cache/plans_cache.go (NEW — small wrapper to keep redis types out of handlers)

// GetPlansCache returns the cached JSON-encoded plans response for the given currency,
// or "" with no error on cache miss / Redis outage (degrades to DB read).
func GetPlansCache(ctx context.Context, client *redis.Client, currency string) (string, error) {
    if client == nil {
        return "", nil // no Redis configured → cache miss
    }
    key := plansPublicKeyPrefix + currency
    val, err := client.Get(ctx, key).Result()
    if err == redis.Nil {
        return "", nil // miss
    }
    if err != nil {
        // Fail open — Redis outage shouldn't break /plans
        return "", nil
    }
    return val, nil
}

// SetPlansCache stores the JSON-encoded plans response with TTL 60s.
func SetPlansCache(ctx context.Context, client *redis.Client, currency, json string) error {
    if client == nil {
        return nil
    }
    key := plansPublicKeyPrefix + currency
    return client.Set(ctx, key, json, 60*time.Second).Err()
}

// BustPlansCache deletes ALL cache:plans:public:* keys. Called by admin write
// handlers via the audit-middleware path or directly in admin handlers.
func BustPlansCache(ctx context.Context, client *redis.Client) error {
    if client == nil {
        return nil
    }
    // SCAN-based delete avoids the O(N) KEYS blocking call on production-sized
    // keyspaces. For 3 currencies the cardinality is bounded, but SCAN is the
    // canonical pattern.
    iter := client.Scan(ctx, 0, plansPublicKeyPrefix+"*", 100).Iterator()
    for iter.Next(ctx) {
        client.Del(ctx, iter.Val())
    }
    return iter.Err()
}

const plansPublicKeyPrefix = "cache:plans:public:"
```

### 6.2 Handler pattern

```go
func ListPlansPublic(logger *zap.Logger, db *gorm.DB, redisClient *redis.Client) fiber.Handler {
    return func(c *fiber.Ctx) error {
        currency := strings.ToUpper(c.Query("currency"))
        if currency == "" {
            currency = deriveCurrencyFromAcceptLanguage(c.Get("Accept-Language"))
        }
        if !isAllowedCurrency(currency) {
            return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid currency"})
        }

        // Cache-aside read.
        if cached, _ := cache.GetPlansCache(c.Context(), redisClient, currency); cached != "" {
            c.Set("Content-Type", "application/json")
            return c.SendString(cached)
        }

        // Miss — query DB, serialize, cache, return.
        plans, err := repository.ListPlansForPublic(db, currency)
        if err != nil {
            logger.Error("/plans: db read failed", zap.Error(err))
            return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
        }
        body, _ := json.Marshal(fiber.Map{"data": fiber.Map{"currency": currency, "plans": plans}})
        _ = cache.SetPlansCache(c.Context(), redisClient, currency, string(body))
        c.Set("Content-Type", "application/json")
        return c.Send(body)
    }
}

func deriveCurrencyFromAcceptLanguage(header string) string {
    if strings.HasPrefix(strings.ToLower(header), "ru") {
        return "RUB"
    }
    return "USD"
}
```

### 6.3 Bust pattern from admin endpoints

Admin write handlers (in `plans_admin.go`) call `cache.BustPlansCache(c.Context(), redisClient)` after a successful state change. Three approaches:

**(a) Inline in each handler** — simplest, explicit. Recommended.
**(b) Wrap the admin group with a "bust-cache-on-2xx" middleware** — clever but couples cache to audit middleware. Not recommended.
**(c) Post-commit hook on the GORM model** — magical, hard to test. Not recommended.

**Recommendation: (a) — explicit bust call in each handler that mutates plans/plan_servers/plan_offers. KEYS cardinality is bounded by currency count (3 today), so the SCAN approach is overkill — `client.Del(ctx, "cache:plans:public:USD", "cache:plans:public:EUR", "cache:plans:public:RUB")` is fine.**

### 6.4 Degradation on Redis down

The wrapper above returns `""` on any Redis error (cache miss), and `SetPlansCache` returns the error but the handler doesn't propagate it (caller pattern: `_ = cache.SetPlansCache(...)`) — same fail-open contract as `IsTokenBlacklisted` already uses (`internal/cache/redis.go:40-48`). Matches the project's existing "Redis outage doesn't break the public API" stance.

---

## 7. JWT plan_id claim

**Confidence:** HIGH (current `generateTokens` shape is clear; backward-compat path is straightforward).

### 7.1 Current `generateTokens` (auth.go:574-611)

```go
func generateTokens(userID, tier, role, name, secret string) (*authResponse, error) {
    accessClaims := jwt.MapClaims{
        "sub":  userID,
        "tier": tier,
        "role": role,
        "name": name,
        "iat":  now.Unix(),
        "exp":  accessExpiry.Unix(),
    }
    accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
    ...
}
```

Called from 4 places: `AdminLogin` (auth.go:99), `RefreshToken` (auth.go:279), `GuestLogin` (auth.go:407, 520), `AppleSignIn` (auth.go:957), `GoogleSignIn` (auth.go:1010). Plus `storeRefreshSession` is called immediately after each — atomic mint+store.

### 7.2 Required amendment (D-29)

**Signature change:**
```go
func generateTokens(userID, tier, role, name, planID, secret string) (*authResponse, error) {
    accessClaims := jwt.MapClaims{
        "sub":     userID,
        "tier":    tier,
        "role":    role,
        "name":    name,
        "plan_id": planID,   // NEW per D-29
        "iat":     now.Unix(),
        "exp":     accessExpiry.Unix(),
    }
    ...
}
```

**Every caller needs to pass `user.PlanID` (or fall back to system-plan ID). Wave 2's `plan_repo.go::FindSystemPlanID(db)` is the fallback resolver.**

### 7.3 Middleware change

`middleware/auth.go:18-24` — extend `Claims`:
```go
type Claims struct {
    UserID string `json:"sub"`
    Tier   string `json:"tier"`
    Role   string `json:"role"`
    PlanID string `json:"plan_id,omitempty"` // NEW
    jwt.RegisteredClaims
}
```

And after the existing `c.Locals` writes (line 101-103):
```go
c.Locals("plan_id", claims.PlanID)
```

### 7.4 Backward-compat fallback (D-29)

Phase 2 JWTs already in flight do NOT have `plan_id`. The 5-minute access TTL bounds the rollout window — within 5 minutes of deploy, every active JWT carries the new claim. But during that window, the middleware must NOT 401 — it must lazily resolve.

**Recommendation: middleware sets `c.Locals("plan_id", "")` when the claim is empty, and handlers that need plan_id resolve on demand via `repository.FindUserByID(db, userID)`. The handler patterns are:**

```go
// Pattern A — server-list / connection handlers (always need plan_id)
planID, _ := c.Locals("plan_id").(string)
if planID == "" {
    user, err := repository.FindUserByID(db, userID)
    if err != nil { /* handle */ }
    planID = user.PlanID
}
servers, err := repository.ListServersForPlan(db, planID)
```

This costs one DB read per request for old-JWT-holders during the 5-minute window; after that, zero overhead. **Do NOT have the middleware itself do the DB read** — that adds a permanent overhead for every protected route just to support a 5-minute transition.

### 7.5 Existing AuthRequired already does a `FindUserByID` per request

**Important note:** `middleware/auth.go:87-98` already does `repository.FindUserByID(db, claims.UserID)` on every protected request (Phase 1 HOTFIX-02 logic to catch deleted users). The planner can extend that pattern: when the JWT has no `plan_id` claim, use the loaded `user.PlanID` instead of doing a second DB read. The fallback is essentially free:

```go
// middleware/auth.go (PROPOSED amendment)
if db != nil {
    user, err := repository.FindUserByID(db, claims.UserID)
    if err != nil { /* existing error handling */ }
    // Backward-compat: empty plan_id in JWT → use the DB value.
    planID := claims.PlanID
    if planID == "" {
        planID = user.PlanID
    }
    c.Locals("plan_id", planID)
}
```

**Recommendation: this version (middleware-side resolution) is cleaner than per-handler fallback. Middleware already does the DB read; just consult the user struct.**

---

## 8. Plans/PlanOffers/PlanServers model conventions

**Confidence:** HIGH (matches existing GORM model patterns in `internal/model/`).

### 8.1 GORM tag conventions in this codebase

From `internal/model/user.go`, `subscription.go`:

```go
ID         string  `gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
UserID     string  `gorm:"not null;index"`
Plan       string  `gorm:"not null;default:free"`
StripeID   string  `gorm:"type:varchar(255)"`
IsActive   bool    `gorm:"default:true"`
StartedAt  time.Time `gorm:"autoCreateTime"`
ExpiresAt  *time.Time   // pointer for nullable
AppleUserID *string `gorm:"column:apple_user_id;uniqueIndex"`
Email       *string `gorm:"column:email;size:320"`
```

**Patterns:**
- UUID PK: `gorm:"primaryKey;type:uuid;default:gen_random_uuid()"` (Postgres-side default).
- Nullable string: `*string` (pointer), with `column:xxx` tag.
- Nullable time: `*time.Time`.
- Booleans default to false unless explicit `default:true`.
- Indexes inline via `index` or `uniqueIndex` tag; complex/partial indexes go in the migration SQL.

### 8.2 Plan / PlanServer / PlanOffer models

```go
// internal/model/plan.go (NEW)

package model

import "time"

// Plan is an admin-defined entitlement bundle per ADR §19.2.
//
// Exactly one row in the table has is_system=TRUE — enforced by the
// partial unique index idx_plans_one_system (migration 019). When a paid
// plan expires the scheduler (D-26) flips users.plan_id back to that row.
type Plan struct {
    ID             string  `json:"id"          gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
    Code           string  `json:"code"        gorm:"type:varchar(40);uniqueIndex;not null"`
    Name           string  `json:"name"        gorm:"type:varchar(100);not null"`
    Description    string  `json:"description" gorm:"type:text;default:''"`
    MaxDevices     int     `json:"max_devices" gorm:"not null"`        // -1 = unlimited
    MaxServers     int     `json:"max_servers" gorm:"not null"`        // -1 = unlimited; informational
    SpeedLimitMbps int     `json:"speed_limit_mbps" gorm:"not null;default:0"` // 0 = unlimited
    IsActive       bool    `json:"is_active"   gorm:"not null;default:true"`
    IsSystem       bool    `json:"is_system"   gorm:"not null;default:false"`
    SortOrder      int     `json:"sort_order"  gorm:"not null;default:0"`
    CreatedAt      time.Time `json:"created_at" gorm:"autoCreateTime"`
    UpdatedAt      time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

// PlanServer is the M:N join table between plans and vpn_servers.
// Composite PK matches the migration's PRIMARY KEY (plan_id, server_id).
type PlanServer struct {
    PlanID   string `json:"plan_id"   gorm:"primaryKey;type:uuid"`
    ServerID string `json:"server_id" gorm:"primaryKey;type:uuid"`
}

// PlanOffer is a (plan, periodicity, currency) tuple bound to a lava_offer_id.
// Multiple offers per plan; multiple offers per (plan, periodicity, currency)
// allowed but only one with is_active=true (enforced by partial unique index).
type PlanOffer struct {
    ID           string    `json:"id"            gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
    PlanID       string    `json:"plan_id"       gorm:"type:uuid;not null;index"`
    Periodicity  string    `json:"periodicity"   gorm:"type:varchar(20);not null"`  // ONE_TIME|MONTHLY|PERIOD_90_DAYS|PERIOD_180_DAYS|PERIOD_YEAR
    Currency     string    `json:"currency"      gorm:"type:varchar(3);not null"`   // USD|EUR|RUB
    Amount       float64   `json:"amount"        gorm:"type:numeric(10,2);not null"`
    LavaOfferID  *string   `json:"lava_offer_id" gorm:"column:lava_offer_id;type:varchar(64)"` // nullable until admin sets via dropdown D-12
    IsActive     bool      `json:"is_active"     gorm:"not null;default:true"`
    CreatedAt    time.Time `json:"created_at"    gorm:"autoCreateTime"`
    UpdatedAt    time.Time `json:"updated_at"    gorm:"autoUpdateTime"`
}
```

### 8.3 `User.PlanID` amendment

Add to existing `User` struct in `internal/model/user.go`:
```go
PlanID string `json:"plan_id" gorm:"column:plan_id;type:uuid;not null;index"`
```

**Note:** D-08 says the migration backfills `plan_id` and then sets `NOT NULL`. So in Go, `PlanID` is a non-pointer `string` (NOT NULL after migration completes). The model can be plain `string` — the planner doesn't need `*string`.

### 8.4 `Subscription` REWRITE

Per CONTEXT.md D-01/D-11:
```go
// internal/model/subscription.go (REWRITE)
package model

import "time"

// Subscription is the canonical "current entitlement" record. Phase 3 drops
// stripe_id and adds lava_contract_id. The PlanLimits map is deleted — limits
// live in the plans table.
type Subscription struct {
    ID              string     `json:"id"                gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
    UserID          string     `json:"user_id"           gorm:"not null;index"`
    Plan            string     `json:"plan"              gorm:"not null;default:free"` // free, pro, ... (denormalized plans.code)
    LavaContractID  *string    `json:"-"                 gorm:"column:lava_contract_id;type:varchar(64);index"`
    IsActive        bool       `json:"is_active"         gorm:"default:true"`
    StartedAt       time.Time  `json:"started_at"        gorm:"autoCreateTime"`
    ExpiresAt       *time.Time `json:"expires_at"`
}

// UnlimitedServers / UnlimitedDevices sentinel values stay — used by handlers
// to interpret plans.max_devices == -1 / plans.max_servers == -1.
const (
    UnlimitedServers = -1
    UnlimitedDevices = -1
)
```

**`PlanLimits` map is DELETED.** All readers in `servers.go`, `connection.go`, `health.go`, `admin.go` switch to `repository.FindPlanByID` or `repository.FindPlanByCode`.

### 8.5 New invoice + lava_contract models

```go
// internal/model/invoice.go (NEW)
type Invoice struct {
    ID            string     `gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
    UserID        string     `gorm:"type:uuid;not null;index"`
    LavaInvoiceID string     `gorm:"type:varchar(64);uniqueIndex;not null"`
    OfferID       string     `gorm:"type:varchar(64);not null"` // lava-side offer UUID (forensics)
    PlanID        *string    `gorm:"type:uuid;index"`           // ADR §19.6 amendment
    PlanOfferID   *string    `gorm:"type:uuid;index"`           // ADR §19.6 amendment
    Plan          string     `gorm:"type:varchar(20);not null"`
    Periodicity   string     `gorm:"type:varchar(20);not null"`
    Currency      string     `gorm:"type:varchar(3);not null"`
    Amount        float64    `gorm:"type:numeric(10,2);not null"`
    Status        string     `gorm:"type:varchar(20);not null"`
    PaymentURL    string     `gorm:"type:text"`
    CreatedAt     time.Time  `gorm:"autoCreateTime"`
    UpdatedAt     time.Time  `gorm:"autoUpdateTime"`
}

// internal/model/lava_contract.go (NEW)
type LavaContract struct {
    ID               string     `gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
    UserID           string     `gorm:"type:uuid;not null;index"`
    ContractID       string     `gorm:"type:varchar(64);uniqueIndex;not null"`
    ParentContractID *string    `gorm:"type:varchar(64)"` // for renewals
    OfferID          string     `gorm:"type:varchar(64);not null"`
    Plan             string     `gorm:"type:varchar(20);not null"`
    Periodicity      string     `gorm:"type:varchar(20);not null"`
    Currency         string     `gorm:"type:varchar(3);not null"`
    IsActive         bool       `gorm:"not null;default:true"`
    StartedAt        time.Time  `gorm:"autoCreateTime"`
    ExpiresAt        *time.Time
    CancelledAt      *time.Time
    CreatedAt        time.Time  `gorm:"autoCreateTime"`
}

// internal/model/lava_webhook_event.go (NEW)
type LavaWebhookEvent struct {
    ID          string         `gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
    EventType   string         `gorm:"type:varchar(64);not null"`
    ContractID  *string        `gorm:"type:varchar(64)"`
    InvoiceID   *string        `gorm:"type:varchar(64)"`
    Payload     datatypes.JSON `gorm:"type:jsonb;not null"`
    ReceivedAt  time.Time      `gorm:"autoCreateTime"`
    ProcessedAt *time.Time
    Error       *string        `gorm:"type:text"`
}
```

**Note: `datatypes.JSON`** requires `gorm.io/datatypes` — add to `go.mod`:
```bash
go get gorm.io/datatypes
```

Or use `json.RawMessage` directly (Postgres pq driver handles it natively for jsonb columns). The planner picks — both work; `datatypes.JSON` is idiomatic in GORM tests.

### 8.6 UUID PK approach

`gen_random_uuid()` is the Postgres 13+ built-in. CLAUDE.md locks Postgres 16, so this is fine. **No client-side UUID generation needed.** Existing models use this pattern verbatim (`internal/model/user.go:16`, `subscription.go:13`).

### 8.7 Code TEXT UNIQUE NOT NULL

`Code string `gorm:"type:varchar(40);uniqueIndex;not null"` — matches existing `users.email_hash` pattern but with `varchar(40)` instead of jsonbcolumn:`. The `uniqueIndex` tag generates a non-partial unique index; the migration adds the explicit partial-active index separately.

---

## 9. Migration test harness

**Confidence:** MEDIUM (existing migration tests are sparse — `migrations/` has SQL files but no `migrations_test.go`; the closest pattern is `repository/subscription_repo_test.go` which builds test schemas in SQLite).

### 9.1 Existing test infrastructure

Repo tests in `internal/repository/*_test.go` use `gorm.io/driver/sqlite` with in-memory DB:
```go
db, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
db.Exec(`CREATE TABLE subscriptions (id TEXT PRIMARY KEY, ... stripe_id TEXT, ...);`)
```

This is fine for repository unit tests but does NOT test the actual migration SQL files.

### 9.2 Recommended migration test for 019/020

The planner adds a dedicated test that runs against Postgres (testcontainers), apply 018 → seed → apply 019 → assert:

```go
// server/api/migrations/migrations_test.go (NEW)
//
// Verifies:
//   - 019 applies cleanly after 018
//   - Pro is seeded with max_devices=3 (D-08 override)
//   - Free is seeded with max_devices=1, max_servers=3 (D-08)
//   - 6 placeholder offers exist for Pro (D-09)
//   - premium/ultimate user rows coerce to pro (D-08 destruction-free)
//   - users.plan_id is backfilled and NOT NULL
//   - 020 applies cleanly
//   - subscriptions.stripe_id is dropped (D-11)
//   - subscriptions.lava_contract_id exists

func TestMigrations019_020(t *testing.T) {
    pgContainer := startTestcontainerPostgres(t)
    defer pgContainer.Terminate(context.Background())

    db := openGormToContainer(t, pgContainer)

    // Apply 001-018 (existing — replay all in order).
    applyAllMigrationsUpTo(t, db, "018_add_sso_columns.sql")

    // Seed a few premium/ultimate users for the coercion test.
    db.Exec(`INSERT INTO users (id, full_name, subscription_tier) VALUES
        (gen_random_uuid(), 'a', 'premium'),
        (gen_random_uuid(), 'b', 'ultimate'),
        (gen_random_uuid(), 'c', 'free');`)

    // Apply 019.
    apply(t, db, "019_plans_catalog.sql")

    // Assert coercion.
    var n int64
    db.Raw(`SELECT count(*) FROM users WHERE subscription_tier IN ('premium', 'ultimate')`).Scan(&n)
    require.Zero(t, n, "premium/ultimate must be coerced to pro")

    // Assert Pro seeded with max_devices=3.
    var proMaxDevices int
    db.Raw(`SELECT max_devices FROM plans WHERE code = 'pro'`).Scan(&proMaxDevices)
    require.Equal(t, 3, proMaxDevices, "D-08 — Pro max_devices=3 (not 5)")

    // Assert 6 placeholder offers for Pro.
    var offerCount int64
    db.Raw(`SELECT count(*) FROM plan_offers WHERE plan_id = (SELECT id FROM plans WHERE code = 'pro')`).Scan(&offerCount)
    require.Equal(t, int64(6), offerCount, "D-09 — 6 placeholder offers")

    // Assert all users have a plan_id.
    db.Raw(`SELECT count(*) FROM users WHERE plan_id IS NULL`).Scan(&n)
    require.Zero(t, n, "all users must have plan_id backfilled")

    // Apply 020.
    apply(t, db, "020_lava_payments.sql")

    // Assert stripe_id dropped, lava_contract_id added.
    var hasStripeID, hasLavaContractID bool
    db.Raw(`SELECT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name='subscriptions' AND column_name='stripe_id'
    )`).Scan(&hasStripeID)
    require.False(t, hasStripeID, "D-11 — subscriptions.stripe_id must be dropped")

    db.Raw(`SELECT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name='subscriptions' AND column_name='lava_contract_id'
    )`).Scan(&hasLavaContractID)
    require.True(t, hasLavaContractID, "D-11 — subscriptions.lava_contract_id must be added")
}
```

**Dependency:** add `github.com/testcontainers/testcontainers-go` and the Postgres module:
```bash
go get github.com/testcontainers/testcontainers-go
go get github.com/testcontainers/testcontainers-go/modules/postgres
```

Both are in active maintenance.

**Speed note:** testcontainers spins up a real Postgres in Docker — ~3-5s per test setup. CI cost is small (one container per test file with `TestMain`). Docker is available locally (verified via `docker --version` → `Docker version 29.2.0`).

---

## 10. Admin-web Plans UI

**Confidence:** HIGH (existing admin-web is verified — Vite + React 19 + TanStack Query + Zustand + shadcn/ui + react-router-dom 7 + axios).

### 10.1 Current admin-web structure

```
admin-web/src/
├── App.tsx                    # routes via react-router-dom 7 (not TanStack Router)
├── main.tsx
├── api/
│   ├── client.ts              # axios instance with token-refresh interceptor (cross-tab via navigator.locks)
│   ├── servers.ts             # GET /admin/servers, PATCH, DELETE
│   ├── users.ts, analytics.ts, audit.ts, auth.ts, ...
├── components/
│   ├── layout/AdminLayout.tsx # sidebar nav + logout (current navItems array at line 18)
│   ├── ui/                    # shadcn — currently: badge, button, card, dialog, dropdown-menu, input, label, separator, skeleton, table
│   ├── AnalyticsSection.tsx, ConnectionsSection.tsx, ...
├── pages/                     # Dashboard, Login, Users, UserDetail, Servers, Activity, Settings
├── stores/                    # Zustand stores (authStore, etc.)
└── lib/                       # utils, format helpers
```

### 10.2 Routing setup

`App.tsx` uses `react-router-dom` (NOT TanStack Router). Adding `/plans` and `/plans/:id`:
```tsx
import { Plans } from "@/pages/Plans";
import { PlanDetail } from "@/pages/PlanDetail";

<Route path="/plans" element={<Plans />} />
<Route path="/plans/:id" element={<PlanDetail />} />
```

### 10.3 TanStack Query patterns

From `pages/Servers.tsx:38-71`:
```tsx
const { data, isLoading, isError, error } = useQuery({
    queryKey: ["admin", "servers"],
    queryFn: listServers,
});

const toggleMutation = useMutation({
    mutationFn: ({ id, isActive }) => updateServer(id, { is_active: isActive }),
    onSuccess: async (_, vars) => {
        await qc.invalidateQueries({ queryKey: ["admin", "servers"] });
        await qc.invalidateQueries({ queryKey: ["admin", "stats"] });
        toast.success(...);
    },
    onError: (err) => {
        const axiosErr = err as AxiosError<{ error?: string }>;
        toast.error(axiosErr.response?.data?.error ?? "...");
    },
});
```

**Phase 3 follows this verbatim.** Query keys: `["admin", "plans"]`, `["admin", "plan", id]`, `["admin", "plan", id, "offers"]`, `["admin", "plan", id, "servers"]`, `["admin", "lava", "products"]`.

### 10.4 API client patterns

From `api/servers.ts`:
```tsx
import { api } from "@/api/client";

export interface AdminServer { id: string; hostname: string; ... }

export async function listServers(): Promise<AdminServer[]> {
    const resp = await api.get<AdminServer[]>("/api/v1/admin/servers");
    return resp.data;
}
```

**Important: the client interceptor (`api/client.ts:159-167`) unwraps `{ data: ... }` envelopes automatically.** Handlers return `{"data": [...]}`; the interceptor strips it; type the call as the inner shape.

### 10.5 shadcn/ui inventory and gaps

**Currently installed (`admin-web/src/components/ui/`):**
- badge.tsx, button.tsx, card.tsx, dialog.tsx, dropdown-menu.tsx, input.tsx, label.tsx, separator.tsx, skeleton.tsx, table.tsx

**Needed for Plans UI (D-13):**
| Component | Use | Available? |
|-----------|-----|------------|
| `Table` | PlansTable | ✓ |
| `Dialog` | DeletePlanDialog, ReplaceOfferDialog, offer-edit modal | ✓ |
| `Button` | All forms | ✓ |
| `Input`, `Label` | PlanForm fields | ✓ |
| `Card` | Plan card layout | ✓ |
| `Badge` | Status pills, code badge | ✓ |
| `Dropdown menu` | Row action menu | ✓ |
| `Skeleton` | Loading states | ✓ |
| **`Form`** | Form validation (react-hook-form + zod) | ✗ MISSING |
| **`Select`** | Lava offer dropdown (D-12 Option B), periodicity/currency selects | ✗ MISSING |
| **`Combobox`** | Searchable lava offer picker (optional but nicer than basic Select) | ✗ MISSING |
| **`Checkbox`** | PlanServersPicker multi-select | ✗ MISSING |
| **`Switch`** | is_active toggle | ✗ MISSING |
| **`Tabs`** | PlanDetail's 3 tabs (Limits / Servers / Pricing) | ✗ MISSING |
| **`Tooltip`** | Warnings ("lava floor is $5"), greyed-out fields | ✗ MISSING |
| **`Textarea`** | Plan description (markdown) | ✗ MISSING |
| **`Sonner` / Toast** | Already imported in Servers.tsx (`sonner` package in package.json) | ✓ (via `sonner` lib) |

**Action: Wave 5 Wave 0 — install missing shadcn components:**
```bash
# These are shadcn CLI installs (or hand-vendored). Existing components were hand-vendored
# per the project pattern (the project uses Radix primitives directly).
npm install @radix-ui/react-checkbox @radix-ui/react-select @radix-ui/react-switch @radix-ui/react-tabs @radix-ui/react-tooltip
# Then hand-vendor each into components/ui/ following existing component shape.
```

**Recommendation: planner adds a dedicated "03-10a Wave 0" task for adding the shadcn components before any Plans page work.** This is a single commit, makes the rest of the UI work reviewable.

### 10.6 API client files for Phase 3

```
admin-web/src/api/
├── plans.ts        (NEW — list/get/create/update/delete plans, replace servers, CRUD offers, replace offer)
├── lava.ts         (NEW — list lava products via GET /admin/lava/products)
```

**`plans.ts` API surface:**
```tsx
import { api } from "@/api/client";

export interface AdminPlanSummary { id: string; code: string; name: string; ... }
export interface AdminPlanDetail extends AdminPlanSummary { servers: AdminServer[]; offers: PlanOffer[]; ... }
export interface PlanOffer { id: string; periodicity: string; currency: string; amount: number; lava_offer_id: string|null; is_active: boolean; }
export interface CreatePlanInput { code: string; name: string; max_devices: number; max_servers: number; speed_limit_mbps: number; sort_order: number; server_ids: string[]; offers: { periodicity: string; currency: string; amount: number; lava_offer_id?: string|null }[] }

export async function listPlans(): Promise<AdminPlanSummary[]> { ... }
export async function getPlan(id: string): Promise<AdminPlanDetail> { ... }
export async function createPlan(input: CreatePlanInput): Promise<AdminPlanDetail> { ... }
export async function updatePlan(id: string, input: Partial<CreatePlanInput>): Promise<AdminPlanDetail> { ... }
export async function deletePlan(id: string, force?: boolean): Promise<{ affected_users: number }> { ... }
export async function replacePlanServers(planId: string, serverIds: string[]): Promise<void> { ... }
export async function addPlanServer(planId: string, serverId: string): Promise<void> { ... }
export async function removePlanServer(planId: string, serverId: string): Promise<void> { ... }
export async function createOffer(planId: string, input: { periodicity: string; currency: string; amount: number; lava_offer_id?: string|null }): Promise<PlanOffer> { ... }
export async function updateOffer(planId: string, offerId: string, input: Partial<{ amount: number; lava_offer_id: string|null; is_active: boolean }>): Promise<PlanOffer> { ... }
export async function deleteOffer(planId: string, offerId: string): Promise<void> { ... }
export async function replaceOffer(planId: string, offerId: string, input: { amount: number; lava_offer_id?: string|null }): Promise<PlanOffer> { ... }
```

**`lava.ts` API surface:**
```tsx
export interface LavaProduct {
    productId: string; productName: string; offerId: string; offerName: string;
    periodicity: string; currency: string; amount: number;
}
export async function listLavaProducts(): Promise<LavaProduct[]> {
    const resp = await api.get<LavaProduct[]>("/api/v1/admin/lava/products");
    return resp.data;
}
```

### 10.7 Sidebar entry

`components/layout/AdminLayout.tsx:18-24`:
```tsx
const navItems = [
    { to: "/dashboard", label: "Обзор", Icon: LayoutDashboard },
    { to: "/users", label: "Пользователи", Icon: Users },
    { to: "/servers", label: "Серверы", Icon: Server },
    { to: "/plans", label: "Тарифы", Icon: Tag },  // NEW — D-13. Russian "Тарифы" matches existing labels.
    { to: "/activity", label: "Журнал", Icon: Activity },
    { to: "/settings", label: "Настройки", Icon: Settings },
];
```

`Tag` icon from `lucide-react` (already imported elsewhere). Single-line addition.

### 10.8 Component file plan

Per ADR §19.13 + D-13 (in scope for Phase 3):

```
admin-web/src/pages/
├── Plans.tsx                                     (NEW — table view + "New Plan" CTA)
├── PlanDetail.tsx                                (NEW — three tabs: Limits, Servers, Pricing)

admin-web/src/components/plans/
├── PlansTable.tsx                                (NEW — list view, columns: Code, Name, Status, Servers, Devices, Active users, Updated)
├── PlanForm.tsx                                  (NEW — Limits tab form, react-hook-form + zod)
├── PlanServersPicker.tsx                         (NEW — split pane: available/selected, country group, checkbox)
├── PlanOffersGrid.tsx                            (NEW — Pricing tab grid: periodicities × currencies)
├── LavaOfferPicker.tsx                           (NEW — Select/Combobox of /admin/lava/products, used inside PlanOffersGrid cell modal)
├── DeletePlanDialog.tsx                          (NEW — confirm + ?force banner; user count display)
├── ReplaceOfferDialog.tsx                        (NEW — "Update price" flow per §19.10)
├── PlanCodeBadge.tsx                             (NEW — small immutable-slug display, matches existing Badge usage)
```

**Planner discretion (CONTEXT.md):** can collapse Plans.tsx + PlanDetail.tsx + components into one or two plans depending on how granular the per-commit boundaries should be. Recommendation: one Plans listing plan + one PlanDetail (Limits + Servers + Pricing) plan = 2 plans for the UI tier. Or 3 if Pricing is its own commit boundary.

---

## 11. lava.top sandbox specifics

**Confidence:** MEDIUM (public docs are sparse on sandbox; faq.lava.top returns 403 to unauthenticated WebFetch; recommendation is to confirm with operator).

### 11.1 Base URL

Per ADR §19.6 and CONTEXT.md D-15: `https://gate.lava.top` is the production URL. **No documented separate sandbox URL.** The distinction is by API key alone (sandbox keys vs production keys).

### 11.2 Test cards

**Not documented in publicly accessible lava.top materials.** The OpenAPI spec mentions test cards only obliquely. Operator confirmation required.

### 11.3 Webhook delivery to localhost

For local dev, the planner needs a public tunnel (ngrok, cloudflared, or operator's existing staging URL) so lava can POST to the webhook handler. **Sandbox webhook delivery uses the same mechanism as production** — IP allowlist remains relevant; the same `158.160.60.174/32` IP is documented as lava.top's webhook source.

**Recommendation for the integration test (D-06):** the Wave 5 `03-12` plan adds a sandbox integration test that:
1. Boots the API server in a docker-compose setup with `LAVA_ENV=sandbox` and `LAVA_API_KEY_SANDBOX` injected.
2. Calls `POST /api/v1/checkout` with a real test offer ID (operator pre-configures a "Test Pro Monthly $5" offer in lava sandbox).
3. Asserts `paymentUrl` is returned.
4. Optionally automates browser-driven payment via a test card if Phase 3 timeline allows; otherwise mark it manual ("operator clicks pay button, confirms within 60s, test waits for webhook").
5. Asserts the webhook arrives (LavaWebhookEvent row inserted, `users.plan_id` flipped).

**For CI:** the planner can stub the webhook delivery — POST the expected payload directly to `/webhook/lava` from the test, bypassing lava's actual delivery. The integration test ONLY runs against sandbox manually before tagging `v2.2.0-pay` (D-05).

---

## 12. `internal/lava/` package layout (D-14)

**Confidence:** HIGH (matches the Phase 2 `internal/auth/{apple,google}/` precedent).

### 12.1 Files and signatures

```
server/api/internal/lava/
├── client.go            // Client struct + constructor
├── invoice.go           // CreateInvoice, GetInvoice
├── products.go          // ListProducts (paginated cursor drain)
├── subscription.go      // CancelSubscription
├── webhook.go           // VerifyAPIKey (constant-time compare both secrets)
├── dto.go               // All request/response DTOs from §1
├── client_test.go       // httptest-based unit tests
├── invoice_test.go
├── products_test.go
├── subscription_test.go
├── webhook_test.go
```

### 12.2 `client.go`

```go
package lava

import (
    "context"
    "fmt"
    "net/http"
    "time"
)

// BaseURL is the lava.top API root. Hardcoded per D-15 — no env override.
// SSRF mitigation: this is a const string literal; the verifier in Wave 5
// (sandbox integration test) MUST grep for `const BaseURL = "https://gate.lava.top"`
// and fail if anything else appears.
const BaseURL = "https://gate.lava.top"

// Client is an HTTP client for the lava.top public API.
//
// Construction is once-at-startup in cmd/main.go; the Client is passed
// to handlers via DI. The package has no globals beyond BaseURL.
type Client struct {
    apiKey string
    http   *http.Client
}

// New constructs a Client. apiKey is the X-Api-Key header value
// (LAVA_API_KEY or LAVA_API_KEY_SANDBOX, picked by cmd/main.go per LAVA_ENV).
//
// The HTTP client uses:
//   - 5-second context timeout (D-14)
//   - no redirect following (defense against open-redirect abuse)
//   - default TLS verification (no InsecureSkipVerify — D-14 explicit)
func New(apiKey string) *Client {
    return &Client{
        apiKey: apiKey,
        http: &http.Client{
            Timeout: 5 * time.Second,
            CheckRedirect: func(req *http.Request, via []*http.Request) error {
                return http.ErrUseLastResponse // do not follow redirects
            },
        },
    }
}

// do is the shared request helper. Stamps X-Api-Key and Content-Type;
// returns the response or a wrapped error. Caller closes resp.Body.
func (c *Client) do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
    req, err := http.NewRequestWithContext(ctx, method, BaseURL+path, body)
    if err != nil {
        return nil, fmt.Errorf("lava: build request: %w", err)
    }
    req.Header.Set("X-Api-Key", c.apiKey)
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Accept", "application/json")
    return c.http.Do(req)
}
```

### 12.3 `invoice.go`, `products.go`, `subscription.go` skeleton

```go
// invoice.go
func (c *Client) CreateInvoice(ctx context.Context, req CreateInvoiceRequest) (*InvoiceResponse, error) {
    body, _ := json.Marshal(req)
    resp, err := c.do(ctx, "POST", "/api/v3/invoice", bytes.NewReader(body))
    if err != nil { return nil, err }
    defer resp.Body.Close()
    if resp.StatusCode >= 400 {
        var lavaErr struct{ Message string `json:"message"` }
        _ = json.NewDecoder(resp.Body).Decode(&lavaErr)
        return nil, fmt.Errorf("lava CreateInvoice: %d %s", resp.StatusCode, lavaErr.Message)
    }
    var out InvoiceResponse
    if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
        return nil, fmt.Errorf("lava CreateInvoice: decode: %w", err)
    }
    return &out, nil
}

func (c *Client) GetInvoice(ctx context.Context, invoiceID string) (*InvoiceDetailResponse, error) {
    resp, err := c.do(ctx, "GET", "/api/v2/invoices/"+url.PathEscape(invoiceID), nil)
    // ... same shape
}

// products.go — drains pagination cursor
func (c *Client) ListProducts(ctx context.Context) ([]ProductItemResponse, error) {
    var all []ProductItemResponse
    next := "" // first page has no cursor
    for {
        path := "/api/v2/products"
        if next != "" { path += "?nextPage=" + url.QueryEscape(next) }
        resp, err := c.do(ctx, "GET", path, nil)
        // ... handle errors
        var page ProductsResponse
        json.NewDecoder(resp.Body).Decode(&page)
        for _, item := range page.Items {
            if item.Type == "PRODUCT" { all = append(all, item.Data) }
        }
        if page.NextPage == nil || *page.NextPage == "" { break }
        next = *page.NextPage
    }
    return all, nil
}

// subscription.go
func (c *Client) CancelSubscription(ctx context.Context, contractID, email string) error {
    path := "/api/v1/subscriptions?contractId=" + url.QueryEscape(contractID) +
            "&email=" + url.QueryEscape(email)
    resp, err := c.do(ctx, "DELETE", path, nil)
    if err != nil { return err }
    defer resp.Body.Close()
    if resp.StatusCode >= 400 {
        return fmt.Errorf("lava CancelSubscription: %d", resp.StatusCode)
    }
    return nil
}
```

### 12.4 `webhook.go` — VerifyAPIKey

```go
import "crypto/subtle"

// VerifyAPIKey returns true if the provided X-Api-Key value matches either
// the current secret or (when set) the previous one. Both comparisons use
// constant-time equality so timing attacks can't leak prefix matches.
//
// Returns false on any mismatch. Use directly in the handler:
//
//   if !lava.VerifyAPIKey(c.Get("X-Api-Key"), cfg.LavaWebhookSecret, cfg.LavaWebhookSecretPrevious) {
//       return c.SendStatus(fiber.StatusUnauthorized)
//   }
func VerifyAPIKey(received, current, previous string) bool {
    if subtle.ConstantTimeCompare([]byte(received), []byte(current)) == 1 {
        return true
    }
    if previous != "" && subtle.ConstantTimeCompare([]byte(received), []byte(previous)) == 1 {
        return true
    }
    return false
}
```

### 12.5 Testing approach

Each `*_test.go` uses `httptest.NewServer` to mock lava endpoints:
```go
func TestCreateInvoice(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        assert.Equal(t, "/api/v3/invoice", r.URL.Path)
        assert.Equal(t, "test-key", r.Header.Get("X-Api-Key"))
        var body CreateInvoiceRequest
        _ = json.NewDecoder(r.Body).Decode(&body)
        assert.Equal(t, "test@example.com", body.Email)
        w.WriteHeader(200)
        json.NewEncoder(w).Encode(InvoiceResponse{ID: "inv-123", Status: "in-progress", PaymentURL: ptr("https://pay.lava/...")})
    }))
    defer srv.Close()

    // Override BaseURL for this test. Since BaseURL is `const`, the planner
    // either makes `Client.baseURL` a struct field (with constructor still
    // hard-coding BaseURL for production), OR uses `httptest.NewServer` +
    // url.Parse(srv.URL).Host to rewire net/http via a custom Transport.
    //
    // Recommendation: make Client.baseURL a struct field, exposed via a
    // package-private constructor newWithBaseURL(apiKey, baseURL) for tests.
    // The public New() calls it with BaseURL. Production code goes through
    // New() which is fed by the constant. SSRF audit still passes because
    // newWithBaseURL is package-private and the only caller from production
    // is New() with the hardcoded constant.
    client := newWithBaseURL("test-key", srv.URL)
    inv, err := client.CreateInvoice(context.Background(), CreateInvoiceRequest{
        Email: "test@example.com", OfferID: "uuid-1", Currency: "USD",
    })
    require.NoError(t, err)
    assert.Equal(t, "inv-123", inv.ID)
}
```

**Important:** `BaseURL` as a `const` makes test rewiring tricky. The planner's choice: (a) expose a package-private `newWithBaseURL` for tests, or (b) inject the URL via a `Client.WithBaseURL(string)` method. Recommended (a) — keeps the production-call shape `lava.New(apiKey)` clean.

---

## 13. D-30 env validation

**Confidence:** HIGH (HOTFIX-08 validator pattern is in `config.go:180-201`).

### 13.1 HOTFIX-08 validator recap

`config.go::RequireEnv()` scans a hardcoded list and returns missing keys:
```go
func RequireEnv() []string {
    required := []string{
        "JWT_SECRET", "DATABASE_URL", "REDIS_URL", "TUNNEL_VLESS_UUID",
        "APPLE_TEAM_ID", "APPLE_BUNDLE_ID", "APPLE_SERVICE_ID",
        "GOOGLE_CLIENT_ID_IOS", "GOOGLE_CLIENT_ID_ANDROID", "GOOGLE_CLIENT_ID_WEB",
    }
    var missing []string
    for _, key := range required {
        if os.Getenv(key) == "" { missing = append(missing, key) }
    }
    return missing
}
```

`cmd/main.go:42-47` calls it BEFORE `config.Load()`; non-empty result → `logger.Fatal` → `os.Exit(1)`.

### 13.2 Phase 3 amendments

```go
// config.go (AMENDED)
func RequireEnv() []string {
    required := []string{
        "JWT_SECRET", "DATABASE_URL", "REDIS_URL", "TUNNEL_VLESS_UUID",
        "APPLE_TEAM_ID", "APPLE_BUNDLE_ID", "APPLE_SERVICE_ID",
        "GOOGLE_CLIENT_ID_IOS", "GOOGLE_CLIENT_ID_ANDROID", "GOOGLE_CLIENT_ID_WEB",

        // Phase 3 D-30 — payment provider required at startup.
        "LAVA_WEBHOOK_SECRET",
        "LAVA_WEBHOOK_ALLOWED_CIDRS",  // Planner's pick — RECOMMENDATION: strict-required
        "LAVA_SUCCESS_URL",
        "LAVA_FAIL_URL",
        // LAVA_API_KEY OR LAVA_API_KEY_SANDBOX — exactly one MUST be set,
        // picked by LAVA_ENV. Validation in a separate function.
    }
    var missing []string
    for _, key := range required {
        if os.Getenv(key) == "" { missing = append(missing, key) }
    }
    // Additional: LAVA_ENV + correct API key set together.
    lavaEnv := os.Getenv("LAVA_ENV") // default "production" when empty
    if lavaEnv == "" { lavaEnv = "production" }
    switch lavaEnv {
    case "production":
        if os.Getenv("LAVA_API_KEY") == "" { missing = append(missing, "LAVA_API_KEY (LAVA_ENV=production)") }
    case "sandbox":
        if os.Getenv("LAVA_API_KEY_SANDBOX") == "" { missing = append(missing, "LAVA_API_KEY_SANDBOX (LAVA_ENV=sandbox)") }
    default:
        missing = append(missing, fmt.Sprintf("LAVA_ENV=%q (must be 'production' or 'sandbox')", lavaEnv))
    }
    return missing
}
```

### 13.3 Recommendations for the two open D-30 sub-choices

**Choice 1: `LAVA_WEBHOOK_ALLOWED_CIDRS` strict-required vs default.**

**Recommendation: strict-required.** Reasons:
- HOTFIX-08 pattern is "fail-fast with one aggregate error" — defaulting to a hardcoded `158.160.60.174/32` hides operator misconfiguration.
- A wrong default in production would mean any IP can POST webhooks (if the secret is also wrong).
- The cost of strict-required is one extra env var line in `.env.example`; the value is short (`LAVA_WEBHOOK_ALLOWED_CIDRS=158.160.60.174/32`) and stable.

**Choice 2: `LAVA_API_KEY` vs `_SANDBOX` selection logic.**

**Recommendation: explicit `LAVA_ENV=sandbox|production` flag** (defaults to `production` when unset, matches the safer-by-default principle). When `sandbox`, the validator demands `LAVA_API_KEY_SANDBOX`. When `production`, demands `LAVA_API_KEY`. Both keys MAY be set simultaneously in dev (where `LAVA_ENV` flips between calls) — but the active one is determined by `LAVA_ENV`. See §13.2 code above.

### 13.4 `.env.example` updates

```
# Phase 3 lava.top payment provider (D-30)
LAVA_ENV=production                          # sandbox | production (default: production)
LAVA_API_KEY=                                # required when LAVA_ENV=production
LAVA_API_KEY_SANDBOX=                        # required when LAVA_ENV=sandbox
LAVA_WEBHOOK_SECRET=                         # required — X-Api-Key on inbound webhook
LAVA_WEBHOOK_SECRET_PREVIOUS=                # optional — set only during rotation
LAVA_WEBHOOK_ALLOWED_CIDRS=158.160.60.174/32 # required — CSV of CIDRs lava.top webhook sources
LAVA_SUCCESS_URL=https://risevpn.com/pay/success  # required
LAVA_FAIL_URL=https://risevpn.com/pay/fail        # required
```

---

## 14. Stripe leakage check (D-11 verification)

**Confidence:** HIGH (verified by `grep -rn "stripe_id"` and `grep -rn "StripeID"`).

### 14.1 Sites that reference `stripe_id` or `StripeID`

| File | Line | Reference | Phase 3 action |
|------|------|-----------|-----------------|
| `handler/payment.go` | many | Production code | DELETED in D-01 rewrite |
| `repository/subscription_repo.go` | 29, 64 | `FindSubscriptionByStripeID`, `CreateOrUpdateSubscription` Updates | DELETE both lines — those functions are Stripe-only and unreferenced after D-01 |
| `repository/subscription_repo_test.go` | 52, 261 | Test-local DDL + test asserting `stripe_id=sub_new` | **STALE after D-11.** The DDL line creates a fresh table that doesn't match production schema — test passes because the test owns its schema. But the assertion at line 261 references `found.StripeID` from `model.Subscription`, which is REMOVED in D-01. **This test file must be updated.** |
| `handler/payment_test.go` | 75, 112, 326, 439 | Stripe-only tests | Per D-03, **NOT touched this phase**. Orphaned for Phase 8 cleanup. |
| `handler/auth_test.go` | 85 | Test-local DDL only (creates schema, doesn't assert stripe_id) | Safe — the DDL builds a SQLite test table; once `model.Subscription.StripeID` is removed, GORM auto-migrate (if used) might fail. Check whether tests use AutoMigrate or hand-rolled DDL. **Hand-rolled — the `CREATE TABLE subscriptions (... stripe_id TEXT ...)` is verbatim string SQL.** Phase 3 doesn't break this. |
| `handler/admin_test.go` | 336 | Same — test-local DDL string | Same — safe. |

### 14.2 The blocking question for the planner

**`subscription_repo_test.go:261` references `found.StripeID`.** After D-01 strips `StripeID` from `model.Subscription`, this test **won't compile**.

**Two options:**
- **(a) Update the test to assert against `LavaContractID` instead** (modernizes the test for Phase 3 — matches the new column shape).
- **(b) Skip the assertion** (planner adds a `t.Skip("Stripe-only — deleted in Phase 8 HARD-01")` for that subtest).

**Recommendation: (a).** Updating it is a 2-line change, keeps the regression test alive for the new column shape, and matches the spirit of D-11 (the column dropped, the test should test the replacement). Also: this is a regression-prevention test for `CreateOrUpdateSubscription` — Phase 3 still keeps that function (it's used by `SetUserPlan`); only the field renames.

### 14.3 Recommended verification task

Add a `03-12` (or wherever Wave 5 lands the smoke checks) task:
```bash
# After all code changes:
grep -rn 'PlanLimits' server/api/internal/ server/api/cmd/  # must show ZERO hits outside model/subscription.go (which is REWRITTEN to delete it) and the migration test
grep -rn 'StripeID' server/api/internal/  # must show ZERO hits outside payment_test.go (per D-03 — orphaned)
grep -rn 'stripe_id' server/api/internal/  # must show ONLY hits in *_test.go files' DDL strings (per D-03)
go build ./...  # must succeed
go test ./...  # all tests pass; payment_test.go Stripe tests are orphaned but should still compile + skip themselves OR pass against the test-local schema
```

---

## 15. PAY-01..PAY-16 → research-section index

| Req ID | Description | Research sections supporting it |
|--------|-------------|---------------------------------|
| PAY-01 | Plans catalog tables exist; users.plan_id FK | §8 (GORM models), §9 (migration test) |
| PAY-02 | POST /api/v1/checkout | §1.1 (lava CreateInvoice), §12.3 (Client.CreateInvoice), §5.2 (route mount) |
| PAY-03 | POST /api/v1/webhook/lava handles 5 event types | §1.5 (webhook payloads), §3.4 (handler skeleton) |
| PAY-04 | Idempotent — UNIQUE rejects duplicates, 200 no-op | §3.1 (OnConflict{DoNothing}), §4 (expression index) |
| PAY-05 | 500 on processing errors so lava retries | §3.4 (handler returns 500 on tx error) |
| PAY-06 | IP allowlist via Fiber TrustedProxies; never read X-Forwarded-For directly | §2 (Fiber behavior + LavaWebhookIPAllowlist middleware) |
| PAY-07 | crypto/subtle.ConstantTimeCompare for X-Api-Key | §12.4 (VerifyAPIKey) |
| PAY-08 | Tier derived from offerId via plan_offers, never client metadata | §1.5 (contractId-based resolution chain) |
| PAY-09 | subscription_expires_at from webhook period_end | §1.2 (subscriptionDetails.expiredAt), §3.3 (SetUserPlan) |
| PAY-10 | POST /api/v1/subscription/cancel calls lava DELETE; user keeps Pro | §1.4 (DELETE endpoint), §12 (subscription.go) |
| PAY-11 | Server access enforced at repo layer; admins bypass | §8 (plan_repo.go), §5 (handler role branch) |
| PAY-12 | GET /api/v1/plans public, currency-aware | §6 (Redis cache pattern), §1.3 (currency derivation) |
| PAY-13 | Admin CRUD plans | §5.2 (route registration), §10 (admin-web UI) |
| PAY-14 | Admin manage plan-servers via PUT/POST/DELETE | §5.2, §10 (PlanServersPicker) |
| PAY-15 | Admin manage offers + replace for price versioning | §3 (GORM tx replace), §10 (ReplaceOfferDialog) |
| PAY-16 | Lava client in internal/lava/; hardcoded base URL; 5s timeout; no SSRF | §12 (package layout + D-15 const verification) |

**Coverage:** all 16 requirements are addressed by at least one research section. Zero gaps.

---

## Validation Architecture

> Nyquist Dimension 8 — workflow.nyquist_validation=true in `.planning/config.json`. This section is REQUIRED.

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Go's standard `testing` package + `gorm.io/driver/sqlite` (existing in-memory pattern) + `github.com/testcontainers/testcontainers-go` (NEW — for migration test and integration test) |
| Test config file | none — tests live alongside source as `*_test.go` |
| Quick run command (per task commit) | `go test ./internal/... -count=1 -timeout=60s` |
| Full suite command (per wave merge) | `go test ./... -race -count=1 -timeout=300s` |
| Integration test (manual, Wave 5) | `go test -tags=integration ./server/api/integration/... -run TestLavaSandbox` (manual, only operator runs against lava sandbox) |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|--------------|
| PAY-01 | plans/plan_servers/plan_offers tables created; users.plan_id NOT NULL after migration; tier coercion premium→pro destruction-free | migration | `go test ./server/api/migrations/ -run TestMigrations019_020 -count=1` | ❌ Wave 1 (`03-01`) |
| PAY-02 | POST /checkout returns paymentUrl + invoice_id; 409 on offer_not_configured (lava_offer_id NULL) | handler unit (mock lava via httptest) | `go test ./internal/handler/ -run TestCreateCheckoutSession` | ❌ Wave 3 (`03-05`) |
| PAY-03 | Webhook dispatches all 5 event types to correct branches | handler unit | `go test ./internal/handler/ -run TestHandleLavaWebhook_AllEvents` | ❌ Wave 3 (`03-06`) |
| PAY-04 | 20 duplicates → 1 side effect, 19 noops (RowsAffected==0 on conflict) | repository + handler unit | `go test ./internal/repository/ -run TestInsertWebhookEventIfNew_Idempotent` and `go test ./internal/handler/ -run TestHandleLavaWebhook_DuplicateNoop` | ❌ Wave 3 (`03-06`) |
| PAY-05 | Induced DB failure during processing → 500 (lava retries) | handler unit | `go test ./internal/handler/ -run TestHandleLavaWebhook_ProcessingError_Returns500` | ❌ Wave 3 (`03-06`) |
| PAY-06 | Request from IP outside allowlist rejected at LavaWebhookIPAllowlist middleware | middleware unit | `go test ./internal/middleware/ -run TestLavaWebhookIPAllowlist` | ❌ Wave 3 (`03-06`) |
| PAY-07 | ConstantTimeCompare timing-safe — fuzz/property test for non-leakage | lava unit | `go test ./internal/lava/ -run TestVerifyAPIKey_ConstantTime` | ❌ Wave 1 (`03-02`) |
| PAY-08 | Tier derived ONLY from offerId via plan_offers reverse-lookup; client `plan` field in body is ignored | handler unit | `go test ./internal/handler/ -run TestHandleLavaWebhook_TierFromOfferIDNotClient` | ❌ Wave 3 (`03-06`) |
| PAY-09 | subscription_expires_at populated from period_end on first payment.success and extended on recurring | handler integration (sqlite) | `go test ./internal/handler/ -run TestHandleLavaWebhook_ExpiresAt_FirstAndRenewal` | ❌ Wave 3 (`03-06`) |
| PAY-10 | POST /subscription/cancel calls lava DELETE; user.subscription_tier unchanged until cron | handler unit (mock lava) | `go test ./internal/handler/ -run TestCancelSubscription_KeepsProUntilExpiry` | ❌ Wave 3 (`03-05`) |
| PAY-11 | ListServersForPlan filters correctly; admin bypass returns all active | repository + handler unit | `go test ./internal/repository/ -run TestListServersForPlan` and `go test ./internal/handler/ -run TestListServers_AdminBypass` | ❌ Wave 2 (`03-03`+`03-04`) |
| PAY-12 | GET /plans returns active plans; currency derivation; cache hit/miss | handler unit (with miniredis) | `go test ./internal/handler/ -run TestListPlansPublic_CacheHitMissBust` | ❌ Wave 3 (`03-07`) |
| PAY-13 | Admin CRUD validates is_system immutable, refuses delete with active users without force | handler unit | `go test ./internal/handler/ -run TestAdminPlansCRUD` | ❌ Wave 4 (`03-08`) |
| PAY-14 | PUT/POST/DELETE /admin/plans/:id/servers/... behave per ADR §19.7.6 | handler unit | `go test ./internal/handler/ -run TestAdminPlanServers` | ❌ Wave 4 (`03-08`) |
| PAY-15 | POST /admin/plans/:id/offers/:offer_id/replace deactivates old + inserts new in one tx | handler unit | `go test ./internal/handler/ -run TestAdminReplaceOffer_Transactional` | ❌ Wave 4 (`03-08`) |
| PAY-16 | Lava client uses hardcoded BaseURL + 5s timeout + no redirect follow | lava unit + smoke | `go test ./internal/lava/ -run TestClient_HardcodedBaseURL_5sTimeout_NoRedirect` AND `grep -rn 'lava.BaseURL\|"https://gate.lava.top"' server/api/internal/lava/ \| ...` | ❌ Wave 1 (`03-02`) |

### Sampling Rate

- **Per task commit:** `go test ./internal/{package-touched}/...` — run only the package's tests (~5-10s).
- **Per wave merge:** `go test ./... -race -count=1 -timeout=300s` — full suite (~60-90s on a modern Mac).
- **Phase gate (before `/gsd-verify-work`):**
  - `go test ./... -race -count=1 -timeout=300s` — green.
  - `go vet ./...` — clean.
  - `grep -rn 'PlanLimits' server/api/internal/ server/api/cmd/` — ZERO hits outside `model/subscription.go` constants and the migration test.
  - `grep -rn 'StripeID' server/api/internal/handler/` — ZERO hits outside `payment_test.go` (orphaned per D-03).
  - Manual sandbox integration test on operator's machine (`go test -tags=integration ./...`).
- **Admin-web (TypeScript):** `npm run lint && tsc --noEmit && npm run build` in `admin-web/` for each Wave 5 plan.

### Wave 0 Gaps

Tests/infrastructure that must exist BEFORE Wave 1 implementations land:

- [ ] `server/api/migrations/migrations_test.go` — testcontainers Postgres harness (covers PAY-01)
- [ ] `go.mod` add: `github.com/testcontainers/testcontainers-go`, `github.com/testcontainers/testcontainers-go/modules/postgres`, `gorm.io/datatypes` (if used for jsonb)
- [ ] `internal/lava/client_test.go` — `newWithBaseURL` helper for httptest mocking (covers PAY-02, PAY-07, PAY-16 base)
- [ ] `internal/middleware/lava_ip_allowlist_test.go` — pure-function test of CIDR parsing + IP matching (covers PAY-06)
- [ ] `internal/repository/plan_repo_test.go` — sqlite-backed tests for `FindPlanByID`, `FindPlanByCode`, `ListServersForPlan`, `IsServerAllowedForPlan`, `SetUserPlan` (covers PAY-11)
- [ ] `internal/cache/plans_cache_test.go` — miniredis-backed test for cache wrappers (covers PAY-12)
- [ ] `admin-web/src/api/plans.ts` and `admin-web/src/api/lava.ts` need typed mock fetchers for unit tests (admin-web doesn't currently have any unit tests — establishing the pattern in Phase 3 may be deferred to Phase 7 or limited to type-check + lint + build)
- [ ] `internal/handler/plans_public_test.go`, `plans_admin_test.go`, `admin_lava_test.go`, `webhook_lava_test.go` — new test files for each new handler file (covers PAY-02..05, 08, 10, 12, 13, 14, 15)

*If no gaps existed: not the case here — Phase 3 is a large feature and explicitly creates new test files. All listed gaps must be addressed in Wave 0 of their respective parent waves.*

---

## Security Domain

> ASVS L2 on payment paths (D-31), L1 elsewhere. Per CONTEXT.md D-32, each PLAN.md gets an inline `<threat_model>` block covering 4 categories where applicable.

### Applicable ASVS Categories (per CONTEXT.md D-31)

| ASVS Category | Applies | Standard Control |
|---------------|---------|------------------|
| V2 Authentication | yes (admin endpoints inherit from AuthRequired + AdminRequired) | Existing JWT + bcrypt admin login (Phase 1) — unchanged this phase |
| V3 Session Management | yes (logout + refresh paths unchanged this phase) | Phase 2 patterns — JWT blacklist, transactional refresh rotation |
| V4 Access Control | yes | Admin route group inherits middleware (D-32 admin abuse mitigations); repo-layer server-access enforcement (D-21) |
| V5 Input Validation | yes (L2 on payment paths) | `c.BodyParser` + per-field whitelist validation in handlers; for plan_code/code regex; for currency enum; for periodicity enum; for amount ≥ 0 |
| V6 Cryptography | yes (L2 on payment paths) | `crypto/subtle.ConstantTimeCompare` for X-Api-Key (PAY-07); HS256 JWT (existing); `gen_random_uuid()` (Postgres CSPRNG); NEVER hand-roll |
| V7 Error Handling & Logging | yes | HOTFIX-04 ErrorHandler scrub already in place — 5xx returns `{error:"internal server error", request_id}`; 4xx keeps verbose. Apply to new handlers. |
| V8 Data Protection | yes (L2 on payment paths) | API key NEVER logged or echoed in error paths (verified by code review); user email is PII, treat per Phase 2 D-05 |
| V9 Communication | yes (L2 on payment paths) | TLS verification ON for outbound lava calls (no `InsecureSkipVerify` — D-14 explicit); HTTPS-only deploys |
| V10 Malicious Code | yes | govulncheck in CI (Phase 8 HARD-09) — out of scope this phase |
| V11 Business Logic | yes (L2 on payment paths) | Tier derived from offerId via DB lookup ONLY (PAY-08); 60s checkout idempotency; soft-delete plans grandfathered; replace-offer in tx |
| V12 Files & Resources | n/a | No file uploads this phase |
| V13 API & Web Service | yes (L2 on payment paths) | Per-endpoint AuthRequired + AdminRequired wiring; IP allowlist on webhook (PAY-06) |
| V14 Configuration | yes (L2) | HOTFIX-08 fail-fast env validator extended for LAVA_* (D-30); hardcoded base URL (D-15); no env-driven SSRF surface (D-32 §3) |

### Known Threat Patterns for {Go + Fiber + GORM + Postgres + Redis + lava.top + admin-web}

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|----------------------|
| SQL injection via plan_code or currency query param | Tampering | GORM parameterized queries (already pattern in `repository/*.go`); regex validation on `plan_code`; enum whitelist on `currency` |
| JSON injection / mass assignment via /admin/plans body | Tampering | Explicit field whitelist in `BodyParser`; never bind directly into the model — use per-handler request struct |
| Webhook replay (lava 20-retry) | Spoofing/Repudiation | UNIQUE on `(event_type, contract_id, payload->>'timestamp')` + GORM `OnConflict{DoNothing}` + tx isolation (PAY-04) |
| X-Forwarded-For spoof to bypass IP allowlist | Spoofing | Route-scoped middleware reads `c.Context().RemoteIP()` (TCP layer), NOT `c.IP()` (PAY-06, §2.2) |
| X-Api-Key timing attack | Information disclosure | `crypto/subtle.ConstantTimeCompare` for both current + previous secrets (PAY-07, §12.4) |
| SSRF via client-supplied URLs in lava client | Information disclosure | Hardcoded `const BaseURL`; smoke test greps for "https://gate.lava.top" (PAY-16, D-15) |
| Client supplies their own `plan` field to elevate tier | Elevation of privilege | Tier derived ONLY from `offerId` via DB lookup (PAY-08); /checkout body's `plan_code` is validated against `plans.code` but the tier-grant happens through webhook handler reading `plan_offers.plan_id`, not from /checkout body |
| Admin sets `is_system=true` on a plan via API | Elevation of privilege | `is_system` is not in the create/update request structs; only the migration's INSERT sets it |
| Admin force-deletes system plan to lock out all users | DoS | `/admin/plans/:id` DELETE returns 403 when `is_system=true`, even with `?force=true` (D-32 §4) |
| Race between admin force-cancel and webhook payment.success | Tampering | Phase 7 ADMIN-03 adds per-user advisory lock. Phase 3 documents the gap; uses transactional UPSERT as best-effort. |
| Concurrent /checkout taps double-charge | Tampering | 60s idempotency window: reuse existing `pending` invoice for same user+offer (ADR §9.2) |
| Plan deletion cascade kills paying users immediately | Tampering | Soft delete only (`is_active=false`); FK never broken; cron flips to system plan at expiry (D-23, D-26) |
| API key exposed in admin-web bundle | Information disclosure | `/admin/lava/products` proxy endpoint uses SERVER-SIDE `LAVA_API_KEY`; key never reaches browser (D-12) |
| Plan offer amount < lava floor ($5 / €5) silently creates a non-payable offer | Business logic | UI warning only — admin owns the decision (per ADR §19.13 Tab 3); backend doesn't enforce floor |
| Lava sandbox key used in production by mistake | Confidentiality / Integrity | `LAVA_ENV` flag explicit; production startup demands `LAVA_API_KEY` non-empty (§13.2) |
| Webhook handler 500s on transient DB error — does lava retry hammer the DB? | Availability | Global per-IP rate limiter (HOTFIX-03) is bypassed for webhook (D-20) — relying on lava's 20-retry exponential backoff as a soft circuit-breaker. **Risk noted, not mitigated; planner documents.** |

---

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go runtime | All backend work | ✓ | go1.26.1 (vs locked 1.25.0 in go.mod — newer is fine, planner doesn't bump) | — |
| Docker | testcontainers (migration test, eventually) | ✓ | 29.2.0 | — |
| PostgreSQL 16 client | (only needed during testcontainers spin-up) | indirect (via Docker image) | — | — |
| Redis 7 client | (miniredis is used in-tests — already in go.mod) | indirect (`alicebob/miniredis/v2 v2.37.0` in go.mod) | v2.37.0 | — |
| Node.js | admin-web build + tests | ✓ | v25.8.1 (npm 11.11.0) | — |
| `testcontainers-go` | Migration test (NEW) | ✗ (not in go.mod yet) | will be added by Wave 1 | — |
| `gorm.io/datatypes` | jsonb column for `lava_webhook_events.payload` (optional) | ✗ (not in go.mod yet) | will be added by Wave 1 OR use `json.RawMessage` | `json.RawMessage` directly |
| lava.top sandbox API key | Wave 5 integration test (D-06) | ✗ (operator must provide before Wave 5 runs) | — | Wave 5 falls back to mocked-payload tests; manual sandbox test deferred until operator provides key |
| @radix-ui/react-{checkbox,select,switch,tabs,tooltip} | Wave 5 admin-web | ✗ (not in package.json) | will be added by Wave 5 | — |

**Missing dependencies with no fallback:**
- lava.top sandbox API key (operator-supplied; documented as a Wave 5 gate)

**Missing dependencies with fallback:**
- `gorm.io/datatypes` → `json.RawMessage` (decision deferred to planner)

---

## Open Questions for Planner

Bounded by CONTEXT.md "Claude's Discretion":

1. **Wave 5 plan boundary granularity.** D-04 suggests `03-10` admin-web UI + `03-11` docs + sandbox test. With the shadcn install + 2 pages + ~8 components + ReplaceOfferDialog + LavaOfferPicker the admin-web work is heavier than 1 plan. **Recommendation: split into `03-10` (shadcn install + plans.ts API client + Plans listing page + DeletePlanDialog), `03-11` (PlanDetail with three tabs + ReplaceOfferDialog + LavaOfferPicker), `03-12` (API contract doc `docs/lava-payments-api.md` + grep smoke + sandbox integration test).** Planner adjudicates.

2. **GORM `OnConflict` clause shape for `lava_contracts`.** D-19 + D-18 leave the clause to the planner. Recommended: `clause.OnConflict{Columns:[{Name:"contract_id"}], DoUpdates:clause.AssignmentColumns([]string{"is_active","expires_at","cancelled_at","parent_contract_id"})}`. See §3.2.

3. **`subscriptions` table partial UNIQUE on `(user_id) WHERE is_active=true`.** Optional — see §3.3 option (a) vs (b). Recommendation: option (a) — don't add the index, keep `CreateOrUpdateSubscription` pattern.

4. **`gorm.io/datatypes` vs `json.RawMessage` for jsonb column.** See §8.5 — both work. Recommendation: `json.RawMessage` (no new dependency).

5. **`LAVA_WEBHOOK_ALLOWED_CIDRS` strict-required vs default.** §13.3 recommends strict-required.

6. **`LAVA_ENV` selection logic.** §13.3 recommends explicit `LAVA_ENV=sandbox|production` flag.

7. **Lava webhook payload field name for `subscription.cancelled.timestamp`.** §1.5 caveat — recommendation: COALESCE expression in UNIQUE index.

8. **Compute `expires_at` locally vs fetch via lava on first `payment.success`.** §1.2 — recommendation: compute locally from `started_at + periodicity` math; only use lava's `GET /api/v2/invoices/{id}` for the escalate path.

9. **Admin-web admin-login flow during Phase 3.** Confirm: stays password-based (Phase 1's admin-login). No SSO for admins. Discretion noted in CONTEXT.md.

---

## Risks & Mitigations

| # | Risk | Severity | Mitigation |
|---|------|----------|------------|
| 1 | Lava webhook payload field names differ from OpenAPI spec when real events arrive (sandbox vs production differences, undocumented fields) | High | Wave 1 `03-02` adds a one-shot sandbox probe: trigger a real test payment in sandbox, capture the webhook payload, pin DTO struct tags from real data. Mark TODO in `internal/lava/dto.go` to update after sandbox probe. |
| 2 | Fiber `EnableTrustedProxyCheck=true` alone does NOT reject untrusted IPs — would silently pass webhook traffic from spoofers | High | Route-scoped `LavaWebhookIPAllowlist` middleware (§2.2) — reads `c.Context().RemoteIP()` (TCP layer), 403s outside allowlist. Combined with global TrustedProxies config. |
| 3 | `subscription.cancelled` events break idempotency UNIQUE (no `timestamp` field) | Medium | COALESCE expression in CREATE UNIQUE INDEX (§4.2). Tested with cancellation events in Wave 3 webhook tests. |
| 4 | `gen_random_uuid()` requires Postgres 13+ — CLAUDE.md locks 16, fine. SQLite test schemas can't replicate (use `lower(hex(randomblob(16)))` or just `''` PK) | Low | Existing test pattern (SQLite for repo tests, Postgres testcontainers for migration tests) already handles this. Migration test (Wave 1 `03-01`) uses Postgres explicitly. |
| 5 | Admin-web shadcn install pull a Radix update that breaks existing components | Medium | Pin to current Radix versions (`@radix-ui/react-dialog ^1.1.6` in package.json), add new components at matching `^1.x` versions. Visual regression check on `Dialog` after install. |
| 6 | `payment_test.go` references `model.Subscription.StripeID` AFTER D-01 removes the field, but D-03 says "do not touch" → test compile fails | Medium | D-03 explicitly says payment_test.go is "orphaned this phase" — but if it doesn't compile, **the whole package's tests don't compile**, breaking unrelated tests. **Mitigation: planner verifies during Wave 1 that `payment_test.go` either uses test-local DDL string + a `model.Subscription` with `StripeID` (need to check) or already builds its own local Stripe-fixture struct.** Reading `payment_test.go:112` shows `StripeID: stripeID` written to the model — this WILL break. **Action: Wave 1 deletes `payment_test.go` or stubs it** (re-reading D-03 carefully: "Phase 3 does not touch payment_test.go Stripe tests — they become orphaned for one milestone, then deleted." Implies the test file is deleted in Phase 3 OR the deletion is conditional on compile-failure. Planner re-reads D-03 + D-11 and decides whether the deletion lands in Phase 3 or Phase 8.) **Recommendation: planner adds a `03-12` task that deletes `payment_test.go` if and only if it fails to compile after `model/subscription.go` rewrite.** |
| 7 | `repository/subscription_repo.go::FindSubscriptionByStripeID` and `CreateOrUpdateSubscription` reference `stripe_id` column that's been dropped | Medium | Wave 1 also rewrites `subscription_repo.go` — delete `FindSubscriptionByStripeID` (unreferenced after `payment.go` rewrite); rewrite `CreateOrUpdateSubscription` to use `lava_contract_id`. Add to `03-01` migration plan or split into a `03-01b` repository-cleanup plan. |

---

## Sources

### Primary (HIGH confidence)
- **`/Users/abdunabi/Desktop/vpn/.planning/phases/03-lava-top-plans-catalog/03-CONTEXT.md`** — the 32-decision contract for this phase
- **`/Users/abdunabi/Desktop/vpn/docs/ADR-007-lava-sso-rework.md` §1-§18 + §19** — architectural source of truth; §19 dynamic-plans amendment
- **`/Users/abdunabi/Desktop/vpn/.planning/REQUIREMENTS.md`** — PAY-01..PAY-16 acceptance criteria
- **`/Users/abdunabi/Desktop/vpn/.planning/phases/02-auth-sso-backend/02-CONTEXT.md`** — Phase 2 patterns (threat-model inline block, migration numbering, verifier package layout)
- **`/Users/abdunabi/Desktop/vpn/.planning/phases/01-hotfix-audit-critical-fixes/01-CONTEXT.md`** — HOTFIX-08 fail-fast validator pattern (D-30 baseline)
- **`https://gate.lava.top/docs/documentation.yaml`** (via WebFetch) — lava.top OpenAPI 1.17.0; verified request/response shapes for `/api/v3/invoice`, `/api/v2/invoices/{id}`, `/api/v2/products`, `/api/v1/subscriptions`, and all 5 webhook event payloads
- **`https://raw.githubusercontent.com/gofiber/fiber/v2/app.go`** (via WebFetch) — Fiber v2 source confirming `EnableTrustedProxyCheck` "WON't" reject behavior (header-ignore, not request-reject)
- **Current code anchors (verified by direct Read):**
  - `/Users/abdunabi/Desktop/vpn/server/api/cmd/main.go` — route registration, middleware order, audit middleware mount
  - `/Users/abdunabi/Desktop/vpn/server/api/internal/handler/payment.go` (current Stripe shape — gets REWRITTEN)
  - `/Users/abdunabi/Desktop/vpn/server/api/internal/handler/servers.go:114` (PlanLimits[tier] slicing — REPLACE)
  - `/Users/abdunabi/Desktop/vpn/server/api/internal/handler/health.go:45,55` (PlanLimits reads)
  - `/Users/abdunabi/Desktop/vpn/server/api/internal/handler/connection.go:97,100` (PlanLimits[tier])
  - `/Users/abdunabi/Desktop/vpn/server/api/internal/handler/admin.go:137` (PlanLimits validation)
  - `/Users/abdunabi/Desktop/vpn/server/api/internal/handler/auth.go:574` (generateTokens)
  - `/Users/abdunabi/Desktop/vpn/server/api/internal/middleware/auth.go` (JWT middleware with PostgreSQL user re-read)
  - `/Users/abdunabi/Desktop/vpn/server/api/internal/middleware/audit.go` (admin route group audit middleware)
  - `/Users/abdunabi/Desktop/vpn/server/api/internal/model/subscription.go` (PlanLimits map — DELETE)
  - `/Users/abdunabi/Desktop/vpn/server/api/internal/model/user.go` (existing User struct, Phase 2 SSO fields)
  - `/Users/abdunabi/Desktop/vpn/server/api/internal/cache/redis.go` (existing wrapper API — Lua INCR script + BlacklistToken/IsTokenBlacklisted)
  - `/Users/abdunabi/Desktop/vpn/server/api/internal/scheduler/scheduler.go` (existing scheduler — add runExpiryDowngrade)
  - `/Users/abdunabi/Desktop/vpn/server/api/internal/config/config.go` (HOTFIX-08 RequireEnv pattern)
  - `/Users/abdunabi/Desktop/vpn/server/api/internal/repository/user_repo.go` (Phase 2 SSO repository functions)
  - `/Users/abdunabi/Desktop/vpn/server/api/internal/repository/subscription_repo.go` (existing — gets stripe_id references removed)
  - `/Users/abdunabi/Desktop/vpn/server/api/go.mod` (current dependencies — Go 1.25)
  - `/Users/abdunabi/Desktop/vpn/admin-web/package.json` (admin-web deps: Vite, React 19, TanStack Query 5.66, Radix 1.1.6, react-router 7)
  - `/Users/abdunabi/Desktop/vpn/admin-web/src/api/client.ts` (axios + interceptor + token refresh)
  - `/Users/abdunabi/Desktop/vpn/admin-web/src/api/servers.ts` (api file pattern)
  - `/Users/abdunabi/Desktop/vpn/admin-web/src/pages/Servers.tsx` (TanStack Query + mutation pattern)
  - `/Users/abdunabi/Desktop/vpn/admin-web/src/App.tsx` (react-router routing)
  - `/Users/abdunabi/Desktop/vpn/admin-web/src/components/layout/AdminLayout.tsx` (sidebar navItems — add Plans entry here)
  - `/Users/abdunabi/Desktop/vpn/admin-web/src/components/ui/` (shadcn inventory — 10 components installed)

### Secondary (MEDIUM confidence)
- **GORM v2 docs + GitHub issues** (via WebSearch) — `clause.OnConflict{DoNothing:true}` PostgreSQL behavior + `RowsAffected==0` detection pattern. Cross-verified with GORM issue #6554 (MySQL caveat — N/A here, we're on Postgres).
- **Fiber v2 GitHub source via WebFetch** — `app.go` field doc comments quoted directly.

### Tertiary (LOW confidence — flagged for planner)
- **Lava.top webhook payload field names for `subscription.cancelled`** (no `timestamp` field in OpenAPI spec — recommendation: COALESCE + sandbox probe before Wave 3 lands).
- **Lava.top sandbox base URL** (no documented separate URL; assumed same `gate.lava.top` with sandbox API key per D-06).
- **Lava.top test card numbers** (not in public docs; operator must supply).
- **lava.top 8% commission + $5/€5 minimum** (from CLAUDE.md; not independently verified against lava docs this session — but matches faq.lava.top excerpts).

---

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — Go 1.25 + Fiber v2 + GORM + Postgres 16 + Redis 7 are locked and verified in go.mod / go.sum / cmd/main.go.
- Architecture: HIGH — CONTEXT.md's 32 decisions are unambiguous; ADR-007 §19 is comprehensive; only ~7 small Claude's Discretion items remain.
- Lava.top API surface: HIGH for endpoint paths/shapes, MEDIUM for webhook payload field name consistency between OpenAPI spec and real sandbox events (recommendation: sandbox probe in Wave 1 `03-02`).
- Fiber TrustedProxies behavior: HIGH — verified directly from source.
- GORM patterns: HIGH — verified from existing repo code + GORM v2 docs.
- Pitfalls: HIGH — Stripe leakage check completed (§14); migration test approach scoped (§9).
- admin-web UI: HIGH — current structure mapped, shadcn gaps identified.

**Research date:** 2026-05-23
**Valid until:** 2026-06-23 (stable target — lava.top API is at 1.17.0 with no major-version migration imminent; Fiber v2 is at 2.52.5 and the v3 transition is non-blocking for this phase).

---

## RESEARCH COMPLETE

**Phase:** 3 — Lava.top + plans catalog
**Confidence:** HIGH overall (one MEDIUM-confidence area flagged: lava webhook payload field names — sandbox probe recommended in Wave 1).

### Key Findings
1. **Fiber `EnableTrustedProxyCheck` does NOT reject untrusted IPs** — it silently ignores their proxy headers. PAY-06 requires a dedicated `LavaWebhookIPAllowlist` route-scoped middleware that calls `c.Context().RemoteIP()` and 403s on miss. This is a critical architectural gap the planner must close (§2).
2. **Lava.top webhook payload for `subscription.cancelled` has no `timestamp` field** — the D-10 UNIQUE constraint must use `COALESCE((payload->>'timestamp'), (payload->>'cancelledAt'))` to cover both shapes (§1.5, §4.2).
3. **Recurring renewal events carry `parentContractId`** — the planner uses this to populate `lava_contracts.parent_contract_id` and the reverse-lookup chain to find the original user (§1.5).
4. **`payment_test.go` will NOT compile after `model.Subscription.StripeID` is removed** (line 112 writes `StripeID: stripeID`). D-03 says "do not touch" — but compile failure cascades. Planner must either delete the file in Wave 1 OR stub it; this is a hidden D-03 / D-01 cross-decision conflict the planner should resolve early (§14, Risk #6).
5. **`subscription_repo.go::FindSubscriptionByStripeID` + `CreateOrUpdateSubscription`'s `"stripe_id"` field reference** will also fail after D-11 drops the column. Wave 1 must include a `subscription_repo.go` cleanup alongside the migration (§14, Risk #7).
6. **Admin-web needs 7 new shadcn components** (`Form`, `Select`, `Combobox`, `Checkbox`, `Switch`, `Tabs`, `Tooltip`, `Textarea`) before Wave 5 plans-UI work can start (§10.5).
7. **`gen_random_uuid()` requires Postgres 13+**, present on locked 16 — but SQLite test schemas can't replicate this. Migration test (Wave 1 `03-01`) MUST use Postgres testcontainers; existing repo unit tests stay on SQLite for non-migration paths (§9).

### File Created
`/Users/abdunabi/Desktop/vpn/.planning/phases/03-lava-top-plans-catalog/03-RESEARCH.md`

### Confidence Assessment

| Area | Level | Reason |
|------|-------|--------|
| Lava.top API endpoint shapes | HIGH | Verified directly from `gate.lava.top/docs/documentation.yaml` |
| Lava.top webhook payload fields | MEDIUM | OpenAPI spec is descriptive but doesn't enumerate every field for every event; recommend Wave 1 sandbox probe to pin DTO struct tags |
| Fiber v2 TrustedProxies | HIGH | Verified directly from fiber/v2 `app.go` source |
| GORM v2 OnConflict + transactions | HIGH | GORM docs + existing repo patterns confirm `RowsAffected==0` for DoNothing-on-conflict on Postgres |
| Code anchors (current source) | HIGH | All files read directly; line numbers verified |
| admin-web stack | HIGH | package.json + existing components mapped |
| Stripe leakage post-D-11 | HIGH | grep verified 4 test files reference stripe_id (in DDL strings only) + 1 test file references `model.Subscription.StripeID` field (will break) |

### Open Questions for Planner
See §"Open Questions for Planner" — 9 items, all bounded by CONTEXT.md "Claude's Discretion".

### Ready for Planning
Research complete. The planner has:
- Concrete request/response DTOs for all 4 lava endpoints + all 5 webhook event shapes.
- A correctness fix for PAY-06 (route-scoped IP allowlist middleware) that wouldn't be apparent from CONTEXT.md alone.
- GORM v2 idiomatic snippets for `OnConflict`, transactional `SetUserPlan`, and idempotency-detection via `RowsAffected`.
- A precise file inventory for `internal/lava/` package + `internal/model/` new models + admin-web new files.
- A complete Validation Architecture section covering all 16 PAY-XX requirements with specific test files and commands.
- A complete threat-model basis for D-32's inline blocks across all PLAN.md files.
- Risk-7 surfaces a hidden D-03 ↔ D-01 conflict (payment_test.go compile failure) that needs early planner attention.
