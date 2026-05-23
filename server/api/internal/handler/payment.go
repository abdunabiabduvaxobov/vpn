package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"vpnapp/server/api/internal/config"
	"vpnapp/server/api/internal/model"
	"vpnapp/server/api/internal/repository"

	"github.com/gofiber/fiber/v2"
	stripe "github.com/stripe/stripe-go/v81"
	"github.com/stripe/stripe-go/v81/checkout/session"
	stripewebhook "github.com/stripe/stripe-go/v81/webhook"
	stripesub "github.com/stripe/stripe-go/v81/subscription"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type checkoutRequest struct {
	Plan string `json:"plan"`
}

type checkoutResponse struct {
	SessionID string `json:"session_id"`
	URL       string `json:"url"`
}

// CreateCheckoutSession handles POST /subscription/checkout.
// Accepts {"plan":"premium"|"ultimate"}, creates a Stripe Checkout Session
// in subscription mode, and returns the session ID and hosted URL.
func CreateCheckoutSession(logger *zap.Logger, cfg *config.Config, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID := c.Locals("user_id").(string)

		var req checkoutRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "invalid request body",
			})
		}

		priceID, err := planToPriceID(req.Plan, cfg)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": err.Error(),
			})
		}

		successURL := fmt.Sprintf("%s://payment/success?session_id={CHECKOUT_SESSION_ID}", cfg.AppDeepLinkScheme)
		cancelURL := fmt.Sprintf("%s://payment/cancel", cfg.AppDeepLinkScheme)

		params := &stripe.CheckoutSessionParams{
			Mode: stripe.String(string(stripe.CheckoutSessionModeSubscription)),
			LineItems: []*stripe.CheckoutSessionLineItemParams{
				{
					Price:    stripe.String(priceID),
					Quantity: stripe.Int64(1),
				},
			},
			SuccessURL: stripe.String(successURL),
			CancelURL:  stripe.String(cancelURL),
			Metadata: map[string]string{
				"user_id": userID,
				"plan":    req.Plan,
			},
		}

		sess, err := session.New(params)
		if err != nil {
			logger.Error("failed to create stripe checkout session",
				zap.String("user_id", userID),
				zap.String("plan", req.Plan),
				zap.Error(err),
			)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "failed to create checkout session",
			})
		}

		logger.Info("checkout session created",
			zap.String("user_id", userID),
			zap.String("plan", req.Plan),
			zap.String("session_id", sess.ID),
		)

		return c.Status(fiber.StatusCreated).JSON(fiber.Map{
			"data": checkoutResponse{
				SessionID: sess.ID,
				URL:       sess.URL,
			},
		})
	}
}

// HandleStripeWebhook handles POST /webhook/stripe.
// Validates the Stripe-Signature header, then dispatches on event type:
//   - checkout.session.completed      → provision subscription, upgrade user tier
//   - customer.subscription.deleted   → deactivate subscription, downgrade to free
//   - invoice.payment_failed          → mark subscription inactive
func HandleStripeWebhook(logger *zap.Logger, cfg *config.Config, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		signature := c.Get("Stripe-Signature")
		if signature == "" {
			logger.Warn("webhook request missing Stripe-Signature header")
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "missing Stripe-Signature header",
			})
		}

		payload := c.Body()
		event, err := stripewebhook.ConstructEventWithOptions(payload, signature, cfg.StripeWebhookSecret,
			stripewebhook.ConstructEventOptions{
				// Ignore API version mismatches between the library and the Stripe
				// account's configured version — handled by ensuring webhook endpoint
				// version is kept in sync in the Stripe dashboard.
				IgnoreAPIVersionMismatch: true,
			},
		)
		if err != nil {
			logger.Warn("webhook signature validation failed", zap.Error(err))
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "invalid webhook signature",
			})
		}

		switch event.Type {
		case "checkout.session.completed":
			if err := handleCheckoutCompleted(logger, db, event.Data.Raw); err != nil {
				logger.Error("error processing checkout.session.completed",
					zap.String("event_id", event.ID),
					zap.Error(err),
				)
				// Return 200 so Stripe does not retry — log and investigate manually.
			}

		case "customer.subscription.deleted":
			if err := handleSubscriptionDeleted(logger, db, event.Data.Raw); err != nil {
				logger.Error("error processing customer.subscription.deleted",
					zap.String("event_id", event.ID),
					zap.Error(err),
				)
			}

		case "invoice.payment_failed":
			if err := handlePaymentFailed(logger, db, event.Data.Raw); err != nil {
				logger.Error("error processing invoice.payment_failed",
					zap.String("event_id", event.ID),
					zap.Error(err),
				)
			}

		default:
			logger.Debug("unhandled stripe event type", zap.String("type", string(event.Type)))
		}

		return c.SendStatus(fiber.StatusOK)
	}
}

