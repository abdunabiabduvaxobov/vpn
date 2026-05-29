package repository

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TestGormConfig_PrepareStmtEnabled locks the D-10d contract: the production
// GORM config enables the server-side prepared-statement cache. NewDB itself
// needs a live PostgreSQL (it Pings), so we assert the extracted config helper
// instead.
func TestGormConfig_PrepareStmtEnabled(t *testing.T) {
	cfg := gormConfig()
	if !cfg.PrepareStmt {
		t.Error("gormConfig().PrepareStmt = false, want true (D-10d prepared-statement cache)")
	}
	if cfg.Logger == nil {
		t.Error("gormConfig().Logger = nil, want a configured logger")
	}
}

// TestApplyPoolSettings_NoPanic exercises the D-10b pool-tuning path against a
// sqlite-backed *sql.DB. sql.DB exposes no getters for MaxIdleConns /
// ConnMaxIdleTime, so this is a behavioral guard (the apply path runs cleanly);
// the concrete values (25 idle, 5m idle-time) are locked by acceptance-criteria
// grep on db.go.
func TestApplyPoolSettings_NoPanic(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), gormConfig())
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql.DB: %v", err)
	}

	applyPoolSettings(sqlDB)

	// Sanity: the connection is still usable after applying pool settings.
	if err := sqlDB.Ping(); err != nil {
		t.Errorf("ping after applyPoolSettings: %v", err)
	}
}
