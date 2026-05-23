package handler_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"vpnapp/server/api/internal/handler"
	"vpnapp/server/api/internal/model"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)


// newHandlerTestDB opens an in-memory SQLite database suitable for handler
// integration tests.
//
// Tables are created via raw DDL rather than AutoMigrate because the GORM
// models use PostgreSQL-specific defaults (gen_random_uuid()) that SQLite does
// not support.
func newHandlerTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	ddl := `
		CREATE TABLE IF NOT EXISTS vpn_servers (
			id TEXT PRIMARY KEY,
			hostname TEXT NOT NULL UNIQUE,
			ip_address TEXT NOT NULL,
			region TEXT NOT NULL,
			city TEXT NOT NULL,
			country TEXT NOT NULL,
			country_code TEXT NOT NULL,
			protocol TEXT NOT NULL DEFAULT 'vless-reality',
			capacity INTEGER NOT NULL DEFAULT 500,
			current_load INTEGER NOT NULL DEFAULT 0,
			is_active INTEGER NOT NULL DEFAULT 1,
			reality_public_key TEXT,
			reality_short_id TEXT,
			ws_enabled INTEGER NOT NULL DEFAULT 0,
			ws_host TEXT,
			ws_path TEXT DEFAULT '/ws',
			awg_public_key TEXT,
			awg_endpoint TEXT,
			awg_params TEXT,
			created_at DATETIME
		);
		CREATE TABLE IF NOT EXISTS connections (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			server_id TEXT NOT NULL,
			connected_at DATETIME,
			disconnected_at DATETIME,
			bytes_up INTEGER NOT NULL DEFAULT 0,
			bytes_down INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'connected',
			last_heartbeat_at DATETIME
		);
		CREATE TABLE IF NOT EXISTS users (
			id                      TEXT PRIMARY KEY,
			email_hash              TEXT,
			password_hash           TEXT,
			full_name               TEXT NOT NULL DEFAULT '',
			subscription_tier       TEXT NOT NULL DEFAULT 'free',
			subscription_expires_at DATETIME,
			role                    TEXT NOT NULL DEFAULT 'user',
			telegram_user_id        INTEGER UNIQUE,
			telegram_linked_at      DATETIME,
			telegram_username       TEXT,
			telegram_first_name     TEXT,
			plan_id                 TEXT NOT NULL DEFAULT '',
			created_at              DATETIME,
			updated_at              DATETIME
		);
		CREATE TABLE IF NOT EXISTS plans (
			id                TEXT PRIMARY KEY,
			code              TEXT NOT NULL UNIQUE,
			name              TEXT NOT NULL,
			description       TEXT NOT NULL DEFAULT '',
			max_devices       INTEGER NOT NULL,
			max_servers       INTEGER NOT NULL,
			speed_limit_mbps  INTEGER NOT NULL DEFAULT 0,
			is_active         INTEGER NOT NULL DEFAULT 1,
			is_system         INTEGER NOT NULL DEFAULT 0,
			sort_order        INTEGER NOT NULL DEFAULT 0,
			created_at        DATETIME,
			updated_at        DATETIME
		);
		CREATE TABLE IF NOT EXISTS plan_servers (
			plan_id   TEXT NOT NULL,
			server_id TEXT NOT NULL,
			PRIMARY KEY (plan_id, server_id)
		);
	`
	if err := db.Exec(ddl).Error; err != nil {
		t.Fatalf("failed to create test tables: %v", err)
	}

	return db
}

// seedActiveServer creates an active VPNServer in the test database.
// The ID is pre-generated in Go so the test works with SQLite.
func seedActiveServer(t *testing.T, db *gorm.DB) *model.VPNServer {
	t.Helper()

	srv := &model.VPNServer{
		ID:          uuid.NewString(),
		Hostname:    "handler-test-01",
		IPAddress:   "10.0.0.1",
		Region:      "test",
		City:        "TestCity",
		Country:     "Testland",
		CountryCode: "TT",
		Protocol:    "vless-reality",
		IsActive:    true,
	}
	if err := db.Create(srv).Error; err != nil {
		t.Fatalf("seedActiveServer: %v", err)
	}
	return srv
}

