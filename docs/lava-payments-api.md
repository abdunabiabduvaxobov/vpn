# Lava.top Payments + Dynamic Plans Catalog — API Contract

**Phase:** 3 — lava-top-plans-catalog
**Status:** Stable. Phase 4 (landing checkout) and Phase 5 (mobile foreground refresh) code against this contract.
**Source of truth:** This document. If implementation diverges, file an issue and fix the divergence.

Base URL: `https://api.risevpn.com/api/v1` (production), `http://localhost:8080/api/v1` (dev).

All requests/responses use `Content-Type: application/json`. Empty-body responses (204) carry no body.

Requirements addressed: **PAY-01..PAY-16**. Implementation lands across plans 03-01..03-11.

---

## Quick map

| Group | Routes | Auth |
|-------|--------|------|
| Public  | `GET /plans` | none |
| Authenticated | `POST /checkout`, `GET /invoices/:id`, `POST /subscription/cancel` | Bearer JWT |
| Webhook | `POST /webhook/lava` | IP allowlist + `X-Api-Key` |
| Admin (lava proxy) | `GET /admin/lava/products` | Bearer JWT + admin role |
| Admin (plans CRUD) | 13 routes under `/admin/plans/*` | Bearer JWT + admin role |

Total: **18 endpoints** documented below.

---

## 1. Public endpoint

### `GET /api/v1/plans`

Dynamic plan catalog for landing (`/pricing`) and mobile (informational, no IAP). PAY-12, D-27.

**Auth:** none. (`SkipRule` on AppVersion + JWT middleware in `cmd/main.go`.)

**Query parameters:**

| Name | Type | Required | Notes |
|------|------|----------|-------|
| `currency` | string | no | One of `USD`, `EUR`, `RUB` (case-insensitive). When omitted, derived from `Accept-Language` (`ru*` → `RUB`, else `USD`). Invalid value → 400. |

**Success response — 200 OK:**

```json
{
  "data": {
    "currency": "USD",
    "plans": [
      {
        "code": "free",
        "name": "Free",
        "description": "Try RiseVPN risk-free",
        "max_devices": 1,
        "max_servers": 3,
        "speed_limit_mbps": 50,
        "is_system": true,
        "sort_order": 0,
        "server_countries": ["US", "DE", "NL"],
        "offers": []
      },
      {
        "code": "pro",
        "name": "Pro",
        "description": "Unlimited speed, all servers, up to 5 devices",
        "max_devices": 5,
        "max_servers": -1,
        "speed_limit_mbps": 0,
        "is_system": false,
        "sort_order": 10,
        "server_countries": ["US", "DE", "NL", "JP", "SG", "AU", "..."],
        "offers": [
          {"periodicity": "MONTHLY", "currency": "USD", "amount": 5.00},
          {"periodicity": "PERIOD_YEAR", "currency": "USD", "amount": 39.99}
        ]
      }
    ]
  }
}
```

Fields per plan:

- `code` — slug; primary key for `POST /checkout` body
- `max_devices` — `-1` means unlimited
- `max_servers` — `-1` means all servers (subject to `plan_servers` join)
- `speed_limit_mbps` — `0` means uncapped
- `is_system` — exactly ONE plan in the catalog has `is_system=true`; it is the default for guest users and survives delete attempts (D-32 §4)
- `server_countries` — derived from `plan_servers` join; UI shows flags

**Excluded from public response (admin-only):** `id`, `lava_offer_id`, `active_user_count`, `is_active=false` plans, `is_active=false` offers. PAY-12 / D-27.

**Cache:** `cache:plans:public:{currency}` in Redis, TTL 60s. Admin write handlers bust on success. Cache miss or Redis outage falls through to DB.

**Error responses:**

| Status | Body | When |
|--------|------|------|
| 400 | `{"error":"invalid currency"}` | `?currency=` not in `USD|EUR|RUB` |
| 500 | `{"error":"internal server error"}` | DB read failed |

---

## 2. Authenticated endpoints (Bearer JWT)

All require `Authorization: Bearer <access_token>` from a successful `/auth/apple`, `/auth/google`, or `/auth/guest` call (subject to per-route SSO requirement noted in each section).

### `POST /api/v1/checkout`

Create a lava.top invoice for the requested plan / periodicity / currency. PAY-02, D-09.

**Auth:** Bearer JWT. User MUST have `email` populated (i.e. signed in with Apple or Google). Guests get 403.

**Request body:**

