package scheduler_test

import (
	"testing"
	"time"

	"vpnapp/server/api/internal/config"
	"vpnapp/server/api/internal/scheduler"

	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// testCfg returns a config with the duration tunables set to sane defaults
// so the scheduler doesn't have to work with zero values.
func testCfg() *config.Config {
	return &config.Config{
		StaleConnectionAfter: 3 * time.Minute,
		StaleDeviceAfter:     30 * 24 * time.Hour,
		LinkCodeTTL:          5 * time.Minute,
	}
}

// openTestDB creates an in-memory SQLite database with a minimal sessions table
// used to verify that DeleteExpiredSessions is called by the scheduler.
func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open test DB: %v", err)
	}

	if err := db.Exec(`CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		refresh_token_hash TEXT NOT NULL,
		device_info TEXT,
		created_at DATETIME,
		expires_at DATETIME NOT NULL
	)`).Error; err != nil {
		t.Fatalf("failed to create sessions table: %v", err)
	}

	return db
}

// seedExpiredSession inserts a session that expired in the past.
func seedExpiredSession(t *testing.T, db *gorm.DB) {
	t.Helper()
	past := time.Now().Add(-1 * time.Hour).Format("2006-01-02 15:04:05")
	if err := db.Exec(`INSERT INTO sessions (id, user_id, refresh_token_hash, expires_at)
		VALUES ('sess-1', 'user-1', 'hash-1', ?)`, past).Error; err != nil {
		t.Fatalf("failed to seed expired session: %v", err)
	}
}

// countSessions returns the number of rows in the sessions table.
func countSessions(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var count int64
	if err := db.Raw("SELECT COUNT(*) FROM sessions").Scan(&count).Error; err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	return count
}

func TestScheduler_StopBeforeStart_IsNoop(t *testing.T) {
	// Calling Stop before Start must not panic.
	scheduler.Stop()
}

func TestScheduler_StartStop_DoesNotPanic(t *testing.T) {
	db := openTestDB(t)
	log := zap.NewNop()

	scheduler.Start(db, log, testCfg(), nil)
	// Give the immediate cleanup goroutine a moment to run.
	time.Sleep(50 * time.Millisecond)
	scheduler.Stop()
}

func TestScheduler_CleansExpiredSessionsOnStart(t *testing.T) {
	db := openTestDB(t)
	log := zap.NewNop()

	seedExpiredSession(t, db)
	if count := countSessions(t, db); count != 1 {
		t.Fatalf("expected 1 session before scheduler starts, got %d", count)
	}

	scheduler.Start(db, log, testCfg(), nil)
	// The scheduler runs cleanup immediately on start; give it a moment.
	time.Sleep(100 * time.Millisecond)
	scheduler.Stop()

	if count := countSessions(t, db); count != 0 {
		t.Fatalf("expected 0 sessions after cleanup, got %d", count)
	}
}

func TestScheduler_StartTwice_IsNoop(t *testing.T) {
	db := openTestDB(t)
	log := zap.NewNop()

	scheduler.Start(db, log, testCfg(), nil)
	scheduler.Start(db, log, testCfg(), nil) // second call must be a no-op, not a panic
	scheduler.Stop()
}