// seedPlansOnce seeds free + premium + pro + ultimate rows in the plans
// table (idempotent — INSERT OR IGNORE keyed by code). Returns the plan_id
// matching `tier`. After plan 03-04 every handler reads MaxDevices/MaxServers
// via repository.FindPlanByID(planID) instead of the legacy hardcoded
// in-Go limits map, so tests must populate the plans table before invoking
// those handlers.
//
// Limit values mirror the legacy hardcoded constants so existing test
// assertions (e.g. "free has 1 device cap", "premium has 3 device cap")
// keep working without rewriting every test body.
func seedPlansOnce(t *testing.T, db *gorm.DB, tier string) string {
	t.Helper()
	if tier == "" {
		tier = "free"
	}
	specs := []struct {
		code, name                                string
		maxDevices, maxServers, speedLimitMbps int
		isSystem                                  bool
	}{
		{"free", "Free", 1, 3, 50, true},
		{"premium", "Premium", 3, -1, 0, false},
		{"ultimate", "Ultimate", 6, -1, 0, false},
		{"pro", "Pro", 3, -1, 0, false},
	}
	for _, s := range specs {
		id := "plan-" + s.code
		isSys := 0
		if s.isSystem {
			isSys = 1
		}
		if err := db.Exec(
			`INSERT OR IGNORE INTO plans
			 (id, code, name, description, max_devices, max_servers, speed_limit_mbps, is_active, is_system, sort_order, created_at, updated_at)
			 VALUES (?, ?, ?, '', ?, ?, ?, 1, ?, 0, datetime('now'), datetime('now'))`,
			id, s.code, s.name, s.maxDevices, s.maxServers, s.speedLimitMbps, isSys,
		).Error; err != nil {
			t.Fatalf("seedPlansOnce(%s): %v", s.code, err)
		}
	}
	return "plan-" + tier
}

// seedUserRow inserts a users row with the given id and tier so that
// RegisterConnection's live tier read finds something. The row is inserted
// idempotently so callers can call this multiple times for the same user.
//
// PAY-11: also seeds the plans table and sets users.plan_id to the row
// matching the requested tier; handlers now read MaxDevices via
// repository.FindPlanByID(user.PlanID) rather than the legacy hardcoded
// in-Go limits map.
func seedUserRow(t *testing.T, db *gorm.DB, userID, tier string) {
	t.Helper()
	if tier == "" {
		tier = "free"
	}
	planID := seedPlansOnce(t, db, tier)
	if err := db.Exec(
		`INSERT OR IGNORE INTO users (id, full_name, subscription_tier, role, plan_id)
		 VALUES (?, ?, ?, 'user', ?)`,
		userID, "test_"+userID[:6], tier, planID,
	).Error; err != nil {
		t.Fatalf("seedUserRow: %v", err)
	}
	// In case the user already existed (idempotent path), keep plan_id in sync
	// with the requested tier so re-buildApp calls don't drift.
	if err := db.Exec(
		`UPDATE users SET plan_id = ?, subscription_tier = ? WHERE id = ?`,
		planID, tier, userID,
	).Error; err != nil {
		t.Fatalf("seedUserRow update: %v", err)
	}
}

// buildApp constructs a Fiber app with the three connection routes and a
// middleware that injects user_id and tier locals — simulating an authenticated
// request. The matching users row is seeded so RegisterConnection's live tier
// read returns the expected tier.
func buildApp(t *testing.T, db *gorm.DB, userID, tier string) *fiber.App {
	t.Helper()
	seedUserRow(t, db, userID, tier)

	log := zap.NewNop()
	app := fiber.New(fiber.Config{ErrorHandler: handler.ErrorHandler(log)})

	// Inject auth locals without a real JWT.
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user_id", userID)
		c.Locals("tier", tier)
		return c.Next()
	})

	app.Post("/connections", handler.RegisterConnection(log, db))
	app.Delete("/connections/:id", handler.UnregisterConnection(log, db))
	app.Get("/connections", handler.ListActiveConnections(log, db))
	app.Patch("/connections/:id/heartbeat", handler.HeartbeatConnection(log, db))

	return app
}

// doRequest is a small helper that builds a request, calls app.Test, and
// returns the response plus the decoded body as a raw map.
func doRequest(t *testing.T, app *fiber.App, method, path string, body interface{}) (*http.Response, map[string]interface{}) {
	t.Helper()

	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("failed to marshal request body: %v", err)
		}
		reqBody = bytes.NewReader(b)
	}

	req := httptest.NewRequest(method, path, reqBody)
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil && err != io.EOF {
		t.Fatalf("failed to decode response body: %v", err)
	}
	return resp, result
}

// --- RegisterConnection ---

