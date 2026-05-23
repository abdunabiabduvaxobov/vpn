package handler_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"vpnapp/server/api/internal/config"
	"vpnapp/server/api/internal/handler"
	"vpnapp/server/api/internal/model"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// newServersTestDB opens an in-memory SQLite database for server handler tests.
// Contains the vpn_servers and connections tables (same schema as connection_test.go).
func newServersTestDB(t *testing.T) *gorm.DB {
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

// seedPlansForServerTests seeds free + premium + pro + ultimate rows
// (idempotent) AND inserts a users row for `userID` with plan_id matching
// `tier`. Returns the plan_id. Plan 03-04 requires plans + users.plan_id
// to be populated so GetServerConfig's resolveUserPlanID lookup succeeds.
//
// Limit values mirror the legacy hardcoded constants for parity with
// older assertions.
func seedPlansForServerTests(t *testing.T, db *gorm.DB, userID, tier string) string {
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
			t.Fatalf("seedPlansForServerTests(%s): %v", s.code, err)
		}
	}
	planID := "plan-" + tier
	// Idempotent INSERT for the user (tests share a DB in some paths).
	if err := db.Exec(
		`INSERT OR IGNORE INTO users (id, full_name, subscription_tier, role, plan_id)
		 VALUES (?, ?, ?, 'user', ?)`,
		userID, "tester", tier, planID,
	).Error; err != nil {
		t.Fatalf("seedPlansForServerTests user: %v", err)
	}
	if err := db.Exec(
		`UPDATE users SET plan_id = ?, subscription_tier = ? WHERE id = ?`,
		planID, tier, userID,
	).Error; err != nil {
		t.Fatalf("seedPlansForServerTests user update: %v", err)
	}
	return planID
}

// linkPlanToServer creates the (plan, server) row in plan_servers so
// IsServerAllowedForPlan returns true for non-admin GetServerConfig calls.
func linkPlanToServer(t *testing.T, db *gorm.DB, planID, serverID string) {
	t.Helper()
	if err := db.Exec(
		`INSERT OR IGNORE INTO plan_servers (plan_id, server_id) VALUES (?, ?)`,
		planID, serverID,
	).Error; err != nil {
		t.Fatalf("linkPlanToServer: %v", err)
	}
}

// buildServerConfigApp constructs a Fiber app with the GetServerConfig route.
// Default role is admin so existing GetServerConfig tests that don't seed
// plan_servers still pass (admin bypass per D-21). Use
// buildServerConfigAppWithRole when a test specifically needs to exercise the
// non-admin plan-checked branch.
//
// NOTE: callers must seed plans + the user row themselves via
// seedPlansForServerTests(t, db, userID, tier) so the in-memory DB has the
// users.plan_id and plans rows that resolveUserPlanID expects.
func buildServerConfigApp(db *gorm.DB, userID, tier string) *fiber.App {
	return buildServerConfigAppWithRole(db, userID, tier, "admin")
}

// buildServerConfigAppWithRole is identical to buildServerConfigApp but lets
// tests pin the role local — used by TestListServers_AdminBypass and any
// future test that needs to exercise the plan-checked branch.
func buildServerConfigAppWithRole(db *gorm.DB, userID, tier, role string) *fiber.App {
	log := zap.NewNop()
	cfg := &config.Config{TunnelVLESSUUID: "00000000-0000-0000-0000-000000000000"}
	app := fiber.New()

	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user_id", userID)
		c.Locals("tier", tier)
		c.Locals("role", role)
		return c.Next()
	})

	app.Get("/servers", handler.ListServers(log, db))
	app.Get("/servers/:id/config", handler.GetServerConfig(log, db, cfg))
	return app
}

// seedServer inserts a VPNServer with configurable fields.
func seedServer(t *testing.T, db *gorm.DB, srv *model.VPNServer) *model.VPNServer {
	t.Helper()
	if srv.ID == "" {
		srv.ID = uuid.NewString()
	}
	if err := db.Create(srv).Error; err != nil {
		t.Fatalf("seedServer: %v", err)
	}
	return srv
}

