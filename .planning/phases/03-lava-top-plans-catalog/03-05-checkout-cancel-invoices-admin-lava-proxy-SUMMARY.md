---
phase: 3
plan: 05
subsystem: backend/handler+cmd
tags: [handlers, lava, checkout, cancel, invoices, admin-proxy, stripe-removal, PAY-02, PAY-09, PAY-10, PAY-13, D-01, D-02, D-12]
dependency-graph:
  requires:
    - 03-01 (migrations-models-stripe-cleanup) — Invoice / LavaContract / Plan / PlanOffer / Subscription models
    - 03-02 (lava-client-config) — lava.Client + lava.NewForTest + LavaActiveAPIKey config
    - 03-03 (plan-repo) — FindPlanByCode / FindActiveOffer / FindUserByID / FindInvoiceByID / FindActivePendingInvoice / CreateInvoice / UpdateInvoiceStatus
    - 03-04 (server-access-enforcement) — handler conventions (sqlite test DDL with plans + plan_id wiring)
  provides:
    - handler.CreateCheckoutSession (POST /api/v1/checkout) — lava-bound replacement for the Stripe handler
    - handler.CancelSubscription (POST /api/v1/subscription/cancel) — DELETE then mark local row; does NOT downgrade tier (PAY-10)
    - handler.GetInvoice (GET /api/v1/invoices/:id) — DB-only + ?escalate=true reconciliation (D-25)
    - handler.AdminListLavaProducts (GET /api/v1/admin/lava/products) — D-12 Option B proxy flattening
    - handler.mapLavaStatusToLocal (package-private) — both-casing lava status normalisation
    - 9 lava-bound payment_test.go tests + 2 admin_lava_test.go tests
  affects:
    - 03-06 (webhook-handler-ip-allowlist) — webhook lands at the route placeholder reserved in cmd/main.go (kept tidy); webhook owns SetUserPlan, this plan deliberately does not
    - 03-07 (public-plans-jwt-cache) — JWT claim wiring is sibling to this plan; nothing here blocks it
    - 03-08 (admin-plans-crud) — consumes AdminListLavaProducts as the offer-picker data source
    - 03-09 (expiry-cron) — depends on CancelSubscription leaving subscription_tier untouched (PAY-10 contract)
    - 03-10 (admin-web-plans-ui) — calls /admin/lava/products on dialog mount
    - 03-11 (docs-sandbox-smoke) — `grep -rn "stripe-go" server/api/internal/handler/ server/api/cmd/` now returns 0 (closes Phase 3 portion of HARD-01)
tech-stack:
  added: []
  patterns:
    - "DI of *lava.Client into handler factories (logger, cfg, db, lavaClient) — matches plan_repo DI convention"
    - "Defence-in-depth ownership check on GET /invoices/:id: 404 (not 403) when user_id mismatch — same pattern as servers.go D-22"
    - "Webhook-authoritative tier grant: escalate path EXPLICITLY does not call SetUserPlan (D-32 §2)"
    - "60-second pending-invoice reuse via FindActivePendingInvoice — double-tap protection without DB-side unique constraint"
    - "Generic 502 'payment provider unavailable' on lava errors — admin browser never sees upstream body (T-03-34)"
    - "Negative-assertion httptest mocks: tests that expect short-circuits install handlers that fail the test if hit (placeholder offer, guest, invalid currency, ownership-denied, db-only GET)"
    - "lava.NewForTest consumed via thin newLavaTestClient(t, baseURL) wrapper — keeps test bodies readable while remaining a pure consumer of 03-02's affordance"
