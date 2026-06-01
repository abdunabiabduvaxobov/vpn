package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"vpnapp/server/api/internal/cache"
	"vpnapp/server/api/internal/config"
	"vpnapp/server/api/internal/lava"
	"vpnapp/server/api/internal/model"
	"vpnapp/server/api/internal/repository"

	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// HandleLavaWebhook handles POST /api/v1/webhook/lava.
//
// Mounted with the LavaWebhookIPAllowlist middleware (cmd/main.go) so any
// request from outside the configured CIDR set is 403'd BEFORE this handler
// is reached.
//
// Auth: X-Api-Key header compared via crypto/subtle.ConstantTimeCompare to
// LAVA_WEBHOOK_SECRET (and LAVA_WEBHOOK_SECRET_PREVIOUS during rotation).
//
// Idempotency: every received event is INSERTed via OnConflict{DoNothing} into
// lava_webhook_events. Duplicates (RowsAffected=0) return 200 immediately
// without re-applying side effects (PAY-04). Processing errors return 500 so
// lava retries (PAY-05).
//
// Event dispatch (D-19):
//
//	payment.success                          → grant tier, upsert contract
//	subscription.recurring.payment.success   → extend expires_at
//	payment.failed                           → mark invoice failed
//	subscription.recurring.payment.failed    → contract is_active=false (tier WAITS for cron)
//	subscription.cancelled                   → contract cancelled_at + is_active=false
//
// PAY-08 invariant: tier is derived from the lava `offer_id` via plan_offers
// reverse-lookup, NEVER from any client-supplied metadata in the payload.
// The resolution chain is:
//
//	payload.contractId
//	    └─→ invoices.lava_invoice_id == contractId   (initial payment)
//	OR
//	payload.parentContractId
//	    └─→ lava_contracts.contract_id == parent     (renewal)
//	         └─→ invoices.lava_invoice_id            (back-trace via started_at lookup)
//
// Once the invoice row is resolved, `invoices.offer_id` (the lava-side UUID)
// is the lookup key for FindOfferByLavaOfferID → PlanID → SetUserPlan.
// PERF-04 / D-06 (redisClient): on a successful Pro-grant / renewal the
// success handlers synchronously bust user:<id> so the buyer's Pro unlocks on
// the next AuthRequired pass (the core-value path). The bust lives on the
// SUCCESS side-effect path ONLY — a duplicate event short-circuits on the
// lava_webhook_events UNIQUE (the !isNew branch below returns BEFORE dispatch)
// and therefore neither re-applies nor re-busts (T-06-WEBHOOK-IDEMP). A bust
// error logs but never changes the 200/500 contract (the 5s TTL is the backstop).
func HandleLavaWebhook(logger *zap.Logger, cfg *config.Config, db *gorm.DB, lavaClient *lava.Client, redisClient *redis.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// 1. X-Api-Key check (PAY-07). The IP allowlist middleware already 403'd
		//    anyone outside the CIDR list before we got here.
		apiKey := c.Get("X-Api-Key")
		if !lava.VerifyAPIKey(apiKey, cfg.LavaWebhookSecret, cfg.LavaWebhookSecretPrevious) {
			logger.Warn("webhook: X-Api-Key mismatch", zap.String("path", c.Path()))
			return c.SendStatus(fiber.StatusUnauthorized)
		}

		// 2. Parse payload + capture raw body for jsonb persistence.
		rawBody := c.Body()
		var event lava.WebhookEvent
		if err := json.Unmarshal(rawBody, &event); err != nil {
			logger.Warn("webhook: invalid JSON payload", zap.Error(err))
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid payload"})
		}
		if event.EventType == "" || event.ContractID == "" {
			logger.Warn("webhook: missing eventType or contractId", zap.String("event_type", event.EventType))
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "missing eventType or contractId"})
		}

		// 3. Idempotency INSERT. ContractID copied to a pointer for the model.
		contractID := event.ContractID
		rec := &model.LavaWebhookEvent{
			EventType:  event.EventType,
			ContractID: &contractID,
			Payload:    datatypes.JSON(rawBody),
		}
		isNew, err := repository.InsertWebhookEventIfNew(c.Context(), db, rec)
		if err != nil {
			logger.Error("webhook: idempotency insert failed", zap.Error(err))
			// 500 → lava retries (PAY-05).
			return c.SendStatus(fiber.StatusInternalServerError)
		}
		if !isNew {
			// Duplicate — return 200 without re-applying (PAY-04).
			logger.Info("webhook: duplicate event ignored",
				zap.String("event_type", event.EventType),
				zap.String("contract_id", contractID),
			)
			return c.SendStatus(fiber.StatusOK)
		}

		// 4. Dispatch on event type via applyLavaEvent — the SAME transport-free
		//    dispatch the admin replay endpoint re-invokes with the stored row
		//    (ADMIN-06). Passing *rec means applyLavaEvent unmarshals the persisted
		//    Payload, so live and replay run byte-identical dispatch logic. The
		//    handleLava* success paths take WithUserLock internally (07-05), so the
		//    lock is inherited on both the live and replay paths automatically.
		processErr := applyLavaEvent(c.Context(), db, redisClient, logger, *rec)

		// 5. Record outcome.
		if processErr != nil {
			errStr := processErr.Error()
			if merr := repository.MarkWebhookProcessed(c.Context(), db, rec.ID, &errStr); merr != nil {
				logger.Warn("webhook: MarkWebhookProcessed failed (forensic record will be stale)",
					zap.String("event_id", rec.ID),
					zap.String("event_type", event.EventType),
					zap.String("contract_id", contractID),
					zap.Error(merr),
				)
			}
			logger.Error("webhook: processing failed",
				zap.String("event_type", event.EventType),
				zap.String("contract_id", contractID),
				zap.Error(processErr),
			)
			// 500 → lava retries (PAY-05). The event row stays so forensics
			// can correlate the retry.
			return c.SendStatus(fiber.StatusInternalServerError)
		}
		if merr := repository.MarkWebhookProcessed(c.Context(), db, rec.ID, nil); merr != nil {
			logger.Warn("webhook: MarkWebhookProcessed failed (forensic record will be stale)",
				zap.String("event_id", rec.ID),
				zap.String("event_type", event.EventType),
				zap.String("contract_id", contractID),
				zap.Error(merr),
			)
		}
		return c.SendStatus(fiber.StatusOK)
	}
}

