package repository_test

import (
	"context"
	"testing"
	"time"

	"vpnapp/server/api/internal/model"
	"vpnapp/server/api/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// ptr returns a pointer to v — used to populate the nullable LavaContractID
// column without per-test boilerplate.
func ptr[T any](v T) *T { return &v }

// openTestDB opens an in-memory SQLite database with the tables needed for
// subscription repository tests.
func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id                      TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
			email_hash              TEXT NOT NULL UNIQUE,
			password_hash           TEXT NOT NULL,
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
			created_at              DATETIME,
			updated_at              DATETIME
		)`,
		`CREATE TABLE IF NOT EXISTS subscriptions (
			id               TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
			user_id          TEXT NOT NULL,
			plan             TEXT NOT NULL DEFAULT 'free',
			lava_contract_id TEXT,
			is_active        INTEGER NOT NULL DEFAULT 1,
			started_at       DATETIME,
			expires_at       DATETIME
		)`,
	}
	for _, stmt := range stmts {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("failed to create test table: %v", err)
		}
	}
	return db
}

func seedTestUser(t *testing.T, db *gorm.DB) *model.User {
	t.Helper()
	emailHash := "testhash-" + t.Name()
	passwordHash := "ph"
	user := &model.User{
		EmailHash:        &emailHash,
		PasswordHash:     &passwordHash,
		SubscriptionTier: "free",
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
	return user
}

// ---- FindSubscriptionByUserID ----

func TestFindSubscriptionByUserID_NotFound(t *testing.T) {
	db := openTestDB(t)
	_, err := repository.FindSubscriptionByUserID(context.Background(), db, "nonexistent-user")
	if err == nil {
		t.Fatal("expected ErrNotFound, got nil")
	}
	if err != repository.ErrNotFound {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestFindSubscriptionByUserID_ReturnsActiveSub(t *testing.T) {
	db := openTestDB(t)
	user := seedTestUser(t, db)

	sub := &model.Subscription{
		UserID:    user.ID,
		Plan:      "pro",
		IsActive:  true,
		StartedAt: time.Now(),
	}
	if err := db.Create(sub).Error; err != nil {
		t.Fatalf("failed to seed subscription: %v", err)
	}

	found, err := repository.FindSubscriptionByUserID(context.Background(), db, user.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found.Plan != "pro" {
		t.Errorf("expected plan=pro, got %q", found.Plan)
	}
}

func TestFindSubscriptionByUserID_SkipsInactiveSub(t *testing.T) {
	db := openTestDB(t)
	user := seedTestUser(t, db)

	// Insert an active subscription first, then deactivate it directly so
	// GORM does not skip the false value as a zero-value field on Create.
	sub := &model.Subscription{
		UserID:    user.ID,
		Plan:      "pro",
		IsActive:  true,
		StartedAt: time.Now(),
	}
	if err := db.Create(sub).Error; err != nil {
		t.Fatalf("failed to seed subscription: %v", err)
	}
	if err := db.Model(sub).Update("is_active", false).Error; err != nil {
		t.Fatalf("failed to deactivate subscription: %v", err)
	}

	_, err := repository.FindSubscriptionByUserID(context.Background(), db, user.ID)
	if err != repository.ErrNotFound {
		t.Errorf("expected ErrNotFound for inactive sub, got: %v", err)
	}
}

// ---- CreateSubscription ----

func TestCreateSubscription_InsertsRow(t *testing.T) {
	db := openTestDB(t)
	user := seedTestUser(t, db)

	sub := &model.Subscription{
		UserID:    user.ID,
		Plan:      "pro",
		IsActive:  true,
		StartedAt: time.Now(),
	}
	if err := repository.CreateSubscription(context.Background(), db, sub); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sub.ID == "" {
		t.Error("expected subscription ID to be set after insert")
	}
}

// ---- CreateOrUpdateSubscription ----

func TestCreateOrUpdateSubscription_CreatesWhenNoneExist(t *testing.T) {
	db := openTestDB(t)
	user := seedTestUser(t, db)

	sub := &model.Subscription{
		UserID:         user.ID,
		Plan:           "pro",
		LavaContractID: ptr("contract_new"),
		IsActive:       true,
		StartedAt:      time.Now(),
	}
	if err := repository.CreateOrUpdateSubscription(context.Background(), db, sub); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var count int64
	db.Model(&model.Subscription{}).Where("user_id = ?", user.ID).Count(&count)
	if count != 1 {
		t.Errorf("expected 1 subscription row, got %d", count)
	}
}

func TestCreateOrUpdateSubscription_UpdatesExistingActiveSub(t *testing.T) {
	db := openTestDB(t)
	user := seedTestUser(t, db)

	// Create initial subscription.
	initial := &model.Subscription{
		UserID:         user.ID,
		Plan:           "pro",
		LavaContractID: ptr("contract_old"),
		IsActive:       true,
		StartedAt:      time.Now(),
	}
	if err := db.Create(initial).Error; err != nil {
		t.Fatalf("failed to seed subscription: %v", err)
	}

	// Upsert with updated contract.
	updated := &model.Subscription{
		UserID:         user.ID,
		Plan:           "pro",
		LavaContractID: ptr("contract_new"),
		IsActive:       true,
		StartedAt:      time.Now(),
	}
	if err := repository.CreateOrUpdateSubscription(context.Background(), db, updated); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// There must still be exactly one row — not two.
	var count int64
	db.Model(&model.Subscription{}).Where("user_id = ?", user.ID).Count(&count)
	if count != 1 {
		t.Errorf("expected 1 subscription row after upsert, got %d", count)
	}

	var found model.Subscription
	db.Where("user_id = ?", user.ID).First(&found)
	if found.Plan != "pro" {
		t.Errorf("expected plan=pro after update, got %q", found.Plan)
	}
	if found.LavaContractID == nil || *found.LavaContractID != "contract_new" {
		t.Errorf("expected lava_contract_id=contract_new after update, got %v", found.LavaContractID)
	}
}

// ---- DeactivateSubscription ----

func TestDeactivateSubscription_SetsIsActiveFalse(t *testing.T) {
	db := openTestDB(t)
	user := seedTestUser(t, db)

	sub := &model.Subscription{
		UserID:    user.ID,
		Plan:      "pro",
		IsActive:  true,
		StartedAt: time.Now(),
	}
	if err := db.Create(sub).Error; err != nil {
		t.Fatalf("failed to seed subscription: %v", err)
	}

	if err := repository.DeactivateSubscription(context.Background(), db, sub.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var found model.Subscription
	db.First(&found, "id = ?", sub.ID)
	if found.IsActive {
		t.Error("expected is_active=false after deactivation")
	}
}

func TestDeactivateSubscription_NotFound(t *testing.T) {
	db := openTestDB(t)
	err := repository.DeactivateSubscription(context.Background(), db, "nonexistent-id")
	if err != repository.ErrNotFound {
		t.Errorf("expected ErrNotFound for unknown ID, got: %v", err)
	}
}