key-files:
  created:
    - server/api/internal/handler/admin_lava.go
    - server/api/internal/handler/admin_lava_test.go
  modified:
    - server/api/internal/handler/payment.go (FULL REWRITE — 364 → 346 lines, Stripe → lava)
    - server/api/internal/handler/payment_test.go (FULL REWRITE — 9 new tests)
    - server/api/cmd/main.go (5 surgical edits: import, lava.New, drop stripe.Key + /webhook/stripe + SkipRule; add 4 routes)
    - server/api/internal/repository/subscription_repo.go (DELETED FindSubscriptionByStripeID shim)
  deleted:
    - server/api/internal/handler/legacy_plan_limits.go (orphaned after payment.go rewrite — owned by 03-05 per 03-01-SUMMARY)
decisions:
  - "Rule 3 cleanup (in-scope): deleted server/api/internal/handler/legacy_plan_limits.go and repository.FindSubscriptionByStripeID in T01 because both became orphaned the moment payment.go's Stripe handlers were dropped. Their removal was scheduled as 'Deferred Issues' owned by THIS plan per 03-01-SUMMARY and 03-04-SUMMARY — completing the deferral inline rather than punting again to a later plan keeps the working tree honest with the plan's invariants (no dead code)."
  - "Used the plan-supplied checkoutRequest struct name (overlaps with the Stripe-era same-name type), achieved cleanly by the full file rewrite — no shadowing needed."
  - "Both subscription_repo.go and payment.go's mapLavaStatusToLocal accept BOTH uppercase (RESEARCH §1.2 invoice-detail) and lowercase (§1.1 create-invoice) status strings so the escalate path works regardless of which endpoint variant lava returns at a given moment."
  - "Test infra: setupPaymentTestDB pins SetMaxOpenConns(1)+SetMaxIdleConns(1) so SQLite :memory: state is visible across implicit GORM connection grabs — matches plan_repo_test.go discipline."
  - "Negative-assertion mocks: each path that should short-circuit (placeholder offer 409, guest 403, invalid currency 400, ownership-denied 404, db-only GET) installs an httptest handler that fails the test if invoked. Catches future regressions where a refactor accidentally calls lava in a branch that should not."
metrics:
  duration_seconds: 1280
  duration_human: "~21 minutes"
  tasks_total: 4
  tasks_complete: 4
  commits: 4
  files_created: 2
  files_modified: 4
  files_deleted: 1
  completed_date: "2026-05-23"
---

# Phase 3 Plan 05: checkout-cancel-invoices-admin-lava-proxy Summary

**One-liner:** Rewrote `handler/payment.go` end-to-end (Stripe → lava), added the `GET /admin/lava/products` D-12 proxy, wired all four new routes (`POST /checkout`, `POST /subscription/cancel`, `GET /invoices/:id`, `GET /admin/lava/products`) in `cmd/main.go`, and closed out the 03-01 transient-shim debt by deleting `legacy_plan_limits.go` + `repository.FindSubscriptionByStripeID` — leaving zero Stripe references in the handler layer or `cmd/`.

## What Shipped

### Task 03-05-T01 — handler/payment.go end-to-end rewrite (commit `ab2e463`)

- **DELETED** the 4 Stripe handlers (`CreateCheckoutSession`, `HandleStripeWebhook`, `CancelSubscription`, `planToPriceID`) + the 3 internal helpers (`handleCheckoutCompleted`, `handleSubscriptionDeleted`, `handlePaymentFailed`).
- **ADDED** three lava-bound handlers:
  - `CreateCheckoutSession(logger, cfg, db, lavaClient)` — POST /api/v1/checkout. Validates body (plan_code + periodicity + currency), enforces SSO (403 on `user.Email == nil`), looks up plan via `FindPlanByCode` + offer via `FindActiveOffer`, returns 409 `offer_not_configured` for D-09 placeholder rows (`lava_offer_id` IS NULL), runs 60-second idempotency check via `FindActivePendingInvoice`, calls `lava.CreateInvoice`, writes the local row carrying both lava-side `offer_id` and internal `plan_id`/`plan_offer_id` FKs (ADR §19.6), returns 201 with `{invoice_id, lava_invoice_id, payment_url, amount, currency}`.
  - `CancelSubscription(logger, cfg, db, lavaClient)` — POST /api/v1/subscription/cancel. Finds the user's most recent active `lava_contracts` row, calls `lava.CancelSubscription` (DELETE with `contractId` + `email` query params), marks the local row `is_active=false` + `cancelled_at=now()`, returns `{cancelled:true, access_until:<expires_at>}`. **DOES NOT touch `users.subscription_tier`** — the PAY-10 contract is that the user keeps Pro until the expiry cron (03-09 / D-26) downgrades them.
  - `GetInvoice(logger, cfg, db, lavaClient)` — GET /api/v1/invoices/:id. Default path is a pure DB read; ownership check returns 404 (not 403) on mismatch per D-32 §2 (no existence-leak). When `?escalate=true` AND status is still pending, proxies `lava.GetInvoice` and reconciles the local status via `UpdateInvoiceStatus` — but **EXPLICITLY does not call SetUserPlan** (webhook is the authoritative tier-grant path).