// getServerConfig fires GET /servers/:id/config and returns the decoded response.
func getServerConfig(t *testing.T, app *fiber.App, serverID string) (*http.Response, map[string]interface{}) {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/servers/"+serverID+"/config", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test failed: %v", err)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil && err != io.EOF {
		t.Fatalf("failed to decode response body: %v", err)
	}
	return resp, body
}

// --- GetServerConfig AWG tests ---

func TestGetServerConfig_NoAWG_OmitsAWGField(t *testing.T) {
	db := newServersTestDB(t)
	srv := seedServer(t, db, &model.VPNServer{
		Hostname:         "test-no-awg",
		IPAddress:        "10.0.0.1",
		Region:           "EU",
		City:             "Berlin",
		Country:          "Germany",
		CountryCode:      "DE",
		Protocol:         "vless-reality",
		IsActive:         true,
		RealityPublicKey: "reality-public-key",
		RealityShortID:   "abcd1234",
	})

	app := buildServerConfigApp(db, "user-123", "premium")
	resp, body := getServerConfig(t, app, srv.ID)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %v", resp.StatusCode, body)
	}

	data, ok := body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("response missing 'data' object")
	}

	// When the server has no AWG keys, the "awg" field must be absent (omitempty).
	if _, hasAWG := data["awg"]; hasAWG {
		t.Error("expected 'awg' to be absent when server has no AWG configuration")
	}
}

func TestGetServerConfig_WithAWG_IncludesAWGField(t *testing.T) {
	db := newServersTestDB(t)

	pubKey := "awg-public-key-base64=="
	endpoint := "10.0.0.1:51820"
	awgParamsJSON := `{"jc":5,"jmin":50,"jmax":1000,"s1":10,"s2":20,"h1":1,"h2":2,"h3":3,"h4":4}`

	// Insert via raw SQL to avoid GORM JSONB serializer issues in SQLite test env.
	if err := db.Exec(`
		INSERT INTO vpn_servers
			(id, hostname, ip_address, region, city, country, country_code,
			 protocol, is_active, reality_public_key, reality_short_id,
			 awg_public_key, awg_endpoint, awg_params)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		uuid.NewString(), "test-with-awg", "10.0.0.1",
		"EU", "Amsterdam", "Netherlands", "NL",
		"vless-reality", 1,
		"TestRealityPublicKey123456789012345678901234", "abcd1234",
		pubKey, endpoint, awgParamsJSON,
	).Error; err != nil {
		t.Fatalf("failed to seed AWG server: %v", err)
	}

	// Fetch the inserted server ID.
	var srvID string
	db.Raw("SELECT id FROM vpn_servers WHERE hostname = ?", "test-with-awg").Scan(&srvID)
	if srvID == "" {
		t.Fatal("could not retrieve seeded server ID")
	}

	app := buildServerConfigApp(db, "user-456", "premium")
	resp, body := getServerConfig(t, app, srvID)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %v", resp.StatusCode, body)
	}

	data, ok := body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("response missing 'data' object; body=%v", body)
	}

	awg, hasAWG := data["awg"]
	if !hasAWG {
		t.Fatal("expected 'awg' field in response when server has AWG configuration")
	}

	awgMap, ok := awg.(map[string]interface{})
	if !ok {
		t.Fatalf("'awg' field is not an object; got %T", awg)
	}

	if awgMap["public_key"] != pubKey {
		t.Errorf("expected public_key=%q, got %q", pubKey, awgMap["public_key"])
	}
	if awgMap["endpoint"] != endpoint {
		t.Errorf("expected endpoint=%q, got %q", endpoint, awgMap["endpoint"])
	}
	if awgMap["allowed_ips"] != "0.0.0.0/0, ::/0" {
		t.Errorf("expected allowed_ips='0.0.0.0/0, ::/0', got %q", awgMap["allowed_ips"])
	}
}

func TestGetServerConfig_WithAWG_AllowedIPsIsFullTunnel(t *testing.T) {
	db := newServersTestDB(t)

	pubKey := "awg-pk=="
	if err := db.Exec(`
		INSERT INTO vpn_servers
			(id, hostname, ip_address, region, city, country, country_code,
			 protocol, is_active, reality_public_key, reality_short_id,
			 awg_public_key, awg_endpoint)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		uuid.NewString(), "test-awg-ips", "1.2.3.4",
		"AS", "Tokyo", "Japan", "JP",
		"vless-reality", 1,
		"TestRealityPublicKey123456789012345678901234", "abcd1234",
		pubKey, "1.2.3.4:51820",
	).Error; err != nil {
		t.Fatalf("failed to seed: %v", err)
	}

	var srvID string
	db.Raw("SELECT id FROM vpn_servers WHERE hostname = ?", "test-awg-ips").Scan(&srvID)

	app := buildServerConfigApp(db, "user-789", "premium")
	resp, body := getServerConfig(t, app, srvID)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	data := body["data"].(map[string]interface{})
	awgMap := data["awg"].(map[string]interface{})

	// The client must route all traffic through the tunnel.
	allowedIPs, _ := awgMap["allowed_ips"].(string)
	if allowedIPs == "" {
		t.Error("allowed_ips must not be empty")
	}
}

