package handler_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"vpnapp/server/api/internal/cache"
	"vpnapp/server/api/internal/config"
	"vpnapp/server/api/internal/handler"

	"github.com/alicebob/miniredis/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// Wave 0 RED-first test for the /servers cache-hit zero-SELECT path
// (PERF-01 / D-05, D-09 (b)). On a cache hit the handler must serve the cached
// `cache:servers:active` blob and emit ZERO `vpn_servers` SELECT against the DB.
//
// It references plan 04's not-yet-built wiring: handler.ListServersCached, which
// takes a *redis.Client in addition to (logger, db) and reads the cache before
// touching the DB. Until plan 04 lands that handler (and the admin server-write
// bust), this file fails to compile / fails at runtime — the intended RED state.

// recordingLogger is a gorm logger.Interface that captures every SQL statement
// executed so the test can assert on what hit the DB.
type recordingLogger struct {
	mu    sync.Mutex
	stmts []string
}

func (r *recordingLogger) LogMode(gormlogger.LogLevel) gormlogger.Interface { return r }
func (r *recordingLogger) Info(context.Context, string, ...interface{})     {}
func (r *recordingLogger) Warn(context.Context, string, ...interface{})     {}
func (r *recordingLogger) Error(context.Context, string, ...interface{})    {}
func (r *recordingLogger) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	sql, _ := fc()
	r.mu.Lock()
	r.stmts = append(r.stmts, sql)
	r.mu.Unlock()
}

// selectedServers reports whether any captured statement is a SELECT against
// the servers table (the amplifier D-05 eliminates on a cache hit).
func (r *recordingLogger) selectedServers() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.stmts {
		up := strings.ToUpper(s)
		if strings.Contains(up, "SELECT") &&
			(strings.Contains(s, "vpn_servers") || strings.Contains(s, "servers")) {
			return true
		}
	}
	return false
}

func newMiniRedisClient(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	return mr, redis.NewClient(&redis.Options{Addr: mr.Addr()})
}

// TestServersCacheNoSelect: with cache:servers:active primed, the cached
// handler path must serve the blob and run ZERO servers SELECT.
func TestServersCacheNoSelect(t *testing.T) {
	rec := &recordingLogger{}
	db := newHandlerTestDBWithLogger(t, rec)

	_, rc := newMiniRedisClient(t)
	ctx := context.Background()
	// Prime the cache with a non-empty blob so the handler hits the cache branch.
	if err := cache.SetServersCache(ctx, rc, `[{"id":"s1","hostname":"h1"}]`); err != nil {
		t.Fatalf("prime cache: %v", err)
	}

	app := fiber.New()
	// Admin role so the handler takes the unfiltered (full-list) cache path.
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user_id", "admin-1")
		c.Locals("role", "admin")
		return c.Next()
	})
	app.Get("/servers", handler.ListServersCached(zap.NewNop(), db, rc, &config.Config{JWTSecret: "test-secret"}))

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/servers", nil), -1)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}
	if rec.selectedServers() {
		t.Errorf("PERF-01: cache hit must emit ZERO servers SELECT; captured: %v", rec.stmts)
	}
}

// TestServersCacheBustOnAdminWrite (skeleton): an admin server-write must call
// BustServersCache so the next /servers request misses cache and re-reads the
// DB. RED until plan 04 wires the bust into the admin server-write handler.
func TestServersCacheBustOnAdminWrite(t *testing.T) {
	t.Skip("RED skeleton — wire when plan 04 adds BustServersCache to the admin server-write handler")

	_, rc := newMiniRedisClient(t)
	ctx := context.Background()
	if err := cache.SetServersCache(ctx, rc, `[{"id":"s1"}]`); err != nil {
		t.Fatalf("prime: %v", err)
	}
	// After an admin server create/update/delete the handler must have called
	// cache.BustServersCache(ctx, rc) — asserted here once the handler exists.
	got, _ := cache.GetServersCache(ctx, rc)
	if got != "" {
		t.Errorf("expected cache busted after admin server-write, got %q", got)
	}
}

// newHandlerTestDBWithLogger mirrors newHandlerTestDB but installs the supplied
// recording logger so SQL statements are captured. Reuses the shared schema DDL
// builder so this file stays in lock-step with the other handler tests.
func newHandlerTestDBWithLogger(t *testing.T, rec *recordingLogger) *gorm.DB {
	t.Helper()
	db := newHandlerTestDB(t)
	// Swap in the recording logger for the assertion window.
	db.Logger = rec
	// Seed one server so a (wrongly) cache-missing path would actually SELECT
	// rows — making a false-negative impossible.
	if err := db.Exec(`INSERT INTO vpn_servers
		(id, hostname, ip_address, region, city, country, country_code)
		VALUES ('s1','h1','10.0.0.1','eu','AMS','NL','NL')`).Error; err != nil {
		t.Fatalf("seed server: %v", err)
	}
	return db
}
