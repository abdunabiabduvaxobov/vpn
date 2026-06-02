// Package testutil provides shared integration-test helpers for server/api.
//
// StartPostgres is the single source of a real Postgres connection for the
// Phase 7 advisory-lock race (ADMIN-03) and webhook-replay idempotency
// (ADMIN-06) tests — SQLite cannot exercise pg_advisory_xact_lock, so those
// tests need a live Postgres. The helper spins postgres:16-alpine via
// testcontainers, applies EVERY migration in server/api/migrations in
// lexicographic order, and returns a gorm DB on the pgx driver.
//
// It t.Skips (never t.Fatals) when Docker/testcontainers is unavailable so CI
// without Docker stays green. Callers MUST also guard on testing.Short().
package testutil

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	postgresdriver "gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// StartPostgres spins a Postgres 16 testcontainer, applies all migrations
// (001..NNN) in lexicographic order, and returns a gorm DB on the pgx driver.
//
// It returns nil and calls t.Skip when Docker/testcontainers is unavailable;
// callers must `if db == nil { return }` after calling it (and should also
// guard the whole test behind `if testing.Short() { t.Skip(...) }`).
//
// The container is terminated via t.Cleanup.
func StartPostgres(t *testing.T) *gorm.DB {
	t.Helper()

	if testing.Short() {
		t.Skip("integration: -short set, skipping testcontainers Postgres")
		return nil
	}

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
		// Docker / testcontainers unavailable — skip rather than fail so CI
		// without Docker stays green.
		t.Skipf("integration: no docker/testcontainers available: %v", err)
		return nil
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}

	db, err := gorm.Open(postgresdriver.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
		// WR-03: mirror production gormConfig so integration tests observe the
		// same gorm.ErrDuplicatedKey translation for unique-violation paths.
		TranslateError: true,
	})
	if err != nil {
		t.Fatalf("open gorm: %v", err)
	}

	// gen_random_uuid requires pgcrypto (Postgres 16 ships it but does not
	// auto-enable it).
	if err := db.Exec(`CREATE EXTENSION IF NOT EXISTS pgcrypto`).Error; err != nil {
		t.Fatalf("enable pgcrypto: %v", err)
	}

	applyAllMigrations(t, db)

	return db
}

// migrationsDir resolves server/api/migrations relative to THIS source file via
// runtime.Caller so it works regardless of the test's working directory.
// pg.go lives at server/api/internal/testutil/pg.go, so migrations is ../../migrations.
func migrationsDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("runtime.Caller failed — cannot locate migrations dir")
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "migrations")
}

// applyAllMigrations applies every *.sql file in the migrations dir in
// lexicographic order. Mirrors migrations_test.go::applyMigration — files
// containing CONCURRENTLY are split at statement boundaries and run one
// statement per Exec (CREATE INDEX CONCURRENTLY cannot run inside a tx).
func applyAllMigrations(t *testing.T, db *gorm.DB) {
	t.Helper()
	dir := migrationsDir(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read migrations dir %s: %v", dir, err)
	}
	var files []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	for _, name := range files {
		applyMigration(t, db, filepath.Join(dir, name))
	}
}

// applyMigration executes a single migration .sql file. Files using
// CONCURRENTLY are split and run statement-by-statement so the
// non-transactional DDL is not wrapped in an implicit transaction.
func applyMigration(t *testing.T, db *gorm.DB, path string) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	src := string(body)
	if strings.Contains(strings.ToUpper(src), "CONCURRENTLY") {
		for _, stmt := range splitSQLStatements(src) {
			if strings.TrimSpace(stmt) == "" {
				continue
			}
			if err := db.Exec(stmt).Error; err != nil {
				t.Fatalf("apply %s (stmt): %v", filepath.Base(path), err)
			}
		}
		return
	}
	if err := db.Exec(src).Error; err != nil {
		t.Fatalf("apply %s: %v", filepath.Base(path), err)
	}
}

// splitSQLStatements naively splits SQL on semicolons after stripping
// single-line `-- comments`. Sufficient for the hand-authored migration files
// (none use dollar-quoted bodies or semicolons inside literals).
func splitSQLStatements(src string) []string {
	var cleaned strings.Builder
	for _, line := range strings.Split(src, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		cleaned.WriteString(line)
		cleaned.WriteString("\n")
	}
	parts := strings.Split(cleaned.String(), ";")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if strings.TrimSpace(p) == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}
