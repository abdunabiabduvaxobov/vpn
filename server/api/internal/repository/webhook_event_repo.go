package repository

import (
	"context"
	"errors"
	"time"

	"vpnapp/server/api/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// InsertWebhookEventIfNew inserts a new LavaWebhookEvent row using
// INSERT ... ON CONFLICT DO NOTHING. On Postgres this returns RowsAffected=0
// when a duplicate (by the natural-key UNIQUE index from migration 020) hits.
//
// Returns:
//   - (true, nil) on first delivery — caller proceeds to dispatch event-type handlers.
//   - (false, nil) on duplicate (PAY-04) — caller returns 200 immediately.
//   - (false, err) on DB error — caller returns 500 (lava retries per PAY-05).
//
// CRITICAL: This insert MUST commit independently of the event-processing
// transaction. RESEARCH §3.4 explains: if Steps 3 and 4 are wrapped in one
// transaction, a Step-4 failure rolls back the Step-3 dedup record, allowing
// the retry to bypass idempotency. Caller wraps THIS function's call OUTSIDE
// any larger TX.
func InsertWebhookEventIfNew(ctx context.Context, db *gorm.DB, event *model.LavaWebhookEvent) (bool, error) {
	result := db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(event)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// MarkWebhookProcessed updates lava_webhook_events.processed_at = now() OR
// lava_webhook_events.error = errStr based on whether errStr is nil.
// Best-effort — caller does NOT propagate error from this call (the side
// effect of failing here is a stale forensic record; the 500 returned to
// lava ensures retry handles the real work).
func MarkWebhookProcessed(ctx context.Context, db *gorm.DB, eventID string, errStr *string) error {
	now := time.Now()
	updates := map[string]interface{}{
		"processed_at": &now,
		"error":        errStr,
	}
	return db.WithContext(ctx).Model(&model.LavaWebhookEvent{}).Where("id = ?", eventID).Updates(updates).Error
}

// FindLavaContractByContractID returns the lava-side recurring contract row.
// Used by webhook handlers to resolve renewals (contractId on
// subscription.recurring.* events) or cancellations.
func FindLavaContractByContractID(ctx context.Context, db *gorm.DB, contractID string) (*model.LavaContract, error) {
	var c model.LavaContract
	result := db.WithContext(ctx).Where("contract_id = ?", contractID).First(&c)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, result.Error
	}
	return &c, nil
}

// UpsertLavaContract inserts a new contract or updates the lifecycle fields
// (is_active, expires_at, cancelled_at, parent_contract_id) of an existing
// row when contract_id collides. RESEARCH §3.2 prescribes the exact clause.
//
// IMPORTANT: write-once fields (user_id, offer_id, plan, periodicity, currency,
// started_at) are NOT in the DoUpdates list — a hostile or buggy webhook
// payload cannot rewrite them after the contract is first observed.
func UpsertLavaContract(ctx context.Context, db *gorm.DB, c *model.LavaContract) error {
	return db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "contract_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"is_active",
			"expires_at",
			"cancelled_at",
			"parent_contract_id",
		}),
	}).Create(c).Error
}