// applyLavaEvent is the transport-free dispatch core shared by the live webhook
// handler (HandleLavaWebhook) and the admin replay endpoint
// (AdminReplayWebhookEvent). It unmarshals the STORED payload (ev.Payload) into a
// lava.WebhookEvent and runs the same event-type switch the live handler used to
// run inline, calling the existing handleLava* functions.
//
// Idempotency (ADMIN-06 / T-07-32): re-invoking applyLavaEvent with the same
// stored payload re-applies the SAME side effect. The tier grant is
// set-not-increment (SetUserPlanTx SETS subscription_tier/expires_at, never adds),
// so replaying a payment.success yields the same plan and an expiry anchored to
// now+periodicity — never a compounding double-extend, never a second grant.
//
// Locking (ADMIN-03 / T-07-33): the handleLava* success paths take
// repository.WithUserLock(user_id) internally (added in 07-05). Because both the
// live and replay paths funnel through applyLavaEvent, a replay racing a live
// webhook for the same user serializes on the same advisory lock and cannot
// reopen the hybrid-state race.
//
// It returns an error on a processing failure (the live handler turns that into a
// 500 so lava retries; the replay handler surfaces it to the admin). Unknown
// event types are a no-op (nil) — recorded as received, never errored.
func applyLavaEvent(ctx context.Context, db *gorm.DB, redisClient *redis.Client, logger *zap.Logger, ev model.LavaWebhookEvent) error {
	var event lava.WebhookEvent
	if err := json.Unmarshal([]byte(ev.Payload), &event); err != nil {
		return fmt.Errorf("applyLavaEvent: unmarshal stored payload (event_id=%s): %w", ev.ID, err)
	}

	switch event.EventType {
	case "payment.success":
		return handleLavaPaymentSuccess(ctx, logger, db, redisClient, &event)
	case "subscription.recurring.payment.success":
		return handleLavaRecurringSuccess(ctx, logger, db, redisClient, &event)
	case "payment.failed":
		return handleLavaPaymentFailed(ctx, logger, db, &event)
	case "subscription.recurring.payment.failed":
		return handleLavaRecurringFailed(ctx, logger, db, &event)
	case "subscription.cancelled":
		return handleLavaSubscriptionCancelled(ctx, logger, db, &event)
	default:
		// Unknown but valid-signature event: record received, no side effect.
		logger.Warn("webhook: unknown event type ignored",
			zap.String("event_type", event.EventType),
			zap.String("event_id", ev.ID),
		)
		return nil
	}
}