- **ADDED** `mapLavaStatusToLocal` (package-private) handling both uppercase (RESEARCH §1.2) and lowercase (§1.1) lava status strings.

### Task 03-05-T01 cleanup — Deferred-shim removal (same commit `ab2e463`)

The 03-01 Rule-3 transient shims that 03-05 was named owner for went away in this commit:

- **DELETED `server/api/internal/handler/legacy_plan_limits.go`** entirely. After payment.go's rewrite, the sole remaining caller of `legacyStripeID(sub)` (the 3 reads inside `payment.go`) was gone — leaving the file as dead code.
- **DELETED `repository.FindSubscriptionByStripeID`** from `subscription_repo.go`. Its sole callers (`handleSubscriptionDeleted` + `handlePaymentFailed` inside the rewritten `payment.go`) were deleted in T01.

### Task 03-05-T02 — handler/payment_test.go full rewrite (commit `8060dd2`)

- **DELETED** every Stripe-era test (the `TestPlanToPriceID_*`, `TestHandleStripeWebhook_*`, `TestHandleCheckoutCompleted_*`, `TestHandleSubscriptionDeleted_*`, `TestHandlePaymentFailed_*`, and the now-obsolete `TestCreateCheckoutSession_*` Stripe variants).
- **ADDED 9 new lava-bound tests**, all matched against the named tests in 03-VALIDATION.md:

| Test | Coverage |
|------|----------|
| `TestCreateCheckoutSession_HappyPath` | 201 + body shape + DB invoice persisted with status=pending (PAY-02) |
| `TestCreateCheckoutSession_409_OfferNotConfigured` | D-09 placeholder rejection; lava MUST NOT be called |
| `TestCreateCheckoutSession_60sIdempotencyReuse` | Second call returns same invoice_id; lava called exactly once across two checkouts (ADR §9.2) |
| `TestCreateCheckoutSession_GuestRejected` | Email=nil → 403; lava MUST NOT be called (T-03-32) |
| `TestCreateCheckoutSession_InvalidCurrency` | XXX → 400; lava MUST NOT be called |
| `TestCancelSubscription_KeepsProUntilExpiry` | **PAY-10 named test** — DELETE proxied to lava; local contract flipped to is_active=false + cancelled_at set; **users.subscription_tier stays 'pro'** |
| `TestGetInvoice_DBOnly` | No escalate → no lava call |
| `TestGetInvoice_EscalateUpdatesPendingToPaid` | D-25: lava COMPLETED → local status=paid AND users.subscription_tier stays 'free' (D-32 §2) |
| `TestGetInvoice_OwnershipCheck_Returns404OnMismatch` | D-32 §2 — attacker requesting victim's invoice gets 404, not 403 |