| Field | Type | Required | Notes |
|-------|------|----------|-------|
| `plan_code` | string | yes | Plan slug from `GET /plans` (e.g. `"pro"`) |
| `periodicity` | string | yes | One of `ONE_TIME`, `MONTHLY`, `PERIOD_90_DAYS`, `PERIOD_180_DAYS`, `PERIOD_YEAR` |
| `currency` | string | yes | One of `USD`, `EUR`, `RUB` |
| `clientUtm` | object<string,string> | no | Pass-through to lava for attribution |

**Success response — 201 Created:**

```json
{
  "data": {
    "invoice_id": "550e8400-e29b-41d4-a716-446655440000",
    "lava_invoice_id": "11111111-2222-3333-4444-555555555555",
    "payment_url": "https://app.lava.top/checkout/...",
    "amount": 5.00,
    "currency": "USD"
  }
}
```

**Idempotency reuse — 200 OK** (double-tap within 60s with the same `(user, lava_offer_id)` returns the existing pending invoice; lava is NOT called again):

Same payload shape as above; status code is `200` not `201`.

**Error responses:**

| Status | Body | When |
|--------|------|------|
| 400 | `{"error":"invalid request body"}` | Malformed JSON |
| 400 | `{"error":"plan_code, periodicity, currency required"}` | Missing field |
| 400 | `{"error":"currency must be USD\|EUR\|RUB"}` | Bad currency |
| 400 | `{"error":"invalid periodicity"}` | Periodicity outside allowed set |
| 403 | `{"error":"sign in with Apple or Google before purchasing"}` | Guest user — must SSO first (T-03-32) |
| 404 | `{"error":"plan not found"}` / `{"error":"plan not active"}` | `plan_code` does not exist or is inactive |
| 404 | `{"error":"no active offer for plan/periodicity/currency"}` | No `plan_offers` row for that combination |
| 409 | `{"error":"offer_not_configured"}` | D-09: offer exists but `lava_offer_id IS NULL` (admin must populate via plan 03-10 UI) |
| 500 | `{"error":"internal server error"}` | DB error |
| 502 | `{"error":"payment provider unavailable"}` | lava.top returned an error |

**Side effects:**

- `invoices` row INSERTed with `status='pending'`, `lava_invoice_id`, `offer_id` (lava UUID), `plan_id` and `plan_offer_id` FKs (ADR §19.6).
- Server-side `lavaClient.CreateInvoice` call — `email` field populated from `users.email`, NEVER from request body.

---

### `GET /api/v1/invoices/:id`

Fetch the current status of an invoice the caller owns. Used by `/pay/success` polling on landing and the post-payment mobile foreground refresh. PAY-09, D-25, D-32 §2.

**Auth:** Bearer JWT. Caller MUST own the invoice (ownership check returns **404 not 403** on mismatch to avoid existence-leak per D-32 §2).

**Path params:** `:id` — internal `invoices.id` UUID (NOT `lava_invoice_id`).

**Query parameters:**

| Name | Type | Notes |
|------|------|-------|
| `escalate` | bool | When `true` AND local status is `pending`, the backend issues a server-side `GetInvoice` to lava.top and reconciles the local status (`paid` / `failed` / `cancelled`). Does NOT call `SetUserPlan` (webhook is authoritative — D-32 §2). |

**Success response — 200 OK:**

```json
{
  "data": {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "lava_invoice_id": "11111111-2222-3333-4444-555555555555",
    "status": "pending",
    "amount": 5.00,
    "currency": "USD",
    "plan": "pro",
    "periodicity": "MONTHLY",
    "created_at": "2026-05-23T10:15:00Z"
  }
}
```

`status` is one of `pending` | `paid` | `failed` | `cancelled`.

**Error responses:**

| Status | Body | When |
|--------|------|------|
| 404 | `{"error":"invoice not found"}` | Unknown id OR caller does not own the row (D-32 §2 — same 404 for both) |
| 500 | `{"error":"internal server error"}` | DB error |

**Side effects (escalate path only):**

- If lava reports a terminal status (`COMPLETED`, `FAILED`, `CANCELLED`), `invoices.status` is updated.
- `users.subscription_tier` is **NEVER** changed from this endpoint — webhook owns tier grant (D-32 §2 / T-03-35).
- Escalate failures (lava unreachable, parse error) are non-fatal — local data is returned with logged warning.

---

### `POST /api/v1/subscription/cancel`

Cancel the caller's active lava subscription (contract). User keeps Pro until `expires_at` lapses; the expiry cron (plan 03-09) is the sole downgrade writer. PAY-10, D-19.

**Auth:** Bearer JWT. User MUST have `email` populated. Guest with email=NULL gets 400.

**Request body:** none.

**Success response — 200 OK:**

```json
{
  "data": {
    "cancelled": true,
    "access_until": "2026-06-23T10:15:00Z"
  }
}
```

