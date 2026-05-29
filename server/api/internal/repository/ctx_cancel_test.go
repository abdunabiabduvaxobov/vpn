package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	postgresdriver "gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// Wave 0 integration test for context-cancellation propagation (PERF-07 /
// D-08, D-09 (c)). Proves that cancelling the Go context aborts the in-flight
// Postgres query and removes it from pg_stat_activity — i.e. pgx v5 sends a
// CancelRequest and the DB connection is released rather than leaked.
//
// This is GREEN-able immediately: `db.WithContext(ctx).Exec(...)` already
// threads the context into pgx today, so the test passes once the index/cache
// work lands AND serves as the standing PERF-07 proof. The PERF-07 *handler*
// wins come from threading ctx through the repo signatures (plan 02); this test
// proves the underlying cancellation mechanism the whole phase relies on.

// startPostgresGorm spins a Postgres 16 testcontainer and returns a gorm DB on
// the pgx driver plus a cleanup func.
func startPostgresGorm(t *testing.T) *gorm.DB {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	container, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("test"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	db, err := gorm.Open(postgresdriver.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("open gorm: %v", err)
	}
	return db
}

// countSleepQueries returns how many active backends are running a pg_sleep.
func countSleepQueries(t *testing.T, db *gorm.DB) int {
	t.Helper()
	var n int64
	err := db.Raw(`
		SELECT count(*) FROM pg_stat_activity
		WHERE query LIKE '%pg_sleep%'
		  AND query NOT LIKE '%pg_stat_activity%'
		  AND state = 'active'`).Scan(&n).Error
	if err != nil {
		t.Fatalf("query pg_stat_activity: %v", err)
	}
	return int(n)
}

func TestCtxCancelAbortsQuery(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode — skipping testcontainers")
	}

	db := startPostgresGorm(t)

	ctx, cancel := context.WithCancel(context.Background())

	// Launch a long-running query bound to the cancellable ctx.
	queryDone := make(chan error, 1)
	go func() {
		queryDone <- db.WithContext(ctx).Exec(`SELECT pg_sleep(30)`).Error
	}()

	// Wait until the sleep is visibly active in pg_stat_activity.
	deadline := time.Now().Add(5 * time.Second)
	for countSleepQueries(t, db) < 1 {
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("pg_sleep query never showed up as active in pg_stat_activity")
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Cancel — pgx must send a CancelRequest and tear the query down.
	cancel()

	// The backend must drop out of pg_stat_activity within ~2s.
	gone := false
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
		if countSleepQueries(t, db) == 0 {
			gone = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !gone {
		t.Errorf("PERF-07: cancelled query must disappear from pg_stat_activity within ~2s")
	}

	// The goroutine's Exec must have returned a context-cancellation error.
	select {
	case err := <-queryDone:
		if err == nil {
			t.Errorf("PERF-07: cancelled Exec should return an error (context canceled)")
		}
	case <-time.After(3 * time.Second):
		t.Errorf("PERF-07: cancelled Exec did not return — connection may be leaked")
	}
}