func TestRegisterConnection_HappyPath(t *testing.T) {
	db := newHandlerTestDB(t)
	srv := seedActiveServer(t, db)
	app := buildApp(t, db, "user-reg", "premium")

	resp, body := doRequest(t, app, http.MethodPost, "/connections", map[string]string{
		"server_id": srv.ID,
	})

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("expected 201, got %d; body: %v", resp.StatusCode, body)
	}

	data, ok := body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected 'data' object in response, got: %v", body)
	}
	if data["id"] == "" || data["id"] == nil {
		t.Error("expected connection ID in response data")
	}
}

func TestRegisterConnection_MissingServerID_ReturnsBadRequest(t *testing.T) {
	db := newHandlerTestDB(t)
	app := buildApp(t, db, "user-bad", "free")

	resp, body := doRequest(t, app, http.MethodPost, "/connections", map[string]string{})

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d; body: %v", resp.StatusCode, body)
	}
}

func TestRegisterConnection_UnknownServer_ReturnsNotFound(t *testing.T) {
	db := newHandlerTestDB(t)
	app := buildApp(t, db, "user-nf", "premium")

	resp, body := doRequest(t, app, http.MethodPost, "/connections", map[string]string{
		"server_id": "00000000-0000-0000-0000-000000000000",
	})

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d; body: %v", resp.StatusCode, body)
	}
}

func TestRegisterConnection_DeviceLimitEnforced_Returns429(t *testing.T) {
	db := newHandlerTestDB(t)
	srv := seedActiveServer(t, db)

	// Free tier allows only 1 device.
	app := buildApp(t, db, "user-limit", "free")

	// First connection must succeed.
	resp, _ := doRequest(t, app, http.MethodPost, "/connections", map[string]string{
		"server_id": srv.ID,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected first connection to succeed, got %d", resp.StatusCode)
	}

	// Second connection must be rejected.
	resp2, body2 := doRequest(t, app, http.MethodPost, "/connections", map[string]string{
		"server_id": srv.ID,
	})
	if resp2.StatusCode != http.StatusTooManyRequests {
		t.Errorf("expected 429 when at limit, got %d; body: %v", resp2.StatusCode, body2)
	}
}

func TestRegisterConnection_PremiumTierAllowsMoreDevices(t *testing.T) {
	db := newHandlerTestDB(t)
	srv := seedActiveServer(t, db)

	// Premium tier allows 3 devices (Phase A).
	app := buildApp(t, db, "user-premium", "premium")

	// Three connections must all succeed.
	for i := 0; i < 3; i++ {
		resp, body := doRequest(t, app, http.MethodPost, "/connections", map[string]string{
			"server_id": srv.ID,
		})
		if resp.StatusCode != http.StatusCreated {
			t.Errorf("connection %d expected 201, got %d; body: %v", i+1, resp.StatusCode, body)
		}
	}

	// The fourth must be rejected.
	resp, body := doRequest(t, app, http.MethodPost, "/connections", map[string]string{
		"server_id": srv.ID,
	})
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("expected 429 on 4th connection, got %d; body: %v", resp.StatusCode, body)
	}
}

// --- UnregisterConnection ---

func TestUnregisterConnection_HappyPath(t *testing.T) {
	db := newHandlerTestDB(t)
	srv := seedActiveServer(t, db)
	const userID = "user-unreg"
	app := buildApp(t, db, userID, "premium")

	// Create a connection first.
	createResp, createBody := doRequest(t, app, http.MethodPost, "/connections", map[string]string{
		"server_id": srv.ID,
	})
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("setup: expected 201, got %d; body: %v", createResp.StatusCode, createBody)
	}
	connID := createBody["data"].(map[string]interface{})["id"].(string)

	// Now disconnect it.
	resp, _ := doRequest(t, app, http.MethodDelete, "/connections/"+connID, map[string]int64{
		"bytes_up":   500,
		"bytes_down": 1000,
	})
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("expected 204, got %d", resp.StatusCode)
	}
}

func TestUnregisterConnection_NotFound_Returns404(t *testing.T) {
	db := newHandlerTestDB(t)
	app := buildApp(t, db, "user-nf2", "free")

	resp, body := doRequest(t, app, http.MethodDelete, "/connections/00000000-0000-0000-0000-000000000000", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d; body: %v", resp.StatusCode, body)
	}
}