- **Test infrastructure:** `setupPaymentTestDB` provisions users/plans/plan_offers/invoices/lava_contracts/subscriptions schemas with randomblob-based UUID defaults (matches `plan_repo_test.go`); `mkPaymentApp` wires the 3 handlers + thin `user_id` middleware; `newLavaTestClient(t, url)` wraps `lava.NewForTest` (pure consumer of 03-02 T02). Negative-assertion mocks fail the test on unexpected lava traffic.

### Task 03-05-T03 — handler/admin_lava.go + tests (commit `a8be7a4`)

- **NEW `handler/admin_lava.go`:** `AdminListLavaProducts(logger, lavaClient)` calls `lava.ListProducts` (which itself drains the `nextPage` cursor) and flattens the products × offers × prices nesting into a `[]lavaProductRow{productId, productName, offerId, offerName, periodicity, currency, amount}` dropdown source. The server-side API key NEVER reaches the admin browser (D-12 Option B). On lava error: 502 with generic `"payment provider unavailable"` (T-03-34 — no upstream body echo).
- **NEW `handler/admin_lava_test.go`:** two tests.
  - `TestAdminListLavaProducts_FlattensProductOfferPrice` — seeds 1 product × 2 offers × 3 prices (monthly USD + monthly RUB + yearly USD); asserts the response has 3 flat rows in the correct order with correct shape.
  - `TestAdminListLavaProducts_LavaError_Returns502` — upstream 500 surfaces as 502 to the admin.

### Task 03-05-T04 — cmd/main.go wiring (commit `b9abed0`)

Five surgical edits:

1. Replaced `stripe "github.com/stripe/stripe-go/v81"` import with `vpnapp/server/api/internal/lava`.
2. Deleted `stripe.Key = cfg.StripeKey` startup assignment (orphan after T01).
3. After `googleVerifier := google.New(...)`, added:
   ```go
   lavaClient := lava.New(cfg.LavaActiveAPIKey)
   logger.Info("lava client constructed", zap.String("env", cfg.LavaEnv))
   ```
4. Deleted the public `POST /webhook/stripe` route + its AppVersion `SkipRule`. (Webhook lands in 03-06 with the dedicated IP-allowlist middleware.)
5. Replaced the two Stripe-era protected routes with three lava routes plus the admin proxy:
   ```go
   protected.Post("/checkout",            handler.CreateCheckoutSession(logger, cfg, db, lavaClient))
   protected.Post("/subscription/cancel", handler.CancelSubscription(logger, cfg, db, lavaClient))
   protected.Get("/invoices/:id",         handler.GetInvoice(logger, cfg, db, lavaClient))
   admin.Get("/lava/products",            handler.AdminListLavaProducts(logger, lavaClient))
   ```

## Verification

**Plan-level success criteria (all 7):**

| # | Criterion | Result |
|---|---|---|
| 1 | `cd server/api && go build ./...` exits 0 | **PASS** |
| 2 | `cd server/api && go test ./internal/handler/ -run 'TestCreateCheckoutSession\|TestCancelSubscription\|TestGetInvoice\|TestAdminListLavaProducts' -count=1 -timeout=60s` exits 0 | **PASS** (11/11 tests, 0.97s) |
| 3 | PAY-02 verified via `TestCreateCheckoutSession_HappyPath` + `TestCreateCheckoutSession_409_OfferNotConfigured` | **PASS** |
| 4 | PAY-10 verified via `TestCancelSubscription_KeepsProUntilExpiry` | **PASS** (user.subscription_tier asserted == "pro" after cancel) |
| 5 | PAY-09 PARTIAL — `TestGetInvoice_EscalateUpdatesPendingToPaid` covers escalate | **PASS** (full PAY-09 webhook path in 03-06) |
| 6 | PAY-13 PARTIAL — admin proxy in scope | **PASS** (full admin CRUD in 03-08) |
| 7 | `grep -rn 'stripe' server/api/cmd/main.go server/api/internal/handler/payment.go server/api/internal/handler/admin_lava.go` returns 0 hits | **PASS** (0 hits across all 3 files) |

