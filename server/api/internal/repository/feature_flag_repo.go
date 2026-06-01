package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"vpnapp/server/api/internal/model"

	"gorm.io/gorm"
)

// GetFeatureFlag reads the boolean value of a single feature flag from the
// authoritative feature_flags row. This is the DB fall-through behind
// cache.GetFlag (the ~10s Redis cache is the fast read path; this is the
// source of truth).
//
// A missing row is NOT an error — an unseeded flag reads as false (the safe
// default: signups/payments ON, maintenance OFF). Only a real DB error is
// returned so the caller (Maintenance / RequireFlagOff) can fail open.
func GetFeatureFlag(ctx context.Context, db *gorm.DB, key string) (bool, error) {
	if db == nil {
		return false, errNilDB
	}
	var flag model.FeatureFlag
	if err := db.WithContext(ctx).Where("key = ?", key).First(&flag).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Unseeded flag → safe default (false). Not an error.
			return false, nil
		}
		return false, fmt.Errorf("reading feature flag %q: %w", key, err)
	}
	return flag.Value, nil
}

// SetFeatureFlag upserts a feature flag value, recording the acting admin and
// the update time. updatedBy is the admin UUID (nil for system-initiated
// writes). The write is a parameterized GORM upsert (T-07-30: value is a
// strict bool, never a client string).
func SetFeatureFlag(ctx context.Context, db *gorm.DB, key string, value bool, updatedBy *string) error {
	if db == nil {
		return errNilDB
	}
	flag := model.FeatureFlag{
		Key:       key,
		Value:     value,
		UpdatedAt: time.Now(),
		UpdatedBy: updatedBy,
	}
	// Upsert on the PK so an admin can set a flag that was never explicitly
	// seeded, and re-setting a seeded flag updates value/updated_at/updated_by.
	if err := db.WithContext(ctx).Save(&flag).Error; err != nil {
		return fmt.Errorf("setting feature flag %q: %w", key, err)
	}
	return nil
}

// ListFeatureFlags returns all feature flag rows ordered by key so the admin
// panel renders a stable list.
func ListFeatureFlags(ctx context.Context, db *gorm.DB) ([]model.FeatureFlag, error) {
	if db == nil {
		return nil, errNilDB
	}
	var flags []model.FeatureFlag
	if err := db.WithContext(ctx).Order("key ASC").Find(&flags).Error; err != nil {
		return nil, fmt.Errorf("listing feature flags: %w", err)
	}
	return flags, nil
}
