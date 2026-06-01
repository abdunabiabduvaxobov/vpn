// GREEN tests for ADMIN-04 server controls (plan 07-06): drain/undrain set
// is_draining + bust the servers cache, force-disconnect marks connections by
// server_id and is throttled ≤1/server/60s, and the per-server health endpoint
// reports concurrent_conns/last_seen_at/current_load.
//
// Package handler so it can reuse setupKPITestDB (in-memory SQLite + miniredis).
package handler

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
)

// decodeData decodes a {data:{...}} envelope into a generic map for assertions.
func decodeData(t *testing.T, body io.Reader) map[string]interface{} {
	t.Helper()
	var env struct {
		Data map[string]interface{} `json:"data"`
	}
	if err := json.NewDecoder(body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return env.Data
}

// TestServerDrainBustsCache proves drain sets is_draining=true AND deletes the
// cache:servers:active key so a non-admin /servers read drops the server within
// one request (T-07-24). undrain reverses both.
func TestServerDrainBustsCache(t *testing.T) {
	db, rdb := setupKPITestDB(t)

	if err := db.Exec(`INSERT INTO vpn_servers (id, hostname, is_active, is_draining) VALUES (?, 'srv-1', 1, 0)`, "srv-1").Error; err != nil {
		t.Fatalf("seed server: %v", err)
	}
	// Pre-seed the servers cache so we can prove the handler busts it.
	ctx := httptest.NewRequest("GET", "/", nil).Context()
	if err := rdb.Set(ctx, "cache:servers:active", `[{"id":"srv-1"}]`, time.Minute).Err(); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	app := fiber.New()
	app.Post("/admin/servers/:id/drain", AdminDrainServer(zap.NewNop(), db, rdb))
	app.Post("/admin/servers/:id/undrain", AdminUndrainServer(zap.NewNop(), db, rdb))

	// Drain (no force).
	resp, err := app.Test(httptest.NewRequest("POST", "/admin/servers/srv-1/drain", nil))
	if err != nil {
		t.Fatalf("drain request: %v", err)
	}
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("drain: expected 200, got %d body=%s", resp.StatusCode, string(b))
	}
	data := decodeData(t, resp.Body)
	if data["is_draining"] != true {
		t.Errorf("drain response is_draining = %v, want true", data["is_draining"])
	}

	// is_draining persisted in the DB.
	var draining bool
	if err := db.Raw(`SELECT is_draining FROM vpn_servers WHERE id = ?`, "srv-1").Scan(&draining).Error; err != nil {
		t.Fatalf("read is_draining: %v", err)
	}
	if !draining {
		t.Error("is_draining not persisted as true after drain")
	}

	// Cache key was busted (synchronous DEL within the request).
	if _, err := rdb.Get(ctx, "cache:servers:active").Result(); err == nil {
		t.Error("cache:servers:active still present after drain — expected it to be busted")
	}

	// Undrain clears the flag.
	resp, err = app.Test(httptest.NewRequest("POST", "/admin/servers/srv-1/undrain", nil))
	if err != nil {
		t.Fatalf("undrain request: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("undrain: expected 200, got %d", resp.StatusCode)
	}
	if err := db.Raw(`SELECT is_draining FROM vpn_servers WHERE id = ?`, "srv-1").Scan(&draining).Error; err != nil {
		t.Fatalf("read is_draining post-undrain: %v", err)
	}
	if draining {
		t.Error("is_draining still true after undrain")
	}
}

// TestServerDisconnectMarksByServerAndThrottles proves force-disconnect marks
// every live connection on the server (by server_id, Option-B) and that a second
// call within the 60s window returns 429 (T-07-23).
func TestServerDisconnectMarksByServerAndThrottles(t *testing.T) {
	db, rdb := setupKPITestDB(t)

	if err := db.Exec(`INSERT INTO vpn_servers (id, hostname, is_active) VALUES (?, 'srv-1', 1)`, "srv-1").Error; err != nil {
		t.Fatalf("seed server: %v", err)
	}
	// Two live connections on srv-1, one already-disconnected, and one on another
	// server that must NOT be touched.
	if err := db.Exec(`INSERT INTO connections (id, user_id, server_id, disconnected_at) VALUES ('c1','u1','srv-1',NULL)`).Error; err != nil {
		t.Fatalf("seed c1: %v", err)
	}
	if err := db.Exec(`INSERT INTO connections (id, user_id, server_id, disconnected_at) VALUES ('c2','u2','srv-1',NULL)`).Error; err != nil {
		t.Fatalf("seed c2: %v", err)
	}
	if err := db.Exec(`INSERT INTO connections (id, user_id, server_id, disconnected_at) VALUES ('c3','u3','srv-1',?)`, time.Now()).Error; err != nil {
		t.Fatalf("seed c3: %v", err)
	}
	if err := db.Exec(`INSERT INTO connections (id, user_id, server_id, disconnected_at) VALUES ('c4','u4','srv-other',NULL)`).Error; err != nil {
		t.Fatalf("seed c4: %v", err)
	}

	app := fiber.New()
	app.Post("/admin/servers/:id/disconnect", AdminDisconnectServer(zap.NewNop(), db, rdb))

	// First call: 200, kills the two live srv-1 connections.
	resp, err := app.Test(httptest.NewRequest("POST", "/admin/servers/srv-1/disconnect", nil))
	if err != nil {
		t.Fatalf("disconnect request: %v", err)
	}
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("disconnect: expected 200, got %d body=%s", resp.StatusCode, string(b))
	}
	data := decodeData(t, resp.Body)
	if got, _ := data["killed_count"].(float64); got != 2 {
		t.Errorf("killed_count = %v, want 2", data["killed_count"])
	}

	// The other server's connection is untouched.
	var otherLive int64
	if err := db.Raw(`SELECT COUNT(*) FROM connections WHERE server_id = 'srv-other' AND disconnected_at IS NULL`).Scan(&otherLive).Error; err != nil {
		t.Fatalf("count srv-other: %v", err)
	}
	if otherLive != 1 {
		t.Errorf("srv-other live connections = %d, want 1 (must not be disconnected)", otherLive)
	}

	// Second call within the window: 429 (throttled, T-07-23).
	resp, err = app.Test(httptest.NewRequest("POST", "/admin/servers/srv-1/disconnect", nil))
	if err != nil {
		t.Fatalf("second disconnect request: %v", err)
	}
	if resp.StatusCode != fiber.StatusTooManyRequests {
		t.Errorf("second disconnect within window: expected 429, got %d", resp.StatusCode)
	}
}

