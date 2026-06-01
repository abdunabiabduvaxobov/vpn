package handler_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"vpnapp/server/api/internal/handler"
	"vpnapp/server/api/internal/middleware"

	"github.com/alicebob/miniredis/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// newSystemDB opens an in-memory SQLite DB with the feature_flags,
// broadcast_messages, and audit_log tables (migration-024 / 014 shapes) the
// ADMIN-05 system-control handlers touch.
func newSystemDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	ddl := `
		CREATE TABLE IF NOT EXISTS feature_flags (
			key        TEXT PRIMARY KEY,
			value      INTEGER NOT NULL DEFAULT 0,
			updated_at DATETIME,
			updated_by TEXT
		);
		CREATE TABLE IF NOT EXISTS broadcast_messages (
			id          TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
			title       TEXT NOT NULL,
			body        TEXT NOT NULL,
			severity    TEXT NOT NULL DEFAULT 'info',
			locale      TEXT,
			target_tier TEXT,
			starts_at   DATETIME,
			ends_at     DATETIME,
			created_at  DATETIME
		);
		CREATE TABLE IF NOT EXISTS audit_log (
			id         TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
			admin_id   TEXT NOT NULL,
			action     TEXT NOT NULL,
			target_id  TEXT,
			details    TEXT,
			ip         TEXT,
			created_at DATETIME
		);
	`
	if err := db.Exec(ddl).Error; err != nil {
		t.Fatalf("create tables: %v", err)
	}
	// Seed the three flags like migration 024 does.
	for _, k := range []string{"signups_off", "payments_off", "maintenance_mode"} {
		db.Exec(`INSERT INTO feature_flags (key, value) VALUES (?, 0)`, k)
	}
	return db
}

func newSystemRedis(t *testing.T) *redis.Client {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	return redis.NewClient(&redis.Options{Addr: mr.Addr()})
}

// systemApp wires a system-control handler behind a faux-auth injector that
// sets c.Locals("user_id") so the explicit audit write has an actor.
func systemApp(method, path string, h fiber.Handler) *fiber.App {
	adminID := uuid.NewString()
	app := fiber.New(fiber.Config{
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		},
	})
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user_id", adminID)
		return c.Next()
	})
	app.Add(method, path, h)
	return app
}

func doJSON(t *testing.T, app *fiber.App, method, path string, body interface{}) *http.Response {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

func TestFeatureFlagControls(t *testing.T) {
	t.Run("set flag writes DB, busts cache, audits with reason", func(t *testing.T) {
		db := newSystemDB(t)
		rdb := newSystemRedis(t)

		// Prime the flag cache as if a prior read populated it (value 0).
		rdb.Set(t.Context(), "cache:flag:signups_off", "0", 0)

		app := systemApp(http.MethodPut, "/admin/feature-flags/:key",
			handler.AdminSetFeatureFlag(zap.NewNop(), db, rdb))
		resp := doJSON(t, app, http.MethodPut, "/admin/feature-flags/signups_off",
			map[string]interface{}{"value": true, "reason": "spam wave"})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("set flag: expected 200, got %d", resp.StatusCode)
		}

		// DB row updated to true (1).
		var v int
		db.Raw(`SELECT value FROM feature_flags WHERE key = 'signups_off'`).Scan(&v)
		if v != 1 {
			t.Errorf("expected signups_off=1 in DB, got %d", v)
		}
		// Cache key busted.
		if exists := rdb.Exists(t.Context(), "cache:flag:signups_off").Val(); exists != 0 {
			t.Errorf("expected flag cache key busted, still exists")
		}
		// Audit row written carrying the reason.
		var count int64
		db.Raw(`SELECT COUNT(*) FROM audit_log WHERE action = 'set_feature_flag' AND details LIKE '%spam wave%'`).Scan(&count)
		if count != 1 {
			t.Errorf("expected 1 set_feature_flag audit row with reason, got %d", count)
		}
	})

	t.Run("list returns seeded flags", func(t *testing.T) {
		db := newSystemDB(t)
		app := systemApp(http.MethodGet, "/admin/feature-flags",
			handler.AdminListFeatureFlags(zap.NewNop(), db))
		resp := doJSON(t, app, http.MethodGet, "/admin/feature-flags", nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("list flags: expected 200, got %d", resp.StatusCode)
		}
		var out struct {
			Data struct {
				Flags []map[string]interface{} `json:"flags"`
			} `json:"data"`
		}
		json.NewDecoder(resp.Body).Decode(&out)
		if len(out.Data.Flags) != 3 {
			t.Errorf("expected 3 seeded flags, got %d", len(out.Data.Flags))
		}
	})
}