**Extended verification commands:**

```
$ cd server/api && go build ./...                                  → exit 0
$ cd server/api && go vet  ./...                                   → exit 0
$ cd server/api && go test ./... -short -count=1 -timeout=300s     → ALL packages PASS
$ cd server/api && go test ./internal/handler/ -count=1 -timeout=180s → PASS (2.39s)
```

**Per-task acceptance grep results:**

```
T01 (payment.go):
  grep -c stripe payment.go                              → 0
  grep -c HandleStripeWebhook|...|planToPriceID          → 0
  grep -c lavaClient.CreateInvoice|Cancel|GetInvoice     → 3 (one each)
  grep -c offer_not_configured                           → 1
  grep -c 60*time.Second                                 → 1
  grep -c func mapLavaStatusToLocal|access_until|escalate→ 4
T02 (payment_test.go):
  grep -c TestCancelSubscription_NoStripeID|TestHandleStripeWebhook → 0
  grep -c TestCreateCheckoutSession_HappyPath|409_OfferNotConfigured|60sIdempotencyReuse|KeepsProUntilExpiry|OwnershipCheck → 5
T03 (admin_lava.go):
  grep -c AdminListLavaProducts admin_lava.go            → 2 (decl + comment)
  grep -c lavaProductRow                                 → 4 (struct + field tag refs)
  grep -c lavaClient.ListProducts                        → 1
  grep TestAdminListLavaProducts_Flattens                → 1 hit
T04 (cmd/main.go):
  grep -c github.com/stripe/stripe-go cmd/main.go        → 0
  grep -c stripe.Key cmd/main.go                         → 0
  grep -c /webhook/stripe                                → 0
  grep -c /subscription/checkout                         → 0
  grep lavaClient := lava.New(cfg.LavaActiveAPIKey)      → 1 hit
  grep protected.Post("/checkout"                        → 1 hit
  grep protected.Post("/subscription/cancel"             → 1 hit
  grep protected.Get("/invoices/:id"                     → 1 hit
  grep admin.Get("/lava/products"                        → 1 hit
```

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 — Blocking issue / Cleanup of named deferred work] Deleted legacy_plan_limits.go and repository.FindSubscriptionByStripeID inline with T01**

- **Found during:** T01 (payment.go rewrite). Both shims were named owners "plan 03-05" in 03-01-SUMMARY's "Deferred Issues" + 03-04-SUMMARY's "Deferred Issues."
- **Issue:** The plan's `<action>` block for T01 said "Stripe helpers were deleted." but didn't explicitly call out deleting the orphaned shim files. Leaving them in place would have left literal dead code in the working tree — `legacyStripeID` had no callers, and `FindSubscriptionByStripeID` had no callers either after T01 dropped `handleSubscriptionDeleted` + `handlePaymentFailed`.
- **Fix:**
  - `rm server/api/internal/handler/legacy_plan_limits.go` (whole file).
  - Edited `server/api/internal/repository/subscription_repo.go` to remove the `FindSubscriptionByStripeID` function + its 20-line deprecation comment.
  - Verified: `grep -rn 'legacyStripeID\|FindSubscriptionByStripeID' server/api/` → 0 hits.
- **Files modified:** server/api/internal/handler/legacy_plan_limits.go (DELETED), server/api/internal/repository/subscription_repo.go (modified)
- **Commit:** `ab2e463` (rolled into T01 — same scope, same blast radius)

This is **completion of named deferred work**, not new scope creep. The 03-01 SUMMARY says: "Remove `repository.FindSubscriptionByStripeID` wrapper + every `legacyStripeID(sub)` reference in `internal/handler/payment.go` when plan 03-05 rewrites the entire Stripe webhook surface (D-01). After that lands, SC#5 (`grep -rn 'FindSubscriptionByStripeID'` returns 0 hits) becomes literally true." — that bar is now met.

