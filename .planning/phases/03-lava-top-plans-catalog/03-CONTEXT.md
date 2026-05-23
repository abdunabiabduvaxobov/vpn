# Phase 3: Lava.top + plans catalog - Context

**Gathered:** 2026-05-23
**Status:** Ready for planning
**Source:** Interactive discussion + ADR-007 §19 (PRD)

<domain>
## Phase Boundary

Land the **backend + minimal admin-UI** layer of lava.top payments and the dynamic plans catalog so a real test-card payment through lava sandbox grants Pro to a specific signed-in user within seconds of the webhook arriving, with strict idempotency, and all plan limits/prices managed in `plans`/`plan_offers`/`plan_servers` tables (the hardcoded `model.PlanLimits` map is gone). Server access is enforced at the repository layer.

**In-scope (this phase only):**
- DB migrations: `019_plans_catalog.sql` (plans + plan_servers + plan_offers + users.plan_id FK + collapse premium/ultimate → pro) and `020_lava_payments.sql` (invoices + lava_contracts + lava_webhook_events tables; drop subscriptions.stripe_id; add subscriptions.lava_contract_id; add invoices.plan_id + invoices.plan_offer_id per ADR §19.6).
- GORM models: `internal/model/plan.go` (Plan, PlanServer, PlanOffer); `internal/model/user.go` amended with `PlanID *string`; `internal/model/subscription.go` rewritten (PlanLimits map deleted, Subscription struct kept + LavaContractID column).
- `internal/lava/` HTTP client package: `CreateInvoice`, `GetInvoice`, `ListProducts`, `CancelSubscription`, webhook signature/auth helper. Hardcoded base URL `https://gate.lava.top`, 5-second context timeout, no SSRF surface.
- `internal/repository/plan_repo.go` (new): `FindPlanByID`, `FindPlanByCode`, `ListActivePlans`, `SetUserPlan` (transactional update of `users.plan_id` + `users.subscription_tier`), `ListServersForPlan`, `IsServerAllowedForPlan`, full CRUD for plans/offers/plan_servers.
- Handler rewrites/additions:
  - `handler/payment.go` REWRITTEN in-place (Stripe code deleted, lava handlers replace it).
  - `handler/servers.go` amended: branch on role, switch to `ListServersForPlan` for non-admin, add plan check to `GetServerConfig`.
  - `handler/admin.go` amended: drop `PlanLimits[req.SubscriptionTier]` hard-coded check, validate against `plans` table.
  - `handler/health.go` amended: stop reading `PlanLimits` (read defaults from system plan).
  - `handler/connection.go` amended: stop reading `PlanLimits` (read limits via plan_id).
  - `handler/plans_admin.go` (new): all `/admin/plans/*` handlers + `/admin/plans/:id/servers/*` + `/admin/plans/:id/offers/*` per ADR §19.7.
  - `handler/plans_public.go` (new): `GET /api/v1/plans` per ADR §19.9.1 (Redis-cached 60s).
  - `handler/payment.go` lava endpoints: `POST /api/v1/checkout`, `POST /api/v1/webhook/lava`, `POST /api/v1/subscription/cancel`, `GET /api/v1/invoices/:id` (DB-only + `?escalate=true` lava-fallback after 5 polls per Phase 4 page).
  - `handler/admin_lava.go` (new): `GET /admin/lava/products` proxy endpoint (Option B for offer-ID sourcing).
  - `handler/auth.go` amended: JWT mint adds `plan_id` claim per ADR §19.9.2.
  - `middleware/jwt.go` amended: extract `plan_id` into `c.Locals`.
- `internal/scheduler/scheduler.go` amended: add expiry cron job (every 10 minutes, gated by `RUN_SCHEDULER`) per ADR §19.10.
- `cmd/main.go` amended: register all new routes; remove `/webhook/stripe` + `/subscription/checkout` routes; wire lava client + new env vars into the HOTFIX-08 required-env validator.
- `internal/config/config.go` amended: new env vars (see Decisions D-30).
- Tests:
  - Unit tests per new package (lava client, plan_repo).
  - Handler tests for checkout (happy + 409 reuse + 409 missing offer), webhook (idempotency replay, IP allowlist, secret check, all 5 event types), public /plans, admin CRUD.
  - Integration test against lava.top sandbox verifying success criterion #1 end-to-end.
  - Migration test verifying premium/ultimate → pro coercion is destruction-free.
  - `grep -r 'PlanLimits' server/api/` returns no hits outside the migration's own coercion test (success criterion #4 from ADR §19.12).
- Admin-web UI work (pulled in from ADR §19.13 to support Option B):
  - `admin-web/src/api/plans.ts` (fetchers).
  - `admin-web/src/api/lava.ts` (lists lava products via `GET /admin/lava/products`).
  - `admin-web/src/pages/Plans.tsx`, `PlanDetail.tsx`.
  - `admin-web/src/components/plans/` (PlansTable, PlanForm, PlanServersPicker, PlanOffersGrid w/ dropdown picker, DeletePlanDialog, ReplaceOfferDialog).
  - Sidebar "Plans" entry.