`access_until` is `lava_contracts.expires_at` at the moment of cancel — null if never set by a recurring webhook yet.

**Error responses:**

| Status | Body | When |
|--------|------|------|
| 400 | `{"error":"user has no email"}` | User row has `email IS NULL` |
| 404 | `{"error":"no active subscription"}` | No `lava_contracts` row with `is_active=true` for this user |
| 500 | `{"error":"internal server error"}` | DB error |
| 502 | `{"error":"payment provider unavailable"}` | lava DELETE failed; local row NOT modified |

**Side effects:**

- Server calls `DELETE https://gate.lava.top/api/v1/subscriptions?contractId=...&email=...`.
- On success: `lava_contracts.is_active = false`, `lava_contracts.cancelled_at = now()`.
- **`users.subscription_tier` is NOT changed.** PAY-10 contract: user keeps Pro until the expiry cron (plan 03-09) downgrades them when `expires_at < now()`. This matches lava.top's billing semantics (the user paid for the period).

---

## 3. Inbound webhook

### `POST /api/v1/webhook/lava`

lava.top delivers payment lifecycle events here. PAY-03..09, D-19.

**Auth (two gates):**

1. **IP allowlist** — route-scoped middleware reads TCP-layer `c.Context().RemoteIP()` (NOT `X-Forwarded-For` / `X-Real-IP` — see Security notes). `LAVA_WEBHOOK_ALLOWED_CIDRS` env var (CSV, e.g. `158.160.60.174/32`). Non-allowed IP → 403 `Forbidden`. PAY-06 / RESEARCH §2.4.
2. **`X-Api-Key` header** — constant-time compared against `LAVA_WEBHOOK_SECRET` AND (optionally during rotation) `LAVA_WEBHOOK_SECRET_PREVIOUS`. Mismatch → 401 `Unauthorized`. Uses `crypto/subtle.ConstantTimeCompare` (PAY-07).

`X-App-Version` middleware is bypassed for this route (`SkipRule` in `cmd/main.go`) — lava.top does not send that header.

**Request body (`Content-Type: application/json`):**

Discriminated by `eventType`. Common envelope (RESEARCH §1.5):

```json
{
  "eventType": "payment.success",
  "contractId": "uuid",
  "parentContractId": "uuid (renewals only)",
  "amount": 5.00,
  "currency": "USD",
  "timestamp": "2026-05-23T10:15:00Z",
  "status": "subscription-active",
  "errorMessage": "string (failed events only)",
  "product": {"id": "uuid", "title": "Pro"},
  "buyer":   {"email": "buyer@example.com"},
  "cancelledAt":  "2026-05-23T10:15:00Z (cancelled events only)",
  "willExpireAt": "2026-06-23T10:15:00Z (cancelled events only)"
}
```

**Five event types dispatched:**

| eventType | Action |
|-----------|--------|
| `payment.success` | Resolve `invoices.lava_invoice_id == contractId` → `FindOfferByLavaOfferID(invoice.offer_id)` → `FindPlanByID(offer.PlanID)` → `SetUserPlan(user_id, plan_id)`. UpsertLavaContract. Mark invoice `paid`. Sets `users.subscription_expires_at` from period boundary. **Tier derived from server-side offer → plan chain, NEVER from payload `product.title` / `product.id`** (PAY-08, T-03-41). |
| `subscription.recurring.payment.success` | Resolve parent contract via `parentContractId`. Extend `subscriptions.subscription_expires_at` AND `lava_contracts.expires_at` by one period (`periodicityToDuration`: `MONTHLY=30d`, `PERIOD_90_DAYS`, `PERIOD_180_DAYS`, `PERIOD_YEAR=365d`). UpsertLavaContract child with `parent_contract_id`. |
| `payment.failed` | Mark `invoices.status='failed'`. **No tier change** (initial payment failure does not affect already-Pro users; this is a new-checkout failure). Benign on missing invoice. |
| `subscription.recurring.payment.failed` | Single tx: `subscriptions.is_active = false` AND `lava_contracts.is_active = false`. **Tier waits for the expiry cron** (plan 03-09) to downgrade after `expires_at` lapses. D-19 literal coordination. |
| `subscription.cancelled` | `lava_contracts.cancelled_at = now()`, `lava_contracts.is_active = false`. **Tier untouched** (user keeps Pro until paid period ends — cron handles downgrade). Records `willExpireAt` if present. |
| (unknown) | Log warn, mark event `processed`, return 200. (lava.top may add events; we should not 500 on unknown types.) |

**Idempotency (PAY-04):**