func TestBroadcastControls(t *testing.T) {
	t.Run("create rejects invalid severity", func(t *testing.T) {
		db := newSystemDB(t)
		app := systemApp(http.MethodPost, "/admin/broadcasts",
			handler.AdminCreateBroadcast(zap.NewNop(), db))
		resp := doJSON(t, app, http.MethodPost, "/admin/broadcasts",
			map[string]string{"title": "T", "body": "B", "severity": "boom"})
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400 for invalid severity, got %d", resp.StatusCode)
		}
	})

	t.Run("create then public list returns only minimal fields", func(t *testing.T) {
		db := newSystemDB(t)
		createApp := systemApp(http.MethodPost, "/admin/broadcasts",
			handler.AdminCreateBroadcast(zap.NewNop(), db))
		resp := doJSON(t, createApp, http.MethodPost, "/admin/broadcasts",
			map[string]string{"title": "Maintenance soon", "body": "Heads up", "severity": "warning"})
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("create broadcast: expected 201, got %d", resp.StatusCode)
		}

		// Public endpoint returns the active row, minimal fields only.
		pubApp := fiber.New()
		pubApp.Get("/broadcasts", handler.ListBroadcastsPublic(zap.NewNop(), db))
		preq := httptest.NewRequest(http.MethodGet, "/broadcasts", nil)
		presp, err := pubApp.Test(preq, -1)
		if err != nil {
			t.Fatalf("public list: %v", err)
		}
		if presp.StatusCode != http.StatusOK {
			t.Fatalf("public list: expected 200, got %d", presp.StatusCode)
		}
		var out struct {
			Data struct {
				Broadcasts []map[string]interface{} `json:"broadcasts"`
			} `json:"data"`
		}
		json.NewDecoder(presp.Body).Decode(&out)
		if len(out.Data.Broadcasts) != 1 {
			t.Fatalf("expected 1 active broadcast, got %d", len(out.Data.Broadcasts))
		}
		b := out.Data.Broadcasts[0]
		if b["title"] != "Maintenance soon" || b["severity"] != "warning" {
			t.Errorf("unexpected broadcast payload: %v", b)
		}
		// T-07-29: no internal fields leak.
		for _, leak := range []string{"id", "target_tier", "locale", "created_at"} {
			if _, ok := b[leak]; ok {
				t.Errorf("public broadcast leaked internal field %q", leak)
			}
		}
	})

	t.Run("update missing broadcast 404s", func(t *testing.T) {
		db := newSystemDB(t)
		app := systemApp(http.MethodPatch, "/admin/broadcasts/:id",
			handler.AdminUpdateBroadcast(zap.NewNop(), db))
		resp := doJSON(t, app, http.MethodPatch, "/admin/broadcasts/"+uuid.NewString(),
			map[string]string{"title": "x"})
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 404 updating missing broadcast, got %d", resp.StatusCode)
		}
	})

	t.Run("delete missing broadcast 404s", func(t *testing.T) {
		db := newSystemDB(t)
		app := systemApp(http.MethodDelete, "/admin/broadcasts/:id",
			handler.AdminDeleteBroadcast(zap.NewNop(), db))
		resp := doJSON(t, app, http.MethodDelete, "/admin/broadcasts/"+uuid.NewString(), nil)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 404 deleting missing broadcast, got %d", resp.StatusCode)
		}
	})
}

// TestRequireFlagOffGuard proves the route-scoped flag guard 503s when the flag
// is ON and passes when OFF (signups_off / payments_off enforcement, ADMIN-05).
func TestRequireFlagOffGuard(t *testing.T) {
	db := newSystemDB(t)
	rdb := newSystemRedis(t)

	app := fiber.New()
	app.Post("/auth/guest",
		middleware.RequireFlagOff(db, rdb, zap.NewNop(), "signups_off", "signups are temporarily disabled"),
		func(c *fiber.Ctx) error { return c.SendStatus(http.StatusOK) },
	)

	// OFF → passes.
	if resp, _ := app.Test(httptest.NewRequest(http.MethodPost, "/auth/guest", nil), -1); resp.StatusCode != http.StatusOK {
		t.Fatalf("flag off: expected 200, got %d", resp.StatusCode)
	}

	// Flip ON in DB + bust cache.
	db.Exec(`UPDATE feature_flags SET value = 1 WHERE key = 'signups_off'`)
	rdb.Del(t.Context(), "cache:flag:signups_off")

	// ON → 503.
	if resp, _ := app.Test(httptest.NewRequest(http.MethodPost, "/auth/guest", nil), -1); resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("flag on: expected 503, got %d", resp.StatusCode)
	}
}