- API contract doc: `docs/lava-payments-api.md` (mirrors Phase 2's `docs/auth-sso-api.md` pattern, lists every endpoint with request/response/errors).

**Out-of-scope (explicitly deferred to later phases):**
- Landing `/login`, `/pricing`, `/pay/success`, `/pay/fail` pages — Phase 4 (WEB-01..09).
- Mobile `PaymentScreen.tsx` rewrite, deep-link return handler — Phase 5 (APP-05, APP-06).
- Stripe go.mod removal + payment_test.go cleanup of `stripe-go` test imports + final `subscriptions.stripe_id` column drop — Phase 8 (HARD-01). Phase 3 only stops calling stripe-go from `payment.go` (the import is removed from that file).
- Per-user advisory lock between admin actions and webhook (ADMIN-03) — Phase 7. Phase 3 documents the interim gap; uses transactional UPSERT + GORM's `Clauses(clause.OnConflict)` as best-effort serialization.
- Apple `authorizationCode` exchange — Phase 2 D-18 deferred; still deferred.
- KPI dashboard, webhook event log UI, webhook replay button — Phase 7 (ADMIN-01, ADMIN-06).
- Multi-region / horizontal scale (SCALE-01..03) — v2.

</domain>

<decisions>
## Implementation Decisions

> Every entry is a **locked decision** unless marked Claude's Discretion. ADR-007 §19 is the source of truth; this section captures the decisions made during discussion that pin down DECIDE items or extend ADR scope.

### Stripe disposition (this phase)

- **D-01:** Rewrite `internal/handler/payment.go` in-place this phase: delete the four Stripe handlers (`CreateCheckoutSession` Stripe path, `HandleStripeWebhook`, `CancelSubscription` Stripe path, helpers), delete the `stripe-go` imports from this file, replace with lava-bound implementations of `CreateCheckoutSession`, `HandleLavaWebhook`, `CancelSubscription`, plus new `GetInvoice`.
- **D-02:** Remove the `POST /webhook/stripe` and `POST /subscription/checkout` routes from `cmd/main.go`. Add `POST /checkout`, `POST /webhook/lava`, `POST /subscription/cancel`, `GET /invoices/:id`, `GET /plans` (public), and the `/admin/plans/*` + `/admin/lava/products` admin routes.
- **D-03:** Keep `stripe-go` in `go.mod` for now. Existing test files (`payment_test.go`, `admin_test.go`) still import `stripe-go` for fixture seeding. Phase 8 (HARD-01) removes the go.mod entry, deletes those test files, and drops `subscriptions.stripe_id` via migration. **Phase 3 does not touch `payment_test.go` Stripe tests** — they become orphaned for one milestone, then deleted.

### Chunking, branching, and shipping

- **D-04:** Phase 3 ships as ~7-9 plans across 4-5 waves. Recommended split (planner may refine):
  - **Wave 1 (parallel):** `03-01` migration 019_plans_catalog + 020_lava_payments + GORM models. `03-02` `internal/lava/` HTTP client package (no DB, no Fiber).
  - **Wave 2 (depends W1):** `03-03` `plan_repo.go` (FindPlan*, ListActivePlans, SetUserPlan, ListServersForPlan, IsServerAllowedForPlan, plan/offer/plan_server CRUD). `03-04` server-access enforcement in `handler/servers.go` + `handler/connection.go` + `handler/admin.go` + `handler/health.go` (drop PlanLimits map).
  - **Wave 3 (depends W2):** `03-05` `/checkout` + invoice repo + lava `/admin/lava/products` proxy. `03-06` `/webhook/lava` idempotent with all 5 event types. `03-07` public `GET /api/v1/plans` (Redis cache 60s).
  - **Wave 4 (depends W3):** `03-08` admin `/admin/plans/*` + `/servers` + `/offers` CRUD. `03-09` expiry cron in scheduler.
  - **Wave 5 (depends W4):** `03-10` admin-web UI for plans + offer dropdown picker. `03-11` API contract doc + lava sandbox integration test + `grep -r PlanLimits` smoke.
- **D-05:** Branching matches Phase 1/2: working branch, atomic per-plan commits, no per-phase branch (per `.planning/config.json` branching_strategy=none). After Wave 5 integration test passes against lava sandbox, tag `v2.2.0-pay` for staging smoke (operator may waive per Phase 1 precedent).
- **D-06:** Integration test runs against **lava.top sandbox** with separate `LAVA_API_KEY_SANDBOX`. Production smoke deferred to Phase 5 ship gate. Dev/CI/staging use sandbox config; production-only env (when set) uses production key.

### Schema, migrations, seeds

- **D-07:** Migration filenames: `019_plans_catalog.sql` (Phase 2 took 018) and `020_lava_payments.sql`. ADR §19.3 + §8.3 contents are correct; only the filenames change.
- **D-08:** `019_plans_catalog.sql` content per ADR §19.3 verbatim with these overrides:
  - Pro seeded with `max_devices=3` (not 5 per STATE.md default — tighter, matches old "premium" sizing).
  - Free seeded with `max_devices=1, max_servers=3, speed_limit_mbps=50, is_system=TRUE, sort_order=0` (per ADR).
  - Plan-servers seed: free gets 3 lowest-load active servers; Pro gets every active server.
  - Tier coercion: `UPDATE users SET subscription_tier='pro' WHERE subscription_tier IN ('premium','ultimate')` + same on subscriptions (destruction-free; zero paying users).
  - `users.plan_id UUID REFERENCES plans(id) ON DELETE SET NULL` backfilled from `subscription_tier`, then set NOT NULL.
- **D-09:** Migration `019` also seeds **6 placeholder `plan_offers` rows** for Pro: `{MONTHLY, PERIOD_YEAR} × {USD, EUR, RUB}` with `lava_offer_id=NULL`. Admin opens each in the UI dropdown picker post-deploy and selects matching lava offer. `/checkout` returns `409 Conflict {"error":"offer_not_configured"}` until the row's `lava_offer_id` is non-NULL.
- **D-10:** `020_lava_payments.sql` content per ADR §8.3 + §19.6 amendments. `lava_webhook_events.UNIQUE (event_type, contract_id, (payload->>'timestamp'))` casts timestamp as text via `->>` (handles both string and integer JSON values per lava's spec ambiguity).
- **D-11:** `subscriptions.stripe_id` is dropped in this migration (`DROP COLUMN IF EXISTS stripe_id`) per ADR §8.3. **Note:** this conflicts with D-03's "keep stripe-go in go.mod"; the column drop only affects DB schema, not Go code. `payment_test.go` fixtures that reference `stripe_id` in SQL DDL strings (e.g., `stripe_id TEXT`) are now stale — but they live in test-local table-create SQL, not the production schema. Planner verifies the test DDL doesn't break test runs; if it does, the test is replaced as part of Phase 3 (otherwise deferred to Phase 8).

### Lava client & offer-ID sourcing (Option B)

- **D-12:** `lava_offer_id` sourcing uses **Option B only** (synced dropdown). Admin cannot paste a UUID manually. New backend endpoint `GET /api/v1/admin/lava/products` proxies `GET https://gate.lava.top/api/v2/products` using server-side `LAVA_API_KEY` (key never reaches the browser). Returns normalized array of `{productId, offerId, productName, periodicity, currency, amount}` for dropdown rendering.
- **D-13:** Admin UI for plan-offer editing renders the dropdown picker. The admin opens a plan_offer row (placeholder from D-09), selects the matching lava offer from the dropdown, the form fills `lava_offer_id`, PATCHes the row. **Admin-web UI is in scope for Phase 3** (ADR §19.13 said "Phase 3.5" but D-12's Option-B-only choice requires the dropdown UI to be live for the system to be usable).
- **D-14:** Lava client package layout `server/api/internal/lava/`:
  - `client.go` — `Client` struct holding API key, base URL constant (`https://gate.lava.top`), `http.Client` with 5s timeout, no redirect follow, no `InsecureSkipVerify`.
  - `invoice.go` — `CreateInvoice(ctx, req) (*InvoiceResponse, error)` calls `POST /api/v3/invoice`. `GetInvoice(ctx, lavaInvoiceID) (*InvoiceResponse, error)` calls `GET /api/v2/invoices/{id}`.
  - `products.go` — `ListProducts(ctx) ([]Product, error)` calls `GET /api/v2/products`.
  - `subscription.go` — `CancelSubscription(ctx, contractID, email) error` calls `DELETE /api/v1/subscriptions?contractId=X&email=Y`.
  - `webhook.go` — `VerifySignature(headers, secret, prevSecret) error` constant-time check, accepts either of two secrets.
  - DTOs in `dto.go`. Pure package — no Fiber, no GORM, no globals beyond the base URL constant.
- **D-15:** Hardcoded `const BaseURL = "https://gate.lava.top"` in `client.go`. **No env override.** SSRF mitigation — verifier will check this is a constant string literal not interpolated.

### Webhook security & failure semantics

- **D-16:** IP allowlist via env: `LAVA_WEBHOOK_ALLOWED_CIDRS` (comma-separated CIDR list, default `158.160.60.174/32`). Loaded at startup; Fiber `EnableTrustedProxyCheck=true` + `TrustedProxies=<the list>` per PAY-06. Webhook handler **never reads `X-Forwarded-For` or `X-Real-IP` directly** — it relies on Fiber's trusted-proxy resolution. Validated at startup via the HOTFIX-08 required-env aggregate validator.
- **D-17:** `X-Api-Key` shared secret check: `LAVA_WEBHOOK_SECRET` (required) + `LAVA_WEBHOOK_SECRET_PREVIOUS` (optional). Handler accepts either via `crypto/subtle.ConstantTimeCompare` (PAY-07). Rotation: (1) set `_PREVIOUS=<old>`, `_SECRET=<new>`, restart; (2) update lava.top dashboard to new secret; (3) clear `_PREVIOUS`, restart. Zero-downtime rotation, no dropped webhooks during dashboard update window.
- **D-18:** Idempotency UNIQUE: `(event_type, contract_id, (payload->>'timestamp'))` as text (D-10). Duplicate insert → unique violation → handler returns 200 OK without re-applying (matches PAY-04). Processing error → handler returns HTTP 500 so lava retries (matches PAY-05).
- **D-19:** Event dispatch (ADR §9.3) with this phase's resolved semantics:
  - `payment.success` → mark invoice paid, upsert `lava_contracts`, call `SetUserPlan(user, plan)` (transactional update of `users.plan_id` + `users.subscription_tier` + activate `subscriptions` row with `lava_contract_id`).
  - `subscription.recurring.payment.success` → extend `subscriptions.expires_at` and `lava_contracts.expires_at` by one period.
  - `payment.failed` → mark invoice failed; no tier change.
  - `subscription.recurring.payment.failed` → set `subscriptions.is_active=false` and `lava_contracts.is_active=false` **immediately**. Do NOT downgrade tier inline — the §19.10 expiry cron (every 10 min) flips `plan_id` to system plan after `expires_at` lapses. User keeps paid-for time.
  - `subscription.cancelled` → set `lava_contracts.cancelled_at=now()`, `is_active=false`. Tier untouched until `expires_at` lapses (cron handles).
- **D-20:** No additional rate-limiting on `/webhook/lava` beyond the global per-IP middleware (HOTFIX-03). IP allowlist is the primary defense. Don't risk dropping legitimate lava retries.

### Server access enforcement

- **D-21:** Server-access enforcement lives in `repository/plan_repo.go` via `ListServersForPlan(planID)` and `IsServerAllowedForPlan(planID, serverID)` per ADR §19.5. Handlers compose this with a role check: `if role=="admin" { ListActiveServers() } else { ListServersForPlan(planID) }`.
- **D-22:** `GET /servers/:id/config` for a server NOT in the user's plan returns **404 Not Found** (don't leak server existence to lower-tier users — defense in depth against enumeration). Admins bypass and get the legacy 200/404 by-existence path.
- **D-23:** Admin `DELETE /admin/plans/:id/servers/:server_id` does NOT force-disconnect currently-connected users. Connections stay up; the next reconnect fails the `IsServerAllowedForPlan` check and returns 404. Admin UI shows a warning before save: "N users currently connected to this server — they will be denied on reconnect."
- **D-24:** `connection.go` reads device limits from `repository.FindPlanByID(planID)` instead of `PlanLimits[tier]`. `admin.go` plan validation uses `FindPlanByCode(req.SubscriptionTier)` (returns 404 if no such plan) instead of `_, ok := PlanLimits[req.SubscriptionTier]`. `health.go` reads system-plan limits via `ListActivePlans()` filtered to `is_system=true`.

### Polling endpoint & expiry cron

- **D-25:** `GET /api/v1/invoices/:id` (auth required, must own the invoice): pure DB read by default. If query param `?escalate=true` is set AND DB still shows `pending`, proxy `GET /api/v2/invoices/{lava_invoice_id}` via the lava client and update local DB if lava reports `paid`. Phase 4 `/pay/success` page sends `?escalate=true` after ~5 polls (10 seconds of pure-DB polling). Defends against webhook outages without amplifying lava traffic on every poll.
- **D-26:** Expiry cron `internal/scheduler/scheduler.go` `runExpiryDowngrade` runs every 10 minutes per ADR §19.10. SQL is idempotent (no-op when no users are eligible). Gated by `RUN_SCHEDULER` env (the PERF-06 gate ships in Phase 6 — until then, the scheduler runs in every API replica; Phase 3 only has one replica so this is a no-op concern this milestone).

### Public plans endpoint & JWT

- **D-27:** `GET /api/v1/plans` (no auth) per ADR §19.9.1. Query param `?currency=USD|EUR|RUB`, default derived from `Accept-Language` (RU → RUB else USD). Filters `plans.is_active=TRUE` and `plan_offers.is_active=TRUE`, ordered by `sort_order ASC, id ASC`. Response excludes `id`, `lava_offer_id`, `active_user_count` (admin-only fields). `server_countries` denormalized (distinct `country_code` from joined `plan_servers`).
- **D-28:** Redis cache for `/api/v1/plans`: key `cache:plans:public:{currency}`, TTL 60s. Admin writes to `/admin/plans/*` bust all matching keys via `DEL cache:plans:public:*` (cheap, infrequent). This is the foundation for PERF-01's broader server-list cache; the cache helper goes in `internal/cache/redis.go` as a small wrapper.
- **D-29:** JWT mint amended to include `plan_id` (UUID) claim per ADR §19.9.2 alongside existing `tier`. Middleware (`middleware/jwt.go`) extracts `plan_id` into `c.Locals("plan_id")`. Existing in-flight JWTs from Phase 2 do NOT have `plan_id` — middleware falls back to `(plan_id = nil → resolve via SELECT users.plan_id WHERE id = sub)` on the first request, then JWT refresh issues the new shape. Backward compat for 5-min access TTL bound.

### Config additions

- **D-30:** New env vars added to `internal/config/config.go` and registered with the HOTFIX-08 aggregate validator. All required at startup unless noted:
  - `LAVA_API_KEY` — production lava.top API key (X-Api-Key header for outbound calls).
  - `LAVA_API_KEY_SANDBOX` — optional, used in dev/test when set; production explicitly uses `LAVA_API_KEY`. Planner decides exact selection logic (env-based vs config flag).
  - `LAVA_WEBHOOK_SECRET` — required.
  - `LAVA_WEBHOOK_SECRET_PREVIOUS` — optional; only set during rotation.
  - `LAVA_WEBHOOK_ALLOWED_CIDRS` — required (default `158.160.60.174/32` allowed via config default if env unset, OR strictly required — planner picks).
  - `LAVA_SUCCESS_URL` — required, e.g. `https://risevpn.com/pay/success`.
  - `LAVA_FAIL_URL` — required, e.g. `https://risevpn.com/pay/fail`.
  - **No** `LAVA_OFFER_PRO_MONTHLY` / `LAVA_OFFER_PRO_YEARLY` env vars (ADR §9.1 superseded by §19 — offer IDs live in `plan_offers.lava_offer_id`).

### Threat model

- **D-31:** ASVS **L2** scoping applies to: `handler/payment.go` (checkout, cancel, get-invoice), `handler/webhook_lava.go`, all `handler/plans_admin.go` and `handler/admin_lava.go` routes, `internal/lava/` package. ASVS **L1** elsewhere (public `GET /plans`, server-access enforcement reads, scheduler job).
- **D-32:** Every PLAN.md in Phase 3 includes an inline `<threat_model>` block (matches Phase 2 D-CD). Scope of the block depends on the plan's surface — the migration plan does not need webhook-replay coverage; the webhook plan does. The four categories that MUST appear in plans touching money or admin paths:
  1. **Webhook security & idempotency** — replay, 20-retry burst, race between concurrent deliveries (interim: transactional UPSERT + GORM `OnConflict`; advisory lock waits for Phase 7 ADMIN-03), IP spoof via `X-Forwarded-For` (Fiber TrustedProxies), API-key timing attack (ConstantTimeCompare), secret rotation correctness.
  2. **Payment data integrity** — client-supplied offer/tier/price tampering (PAY-08: tier derived from offerId via `plan_offers` lookup, NEVER from client metadata), 60s checkout idempotency window for double-tap, invoice ownership check on `GET /invoices/{id}`, lava webhook authoritative over `/pay/success` polling.
  3. **Outbound SSRF + secret hygiene** — hardcoded `https://gate.lava.top` base URL (no env override), 5s context timeout, no redirect follow, API key never logged or echoed in error paths, no client-supplied URL fields in lava client interface.
  4. **Admin abuse + privilege boundary** — `is_system=true` not settable via API (migration-only), `?force=true` on system plan delete returns 403, soft-delete grandfathering documented, admin actions audit-logged with diff, immutable `code` field enforced server-side, plan-server CRUD validates server existence + active state, `GET /admin/lava/products` admin-only.
- **D-33:** No additional rate-limiting on `/webhook/lava` beyond global per-IP middleware. IP allowlist is the primary defense.

### Claude's Discretion

- **Wave 5 plan split.** D-04 suggests `03-10` (admin-web UI) + `03-11` (docs + sandbox test). Planner can collapse into one plan if the UI work is small, or split admin-web further (e.g., plan list page vs plan detail page).
- **`LAVA_WEBHOOK_ALLOWED_CIDRS` default.** D-30 leaves planner to choose strict-required vs default-`158.160.60.174/32`. Default to strict-required if HOTFIX-08 framework supports it cleanly (matches HOTFIX-08 pattern from Phase 1).
- **`LAVA_API_KEY` vs `LAVA_API_KEY_SANDBOX` selection logic.** D-30 leaves planner to pick: env-flag-driven (`LAVA_USE_SANDBOX=true`), config-aware (sandbox in dev/test build tags, prod key in prod build), or pure env-name (whichever is set, sandbox takes precedence if both — risky). Default recommendation: explicit `LAVA_ENV=sandbox|production` flag in env, defaults to `production` if unset, picks the matching key.
- **Invoice polling escalate threshold.** D-25 says "after 5 polls (10s)". Planner can adjust if Phase 4 surface diverges; the backend just respects `?escalate=true`.
- **Public `/plans` cache key shape.** D-28 uses `cache:plans:public:{currency}`. Planner can extend to include locale or version suffix if needed; bust strategy stays `DEL cache:plans:public:*`.
- **GORM `OnConflict` clause shape** for webhook idempotent UPSERT. D-19 + D-18 leave the exact clause to the planner — `clause.OnConflict{DoNothing:true}` for `lava_webhook_events` insert vs `clause.OnConflict{Columns:[...], DoUpdates:[...]}` for `lava_contracts` upsert.
- **Admin-web admin-login flow during Phase 3.** Admin login exists from Phase 1 (`/auth/admin-login`). No SSO for admins in Phase 3 (Apple/Google is consumer-only). Confirm planner doesn't accidentally pull admin SSO into scope.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents (researcher, planner, executor) MUST read these before planning or implementing.**

### Architecture — source of truth for this phase
- `docs/ADR-007-lava-sso-rework.md` §4 — component boundaries, package tree (`internal/lava/`, `handler/payment.go` ownership)
- `docs/ADR-007-lava-sso-rework.md` §8.3 — original lava migration content (amended by §19.6 invoices.plan_id additions)
- `docs/ADR-007-lava-sso-rework.md` §9 (all subsections) — checkout flow, webhook handling, idempotency mechanics, polling fallback
- `docs/ADR-007-lava-sso-rework.md` §10 — API contracts for `/checkout`, `/webhook/lava`, `/subscription/cancel`, `/invoices/:id`
- `docs/ADR-007-lava-sso-rework.md` §10.8 — Stripe routes to remove + lava routes to add
- `docs/ADR-007-lava-sso-rework.md` §19 (entire section) — dynamic plans (concepts, schema, server access enforcement, pricing/payment flow update, admin API contracts, lava sync strategy, public endpoint, grandfathering, expiry cron, rollout phasing, admin UI scope)
- `docs/ADR-007-lava-sso-rework.md` §19.3 — `019_plans_catalog.sql` content (use 019 not 018 per D-07; Pro `max_devices=3` per D-08)
- `docs/ADR-007-lava-sso-rework.md` §19.5 — server access enforcement (404 on denied per D-22)
- `docs/ADR-007-lava-sso-rework.md` §19.6 — pricing/payment flow update + `invoices.plan_id`/`plan_offer_id` amendment
- `docs/ADR-007-lava-sso-rework.md` §19.7 — admin API contracts (all `/admin/plans/*` endpoints, validation rules, audit-log integration)
- `docs/ADR-007-lava-sso-rework.md` §19.8 — Option B is locked per D-12
- `docs/ADR-007-lava-sso-rework.md` §19.9 — public `/api/v1/plans` shape + JWT `plan_id` claim
- `docs/ADR-007-lava-sso-rework.md` §19.10 — grandfathering rules + expiry cron SQL (every 10 min per D-26)
- `docs/ADR-007-lava-sso-rework.md` §19.12 — Phase 3 file checklist (use as planner's reference for what files exist)
- `docs/ADR-007-lava-sso-rework.md` §19.13 — admin UI scope (partially pulled into Phase 3 per D-13)

### Roadmap, requirements, prior context
- `.planning/ROADMAP.md` §"Phase 3: Lava.top + plans catalog" — phase goal, depends-on (Phase 2), 7 numbered success criteria
- `.planning/REQUIREMENTS.md` §"Pay — Lava.top + plans catalog" — PAY-01 through PAY-16 acceptance criteria
- `.planning/PROJECT.md` §"Key Decisions" — lava-only, no IAP, dynamic plans, webhook idempotency, Apple/Google primary identity
- `.planning/STATE.md` §"Phase 2 blockers" — Pro device limit default (5); D-08 overrides to 3
- `.planning/phases/01-hotfix-audit-critical-fixes/01-CONTEXT.md` — Phase 1 patterns (HOTFIX-08 validator, atomic per-plan commits)
- `.planning/phases/02-auth-sso-backend/02-CONTEXT.md` — Phase 2 patterns (threat-model block per PLAN.md, migration numbering, audience-whitelist DI, library choices)

### Code anchors (existing surface this phase extends)
- `server/api/cmd/main.go` — Fiber app construction, route registration. Remove Stripe routes; add all new lava + plans routes.
- `server/api/internal/handler/payment.go` — REWRITE in-place (D-01). Currently Stripe-only.
- `server/api/internal/handler/payment_test.go` — orphaned this phase (D-03); Phase 8 cleanup. Verify Phase 3 test runs don't break on stale `stripe_id` DDL strings (D-11).
- `server/api/internal/handler/servers.go:114` — `PlanLimits[tier]` slicing — REPLACE with `ListServersForPlan` (D-21).
- `server/api/internal/handler/health.go:45,55` — `PlanLimits["free"]`, `PlanLimits[sub.Plan]` reads — REPLACE with system-plan lookup (D-24).
- `server/api/internal/handler/connection.go:97,100` — `PlanLimits[tier]` device-limit check — REPLACE with `FindPlanByID(planID)` (D-24).
- `server/api/internal/handler/admin.go:137` — `PlanLimits[req.SubscriptionTier]` validation — REPLACE with `FindPlanByCode` (D-24).
- `server/api/internal/handler/auth.go` — `generateTokens`; amend JWT to include `plan_id` claim (D-29).
- `server/api/internal/middleware/jwt.go` — extract `plan_id` into `c.Locals` (D-29).
- `server/api/internal/model/subscription.go` — REWRITE: delete `PlanLimits` map, keep `Subscription` struct, drop `StripeID` field, add `LavaContractID` field.
- `server/api/internal/model/user.go` — AMEND: add `PlanID *string`.
- `server/api/internal/repository/server_repo.go` — keep `ListActiveServers` for admin path (D-21 hybrid); add nothing else (server-access lives in `plan_repo.go`).
- `server/api/internal/scheduler/scheduler.go` — add `runExpiryDowngrade` every 10 min (D-26).
- `server/api/internal/config/config.go` — add LAVA_* env vars + register with HOTFIX-08 validator (D-30).
- `server/api/migrations/` — next two files are `019_plans_catalog.sql` (D-07/D-08/D-09) + `020_lava_payments.sql` (D-10/D-11).
- `server/api/go.mod` — Phase 3 adds nothing new beyond what's needed for lava client (standard library `net/http` is sufficient). Removes nothing this phase.
- `admin-web/src/api/`, `admin-web/src/pages/`, `admin-web/src/components/` — new files for plans UI per ADR §19.13 (D-13).

### Project-wide rules
- `CLAUDE.md` (project root) — GSD enforcement, lava-only / Apple+Google-only / no-IAP constraints, webhook idempotency requirement, security-gate "Critical/High before any paying user" (Phase 3 IS the launch gate).
- `docs/audit/SECURITY-AUDIT.md` — informs ASVS L2 scoping for payment paths (D-31).
- `docs/audit/MASTER-PLAN.md` §Tranche 2 — original audit-derived shape for this phase.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `internal/cache/redis.go` — Redis client + existing helpers (HOTFIX-03 atomic Lua INCR/EXPIRE landed here). Extend with `cache:plans:public:*` wrapper for D-28.
- `internal/scheduler/scheduler.go` — periodic-job runner. Adding `runExpiryDowngrade` (D-26) follows the existing pattern.
- `internal/config/config.go` + HOTFIX-08 aggregate required-env validator (Phase 1 D-03/D-04) — extended again for `LAVA_*` env vars (D-30).
- `internal/middleware/audit.go` — audit-log middleware on admin route group. Plans admin endpoints inherit it for free (D-32 admin abuse mitigations).
- `internal/middleware/jwt.go` — JWT verification middleware. Extend for `plan_id` claim extraction (D-29).
- `internal/handler/auth.go::generateTokens` — JWT minting. Extend for `plan_id` claim (D-29).
- `internal/repository/subscription_repo.go` — existing entitlement model with `GORM_Subscription` struct + tests. Phase 3 keeps this table; `SetUserPlan` (new in `plan_repo.go`) writes both `users.plan_id` and `subscriptions` in one tx.
- `internal/repository/user_repo.go` — Phase 2's `PromoteGuestToSSO`, `FindUserByAppleID`, etc. landed here. `SetUserPlan` (D-19) could live in `user_repo.go` or `plan_repo.go`; planner picks based on which other functions it calls.
- Phase 1's HOTFIX-01 `subscription_expires_at TIMESTAMPTZ NULL` column on `subscriptions` already exists — D-19's webhook `period_end` populates it.
- Phase 1's HOTFIX-04 `ErrorHandler` (`{error, request_id}` body for 5xx) — covers D-32's "no client-leakage" mitigation in error paths.

### Established Patterns
- **Migration ordering:** sequential integer prefix. Phase 2 used 018; Phase 3 uses 019 + 020.
- **Verifier/client packages:** Phase 2 established `internal/auth/{apple,google}/` as pure libs. `internal/lava/` (D-14) follows the same shape (no DB, no Fiber, DTOs in a `dto.go` file).
- **Threat-model inline blocks:** Phase 2 D-CD pattern. D-32 carries forward.
- **Atomic per-plan commits on working branch:** Phase 1 + 2 pattern. D-05 carries forward.
- **API contract doc per phase:** Phase 2 shipped `docs/auth-sso-api.md`. Phase 3 ships `docs/lava-payments-api.md` (D-04 Wave 5).
- **Test conventions:** `*_test.go` files alongside source; subtests for happy/error paths; testcontainers for Postgres in integration tests.
- **Audit middleware on admin group:** `cmd/main.go:182` admin Fiber group already wires JWT + admin role + audit. Plans admin handlers mount on this group for free.

### Integration Points
- `cmd/main.go` — route registration block. Multiple route additions + Stripe route removal.
- `cmd/main.go` admin Fiber group (line ~182) — mount point for all `/admin/plans/*` and `/admin/lava/products`.
- HOTFIX-08 env validator in `config.go` — register `LAVA_*` env vars.
- Existing `subscriptions` table — Phase 3 keeps it (per ADR §8.3 note); `lava_contracts` is the new payment-provider mirror.
- Existing `vpn_servers.is_active` flag — plan_servers seed and admin add-validation rely on this.

</code_context>

<specifics>
## Specific Ideas

- **lava.top sandbox** is the integration-test target. Sandbox API key (`LAVA_API_KEY_SANDBOX`) is separate from production (`LAVA_API_KEY`). Production smoke deferred to Phase 5 ship.
- **Pro starts at 3 devices** (not the STATE.md default of 5). Easier to bump from admin UI later than to tighten.
- **6 placeholder offer rows** in 019 migration — admin clicks each, dropdown shows lava products, selects, PATCHes the `lava_offer_id`. No paste path.
- **No additional rate-limiting** on the webhook beyond the global per-IP middleware — IP allowlist is the primary defense and dropping legitimate lava retries would be worse than no cap.
- **`subscription.recurring.payment.failed`** → immediate `is_active=false` but tier downgrade waits for `expires_at` cron. User keeps paid-for time.
- **404 on server-access denied** (not 403) — defense in depth against enumeration. Matches what `GET /servers` already does (server isn't in the list).
- **No force-disconnect on plan-server removal** — connections survive; admin UI warns about N affected users.
- **Polling endpoint escalation**: `/invoices/:id` is DB-only by default; Phase 4 `/pay/success` page sends `?escalate=true` after ~10s of pure-DB polling to trigger a one-shot lava proxy fallback.
- **Per-plan inline `<threat_model>` block** — matches Phase 2 D-CD. No phase-level THREAT-MODEL.md.
- **JWT `plan_id` claim** lands this phase. 5-min access TTL bounds staleness when admin changes a user's plan.
- **Stripe-go stays in `go.mod`** through Phase 3 even though `payment.go` no longer imports it — test fixtures still reference it. Phase 8 (HARD-01) drops the dep + the test fixtures + the `stripe_id` column drop migration.
- **`LAVA_WEBHOOK_SECRET_PREVIOUS`** is the zero-downtime rotation pivot. Handler ConstantTimeCompares against both if `_PREVIOUS` is set.
- **Admin-web UI is in scope** for Phase 3 because Option B requires the dropdown UI to be usable.

</specifics>

<deferred>
## Deferred Ideas

- **Lava.top product auto-creation API (Option C from ADR §19.8)** — defer until lava documents a `POST /api/v2/products` endpoint; for now plans are created on lava dashboard and selected via dropdown.
- **Email reminders on failed recurring payment** — would soften UX of immediate `is_active=false` (D-19), but we have no email pipeline; out of scope.
- **Force-disconnect on plan-server removal as opt-in admin checkbox** — D-23 chose passive deny on reconnect; backlog candidate if support burden materializes (operator can re-add the option later).
- **Per-user advisory lock between admin actions and webhook (ADMIN-03)** — ships Phase 7. Phase 3 documents the gap in `<threat_model>` and uses transactional UPSERT + `clause.OnConflict` as best-effort serialization.
- **Stripe code/dep removal** — Phase 8 (HARD-01). Phase 3 only stops the active routes + imports from `payment.go`; `payment_test.go`, `admin_test.go` fixtures, `go.mod` entry, and `subscription.stripe_id` column drop all stay until Phase 8.
- **Apple `authorizationCode` exchange** — Phase 2 D-18 deferred; still deferred.
- **Admin SSO** — Phase 3 keeps admin password login. Apple/Google is consumer-only.
- **Admin UI sub-pages beyond plan-offer picker** — full admin panel overhaul is Phase 7 (ADMIN-01..08). Phase 3 only ships what's needed for offer-ID picking.
- **PERF-06 `RUN_SCHEDULER` env gate** — Phase 6. Phase 3 expiry cron runs in every replica; single-replica deployment makes this a no-op concern this milestone.
- **Webhook event log UI + replay button** — Phase 7 (ADMIN-06). `lava_webhook_events` table + `payload jsonb` column exist this phase to enable replay later.
- **KPI dashboard with MRR / paid users / churn** — Phase 7 (ADMIN-01).
- **Mid-cycle plan upgrade with proration** — out of scope project-wide (PROJECT.md). User cancels current + subscribes to new plan.
- **Email magic-link as a third SSO option (IDX-01)** — v2.
- **Multi-region/horizontal scale** — v2.

</deferred>

---

*Phase: 03-lava-top-plans-catalog*
*Context gathered: 2026-05-23 via interactive discussion grounded in ADR-007 §19*