- `lava_webhook_events` table has UNIQUE on `(event_type, contract_id, payload->>'timestamp')` (migration 020).
- Handler INSERTs via `clause.OnConflict{DoNothing: true}` and uses `RowsAffected > 0` to detect first-delivery vs duplicate.
- Duplicate (RowsAffected = 0) → 200 OK immediately WITHOUT re-applying side effects.
- Event row is committed in an independent transaction BEFORE processing — so a Step-4 processing failure can be retried without losing the dedup record (RESEARCH §3.4).

**Response codes (PAY-05):**

| Status | When |
|--------|------|
| 200 OK | First-delivery success OR duplicate event (idempotency hit) OR unknown eventType |
| 400 Bad Request | Body parse failed OR missing `eventType` / `contractId` |
| 401 Unauthorized | `X-Api-Key` mismatch |
| 403 Forbidden | Source IP not in `LAVA_WEBHOOK_ALLOWED_CIDRS` |
| 500 Internal Server Error | Processing failed (DB write, unresolvable contractId, etc). Event row is persisted with `error` populated for forensics; lava.top retries per its 20-attempt policy. |

**Important — 500 is the retry trigger.** Per CLAUDE.md "Webhook reliability" constraint, returning 500 is the intended path on processing error so lava's retries drive at-least-once delivery.

**Side effects per event type:** see table above. All event payloads are persisted to `lava_webhook_events.payload` (jsonb) before any other action for forensic recovery + Phase 7 webhook-replay UI (ADMIN-06).

---

## 4. Admin endpoints

All admin routes require `Authorization: Bearer <access_token>` from an admin user (`users.role = 'admin'`). The `AdminRequired` middleware re-reads the role from the DB on every request (no JWT-stale-claim window). All admin actions are recorded in `audit_log`.

### `GET /api/v1/admin/lava/products`

Server-side proxy to lava `GET /api/v2/products`, flattened into dropdown rows. Used by admin-web (plan 03-10) on plan-offer dialog mount. D-12 Option B (lava API key NEVER reaches the browser).

**Auth:** Bearer JWT + admin role.

**Request:** no params.

**Success response — 200 OK:**

```json
{
  "data": [
    {
      "productId":   "uuid",
      "productName": "Pro",
      "offerId":     "uuid",
      "offerName":   "Monthly",
      "periodicity": "MONTHLY",
      "currency":    "USD",
      "amount":      5.00
    }
  ]
}
```

One row per `(product, offer, price)` tuple — admin picks one and the resulting `offerId` is what `lava_offer_id` columns store.

**Error responses:**

| Status | Body | When |
|--------|------|------|
| 401 | (Bearer middleware response) | Missing/invalid JWT |
| 403 | (AdminRequired middleware response) | Non-admin caller |
| 502 | `{"error":"payment provider unavailable"}` | lava error (T-03-34 — no upstream body echoed; lava key never leaked through error message) |

---

### `GET /api/v1/admin/plans`

List all plans (active + inactive) with computed counts. PAY-13, ADR §19.7.1.

**Success — 200 OK:**

```json
{
  "data": [
    {
      "id": "uuid",
      "code": "free",
      "name": "Free",
      "description": "...",
      "max_devices": 1,
      "max_servers": 3,
      "speed_limit_mbps": 50,
      "is_active": true,
      "is_system": true,
      "sort_order": 0,
      "server_count": 3,
      "offer_count": 0,
      "active_user_count": 1247,
      "created_at": "2026-05-22T00:00:00Z",
      "updated_at": "2026-05-22T00:00:00Z"
    }
  ]
}
```

---

### `POST /api/v1/admin/plans`

Create a plan + initial servers + initial offers in one transaction. PAY-13.

**Request body:**

```json
{
  "code": "pro",
  "name": "Pro",
  "description": "...",
  "max_devices": 5,
  "max_servers": -1,
  "speed_limit_mbps": 0,
  "sort_order": 10,
  "server_ids": ["server-uuid-1", "server-uuid-2"],
  "offers": [
    {"periodicity": "MONTHLY",     "currency": "USD", "amount": 5.00,  "lava_offer_id": "uuid"},
    {"periodicity": "PERIOD_YEAR", "currency": "USD", "amount": 39.99, "lava_offer_id": null}
  ]
}
```

**Field rules:**

- `code` — regex `^[a-z0-9][a-z0-9_-]*$`, 1-40 chars
- `name` — 1-100 chars
- `max_devices` — `-1` (unlimited) or 1..1000
- `max_servers` — `-1` or 0..9999
- `speed_limit_mbps` — 0..100000
- `is_system` is **silently ignored** (D-32 §4 — only seed migrations can set `is_system=true`)
- `lava_offer_id` may be `null` (placeholder per D-09) — admin populates later via PATCH

