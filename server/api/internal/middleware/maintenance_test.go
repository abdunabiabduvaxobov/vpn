// GREEN for ADMIN-05/08 maintenance mode (plan 07-07). The 07-01 RED stub
// (t.Skip) is replaced with the real assertion now that middleware.Maintenance
// exists.
package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"vpnapp/server/api/internal/middleware"

	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// openMaintenanceTestDB opens an in-memory SQLite DB with a feature_flags table
// matching migration 024's shape, seeded with maintenance_mode at the given
// value. Redis is nil in these tests so cache.GetFlag falls through to this DB
// (the authoritative source of truth).
func openMaintenanceTestDB(t *testing.T, maintenanceOn bool) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	stmt := `CREATE TABLE IF NOT EXISTS feature_flags (
		key        TEXT PRIMARY KEY,
		value      INTEGER NOT NULL DEFAULT 0,
		updated_at DATETIME,
		updated_by TEXT
	)`
	if err := db.Exec(stmt).Error; err != nil {
		t.Fatalf("failed to create feature_flags table: %v", err)
	}
	v := 0
	if maintenanceOn {
		v = 1
	}
	if err := db.Exec(
		`INSERT INTO feature_flags (key, value, updated_at) VALUES ('maintenance_mode', ?, CURRENT_TIMESTAMP)`,
		v,
	).Error; err != nil {
		t.Fatalf("failed to seed maintenance_mode: %v", err)
	}
	return db
}

// newMaintenanceApp builds a Fiber app mounting middleware.Maintenance on the
// /api/v1 group with a catch-all 200 handler, mirroring production where the
// middleware runs before the routes resolve.
func newMaintenanceApp(db *gorm.DB) *fiber.App {
	app := fiber.New()
	api := app.Group("/api/v1")
	api.Use(middleware.Maintenance(db, nil, zap.NewNop()))
	api.All("/*", func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})
	return app
}

func doReq(t *testing.T, app *fiber.App, method, path string) int {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("%s %s: app.Test error: %v", method, path, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// TestMaintenanceMiddleware is the ADMIN-05 maintenance-mode proof (SC-6):
// when maintenance_mode is ON a non-admin request gets a friendly 503 +
// Retry-After while the operator escape hatch (admin routes + /auth/admin-login
// + livez/readyz) stays reachable; when OFF every path passes through.
func TestMaintenanceMiddleware(t *testing.T) {
	// (a) maintenance ON → a non-admin path gets 503 + Retry-After.
	t.Run("on_blocks_non_admin_with_503_and_retry_after", func(t *testing.T) {
		app := newMaintenanceApp(openMaintenanceTestDB(t, true))
		req := httptest.NewRequest(http.MethodGet, "/api/v1/servers", nil)
		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatalf("app.Test error: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("expected 503 for non-admin path under maintenance, got %d", resp.StatusCode)
		}
		if ra := resp.Header.Get("Retry-After"); ra == "" {
			t.Fatalf("expected a Retry-After header on the 503, got none")
		}
	})

	// (b) maintenance ON → an /api/v1/admin/ path gets through (operator escape hatch).
	t.Run("on_exempts_admin_routes", func(t *testing.T) {
		app := newMaintenanceApp(openMaintenanceTestDB(t, true))
		if code := doReq(t, app, http.MethodGet, "/api/v1/admin/feature-flags"); code != http.StatusOK {
			t.Fatalf("expected admin route to bypass maintenance (200), got %d", code)
		}
	})

	// (c) maintenance ON → POST /api/v1/auth/admin-login gets through.
	t.Run("on_exempts_admin_login", func(t *testing.T) {
		app := newMaintenanceApp(openMaintenanceTestDB(t, true))
		if code := doReq(t, app, http.MethodPost, "/api/v1/auth/admin-login"); code != http.StatusOK {
			t.Fatalf("expected /auth/admin-login to bypass maintenance (200), got %d", code)
		}
	})

	// (c2) maintenance ON → liveness probes get through.
	t.Run("on_exempts_liveness_probes", func(t *testing.T) {
		app := newMaintenanceApp(openMaintenanceTestDB(t, true))
		if code := doReq(t, app, http.MethodGet, "/api/v1/livez"); code != http.StatusOK {
			t.Fatalf("expected /livez to bypass maintenance (200), got %d", code)
		}
		if code := doReq(t, app, http.MethodGet, "/api/v1/readyz"); code != http.StatusOK {
			t.Fatalf("expected /readyz to bypass maintenance (200), got %d", code)
		}
	})

	// (d) maintenance OFF → all paths pass.
	t.Run("off_lets_all_paths_through", func(t *testing.T) {
		app := newMaintenanceApp(openMaintenanceTestDB(t, false))
		if code := doReq(t, app, http.MethodGet, "/api/v1/servers"); code != http.StatusOK {
			t.Fatalf("expected non-admin path to pass when maintenance off (200), got %d", code)
		}
		if code := doReq(t, app, http.MethodGet, "/api/v1/admin/feature-flags"); code != http.StatusOK {
			t.Fatalf("expected admin path to pass when maintenance off (200), got %d", code)
		}
	})
}
