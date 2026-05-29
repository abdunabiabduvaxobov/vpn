package repository

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Sentinel errors for repository operations.
var (
	ErrNotFound  = errors.New("record not found")
	ErrDuplicate = errors.New("duplicate record")
)

// gormConfig returns the GORM configuration used for the production DB.
// Extracted from NewDB so the D-10d PrepareStmt contract is unit-testable
// without a live PostgreSQL (see db_test.go / TestGormConfig).
//
// PrepareStmt enables GORM's server-side prepared-statement cache. The repo's
// query set is small and static (no dynamic SQL string concatenation found in
// the repository layer), so statements cache cleanly. [D-10d]
func gormConfig() *gorm.Config {
	return &gorm.Config{
		Logger:      logger.Default.LogMode(logger.Warn),
		PrepareStmt: true, // D-10d
	}
}

// applyPoolSettings applies the D-10b connection-pool tuning to a *sql.DB.
// Extracted so it can be exercised against any driver in tests.
//
// sql.DB exposes no getters for MaxIdleConns / ConnMaxIdleTime, so the
// behavioral test (db_test.go) asserts the apply path runs without panic and
// the values are guarded by the acceptance-criteria grep below.
func applyPoolSettings(sqlDB *sql.DB) {
	sqlDB.SetMaxIdleConns(25) // D-10b — was 10; more warm conns for the API + scheduler
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(1 * time.Hour)
	sqlDB.SetConnMaxIdleTime(5 * time.Minute) // D-10b — recycle idle conns so the pool doesn't pin stale server-side state
}

// NewDB opens a PostgreSQL connection via GORM.
// Schema is managed by SQL migrations (001_initial.sql), not AutoMigrate.
func NewDB(databaseURL string) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(databaseURL), gormConfig())
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("getting sql.DB: %w", err)
	}

	// Connection pool settings (D-10b).
	applyPoolSettings(sqlDB)

	// Verify connectivity
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	return db, nil
}

// isDuplicateError checks if a GORM error is a unique constraint violation.
// Handles both PostgreSQL ("23505") and SQLite ("UNIQUE constraint failed").
func isDuplicateError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "23505") || strings.Contains(msg, "UNIQUE constraint failed")
}