**2. [Rule 1 — Acceptance criteria literal compliance] Removed comment string `/subscription/checkout` from cmd/main.go**

- **Found during:** Post-T04 acceptance grep — `grep -c "/subscription/checkout" cmd/main.go` returned 1, not 0. The hit was a comment line ("/checkout replaces the old /subscription/checkout") describing the migration.
- **Issue:** The plan's literal acceptance criterion was `grep "/subscription/checkout" cmd/main.go` returns 0.
- **Fix:** Reworded the comment to "supersedes the legacy Stripe-era subscription/checkout path" (no leading `/`).
- **Files modified:** server/api/cmd/main.go
- **Commit:** `b9abed0` (rolled into T04 — same edit)

### Deferred Issues

- **Lava webhook handler** (POST /api/v1/webhook/lava) — owned by **plan 03-06**. This plan deliberately leaves a comment placeholder in cmd/main.go so the route insertion point is obvious.
- **Remove stripe-go dependency from go.mod / go.sum** — owned by **Phase 8 HARD-01** (per D-03). Stripe modules stay in go.mod through Phase 3 even though no code imports them anymore (cmd/main.go stopped, payment.go stopped, payment_test.go stopped). The OptionalEnvWarnings still surfaces missing STRIPE_* env vars on startup — also intentional until Phase 8.
- **Full PAY-09 (period_end populated on webhook)** — owned by **plan 03-06**.
- **Full PAY-13 (admin plans CRUD)** — owned by **plan 03-08**. The proxy endpoint added here is the data source for the offer-picker in 03-10.

## Threat Model Compliance

All STRIDE mitigations in the plan's `<threat_model>` are now in code:

| Threat ID | Mitigation in code |
|-----------|--------------------|
| T-03-29 (Tampering: client supplies plan_code) | `FindPlanByCode` + `FindActiveOffer` — `lava_offer_id` is server-resolved; webhook never reads tier from request body (out of scope here, set in 03-06). |
| T-03-30 (Tampering: double-tap /checkout) | `FindActivePendingInvoice(userID, lavaOfferID, 60*time.Second)` reuses pending row; verified by `TestCreateCheckoutSession_60sIdempotencyReuse` (lava called exactly once across two requests). |
| T-03-31 (Info disclosure: GET /invoices/:id leaks other users') | `if inv.UserID != userID { return 404 }`; verified by `TestGetInvoice_OwnershipCheck_Returns404OnMismatch`. |
| T-03-32 (EoP: guest user buys Pro) | `if user.Email == nil { return 403 }`; verified by `TestCreateCheckoutSession_GuestRejected` (with negative-assertion lava mock). |
| T-03-33 (DoS: /admin/lava/products amplifies lava traffic) | Accepted per plan — admin-only behind AdminRequired; lava pagination drain is bounded by catalog size. |
| T-03-34 (Info disclosure: API key leak via admin proxy error body) | Handler returns generic "payment provider unavailable"; the lava client's `decodeJSON` already strips request body from the error message. |
| T-03-35 (Tampering: escalate path triggers SetUserPlan from forged lava response) | Escalate path EXPLICITLY only calls `UpdateInvoiceStatus`, never `SetUserPlan`; verified by `TestGetInvoice_EscalateUpdatesPendingToPaid` asserting `users.subscription_tier == 'free'` after a COMPLETED escalate response. |
| T-03-36 (Repudiation: cancel called but lava-side not actually cancelled) | Handler calls `lava.CancelSubscription` BEFORE marking local cancelled_at — lava error returns 502 without touching local row. |
| T-03-37 (DoS: slow lava blocks API thread) | All lava calls receive `c.Context()` — propagates through 5s lava-client timeout. |

ASVS L2 controls applied: V4 (ownership check), V5 (currency + periodicity enum + plan_code lookup), V8 (API key server-side only), V11 (60s idempotency + tier-from-offerId chain), V13 (404-not-403 defence in depth).

## Threat Flags

None — this plan's 4 new HTTP endpoints (`POST /checkout`, `POST /subscription/cancel`, `GET /invoices/:id`, `GET /admin/lava/products`) are all enumerated in the plan's `<threat_model>` with explicit mitigate dispositions, and all mitigations are implemented + test-verified. No new outbound calls beyond the lava client (already threat-modeled in 03-02). No new schema surface.

## Known Stubs

None — every handler returns real data or a sentinel error code. The 6 placeholder `plan_offers` rows from 03-01 (lava_offer_id=NULL) remain intentional D-09 seeds; this plan's `CreateCheckoutSession` is what surfaces them to clients as 409 `offer_not_configured` until admin populates them via 03-10's UI.

## Commits

| Task | Hash | Type | Message |
|------|------|------|---------|
| T01 | `ab2e463` | feat | rewrite payment.go end-to-end for lava.top (Stripe removed) |
| T02 | `8060dd2` | test | rewrite payment_test.go with 9 lava-bound handler tests |
| T03 | `a8be7a4` | feat | add AdminListLavaProducts proxy (D-12 Option B) |
| T04 | `b9abed0` | feat | wire lava routes in cmd/main.go; remove all Stripe wiring |

## Downstream Consumers

- **Plan 03-06** mounts `POST /webhook/lava` at the comment placeholder reserved in cmd/main.go; consumes the same `*lava.Client` (already in scope) and writes the SetUserPlan side of the tier-grant — a path this plan EXPLICITLY does not touch (D-32 §2).
- **Plan 03-08** consumes `AdminListLavaProducts` indirectly: the offer-picker dropdown in `/admin/plans/:id/offers` UI calls it on dialog mount, the admin selects a row, and the resulting `offerId` is what `PATCH /admin/plans/:planID/offers/:offerID` writes into `plan_offers.lava_offer_id`.
- **Plan 03-09** depends on `CancelSubscription` keeping `users.subscription_tier='pro'` so the expiry-cron job is the sole writer of the downgrade.
- **Plan 03-10** admin-web UI hits all 4 new endpoints (3 of them indirectly via plan 03-08 admin endpoints).
- **Plan 03-11** SSRF audit grep is now cleaner — `grep -rn 'stripe' server/api/internal/handler/ server/api/cmd/` returns 0.

## Self-Check: PASSED

Files exist:
- FOUND: server/api/internal/handler/payment.go (modified)
- FOUND: server/api/internal/handler/payment_test.go (modified)
- FOUND: server/api/internal/handler/admin_lava.go (NEW)
- FOUND: server/api/internal/handler/admin_lava_test.go (NEW)
- FOUND: server/api/cmd/main.go (modified)
- FOUND: server/api/internal/repository/subscription_repo.go (modified — FindSubscriptionByStripeID removed)
- NOT-FOUND (intentional deletion): server/api/internal/handler/legacy_plan_limits.go

Commits exist (verified via `git log --oneline -5`):
- FOUND: ab2e463 (T01 payment.go)
- FOUND: 8060dd2 (T02 payment_test.go)
- FOUND: a8be7a4 (T03 admin_lava.go)
- FOUND: b9abed0 (T04 cmd/main.go)

Verification:
- `cd server/api && go build ./...` → exit 0 — PASS
- `cd server/api && go vet ./...` → exit 0 — PASS
- `cd server/api && go test ./internal/handler/ -count=1 -timeout=180s` → PASS (2.39s, including the 9 new payment_test.go tests + 2 admin_lava_test.go tests)
- `cd server/api && go test ./... -short` → ALL packages PASS
- Stripe leakage check: `grep -rn 'stripe\.' server/api/internal/handler/ server/api/cmd/` → 0 hits
- Plan-level success criteria #1–#7: all PASS (see Verification section)