// handleLavaPaymentSuccess processes the first-payment event.
//
// Flow:
//  1. Look up invoice by lava_invoice_id == contractId.
//  2. Look up offer by invoice.offer_id (the lava-side offer UUID).
//  3. Compute expires_at locally: started_at + periodicity (RESEARCH §1.2 —
//     first payment.success doesn't carry expiredAt directly; we compute
//     from periodicity).
//  4. SetUserPlan(userID, planID, &contractId, expires_at) — transactional.
//  5. UpsertLavaContract.
//  6. UpdateInvoiceStatus to "paid".
func handleLavaPaymentSuccess(ctx context.Context, logger *zap.Logger, db *gorm.DB, redisClient *redis.Client, event *lava.WebhookEvent) error {
	inv, err := repository.FindInvoiceByLavaID(ctx, db, event.ContractID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return fmt.Errorf("payment.success: no invoice for contractId=%s", event.ContractID)
		}
		return fmt.Errorf("payment.success: FindInvoiceByLavaID: %w", err)
	}
	offerRow, err := repository.FindOfferByLavaOfferID(ctx, db, inv.OfferID)
	if err != nil {
		return fmt.Errorf("payment.success: FindOfferByLavaOfferID(%s): %w", inv.OfferID, err)
	}
	// PAY-08 defence-in-depth: re-resolve plan.Code from offerRow.PlanID via FindPlanByID
	// instead of trusting the denormalised inv.Plan column. The invoice was server-created
	// in /checkout but the plans table is the authoritative source for tier semantics.
	plan, err := repository.FindPlanByID(ctx, db, offerRow.PlanID)
	if err != nil {
		return fmt.Errorf("payment.success: FindPlanByID(%s): %w", offerRow.PlanID, err)
	}

	// Compute expires_at locally from periodicity.
	dur := periodicityToDuration(inv.Periodicity)
	startedAt := time.Now()
	var expiresAt *time.Time
	if dur > 0 {
		t := startedAt.Add(dur)
		expiresAt = &t
	}

	contractID := event.ContractID

	// ADMIN-03: serialize this tier-grant against the admin force-cancel path on
	// the SAME per-user advisory lock key (repository.WithUserLock keyed on the
	// resolved inv.UserID) so the two can never interleave into a hybrid state.
	// The lock is ADDITIONAL — lava_webhook_events UNIQUE (checked in
	// HandleLavaWebhook BEFORE dispatch, outside this lock) still provides event
	// idempotency; this lock only serializes the user/subscription/contract
	// WRITE block. All writes run on tx (the lock-holding transaction); using
	// SetUserPlanTx instead of SetUserPlan keeps them inside the lock rather than
	// opening a second, un-locked transaction.
	if err := repository.WithUserLock(ctx, db, inv.UserID, func(tx *gorm.DB) error {
		// 1. SetUserPlanTx — users + subscriptions row, on the lock tx.
		if err := repository.SetUserPlanTx(tx, inv.UserID, offerRow.PlanID, &contractID, expiresAt); err != nil {
			return fmt.Errorf("payment.success: SetUserPlanTx(%s, %s): %w", inv.UserID, offerRow.PlanID, err)
		}

		// 2. UpsertLavaContract. Plan field uses freshly-resolved plan.Code (NOT inv.Plan) — PAY-08 defence-in-depth.
		if err := repository.UpsertLavaContract(ctx, tx, &model.LavaContract{
			UserID:      inv.UserID,
			ContractID:  contractID,
			OfferID:     inv.OfferID,
			Plan:        plan.Code, // freshly resolved via FindPlanByID, NOT inv.Plan denormalisation
			Periodicity: inv.Periodicity,
			Currency:    inv.Currency,
			IsActive:    true,
			StartedAt:   startedAt,
			ExpiresAt:   expiresAt,
		}); err != nil {
			return fmt.Errorf("payment.success: UpsertLavaContract: %w", err)
		}

		// 3. Flip invoice status.
		if err := repository.UpdateInvoiceStatus(ctx, tx, inv.ID, "paid"); err != nil {
			return fmt.Errorf("payment.success: UpdateInvoiceStatus: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}

	// PERF-04 / D-06: Pro is now granted — bust user:<id> so the buyer's
	// next AuthRequired pass reflects Pro immediately (the core-value path:
	// "pays on risevpn.com → Pro unlocks on every device"). Reached ONLY on
	// the success path; a duplicate webhook short-circuited before dispatch
	// so this never double-busts. A bust error logs but does NOT fail the
	// handler — the 200 idempotency contract and the 5s TTL backstop hold.
	if berr := cache.BustUserCache(context.Background(), redisClient, inv.UserID); berr != nil {
		logger.Warn("webhook: BustUserCache failed on payment.success (5s TTL is the backstop)",
			zap.String("user_id", inv.UserID), zap.Error(berr))
	}

	logger.Info("webhook: payment.success applied",
		zap.String("user_id", inv.UserID),
		zap.String("plan_id", offerRow.PlanID),
		zap.String("contract_id", contractID),
		zap.Timep("expires_at", expiresAt),
	)
	return nil
}

// handleLavaRecurringSuccess extends expires_at by one period.
//
// Renewal events carry parentContractId (RESEARCH §1.5) — that points at the
// ORIGINAL contract; the event's contractId is the renewal's invoice id.
// We look up the parent contract to find the user, then compute new
// expires_at = old_expires_at + periodicity.
func handleLavaRecurringSuccess(ctx context.Context, logger *zap.Logger, db *gorm.DB, redisClient *redis.Client, event *lava.WebhookEvent) error {
	parentID := ""
	if event.ParentContractID != nil {
		parentID = *event.ParentContractID
	}
	if parentID == "" {
		return fmt.Errorf("recurring.success: missing parentContractId")
	}
	parent, err := repository.FindLavaContractByContractID(ctx, db, parentID)
	if err != nil {
		return fmt.Errorf("recurring.success: FindLavaContractByContractID(%s): %w", parentID, err)
	}

	// Extend expires_at by one period.
	dur := periodicityToDuration(parent.Periodicity)
	startedAt := time.Now()
	newExp := startedAt.Add(dur)
	// If parent already has a future expires_at, extend from THAT — preserves
	// the user's paid-for time even if the renewal hits "early".
	if parent.ExpiresAt != nil && parent.ExpiresAt.After(startedAt) {
		newExp = parent.ExpiresAt.Add(dur)
	}

	// SetUserPlan with new expires_at (keeps same plan_id; just refreshes expiry).
	contractID := event.ContractID
	planID, planErr := planIDFromContract(ctx, db, parent)
	if planErr != nil {
		// WR-01: double-failure (offer gone AND system plan gone — exceedingly
		// rare, only during a migration in-flight). Surface the error so the
		// outer wrapper records it in lava_webhook_events.error and the
		// operator sees the failed retries instead of fail-stuck silently
		// returning record-not-found on every retry.
		return fmt.Errorf("recurring.success: planIDFromContract: %w", planErr)
	}
	// ADMIN-03: same per-user advisory lock as payment.success and admin
	// force-cancel — keyed on the resolved parent.UserID — serializes this
	// renewal grant so it can never interleave a force-cancel into a hybrid
	// state. Lock is ADDITIVE to the lava_webhook_events UNIQUE idempotency
	// (checked before dispatch, outside this lock). All writes run on tx.
	if err := repository.WithUserLock(ctx, db, parent.UserID, func(tx *gorm.DB) error {
		// SetUserPlanTx with new expires_at (keeps same plan_id; just refreshes expiry).
		if err := repository.SetUserPlanTx(tx, parent.UserID, planID, &contractID, &newExp); err != nil {
			return fmt.Errorf("recurring.success: SetUserPlanTx: %w", err)
		}

		// Upsert child contract with parent_contract_id set.
		if err := repository.UpsertLavaContract(ctx, tx, &model.LavaContract{
			UserID:           parent.UserID,
			ContractID:       contractID,
			ParentContractID: &parentID,
			OfferID:          parent.OfferID,
			Plan:             parent.Plan,
			Periodicity:      parent.Periodicity,
			Currency:         parent.Currency,
			IsActive:         true,
			StartedAt:        startedAt,
			ExpiresAt:        &newExp,
		}); err != nil {
			return fmt.Errorf("recurring.success: UpsertLavaContract: %w", err)
		}

		// Also refresh parent's expires_at so the local view stays consistent.
		if err := tx.Model(&model.LavaContract{}).Where("contract_id = ?", parentID).Update("expires_at", &newExp).Error; err != nil {
			return fmt.Errorf("recurring.success: update parent expires_at: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}

	// PERF-04 / D-06: expiry just extended — bust user:<id> so the renewed
	// expires_at is reflected immediately (a user whose entry was about to
	// expire keeps Pro without waiting for the next cache fill). Success path
	// only; duplicate-safe; bust error logs but never fails the handler.
	if berr := cache.BustUserCache(context.Background(), redisClient, parent.UserID); berr != nil {
		logger.Warn("webhook: BustUserCache failed on recurring.success (5s TTL is the backstop)",
			zap.String("user_id", parent.UserID), zap.Error(berr))
	}

	logger.Info("webhook: recurring.payment.success extended",
		zap.String("user_id", parent.UserID),
		zap.String("parent_contract", parentID),
		zap.String("renewal_contract", contractID),
		zap.Time("new_expires_at", newExp),
	)
	return nil
}

// handleLavaPaymentFailed marks the matching invoice as failed.
// No tier change for first-payment failures.
func handleLavaPaymentFailed(ctx context.Context, logger *zap.Logger, db *gorm.DB, event *lava.WebhookEvent) error {
	inv, err := repository.FindInvoiceByLavaID(ctx, db, event.ContractID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			logger.Warn("payment.failed: no matching invoice", zap.String("contract_id", event.ContractID))
			return nil // benign — invoice may have been deleted
		}
		return fmt.Errorf("payment.failed: FindInvoiceByLavaID: %w", err)
	}
	if err := repository.UpdateInvoiceStatus(ctx, db, inv.ID, "failed"); err != nil {
		return fmt.Errorf("payment.failed: UpdateInvoiceStatus: %w", err)
	}
	logger.Info("webhook: payment.failed recorded", zap.String("invoice_id", inv.ID))
	return nil
}

// handleLavaRecurringFailed flips BOTH subscriptions.is_active and
// lava_contracts.is_active to false in a single transaction, per D-19 literal
// reading ("set subscriptions.is_active=false and lava_contracts.is_active=false
// **immediately**"). Tier is NOT downgraded here — the expiry cron in plan 03-09
// handles the actual plan_id flip once expires_at lapses, regardless of is_active.
//
// D-19: flip both rows to is_active=false immediately; cron handles tier downgrade at expires_at via the broader WHERE clause in 03-09.
func handleLavaRecurringFailed(ctx context.Context, logger *zap.Logger, db *gorm.DB, event *lava.WebhookEvent) error {
	parentID := ""
	if event.ParentContractID != nil {
		parentID = *event.ParentContractID
	}
	if parentID == "" {
		return fmt.Errorf("recurring.failed: missing parentContractId")
	}

	// Resolve user_id from parent contract — we need it to find the matching
	// subscriptions row (subscriptions is keyed by user_id, not contract_id
	// directly; lava_contract_id is a nullable FK that may or may not equal parentID).
	parent, ferr := repository.FindLavaContractByContractID(ctx, db, parentID)
	if ferr != nil {
		if errors.Is(ferr, repository.ErrNotFound) {
			logger.Warn("recurring.failed: parent contract not found — skipping",
				zap.String("parent_contract", parentID))
			return nil
		}
		return fmt.Errorf("recurring.failed: FindLavaContractByContractID: %w", ferr)
	}

	// Single transaction: flip both subscriptions.is_active and
	// lava_contracts.is_active to false atomically. Per D-19 literal reading.
	// WithContext(ctx) so a client disconnect / wedged query is cancellable (PERF-07).
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.Subscription{}).
			Where("user_id = ? AND lava_contract_id IS NOT NULL", parent.UserID).
			Update("is_active", false).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.LavaContract{}).
			Where("contract_id = ?", parentID).
			Update("is_active", false).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("recurring.failed: tx flip both rows: %w", err)
	}

	logger.Info("webhook: recurring.payment.failed — both is_active flipped to false (tier waits for cron at expires_at)",
		zap.String("parent_contract", parentID),
		zap.String("user_id", parent.UserID))
	return nil
}

// handleLavaSubscriptionCancelled records cancellation without touching tier.
// Cron downgrades after expires_at lapses.
func handleLavaSubscriptionCancelled(ctx context.Context, logger *zap.Logger, db *gorm.DB, event *lava.WebhookEvent) error {
	// subscription.cancelled events have NO `timestamp` — they have `cancelledAt`
	// (RESEARCH §1.5). The migration 020 UNIQUE uses COALESCE for idempotency;
	// here we just need to update the contract.
	now := time.Now()
	if err := db.WithContext(ctx).Model(&model.LavaContract{}).Where("contract_id = ?", event.ContractID).Updates(map[string]interface{}{
		"is_active":    false,
		"cancelled_at": &now,
	}).Error; err != nil {
		return fmt.Errorf("subscription.cancelled: update contract: %w", err)
	}
	logger.Info("webhook: subscription.cancelled recorded",
		zap.String("contract_id", event.ContractID))
	return nil
}

// periodicityToDuration converts lava periodicity strings to time.Duration.
// MONTHLY = 30 days (approximation — lava is the authoritative period source via
// future webhooks); PERIOD_YEAR = 365 days; PERIOD_90_DAYS = 90 days; etc.
// ONE_TIME returns 0 (no recurrence).
func periodicityToDuration(p string) time.Duration {
	switch p {
	case "MONTHLY":
		return 30 * 24 * time.Hour
	case "PERIOD_90_DAYS":
		return 90 * 24 * time.Hour
	case "PERIOD_180_DAYS":
		return 180 * 24 * time.Hour
	case "PERIOD_YEAR":
		return 365 * 24 * time.Hour
	case "ONE_TIME":
		return 0
	default:
		return 0
	}
}

// planIDFromContract looks up the offer for a given parent contract to
// resolve the plan_id. RESEARCH §1.5 confirms parent.OfferID is the lava
// offer UUID; FindOfferByLavaOfferID then resolves to local plan_id.
//
// Resolution order (fail-safe — never elevates beyond the system plan):
//  1. Offer lookup by lava_offer_id → return offer.PlanID.
//  2. On ErrNotFound for offer: fall back to the system plan.
//  3. On ErrNotFound for system plan too: return a structured error.
//  4. On any non-NotFound DB error from either step: return the wrapped error
//     immediately (don't paper over a DB outage as "fall back to free").
//
// WR-01: previously this returned "" on any failure, which the caller
// silently forwarded to SetUserPlan — producing record-not-found, a 500,
// and a lava retry-storm that hits the same condition forever. The error
// return lets the caller log loudly and the event row carry the cause.
func planIDFromContract(ctx context.Context, db *gorm.DB, contract *model.LavaContract) (string, error) {
	offer, oerr := repository.FindOfferByLavaOfferID(ctx, db, contract.OfferID)
	if oerr == nil {
		return offer.PlanID, nil
	}
	if !errors.Is(oerr, repository.ErrNotFound) {
		return "", fmt.Errorf("planIDFromContract: lookup offer %q: %w", contract.OfferID, oerr)
	}
	sid, serr := repository.FindSystemPlanID(ctx, db)
	if serr == nil {
		return sid, nil
	}
	if errors.Is(serr, repository.ErrNotFound) {
		return "", fmt.Errorf("planIDFromContract: offer %q not found AND no system plan exists", contract.OfferID)
	}
	return "", fmt.Errorf("planIDFromContract: lookup system plan: %w", serr)
}