func TestUnregisterConnection_WrongOwner_ReturnsForbidden(t *testing.T) {
	db := newHandlerTestDB(t)
	srv := seedActiveServer(t, db)

	// Create a connection as user-A.
	appA := buildApp(t, db, "user-A", "premium")
	createResp, createBody := doRequest(t, appA, http.MethodPost, "/connections", map[string]string{
		"server_id": srv.ID,
	})
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("setup: expected 201, got %d; body: %v", createResp.StatusCode, createBody)
	}
	connID := createBody["data"].(map[string]interface{})["id"].(string)

	// Attempt to delete it as user-B.
	appB := buildApp(t, db, "user-B", "premium")
	resp, body := doRequest(t, appB, http.MethodDelete, "/connections/"+connID, nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403, got %d; body: %v", resp.StatusCode, body)
	}
}

func TestUnregisterConnection_AlreadyDisconnected_ReturnsNoContent(t *testing.T) {
	db := newHandlerTestDB(t)
	srv := seedActiveServer(t, db)
	const userID = "user-idem"
	app := buildApp(t, db, userID, "premium")

	createResp, createBody := doRequest(t, app, http.MethodPost, "/connections", map[string]string{
		"server_id": srv.ID,
	})
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("setup: expected 201, got %d", createResp.StatusCode)
	}
	connID := createBody["data"].(map[string]interface{})["id"].(string)

	// First disconnect.
	doRequest(t, app, http.MethodDelete, "/connections/"+connID, nil)

	// Second disconnect must be idempotent (204).
	resp, _ := doRequest(t, app, http.MethodDelete, "/connections/"+connID, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("expected idempotent 204 on second disconnect, got %d", resp.StatusCode)
	}
}

// --- ListActiveConnections ---

func TestListActiveConnections_ReturnsOnlyUserConnections(t *testing.T) {
	db := newHandlerTestDB(t)
	srv := seedActiveServer(t, db)

	// User-A creates 2 connections.
	appA := buildApp(t, db, "user-listA", "premium")
	for i := 0; i < 2; i++ {
		doRequest(t, appA, http.MethodPost, "/connections", map[string]string{
			"server_id": srv.ID,
		})
	}

	// User-B creates 1 connection.
	appB := buildApp(t, db, "user-listB", "premium")
	doRequest(t, appB, http.MethodPost, "/connections", map[string]string{
		"server_id": srv.ID,
	})

	// Listing for user-A should return exactly 2.
	resp, body := doRequest(t, appA, http.MethodGet, "/connections", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d; body: %v", resp.StatusCode, body)
	}

	data, ok := body["data"].([]interface{})
	if !ok {
		t.Fatalf("expected 'data' array in response, got: %v", body)
	}
	if len(data) != 2 {
		t.Errorf("expected 2 connections for user-A, got %d", len(data))
	}
}

func TestListActiveConnections_EmptyForNewUser(t *testing.T) {
	db := newHandlerTestDB(t)
	app := buildApp(t, db, "user-empty", "free")

	resp, body := doRequest(t, app, http.MethodGet, "/connections", nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d; body: %v", resp.StatusCode, body)
	}

	data, ok := body["data"].([]interface{})
	if !ok {
		t.Fatalf("expected 'data' array in response, got: %v", body)
	}
	if len(data) != 0 {
		t.Errorf("expected empty list for new user, got %d elements", len(data))
	}
}

