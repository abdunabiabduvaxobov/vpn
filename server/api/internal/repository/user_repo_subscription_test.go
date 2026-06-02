package repository_test

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"vpnapp/server/api/internal/model"
	"vpnapp/server/api/internal/repository"

	sqlite3 "github.com/mattn/go-sqlite3"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// HOTFIX-01 regression tests for DowngradeExpiredSubscriptions.
//
// Why these tests exist (read before deleting):
//
// The production code path that auto-downgrades expired Pro users to "free"
// already exists end-to-end on main (see plan 01-07 commit message and
// RESEARCH §HOTFIX-01 "CRITICAL FINDING"):
//
//   - column:    users.subscription_expires_at TIMESTAMPTZ NULL  (migrations/001_initial.sql:12)
//   - model:     model.User.SubscriptionExpiresAt *time.Time     (model/user.go:21)
//   - repo read: repository.DowngradeExpiredSubscriptions(db)    (user_repo.go:277-288)
//   - schedule:  scheduler.runCleanup calls it every minute      (scheduler/scheduler.go:124)
//
// The MISSING half — the webhook WRITE that POPULATES subscription_expires_at
// on successful payment — is intentionally deferred to Phase 3 (lava.top
// webhook) per CONTEXT.md D-07. The legacy payment handler has been deleted
// in Phase 8 and had zero paying users.
//
// These three regression tests therefore guard the existing scheduler
// behavior while Phase 3 is being built, so a future refactor that
// accidentally breaks the repo read or removes the column doesn't silently
// re-introduce CRIT-01 (subscription never expires).

// sqliteWithNow is a sqlite3 driver registered once for HOTFIX-01 tests with
// a user-defined NOW() SQL function that returns the current local timestamp
// in the same ISO-8601 string layout that gorm.io/driver/sqlite writes for
// time.Time columns ("2006-01-02 15:04:05.999999999-07:00"). This lets the
// production query in DowngradeExpiredSubscriptions — which embeds the
// literal SQL fragment `subscription_expires_at < NOW()` for Postgres — be
// exercised verbatim against in-memory sqlite without changing the
// production signature (zero production code modified, see plan 01-07 D-07).
//
// The lexicographic comparison of two strings in identical ISO-8601 layout
// is equivalent to the chronological comparison required by the production
// query against Postgres.
const sqliteWithNowDriverName = "sqlite3_with_now"

var registerSqliteWithNowOnce sync.Once

func registerSqliteWithNow() {
	registerSqliteWithNowOnce.Do(func() {
		sql.Register(sqliteWithNowDriverName, &sqlite3.SQLiteDriver{
			ConnectHook: func(conn *sqlite3.SQLiteConn) error {
				// GORM's sqlite driver stores time.Time using the layout
				// "2006-01-02 15:04:05.999999999-07:00" (see gorm.io/driver/sqlite).
				// Match it so string comparisons in WHERE clauses behave correctly.
				now := func() string {
					return time.Now().Format("2006-01-02 15:04:05.999999999-07:00")
				}
				return conn.RegisterFunc("NOW", now, true)
			},
		})
	})
}

// openSubscriptionTestDB opens an in-memory sqlite DB with a NOW() SQL
// function registered (so the production query
//
//	WHERE subscription_tier <> 'free'
//	  AND subscription_expires_at IS NOT NULL
//	  AND subscription_expires_at < NOW()
//
// runs verbatim against sqlite without rewriting production SQL). Mirrors
// the schema needed by user_repo subscription tests.
func openSubscriptionTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	registerSqliteWithNow()

	sqlDB, err := sql.Open(sqliteWithNowDriverName, ":memory:")
	if err != nil {
		t.Fatalf("failed to open sql.DB: %v", err)
	}

	db, err := gorm.Open(sqlite.Dialector{Conn: sqlDB}, &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open gorm db: %v", err)
	}

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id                      TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
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
	}
	for _, stmt := range stmts {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("failed to create users table: %v", err)
		}
	}
	return db
}

// timePtr returns a pointer to the supplied time. Local helper because
// model.User.SubscriptionExpiresAt is *time.Time.
func timePtr(t time.Time) *time.Time { return &t }

// seedProUser inserts a "pro" user with the supplied (optional) expiry into
// the in-memory test DB and returns the persisted ID for re-fetching.
func seedProUser(t *testing.T, db *gorm.DB, expiresAt *time.Time) string {
	t.Helper()
	email := "pro-" + t.Name()
	pw := "ph"
	user := &model.User{
		EmailHash:             &email,
		PasswordHash:          &pw,
		SubscriptionTier:      "pro",
		SubscriptionExpiresAt: expiresAt,
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to seed pro user: %v", err)
	}
	return user.ID
}

