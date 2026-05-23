---
phase: 3
plan: 05
type: execute
slug: lava-top-plans-catalog
plan_number: 5
wave: 3
depends_on: [1, 2, 3]
files_modified:
  - server/api/internal/handler/payment.go
  - server/api/internal/handler/payment_test.go
  - server/api/internal/handler/admin_lava.go
  - server/api/internal/handler/admin_lava_test.go
  - server/api/cmd/main.go
autonomous: true
requirements_addressed: [PAY-02, PAY-09, PAY-10, PAY-13]
estimated_complexity: high
must_haves:
  refs:
    - "See <must_haves> XML block in plan body for canonical truths / artifacts / key_links"

---

<objective>
Rewrite `handler/payment.go` in-place (D-01) — delete all 4 Stripe handlers + helpers, replace with the lava-bound stack: `CreateCheckoutSession` (POST /checkout), `CancelSubscription` (POST /subscription/cancel), `GetInvoice` (GET /invoices/:id with `?escalate=true`), plus the new `AdminListLavaProducts` (GET /admin/lava/products — D-12 Option B proxy) in a sibling file `handler/admin_lava.go`. Wire all new routes in `cmd/main.go`, remove the dead Stripe routes, construct the `*lava.Client` once at startup. The webhook handler (POST /webhook/lava) is OUT OF SCOPE for this plan — it lands in 03-06.
</objective>

<context>
@.planning/phases/03-lava-top-plans-catalog/03-CONTEXT.md
@.planning/phases/03-lava-top-plans-catalog/03-RESEARCH.md
@server/api/internal/handler/payment.go
@server/api/internal/lava/client.go
@server/api/internal/lava/invoice.go
@server/api/internal/lava/subscription.go
@server/api/internal/lava/dto.go
@server/api/internal/repository/plan_repo.go
@server/api/internal/repository/invoice_repo.go
@server/api/cmd/main.go
</context>

<interfaces>
Public endpoints this plan ADDS to /api/v1 (auth required unless noted):

```
POST   /api/v1/checkout                           -> {payment_url, invoice_id, lava_invoice_id, amount, currency}
POST   /api/v1/subscription/cancel                -> {cancelled: true, access_until: <expires_at>}
GET    /api/v1/invoices/:id?escalate=true|false   -> {id, lava_invoice_id, status, amount, currency, ...}
GET    /api/v1/admin/lava/products                -> [{productId, productName, offerId, offerName, periodicity, currency, amount}]
```

Removed routes (D-02):
```
POST /api/v1/subscription/checkout    (replaced by /checkout)
POST /api/v1/webhook/stripe           (lava webhook lands in 03-06)
```

The `*lava.Client` is constructed in cmd/main.go via `lava.New(cfg.LavaActiveAPIKey)` once at startup; injected into the handler constructors.

Handler signatures the plan adds:

```go
func CreateCheckoutSession(logger *zap.Logger, cfg *config.Config, db *gorm.DB, lavaClient *lava.Client) fiber.Handler
func CancelSubscription(logger *zap.Logger, cfg *config.Config, db *gorm.DB, lavaClient *lava.Client) fiber.Handler
func GetInvoice(logger *zap.Logger, cfg *config.Config, db *gorm.DB, lavaClient *lava.Client) fiber.Handler
func AdminListLavaProducts(logger *zap.Logger, lavaClient *lava.Client) fiber.Handler
```

Request body for POST /checkout (ADR §10.3 — `plan_code` not `plan`):
```json
{ "plan_code": "pro", "periodicity": "MONTHLY", "currency": "USD", "clientUtm": null }
```

Response 201:
```json
{
  "data": {
    "invoice_id": "<our uuid>",
    "lava_invoice_id": "<lava id>",
    "payment_url": "https://app.lava.top/pay/...",
    "amount": 5.00,
    "currency": "USD",
    "expires_at": null
  }
}
```

Error 409 when `FindActiveOffer.LavaOfferID == nil` (D-09 placeholder offer):
```json
{ "error": "offer_not_configured" }
```
</interfaces>

<tasks>

<task type="auto">
  <id>03-05-T01</id>
  <name>Rewrite handler/payment.go (delete Stripe, add CreateCheckoutSession + CancelSubscription + GetInvoice)</name>
  <files>server/api/internal/handler/payment.go</files>
  <read_first>
    - server/api/internal/handler/payment.go (CURRENT — full Stripe implementation; this plan REWRITES the file end-to-end)
    - server/api/internal/lava/invoice.go (T02 of plan 03-02 — CreateInvoice + GetInvoice signatures)
    - server/api/internal/lava/subscription.go (T02 of plan 03-02 — CancelSubscription signature)
    - server/api/internal/lava/dto.go (T02 of plan 03-02 — CreateInvoiceRequest + InvoiceResponse + InvoiceDetailResponse shapes)
    - server/api/internal/repository/plan_repo.go (FindPlanByCode + FindActiveOffer)
    - server/api/internal/repository/invoice_repo.go (CreateInvoice + FindInvoiceByID + FindActivePendingInvoice + UpdateInvoiceStatus + FindInvoiceByLavaID)
    - .planning/phases/03-lava-top-plans-catalog/03-CONTEXT.md D-01 (rewrite in-place), D-09 (409 offer_not_configured), D-10 (60s idempotency reuse), D-25 (?escalate=true), D-32 §2 (payment-data integrity)
    - docs/ADR-007-lava-sso-rework.md §9.2 (60s reuse-pending), §9.4 (polling fallback), §10.3 (request shape), §10.6 (cancel keeps Pro), §10.7 (GetInvoice)
  </read_first>
  <action>
    **REWRITE** `server/api/internal/handler/payment.go` entirely. Delete ALL existing Stripe code (the 4 Stripe handlers + 4 helpers). New file body:

```go
package handler

import (
	"errors"
	"fmt"
	"time"

	"vpnapp/server/api/internal/config"
	"vpnapp/server/api/internal/lava"
	"vpnapp/server/api/internal/model"
	"vpnapp/server/api/internal/repository"

	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// allowedCurrencies enumerates the lava-supported currencies. Mirrors the
// CHECK constraint in migration 019 + 020.
var allowedCurrencies = map[string]struct{}{
	"USD": {}, "EUR": {}, "RUB": {},
}

// allowedPeriodicities enumerates the lava-supported periodicities. Mirrors
// the CHECK constraint in migration 019.
var allowedPeriodicities = map[string]struct{}{
	"ONE_TIME":         {},
	"MONTHLY":          {},
	"PERIOD_90_DAYS":   {},
	"PERIOD_180_DAYS":  {},
	"PERIOD_YEAR":      {},
}

// checkoutRequest is the POST /checkout body (ADR §10.3).
type checkoutRequest struct {
	PlanCode    string            `json:"plan_code"`
	Periodicity string            `json:"periodicity"`
	Currency    string            `json:"currency"`
	ClientUtm   map[string]string `json:"clientUtm,omitempty"`
}

// checkoutResponse is the POST /checkout response.
type checkoutResponse struct {
	InvoiceID     string  `json:"invoice_id"`
	LavaInvoiceID string  `json:"lava_invoice_id"`
	PaymentURL    string  `json:"payment_url"`
	Amount        float64 `json:"amount"`
	Currency      string  `json:"currency"`
}

// CreateCheckoutSession handles POST /api/v1/checkout (PAY-02).
//
// Flow (ADR §9.2 + §19.6):
//   1. Validate body (plan_code, periodicity, currency).
//   2. Load user — require email present (SSO-bound users only; guest users get 403).
//   3. FindPlanByCode + FindActiveOffer — 404 if either missing.
//   4. If offer.LavaOfferID is nil → 409 "offer_not_configured" (D-09 placeholder rows).
//   5. 60s idempotency: if a pending invoice for (user, lava_offer_id) exists
//      within the last 60s, return THAT instead of creating a duplicate.
//   6. Call lava.CreateInvoice; INSERT invoices row; return paymentUrl.
func CreateCheckoutSession(logger *zap.Logger, cfg *config.Config, db *gorm.DB, lavaClient *lava.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID := c.Locals("user_id").(string)

		var req checkoutRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
		}
		if req.PlanCode == "" || req.Periodicity == "" || req.Currency == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "plan_code, periodicity, currency required"})
		}
		if _, ok := allowedCurrencies[req.Currency]; !ok {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "currency must be USD|EUR|RUB"})
		}
		if _, ok := allowedPeriodicities[req.Periodicity]; !ok {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid periodicity"})
		}

		// Load user — must have email (SSO-identified, not guest).
		user, err := repository.FindUserByID(db, userID)
		if err != nil {
			logger.Error("checkout: load user", zap.String("user_id", userID), zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		if user.Email == nil || *user.Email == "" {
			// Guest user — must SSO first.
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "sign in with Apple or Google before purchasing",
			})
		}

		// Lookup plan + active offer.
		plan, err := repository.FindPlanByCode(db, req.PlanCode)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "plan not found"})
			}
			logger.Error("checkout: FindPlanByCode", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		if !plan.IsActive {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "plan not active"})
		}
		offer, err := repository.FindActiveOffer(db, plan.ID, req.Periodicity, req.Currency)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "no active offer for plan/periodicity/currency"})
			}
			logger.Error("checkout: FindActiveOffer", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		if offer.LavaOfferID == nil || *offer.LavaOfferID == "" {
			// D-09: placeholder offer — admin hasn't selected the lava offer yet.
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": "offer_not_configured"})
		}

		// 60s idempotency reuse (ADR §9.2).
		if existing, ierr := repository.FindActivePendingInvoice(db, userID, *offer.LavaOfferID, 60*time.Second); ierr == nil {
			logger.Info("checkout: reusing pending invoice within 60s window",
				zap.String("user_id", userID), zap.String("invoice_id", existing.ID))
			return c.Status(fiber.StatusOK).JSON(fiber.Map{
				"data": checkoutResponse{
					InvoiceID:     existing.ID,
					LavaInvoiceID: existing.LavaInvoiceID,
					PaymentURL:    existing.PaymentURL,
					Amount:        existing.Amount,
					Currency:      existing.Currency,
				},
			})
		} else if !errors.Is(ierr, repository.ErrNotFound) {
			logger.Error("checkout: FindActivePendingInvoice", zap.Error(ierr))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}

		// Call lava.
		lavaResp, err := lavaClient.CreateInvoice(c.Context(), lava.CreateInvoiceRequest{
			Email:       *user.Email,
			OfferID:     *offer.LavaOfferID,
			Currency:    req.Currency,
			Periodicity: req.Periodicity,
			ClientUtm:   req.ClientUtm,
		})
		if err != nil {
			logger.Error("checkout: lava CreateInvoice failed",
				zap.String("user_id", userID), zap.String("lava_offer_id", *offer.LavaOfferID), zap.Error(err))
			return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "payment provider unavailable"})
		}

		// Persist locally.
		paymentURL := ""
		if lavaResp.PaymentURL != nil {
			paymentURL = *lavaResp.PaymentURL
		}
		offerID := offer.ID
		planID := plan.ID
		inv := &model.Invoice{
			UserID:        userID,
			LavaInvoiceID: lavaResp.ID,
			OfferID:       *offer.LavaOfferID,
			PlanID:        &planID,
			PlanOfferID:   &offerID,
			Plan:          plan.Code,
			Periodicity:   req.Periodicity,
			Currency:      req.Currency,
			Amount:        lavaResp.AmountTotal.Amount,
			Status:        "pending",
			PaymentURL:    paymentURL,
		}
		if err := repository.CreateInvoice(db, inv); err != nil {
			logger.Error("checkout: CreateInvoice db insert failed", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}

		logger.Info("checkout: invoice created",
			zap.String("user_id", userID),
			zap.String("plan_code", plan.Code),
			zap.String("periodicity", req.Periodicity),
			zap.String("currency", req.Currency),
			zap.String("lava_invoice_id", lavaResp.ID),
		)

		return c.Status(fiber.StatusCreated).JSON(fiber.Map{
			"data": checkoutResponse{
				InvoiceID:     inv.ID,
				LavaInvoiceID: lavaResp.ID,
				PaymentURL:    paymentURL,
				Amount:        lavaResp.AmountTotal.Amount,
				Currency:      req.Currency,
			},
		})
	}
}

// CancelSubscription handles POST /api/v1/subscription/cancel (PAY-10).
//
// Flow (ADR §10.6 + D-19 subscription.cancelled):
//   1. Find the user's most recent active LavaContract.
//   2. Call lava.CancelSubscription (DELETE /api/v1/subscriptions?contractId=X&email=Y).
//   3. Locally mark lava_contracts.cancelled_at=now(), is_active=false.
//   4. Do NOT downgrade tier — user keeps Pro until expires_at (cron downgrades in 03-09).
//   5. Return {cancelled: true, access_until: <lava_contracts.expires_at>}.
func CancelSubscription(logger *zap.Logger, cfg *config.Config, db *gorm.DB, lavaClient *lava.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID := c.Locals("user_id").(string)

		user, err := repository.FindUserByID(db, userID)
		if err != nil {
			logger.Error("cancel: load user", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		if user.Email == nil || *user.Email == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "user has no email"})
		}

		// Find the most recent active lava_contracts row for this user.
		var contract model.LavaContract
		findErr := db.Where("user_id = ? AND is_active = ?", userID, true).Order("started_at DESC").First(&contract).Error
		if findErr != nil {
			if errors.Is(findErr, gorm.ErrRecordNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "no active subscription"})
			}
			logger.Error("cancel: find active contract", zap.Error(findErr))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}

		// Call lava.
		if err := lavaClient.CancelSubscription(c.Context(), contract.ContractID, *user.Email); err != nil {
			logger.Error("cancel: lava CancelSubscription failed",
				zap.String("contract_id", contract.ContractID), zap.Error(err))
			return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "payment provider unavailable"})
		}

		// Mark local contract cancelled (tier NOT downgraded — expiry cron handles that).
		now := time.Now()
		if err := db.Model(&model.LavaContract{}).Where("id = ?", contract.ID).Updates(map[string]interface{}{
			"is_active":    false,
			"cancelled_at": &now,
		}).Error; err != nil {
			logger.Error("cancel: update local contract failed", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}

		logger.Info("subscription cancelled",
			zap.String("user_id", userID),
			zap.String("contract_id", contract.ContractID),
		)

		return c.JSON(fiber.Map{
			"data": fiber.Map{
				"cancelled":    true,
				"access_until": contract.ExpiresAt,
			},
		})
	}
}

// GetInvoice handles GET /api/v1/invoices/:id (PAY-09, D-25).
//
// Default: pure DB read.
//
// When ?escalate=true AND the local DB still shows status=pending, proxy to
// lava's GET /api/v2/invoices/{lava_invoice_id} for a one-shot reconciliation:
// if lava reports COMPLETED, we update local status to "paid" and ALSO update
// invoices.status BUT DO NOT trigger SetUserPlan from this endpoint — the
// webhook handler is the authoritative tier-grant path (per D-32 §2).
// The escalate path solely flips the local invoice status so the /pay/success
// page UX is snappy when the webhook is delayed.
func GetInvoice(logger *zap.Logger, cfg *config.Config, db *gorm.DB, lavaClient *lava.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID := c.Locals("user_id").(string)
		invoiceID := c.Params("id")
		escalate := c.Query("escalate") == "true"

		inv, err := repository.FindInvoiceByID(db, invoiceID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "invoice not found"})
			}
			logger.Error("invoice: load failed", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		// Ownership check — must own the invoice (D-32 §2).
		if inv.UserID != userID {
			// Same response as not-found to avoid existence leak.
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "invoice not found"})
		}

		// Escalate path — only when status is still pending.
		if escalate && inv.Status == "pending" {
			lavaInv, err := lavaClient.GetInvoice(c.Context(), inv.LavaInvoiceID)
			if err != nil {
				logger.Warn("invoice: lava GetInvoice failed during escalate (returning local data)",
					zap.String("lava_invoice_id", inv.LavaInvoiceID), zap.Error(err))
				// Non-fatal — fall through to return local pending status.
			} else if lavaInv != nil {
				localStatus := mapLavaStatusToLocal(lavaInv.Status)
				if localStatus != inv.Status && localStatus != "" {
					if uerr := repository.UpdateInvoiceStatus(db, inv.ID, localStatus); uerr != nil {
						logger.Error("invoice: UpdateInvoiceStatus failed", zap.Error(uerr))
						// Non-fatal — return what we have.
					} else {
						inv.Status = localStatus
					}
				}
			}
		}

		return c.JSON(fiber.Map{
			"data": fiber.Map{
				"id":              inv.ID,
				"lava_invoice_id": inv.LavaInvoiceID,
				"status":          inv.Status,
				"amount":          inv.Amount,
				"currency":        inv.Currency,
				"plan":            inv.Plan,
				"periodicity":     inv.Periodicity,
				"created_at":      inv.CreatedAt,
			},
		})
	}
}

// mapLavaStatusToLocal maps lava's invoice status enum (uppercase, NEW |
// IN_PROGRESS | COMPLETED | FAILED) to the local enum (pending | paid |
// failed | cancelled). The mapping for invoice-detail responses differs
// from the create-invoice response casing (RESEARCH §1.1 vs §1.2) —
// handle both.
func mapLavaStatusToLocal(lavaStatus string) string {
	switch lavaStatus {
	case "COMPLETED", "completed":
		return "paid"
	case "FAILED", "failed":
		return "failed"
	case "CANCELLED", "cancelled", "subscription-cancelled":
		return "cancelled"
	case "NEW", "IN_PROGRESS", "in-progress", "new":
		return "pending"
	default:
		return ""
	}
}

// Ensure the package compiles when imported by tests that previously
// referenced Stripe helpers. The compile-time fmt usage prevents an
// "imported and not used" error if no other reference remains.
var _ = fmt.Sprintf
```

    Notes for the executor:
    - The OLD `payment.go` had functions `planToPriceID`, `handleCheckoutCompleted`, `handleSubscriptionDeleted`, `handlePaymentFailed`, `HandleStripeWebhook`. ALL DELETED.
    - `payment_test.go` references to the Stripe-only test paths were t.Skip-ed in plan 03-01 T05. They'll start failing again if they try to call DELETED handlers (`HandleStripeWebhook`, `handleCheckoutCompleted`); these specific tests must be DELETED in T02 of THIS plan.
    - `stripe-go` imports MUST be removed from payment.go — they no longer compile. The dependency stays in go.mod through Phase 3 per D-03.

    Run `cd server/api && go build ./internal/handler/...`. Expect failures only in payment_test.go (handled in T02).
  </action>
  <acceptance_criteria>
    - `grep -c "stripe" server/api/internal/handler/payment.go` returns 0 (lowercase grep — no stripe references anywhere in the file)
    - `grep "HandleStripeWebhook\|handleCheckoutCompleted\|handleSubscriptionDeleted\|planToPriceID" server/api/internal/handler/payment.go` returns 0 hits
    - `grep "lavaClient.CreateInvoice" server/api/internal/handler/payment.go` finds one match
    - `grep "lavaClient.CancelSubscription" server/api/internal/handler/payment.go` finds one match
    - `grep "lavaClient.GetInvoice" server/api/internal/handler/payment.go` finds one match
    - `grep "offer_not_configured" server/api/internal/handler/payment.go` finds one match (D-09)
    - `grep "60\\*time.Second\\|60 \\* time.Second" server/api/internal/handler/payment.go` finds one match (60s idempotency window)
    - `grep "func mapLavaStatusToLocal" server/api/internal/handler/payment.go` finds one match
    - `grep "access_until" server/api/internal/handler/payment.go` finds one match (cancel response)
    - `grep "?escalate=true\\|c.Query(\"escalate\")" server/api/internal/handler/payment.go` finds matches
    - `cd server/api && go build ./internal/handler/payment.go` exits 0 (the file itself compiles; payment_test.go is broken — handled in T02)
  </acceptance_criteria>
  <automated>cd server/api && go build ./internal/handler/payment.go</automated>
  <done>payment.go is 100% lava — no Stripe imports, no Stripe handlers. PAY-02 (CreateCheckoutSession), PAY-09 (GetInvoice + escalate), PAY-10 (CancelSubscription) all present.</done>