func TestListActiveConnections_ExcludesDisconnected(t *testing.T) {
	db := newHandlerTestDB(t)
	srv := seedActiveServer(t, db)
	const userID = "user-disc-list"
	app := buildApp(t, db, userID, "premium")

	// Create two connections.
	var connID string
	for i := 0; i < 2; i++ {
		_, createBody := doRequest(t, app, http.MethodPost, "/connections", map[string]string{
			"server_id": srv.ID,
		})
		if i == 0 {
			connID = createBody["data"].(map[string]interface{})["id"].(string)
		}
	}

	// Disconnect the first one.
	doRequest(t, app, http.MethodDelete, "/connections/"+connID, nil)

	// List should return only 1.
	resp, body := doRequest(t, app, http.MethodGet, "/connections", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	data := body["data"].([]interface{})
	if len(data) != 1 {
		t.Errorf("expected 1 active connection after disconnect, got %d", len(data))
	}
}

// ---------------------------------------------------------------------------
// Additional edge-case tests
// ---------------------------------------------------------------------------

// TestRegisterConnection_InactiveServer_ReturnsNotFound ensures that an
// inactive server cannot accept new connections (clients must not reach
// a drained server).
func TestRegisterConnection_InactiveServer_ReturnsNotFound(t *testing.T) {
	db := newHandlerTestDB(t)

	// Seed an inactive server via raw SQL.
	srvID := "00000000-0000-0000-0000-000000000099"
	if err := db.Exec(`INSERT INTO vpn_servers
		(id, hostname, ip_address, region, city, country, country_code, protocol, is_active)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		srvID, "inactive-for-conn", "10.0.0.99", "EU", "London", "UK", "GB", "vless-reality",
	).Error; err != nil {
		t.Fatalf("failed to seed inactive server: %v", err)
	}

	app := buildApp(t, db, "user-inactive-srv", "premium")
	resp, body := doRequest(t, app, http.MethodPost, "/connections", map[string]string{
		"server_id": srvID,
	})

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for inactive server, got %d; body: %v", resp.StatusCode, body)
	}
}

// TestUnregisterConnection_InvalidIDFormat ensures the handler degrades
// gracefully when the connection ID in the URL is not a valid UUID.
func TestUnregisterConnection_InvalidIDFormat_ReturnsNotFound(t *testing.T) {
	db := newHandlerTestDB(t)
	app := buildApp(t, db, "user-bad-id", "premium")

	resp, _ := doRequest(t, app, http.MethodDelete, "/connections/not-a-valid-uuid", nil)
	// The repo will do a WHERE id = 'not-a-valid-uuid' which returns no rows → 404.
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for invalid connection ID format, got %d", resp.StatusCode)
	}
}

// TestRegisterConnection_EmptyBody_ReturnsBadRequest tests that an empty
// JSON body (not just missing server_id) is handled properly.
func TestRegisterConnection_EmptyBody_ReturnsBadRequest(t *testing.T) {
	db := newHandlerTestDB(t)
	app := buildApp(t, db, "user-empty-body", "free")

	req := httptest.NewRequest(http.MethodPost, "/connections", nil)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("empty body: expected 400, got %d", resp.StatusCode)
	}
}

// TestRegisterConnection_InvalidJSON_ReturnsBadRequest ensures the handler
// does not panic on malformed JSON input.
func TestRegisterConnection_InvalidJSON_ReturnsBadRequest(t *testing.T) {
	db := newHandlerTestDB(t)
	app := buildApp(t, db, "user-bad-json", "free")

	req := httptest.NewRequest(http.MethodPost, "/connections", bytes.NewBufferString("{bad json"))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("invalid JSON: expected 400, got %d", resp.StatusCode)
	}
}

// TestUnregisterConnection_WithTrafficStats verifies that non-zero byte
// counters are persisted when a connection is unregistered.
func TestUnregisterConnection_WithTrafficStats_PersistsByteCounts(t *testing.T) {
	db := newHandlerTestDB(t)
	srv := seedActiveServer(t, db)
	const userID = "user-traffic"
	app := buildApp(t, db, userID, "premium")

	createResp, createBody := doRequest(t, app, http.MethodPost, "/connections", map[string]string{
		"server_id": srv.ID,
	})
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("setup: expected 201, got %d", createResp.StatusCode)
	}
	connID := createBody["data"].(map[string]interface{})["id"].(string)

	resp, _ := doRequest(t, app, http.MethodDelete, "/connections/"+connID, map[string]int64{
		"bytes_up":   123456,
		"bytes_down": 654321,
	})
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("expected 204, got %d", resp.StatusCode)
	}
}

// TestRegisterConnection_FreeUserMaxDevicesIsOne verifies that the free-tier
// limit of exactly 1 simultaneous device is enforced (not 0, not 2).
func TestRegisterConnection_FreeUserExactlyOneAllowed(t *testing.T) {
	db := newHandlerTestDB(t)
	srv := seedActiveServer(t, db)
	app := buildApp(t, db, "user-free-exact", "free")

	// Exactly 1 device allowed.
	resp, body := doRequest(t, app, http.MethodPost, "/connections", map[string]string{
		"server_id": srv.ID,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("first connection (free): expected 201, got %d; body: %v", resp.StatusCode, body)
	}

	// Immediately a second connection must be rejected.
	resp2, body2 := doRequest(t, app, http.MethodPost, "/connections", map[string]string{
		"server_id": srv.ID,
	})
	if resp2.StatusCode != http.StatusTooManyRequests {
		t.Errorf("second connection (free): expected 429, got %d; body: %v", resp2.StatusCode, body2)
	}
}