**Success — 201 Created:** the created plan row (no nested server/offer arrays — fetch via `GET /admin/plans/:id` for that).

**Errors:**

| Status | Body | When |
|--------|------|------|
| 400 | `{"error":"<rule>"}` | Validation failure (code regex, name length, range checks) |
| 500 | `{"error":"create plan failed"}` | DB error |

Cache `cache:plans:public:*` is busted on success.

---

### `GET /api/v1/admin/plans/:id`

Full plan detail with server list + offer list + active user count.

**Success — 200 OK:**

```json
{
  "data": {
    "id": "uuid",
    "code": "pro",
    "name": "Pro",
    "description": "...",
    "max_devices": 5,
    "max_servers": -1,
    "speed_limit_mbps": 0,
    "is_active": true,
    "is_system": false,
    "sort_order": 10,
    "servers": [{"id": "uuid", "name": "us-east-1", "country": "US"}],
    "offers": [{"id": "uuid", "periodicity": "MONTHLY", "currency": "USD", "amount": 5.00, "lava_offer_id": "uuid", "is_active": true}],
    "active_user_count": 412,
    "created_at": "...",
    "updated_at": "..."
  }
}
```

**Errors:** 404 `{"error":"plan not found"}`, 500.

---

### `PATCH /api/v1/admin/plans/:id`

Update mutable fields. **`code` and `is_system` are absent from the request DTO — both immutable** (ADR §19.7.4, D-32 §4). Repository layer ALSO strips them (defence in depth).

**Request body** (all fields optional):

```json
{
  "name": "...",
  "description": "...",
  "max_devices": 5,
  "max_servers": -1,
  "speed_limit_mbps": 0,
  "sort_order": 10,
  "is_active": true
}
```

**Errors:**

| Status | Body | When |
|--------|------|------|
| 400 | `{"error":"<rule>"}` | Validation failure |
| 403 | `{"error":"cannot deactivate system plan"}` | `is_active=false` on the `is_system=true` plan |
| 404 | `{"error":"plan not found"}` | Unknown id |
| 500 | `{"error":"internal server error"}` | DB error |

Cache busted on success.

---

### `DELETE /api/v1/admin/plans/:id`

Soft delete (sets `is_active=false`). PAY-13.

**Query parameters:**

| Name | Type | Notes |
|------|------|-------|
| `force` | bool | When `true`, allows delete despite `active_user_count > 0` (those users will be re-resolved to the system plan by the entitlement layer). |

**Success — 200 OK:**

```json
{"data": {"id": "...", "deleted": true, "affected_users": 0}}
```

**Errors:**

| Status | Body | When |
|--------|------|------|
| 403 | `{"error":"cannot delete system plan"}` | D-32 §4 — system plan is undeletable EVEN WITH `?force=true` |
| 404 | `{"error":"plan not found"}` | Unknown id |
| 409 | `{"error":"plan has active users — use ?force=true to confirm", "affected_users": <N>}` | Delete attempted without `force` while users still on plan |
| 500 | `{"error":"internal server error"}` | DB error |

---

### `PUT /api/v1/admin/plans/:id/servers`

Atomically replace the entire `plan_servers` set for this plan. PAY-14.

**Request body:**

```json
{"server_ids": ["uuid-1", "uuid-2", "uuid-3"]}
```

**Success — 200 OK:**

```json
{"data": {"plan_id": "...", "server_ids": ["..."]}}
```

**Errors:**

| Status | Body | When |
|--------|------|------|
| 400 | `{"error":"invalid request body"}` | Malformed JSON |
| 404 | `{"error":"plan not found"}` | Unknown plan id |
| 422 | `{"error":"server not found or inactive", "server_id": "..."}` | Any `server_id` is not in `vpn_servers` or has `is_active=false` |
| 500 | `{"error":"internal server error"}` | DB error |

---

### `POST /api/v1/admin/plans/:id/servers/:server_id`

Add one server to the plan's set. **Idempotent** — re-adding an existing pairing returns 201 (not 409). PAY-14, ADR §19.7.6.

**Success — 201 Created:** `{"data": {"plan_id": "...", "server_id": "..."}}`.

**Errors:**

| Status | Body | When |
|--------|------|------|
| 404 | `{"error":"plan not found"}` | Unknown plan id |
| 422 | `{"error":"server not found or inactive"}` | Server id unknown or inactive |
| 500 | `{"error":"internal server error"}` | DB error |

---

### `DELETE /api/v1/admin/plans/:id/servers/:server_id`