// TestServerHealthReportsSnapshot proves the per-server health endpoint returns
// concurrent_conns (by server_id), last_seen_at, and current_load.
func TestServerHealthReportsSnapshot(t *testing.T) {
	db, _ := setupKPITestDB(t)

	seen := time.Now().UTC().Add(-30 * time.Second).Truncate(time.Second)
	if err := db.Exec(`INSERT INTO vpn_servers (id, hostname, is_active, current_load, last_seen_at) VALUES (?, 'srv-1', 1, 42, ?)`, "srv-1", seen).Error; err != nil {
		t.Fatalf("seed server: %v", err)
	}
	if err := db.Exec(`INSERT INTO connections (id, user_id, server_id, disconnected_at) VALUES ('c1','u1','srv-1',NULL)`).Error; err != nil {
		t.Fatalf("seed c1: %v", err)
	}
	if err := db.Exec(`INSERT INTO connections (id, user_id, server_id, disconnected_at) VALUES ('c2','u2','srv-1',NULL)`).Error; err != nil {
		t.Fatalf("seed c2: %v", err)
	}

	app := fiber.New()
	app.Get("/admin/servers/:id/health", AdminServerHealth(zap.NewNop(), db))

	resp, err := app.Test(httptest.NewRequest("GET", "/admin/servers/srv-1/health", nil))
	if err != nil {
		t.Fatalf("health request: %v", err)
	}
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("health: expected 200, got %d body=%s", resp.StatusCode, string(b))
	}
	data := decodeData(t, resp.Body)
	if got, _ := data["concurrent_conns"].(float64); got != 2 {
		t.Errorf("concurrent_conns = %v, want 2", data["concurrent_conns"])
	}
	if got, _ := data["current_load"].(float64); got != 42 {
		t.Errorf("current_load = %v, want 42", data["current_load"])
	}
	ls, _ := data["last_seen_at"].(string)
	if ls == "" || !strings.HasPrefix(ls, seen.Format("2006-01-02")) {
		t.Errorf("last_seen_at = %q, want an RFC3339 timestamp near %v", ls, seen)
	}

	// 404 for an unknown server.
	resp, err = app.Test(httptest.NewRequest("GET", "/admin/servers/nope/health", nil))
	if err != nil {
		t.Fatalf("health 404 request: %v", err)
	}
	if resp.StatusCode != 404 {
		t.Errorf("health unknown server: expected 404, got %d", resp.StatusCode)
	}
}

// TestServerDrainForceDisconnects proves drain with {force:true} both sets
// is_draining and force-disconnects the server's live connections in one call.
func TestServerDrainForceDisconnects(t *testing.T) {
	db, rdb := setupKPITestDB(t)

	if err := db.Exec(`INSERT INTO vpn_servers (id, hostname, is_active, is_draining) VALUES (?, 'srv-1', 1, 0)`, "srv-1").Error; err != nil {
		t.Fatalf("seed server: %v", err)
	}
	if err := db.Exec(`INSERT INTO connections (id, user_id, server_id, disconnected_at) VALUES ('c1','u1','srv-1',NULL)`).Error; err != nil {
		t.Fatalf("seed c1: %v", err)
	}

	app := fiber.New()
	app.Post("/admin/servers/:id/drain", AdminDrainServer(zap.NewNop(), db, rdb))

	req := httptest.NewRequest("POST", "/admin/servers/srv-1/drain", strings.NewReader(`{"force":true}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("drain force request: %v", err)
	}
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("drain force: expected 200, got %d body=%s", resp.StatusCode, string(b))
	}
	data := decodeData(t, resp.Body)
	if data["is_draining"] != true {
		t.Errorf("is_draining = %v, want true", data["is_draining"])
	}
	if got, _ := data["killed_count"].(float64); got != 1 {
		t.Errorf("killed_count = %v, want 1", data["killed_count"])
	}

	// The live connection is now disconnected.
	var live int64
	if err := db.Raw(`SELECT COUNT(*) FROM connections WHERE server_id = 'srv-1' AND disconnected_at IS NULL`).Scan(&live).Error; err != nil {
		t.Fatalf("count live: %v", err)
	}
	if live != 0 {
		t.Errorf("srv-1 live connections = %d, want 0 after force drain", live)
	}
}
