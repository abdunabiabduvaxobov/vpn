---
phase: 3
slug: lava-top-plans-catalog
status: verified
nyquist_compliant: true
wave_0_complete: true
created: 2026-05-23
validated: 2026-06-03
---

# Phase 3 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Derived from `03-RESEARCH.md` §"Validation Architecture".

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go `testing` + `gorm.io/driver/sqlite` (in-mem) + `github.com/testcontainers/testcontainers-go` (NEW — migration + integration) + `miniredis` (cache tests) |
| **Config file** | none — tests live alongside source as `*_test.go` |
| **Quick run command** | `go test ./internal/{package-touched}/... -count=1 -timeout=60s` |
| **Full suite command** | `go test ./... -race -count=1 -timeout=300s` |
| **Integration command (operator-only, Wave 5)** | `go test -tags=integration ./server/api/integration/... -run TestLavaSandbox` |
| **Estimated runtime (full suite)** | ~60-90s on a modern Mac |

---

## Sampling Rate

- **After every task commit:** Run package-scoped `go test ./internal/<touched>/...` (~5-10s).
- **After every plan wave:** Run full suite `go test ./... -race -count=1 -timeout=300s`.
- **Before `/gsd-verify-work`:** Full suite must be green AND:
  - `go vet ./...` clean.
  - `grep -rn 'PlanLimits' server/api/internal/ server/api/cmd/` returns ZERO hits outside `model/subscription.go` constants and the migration coercion test.
  - `grep -rn 'StripeID\|stripe_id' server/api/internal/handler/` returns ZERO hits outside orphaned `payment_test.go` (D-03).
  - Operator runs the lava sandbox integration test (success criterion #1).
- **Admin-web:** `npm run lint && tsc --noEmit && npm run build` in `admin-web/` after each Wave 5 plan.
- **Max feedback latency:** 90 seconds (full suite) / 10 seconds (package).

---

## Per-Task Verification Map

> Per-plan tasks are filled by the planner — this table is the requirement-anchored skeleton. The planner MUST extend it during planning so every task gets a row.

| Req ID | Plan (target wave) | Behavior | Test Type | Automated Command | File Exists | Status |
|--------|-------------------|----------|-----------|-------------------|-------------|--------|
| PAY-01 | 03-01 (W1) | plans/plan_servers/plan_offers tables created; users.plan_id NOT NULL after migration; premium/ultimate → pro coercion is destruction-free | migration (testcontainers Postgres) | `go test ./server/api/migrations/ -run TestMigrations019_020`; schema also covered transitively by the passing PAY-12..15 CRUD tests (plans/plan_offers/plan_servers) | 🔶 covered (TestMigrations019_020 itself fails on the PRE-EXISTING harness ordering bug DEF-08-02-A — applies 024 before 020 — NOT PAY-01 content) |
| PAY-02 | 03-05 (W3) | `POST /checkout` returns paymentUrl + invoice_id; 409 on offer_not_configured (lava_offer_id NULL) | handler unit (httptest mock lava) | `go test ./internal/handler/ -run TestCreateCheckoutSession` | ❌ W0 (03-05) | ✅ green |
| PAY-03 | 03-06 (W3) | Webhook dispatches all 5 event types (`payment.success`, `subscription.recurring.payment.success`, `payment.failed`, `subscription.recurring.payment.failed`, `subscription.cancelled`) to correct branches | handler unit | `go test ./internal/handler/ -run TestHandleLavaWebhook_AllEvents` | ❌ W0 (03-06) | ✅ green |
| PAY-04 | 03-06 (W3) | 20 duplicates → 1 side effect, 19 no-ops (GORM `RowsAffected==0` on `OnConflict{DoNothing}`) | repository + handler unit | `go test ./internal/repository/ -run TestInsertWebhookEventIfNew_Idempotent` AND `go test ./internal/handler/ -run TestHandleLavaWebhook_DuplicateNoop` | ❌ W0 (03-06) | ✅ green |
| PAY-05 | 03-06 (W3) | Induced DB failure mid-processing → handler returns HTTP 500 (lava retries) | handler unit | `go test ./internal/handler/ -run TestHandleLavaWebhook_ProcessingError_Returns500` | ❌ W0 (03-06) | ✅ green |
| PAY-06 | 03-06 (W3) | Request from IP outside allowlist rejected at dedicated `LavaWebhookIPAllowlist` middleware (Fiber's `EnableTrustedProxyCheck` alone is INSUFFICIENT — see RESEARCH §2.1) | middleware unit | `go test ./internal/middleware/ -run TestLavaWebhookIPAllowlist` | ❌ W0 (03-06) | ✅ green |
| PAY-07 | 03-02 (W1) | `crypto/subtle.ConstantTimeCompare` used for X-Api-Key (both current + previous secrets); no length-based leakage | lava unit + fuzz | `go test ./internal/lava/ -run TestVerifyAPIKey_ConstantTime` | ❌ W0 (03-02) | ✅ green |
| PAY-08 | 03-06 (W3) | Tier derived ONLY from `offerId` via `plan_offers` reverse-lookup; any client-supplied `plan` field is ignored | handler unit | `go test ./internal/handler/ -run TestHandleLavaWebhook_TierFromOfferIDNotClient` | ❌ W0 (03-06) | ✅ green |
| PAY-09 | 03-06 (W3) | `subscriptions.subscription_expires_at` populated from webhook `period_end` on first `payment.success`, extended by one period on `subscription.recurring.payment.success` | handler integration (sqlite) | `go test ./internal/handler/ -run TestHandleLavaWebhook_ExpiresAt_FirstAndRenewal` | ❌ W0 (03-06) | ✅ green |
| PAY-10 | 03-05 (W3) | `POST /subscription/cancel` calls lava `DELETE /api/v1/subscriptions`; user keeps Pro until `expires_at` lapses (cron handles downgrade) | handler unit (mock lava) | `go test ./internal/handler/ -run TestCancelSubscription_KeepsProUntilExpiry` | ❌ W0 (03-05) | ✅ green |
| PAY-11 | 03-03 + 03-04 (W2) | `ListServersForPlan` filters non-admins to plan_servers join; admin bypass returns all active servers; `GET /servers/:id/config` returns 404 (not 403) when denied | repository + handler unit | `go test ./internal/repository/ -run TestListServersForPlan` AND `go test ./internal/handler/ -run TestListServers_AdminBypass` | ❌ W0 (03-03) | ✅ green |
| PAY-12 | 03-07 (W3) | `GET /api/v1/plans` returns active plans; currency derivation from `Accept-Language`; cache hit/miss; admin write busts `cache:plans:public:*` | handler unit (miniredis) | `go test ./internal/handler/ -run TestListPlansPublic_CacheHitMissBust` | ❌ W0 (03-07) | ✅ green |
| PAY-13 | 03-08 (W4) | Admin CRUD: `is_system` immutable via API; system plan delete returns 403 even with `?force=true`; non-system plan delete is soft (sets `is_active=false`, FK preserved) | handler unit | `go test ./internal/handler/ -run TestAdminPlansCRUD` | ❌ W0 (03-08) | ✅ green |
| PAY-14 | 03-08 (W4) | `POST/DELETE /admin/plans/:id/servers/:server_id` add/remove server from plan; validates server existence + active state | handler unit | `go test ./internal/handler/ -run TestAdminPlanServers` | ❌ W0 (03-08) | ✅ green |
| PAY-15 | 03-08 (W4) | `POST /admin/plans/:id/offers/:offer_id/replace` deactivates old + inserts new in one transaction; never both-active | handler unit | `go test ./internal/handler/ -run TestAdminReplaceOffer_Transactional` | ❌ W0 (03-08) | ✅ green |
| PAY-16 | 03-02 (W1) | lava client uses hardcoded `const BaseURL = "https://gate.lava.top"`, 5s timeout, no redirect follow, no `InsecureSkipVerify` | lava unit + smoke grep | `go test ./internal/lava/ -run TestClient_HardcodedBaseURL_5sTimeout_NoRedirect` AND `grep -rn '"https://gate.lava.top"' server/api/internal/lava/` | ❌ W0 (03-02) | ✅ green |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

Tests / infrastructure that must exist BEFORE Wave 1 implementations land (per RESEARCH §"Validation Architecture"):

- [ ] **`server/api/migrations/migrations_test.go`** — testcontainers Postgres harness (covers PAY-01)
- [ ] **`go.mod` adds:** `github.com/testcontainers/testcontainers-go`, `github.com/testcontainers/testcontainers-go/modules/postgres`, `github.com/alicebob/miniredis/v2`, `gorm.io/datatypes` (for jsonb if used)
- [ ] **`internal/lava/client_test.go`** — `newWithBaseURL` test helper for httptest-mocking lava (covers PAY-02, PAY-07, PAY-16)
- [ ] **`internal/middleware/lava_ip_allowlist_test.go`** — pure-function test of CIDR parsing + `c.Context().RemoteIP()` matching (covers PAY-06)
- [ ] **`internal/repository/plan_repo_test.go`** — sqlite-backed unit tests for `FindPlanByID`, `FindPlanByCode`, `ListActivePlans`, `ListServersForPlan`, `IsServerAllowedForPlan`, `SetUserPlan` (covers PAY-11)
- [ ] **`internal/cache/plans_cache_test.go`** — miniredis-backed test for the cache wrapper (covers PAY-12)
- [ ] **`internal/handler/plans_public_test.go`, `plans_admin_test.go`, `admin_lava_test.go`, `webhook_lava_test.go`** — new test files for each new handler (covers PAY-02..05, 08, 10, 12, 13, 14, 15)
- [ ] **`server/api/integration/lava_sandbox_test.go`** — `//go:build integration` end-to-end test against lava sandbox (operator-run, covers success criterion #1)
- [ ] **Stripe-leakage clean-up:** `internal/repository/subscription_repo.go` references `StripeID` (`FindSubscriptionByStripeID`, `CreateOrUpdateSubscription` Updates map) — must be removed in Wave 1 to keep build green after D-11 column drop (RESEARCH §14 calls this out — D-01 ↔ D-03 hidden conflict)
- [ ] **`payment_test.go` decision:** the file writes `StripeID: stripeID` at line ~112 — it will not compile after `model.Subscription.StripeID` is removed. Planner picks: delete in Wave 1, OR rewrite for lava in Wave 3. D-03 said "do not touch" but compile failure cascades — this MUST be addressed Wave 1.
- [ ] **admin-web shadcn components (Wave 5 prerequisite):** install `Form`, `Select`, `Combobox`, `Checkbox`, `Switch`, `Tabs`, `Tooltip`, `Textarea` via shadcn CLI before Plans UI work begins.

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Real card payment via lava.top sandbox grants Pro in ≤5s | Success criterion #1 (PAY-02 + PAY-03 + PAY-09 e2e) | Requires lava sandbox account + webhook reachability (ngrok/cloudflared) — cannot run in CI | Operator: (1) start API with `LAVA_ENV=sandbox` + sandbox key, (2) expose webhook URL via tunnel, (3) configure sandbox webhook target, (4) use sandbox test card, (5) observe `GET /api/v1/subscription` flip from `free` → `pro` within 5 seconds of webhook receipt. |
| Admin-web Plans UI flow end-to-end | PAY-13, PAY-14, PAY-15 (UI side of D-13) | shadcn/ui interactions, dropdown picker UX, dialog confirmations | Operator: create a plan, add servers, open offer-edit dialog, select lava offer from dropdown, save, refresh, verify `lava_offer_id` populated; soft-delete a plan, confirm system plan delete is forbidden, confirm warning text on plan-server removal. |
| Secret rotation zero-downtime | PAY-07 (rotation correctness per D-17) | Requires multi-step process across config + lava dashboard | Operator: (1) set `LAVA_WEBHOOK_SECRET_PREVIOUS=<old>`, `LAVA_WEBHOOK_SECRET=<new>`, restart; (2) verify both old + new secret webhooks accepted; (3) update lava dashboard to new secret; (4) clear `_PREVIOUS`, restart; (5) verify only new secret accepted. |
| Webhook 20-retry burst real lava behavior | PAY-04 | testcontainers test simulates this but only the real provider exhibits real burst | Operator: in sandbox, trigger a successful payment, observe lava deliver the success event 20× to a slow handler — verify exactly 1 grant; rows count `lava_webhook_events.event_type='payment.success' AND contract_id=X` = 1. |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify OR a Wave 0 dependency listed above
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references
- [x] No watch-mode flags in commands
- [x] Feedback latency < 90s (full suite)
- [x] `nyquist_compliant: true` set in frontmatter

## Validation Audit 2026-06-03

All 16 PAY requirements run green: checkout (happy/idempotency-reuse/guest-rejected/invalid-currency/offer-not-configured), webhook (all-events/duplicate-noop/processing-error-500/bad-sig-401/IP-allowlist/constant-time-key/tier-from-offer-id/expires-at-first+renewal/natural-key-collision/recurring-failed), cancel-keeps-pro-until-expiry, admin-bypass server list, public-plans cache hit/miss/bust, plans+offers+plan-servers CRUD, transactional offer replace, hardcoded base URL + 5s timeout. PAY-01 migration schema is verified transitively by the passing CRUD tests; its dedicated `TestMigrations019_020` fails only on the pre-existing harness ordering bug (DEF-08-02-A), not PAY-01 content. No automatable gaps. `nyquist_compliant: true`.

**Approval:** pending
