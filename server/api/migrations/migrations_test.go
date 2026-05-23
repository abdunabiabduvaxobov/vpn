//go:build !integration
// +build !integration

package migrations_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// migrationsDir is resolved from the file location at test time so the test
// runs correctly regardless of the working directory chosen by `go test`.
func migrationsDir(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return wd
}

// applyMigration reads a single .sql file and executes it. Postgres
// supports the multi-statement BEGIN; ... COMMIT; blocks we use directly.
func applyMigration(t *testing.T, db *sql.DB, path string) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if _, err := db.Exec(string(body)); err != nil {
		t.Fatalf("apply %s: %v", filepath.Base(path), err)
	}
}

func TestMigrations019_020(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode — skipping testcontainers")
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
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() {
		_ = container.Terminate(context.Background())
	})

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	// Enable gen_random_uuid (Postgres 16 has pgcrypto out of the box, but the
	// extension is not auto-enabled).
	if _, err := db.Exec(`CREATE EXTENSION IF NOT EXISTS pgcrypto`); err != nil {
		t.Fatalf("enable pgcrypto: %v", err)
	}

	// Apply 001..018 in lexicographic order.
	dir := migrationsDir(t)
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}
	var sqlFiles []string
	for _, f := range files {
		name := f.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		sqlFiles = append(sqlFiles, name)
	}
	sort.Strings(sqlFiles)
	for _, name := range sqlFiles {
		// We apply through 020 here so the final assertions see both 019 and 020 effects;
		// the per-stage assertions break this sequence below.
		if name == "019_plans_catalog.sql" || name == "020_lava_payments.sql" {
			continue // applied with assertions below
		}
		applyMigration(t, db, filepath.Join(dir, name))
	}

	// Seed legacy users for the coercion check (D-08).
	if _, err := db.Exec(`
		INSERT INTO users (id, full_name, subscription_tier) VALUES
		    (gen_random_uuid(), 'a', 'premium'),
		    (gen_random_uuid(), 'b', 'ultimate'),
		    (gen_random_uuid(), 'c', 'free')`); err != nil {
		t.Fatalf("seed users: %v", err)
	}

	// Apply 019.
	applyMigration(t, db, filepath.Join(dir, "019_plans_catalog.sql"))

	// --- Assertions on 019 (PAY-01) ---
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM users WHERE subscription_tier IN ('premium','ultimate')`).Scan(&n); err != nil {
		t.Fatalf("count legacy tiers: %v", err)
	}
	if n != 0 {
		t.Errorf("D-08: premium/ultimate must be coerced to pro; %d remain", n)
	}

	var proMaxDevices int
	if err := db.QueryRow(`SELECT max_devices FROM plans WHERE code = 'pro'`).Scan(&proMaxDevices); err != nil {
		t.Fatalf("query pro max_devices: %v", err)
	}
	if proMaxDevices != 3 {
		t.Errorf("D-08: Pro max_devices must be 3, got %d", proMaxDevices)
	}

	if err := db.QueryRow(`SELECT count(*) FROM plan_offers WHERE plan_id = (SELECT id FROM plans WHERE code='pro')`).Scan(&n); err != nil {
		t.Fatalf("count pro offers: %v", err)
	}
	if n != 6 {
		t.Errorf("D-09: 6 placeholder offers expected for Pro, got %d", n)
	}

	if err := db.QueryRow(`SELECT count(*) FROM users WHERE plan_id IS NULL`).Scan(&n); err != nil {
		t.Fatalf("count null plan_id: %v", err)
	}
	if n != 0 {
		t.Errorf("all users must have plan_id backfilled; %d remain NULL", n)
	}

	// Apply 020.
	applyMigration(t, db, filepath.Join(dir, "020_lava_payments.sql"))

	// --- Assertions on 020 (PAY-01 + D-11) ---
	var exists bool
	if err := db.QueryRow(`SELECT EXISTS(
		SELECT 1 FROM information_schema.columns
		WHERE table_name='subscriptions' AND column_name='stripe_id')`).Scan(&exists); err != nil {
		t.Fatalf("check stripe_id existence: %v", err)
	}
	if exists {
		t.Errorf("D-11: subscriptions.stripe_id must be dropped")
	}

	if err := db.QueryRow(`SELECT EXISTS(
		SELECT 1 FROM information_schema.columns
		WHERE table_name='subscriptions' AND column_name='lava_contract_id')`).Scan(&exists); err != nil {
		t.Fatalf("check lava_contract_id existence: %v", err)
	}
	if !exists {
		t.Errorf("D-11: subscriptions.lava_contract_id must be added")
	}

	// Sanity: all six new tables exist.
	for _, table := range []string{"plans", "plan_servers", "plan_offers", "invoices", "lava_contracts", "lava_webhook_events"} {
		if err := db.QueryRow(`SELECT EXISTS(
			SELECT 1 FROM information_schema.tables WHERE table_name = $1)`, table).Scan(&exists); err != nil {
			t.Fatalf("check %s: %v", table, err)
		}
		if !exists {
			t.Errorf("table %s must exist after 020", table)
		}
	}

	// COALESCE expression index sanity — insert a subscription.cancelled row
	// (no `timestamp` field, only `cancelledAt`), then insert it AGAIN and
	// expect the unique constraint to reject it.
	payload1 := `{"cancelledAt":"2026-05-23T10:00:00Z","contractId":"contract-X"}`
	if _, err := db.Exec(`
		INSERT INTO lava_webhook_events (event_type, contract_id, payload)
		VALUES ('subscription.cancelled', 'contract-X', $1::jsonb)`, payload1); err != nil {
		t.Fatalf("first cancelled event insert: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO lava_webhook_events (event_type, contract_id, payload)
		VALUES ('subscription.cancelled', 'contract-X', $1::jsonb)`, payload1); err == nil {
		t.Errorf("D-10 COALESCE: duplicate subscription.cancelled (no timestamp) must violate unique")
	}
}