Remove one server from the plan's set. Does NOT force-disconnect active users (D-23 — the operator's call to make in a separate step).

**Success — 204 No Content.**

**Errors:** 404 `{"error":"pairing not found"}`, 500.

---

### `GET /api/v1/admin/plans/:id/offers`

List all offers (active + inactive) for the plan.

**Success — 200 OK:**

```json
{"data": [{"id": "uuid", "plan_id": "...", "periodicity": "MONTHLY", "currency": "USD", "amount": 5.00, "lava_offer_id": "uuid", "is_active": true, "created_at": "..."}]}
```

**Errors:** 404 `{"error":"plan not found"}`, 500.

---

### `POST /api/v1/admin/plans/:id/offers`

Add an offer to the plan. PAY-15.

**Request body:**

```json
{"periodicity": "MONTHLY", "currency": "USD", "amount": 5.00, "lava_offer_id": "uuid-or-null"}
```

**Success — 201 Created:** the created offer row.

**Errors:**

| Status | Body | When |
|--------|------|------|
| 400 | `{"error":"<rule>"}` | Bad periodicity / currency / amount |
| 404 | `{"error":"plan not found"}` | Unknown plan id |
| 409 | `{"error":"active offer already exists for this (periodicity, currency); use POST .../offers/:offer_id/replace to update price"}` | Partial-unique violation on `(plan, periodicity, currency)` WHERE `is_active=true`. Caller must use `/replace` for price versioning. |
| 500 | `{"error":"internal server error"}` | DB error |

Cache busted on success.

---

### `PATCH /api/v1/admin/plans/:id/offers/:offer_id`

Mutate an offer's `amount`, `lava_offer_id`, or `is_active`. **`periodicity` and `currency` are immutable** (ADR §19.7.7); repository ALSO strips them.

**Request body** (all fields optional):

```json
{"amount": 6.00, "lava_offer_id": "uuid", "is_active": true}
```

**Success — 200 OK:** updated offer row.

**Errors:** 400 (bad amount), 404 (`{"error":"offer not found"}`), 500.

---

### `DELETE /api/v1/admin/plans/:id/offers/:offer_id`

Soft delete the offer.

**Success — 204 No Content.**

**Errors:** 404 `{"error":"offer not found"}`, 500.

---

### `POST /api/v1/admin/plans/:id/offers/:offer_id/replace`

Price-versioning operation (PAY-15): deactivate the old offer + insert a new one with the same `(periodicity, currency)` but a different `amount` / `lava_offer_id`, in one transaction. Old invoices keep pointing at the old offer for audit.

**Request body:**

```json
{"amount": 6.50, "lava_offer_id": "uuid-or-null"}
```

`periodicity` and `currency` are inherited from the old offer (immutable per ADR §19.7.7).

**Success — 201 Created:** the new offer row.

**Errors:**

| Status | Body | When |
|--------|------|------|
| 400 | `{"error":"invalid request body"}` / `{"error":"amount must be >= 0"}` / `{"error":"offer does not belong to plan"}` | Bad input |
| 404 | `{"error":"offer not found"}` | Unknown offer id |
| 500 | `{"error":"internal server error"}` | DB error |

Cache busted on success.

---

## 5. Error catalogue (consolidated)

Every distinct error body string used across the Phase 3 surface, with HTTP status and trigger:

