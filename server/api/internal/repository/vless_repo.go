package repository

import (
	"context"
	"errors"
	"time"

	"vpnapp/server/api/internal/model"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// GetOrCreateActiveVlessUUID returns the user's currently-active VLESS UUID,
// lazily allocating one on first request (HARD-02, D-06).
//
// If the user already has an active identity its UUID is returned. Otherwise a
// fresh random UUIDv4 is inserted (is_active=true) and returned. The allocation
// is random-in-DB (uuid.NewString), NOT a deterministic HMAC — so a UUID is
// unguessable and unlinkable across users.
//
// This is the read path GetServerConfig calls per config fetch; it runs on the
// shared *gorm.DB (its own implicit statement), not inside a caller transaction.
func GetOrCreateActiveVlessUUID(ctx context.Context, db *gorm.DB, userID string) (string, error) {
	if db == nil {
		return "", errNilDB
	}
	var ident model.UserVlessIdentity
	err := db.WithContext(ctx).
		Where("user_id = ? AND is_active = ?", userID, true).
		Order("created_at DESC").
		First(&ident).Error
	if err == nil {
		return ident.VlessUUID, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", err
	}

	// No active identity — allocate a fresh random UUIDv4 (D-06).
	newUUID := uuid.NewString()
	row := &model.UserVlessIdentity{
		UserID:    userID,
		VlessUUID: newUUID,
		IsActive:  true,
	}
	if err := db.WithContext(ctx).Create(row).Error; err != nil {
		return "", err
	}
	return newUUID, nil
}

// RotateVlessUUID retires all of the user's active identities and issues a fresh
// one, inside the caller-supplied transaction (HARD-02, D-07).
//
// Called from the plan-change paths (admin tier change + lava payment.success)
// inside the SAME WithUserLock transaction as the plan write, so the rotation is
// serialized with every other per-user mutation and is atomic with the tier
// grant. The old UUID is marked is_active=false / revoked_at=now() but keeps
// working at the wire until the next tunnel reload — the natural ~30-60s window
// IS the grace period (D-07), avoiding mid-request connection drops for the user
// who just changed plans. Returns the new active UUID.
func RotateVlessUUID(ctx context.Context, tx *gorm.DB, userID string) (string, error) {
	if tx == nil {
		return "", errNilDB
	}
	now := time.Now()
	// Retire every currently-active identity for this user.
	if err := tx.WithContext(ctx).
		Model(&model.UserVlessIdentity{}).
		Where("user_id = ? AND is_active = ?", userID, true).
		Updates(map[string]interface{}{"is_active": false, "revoked_at": now}).Error; err != nil {
		return "", err
	}

	// Issue a fresh random UUIDv4 as the new active identity.
	newUUID := uuid.NewString()
	row := &model.UserVlessIdentity{
		UserID:    userID,
		VlessUUID: newUUID,
		IsActive:  true,
	}
	if err := tx.WithContext(ctx).Create(row).Error; err != nil {
		return "", err
	}
	return newUUID, nil
}

// RevokeAllVlessUUIDs marks every active identity for the user inactive, inside
// the caller-supplied transaction (HARD-02). No replacement is issued — used by
// admin force-cancel / suspend where the user should lose wire access entirely
// until they re-fetch a config (which lazily re-allocates via
// GetOrCreateActiveVlessUUID). The revoked UUID stops being admitted at the next
// tunnel reload (~30-60s floor).
func RevokeAllVlessUUIDs(ctx context.Context, tx *gorm.DB, userID string) error {
	if tx == nil {
		return errNilDB
	}
	return tx.WithContext(ctx).
		Model(&model.UserVlessIdentity{}).
		Where("user_id = ? AND is_active = ?", userID, true).
		Updates(map[string]interface{}{"is_active": false, "revoked_at": time.Now()}).Error
}

// ListActiveVlessUUIDs returns the set of all currently-active VLESS UUIDs the
// tunnel should admit (HARD-02). This is the active-set the tunnel pulls each
// heartbeat over the internal channel.
//
// serverID is accepted for forward-compat with per-server scoping but is NOT
// used today: per the shared-UUID model the identities are global (one active
// UUID per user, admitted on every server the user's plan grants). If per-server
// scoping is later needed, extend with a plan_servers join keyed on serverID.
// Returned newest-first so the set is deterministic for hashing/ETag purposes.
func ListActiveVlessUUIDs(ctx context.Context, db *gorm.DB, serverID string) ([]string, error) {
	if db == nil {
		return nil, errNilDB
	}
	var uuids []string
	err := db.WithContext(ctx).
		Model(&model.UserVlessIdentity{}).
		Where("is_active = ?", true).
		Order("created_at DESC").
		Pluck("vless_uuid", &uuids).Error
	if err != nil {
		return nil, err
	}
	return uuids, nil
}
