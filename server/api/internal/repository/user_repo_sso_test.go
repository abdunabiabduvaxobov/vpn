package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"vpnapp/server/api/internal/model"
	"vpnapp/server/api/internal/repository"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// newSSOTestDB opens an in-memory SQLite DB whose schema mirrors the
// SSO-extended users table from migration 018 (plan 02-01) plus the
// minimum sessions + devices tables needed to exercise DeleteUserSessions
// and ReassignDevicesByUserID. The two partial unique indexes on
// apple_user_id / google_user_id are created so PromoteGuestToSSO's
// duplicate-sub path returns ErrDuplicate via isDuplicateError.
func newSSOTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id                      TEXT PRIMARY KEY,
			email_hash              TEXT UNIQUE,
			password_hash           TEXT,
			full_name               TEXT NOT NULL DEFAULT '',
			subscription_tier       TEXT NOT NULL DEFAULT 'free',
			subscription_expires_at DATETIME,
			role                    TEXT NOT NULL DEFAULT 'user',
			telegram_user_id        INTEGER UNIQUE,
			telegram_linked_at      DATETIME,
			telegram_username       TEXT,
			telegram_first_name     TEXT,
			apple_user_id           TEXT,
			google_user_id          TEXT,
			email                   TEXT,
			email_verified          INTEGER NOT NULL DEFAULT 0,
			email_is_private_relay  INTEGER NOT NULL DEFAULT 0,
			auth_provider           TEXT NOT NULL DEFAULT 'guest',
			plan_id                 TEXT NOT NULL DEFAULT '',
			suspended_at            DATETIME,
			suspended_reason        TEXT,
			created_at              DATETIME,
			updated_at              DATETIME
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_apple_user_id ON users(apple_user_id) WHERE apple_user_id IS NOT NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_google_user_id ON users(google_user_id) WHERE google_user_id IS NOT NULL`,
		`CREATE TABLE IF NOT EXISTS sessions (
			id                  TEXT PRIMARY KEY,
			user_id             TEXT NOT NULL,
			refresh_token_hash  TEXT NOT NULL,
			device_info         TEXT,
			device_id           TEXT,
			issue_ip            TEXT,
			created_at          DATETIME,
			expires_at          DATETIME NOT NULL
		)`,
		// devices table — mirrors the production shape used by
		// internal/handler/auth_test.go (id is the UUID PK; device_id is
		// the OS-issued unique identifier column). ReassignDevicesByUserID
		// updates by user_id, so the PK column name doesn't matter for the
		// function under test, but the GORM model.Device's tag mapping does.
		`CREATE TABLE IF NOT EXISTS devices (
			id                  TEXT PRIMARY KEY,
			user_id             TEXT NOT NULL,
			device_id           TEXT NOT NULL UNIQUE,
			device_secret_hash  TEXT,
			platform            TEXT NOT NULL DEFAULT '',
			model               TEXT NOT NULL DEFAULT '',
			first_seen_at       DATETIME,
			last_seen_at        DATETIME
		)`,
	}
	for _, s := range stmts {
		if err := db.Exec(s).Error; err != nil {
			t.Fatalf("create schema: %v", err)
		}
	}
	return db
}

func seedGuestSSO(t *testing.T, db *gorm.DB) string {
	t.Helper()
	id := uuid.NewString()
	if err := db.Exec(`INSERT INTO users(id, full_name, subscription_tier, role, auth_provider, created_at, updated_at) VALUES(?, '', 'free', 'user', 'guest', ?, ?)`,
		id, time.Now(), time.Now()).Error; err != nil {
		t.Fatalf("seed guest: %v", err)
	}
	return id
}

func seedSSOUser(t *testing.T, db *gorm.DB, applyApple, applyGoogle *string, email *string, verified, relay bool, provider string) string {
	t.Helper()
	id := uuid.NewString()
	if err := db.Exec(`INSERT INTO users(id, full_name, subscription_tier, role, apple_user_id, google_user_id, email, email_verified, email_is_private_relay, auth_provider, created_at, updated_at) VALUES(?, '', 'free', 'user', ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, applyApple, applyGoogle, email, verified, relay, provider, time.Now(), time.Now()).Error; err != nil {
		t.Fatalf("seed sso user: %v", err)
	}
	return id
}