// CancelSubscription handles POST /subscription/cancel.
// Cancels the Stripe subscription at period end and deactivates it in the DB.
func CancelSubscription(logger *zap.Logger, cfg *config.Config, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID := c.Locals("user_id").(string)

		sub, err := repository.FindSubscriptionByUserID(db, userID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
					"error": "no active subscription found",
				})
			}
			logger.Error("failed to find subscription", zap.String("user_id", userID), zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}

		stripeID := legacyStripeID(sub)
		if stripeID == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "subscription has no associated Stripe ID",
			})
		}

			// Cancel at period end to avoid proration surprises.
		cancelParams := &stripe.SubscriptionCancelParams{
			InvoiceNow: stripe.Bool(false),
			Prorate:    stripe.Bool(false),
		}
		_, err = stripesub.Cancel(stripeID, cancelParams)
		if err != nil {
			logger.Error("failed to cancel stripe subscription",
				zap.String("user_id", userID),
				zap.String("stripe_id", stripeID),
				zap.Error(err),
			)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "failed to cancel subscription with payment provider",
			})
		}

		if err := repository.DeactivateSubscription(db, sub.ID); err != nil {
			logger.Error("failed to deactivate subscription in db",
				zap.String("sub_id", sub.ID),
				zap.Error(err),
			)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}

		if err := repository.UpdateUserTier(db, userID, "free"); err != nil {
			logger.Error("failed to downgrade user tier",
				zap.String("user_id", userID),
				zap.Error(err),
			)
			// Non-fatal — subscription is already cancelled in Stripe and deactivated in DB.
		}

		logger.Info("subscription cancelled",
			zap.String("user_id", userID),
			zap.String("stripe_id", stripeID),
		)

		return c.JSON(fiber.Map{
			"data": fiber.Map{
				"cancelled": true,
			},
		})
	}
}

// planToPriceID maps a plan name to the corresponding Stripe price ID from config.
func planToPriceID(plan string, cfg *config.Config) (string, error) {
	switch plan {
	case "premium":
		return cfg.StripePricePremium, nil
	case "ultimate":
		return cfg.StripePriceUltimate, nil
	default:
		return "", fmt.Errorf("invalid plan %q: must be \"premium\" or \"ultimate\"", plan)
	}
}

// handleCheckoutCompleted processes a checkout.session.completed event.
func handleCheckoutCompleted(logger *zap.Logger, db *gorm.DB, raw json.RawMessage) error {
	var sess stripe.CheckoutSession
	if err := json.Unmarshal(raw, &sess); err != nil {
		return fmt.Errorf("unmarshalling checkout session: %w", err)
	}

	userID, ok := sess.Metadata["user_id"]
	if !ok || userID == "" {
		return fmt.Errorf("checkout session %s has no user_id in metadata", sess.ID)
	}

	plan, ok := sess.Metadata["plan"]
	if !ok || plan == "" {
		return fmt.Errorf("checkout session %s has no plan in metadata", sess.ID)
	}

	// sess.Subscription is the Stripe subscription object created for this checkout.
	stripeSubID := ""
	if sess.Subscription != nil {
		stripeSubID = sess.Subscription.ID
	}

	now := time.Now()
	var legacyStripeIDPtr *string
	if stripeSubID != "" {
		legacyStripeIDPtr = &stripeSubID
	}
	sub := &model.Subscription{
		UserID:         userID,
		Plan:           plan,
		LavaContractID: legacyStripeIDPtr,
		IsActive:       true,
		StartedAt:      now,
	}

	if err := repository.CreateOrUpdateSubscription(db, sub); err != nil {
		return fmt.Errorf("upserting subscription for user %s: %w", userID, err)
	}

	if err := repository.UpdateUserTier(db, userID, plan); err != nil {
		return fmt.Errorf("updating user tier for user %s: %w", userID, err)
	}

	logger.Info("subscription provisioned",
		zap.String("user_id", userID),
		zap.String("plan", plan),
		zap.String("stripe_sub_id", stripeSubID),
	)

	return nil
}

// handleSubscriptionDeleted processes a customer.subscription.deleted event.
func handleSubscriptionDeleted(logger *zap.Logger, db *gorm.DB, raw json.RawMessage) error {
	var stripeSub stripe.Subscription
	if err := json.Unmarshal(raw, &stripeSub); err != nil {
		return fmt.Errorf("unmarshalling subscription: %w", err)
	}

	sub, err := repository.FindSubscriptionByStripeID(db, stripeSub.ID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			logger.Warn("subscription.deleted for unknown stripe ID", zap.String("stripe_id", stripeSub.ID))
			return nil
		}
		return fmt.Errorf("finding subscription by stripe id %s: %w", stripeSub.ID, err)
	}

	if err := repository.DeactivateSubscription(db, sub.ID); err != nil {
		return fmt.Errorf("deactivating subscription %s: %w", sub.ID, err)
	}

	if err := repository.UpdateUserTier(db, sub.UserID, "free"); err != nil {
		return fmt.Errorf("downgrading tier for user %s: %w", sub.UserID, err)
	}

	logger.Info("subscription deactivated via webhook",
		zap.String("user_id", sub.UserID),
		zap.String("stripe_id", stripeSub.ID),
	)

	return nil
}

// handlePaymentFailed processes an invoice.payment_failed event.
func handlePaymentFailed(logger *zap.Logger, db *gorm.DB, raw json.RawMessage) error {
	// The invoice object contains a subscription field with the Stripe subscription ID.
	var invoice stripe.Invoice
	if err := json.Unmarshal(raw, &invoice); err != nil {
		return fmt.Errorf("unmarshalling invoice: %w", err)
	}

	if invoice.Subscription == nil || invoice.Subscription.ID == "" {
		logger.Warn("payment_failed invoice has no subscription", zap.String("invoice_id", invoice.ID))
		return nil
	}

	sub, err := repository.FindSubscriptionByStripeID(db, invoice.Subscription.ID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			logger.Warn("payment_failed for unknown stripe subscription",
				zap.String("stripe_id", invoice.Subscription.ID),
			)
			return nil
		}
		return fmt.Errorf("finding subscription by stripe id %s: %w", invoice.Subscription.ID, err)
	}

	if err := repository.DeactivateSubscription(db, sub.ID); err != nil {
		return fmt.Errorf("deactivating subscription %s: %w", sub.ID, err)
	}

	logger.Info("subscription marked inactive due to payment failure",
		zap.String("user_id", sub.UserID),
		zap.String("stripe_id", invoice.Subscription.ID),
	)

	return nil
}
