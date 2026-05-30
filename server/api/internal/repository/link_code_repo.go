package repository

import (
	"context"
	"errors"
	"time"

	"vpnapp/server/api/internal/model"

	"gorm.io/gorm"
)

// CreateLinkCode inserts a new link code row.
// Caller is responsible for generating a unique code and setting ExpiresAt.
func CreateLinkCode(ctx context.Context, db *gorm.DB, code *model.LinkCode) error {
	result := db.WithContext(ctx).Create(code)
	if result.Error != nil {
		if isDuplicateError(result.Error) {
			return ErrDuplicate
		}
		return result.Error
	}
	return nil
}

// FindLinkCode looks up a link code by its 6-digit value.
// Returns ErrNotFound when the code does not exist OR when it has expired
// (expired codes are treated as missing so the caller cannot enumerate them).
func FindLinkCode(ctx context.Context, db *gorm.DB, code string) (*model.LinkCode, error) {
	var lc model.LinkCode
	result := db.WithContext(ctx).Where("code = ? AND expires_at > ?", code, time.Now()).First(&lc)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, result.Error
	}
	return &lc, nil
}

// ConsumeLinkCode atomically deletes a link code by value, returning the row
// that was removed. Used by the redeem flow to guarantee one-time use even
// under concurrent requests.
func ConsumeLinkCode(ctx context.Context, db *gorm.DB, code string) (*model.LinkCode, error) {
	var lc model.LinkCode
	// Use a transaction so the SELECT-and-DELETE happens atomically.
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("code = ? AND expires_at > ?", code, time.Now()).First(&lc).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if err := tx.Delete(&model.LinkCode{}, "code = ?", code).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &lc, nil
}

// DeleteExpiredLinkCodes removes all rows whose expires_at is in the past.
// Called by the background scheduler — never user-facing.
func DeleteExpiredLinkCodes(ctx context.Context, db *gorm.DB) (int64, error) {
	result := db.WithContext(ctx).Where("expires_at <= ?", time.Now()).Delete(&model.LinkCode{})
	return result.RowsAffected, result.Error
}

// CountActiveLinkCodesForUser returns how many unexpired codes a user
// currently holds. Used to rate-limit code generation (max 1 active per user).
func CountActiveLinkCodesForUser(ctx context.Context, db *gorm.DB, userID string) (int64, error) {
	var count int64
	result := db.WithContext(ctx).Model(&model.LinkCode{}).
		Where("user_id = ? AND expires_at > ?", userID, time.Now()).
		Count(&count)
	return count, result.Error
}
