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

// Wave 0 RED-first integration test for the PERF-05 / PERF-08 indexes
// (D-09 (a)). Reuses the migrations_test.go testcontainers bootstrap and the
// shared applyMigration / splitSQLStatements / migrationsDir helpers from that
// file (same package). It runs every migration in the directory against a
// fresh Postgres 16, then asserts via EXPLAIN (FORMAT JSON) that:
//
//   (PERF-05) the stale-connection sweep uses idx_connections_heartbeat_active
//             — a partial index ON connections(last_heartbeat_at)
//               WHERE disconnected_at IS NULL.
//   (PERF-08) the analytics date-bucket query uses idx_connections_connected_at
//             — ON connections(connected_at).
//
// These indexes do not exist yet (migrations stop at 021). Until plan 03 adds
// migrations 022/023, the EXPLAIN plan shows a Seq Scan and the assertions fail
// — the intended Wave 0 RED state. The test references the exact index NAMES
// the implementation must create so the target is unambiguous.

// explainPlanText returns the EXPLAIN (FORMAT JSON) output for a query as one
// string so the test can assert on index-name presence without parsing the JSON
// tree. The whole plan (all nodes) is captured.
func explainPlanText(t *testing.T, db *sql.DB, query string) string {
	t.Helper()
	rows, err := db.Query("EXPLAIN (FORMAT JSON) " + query)
	if err != nil {
		t.Fatalf("EXPLAIN failed: %v\nquery: %s", err, query)
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan EXPLAIN row: %v", err)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("EXPLAIN rows err: %v", err)
	}
	return b.String()
}

func TestPerfIndexes(t *testing.T) {
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

	if _, err := db.Exec(`CREATE EXTENSION IF NOT EXISTS pgcrypto`); err != nil {
		t.Fatalf("enable pgcrypto: %v", err)
	}

	// Apply every migration in lexicographic order (001..023 once plan 03 lands
	// the new index migrations). migrationsDir / applyMigration are shared with
	// migrations_test.go in this same package.
	dir := migrationsDir(t)
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}
	var sqlFiles []string
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".sql") {
			sqlFiles = append(sqlFiles, f.Name())
		}
	}
	sort.Strings(sqlFiles)
	for _, name := range sqlFiles {
		applyMigration(t, db, filepath.Join(dir, name))
	}

	// Seed ~1000 connections with a realistic mix of live/disconnected rows so
	// the planner has stats that favor the partial index over a Seq Scan.
	if _, err := db.Exec(`
		INSERT INTO users (id, full_name, subscription_tier)
		VALUES (gen_random_uuid(), 'perf', 'free')`); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO vpn_servers (id, hostname, ip_address, region, city, country, country_code)
		VALUES (gen_random_uuid(), 'perf-host', '10.0.0.1', 'eu', 'AMS', 'NL', 'NL')`); err != nil {
		t.Fatalf("seed server: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO connections (id, user_id, server_id, connected_at, last_heartbeat_at, disconnected_at)
		SELECT
			gen_random_uuid(),
			(SELECT id FROM users LIMIT 1),
			(SELECT id FROM vpn_servers LIMIT 1),
			now() - (g || ' minutes')::interval,
			now() - (g || ' minutes')::interval,
			CASE WHEN g % 2 = 0 THEN now() ELSE NULL END
		FROM generate_series(1, 1000) AS g`); err != nil {
		t.Fatalf("seed connections: %v", err)
	}
	if _, err := db.Exec(`ANALYZE connections`); err != nil {
		t.Fatalf("analyze connections: %v", err)
	}

	// --- PERF-05: stale-connection sweep must use idx_connections_heartbeat_active ---
	// This mirrors CleanupStaleConnections' predicate without the COALESCE
	// (plan 03 drops the COALESCE so the partial index is usable).
	stalePlan := explainPlanText(t, db, `
		SELECT id FROM connections
		WHERE disconnected_at IS NULL
		  AND last_heartbeat_at < now() - interval '3 minutes'`)
	if !strings.Contains(stalePlan, "idx_connections_heartbeat_active") {
		t.Errorf("PERF-05: stale sweep must use idx_connections_heartbeat_active; plan:\n%s", stalePlan)
	}
	if strings.Contains(stalePlan, `"Node Type": "Seq Scan"`) {
		t.Errorf("PERF-05: stale sweep must NOT Seq Scan; plan:\n%s", stalePlan)
	}

	// --- PERF-08: analytics date-bucket query must use idx_connections_connected_at ---
	// Mirrors GetSignupsTimeseries / GetBytesTimeseries (admin_repo.go) whose
	// hot predicate is `connected_at >= <window start>` grouped by day.
	analyticsPlan := explainPlanText(t, db, `
		SELECT date_trunc('day', connected_at), count(*)
		FROM connections
		WHERE connected_at >= now() - interval '30 days'
		GROUP BY 1`)
	if !strings.Contains(analyticsPlan, "idx_connections_connected_at") {
		t.Errorf("PERF-08: analytics query must use idx_connections_connected_at; plan:\n%s", analyticsPlan)
	}
}