</task>

<task type="auto">
  <id>03-05-T02</id>
  <name>Rewrite payment_test.go (delete Stripe tests; add lava handler tests with httptest-mocked lava client and sqlite DB)</name>
  <files>server/api/internal/handler/payment_test.go</files>
  <read_first>
    - server/api/internal/handler/payment_test.go (CURRENT — Stripe-only tests, mostly t.Skip-stubbed from plan 03-01 T05)
    - server/api/internal/handler/payment.go (T01 of THIS plan — handlers to test)
    - server/api/internal/lava/invoice_test.go (T03 of plan 03-02 — httptest pattern to follow)
    - server/api/internal/handler/servers_test.go (sibling — sqlite setup helper pattern)
    - .planning/phases/03-lava-top-plans-catalog/03-VALIDATION.md PAY-02 row (`TestCreateCheckoutSession`), PAY-10 row (`TestCancelSubscription_KeepsProUntilExpiry`)
  </read_first>
  <action>
    REWRITE `server/api/internal/handler/payment_test.go` entirely. Delete every Stripe-only test (`TestCancelSubscription_NoStripeID`, `TestHandleSubscriptionDeleted_UnknownStripeID`, `TestHandleStripeWebhook_*`, etc.). Replace with lava-bound tests using httptest to mock lava + sqlite for the DB.

    Required tests (names map to 03-VALIDATION.md):
    - `TestCreateCheckoutSession_HappyPath` — POST /checkout with valid plan/periodicity/currency → 201 + payment_url
    - `TestCreateCheckoutSession_409_OfferNotConfigured` — lava_offer_id NULL → 409 with body `{"error":"offer_not_configured"}` (D-09)
    - `TestCreateCheckoutSession_60sIdempotencyReuse` — second call within 60s returns the SAME invoice_id without hitting lava (ADR §9.2)
    - `TestCreateCheckoutSession_GuestRejected` — user with email=NULL gets 403
    - `TestCreateCheckoutSession_InvalidCurrency` — currency="XXX" → 400
    - `TestCancelSubscription_KeepsProUntilExpiry` — POST /subscription/cancel marks lava_contracts.cancelled_at + is_active=false BUT does NOT touch users.subscription_tier (PAY-10 named test in 03-VALIDATION.md)
    - `TestGetInvoice_DBOnly` — GET /invoices/:id without escalate returns local status
    - `TestGetInvoice_EscalateUpdatesPendingToPaid` — escalate=true + lava returns COMPLETED → local status flips to "paid" (does NOT call SetUserPlan from this path — webhook authoritative)
    - `TestGetInvoice_OwnershipCheck_Returns404OnMismatch` — request invoice owned by user B from user A's JWT → 404 (D-32 §2)

    File body skeleton (executor fills in the details):