// TestDowngradeExpiredSubscriptions_DowngradesPastDueProUser proves the
// happy-path of CRIT-01's fix half: a Pro user whose expires_at is in the
// past gets their tier set to "free" on the next scheduler cycle. Also
// asserts the audit-trail invariant — subscription_expires_at is NOT
// cleared by the downgrade so the admin panel can render "expired on X".
func TestDowngradeExpiredSubscriptions_DowngradesPastDueProUser(t *testing.T) {
	db := openSubscriptionTestDB(t)

	pastDue := time.Now().Add(-1 * time.Hour)
	userID := seedProUser(t, db, timePtr(pastDue))

	count, err := repository.DowngradeExpiredSubscriptions(context.Background(), db)
	if err != nil {
		t.Fatalf("DowngradeExpiredSubscriptions returned error: %v", err)
	}
	if len(count) != 1 {
		t.Errorf("expected 1 user downgraded, got %d (%v)", len(count), count)
	}
	// PERF-04 / D-06: the returned id must be the downgraded user so the
	// scheduler can bust user:<id>.
	if len(count) == 1 && count[0] != userID {
		t.Errorf("expected returned id to be %s, got %s", userID, count[0])
	}

	got, err := repository.FindUserByID(context.Background(), db, userID)
	if err != nil {
		t.Fatalf("failed to refetch user: %v", err)
	}
	if got.SubscriptionTier != "free" {
		t.Errorf("expected subscription_tier=free after downgrade, got %q", got.SubscriptionTier)
	}
	// Audit-trail invariant: the timestamp survives the downgrade so the
	// admin UI can show "expired on <date>" instead of just "free".
	if got.SubscriptionExpiresAt == nil {
		t.Fatal("expected subscription_expires_at to be preserved as audit trail, got nil")
	}
	if !got.SubscriptionExpiresAt.Equal(pastDue) {
		// Allow a small tolerance because sqlite stores times truncated to
		// microseconds in some build configurations.
		delta := got.SubscriptionExpiresAt.Sub(pastDue)
		if delta < -time.Second || delta > time.Second {
			t.Errorf("expected subscription_expires_at == %v (audit trail), got %v (delta %v)",
				pastDue, *got.SubscriptionExpiresAt, delta)
		}
	}
}

// TestDowngradeExpiredSubscriptions_LeavesFutureProUserAlone proves a
// paying-and-current Pro user is not collateral damage: future-dated
// expires_at means the scheduler must skip the row.
func TestDowngradeExpiredSubscriptions_LeavesFutureProUserAlone(t *testing.T) {
	db := openSubscriptionTestDB(t)

	future := time.Now().Add(24 * time.Hour)
	userID := seedProUser(t, db, timePtr(future))

	count, err := repository.DowngradeExpiredSubscriptions(context.Background(), db)
	if err != nil {
		t.Fatalf("DowngradeExpiredSubscriptions returned error: %v", err)
	}
	if len(count) != 0 {
		t.Errorf("expected 0 users downgraded for future-dated expiry, got %d (%v)", len(count), count)
	}

	got, err := repository.FindUserByID(context.Background(), db, userID)
	if err != nil {
		t.Fatalf("failed to refetch user: %v", err)
	}
	if got.SubscriptionTier != "pro" {
		t.Errorf("expected subscription_tier=pro (unchanged), got %q", got.SubscriptionTier)
	}
}

// TestDowngradeExpiredSubscriptions_IgnoresNullExpiresAt proves that
// admin-comped "never-expires" Pro grants (subscription_expires_at IS NULL)
// are immune to the scheduler. Without this guard, granting an operator
// permanent Pro from the admin panel would silently flip to free on the
// next cycle.
func TestDowngradeExpiredSubscriptions_IgnoresNullExpiresAt(t *testing.T) {
	db := openSubscriptionTestDB(t)

	userID := seedProUser(t, db, nil)

	count, err := repository.DowngradeExpiredSubscriptions(context.Background(), db)
	if err != nil {
		t.Fatalf("DowngradeExpiredSubscriptions returned error: %v", err)
	}
	if len(count) != 0 {
		t.Errorf("expected 0 users downgraded for NULL expiry, got %d (%v)", len(count), count)
	}

	got, err := repository.FindUserByID(context.Background(), db, userID)
	if err != nil {
		t.Fatalf("failed to refetch user: %v", err)
	}
	if got.SubscriptionTier != "pro" {
		t.Errorf("expected subscription_tier=pro (unchanged), got %q", got.SubscriptionTier)
	}
	if got.SubscriptionExpiresAt != nil {
		t.Errorf("expected subscription_expires_at=nil (unchanged), got %v", *got.SubscriptionExpiresAt)
	}
}
