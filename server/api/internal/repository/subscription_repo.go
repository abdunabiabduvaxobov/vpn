package repository

import (
	"context"
	"errors"

	"vpnapp/server/api/internal/model"

	"gorm.io/gorm"
)

// FindSubscriptionByUserID returns the most recent active subscription for a user.
func FindSubscriptionByUserID(ctx context.Context, db *gorm.DB, userID string) (*model.Subscription, error) {
	var sub model.Subscription
	result := db.WithContext(ctx).Where("user_id = ? AND is_active = ?", userID, true).
		Order("started_at DESC").
		First(&sub)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, result.Error
	}
	return &sub, nil
}

// CreateSubscription inserts a new subscription record.
func CreateSubscription(ctx context.Context, db *gorm.DB, sub *model.Subscription) error {
	return db.WithContext(ctx).Create(sub).Error
}

// CreateOrUpdateSubscription upserts a subscription matched on user_id.
// If an active subscription for the user already exists it is updated in place;
// otherwise a new row is inserted.
//
// Phase 3 (D-11): writes lava_contract_id; the legacy provider column is gone.
func CreateOrUpdateSubscription(ctx context.Context, db *gorm.DB, sub *model.Subscription) error {
	// Thread the request ctx onto the connection once; the lookup, the insert
	// branch, and the update branch below all reuse the same context-bound session.
	db = db.WithContext(ctx)
	var existing model.Subscription
	result := db.Where("user_id = ? AND is_active = ?", sub.UserID, true).
		Order("started_at DESC").
		First(&existing)

	if result.Error != nil {
		if !errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return result.Error
		}
		// No active subscription — insert a new one.
		return db.Create(sub).Error
	}

	// Update the existing row.
	return db.Model(&existing).Updates(map[string]interface{}{
		"plan":             sub.Plan,
		"lava_contract_id": sub.LavaContractID,
		"is_active":        sub.IsActive,
		"expires_at":       sub.ExpiresAt,
	}).Error
}

// DeactivateSubscription marks a subscription as inactive by its primary key.
func DeactivateSubscription(ctx context.Context, db *gorm.DB, subID string) error {
	result := db.WithContext(ctx).Model(&model.Subscription{}).
		Where("id = ?", subID).
		Update("is_active", false)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
