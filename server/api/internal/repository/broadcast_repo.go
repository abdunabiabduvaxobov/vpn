package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"vpnapp/server/api/internal/model"

	"gorm.io/gorm"
)

// ListActiveBroadcasts returns the broadcasts whose active window contains
// now: starts_at IS NULL OR starts_at <= now, AND ends_at IS NULL OR
// ends_at >= now. A NULL bound is open-ended. Backs the PUBLIC GET
// /broadcasts endpoint, newest-first. Uses the idx_broadcasts_active index.
func ListActiveBroadcasts(ctx context.Context, db *gorm.DB) ([]model.BroadcastMessage, error) {
	if db == nil {
		return nil, errNilDB
	}
	now := time.Now()
	var msgs []model.BroadcastMessage
	if err := db.WithContext(ctx).
		Where("(starts_at IS NULL OR starts_at <= ?) AND (ends_at IS NULL OR ends_at >= ?)", now, now).
		Order("created_at DESC").
		Find(&msgs).Error; err != nil {
		return nil, fmt.Errorf("listing active broadcasts: %w", err)
	}
	return msgs, nil
}

// ListAllBroadcasts returns every broadcast (active, expired, scheduled),
// newest-first, for the admin panel's management view.
func ListAllBroadcasts(ctx context.Context, db *gorm.DB) ([]model.BroadcastMessage, error) {
	if db == nil {
		return nil, errNilDB
	}
	var msgs []model.BroadcastMessage
	if err := db.WithContext(ctx).
		Order("created_at DESC").
		Find(&msgs).Error; err != nil {
		return nil, fmt.Errorf("listing broadcasts: %w", err)
	}
	return msgs, nil
}

// CreateBroadcast inserts a new broadcast row and populates its generated ID.
// The caller validates the severity enum before calling (T-07-30).
func CreateBroadcast(ctx context.Context, db *gorm.DB, msg *model.BroadcastMessage) error {
	if db == nil {
		return errNilDB
	}
	if err := db.WithContext(ctx).Create(msg).Error; err != nil {
		return fmt.Errorf("creating broadcast: %w", err)
	}
	return nil
}

// UpdateBroadcast applies a partial update to the broadcast identified by id.
// Returns ErrNotFound when no row matches so the handler can 404.
func UpdateBroadcast(ctx context.Context, db *gorm.DB, id string, updates map[string]interface{}) error {
	if db == nil {
		return errNilDB
	}
	res := db.WithContext(ctx).
		Model(&model.BroadcastMessage{}).
		Where("id = ?", id).
		Updates(updates)
	if res.Error != nil {
		return fmt.Errorf("updating broadcast %s: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteBroadcast removes the broadcast identified by id. Returns ErrNotFound
// when no row matched so the handler can 404 instead of silently 200ing.
func DeleteBroadcast(ctx context.Context, db *gorm.DB, id string) error {
	if db == nil {
		return errNilDB
	}
	res := db.WithContext(ctx).Where("id = ?", id).Delete(&model.BroadcastMessage{})
	if res.Error != nil {
		return fmt.Errorf("deleting broadcast %s: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// FindBroadcastByID returns a single broadcast or ErrNotFound.
func FindBroadcastByID(ctx context.Context, db *gorm.DB, id string) (*model.BroadcastMessage, error) {
	if db == nil {
		return nil, errNilDB
	}
	var msg model.BroadcastMessage
	if err := db.WithContext(ctx).Where("id = ?", id).First(&msg).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("finding broadcast %s: %w", id, err)
	}
	return &msg, nil
}
