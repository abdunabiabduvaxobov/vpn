package repository

import (
	"context"
	"errors"
	"fmt"

	"vpnapp/server/api/internal/model"

	"gorm.io/gorm"
)

// RestoreResult is the summary the restore handler feeds back to the
// Telegram bot so it can answer the user with concrete numbers.
type RestoreResult struct {
	OldUserID         string
	NewUserID         string
	DevicesRebound    int64
	SessionsDeleted   int64
	DeviceHashesReset int64
}

// PerformRestore executes the account-restore transaction for the
// Telegram recovery flow (ADR-006).
//
// Called after the bot has:
//   - Validated a tg_restore JWT signed by the backend and extracted
//     newUserID from its `sub` claim.
//   - Looked up telegramUserID from the bot context (from.id on the
//     /start update) and found oldUserID via FindUserByTelegramID.
//   - Asked the user to confirm the merge via an inline keyboard.
//
// Guarantees:
//   - Exactly one of {commit, rollback} happens for all rows.
//   - The device the user is currently holding (belongs to newUserID)
//     is rebound to oldUserID before newUserID is deleted, so the
//     next /auth/guest call from that device returns oldUserID's
//     tokens and the subscription is effectively transferred.
//   - oldUserID's actual telegram_user_id is re-checked inside the
//     transaction as defence in depth — a forged token that somehow
//     referenced an unrelated user_id cannot overwrite another
//     account's bindings.
//   - The function refuses to operate when old == new (no-op) or
//     when oldUserID is an admin (paranoia: admins are never
//     restored via the guest flow; the error surfaces as "not
//     linked" to the bot user).
//
// Returns ErrNotFound when either user doesn't exist or the telegram
// binding doesn't match. Other errors are wrapped with context.
func PerformRestore(ctx context.Context, db *gorm.DB, oldUserID, newUserID string, telegramUserID int64) (*RestoreResult, error) {
	if db == nil {
		return nil, errNilDB
	}
	if oldUserID == "" || newUserID == "" {
		return nil, fmt.Errorf("restore: old and new user ids are required")
	}
	if oldUserID == newUserID {
		return nil, fmt.Errorf("restore: old and new user ids are identical")
	}

	var out RestoreResult
	txErr := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. Re-verify the old user's telegram binding inside the
		//    transaction. Defence in depth: a forged JWT that
		//    somehow references an unrelated user_id as oldUserID
		//    cannot slip through — the binding must match.
		var oldUser model.User
		if err := tx.Where("id = ?", oldUserID).First(&oldUser).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return fmt.Errorf("restore: load old user: %w", err)
		}
		if oldUser.Role == "admin" {
			// Admins are never subjects of the guest recovery flow.
			// Surface as not-found so the bot says "no linked VPN
			// account" rather than acknowledging the admin exists.
			return ErrNotFound
		}
		if oldUser.TelegramUserID == nil || *oldUser.TelegramUserID != telegramUserID {
			return ErrNotFound
		}

		// 2. Verify the new user actually exists and is a plain
		//    guest (role=user, no email bound — i.e. the exact
		//    shape /auth/guest produces on a fresh install).
		var newUser model.User
		if err := tx.Where("id = ?", newUserID).First(&newUser).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return fmt.Errorf("restore: load new user: %w", err)
		}
		if newUser.Role != "user" || newUser.EmailHash != nil {
			return fmt.Errorf("restore: new user is not a fresh guest")
		}

		// 3. Rebind every device row owned by the new user to the
		//    old user. Typically exactly one (the device the user
		//    is holding right now), but guard against future
		//    multi-device onboarding flows.
		res := tx.Model(&model.Device{}).
			Where("user_id = ?", newUserID).
			Update("user_id", oldUserID)
		if res.Error != nil {
			return fmt.Errorf("restore: rebind devices: %w", res.Error)
		}
		out.DevicesRebound = res.RowsAffected

		// 4. Delete the new user's refresh sessions explicitly. The
		//    ON DELETE CASCADE on users would do this anyway, but
		//    an explicit DELETE lets us report the count back to
		//    the bot so the user knows their old app session is
		//    still valid on the old device.
		res = tx.Where("user_id = ?", newUserID).Delete(&model.Session{})
		if res.Error != nil {
			return fmt.Errorf("restore: delete new sessions: %w", res.Error)
		}
		out.SessionsDeleted = res.RowsAffected

		// 5. Delete the new user row. CASCADE cleans up any
		//    subscription rows created by GuestLogin (the free-tier
		//    row inserted on every fresh guest) and any connection
		//    history that was never attached to a device.
		if err := tx.Delete(&model.User{}, "id = ?", newUserID).Error; err != nil {
			return fmt.Errorf("restore: delete new user: %w", err)
		}

		// 6. Zero the device_secret_hash on every device still bound
		//    to old_user. This is the "re-authenticate the physical
		//    device" step — the user's reinstalled client has a new
		//    secret in its private storage, and without clearing the
		//    old hash, GuestLogin's secret mismatch check would keep
		//    minting fresh users from the phone forever. Clearing
		//    the hash puts the row into the legacy-grace state so
		//    the first secret-bearing call from the phone populates
		//    the hash and returns old_user's tokens cleanly.
		hashRes := tx.Model(&model.Device{}).
			Where("user_id = ? AND device_secret_hash <> ''", oldUserID).
			Update("device_secret_hash", "")
		if hashRes.Error != nil {
			return fmt.Errorf("restore: clear device secrets: %w", hashRes.Error)
		}
		out.DeviceHashesReset = hashRes.RowsAffected

		out.OldUserID = oldUserID
		out.NewUserID = newUserID
		return nil
	})
	if txErr != nil {
		return nil, txErr
	}
	return &out, nil
}
