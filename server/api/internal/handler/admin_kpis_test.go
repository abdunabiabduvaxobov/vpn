// GREEN test for ADMIN-01 (KPI dashboard, plan 07-02) and the still-RED stub
// for ADMIN-04 (server drain, plan 07-06).
//
// Package handler (matches webhook_lava_test.go) so it can reuse the in-memory
// SQLite + miniredis setup pattern used across the handler tests.
package handler

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// setupKPITestDB builds the minimum schema GetDashboardKPIs + GetMRR touch:
// users, plans, plan_offers, vpn_servers, subscriptions, connections,
// lava_contracts, lava_webhook_events. Mirrors setupAdminPlansDB /
// setupWebhookTestDB (SetMaxOpenConns(1) + randomblob uuid default).
func setupKPITestDB(t *testing.T) (*gorm.DB, *redis.Client) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db.DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	uuidDefault := `(lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6))))`

	stmts := []string{
		`CREATE TABLE users (
			id TEXT PRIMARY KEY DEFAULT ` + uuidDefault + `,
			email TEXT,
			full_name TEXT NOT NULL DEFAULT '',
			subscription_tier TEXT NOT NULL DEFAULT 'free',
			subscription_expires_at TIMESTAMP,
			role TEXT NOT NULL DEFAULT 'user',
			plan_id TEXT NOT NULL DEFAULT '',
			suspended_at TIMESTAMP,
			suspended_reason TEXT,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE plans (
			id TEXT PRIMARY KEY DEFAULT ` + uuidDefault + `,
			code TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			max_devices INTEGER NOT NULL DEFAULT 0,
			max_servers INTEGER NOT NULL DEFAULT 0,
			speed_limit_mbps INTEGER NOT NULL DEFAULT 0,
			is_active INTEGER NOT NULL DEFAULT 1,
			is_system INTEGER NOT NULL DEFAULT 0,
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE plan_offers (
			id TEXT PRIMARY KEY DEFAULT ` + uuidDefault + `,
			plan_id TEXT NOT NULL,
			periodicity TEXT NOT NULL,
			currency TEXT NOT NULL,
			amount REAL NOT NULL,
			lava_offer_id TEXT,
			is_active INTEGER NOT NULL DEFAULT 1,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE vpn_servers (
			id TEXT PRIMARY KEY DEFAULT ` + uuidDefault + `,
			hostname TEXT NOT NULL,
			is_active INTEGER NOT NULL DEFAULT 1,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE subscriptions (
			id TEXT PRIMARY KEY DEFAULT ` + uuidDefault + `,
			user_id TEXT NOT NULL,
			plan TEXT NOT NULL DEFAULT 'free',
			lava_contract_id TEXT,
			is_active INTEGER NOT NULL DEFAULT 1,
			started_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			expires_at TIMESTAMP
		)`,
		`CREATE TABLE connections (
			id TEXT PRIMARY KEY DEFAULT ` + uuidDefault + `,
			user_id TEXT NOT NULL,
			server_id TEXT NOT NULL,
			connected_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			disconnected_at TIMESTAMP,
			bytes_up INTEGER NOT NULL DEFAULT 0,
			bytes_down INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'connected',
			last_heartbeat_at TIMESTAMP
		)`,
		`CREATE TABLE lava_contracts (
			id TEXT PRIMARY KEY DEFAULT ` + uuidDefault + `,
			user_id TEXT NOT NULL,
			contract_id TEXT NOT NULL UNIQUE,
			offer_id TEXT NOT NULL DEFAULT '',
			plan TEXT NOT NULL DEFAULT '',
			periodicity TEXT NOT NULL DEFAULT '',
			currency TEXT NOT NULL DEFAULT '',
			is_active INTEGER NOT NULL DEFAULT 1,
			started_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			expires_at TIMESTAMP,
			cancelled_at TIMESTAMP,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE lava_webhook_events (
			id TEXT PRIMARY KEY DEFAULT ` + uuidDefault + `,
			event_type TEXT NOT NULL,
			contract_id TEXT,
			invoice_id TEXT,
			payload TEXT NOT NULL DEFAULT '{}',
			received_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			processed_at TIMESTAMP,
			error TEXT
		)`,
	}
	for _, s := range stmts {
		if err := db.Exec(s).Error; err != nil {
			t.Fatalf("DDL: %v", err)
		}
	}

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return db, rdb
}

