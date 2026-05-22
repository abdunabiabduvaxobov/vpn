package repository

import (
	"errors"
	"fmt"

	"vpnapp/server/api/internal/model"

	"gorm.io/gorm"
)

// CreateUser inserts a new user into the database.
// Returns ErrDuplicate if the email_hash already exists.
func CreateUser(db *gorm.DB, user *model.User) error {
	result := db.Create(user)
	if result.Error != nil {
		if isDuplicateError(result.Error) {
			return ErrDuplicate
		}
		return result.Error
	}
	return nil
}

// FindUserByEmailHash looks up a user by their SHA-256 email hash.
func FindUserByEmailHash(db *gorm.DB, emailHash string) (*model.User, error) {
	var user model.User
	result := db.Where("email_hash = ?", emailHash).First(&user)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, result.Error
	}
	return &user, nil
}

// FindUserByID looks up a user by UUID.
func FindUserByID(db *gorm.DB, id string) (*model.User, error) {
	var user model.User
	result := db.First(&user, "id = ?", id)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, result.Error
	}
	return &user, nil
}

// DeleteUser permanently removes a user record by UUID.
// Used to roll back user creation when a subsequent operation (e.g. creating
// the default subscription) fails and the registration must be treated as atomic.
func DeleteUser(db *gorm.DB, userID string) error {
	result := db.Delete(&model.User{}, "id = ?", userID)
	return result.Error
}

// UpdateUserTier sets the subscription_tier on the users row identified by id.
func UpdateUserTier(db *gorm.DB, userID, tier string) error {
	result := db.Model(&model.User{}).
		Where("id = ?", userID).
		Update("subscription_tier", tier)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteOrphanGuestUser removes a user row IF it is provably safe:
//   - it has no email_hash (was an anonymous guest)
//   - it is not an admin
//   - its subscription_tier is 'free' (no paid plan to lose)
//   - it has no active subscription rows in the subscriptions table
//   - it has no remaining devices bound to it
//
// Used by LinkDevice to clean up the previous owner of a device row that
// was just rebound to a plan owner via a share code. Refusing to delete
// paid guests prevents a leaked device_id from becoming a plan-deletion
// primitive (a guest who paid for premium then had their device row
// re-linked must NOT be silently deleted along with their Stripe data).
//
// Returns ErrNotFound when the user does not exist OR does not match the
// "safe orphan" pattern — callers must treat the not-found case as a soft
// signal that no cleanup happened, not as a real error.
//
// The cascading FKs on devices, sessions, and subscriptions take care of
// removing the dependent rows; we do not need to delete them by hand.
func DeleteOrphanGuestUser(db *gorm.DB, userID string) error {
	if db == nil {
		return errNilDB
	}
	// Filter on every safety condition we can express in SQL up front.
	var user model.User
	if err := db.Where(
		"id = ? AND email_hash IS NULL AND role <> 'admin' AND subscription_tier = 'free'",
		userID,
	).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return err
	}
	// Defence in depth: refuse if any subscription row claims to be active
	// (covers historical data where subscription_tier might be 'free' but
	// a Stripe subscription is still on file).
	var subCount int64
	if err := db.Model(&model.Subscription{}).
		Where("user_id = ? AND is_active = ?", userID, true).
		Count(&subCount).Error; err != nil {
		return err
	}
	if subCount > 0 {
		return ErrNotFound
	}
	var deviceCount int64
	if err := db.Model(&model.Device{}).Where("user_id = ?", userID).Count(&deviceCount).Error; err != nil {
		return err
	}
	if deviceCount > 0 {
		return ErrNotFound
	}
	if err := db.Delete(&model.User{}, "id = ?", userID).Error; err != nil {
		return err
	}
	return nil
}

// FindUserByTelegramID looks up a user by their bound Telegram
// numeric user ID. Used by the recovery bot to find which VPN
// account belongs to the Telegram user sending /start restore_<jwt>.
//
// Returns ErrNotFound when no user has that telegram_user_id bound,
// which the bot treats as "this Telegram account has no VPN account
// to recover — please link from the old device first".
func FindUserByTelegramID(db *gorm.DB, telegramUserID int64) (*model.User, error) {
	if db == nil {
		return nil, errNilDB
	}
	var user model.User
	result := db.Where("telegram_user_id = ?", telegramUserID).First(&user)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, result.Error
	}
	return &user, nil
}

