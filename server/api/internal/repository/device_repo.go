package repository

import (
	"context"
	"errors"
	"time"

	"vpnapp/server/api/internal/model"

	"gorm.io/gorm"
)

// FindDeviceByDeviceID looks up a device row by its OS-issued device_id.
// Returns ErrNotFound when the device has never authenticated before.
func FindDeviceByDeviceID(ctx context.Context, db *gorm.DB, deviceID string) (*model.Device, error) {
	var device model.Device
	result := db.WithContext(ctx).Where("device_id = ?", deviceID).First(&device)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, result.Error
	}
	return &device, nil
}

// CreateDevice inserts a new device row.
// Returns ErrDuplicate if the device_id already exists (caller should
// FindDeviceByDeviceID first or handle the error).
func CreateDevice(ctx context.Context, db *gorm.DB, device *model.Device) error {
	result := db.WithContext(ctx).Create(device)
	if result.Error != nil {
		if isDuplicateError(result.Error) {
			return ErrDuplicate
		}
		return result.Error
	}
	return nil
}

// TouchDevice updates last_seen_at to NOW() for the given device row.
// Idempotent — safe to call on every guest login.
func TouchDevice(ctx context.Context, db *gorm.DB, id string) error {
	result := db.WithContext(ctx).Model(&model.Device{}).
		Where("id = ?", id).
		Update("last_seen_at", time.Now())
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// SetDeviceSecretHash updates the device_secret_hash for an existing device
// row. Used during the grace-period rollout: legacy devices that had no
// secret on file get one populated on their first authenticated call.
func SetDeviceSecretHash(ctx context.Context, db *gorm.DB, id, secretHash string) error {
	result := db.WithContext(ctx).Model(&model.Device{}).
		Where("id = ?", id).
		Update("device_secret_hash", secretHash)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// ReassignDeviceUser updates a device row to belong to a different user_id
// and refreshes the platform/model/secret bound to it. Used by the
// share-code link flow: when a friend redeems a code, their existing
// device row is rebound to the plan owner so that future connections
// count against the owner's quota. The secret is rotated to whatever the
// redeeming client just sent (the old owner's secret would let the friend
// be impersonated by the previous owner).
//
// Pass empty strings for platform/model/secretHash to leave those columns
// unchanged.
func ReassignDeviceUser(ctx context.Context, db *gorm.DB, deviceID, newUserID, platform, model_, secretHash string) error {
	updates := map[string]interface{}{
		"user_id":      newUserID,
		"last_seen_at": time.Now(),
	}
	if platform != "" {
		updates["platform"] = platform
	}
	if model_ != "" {
		updates["model"] = model_
	}
	if secretHash != "" {
		updates["device_secret_hash"] = secretHash
	}
	result := db.WithContext(ctx).Model(&model.Device{}).
		Where("device_id = ?", deviceID).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// CountDevicesByUser returns the number of devices currently bound to a user.
// Used by the share-code endpoint to refuse generating a code when the owner
// is already at their device cap (no point sharing if there is no slot left).
func CountDevicesByUser(ctx context.Context, db *gorm.DB, userID string) (int64, error) {
	var count int64
	result := db.WithContext(ctx).Model(&model.Device{}).Where("user_id = ?", userID).Count(&count)
	return count, result.Error
}

// ListDevicesByUser returns all devices bound to a user, newest first.
// Exposed via /admin/users/:id/devices and the in-app "My devices" screen.
func ListDevicesByUser(ctx context.Context, db *gorm.DB, userID string) ([]model.Device, error) {
	var devices []model.Device
	result := db.WithContext(ctx).Where("user_id = ?", userID).
		Order("last_seen_at DESC").
		Find(&devices)
	return devices, result.Error
}

// DeleteDeviceByOwner removes a device row but only if it currently belongs
// to the calling user. Returns ErrNotFound when no such row exists (covers
// both "id does not exist" and "you don't own that device").
//
// Used by the in-app "Remove device" UI so a plan owner can free a slot
// after a friend's iOS reinstall (which generates a fresh IDFV) leaves a
// ghost device row consuming a quota slot.
func DeleteDeviceByOwner(ctx context.Context, db *gorm.DB, deviceRowID, ownerUserID string) error {
	result := db.WithContext(ctx).Where("id = ? AND user_id = ?", deviceRowID, ownerUserID).
		Delete(&model.Device{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// FindDeviceByID looks up a device row by its UUID primary key. Used by
// the admin handler to verify the declared user matches the device's
// actual owner before issuing a delete — see AdminDeleteUserDevice.
func FindDeviceByID(ctx context.Context, db *gorm.DB, id string) (*model.Device, error) {
	var device model.Device
	result := db.WithContext(ctx).Where("id = ?", id).First(&device)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, result.Error
	}
	return &device, nil
}

// ClearDeviceSecretHashesForUser zeros out the stored device_secret_hash
// on every device row belonging to the given user. Intended for use
// inside the Telegram recovery flow (ADR-006): after PerformRestore
// merges a new guest's /auth/guest token into the existing linked
// account, the next call from the user's physical device will have
// a fresh secret that won't match whatever was stored before the
// reinstall. Clearing the hash lets GuestLogin's legacy-grace path
// re-populate it from the first secret-bearing call, re-binding
// the device cleanly to the old account.
//
// Safe because:
//   - it only runs after a successful PerformRestore, which already
//     required proof of Telegram control
//   - the legacy-grace path still requires the caller to know the
//     device_id (not publicly leaked) and to supply a non-empty
//     secret (empty-empty is denied)
//   - the window between hash-clear and legitimate re-auth is
//     typically seconds, limited by the user opening the app
//
// Returns the number of rows updated (0 on no-op).
func ClearDeviceSecretHashesForUser(ctx context.Context, db *gorm.DB, userID string) (int64, error) {
	if db == nil {
		return 0, errNilDB
	}
	result := db.WithContext(ctx).Model(&model.Device{}).
		Where("user_id = ? AND device_secret_hash <> ''", userID).
		Update("device_secret_hash", "")
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

// AdminDeleteDevice removes a device row by id with no ownership check.
// The admin handler uses this when evicting a device from any user's
// account (e.g. after a support request for a stolen phone). Returns
// ErrNotFound when no row matches the id.
func AdminDeleteDevice(ctx context.Context, db *gorm.DB, deviceRowID string) error {
	result := db.WithContext(ctx).Where("id = ?", deviceRowID).Delete(&model.Device{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteStaleDevices removes devices whose last_seen_at is older than the
// given cutoff. Called by the background scheduler to free quota slots
// occupied by devices the user has stopped using (factory reset, lost
// phone, friend who never came back).
func DeleteStaleDevices(ctx context.Context, db *gorm.DB, olderThan time.Time) (int64, error) {
	result := db.WithContext(ctx).Where("last_seen_at < ?", olderThan).Delete(&model.Device{})
	return result.RowsAffected, result.Error
}

// ReassignDevicesByUserID reassigns EVERY device row from oldUserID to newUserID
// in a single multi-row UPDATE. This is the multi-device sibling of
// ReassignDeviceUser (which targets a single device by device_id) — required by
// plan 05's `resolveSSOUser` Step A reassign-and-orphan branch per D-06.
//
// Idempotent: if no rows match oldUserID (already reassigned by a prior call,
// or the guest never had any devices), the function returns 0, nil. Plan 05
// wraps this in a `db.Transaction(...)` together with `DeleteOrphanGuestUser`
// so the two operations succeed or fail atomically.
//
// Parameterized — no string concatenation (T-2-SQLi defense in depth).
func ReassignDevicesByUserID(ctx context.Context, db *gorm.DB, oldUserID, newUserID string) (int64, error) {
	result := db.WithContext(ctx).Model(&model.Device{}).
		Where("user_id = ?", oldUserID).
		Updates(map[string]interface{}{
			"user_id":      newUserID,
			"last_seen_at": time.Now(),
		})
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}