```go
package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"vpnapp/server/api/internal/config"
	"vpnapp/server/api/internal/lava"
	"vpnapp/server/api/internal/model"
	"vpnapp/server/api/internal/repository"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupPaymentTestDB creates the minimum schema for payment handler tests.
// Includes users + plans + plan_offers + invoices + lava_contracts.
func setupPaymentTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	stmts := []string{
		// users — must include plan_id + email columns
		`CREATE TABLE users (id TEXT PRIMARY KEY, email TEXT, email_verified INTEGER DEFAULT 0,
			email_is_private_relay INTEGER DEFAULT 0, full_name TEXT NOT NULL DEFAULT '',
			subscription_tier TEXT NOT NULL DEFAULT 'free', subscription_expires_at TIMESTAMP,
			role TEXT NOT NULL DEFAULT 'user', auth_provider TEXT NOT NULL DEFAULT 'guest',
			apple_user_id TEXT, google_user_id TEXT, email_hash TEXT, password_hash TEXT,
			telegram_user_id INTEGER, telegram_linked_at TIMESTAMP, telegram_username TEXT, telegram_first_name TEXT,
			plan_id TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE plans (id TEXT PRIMARY KEY, code TEXT NOT NULL UNIQUE, name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '', max_devices INTEGER NOT NULL, max_servers INTEGER NOT NULL,
			speed_limit_mbps INTEGER NOT NULL DEFAULT 0, is_active INTEGER NOT NULL DEFAULT 1,
			is_system INTEGER NOT NULL DEFAULT 0, sort_order INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP, updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE plan_offers (id TEXT PRIMARY KEY, plan_id TEXT NOT NULL, periodicity TEXT NOT NULL,
			currency TEXT NOT NULL, amount REAL NOT NULL, lava_offer_id TEXT, is_active INTEGER NOT NULL DEFAULT 1,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP, updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE invoices (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, lava_invoice_id TEXT NOT NULL UNIQUE,
			offer_id TEXT NOT NULL, plan_id TEXT, plan_offer_id TEXT, plan TEXT NOT NULL,
			periodicity TEXT NOT NULL, currency TEXT NOT NULL, amount REAL NOT NULL, status TEXT NOT NULL,
			payment_url TEXT, created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE lava_contracts (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, contract_id TEXT NOT NULL UNIQUE,
			parent_contract_id TEXT, offer_id TEXT NOT NULL, plan TEXT NOT NULL, periodicity TEXT NOT NULL,
			currency TEXT NOT NULL, is_active INTEGER NOT NULL DEFAULT 1,
			started_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP, expires_at TIMESTAMP,
			cancelled_at TIMESTAMP, created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE subscriptions (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, plan TEXT NOT NULL DEFAULT 'free',
			lava_contract_id TEXT, is_active INTEGER NOT NULL DEFAULT 1,
			started_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP, expires_at TIMESTAMP)`,
	}
	for _, s := range stmts {
		if err := db.Exec(s).Error; err != nil {
			t.Fatalf("DDL: %v", err)
		}
	}
	return db
}

// mkApp wires the three handlers + the lava client (httptest-mocked) onto a
// fresh Fiber app. The single test_userID + plan/offer are seeded.
func mkPaymentApp(t *testing.T, db *gorm.DB, lavaClient *lava.Client, userID string) *fiber.App {
	logger := zap.NewNop()
	cfg := &config.Config{}
	app := fiber.New()
	// Inject c.Locals("user_id") via a thin middleware so tests don't need full JWT auth.
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user_id", userID)
		return c.Next()
	})
	app.Post("/api/v1/checkout", CreateCheckoutSession(logger, cfg, db, lavaClient))
	app.Post("/api/v1/subscription/cancel", CancelSubscription(logger, cfg, db, lavaClient))
	app.Get("/api/v1/invoices/:id", GetInvoice(logger, cfg, db, lavaClient))
	return app
}

// seedUserAndPlan inserts a free + pro plan, a SSO user with the given email,
// and an active Pro MONTHLY/USD offer with the supplied lava_offer_id (nil
// for the placeholder 409 test).
func seedUserAndPlan(t *testing.T, db *gorm.DB, email string, lavaOfferID *string) (userID, planID, offerID string) {
	t.Helper()
	freeID := uuid.NewString()
	proID := uuid.NewString()
	for _, p := range []model.Plan{
		{ID: freeID, Code: "free", Name: "Free", MaxDevices: 1, MaxServers: 3, IsActive: true, IsSystem: true, SortOrder: 0},
		{ID: proID, Code: "pro", Name: "Pro", MaxDevices: 3, MaxServers: -1, IsActive: true, IsSystem: false, SortOrder: 10},
	} {
		if err := db.Create(&p).Error; err != nil {
			t.Fatalf("seed plan: %v", err)
		}
	}
	offerID = uuid.NewString()
	if err := db.Create(&model.PlanOffer{
		ID: offerID, PlanID: proID, Periodicity: "MONTHLY", Currency: "USD",
		Amount: 5.0, LavaOfferID: lavaOfferID, IsActive: true,
	}).Error; err != nil {
		t.Fatalf("seed offer: %v", err)
	}
	userID = uuid.NewString()
	if err := db.Create(&model.User{
		ID: userID, Email: &email, EmailVerified: true, FullName: "u",
		SubscriptionTier: "free", PlanID: freeID, AuthProvider: "google",
	}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return userID, proID, offerID
}

// --- Tests follow ---

func TestCreateCheckoutSession_HappyPath(t *testing.T) {
	db := setupPaymentTestDB(t)
	lavaOID := "lava-off-1"
	userID, _, _ := seedUserAndPlan(t, db, "alice@example.com", &lavaOID)

	lavaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/invoice" {
			t.Errorf("expected /api/v3/invoice, got %s", r.URL.Path)
		}
		w.WriteHeader(200)
		pu := "https://app.lava.top/pay/abc"
		_ = json.NewEncoder(w).Encode(lava.InvoiceResponse{
			ID:          "lava-inv-1",
			Status:      "in-progress",
			AmountTotal: lava.InvoiceAmount{Amount: 5.0, Currency: "USD"},
			PaymentURL:  &pu,
		})
	}))
	defer lavaServer.Close()
	// Use the package-private constructor (same package).
	client := newLavaTestClient(t, lavaServer.URL)

	app := mkPaymentApp(t, db, client, userID)
	body := strings.NewReader(`{"plan_code":"pro","periodicity":"MONTHLY","currency":"USD"}`)
	req := httptest.NewRequest("POST", "/api/v1/checkout", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil || resp.StatusCode != 201 {
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(resp.Body)
		t.Fatalf("expected 201, got %d body=%s err=%v", resp.StatusCode, buf.String(), err)
	}
}

// TODO executor: implement remaining 8 tests following the same pattern.
// Names called out in this plan's <action> block. Use t.Run subtests if
// preferred. Each test should:
//   1. seed user + plan + offer (or override one input)
//   2. mock lava with httptest
//   3. Construct app via mkPaymentApp
//   4. Assert status code + JSON body shape via json.NewDecoder
```

    **CRITICAL:** the executor MUST implement all 9 tests named in the action block. The skeleton above shows the pattern (httptest mock + sqlite DB + mkPaymentApp helper). For `newLavaTestClient`, add this small helper inside the test file:

```go
// newLavaTestClient calls the package-private newWithBaseURL via a small bridge.
// Since payment_test.go and lava package are different packages, we cannot
// directly invoke lava.newWithBaseURL. Instead, expose a test-only helper IN
// THE lava PACKAGE — RESEARCH §12.5 documents this; add to internal/lava/client_test.go
// (or a new exported helper in internal/lava/test_helpers.go) called
// NewWithBaseURLForTest. Alternative: do NOT mock at the lava-client layer;
// instead use a stub Client struct with the same methods (but Client is opaque).
// The simpler approach: add an unexported helper in internal/lava/dto.go or
// elsewhere that constructs the client by calling newWithBaseURL — this stays
// in-package.
//
// IMPLEMENTER: add `func NewForTest(apiKey, baseURL string) *Client {
//     return newWithBaseURL(apiKey, baseURL) }` to internal/lava/client.go,
// reachable from external test files. Update plan 03-02's <acceptance> if needed.
//
// For THIS plan, assume the helper exists; if not, add it as a one-line
// follow-up to client.go.
func newLavaTestClient(t *testing.T, baseURL string) *lava.Client {
	return lava.NewForTest("test-key", baseURL)
}
```

    **Two-line modification** to plan 03-02's `internal/lava/client.go`: add at the bottom (before the closing `}` of the package):
```go
// NewForTest constructs a Client pointed at a custom base URL. ONLY for use
// by other-package tests that need to mock lava via httptest. The production
// codebase MUST call New() — verified by the SSRF audit grep in plan 03-11.
func NewForTest(apiKey, baseURL string) *Client {
	return newWithBaseURL(apiKey, baseURL)
}
```
    This is a 4-line addition to T02 of plan 03-02 — the executor of THIS plan (03-05) makes this addition INLINE while writing payment_test.go.

    Run `cd server/api && go test ./internal/handler/ -run "TestCreateCheckoutSession|TestCancelSubscription_KeepsProUntilExpiry|TestGetInvoice" -count=1 -timeout=60s -v`.
  </action>
  <acceptance_criteria>
    - `grep -c "TestCancelSubscription_NoStripeID\|TestHandleSubscriptionDeleted_UnknownStripeID\|TestHandleStripeWebhook" server/api/internal/handler/payment_test.go` returns 0 (Stripe tests deleted)
    - `grep "TestCreateCheckoutSession_HappyPath" server/api/internal/handler/payment_test.go` finds one match
    - `grep "TestCreateCheckoutSession_409_OfferNotConfigured" server/api/internal/handler/payment_test.go` finds one match (PAY-02 + D-09)
    - `grep "TestCreateCheckoutSession_60sIdempotencyReuse" server/api/internal/handler/payment_test.go` finds one match (ADR §9.2)
    - `grep "TestCancelSubscription_KeepsProUntilExpiry" server/api/internal/handler/payment_test.go` finds one match (PAY-10 in 03-VALIDATION.md)
    - `grep "TestGetInvoice_OwnershipCheck_Returns404OnMismatch" server/api/internal/handler/payment_test.go` finds one match (D-32 §2)
    - `grep "NewForTest" server/api/internal/lava/client.go` finds one match (the 4-line helper added by this plan)
    - `cd server/api && go test ./internal/handler/ -run "TestCreateCheckoutSession|TestCancelSubscription|TestGetInvoice" -count=1 -timeout=60s` exits 0
  </acceptance_criteria>
  <automated>cd server/api && go test ./internal/handler/ -run "TestCreateCheckoutSession|TestCancelSubscription_KeepsProUntilExpiry|TestGetInvoice" -count=1 -timeout=60s</automated>
  <done>payment_test.go has 9 lava-bound tests; PAY-02 + PAY-09 + PAY-10 + D-09 + 60s idempotency + ownership-check all verified.</done>
</task>

<task type="auto">
  <id>03-05-T03</id>
  <name>Add admin_lava.go (GET /admin/lava/products proxy — D-12 Option B)</name>
  <files>
    server/api/internal/handler/admin_lava.go,
    server/api/internal/handler/admin_lava_test.go
  </files>
  <read_first>
    - server/api/internal/lava/products.go (T02 of plan 03-02 — ListProducts signature)
    - server/api/internal/lava/dto.go (T02 of plan 03-02 — ProductItemResponse + ProductOffer shapes)
    - .planning/phases/03-lava-top-plans-catalog/03-CONTEXT.md D-12 (Option B — synced dropdown), D-13 (admin UI in scope this phase)
    - .planning/phases/03-lava-top-plans-catalog/03-RESEARCH.md §1.3 (proxy response normalization shape — flat array of productId/productName/offerId/offerName/periodicity/currency/amount)
  </read_first>
  <action>
    Two new files.

    **(a) `server/api/internal/handler/admin_lava.go`:**

```go
package handler

import (
	"vpnapp/server/api/internal/lava"

	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
)

// lavaProductRow is the flattened admin-dropdown source row (D-12 Option B).
// One row per (product, offer, price) tuple — the admin UI presents these as
// a dropdown so admin selects WITHOUT typing or pasting a UUID.
type lavaProductRow struct {
	ProductID   string  `json:"productId"`
	ProductName string  `json:"productName"`
	OfferID     string  `json:"offerId"`
	OfferName   string  `json:"offerName"`
	Periodicity string  `json:"periodicity"`
	Currency    string  `json:"currency"`
	Amount      float64 `json:"amount"`
}

// AdminListLavaProducts handles GET /api/v1/admin/lava/products (D-12).
//
// Proxies GET /api/v2/products via the server-side lava client (admin
// API key NEVER reaches the browser) and flattens the response into a
// dropdown-friendly array. Admin UI (plan 03-10) calls this on plan-offer
// dialog mount.
//
// Mounted on the admin route group in cmd/main.go — inherits AuthRequired
// + AdminRequired + AuditLog middleware automatically.
func AdminListLavaProducts(logger *zap.Logger, lavaClient *lava.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		products, err := lavaClient.ListProducts(c.Context())
		if err != nil {
			logger.Error("admin: ListLavaProducts failed", zap.Error(err))
			return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{
				"error": "payment provider unavailable",
			})
		}

		// Flatten products × offers × prices into a single dropdown source.
		rows := make([]lavaProductRow, 0, len(products)*2)
		for _, p := range products {
			productName := ""
			if p.Title != nil {
				productName = *p.Title
			}
			for _, offer := range p.Offers {
				for _, price := range offer.Prices {
					rows = append(rows, lavaProductRow{
						ProductID:   p.ID,
						ProductName: productName,
						OfferID:     offer.ID,
						OfferName:   offer.Name,
						Periodicity: price.Periodicity,
						Currency:    price.Currency,
						Amount:      price.Amount,
					})
				}
			}
		}

		return c.JSON(fiber.Map{
			"data": rows,
		})
	}
}
```

    **(b) `server/api/internal/handler/admin_lava_test.go`:** httptest-mocks `/api/v2/products` returning two products with multiple offers and asserts the flattened response.

```go
package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"vpnapp/server/api/internal/lava"

	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
)

func TestAdminListLavaProducts_FlattensProductOfferPrice(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/products" {
			t.Errorf("expected /api/v2/products, got %s", r.URL.Path)
		}
		w.WriteHeader(200)
		proName := "Pro"
		_ = json.NewEncoder(w).Encode(lava.ProductsResponse{
			Items: []lava.ProductsItem{
				{Type: "PRODUCT", Data: lava.ProductItemResponse{
					ID: "prod-1", Title: &proName, Type: "SUBSCRIPTION",
					Offers: []lava.ProductOffer{
						{ID: "off-month-usd", Name: "Monthly", Prices: []lava.ProductOfferPrice{
							{Amount: 5.00, Currency: "USD", Periodicity: "MONTHLY"},
							{Amount: 499.0, Currency: "RUB", Periodicity: "MONTHLY"},
						}},
						{ID: "off-year-usd", Name: "Yearly", Prices: []lava.ProductOfferPrice{
							{Amount: 49.99, Currency: "USD", Periodicity: "PERIOD_YEAR"},
						}},
					},
				}},
			},
		})
	}))
	defer srv.Close()
	client := lava.NewForTest("test-key", srv.URL)

	app := fiber.New()
	app.Get("/api/v1/admin/lava/products", AdminListLavaProducts(zap.NewNop(), client))
	resp, err := app.Test(httptest.NewRequest("GET", "/api/v1/admin/lava/products", nil))
	if err != nil || resp.StatusCode != 200 {
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(resp.Body)
		t.Fatalf("expected 200, got %d body=%s err=%v", resp.StatusCode, buf.String(), err)
	}
	var body struct {
		Data []lavaProductRow `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Expect 3 rows: month-USD, month-RUB, year-USD.
	if len(body.Data) != 3 {
		t.Fatalf("expected 3 flattened rows, got %d: %+v", len(body.Data), body.Data)
	}
	// Check first row shape.
	if body.Data[0].ProductName != "Pro" || body.Data[0].OfferID != "off-month-usd" || body.Data[0].Periodicity != "MONTHLY" {
		t.Errorf("unexpected first row: %+v", body.Data[0])
	}
}

func TestAdminListLavaProducts_LavaError_Returns502(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte(`{"message":"upstream error"}`))
	}))
	defer srv.Close()
	client := lava.NewForTest("test-key", srv.URL)

	app := fiber.New()
	app.Get("/api/v1/admin/lava/products", AdminListLavaProducts(zap.NewNop(), client))
	resp, err := app.Test(httptest.NewRequest("GET", "/api/v1/admin/lava/products", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 502 {
		t.Errorf("expected 502 on lava error, got %d", resp.StatusCode)
	}
}
```

    Run `cd server/api && go test ./internal/handler/ -run "TestAdminListLavaProducts" -count=1 -timeout=30s -v`.
  </action>
  <acceptance_criteria>
    - Files `server/api/internal/handler/admin_lava.go` and `admin_lava_test.go` exist
    - `grep "AdminListLavaProducts" server/api/internal/handler/admin_lava.go` finds one match
    - `grep "lavaProductRow" server/api/internal/handler/admin_lava.go` finds matches (struct + field tags)
    - `grep "lavaClient.ListProducts" server/api/internal/handler/admin_lava.go` finds one match
    - `grep "TestAdminListLavaProducts_FlattensProductOfferPrice" server/api/internal/handler/admin_lava_test.go` finds one match
    - `cd server/api && go test ./internal/handler/ -run "TestAdminListLavaProducts" -count=1 -timeout=30s` exits 0
  </acceptance_criteria>
  <automated>cd server/api && go test ./internal/handler/ -run "TestAdminListLavaProducts" -count=1 -timeout=30s</automated>
  <done>admin_lava.go proxies GET /api/v2/products with server-side key, flattens to dropdown source; admin_lava_test.go verifies happy path + 502 on lava error.</done>
</task>

<task type="auto">
  <id>03-05-T04</id>
  <name>Wire all new routes in cmd/main.go (construct lava.Client; remove Stripe routes; add /checkout + /subscription/cancel + /invoices/:id + /admin/lava/products)</name>
  <files>server/api/cmd/main.go</files>
  <read_first>
    - server/api/cmd/main.go (CURRENT — see lines 76-77 stripe.Key + line 195 /webhook/stripe + line 237 /subscription/checkout + line 256-281 admin route group)
    - server/api/internal/lava/client.go (T02 of plan 03-02 — lava.New(apiKey))
    - server/api/internal/handler/payment.go (T01 of THIS plan — new handler signatures)
    - server/api/internal/handler/admin_lava.go (T03 of THIS plan)
    - .planning/phases/03-lava-top-plans-catalog/03-CONTEXT.md D-02 (remove Stripe routes; add lava routes)
    - .planning/phases/03-lava-top-plans-catalog/03-RESEARCH.md §5.2 (route mount locations)
  </read_first>
  <action>
    Five edits to `server/api/cmd/main.go`. **DO NOT remove the `stripe-go` import yet** — payment.go has stripped it but the import was at the cmd level (`stripe.Key = cfg.StripeKey` at line 76). After T01 deletes the stripe references in payment.go, we can also remove the cmd-level stripe.Key set.

    **(a) Remove the `stripe` import** (line 25) and the `stripe.Key = cfg.StripeKey` line (line 76-77):

    Find:
    ```go
    stripe "github.com/stripe/stripe-go/v81"
    ```
    DELETE that import line. Then delete:
    ```go
    // Set Stripe API key once at startup — handlers must not override this.
    stripe.Key = cfg.StripeKey
    ```
    (lines 76-77 — the comment + the assignment).

    **(b) After the `googleVerifier := google.New(...)` block (around line 91-95), construct the lava client:**

```go
	// Phase 3 lava.top client (D-14). Constructed once at startup with the
	// active API key (selected by LAVA_ENV — config.go resolves into LavaActiveAPIKey).
	// SSRF mitigation: BaseURL is hardcoded in internal/lava/client.go (D-15);
	// API key never logged or echoed in error paths (D-32 §3).
	lavaClient := lava.New(cfg.LavaActiveAPIKey)
	logger.Info("lava client constructed", zap.String("env", cfg.LavaEnv))
```

    Add to the imports at the top:
```go
	"vpnapp/server/api/internal/lava"
```

    **(c) Replace the Stripe webhook line (~line 195):**

    Find:
    ```go
    api.Post("/webhook/stripe", handler.HandleStripeWebhook(logger, cfg, db))
    ```
    DELETE the line entirely. The lava webhook (POST /webhook/lava) lands in plan 03-06 with the IP-allowlist middleware — DO NOT add it here.

    Also DELETE the SkipRule line for the old stripe webhook around line 157:
    ```go
    middleware.SkipRule{Method: fiber.MethodPost, Path: "/api/v1/webhook/stripe"},
    ```
    (this becomes meaningless once the route is gone; the new /webhook/lava line in 03-06 will add a fresh SkipRule).

    **(d) Replace the protected Stripe routes (~line 237-238):**

    Find:
    ```go
    protected.Post("/subscription/checkout", handler.CreateCheckoutSession(logger, cfg, db))
    protected.Post("/subscription/cancel", handler.CancelSubscription(logger, cfg, db))
    ```
    Replace with:
    ```go
	// Phase 3 lava endpoints (D-02). /checkout replaces the old /subscription/checkout.
	// All three are PROTECTED (JWT required) — guest users get 403 from the handler
	// itself (CreateCheckoutSession checks user.Email presence).
	protected.Post("/checkout", handler.CreateCheckoutSession(logger, cfg, db, lavaClient))
	protected.Post("/subscription/cancel", handler.CancelSubscription(logger, cfg, db, lavaClient))
	protected.Get("/invoices/:id", handler.GetInvoice(logger, cfg, db, lavaClient))
    ```

    **(e) After the existing admin routes (between `admin.Get("/audit-log", ...)` and `admin.Post("/change-password", ...)` around line 277-281), add the lava-products proxy:**

```go
	// Phase 3 lava admin endpoint (D-12 Option B). Proxies /api/v2/products
	// via server-side API key so admin can pick lava offers from a dropdown.
	// Inherits AuthRequired + AdminRequired + AuditLog from the admin group.
	admin.Get("/lava/products", handler.AdminListLavaProducts(logger, lavaClient))
```

    Run `cd server/api && go build ./...`. Expect no errors. If `OptionalEnvWarnings()` in config.go still references Stripe vars, leave those warnings in place — Stripe vars stay until Phase 8 (D-03).
  </action>
  <acceptance_criteria>
    - `grep -c "github.com/stripe/stripe-go" server/api/cmd/main.go` returns 0
    - `grep -c "stripe.Key" server/api/cmd/main.go` returns 0
    - `grep "lavaClient := lava.New(cfg.LavaActiveAPIKey)" server/api/cmd/main.go` finds one match
    - `grep -c "/webhook/stripe" server/api/cmd/main.go` returns 0 (route + skip rule both gone)
    - `grep "/subscription/checkout" server/api/cmd/main.go` returns 0 (replaced by /checkout)
    - `grep "protected.Post(\"/checkout\"" server/api/cmd/main.go` finds one match
    - `grep "protected.Post(\"/subscription/cancel\"" server/api/cmd/main.go` finds one match
    - `grep "protected.Get(\"/invoices/:id\"" server/api/cmd/main.go` finds one match
    - `grep "admin.Get(\"/lava/products\"" server/api/cmd/main.go` finds one match
    - `cd server/api && go build ./...` exits 0
    - `cd server/api && go vet ./...` exits 0
  </acceptance_criteria>
  <automated>cd server/api && go build ./... && go vet ./...</automated>
  <done>cmd/main.go is Stripe-free at the route layer; lava client constructed once at startup; 4 new routes wired (3 protected + 1 admin); webhook route reserved for 03-06.</done>
</task>

</tasks>

<verification>
- `cd server/api && go build ./...` exits 0
- `cd server/api && go test ./internal/handler/ -count=1 -timeout=180s` exits 0
- `grep -rn "stripe\." server/api/internal/handler/ server/api/cmd/` returns 0 hits (no stripe-go references in handler layer or cmd; stripe-go remains in go.mod through Phase 3 per D-03)
- `grep -rn "HandleStripeWebhook\|handleCheckoutCompleted" server/api/` returns 0 hits (functions deleted)
- All 4 new routes mounted: `/checkout`, `/subscription/cancel`, `/invoices/:id`, `/admin/lava/products`
- Lava client constructed once at startup; `cfg.LavaActiveAPIKey` flows from config to handlers via DI
</verification>

<must_haves>
truths:
  - "POST /api/v1/checkout returns 201 with {invoice_id, lava_invoice_id, payment_url, amount, currency} for a SSO-bound user with a configured offer."
  - "POST /api/v1/checkout returns 409 {\"error\":\"offer_not_configured\"} when the plan_offer.lava_offer_id is NULL (D-09 placeholder)."
  - "POST /api/v1/checkout returns the SAME invoice within a 60-second idempotency window (ADR §9.2 — double-tap protection)."
  - "POST /api/v1/checkout returns 403 for guest users (user.Email==nil) — must SSO first."
  - "POST /api/v1/subscription/cancel calls lava DELETE then marks lava_contracts.cancelled_at + is_active=false; users.subscription_tier is UNCHANGED until expiry cron (PAY-10)."
  - "GET /api/v1/invoices/:id returns 404 when invoice.UserID != caller (D-32 §2 ownership check)."
  - "GET /api/v1/invoices/:id?escalate=true reconciles local status with lava's GET /api/v2/invoices/{id} but DOES NOT call SetUserPlan — webhook is the authoritative tier-grant path (D-32 §2)."
  - "GET /api/v1/admin/lava/products flattens lava's products×offers×prices into a dropdown source; API key never reaches the browser (D-12 Option B)."
  - "cmd/main.go constructs *lava.Client once at startup via lava.New(cfg.LavaActiveAPIKey); Stripe routes and stripe.Key removed."
artifacts:
  - path: "server/api/internal/handler/payment.go"
    provides: "Stripe-free payment handlers (checkout, cancel, get-invoice)"
    contains: "func CreateCheckoutSession"
  - path: "server/api/internal/handler/admin_lava.go"
    provides: "Admin proxy for lava products (D-12 Option B)"
    contains: "AdminListLavaProducts"
  - path: "server/api/cmd/main.go"
    provides: "Phase 3 routes wired"
    contains: "lavaClient := lava.New"
key_links:
  - from: "server/api/internal/handler/payment.go::CreateCheckoutSession"
    to: "server/api/internal/repository/invoice_repo.go::FindActivePendingInvoice"
    via: "60-second idempotency reuse (ADR §9.2)"
    pattern: "FindActivePendingInvoice"
  - from: "server/api/internal/handler/payment.go::CancelSubscription"
    to: "server/api/internal/lava/subscription.go::CancelSubscription"
    via: "DELETE /api/v1/subscriptions?contractId=X&email=Y"
    pattern: "lavaClient.CancelSubscription"
  - from: "server/api/cmd/main.go"
    to: "server/api/internal/handler/admin_lava.go::AdminListLavaProducts"
    via: "admin.Get(\"/lava/products\", ...)"
    pattern: 'admin.Get\("/lava/products"'
</must_haves>

<threat_model>
## Trust Boundaries
| Boundary | Description |
|----------|-------------|
| Client → /checkout, /subscription/cancel, /invoices/:id | Authenticated client supplies plan_code, periodicity, currency. Lava-side offer ID is server-resolved (PAY-08 chain). |
| Admin → /admin/lava/products | Admin JWT + AdminRequired middleware; outbound call uses server-side LAVA_API_KEY. Key never reaches browser. |
| Lava webhook → us (NOT in this plan; covered 03-06) | — |

## STRIDE Threat Register
| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-03-29 | Tampering | Client supplies their own `plan_code` to escalate tier | mitigate | The /checkout handler validates plan_code via FindPlanByCode + FindActiveOffer; the resulting `lava_offer_id` is what's sent to lava. **The webhook handler (03-06) NEVER reads anything tier-bearing from the request body — only contractId is used as a key for the reverse-lookup chain.** So even if a client crafted a plan_code="root" /checkout, FindPlanByCode would return 404. |
| T-03-30 | Tampering | Client double-taps /checkout to double-charge | mitigate | 60-second idempotency reuse via FindActivePendingInvoice (ADR §9.2). Same lava_offer_id + same user + pending status → return existing invoice without creating a new lava invoice. |
| T-03-31 | Information disclosure | /invoices/:id leaks other users' invoices | mitigate | Ownership check: `if inv.UserID != userID` returns 404 (not 403 — defence in depth, same as D-22 server access). |
| T-03-32 | Elevation of Privilege | Guest user buys Pro via /checkout | mitigate | `if user.Email == nil || *user.Email == "" { return 403 }` — guest rows have Email=nil. Must SSO first (Phase 2). |
| T-03-33 | DoS | /admin/lava/products amplifies lava API traffic | accept | Admin-only endpoint behind AdminRequired + global per-IP rate limit. Admin UI calls this once per modal mount; not user-facing. Lava pagination drain is bounded by lava's product catalog size (~10s of items for a single-product project). |
| T-03-34 | Information disclosure | API key leaks via /admin/lava/products error body | mitigate | The handler returns generic "payment provider unavailable" on lava errors. The lava client's `decodeJSON` wraps only the lava `message` field — never echoes the request body or headers. Audit-log records the action (not the response body). |
| T-03-35 | Tampering | /invoices/:id?escalate=true triggers SetUserPlan from a forged lava response | mitigate | The escalate path EXPLICITLY does not call SetUserPlan — it only updates `invoices.status` via UpdateInvoiceStatus. Tier-grant remains webhook-only (D-32 §2 — "lava webhook authoritative over /pay/success polling"). |
| T-03-36 | Repudiation | Cancel called but lava-side subscription not actually cancelled | mitigate | The handler calls lava.CancelSubscription BEFORE marking local cancelled_at — if lava errors, the local row stays is_active=true (preserves "we know about the contract" state). Future renewal webhooks land on a still-active local row; lava is authoritative. |
| T-03-37 | DoS | Slow lava response blocks the API request thread | mitigate | All lava calls inherit c.Context() — when the client disconnects, the request context cancels and the lava HTTP call also cancels (5s timeout inherited from lava client; context-derived timeout overrides). |

ASVS L2 scoping per D-31: this plan is in L2 scope (all 4 new handlers are payment-path). Controls applied: V4 ownership checks (invoice owner), V5 input validation (currency + periodicity enums + plan_code lookup), V8 data protection (API key only flows server-side), V11 business logic (60s idempotency + tier from offerId chain), V13 API contract (404 vs 403 defence in depth).
</threat_model>

<success_criteria>
1. `cd server/api && go build ./...` exits 0.
2. `cd server/api && go test ./internal/handler/ -run "TestCreateCheckoutSession|TestCancelSubscription|TestGetInvoice|TestAdminListLavaProducts" -count=1 -timeout=60s` exits 0.
3. PAY-02 verified via `TestCreateCheckoutSession_HappyPath` + `TestCreateCheckoutSession_409_OfferNotConfigured`.
4. PAY-10 verified via `TestCancelSubscription_KeepsProUntilExpiry` (PAY-10 named test in 03-VALIDATION.md).
5. PAY-09 PARTIAL — `TestGetInvoice_EscalateUpdatesPendingToPaid` covers the escalate path; full PAY-09 (period_end populates on webhook) lives in plan 03-06.
6. PAY-13 PARTIAL — admin proxy in scope; full admin CRUD lives in plan 03-08.
7. `grep -rn 'stripe' server/api/cmd/main.go server/api/internal/handler/payment.go server/api/internal/handler/admin_lava.go` returns 0 hits.
</success_criteria>

<output>
T01..T04 land as 4 atomic commits (`feat(03-05): ...`); planner commits this plan file once with `docs(03): plan checkout-cancel-invoices-admin-lava-proxy`.
</output>
