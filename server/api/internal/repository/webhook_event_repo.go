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

// MarkWebhookProcessed updates lava_webhook_events.processed_at = now() and
// .error = errStr, AND sets the ADMIN-06 status column: 'DELIVERED' when errStr
// is nil (processed cleanly), 'FAILED' otherwise. Best-effort — caller does NOT
// propagate error from this call (the side effect of failing here is a stale
// forensic record; the 500 returned to lava ensures retry handles the real work).
func MarkWebhookProcessed(ctx context.Context, db *gorm.DB, eventID string, errStr *string) error {
	now := time.Now()
	status := "DELIVERED"
	if errStr != nil {
		status = "FAILED"
	}
	updates := map[string]interface{}{
		"processed_at": &now,
		"error":        errStr,
		"status":       status,
	}
	return db.WithContext(ctx).Model(&model.LavaWebhookEvent{}).Where("id = ?", eventID).Updates(updates).Error
}

// FindWebhookEventByID returns a single lava_webhook_events row by primary key.
// Used by the admin detail + replay endpoints (ADMIN-06). Returns ErrNotFound
// when no row matches.
func FindWebhookEventByID(ctx context.Context, db *gorm.DB, id string) (*model.LavaWebhookEvent, error) {
	var ev model.LavaWebhookEvent
	result := db.WithContext(ctx).Where("id = ?", id).First(&ev)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, result.Error
	}
	return &ev, nil
}

// ListWebhookEvents returns a page of lava_webhook_events rows for the admin
// webhook log (ADMIN-06), newest first. Optional filters: status and eventType
// (empty string means "no filter"). Returns the page slice plus the total count
// matching the filters (for pagination). All filter values are bound parameters
// (never concatenated) so the list endpoint is not an injection surface.
func ListWebhookEvents(ctx context.Context, db *gorm.DB, status, eventType string, page, limit int) ([]model.LavaWebhookEvent, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}

	q := db.WithContext(ctx).Model(&model.LavaWebhookEvent{})
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if eventType != "" {
		q = q.Where("event_type = ?", eventType)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var events []model.LavaWebhookEvent
	if err := q.Order("received_at DESC").
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&events).Error; err != nil {
		return nil, 0, err
	}
	return events, total, nil
}

// MarkWebhookReplayed flips a webhook event's status to 'REPLAYED' and bumps
// retried_count by one (ADMIN-06). Called after a successful applyLavaEvent
// re-application via the admin replay endpoint. The increment is an atomic
// SQL-side `retried_count + 1` so concurrent replays can't lose a count.
func MarkWebhookReplayed(ctx context.Context, db *gorm.DB, id string) error {
	return db.WithContext(ctx).Model(&model.LavaWebhookEvent{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"status":        "REPLAYED",
			"retried_count": gorm.Expr("retried_count + 1"),
		}).Error
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