func TestGetServerConfig_NotFound(t *testing.T) {
	db := newServersTestDB(t)
	app := buildServerConfigApp(db, "user-x", "premium")

	resp, _ := getServerConfig(t, app, uuid.NewString())
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestGetServerConfig_InactiveServer(t *testing.T) {
	db := newServersTestDB(t)

	// Insert via raw SQL with is_active=0 — GORM skips zero-value booleans on Create
	// so we bypass it here to reliably seed an inactive server in SQLite.
	srvID := uuid.NewString()
	if err := db.Exec(`
		INSERT INTO vpn_servers
			(id, hostname, ip_address, region, city, country, country_code, protocol, is_active)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		srvID, "inactive-srv", "10.0.0.1", "EU", "Paris", "France", "FR", "vless-reality",
	).Error; err != nil {
		t.Fatalf("failed to seed inactive server: %v", err)
	}

	app := buildServerConfigApp(db, "user-y", "premium")
	resp, _ := getServerConfig(t, app, srvID)

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for inactive server, got %d", resp.StatusCode)
	}
}

func TestGetServerConfig_DeviceLimitNotEnforcedAtConfigEndpoint(t *testing.T) {
	db := newServersTestDB(t)
	srv := seedServer(t, db, &model.VPNServer{
		Hostname:         "limit-test-srv",
		IPAddress:        "10.0.0.1",
		Region:           "EU",
		City:             "Madrid",
		Country:          "Spain",
		CountryCode:      "ES",
		Protocol:         "vless-reality",
		IsActive:         true,
		RealityPublicKey: "TestRealityPublicKey123456789012345678901234",
		RealityShortID:   "abcd1234",
	})

	userID := "user-at-limit"

	// Insert active connections exceeding the free tier limit (1 device).
	for i := 0; i < 3; i++ {
		db.Exec(`INSERT INTO connections (id, user_id, server_id, connected_at)
			VALUES (?, ?, ?, datetime('now'))`,
			uuid.NewString(), userID, srv.ID)
	}

	// Device limit is NOT enforced at the config endpoint — the client must be
	// able to fetch config even when over limit. Enforcement happens at POST /connections.
	app := buildServerConfigApp(db, userID, "free")
	resp, body := getServerConfig(t, app, srv.ID)

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 (config always served regardless of device count), got %d; body: %v", resp.StatusCode, body)
	}
}

// --- AWGClientConfig struct field tests ---

func TestAWGClientConfigHasAllRequiredFields(t *testing.T) {
	cfg := handler.AWGClientConfig{
		PublicKey:  "pk==",
		Endpoint:   "1.2.3.4:51820",
		AllowedIPs: "0.0.0.0/0, ::/0",
		Jc:         5,
		Jmin:       50,
		Jmax:       1000,
		S1:         10,
		S2:         20,
		H1:         1,
		H2:         2,
		H3:         3,
		H4:         4,
	}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("failed to marshal AWGClientConfig: %v", err)
	}

	var m map[string]interface{}
	json.Unmarshal(data, &m)

	required := []string{
		"public_key", "endpoint", "allowed_ips",
		"jc", "jmin", "jmax",
		"s1", "s2", "h1", "h2", "h3", "h4",
	}
	for _, field := range required {
		if _, ok := m[field]; !ok {
			t.Errorf("AWGClientConfig missing JSON field %q", field)
		}
	}
}

// --- ListServers role branching (PAY-11 / D-21) ---

// TestListServers_AdminBypass verifies that an admin sees ALL active servers
// regardless of their plan, even with a tight plan that has no plan_servers
// pairings. This is the named PAY-11 evidence test called out in
// 03-VALIDATION.md / 03-04-PLAN.md acceptance criteria. The admin bypass
// guards the operations panel from being inadvertently scoped down by a
// misconfigured admin plan_id.
func TestListServers_AdminBypass(t *testing.T) {
	db := newServersTestDB(t)
	// Seed two active servers — neither is paired to any plan.
	s1 := seedServer(t, db, &model.VPNServer{
		Hostname: "admin-bypass-a", IPAddress: "10.0.0.1",
		Region: "EU", City: "Berlin", Country: "Germany", CountryCode: "DE",
		Protocol: "vless-reality", IsActive: true,
	})
	s2 := seedServer(t, db, &model.VPNServer{
		Hostname: "admin-bypass-b", IPAddress: "10.0.0.2",
		Region: "EU", City: "Amsterdam", Country: "Netherlands", CountryCode: "NL",
		Protocol: "vless-reality", IsActive: true,
	})

	// Seed plans + an admin user with role=admin (no plan_servers pairings).
	seedPlansForServerTests(t, db, "admin-user", "free")

	app := buildServerConfigAppWithRole(db, "admin-user", "free", "admin")

	req := httptest.NewRequest(http.MethodGet, "/servers", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	data, ok := body["data"].([]interface{})
	if !ok {
		t.Fatalf("response missing 'data' array; body=%v", body)
	}
	// Admin must see BOTH active servers despite zero plan_servers pairings.
	if len(data) < 2 {
		t.Errorf("admin bypass: expected >= 2 servers (got %d). s1=%s s2=%s",
			len(data), s1.ID, s2.ID)
	}
}

// TestListServers_NonAdminScopedToPlan verifies the non-admin path: a user on
// a plan with ONE paired server only sees that one, even when more active
// servers exist. Companion to TestListServers_AdminBypass.
func TestListServers_NonAdminScopedToPlan(t *testing.T) {
	db := newServersTestDB(t)
	s1 := seedServer(t, db, &model.VPNServer{
		Hostname: "scoped-a", IPAddress: "10.0.0.1",
		Region: "EU", City: "Berlin", Country: "Germany", CountryCode: "DE",
		Protocol: "vless-reality", IsActive: true,
	})
	seedServer(t, db, &model.VPNServer{
		Hostname: "scoped-b", IPAddress: "10.0.0.2",
		Region: "EU", City: "Amsterdam", Country: "Netherlands", CountryCode: "NL",
		Protocol: "vless-reality", IsActive: true,
	})

	planID := seedPlansForServerTests(t, db, "regular-user", "free")
	linkPlanToServer(t, db, planID, s1.ID)

	app := buildServerConfigAppWithRole(db, "regular-user", "free", "user")

	req := httptest.NewRequest(http.MethodGet, "/servers", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	data, _ := body["data"].([]interface{})
	if len(data) != 1 {
		t.Errorf("non-admin scoped to plan: expected 1 server (only s1 paired), got %d", len(data))
	}
}

// TestGetServerConfig_NonAdminPlanDenied_Returns404 verifies D-22: a
// non-admin requesting a server NOT in their plan_servers gets 404 (not
// 403) so they can't enumerate paid-tier server UUIDs.
func TestGetServerConfig_NonAdminPlanDenied_Returns404(t *testing.T) {
	db := newServersTestDB(t)
	// Seed a real, active server BUT do NOT add it to plan_servers for our user.
	srv := seedServer(t, db, &model.VPNServer{
		Hostname: "paid-only", IPAddress: "10.0.0.1",
		Region: "EU", City: "Berlin", Country: "Germany", CountryCode: "DE",
		Protocol: "vless-reality", IsActive: true,
		RealityPublicKey: "TestRealityPublicKey123456789012345678901234", RealityShortID: "abcd1234",
	})

	seedPlansForServerTests(t, db, "free-user", "free")

	app := buildServerConfigAppWithRole(db, "free-user", "free", "user")
	resp, _ := getServerConfig(t, app, srv.ID)

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("D-22: expected 404 on plan-denied (no leak), got %d", resp.StatusCode)
	}
}
