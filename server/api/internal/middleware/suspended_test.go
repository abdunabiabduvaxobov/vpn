// GREEN for ADMIN-02 per-user suspend enforcement (plan 07-04). Replaces the
// 07-01 RED stub: middleware.SuspendedRequired(db) now exists.
package middleware_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"vpnapp/server/api/internal/middleware"
	"vpnapp/server/api/internal/model"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// openSuspendedTestDB opens an in-memory SQLite users table including the
// suspended_at/suspended_reason columns (migration 024). Reuses the same
// explicit-schema convention as openAdminTestDB (admin_test.go) plus the two
// suspension columns this middleware reads.
func openSuspendedTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := openAdminTestDB(t) // base users table, package-shared helper
	// Add the two ADMIN-02 columns the production users table carries
	// (migration 024). openAdminTestDB's CREATE predates them.
	if err := db.Exec(`ALTER TABLE users ADD COLUMN suspended_at DATETIME`).Error; err != nil {
		t.Fatalf("failed to add suspended_at: %v", err)
	}
	if err := db.Exec(`ALTER TABLE users ADD COLUMN suspended_reason TEXT`).Error; err != nil {
		t.Fatalf("failed to add suspended_reason: %v", err)
	}
	return db
}

// newSuspendedApp builds a minimal Fiber app: a faux-auth step injecting
// c.Locals("user_id", userID) (mimicking AuthRequired), SuspendedRequired(db)
// under test, then a stub 200 handler. Empty userID skips the locals set so
// the unauthenticated branch is exercised.
func newSuspendedApp(db *gorm.DB, userID string) *fiber.App {
	app := fiber.New()
	app.Get("/", func(c *fiber.Ctx) error {
		if userID != "" {
			c.Locals("user_id", userID)
		}
		return c.Next()
	}, middleware.SuspendedRequired(db), func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})
	return app
}

func seedSuspendedTestUser(t *testing.T, db *gorm.DB, suspended bool) *model.User {
	t.Helper()
	emailHash := "susp-" + t.Name()
	passwordHash := "ph"
	user := &model.User{
		EmailHash:        &emailHash,
		PasswordHash:     &passwordHash,
		SubscriptionTier: "free",
		Role:             "user",
	}
	if suspended {
		now := time.Now()
		reason := "abuse"
		user.SuspendedAt = &now
		user.SuspendedReason = &reason
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
	return user
}

// TestSuspendedMiddleware proves a suspended user is blocked (ADMIN-02): a user
// whose suspended_at is set gets 403/account-suspended, while an un-suspended
// user passes through to the protected handler.
func TestSuspendedMiddleware(t *testing.T) {
	t.Run("suspended user gets 403", func(t *testing.T) {
		db := openSuspendedTestDB(t)
		user := seedSuspendedTestUser(t, db, true)
		app := newSuspendedApp(db, user.ID)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("suspended user: expected 403, got %d", resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		if !contains(string(body), "account suspended") {
			t.Errorf("suspended user: body should contain 'account suspended', got %q", string(body))
		}
	})

	t.Run("un-suspended user passes through", func(t *testing.T) {
		db := openSuspendedTestDB(t)
		user := seedSuspendedTestUser(t, db, false)
		app := newSuspendedApp(db, user.ID)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("un-suspended user: expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("missing user_id gets 401", func(t *testing.T) {
		db := openSuspendedTestDB(t)
		app := newSuspendedApp(db, "") // faux-auth does NOT set locals

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("missing user_id: expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("deleted user gets 401", func(t *testing.T) {
		db := openSuspendedTestDB(t)
		user := seedSuspendedTestUser(t, db, false)
		app := newSuspendedApp(db, user.ID)

		if err := db.Delete(&model.User{}, "id = ?", user.ID).Error; err != nil {
			t.Fatalf("failed to delete user: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("deleted user: expected 401, got %d", resp.StatusCode)
		}
	})
}
