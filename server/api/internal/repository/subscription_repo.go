package repository

import (
	"errors"

	"vpnapp/server/api/internal/model"

	"gorm.io/gorm"
)

// FindSubscriptionByUserID returns the most recent active subscription for a user.
func FindSubscriptionByUserID(db *gorm.DB, userID string) (*model.Subscription, error) {
	var sub model.Subscription
	result := db.Where("user_id = ? AND is_active = ?", userID, true).
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
func CreateSubscription(db *gorm.DB, sub *model.Subscription) error {
	return db.Create(sub).Error
}

// CreateOrUpdateSubscription upserts a subscription matched on user_id.
// If an active subscription for the user already exists it is updated in place;
// otherwise a new row is inserted.
//
// Phase 3 (D-11): writes lava_contract_id; the legacy Stripe column is gone.
func CreateOrUpdateSubscription(db *gorm.DB, sub *model.Subscription) error {
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
func DeactivateSubscription(db *gorm.DB, subID string) error {
	result := db.Model(&model.Subscription{}).
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
