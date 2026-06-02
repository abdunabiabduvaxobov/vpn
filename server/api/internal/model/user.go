package model

import "time"

// User represents a VPN user account.
// Email is stored as SHA-256 hash only — zero-knowledge policy.
// Guest users have no email or password (both fields are nil).
//
// Telegram fields (ADR-006) are an optional recovery binding. A
// user who pays for premium can link their Telegram account in the
// mobile app; the numeric Telegram user ID then lets them restore
// their subscription on any future device, across platform switches
// and factory resets. Free users have no reason to link and stay
// anonymous by default.
type User struct {
	ID                    string     `json:"id" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	EmailHash             *string    `json:"-" gorm:"uniqueIndex"`
	PasswordHash          *string    `json:"-"`
	FullName              string     `json:"full_name" gorm:"type:varchar(255);default:''"`
	SubscriptionTier      string     `json:"subscription_tier" gorm:"default:free"`
	SubscriptionExpiresAt *time.Time `json:"subscription_expires_at"`
	Role                  string     `json:"role" gorm:"type:varchar(20);default:user"`
	TelegramUserID        *int64     `json:"telegram_user_id" gorm:"column:telegram_user_id;uniqueIndex"`
	TelegramLinkedAt      *time.Time `json:"telegram_linked_at" gorm:"column:telegram_linked_at"`
	// Cached from Telegram's Update.Message.From at link time so the
	// mobile app and admin panel can render a human identity without
	// a live API round-trip. TelegramUsername is nullable because
	// not every user has set a public @username. TelegramFirstName
	// is always populated per Telegram's contract, but kept as a
	// pointer to preserve backwards-compat with pre-016 rows.
	TelegramUsername  *string `json:"telegram_username" gorm:"column:telegram_username"`
	TelegramFirstName *string `json:"telegram_first_name" gorm:"column:telegram_first_name"`
	// SSO identity columns (AUTH-03, ADR-007 §8.4, CONTEXT.md D-11).
	// AppleUserID / GoogleUserID are nullable so guest rows remain
	// untouched; Email is nullable until SSO sign-in populates it.
	// EmailIsPrivateRelay is never serialized to clients — relay
	// addresses are not a global identity, exposing them would
	// surface a routable Apple-side mailbox. AuthProvider is a soft
	// enum, last-used provider wins (D-07).
	AppleUserID         *string `json:"-" gorm:"column:apple_user_id;uniqueIndex"`
	GoogleUserID        *string `json:"-" gorm:"column:google_user_id;uniqueIndex"`
	Email               *string `json:"email" gorm:"column:email;size:320"`
	EmailVerified       bool    `json:"email_verified" gorm:"column:email_verified;default:false"`
	EmailIsPrivateRelay bool    `json:"-" gorm:"column:email_is_private_relay;default:false"`
	AuthProvider        string  `json:"auth_provider" gorm:"column:auth_provider;default:guest"`
	PlanID              string  `json:"plan_id" gorm:"column:plan_id;type:uuid;not null;index"`
	// Suspension columns (ADMIN-02, migration 024). SuspendedAt is the
	// authority bit: a non-nil value means the account is suspended and
	// SuspendedRequired 403s the user on the next protected request.
	// SuspendedReason carries the operator's free-text justification
	// (also persisted to audit_log.details, Pitfall 4). Both nullable so
	// every pre-024 / unsuspended row reads as nil.
	SuspendedAt     *time.Time `json:"suspended_at" gorm:"column:suspended_at"`
	SuspendedReason *string    `json:"suspended_reason" gorm:"column:suspended_reason"`
	CreatedAt       time.Time  `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt       time.Time  `json:"-" gorm:"autoUpdateTime"`
}

// Session represents an active user session (refresh token).
//
// DeviceID and IssueIP (HARD-04, migration 025) bind a refresh session to the
// device + IP it was issued from. DeviceID is HARD-checked on /auth/refresh —
// a mismatch is a 401, blocking a stolen refresh token from being replayed on
// another device (audit S1-7). IssueIP is SOFT-checked — an IP change is logged
// but allowed, because mobile clients roam networks (D-10). Both are nullable
// VARCHARs (no `not null`) so any pre-migration semantics stay tolerant; after
// migration 025's clean-break DELETE every live row has device_id populated.
type Session struct {
	ID               string    `gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	UserID           string    `gorm:"not null;index"`
	RefreshTokenHash string    `gorm:"not null"`
	DeviceInfo       string    `gorm:"type:varchar(255)"`
	DeviceID         string    `gorm:"column:device_id;type:varchar(255);index"`
	IssueIP          string    `gorm:"column:issue_ip;type:varchar(45)"`
	CreatedAt        time.Time `gorm:"autoCreateTime"`
	ExpiresAt        time.Time `gorm:"not null"`
}