func seedSSODevice(t *testing.T, db *gorm.DB, userID string) string {
	t.Helper()
	id := uuid.NewString()
	devID := uuid.NewString()
	if err := db.Exec(`INSERT INTO devices(id, user_id, device_id, platform, model, device_secret_hash, first_seen_at, last_seen_at) VALUES(?, ?, ?, 'ios', 'test', 'hash', ?, ?)`,
		id, userID, devID, time.Now(), time.Now()).Error; err != nil {
		t.Fatalf("seed device: %v", err)
	}
	return id
}

func ssoStrPtr(s string) *string { return &s }

func TestFindUserByAppleID_HappyPath(t *testing.T) {
	db := newSSOTestDB(t)
	wantID := seedSSOUser(t, db, ssoStrPtr("A123"), nil, ssoStrPtr("u@example.com"), true, false, "apple")

	got, err := repository.FindUserByAppleID(context.Background(), db, "A123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != wantID {
		t.Errorf("ID: want %q, got %q", wantID, got.ID)
	}
}

func TestFindUserByAppleID_NotFound(t *testing.T) {
	db := newSSOTestDB(t)
	_, err := repository.FindUserByAppleID(context.Background(), db, "DOES_NOT_EXIST")
	if !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestFindUserByGoogleID_HappyPath(t *testing.T) {
	db := newSSOTestDB(t)
	wantID := seedSSOUser(t, db, nil, ssoStrPtr("G456"), ssoStrPtr("u@example.com"), true, false, "google")
	got, err := repository.FindUserByGoogleID(context.Background(), db, "G456")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != wantID {
		t.Errorf("ID: want %q, got %q", wantID, got.ID)
	}
}

func TestFindUserByVerifiedEmailForLink_HappyPath(t *testing.T) {
	db := newSSOTestDB(t)
	wantID := seedSSOUser(t, db, ssoStrPtr("A123"), nil, ssoStrPtr("x@example.com"), true, false, "apple")
	got, err := repository.FindUserByVerifiedEmailForLink(context.Background(), db, "x@example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != wantID {
		t.Errorf("ID: want %q, got %q", wantID, got.ID)
	}
	if !got.EmailVerified || got.EmailIsPrivateRelay {
		t.Errorf("bad row: EmailVerified=%v EmailIsPrivateRelay=%v", got.EmailVerified, got.EmailIsPrivateRelay)
	}
}

func TestFindUserByVerifiedEmailForLink_UnverifiedEmail_Excluded(t *testing.T) {
	db := newSSOTestDB(t)
	seedSSOUser(t, db, ssoStrPtr("A123"), nil, ssoStrPtr("x@example.com"), false, false, "apple")
	_, err := repository.FindUserByVerifiedEmailForLink(context.Background(), db, "x@example.com")
	if !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("want ErrNotFound for unverified email, got %v", err)
	}
}

func TestFindUserByVerifiedEmailForLink_PrivateRelayExcluded(t *testing.T) {
	db := newSSOTestDB(t)
	seedSSOUser(t, db, ssoStrPtr("A123"), nil, ssoStrPtr("abc@privaterelay.appleid.com"), true, true, "apple")
	_, err := repository.FindUserByVerifiedEmailForLink(context.Background(), db, "abc@privaterelay.appleid.com")
	if !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("CRITICAL: private-relay email MUST NOT match (T-2-RelaySkip). got %v", err)
	}
}

