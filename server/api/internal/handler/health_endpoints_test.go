// GREEN test for ADMIN-07 (readyz/livez health probes, plan 07-03).
//
// Package handler so it reuses the in-memory SQLite + miniredis setup pattern
// used across the handler tests (see admin_kpis_test.go).
package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"vpnapp/server/api/internal/lava"

	"github.com/alicebob/miniredis/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// setupHealthTestDB builds the minimum schema Readyz touches: vpn_servers with
// is_active + last_seen_at (migration 024 columns). Mirrors setupKPITestDB.
func setupHealthTestDB(t *testing.T) *gorm.DB {
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

	if err := db.Exec(`CREATE TABLE vpn_servers (
		id TEXT PRIMARY KEY,
		hostname TEXT,
		current_load INTEGER NOT NULL DEFAULT 0,
		is_active INTEGER NOT NULL DEFAULT 1,
		last_seen_at TIMESTAMP
	)`).Error; err != nil {
		t.Fatalf("create vpn_servers: %v", err)
	}
	return db
}

func newHealthRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return mr, rdb
}

// TestReadyzLivez is the ADMIN-07 health-probe proof (SC-5).
//
//   - Livez always returns 200 with zero I/O.
//   - Readyz returns 200 when DB + Redis + lava(cached) + a fresh tunnel
//     heartbeat are all healthy.
//   - Readyz returns 503 (per-dep status-word map, no leaked error strings)
//     when a dependency probe is forced unhealthy.
func TestReadyzLivez(t *testing.T) {
	// Lava reachability is read from a Redis cache (never a per-probe dial in
	// the happy path). A throwaway lava client suffices for the cached path.
	lavaClient := lava.NewForTest("test-key", "http://127.0.0.1:1")

	t.Run("livez always 200 zero-io", func(t *testing.T) {
		app := fiber.New()
		app.Get("/livez", Livez())
		resp, err := app.Test(httptest.NewRequest("GET", "/livez", nil))
		if err != nil {
			t.Fatalf("app.Test: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("livez status = %d, want 200", resp.StatusCode)
		}
	})

	t.Run("readyz 200 when all deps healthy", func(t *testing.T) {
		db := setupHealthTestDB(t)
		// Seed a fresh, active tunnel server so the freshness check passes.
		if err := db.Exec(
			`INSERT INTO vpn_servers (id, hostname, is_active, last_seen_at) VALUES (?, ?, 1, ?)`,
			"srv-1", "edge-1", time.Now(),
		).Error; err != nil {
			t.Fatalf("seed server: %v", err)
		}
		mr, rdb := newHealthRedis(t)
		// Pre-seed the lava reachability cache so readyz reads "ok" without a dial.
		mr.Set("cache:health:lava", "ok")

		app := fiber.New()
		app.Get("/readyz", Readyz(db, rdb, lavaClient))
		resp, err := app.Test(httptest.NewRequest("GET", "/readyz", nil))
		if err != nil {
			t.Fatalf("app.Test: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("readyz status = %d, want 200; body=%s", resp.StatusCode, body)
		}
	})

	t.Run("readyz 503 when redis down, no leaked errors", func(t *testing.T) {
		db := setupHealthTestDB(t)
		if err := db.Exec(
			`INSERT INTO vpn_servers (id, hostname, is_active, last_seen_at) VALUES (?, ?, 1, ?)`,
			"srv-1", "edge-1", time.Now(),
		).Error; err != nil {
			t.Fatalf("seed server: %v", err)
		}
		mr, rdb := newHealthRedis(t)
		mr.Set("cache:health:lava", "ok")
		// Force Redis down: close the miniredis server so Ping fails.
		mr.Close()

		app := fiber.New()
		app.Get("/readyz", Readyz(db, rdb, lavaClient))
		resp, err := app.Test(httptest.NewRequest("GET", "/readyz", nil), 5000)
		if err != nil {
			t.Fatalf("app.Test: %v", err)
		}
		if resp.StatusCode != fiber.StatusServiceUnavailable {
			t.Fatalf("readyz status = %d, want 503", resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		// The body must carry only per-dep status WORDS, never leaked detail.
		var payload struct {
			Data struct {
				Deps map[string]string `json:"deps"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("unmarshal body %q: %v", body, err)
		}
		if payload.Data.Deps["redis"] != "down" {
			t.Fatalf("deps.redis = %q, want \"down\"; body=%s", payload.Data.Deps["redis"], body)
		}
		// Every dep value must be exactly "ok" or "down" — no error strings.
		for dep, status := range payload.Data.Deps {
			if status != "ok" && status != "down" {
				t.Fatalf("dep %q leaked non-status value %q (body=%s)", dep, status, body)
			}
		}
	})

	t.Run("readyz 503 when tunnel heartbeat stale", func(t *testing.T) {
		db := setupHealthTestDB(t)
		// Stale heartbeat (well beyond the 90s freshness window) — no fresh server.
		if err := db.Exec(
			`INSERT INTO vpn_servers (id, hostname, is_active, last_seen_at) VALUES (?, ?, 1, ?)`,
			"srv-1", "edge-1", time.Now().Add(-10*time.Minute),
		).Error; err != nil {
			t.Fatalf("seed server: %v", err)
		}
		mr, rdb := newHealthRedis(t)
		mr.Set("cache:health:lava", "ok")

		app := fiber.New()
		app.Get("/readyz", Readyz(db, rdb, lavaClient))
		resp, err := app.Test(httptest.NewRequest("GET", "/readyz", nil), 5000)
		if err != nil {
			t.Fatalf("app.Test: %v", err)
		}
		if resp.StatusCode != fiber.StatusServiceUnavailable {
			t.Fatalf("readyz status = %d, want 503 (stale tunnel)", resp.StatusCode)
		}
	})

	// Guard: keep the unused context import meaningful for the compiler if the
	// implementation evolves; explicit no-op keeps the test self-documenting.
	_ = context.Background()
}
