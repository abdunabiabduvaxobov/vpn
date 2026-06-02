package repository_test

import (
	"context"
	"errors"
	"testing"

	"vpnapp/server/api/internal/model"
	"vpnapp/server/api/internal/repository"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// newVlessTestDB opens an in-memory SQLite DB whose user_vless_identities schema
// mirrors migration 026 — crucially the PARTIAL UNIQUE index on (user_id) WHERE
// is_active that WR-03 added, so the "one active identity per user" invariant is
// enforced by the DB exactly as it is in Postgres. TranslateError is enabled so
// a unique violation surfaces as gorm.ErrDuplicatedKey, the same sentinel the
// production config (gormConfig) produces.
func newVlessTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger:         gormlogger.Default.LogMode(gormlogger.Silent),
		TranslateError: true,
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS user_vless_identities (
			id          TEXT PRIMARY KEY,
			user_id     TEXT NOT NULL,
			vless_uuid  TEXT NOT NULL UNIQUE,
			is_active   INTEGER NOT NULL DEFAULT 1,
			created_at  DATETIME,
			revoked_at  DATETIME
		)`,
		// Mirror migration 026's WR-03 partial unique index.
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_uvi_user_active ON user_vless_identities(user_id) WHERE is_active = 1`,
		`CREATE INDEX IF NOT EXISTS idx_uvi_active ON user_vless_identities(is_active) WHERE is_active = 1`,
	}
	for _, s := range stmts {
		if err := db.Exec(s).Error; err != nil {
			t.Fatalf("schema: %v", err)
		}
	}
	return db
}

// TestGetOrCreateActiveVlessUUID_FirstFetchAllocates verifies the lazy-allocate
// path and that a repeat call returns the SAME active UUID (idempotent read).
func TestGetOrCreateActiveVlessUUID_FirstFetchAllocates(t *testing.T) {
	db := newVlessTestDB(t)
	ctx := context.Background()
	userID := uuid.NewString()

	u1, err := repository.GetOrCreateActiveVlessUUID(ctx, db, userID)
	if err != nil {
		t.Fatalf("first allocate: %v", err)
	}
	if u1 == "" {
		t.Fatal("expected a non-empty UUID")
	}

	u2, err := repository.GetOrCreateActiveVlessUUID(ctx, db, userID)
	if err != nil {
		t.Fatalf("second fetch: %v", err)
	}
	if u2 != u1 {
		t.Fatalf("expected stable active UUID, got %q then %q", u1, u2)
	}
}

// TestGetOrCreateActiveVlessUUID_PartialUniqueRejectsSecondActive is the WR-03
// invariant: with the partial unique index in place, a manual attempt to insert
// a SECOND is_active=TRUE row for the same user is rejected (gorm.ErrDuplicatedKey)
// — proving the DB, not repo discipline, enforces one-active-per-user. The repo's
// get-or-create then re-reads the existing winner rather than producing a dup.
func TestGetOrCreateActiveVlessUUID_PartialUniqueRejectsSecondActive(t *testing.T) {
	db := newVlessTestDB(t)
	ctx := context.Background()
	userID := uuid.NewString()

	first, err := repository.GetOrCreateActiveVlessUUID(ctx, db, userID)
	if err != nil {
		t.Fatalf("first allocate: %v", err)
	}

	// Simulate the losing side of the race: a raw insert of a second active row.
	dup := &model.UserVlessIdentity{
		UserID:    userID,
		VlessUUID: uuid.NewString(),
		IsActive:  true,
	}
	insErr := db.WithContext(ctx).Create(dup).Error
	if insErr == nil {
		t.Fatal("expected partial unique index to reject a second active row, but insert succeeded")
	}
	if !errors.Is(insErr, gorm.ErrDuplicatedKey) {
		t.Fatalf("expected a duplicate-key error, got %v", insErr)
	}

	// The repo must still return the single surviving active UUID, and there must
	// be exactly one active row.
	got, err := repository.GetOrCreateActiveVlessUUID(ctx, db, userID)
	if err != nil {
		t.Fatalf("re-read after conflict: %v", err)
	}
	if got != first {
		t.Fatalf("expected surviving active UUID %q, got %q", first, got)
	}

	var activeCount int64
	if err := db.Model(&model.UserVlessIdentity{}).
		Where("user_id = ? AND is_active = ?", userID, true).
		Count(&activeCount).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if activeCount != 1 {
		t.Fatalf("expected exactly 1 active identity, got %d", activeCount)
	}
}