// TestAdminGetStatsKPIs is the ADMIN-01 live-KPI proof (SC-1).
//
// Seeds one system (free) plan, one paid plan with a $10 MONTHLY USD offer, a
// paid user on that plan with an active connection, a cancelled lava_contract,
// and a failed webhook event — then asserts GET /admin/stats returns all eight
// new KPI keys (plus the four legacy ones) with the expected non-zero values.
func TestAdminGetStatsKPIs(t *testing.T) {
	db, rdb := setupKPITestDB(t)
	now := time.Now().UTC()

	// Plans: one system/free, one paid "pro".
	if err := db.Exec(`INSERT INTO plans (id, code, name, is_system) VALUES (?, 'free', 'Free', 1)`, "plan-free").Error; err != nil {
		t.Fatalf("seed free plan: %v", err)
	}
	if err := db.Exec(`INSERT INTO plans (id, code, name, is_system) VALUES (?, 'pro', 'Pro', 0)`, "plan-pro").Error; err != nil {
		t.Fatalf("seed pro plan: %v", err)
	}
	// $10/mo USD offer on the paid plan.
	if err := db.Exec(`INSERT INTO plan_offers (plan_id, periodicity, currency, amount, is_active) VALUES (?, 'MONTHLY', 'USD', 10.0, 1)`, "plan-pro").Error; err != nil {
		t.Fatalf("seed offer: %v", err)
	}
	// Paid user, not lapsed.
	if err := db.Exec(`INSERT INTO users (id, subscription_tier, plan_id, subscription_expires_at) VALUES (?, 'pro', ?, ?)`,
		"user-paid", "plan-pro", now.Add(30*24*time.Hour)).Error; err != nil {
		t.Fatalf("seed paid user: %v", err)
	}
	// Active connection (heartbeat within 2 min, not disconnected).
	if err := db.Exec(`INSERT INTO connections (id, user_id, server_id, disconnected_at, last_heartbeat_at) VALUES (?, ?, 'srv-1', NULL, ?)`,
		"conn-1", "user-paid", now.Add(-30*time.Second)).Error; err != nil {
		t.Fatalf("seed connection: %v", err)
	}
	// A cancelled contract in the last 30 days → churn_30d = 1.
	if err := db.Exec(`INSERT INTO lava_contracts (user_id, contract_id, cancelled_at) VALUES (?, 'contract-1', ?)`,
		"user-paid", now.Add(-2*24*time.Hour)).Error; err != nil {
		t.Fatalf("seed cancelled contract: %v", err)
	}
	// A failed webhook event in the last 30 days → failed_payments_30d = 1.
	if err := db.Exec(`INSERT INTO lava_webhook_events (event_type, payload, received_at) VALUES ('payment.failed', '{}', ?)`,
		now.Add(-1*24*time.Hour)).Error; err != nil {
		t.Fatalf("seed failed webhook: %v", err)
	}

	app := fiber.New()
	app.Get("/admin/stats", AdminGetStats(zap.NewNop(), db, rdb))

	resp, err := app.Test(httptest.NewRequest("GET", "/admin/stats", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d body=%s", resp.StatusCode, string(b))
	}

	var body struct {
		Data map[string]interface{} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// All eight new KPI keys must be present.
	for _, k := range []string{
		"paid_users", "mrr", "active_connections",
		"signups_today", "signups_week", "signups_month",
		"churn_30d", "failed_payments_30d",
	} {
		if _, ok := body.Data[k]; !ok {
			t.Errorf("missing KPI key %q in response: %+v", k, body.Data)
		}
	}

	// The four legacy keys must be preserved (no regression).
	for _, k := range []string{"total_users", "active_subscriptions", "server_count", "active_server_count"} {
		if _, ok := body.Data[k]; !ok {
			t.Errorf("legacy key %q dropped from /admin/stats: %+v", k, body.Data)
		}
	}

	// Assert the seeded values (JSON numbers decode to float64).
	assertNum := func(key string, want float64) {
		t.Helper()
		got, ok := body.Data[key].(float64)
		if !ok {
			t.Fatalf("%s is not a number: %#v", key, body.Data[key])
		}
		if got != want {
			t.Errorf("%s = %v, want %v", key, got, want)
		}
	}
	assertNum("paid_users", 1)
	assertNum("active_connections", 1)
	assertNum("churn_30d", 1)
	assertNum("failed_payments_30d", 1)
	assertNum("mrr", 10.0)             // single $10/mo USD offer
	assertNum("signups_today", 1)      // the paid user created just now
	assertNum("signups_week", 1)
	assertNum("signups_month", 1)

	// Second call must read MRR from the cache:admin:mrr:USD key seeded by the
	// first call (5-min TTL). Prove the key exists and holds the computed value.
	cached, err := rdb.Get(httptest.NewRequest("GET", "/", nil).Context(), "cache:admin:mrr:USD").Result()
	if err != nil {
		t.Fatalf("expected cache:admin:mrr:USD to be set after first call: %v", err)
	}
	if cached != "10" {
		t.Errorf("cached MRR = %q, want \"10\"", cached)
	}
}

// TestServerDrainHidesFromPublic is the ADMIN-04 server-drain proof (SC-4).
//
// GREEN behavior (plan 07-06): after setting vpn_servers.is_draining=true, a
// non-admin GET /servers omits the draining server (repository.ListActiveServers
// applies an is_draining=false filter), while an admin still sees it.
//
// RED now: the is_draining=false filter / repository.ListActiveServers do not
// exist yet.
func TestServerDrainHidesFromPublic(t *testing.T) {
	t.Skip("RED: pending 07-06 — asserts repository.ListActiveServers applies " +
		"is_draining=false so a draining server is omitted from non-admin /servers")
}