| HTTP | Body | Cause | Endpoint(s) |
|------|------|-------|-------------|
| 400 | `{"error":"invalid request body"}` | Malformed JSON | most write endpoints |
| 400 | `{"error":"plan_code, periodicity, currency required"}` | Missing field on `/checkout` | `POST /checkout` |
| 400 | `{"error":"currency must be USD\|EUR\|RUB"}` | Bad currency | `POST /checkout`, plan-offer CRUD |
| 400 | `{"error":"invalid periodicity"}` | Periodicity outside allowed set | `POST /checkout`, plan-offer CRUD |
| 400 | `{"error":"invalid currency"}` | `?currency=` param invalid | `GET /plans` |
| 400 | `{"error":"user has no email"}` | Email-required endpoint called by no-email user | `POST /subscription/cancel` |
| 400 | `{"error":"code must be 1-40 chars"}` / `{"error":"code must match ^[a-z0-9][a-z0-9_-]*$"}` | Bad plan code | `POST /admin/plans` |
| 400 | `{"error":"name must be 1-100 chars"}` | Bad plan name | `POST/PATCH /admin/plans` |
| 400 | `{"error":"max_devices must be -1 or 1..1000"}` | Bad max_devices | `POST/PATCH /admin/plans` |
| 400 | `{"error":"max_servers must be -1 or 0..9999"}` | Bad max_servers | `POST/PATCH /admin/plans` |
| 400 | `{"error":"speed_limit_mbps must be 0..100000"}` | Bad speed limit | `POST/PATCH /admin/plans` |
| 400 | `{"error":"amount must be >= 0"}` | Negative amount | offer CRUD + `/replace` |
| 400 | `{"error":"offer does not belong to plan"}` | Offer-id / plan-id mismatch | `POST .../offers/:id/replace` |
| 400 | (webhook) `{"error":"<parse error>"}` | Webhook body parse failure or missing required `eventType`/`contractId` | `POST /webhook/lava` |
| 401 | (webhook) | `X-Api-Key` mismatch | `POST /webhook/lava` |
| 403 | `{"error":"sign in with Apple or Google before purchasing"}` | Guest tried to checkout (T-03-32) | `POST /checkout` |
| 403 | `{"error":"cannot deactivate system plan"}` | D-32 §4 | `PATCH /admin/plans/:id` |
| 403 | `{"error":"cannot delete system plan"}` | D-32 §4 — applies even with `?force=true` | `DELETE /admin/plans/:id` |
| 403 | (IP allowlist) | TCP source IP not in `LAVA_WEBHOOK_ALLOWED_CIDRS` | `POST /webhook/lava` |
| 404 | `{"error":"plan not found"}` / `{"error":"plan not active"}` | Unknown / inactive plan | `POST /checkout`, plan CRUD |
| 404 | `{"error":"no active offer for plan/periodicity/currency"}` | No matching offer | `POST /checkout` |
| 404 | `{"error":"invoice not found"}` | Unknown id OR ownership mismatch (D-32 §2 — same 404 for both) | `GET /invoices/:id` |
| 404 | `{"error":"no active subscription"}` | No active contract | `POST /subscription/cancel` |
| 404 | `{"error":"offer not found"}` | Unknown offer id | offer CRUD + `/replace` |
| 404 | `{"error":"pairing not found"}` | Unknown plan-server pair | `DELETE /admin/plans/:id/servers/:server_id` |
| 409 | `{"error":"offer_not_configured"}` | D-09 placeholder — `lava_offer_id IS NULL` | `POST /checkout` |
| 409 | `{"error":"active offer already exists for this (periodicity, currency); use POST .../offers/:offer_id/replace to update price"}` | Partial-unique violation | `POST /admin/plans/:id/offers` |
| 409 | `{"error":"plan has active users — use ?force=true to confirm", "affected_users": <N>}` | Delete without force | `DELETE /admin/plans/:id` |
| 422 | `{"error":"server not found or inactive", "server_id": "..."}` | Bad server id | `PUT /admin/plans/:id/servers`, `POST .../servers/:server_id` |
| 500 | `{"error":"internal server error"}` | DB or internal failure (universally) | every endpoint |
| 500 | `{"error":"create plan failed"}` | Plan-create transaction failed | `POST /admin/plans` |
| 500 | (webhook) | Processing failure — lava retries per its 20-attempt policy (PAY-05) | `POST /webhook/lava` |
| 502 | `{"error":"payment provider unavailable"}` | lava upstream error (no upstream body echoed — T-03-34) | `POST /checkout`, `POST /subscription/cancel`, `GET /admin/lava/products` |

---

## 6. Security notes

- **IP allowlist reads TCP `RemoteIP`, not `X-Forwarded-For`.** PAY-06 / RESEARCH §2.4. The middleware in `internal/middleware/lava_ip_allowlist.go` calls `c.Context().RemoteIP()`. Reading `X-Forwarded-For` / `X-Real-IP` would let an attacker spoof their source IP via a header. The reverse-proxy in production (nginx) strips client-supplied forwarded headers; even so, the application-layer check uses the TCP-layer IP for defence in depth. Fiber's `EnableTrustedProxyCheck` + `TrustedProxies` is also set to the lava CIDR list so `c.IP()` returns the right value for non-webhook routes.
- **`X-Api-Key` compared with `crypto/subtle.ConstantTimeCompare`.** PAY-07. Constant-time prevents timing-channel inference of the secret. Rotation supported via `LAVA_WEBHOOK_SECRET_PREVIOUS`.
- **Webhook idempotency UNIQUE.** PAY-04. `lava_webhook_events.UNIQUE(event_type, contract_id, payload->>'timestamp')` (migration 020). `OnConflict{DoNothing}` + `RowsAffected>0` detection means at-least-once delivery is converted to exactly-once side effects.
- **Tier never read from webhook payload.** PAY-08 / T-03-41. The handler ignores `payload.product.title` and resolves tier via `invoices.offer_id → plan_offers → plans → SetUserPlan(user_id, plan_id)`. A payload that claims `product.title="root"` cannot escalate the user.
- **Ownership check on `GET /invoices/:id` returns 404, not 403.** D-32 §2 / T-03-31. Avoids existence-leak (attacker probing invoice ids cannot tell "exists but not yours" from "does not exist").
- **`GET /admin/lava/products` does NOT proxy the lava API key.** D-12 Option B. Server-side fetch with the operator's `LAVA_API_KEY`; browser only sees the flattened dropdown rows. T-03-34 mitigation also strips upstream error bodies — admins see a generic `"payment provider unavailable"` on 502.
- **Lava client BaseURL is a hardcoded const.** D-15 / PAY-16. `internal/lava/client.go::BaseURL = "https://gate.lava.top"`. No env-var override → no SSRF surface. Plus: `CheckRedirect = http.ErrUseLastResponse` (no redirect following), `Timeout = 5s` (DoS bound).
- **System plan undeletable + un-deactivatable.** D-32 §4. `DELETE /admin/plans/:id` 403s on `is_system=true` EVEN WITH `?force=true`. Two layers — handler check + `repository.SoftDeletePlan` returns `ErrSystemPlan`.
- **`is_system` and `code` immutable.** ADR §19.7.4. PATCH DTO has neither field; repository layer ALSO strips them.