func TestPromoteGuestToSSO_HappyPath_Apple(t *testing.T) {
	db := newSSOTestDB(t)
	guestID := seedGuestSSO(t, db)

	if err := repository.PromoteGuestToSSO(context.Background(), db, guestID, "A123", "u@example.com", "apple", "", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := repository.FindUserByID(context.Background(), db, guestID)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got.AppleUserID == nil || *got.AppleUserID != "A123" {
		t.Errorf("AppleUserID: want A123, got %v", got.AppleUserID)
	}
	if got.Email == nil || *got.Email != "u@example.com" {
		t.Errorf("Email: want u@example.com, got %v", got.Email)
	}
	if !got.EmailVerified {
		t.Errorf("EmailVerified: want true")
	}
	if got.EmailIsPrivateRelay {
		t.Errorf("EmailIsPrivateRelay: want false")
	}
	if got.AuthProvider != "apple" {
		t.Errorf("AuthProvider: want apple, got %q", got.AuthProvider)
	}
}

func TestPromoteGuestToSSO_HappyPath_Google(t *testing.T) {
	db := newSSOTestDB(t)
	guestID := seedGuestSSO(t, db)

	if err := repository.PromoteGuestToSSO(context.Background(), db, guestID, "G456", "u@example.com", "google", "", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, _ := repository.FindUserByID(context.Background(), db, guestID)
	if got.GoogleUserID == nil || *got.GoogleUserID != "G456" {
		t.Errorf("GoogleUserID: want G456, got %v", got.GoogleUserID)
	}
	if got.AuthProvider != "google" {
		t.Errorf("AuthProvider: want google, got %q", got.AuthProvider)
	}
}

func TestPromoteGuestToSSO_PrivateRelay(t *testing.T) {
	db := newSSOTestDB(t)
	guestID := seedGuestSSO(t, db)
	if err := repository.PromoteGuestToSSO(context.Background(), db, guestID, "A999", "abc@privaterelay.appleid.com", "apple", "", true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, _ := repository.FindUserByID(context.Background(), db, guestID)
	if !got.EmailIsPrivateRelay {
		t.Errorf("EmailIsPrivateRelay: want true for relay address")
	}
}

func TestPromoteGuestToSSO_DuplicateSub_ReturnsErrDuplicate(t *testing.T) {
	db := newSSOTestDB(t)
	// Existing user owns "A123" already.
	seedSSOUser(t, db, ssoStrPtr("A123"), nil, ssoStrPtr("first@example.com"), true, false, "apple")
	// Guest tries to promote with the same sub.
	guestID := seedGuestSSO(t, db)
	err := repository.PromoteGuestToSSO(context.Background(), db, guestID, "A123", "second@example.com", "apple", "", false)
	if !errors.Is(err, repository.ErrDuplicate) {
		t.Errorf("want ErrDuplicate on partial-unique collision, got %v", err)
	}
}

func TestPromoteGuestToSSO_InvalidProvider_ReturnsError(t *testing.T) {
	db := newSSOTestDB(t)
	guestID := seedGuestSSO(t, db)
	err := repository.PromoteGuestToSSO(context.Background(), db, guestID, "A123", "u@example.com", "facebook", "", false)
	if err == nil {
		t.Fatal("expected error for invalid provider")
	}
}

func TestPromoteGuestToSSO_GuestRowMissing_ReturnsErrNotFound(t *testing.T) {
	db := newSSOTestDB(t)
	err := repository.PromoteGuestToSSO(context.Background(), db, uuid.NewString(), "A123", "u@example.com", "apple", "", false)
	if !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("want ErrNotFound for missing guest, got %v", err)
	}
}

// WR-04: PromoteGuestToSSO MUST update users.full_name when the caller
// passes a non-empty fullName (the SSO-supplied display name).
// REVIEW.md WR-04 / AUTH-05 contract fidelity.
func TestPromoteGuestToSSO_UpdatesFullName(t *testing.T) {
	db := newSSOTestDB(t)
	guestID := seedGuestSSO(t, db) // full_name=''
	if err := repository.PromoteGuestToSSO(context.Background(), db, guestID, "A123", "u@example.com", "apple", "Alice Apple", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := repository.FindUserByID(context.Background(), db, guestID)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got.FullName != "Alice Apple" {
		t.Errorf("FullName: want %q, got %q (WR-04 regression)", "Alice Apple", got.FullName)
	}
	// Other fields should also be set as before.
	if got.AppleUserID == nil || *got.AppleUserID != "A123" {
		t.Errorf("AppleUserID: want A123, got %v", got.AppleUserID)
	}
	if got.AuthProvider != "apple" {
		t.Errorf("AuthProvider: want apple, got %q", got.AuthProvider)
	}
}

// WR-04 backwards-compat: an empty fullName MUST NOT blank out an existing
// full_name value. Guests that set a custom name out-of-band (or had their
// name backfilled by some other path) must keep it after promotion.
func TestPromoteGuestToSSO_EmptyFullName_PreservesExisting(t *testing.T) {
	db := newSSOTestDB(t)
	guestID := seedGuestSSO(t, db)
	// Directly set the guest's full_name to "OriginalName" so we can prove
	// it's preserved (seedGuestSSO inserts full_name='').
	if err := db.Exec("UPDATE users SET full_name = ? WHERE id = ?", "OriginalName", guestID).Error; err != nil {
		t.Fatalf("update full_name: %v", err)
	}

	if err := repository.PromoteGuestToSSO(context.Background(), db, guestID, "A123", "u@example.com", "apple", "", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, _ := repository.FindUserByID(context.Background(), db, guestID)
	if got.FullName != "OriginalName" {
		t.Errorf("FullName: want %q (preserved), got %q (WR-04 regression — empty fullName overwrote)",
			"OriginalName", got.FullName)
	}
}

// --- DeleteUserSessions tests --------------------------------------------------

func seedSSOSession(t *testing.T, db *gorm.DB, userID string) {
	t.Helper()
	if err := db.Create(&model.Session{
		ID:               uuid.NewString(),
		UserID:           userID,
		RefreshTokenHash: uuid.NewString(),
		ExpiresAt:        time.Now().Add(30 * 24 * time.Hour),
	}).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
}

func TestDeleteUserSessions_RemovesAllForUser(t *testing.T) {
	db := newSSOTestDB(t)
	a := seedGuestSSO(t, db)
	b := seedGuestSSO(t, db)
	seedSSOSession(t, db, a)
	seedSSOSession(t, db, a)
	seedSSOSession(t, db, a)
	seedSSOSession(t, db, b)
	seedSSOSession(t, db, b)

	count, err := repository.DeleteUserSessions(context.Background(), db, a)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 3 {
		t.Errorf("rows affected: want 3, got %d", count)
	}
	// User B's sessions untouched.
	var n int64
	db.Raw("SELECT COUNT(*) FROM sessions WHERE user_id = ?", b).Scan(&n)
	if n != 2 {
		t.Errorf("user B sessions: want 2, got %d", n)
	}
}

func TestDeleteUserSessions_NoSessions_ReturnsZero(t *testing.T) {
	db := newSSOTestDB(t)
	count, err := repository.DeleteUserSessions(context.Background(), db, uuid.NewString())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("rows affected: want 0, got %d", count)
	}
}

// --- ReassignDevicesByUserID tests (W-1 + B-3 fixes) ---------------------------

func TestReassignDevicesByUserID_MovesAllDevices(t *testing.T) {
	db := newSSOTestDB(t)
	guestID := seedGuestSSO(t, db)
	existingID := seedGuestSSO(t, db)
	d1 := seedSSODevice(t, db, guestID)
	d2 := seedSSODevice(t, db, guestID)
	d3 := seedSSODevice(t, db, existingID)

	count, err := repository.ReassignDevicesByUserID(context.Background(), db, guestID, existingID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 2 {
		t.Errorf("rows affected: want 2, got %d", count)
	}

	// All three devices now belong to existingID.
	var n int64
	db.Raw("SELECT COUNT(*) FROM devices WHERE user_id = ?", existingID).Scan(&n)
	if n != 3 {
		t.Errorf("existing user devices: want 3, got %d", n)
	}
	db.Raw("SELECT COUNT(*) FROM devices WHERE user_id = ?", guestID).Scan(&n)
	if n != 0 {
		t.Errorf("guest user devices: want 0, got %d", n)
	}
	// Sanity: the specific device rows each exist exactly once.
	for _, d := range []string{d1, d2, d3} {
		db.Raw("SELECT COUNT(*) FROM devices WHERE id = ?", d).Scan(&n)
		if n != 1 {
			t.Errorf("device %q count: want 1, got %d", d, n)
		}
	}
}

func TestReassignDevicesByUserID_NoDevicesIsNoop(t *testing.T) {
	db := newSSOTestDB(t)
	guestID := seedGuestSSO(t, db)
	count, err := repository.ReassignDevicesByUserID(context.Background(), db, guestID, uuid.NewString())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("rows affected: want 0 (idempotent), got %d", count)
	}
}
