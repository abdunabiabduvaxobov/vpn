package handler_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"vpnapp/server/api/internal/cache"
	"vpnapp/server/api/internal/handler"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// newDepsHealthDB opens an in-memory SQLite DB with the vpn_servers columns the
// ADMIN-08 deps-health server list reads (id, hostname, is_active, current_load,
// last_seen_at — migration 024 shape).
func newDepsHealthDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	ddl := `
		CREATE TABLE IF NOT EXISTS vpn_servers (
			id           TEXT PRIMARY KEY,
			hostname     TEXT NOT NULL,
			is_active    INTEGER NOT NULL DEFAULT 1,
			current_load INTEGER NOT NULL DEFAULT 0,
			last_seen_at DATETIME
		);
	`
	if err := db.Exec(ddl).Error; err != nil {
		t.Fatalf("create vpn_servers: %v", err)
	}
	return db
}

func newDepsHealthRedis(t *testing.T) *redis.Client {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	return redis.NewClient(&redis.Options{Addr: mr.Addr()})
}

// TestDepsHealth proves the admin-only deps-health endpoint returns a detailed
// per-dependency status map plus a per-tunnel-server list with last_seen_at and
// a fresh flag, reusing the readyz probe + the Redis-cached lava reachability
// (no per-call dial). Detail is allowed because the route is admin-authed
// (contrast public /readyz, which returns status words only — T-07-09 / T-07-37).
func TestDepsHealth(t *testing.T) {
	t.Run("returns per-dep status + per-server freshness", func(t *testing.T) {
		db := newDepsHealthDB(t)
		rdb := newDepsHealthRedis(t)

		// Prime the cached lava reachability so checkLava reads the cache and
		// never performs a fresh dial (T-07-38). Pass a nil lava client to prove
		// the cached value alone drives the verdict.
		if err := cache.SetLavaReachable(t.Context(), rdb, true); err != nil {
			t.Fatalf("seed lava cache: %v", err)
		}

		// A fresh server (last_seen_at = now) and a stale one (2 minutes ago,
		// outside the 90s window).
		fresh := uuid.NewString()
		stale := uuid.NewString()
		now := time.Now().UTC()
		db.Exec(`INSERT INTO vpn_servers (id, hostname, is_active, current_load, last_seen_at) VALUES (?, ?, 1, 12, ?)`,
			fresh, "fresh.example.com", now)
		db.Exec(`INSERT INTO vpn_servers (id, hostname, is_active, current_load, last_seen_at) VALUES (?, ?, 1, 80, ?)`,
			stale, "stale.example.com", now.Add(-2*time.Minute))

		app := systemApp(http.MethodGet, "/admin/system/deps-health",
			handler.AdminDepsHealth(zap.NewNop(), db, rdb, nil))
		resp := doJSON(t, app, http.MethodGet, "/admin/system/deps-health", nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("deps-health: expected 200, got %d", resp.StatusCode)
		}

		var out struct {
			Data struct {
				Postgres      string `json:"postgres"`
				Redis         string `json:"redis"`
				Lava          string `json:"lava"`
				TunnelServers []struct {
					ID          string  `json:"id"`
					Hostname    string  `json:"hostname"`
					IsActive    bool    `json:"is_active"`
					CurrentLoad int     `json:"current_load"`
					LastSeenAt  *string `json:"last_seen_at"`
					Fresh       bool    `json:"fresh"`
				} `json:"tunnel_servers"`
			} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("decode: %v", err)
		}

		if out.Data.Postgres != "ok" {
			t.Errorf("postgres: expected ok, got %q", out.Data.Postgres)
		}
		if out.Data.Redis != "ok" {
			t.Errorf("redis: expected ok, got %q", out.Data.Redis)
		}
		if out.Data.Lava != "ok" {
			t.Errorf("lava: expected ok from cache, got %q", out.Data.Lava)
		}
		if len(out.Data.TunnelServers) != 2 {
			t.Fatalf("expected 2 tunnel servers, got %d", len(out.Data.TunnelServers))
		}

		byHost := map[string]bool{}
		for _, s := range out.Data.TunnelServers {
			byHost[s.Hostname] = s.Fresh
			if s.LastSeenAt == nil {
				t.Errorf("server %q: expected non-nil last_seen_at (admin detail)", s.Hostname)
			}
		}
		if !byHost["fresh.example.com"] {
			t.Errorf("recent server expected fresh=true, got false")
		}
		if byHost["stale.example.com"] {
			t.Errorf("stale server expected fresh=false, got true")
		}
	})

	t.Run("lava reads cached value, no dial when client nil", func(t *testing.T) {
		db := newDepsHealthDB(t)
		rdb := newDepsHealthRedis(t)

		// Cache says lava is down; with a nil client and no cache miss there is no
		// dial — the verdict must come straight from the cache.
		if err := cache.SetLavaReachable(t.Context(), rdb, false); err != nil {
			t.Fatalf("seed lava cache: %v", err)
		}

		app := systemApp(http.MethodGet, "/admin/system/deps-health",
			handler.AdminDepsHealth(zap.NewNop(), db, rdb, nil))
		resp := doJSON(t, app, http.MethodGet, "/admin/system/deps-health", nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("deps-health: expected 200 (admin endpoint never 503s), got %d", resp.StatusCode)
		}
		var out struct {
			Data struct {
				Lava string `json:"lava"`
			} `json:"data"`
		}
		json.NewDecoder(resp.Body).Decode(&out)
		if out.Data.Lava != "down" {
			t.Errorf("lava: expected down from cache, got %q", out.Data.Lava)
		}
	})
}