// LinkTelegramAccount binds a Telegram numeric user ID to the given
// VPN user. Sets telegram_linked_at to NOW() and caches the sender's
// @username + first_name so the UI can display a human identity
// without a live Telegram API round-trip. Called from the bot's
// /start link_<token> handler after the token has been consumed
// from Redis and the purpose has been verified.
//
// Re-linking (changing telegram_user_id on a user that already has
// one) is explicitly allowed — the mobile Account screen exposes an
// "Отвязать" button that clears the binding, after which the user
// can link a new Telegram. A direct overwrite (without unlinking
// first) is rejected to force the user through the UI flow and
// avoid silent account takeovers if a link token leaks.
//
// `username` and `firstName` may be empty strings if Telegram
// didn't supply them. Empty strings are stored as SQL NULLs so
// the mobile UI can distinguish "no username set" from "empty
// username string" via its nullability check.
//
// Returns ErrNotFound when userID does not exist, ErrDuplicate when
// the telegram ID is already bound to a different user.
func LinkTelegramAccount(db *gorm.DB, userID string, telegramUserID int64, username, firstName string) error {
	if db == nil {
		return errNilDB
	}
	// Reject overwrite: if the user already has a binding, the
	// caller must unlink first. Silent overwrite would let a stolen
	// link token rebind somebody's account to an attacker's
	// Telegram without any trace in the audit log.
	var existing model.User
	if err := db.Select("id, telegram_user_id").
		Where("id = ?", userID).
		First(&existing).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return err
	}
	if existing.TelegramUserID != nil && *existing.TelegramUserID != telegramUserID {
		return ErrDuplicate
	}

	updates := map[string]interface{}{
		"telegram_user_id":   telegramUserID,
		"telegram_linked_at": gorm.Expr("NOW()"),
	}
	// Nullable fields — persist nil when the source string is empty
	// so the UI can render a clean "no username set" state rather
	// than an ugly empty string.
	if username != "" {
		updates["telegram_username"] = username
	} else {
		updates["telegram_username"] = nil
	}
	if firstName != "" {
		updates["telegram_first_name"] = firstName
	} else {
		updates["telegram_first_name"] = nil
	}

	result := db.Model(&model.User{}).
		Where("id = ?", userID).
		Updates(updates)
	if result.Error != nil {
		if isDuplicateError(result.Error) {
			return ErrDuplicate
		}
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// UnlinkTelegramAccount clears the Telegram binding on a user,
// including the cached profile fields. Idempotent — unlinking a
// never-linked user returns nil without error so the mobile app
// can call it without checking state first.
func UnlinkTelegramAccount(db *gorm.DB, userID string) error {
	if db == nil {
		return errNilDB
	}
	result := db.Model(&model.User{}).
		Where("id = ?", userID).
		Updates(map[string]interface{}{
			"telegram_user_id":    nil,
			"telegram_linked_at":  nil,
			"telegram_username":   nil,
			"telegram_first_name": nil,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// CountTelegramLinkedUsers returns how many users currently have a
// Telegram recovery binding. Used by the admin panel analytics card
// ("X% of premium users have linked Telegram").
func CountTelegramLinkedUsers(db *gorm.DB) (int64, error) {
	if db == nil {
		return 0, errNilDB
	}
	var count int64
	if err := db.Model(&model.User{}).
		Where("telegram_user_id IS NOT NULL").
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// DowngradeExpiredSubscriptions finds every user whose paid subscription
// has passed its expiration date and resets their tier to "free". The
// subscription_expires_at timestamp is left intact as a historical marker
// so the admin panel can show "expired on X" instead of just "free".
//
// Returns the count of users downgraded. Called by the background
// scheduler every minute.
func DowngradeExpiredSubscriptions(db *gorm.DB) (int64, error) {
	if db == nil {
		return 0, errNilDB
	}
	result := db.Model(&model.User{}).
		Where("subscription_tier <> ? AND subscription_expires_at IS NOT NULL AND subscription_expires_at < NOW()", "free").
		Update("subscription_tier", "free")
	if result.Error != nil {
		return 0, fmt.Errorf("downgrading expired subscriptions: %w", result.Error)
	}
	return result.RowsAffected, nil
}

// UpdateUserName sets the full_name on the users row identified by id.
func UpdateUserName(db *gorm.DB, userID, fullName string) error {
	result := db.Model(&model.User{}).
		Where("id = ?", userID).
		Update("full_name", fullName)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// --- SSO functions (Phase 2 AUTH-04, AUTH-05, AUTH-06) -----------------------
//
// All four functions are passive readers EXCEPT PromoteGuestToSSO, which is the
// single TX-wrapped writer per D-29. The race-detection composition (FindOrCreate
// then re-read on ErrDuplicate) lives in plan 05's handler, not here.

// FindUserByAppleID returns the user whose apple_user_id matches sub, or
// ErrNotFound. Used by the Apple sign-in handler to determine whether a row
// already owns this provider sub before creating a new row.
//
// Parameterized — no string concatenation (T-2-SQLi mitigation).
func FindUserByAppleID(db *gorm.DB, sub string) (*model.User, error) {
	var user model.User
	if err := db.Where("apple_user_id = ?", sub).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &user, nil
}

// FindUserByGoogleID is the structural twin of FindUserByAppleID for the
// google_user_id column. See FindUserByAppleID for documentation.
func FindUserByGoogleID(db *gorm.DB, sub string) (*model.User, error) {
	var user model.User
	if err := db.Where("google_user_id = ?", sub).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &user, nil
}

// FindUserByVerifiedEmailForLink returns the auto-link candidate for the given
// email, or ErrNotFound. The WHERE clause filters email_verified=TRUE AND
// email_is_private_relay=FALSE per D-03 and D-04 — relay addresses MUST NEVER
// be used as an auto-link key (T-2-RelaySkip mitigation).
//
// The partial index idx_users_email_verified (created in migration 018) makes
// this lookup index-supported.
func FindUserByVerifiedEmailForLink(db *gorm.DB, email string) (*model.User, error) {
	var user model.User
	err := db.Where("email = ? AND email_verified = ? AND email_is_private_relay = ?",
		email, true, false).First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &user, nil
}

// PromoteGuestToSSO updates an existing guest user row to an SSO-bound user
// row in a single transaction (D-29). Sets the appropriate provider column
// (apple_user_id OR google_user_id), email, email_verified=true,
// email_is_private_relay=<isPrivateRelay>, auth_provider=<provider>.
//
// On UNIQUE constraint violation (the provider sub is already bound to another
// row), returns ErrDuplicate — the handler in plan 05 detects this and falls
// back to the "reassign devices + orphan guest" path per D-06.
//
// Provider MUST be "apple" or "google"; any other value returns an error
// (defense in depth — the handler should never call this with another value
// because it dispatches on the verifier output).
func PromoteGuestToSSO(db *gorm.DB, guestUserID, sub, email, provider string, isPrivateRelay bool) error {
	if provider != "apple" && provider != "google" {
		return fmt.Errorf("PromoteGuestToSSO: invalid provider %q", provider)
	}
	return db.Transaction(func(tx *gorm.DB) error {
		updates := map[string]interface{}{
			"email":                  email,
			"email_verified":         true, // SSO providers verify; private-relay flag is separate
			"email_is_private_relay": isPrivateRelay,
			"auth_provider":          provider,
		}
		switch provider {
		case "apple":
			updates["apple_user_id"] = sub
		case "google":
			updates["google_user_id"] = sub
		}
		result := tx.Model(&model.User{}).Where("id = ?", guestUserID).Updates(updates)
		if result.Error != nil {
			if isDuplicateError(result.Error) {
				return ErrDuplicate
			}
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	})
}