---

## 7. Environment variables

Required for Phase 3 (see `server/api/.env.example` for the operator copy-paste template):

| Var | Required when | Notes |
|-----|---------------|-------|
| `LAVA_ENV` | always (defaults to `production` if unset) | `sandbox` \| `production` |
| `LAVA_API_KEY` | `LAVA_ENV=production` | X-Api-Key for outbound lava calls |
| `LAVA_API_KEY_SANDBOX` | `LAVA_ENV=sandbox` | Sandbox X-Api-Key (also used by the integration test) |
| `LAVA_WEBHOOK_SECRET` | always | X-Api-Key on inbound webhook |
| `LAVA_WEBHOOK_SECRET_PREVIOUS` | optional | Used only during zero-downtime secret rotation |
| `LAVA_WEBHOOK_ALLOWED_CIDRS` | always | CSV of CIDRs (e.g. `158.160.60.174/32`) |
| `LAVA_SUCCESS_URL` | always | e.g. `https://risevpn.com/pay/success` |
| `LAVA_FAIL_URL` | always | e.g. `https://risevpn.com/pay/fail` |

The aggregate validator in `internal/config/config.go::RequireEnv()` fails fast at startup with a single composite error listing every missing key, mirroring HOTFIX-08 from Phase 1.

---

## 8. References

- `docs/ADR-007-lava-sso-rework.md` — §9 (lava integration), §10 (API contracts), §19 (dynamic plans extension)
- `docs/audit/MASTER-PLAN.md` — Phase 3 launch security gate
- `.planning/REQUIREMENTS.md` — **PAY-01..PAY-16** (mapped 1:1 to plans 03-01..03-09)
- `.planning/phases/03-lava-top-plans-catalog/03-CONTEXT.md` — decisions **D-01..D-33** (lava + plans + threat model)
- `.planning/phases/03-lava-top-plans-catalog/03-RESEARCH.md` — lava OpenAPI 1.17.0 derivation + Fiber middleware notes
- `.planning/phases/03-lava-top-plans-catalog/03-VALIDATION.md` — Manual-Only Verifications (sandbox card payment row 1)
- Per-plan summaries:
  - `03-01-migrations-models-stripe-cleanup-SUMMARY.md` (PAY-01)
  - `03-02-lava-client-config-SUMMARY.md` (PAY-07, PAY-16)
  - `03-03-plan-repo-SUMMARY.md`
  - `03-04-server-access-enforcement-SUMMARY.md` (PAY-11)
  - `03-05-checkout-cancel-invoices-admin-lava-proxy-SUMMARY.md` (PAY-02, PAY-10, PAY-13 partial)
  - `03-06-webhook-handler-ip-allowlist-SUMMARY.md` (PAY-03, PAY-04, PAY-05, PAY-06, PAY-08, PAY-09)
  - `03-07-public-plans-jwt-cache-SUMMARY.md` (PAY-12)
  - `03-08-admin-plans-crud-SUMMARY.md` (PAY-13, PAY-14, PAY-15)
  - `03-09-expiry-cron-SUMMARY.md` (D-26 expiry downgrade)
  - `03-10-admin-web-plans-ui-SUMMARY.md` (admin UI for PAY-13/14/15)
  - `03-11-docs-sandbox-smoke-SUMMARY.md` (this doc + sandbox smoke)
