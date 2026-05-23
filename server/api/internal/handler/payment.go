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
	"ONE_TIME":        {},
	"MONTHLY":         {},
	"PERIOD_90_DAYS":  {},
	"PERIOD_180_DAYS": {},
	"PERIOD_YEAR":     {},
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
//  1. Validate body (plan_code, periodicity, currency).
//  2. Load user — require email present (SSO-bound users only; guest users get 403).
//  3. FindPlanByCode + FindActiveOffer — 404 if either missing.
//  4. If offer.LavaOfferID is nil → 409 "offer_not_configured" (D-09 placeholder rows).
//  5. 60s idempotency: if a pending invoice for (user, lava_offer_id) exists
//     within the last 60s, return THAT instead of creating a duplicate.
//  6. Call lava.CreateInvoice; INSERT invoices row; return paymentUrl.
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
//  1. Find the user's most recent active LavaContract.
//  2. Call lava.CancelSubscription (DELETE /api/v1/subscriptions?contractId=X&email=Y).
//  3. Locally mark lava_contracts.cancelled_at=now(), is_active=false.
//  4. Do NOT downgrade tier — user keeps Pro until expires_at (cron downgrades in 03-09).
//  5. Return {cancelled: true, access_until: <lava_contracts.expires_at>}.
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

		// WR-07: match the most recent contract whose access window is still
		// open (expires_at in the future, OR NULL == never-expires), regardless
		// of is_active. After D-19 BLOCKER #1, recurring.payment.failed flips
		// is_active=false immediately but the user keeps Pro until expires_at
		// lapses — so a user who sees "Pro active" in the app and taps Cancel
		// would previously hit a 404 on a "no active subscription" lookup.
		// The lava-side state may also still be live (lava hasn't yet sent
		// subscription.cancelled), so a DELETE to lava is still needed to
		// prevent the next retry from re-attempting the failed payment.
		var contract model.LavaContract
		findErr := db.Where(
			"user_id = ? AND (expires_at IS NULL OR expires_at > ?)",
			userID, time.Now(),
		).Order("started_at DESC").First(&contract).Error
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
				// WR-04: surface the empty-string guard at Warn so the operator
				// catches lava API changes (new enum values) early. Without
				// this log the user's /pay/success page polls forever and the
				// only signal is the support ticket.
				if localStatus == "" {
					logger.Warn("invoice: lava status not mapped — keeping local status",
						zap.String("lava_status", lavaInv.Status),
						zap.String("local_status", inv.Status),
						zap.String("lava_invoice_id", inv.LavaInvoiceID),
					)
				}
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
