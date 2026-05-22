package middleware_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"vpnapp/server/api/internal/middleware"
	"vpnapp/server/api/internal/model"

	"github.com/gofiber/fiber/v2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// openAdminTestDB opens an in-memory SQLite database with the users table
// schema needed for the AdminRequired DB-lookup tests. Mirrors the pattern
// from internal/repository/subscription_repo_test.go.
//
// The `role` column carries the value AdminRequired reads on every request
// after HOTFIX-02. AutoMigrate is intentionally NOT used here so the schema
// stays explicit and matches the production users table shape.
func openAdminTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	stmt := `CREATE TABLE IF NOT EXISTS users (
		id                      TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
		email_hash              TEXT UNIQUE,
		password_hash           TEXT,
		full_name               TEXT NOT NULL DEFAULT '',
		subscription_tier       TEXT NOT NULL DEFAULT 'free',
		subscription_expires_at DATETIME,
		role                    TEXT NOT NULL DEFAULT 'user',
		telegram_user_id        INTEGER UNIQUE,
		telegram_linked_at      DATETIME,
		telegram_username       TEXT,
		telegram_first_name     TEXT,
		apple_user_id           TEXT,
		google_user_id          TEXT,
		email                   TEXT,
		email_verified          INTEGER NOT NULL DEFAULT 0,
		email_is_private_relay  INTEGER NOT NULL DEFAULT 0,
		auth_provider           TEXT NOT NULL DEFAULT 'guest',
		created_at              DATETIME,
		updated_at              DATETIME
	)`
	if err := db.Exec(stmt).Error; err != nil {
		t.Fatalf("failed to create users table: %v", err)
	}
	return db
}

// seedAdminUser inserts a user with role='admin' and returns it. Each
// row uses a per-test email_hash so the UNIQUE index never collides
// across parallel tests in the same package run.
func seedAdminUser(t *testing.T, db *gorm.DB) *model.User {
	t.Helper()
	emailHash := "admin-" + t.Name()
	passwordHash := "ph"
	user := &model.User{
		EmailHash:        &emailHash,
		PasswordHash:     &passwordHash,
		SubscriptionTier: "free",
		Role:             "admin",
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to seed admin user: %v", err)
	}
	return user
}

// newAdminApp builds a minimal Fiber app: a faux-auth middleware that
// injects c.Locals("user_id", userID) (mimicking what AuthRequired does
// in production), AdminRequired(db) under test, then a stub 200 handler.
//
// When userID == "" the faux-auth step does NOT call c.Locals("user_id"),
// so AdminRequired sees the empty-locals branch.
func newAdminApp(db *gorm.DB, userID string) *fiber.App {
	app := fiber.New()
	app.Get("/", func(c *fiber.Ctx) error {
		if userID != "" {
			c.Locals("user_id", userID)
		}
		return c.Next()
	}, middleware.AdminRequired(db), func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})
	return app
}

// ---- Existing behavior tests, updated for the new AdminRequired(db) signature ----

func TestAdminRequired_AllowsAdminRole(t *testing.T) {
	db := openAdminTestDB(t)
	user := seedAdminUser(t, db)
	app := newAdminApp(db, user.ID)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestAdminRequired_RejectsUserRole(t *testing.T) {
	db := openAdminTestDB(t)
	emailHash := "user-" + t.Name()
	passwordHash := "ph"
	user := &model.User{
		EmailHash:        &emailHash,
		PasswordHash:     &passwordHash,
		SubscriptionTier: "free",
		Role:             "user",
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
	app := newAdminApp(db, user.ID)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403, got %d", resp.StatusCode)
	}
}

func TestAdminRequired_RejectsArbitraryRole(t *testing.T) {
	db := openAdminTestDB(t)
	emailHash := "mod-" + t.Name()
	passwordHash := "ph"
	user := &model.User{
		EmailHash:        &emailHash,
		PasswordHash:     &passwordHash,
		SubscriptionTier: "free",
		Role:             "moderator",
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to seed moderator: %v", err)
	}
	app := newAdminApp(db, user.ID)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403, got %d", resp.StatusCode)
	}
}

func TestAdminRequired_ResponseBodyContainsErrorKey(t *testing.T) {
	db := openAdminTestDB(t)
	emailHash := "userbody-" + t.Name()
	passwordHash := "ph"
	user := &model.User{
		EmailHash:        &emailHash,
		PasswordHash:     &passwordHash,
		SubscriptionTier: "free",
		Role:             "user",
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
	app := newAdminApp(db, user.ID)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)
	if bodyStr == "" {
		t.Error("expected non-empty response body")
	}
	const want = "forbidden"
	found := false
	for i := 0; i <= len(bodyStr)-len(want); i++ {
		if bodyStr[i:i+len(want)] == want {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("response body %q does not contain %q", bodyStr, want)
	}
}

// ---- HOTFIX-02 regression tests ----

// TestAdminRequired_DemotionTakesEffect is THE whole point of HOTFIX-02:
// after `UPDATE users SET role='user'`, the very next admin request from
// the same JWT (here simulated by the same c.Locals("user_id") value)
// must return 403, not 200. Before HOTFIX-02 this test fails because
// AdminRequired read role from the JWT claim cached in c.Locals.
func TestAdminRequired_DemotionTakesEffect(t *testing.T) {
	db := openAdminTestDB(t)
	user := seedAdminUser(t, db)
	app := newAdminApp(db, user.ID)

	// First request: live row has role='admin' → 200.
	req1 := httptest.NewRequest(http.MethodGet, "/", nil)
	resp1, err := app.Test(req1)
	if err != nil {
		t.Fatalf("first request: %v", err)
	}
	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("first request: expected 200 for admin, got %d", resp1.StatusCode)
	}

	// Demote the user server-side. Same user_id, same Fiber app.
	if err := db.Model(&model.User{}).
		Where("id = ?", user.ID).
		Update("role", "user").Error; err != nil {
		t.Fatalf("failed to demote user: %v", err)
	}

	// Second request with the same locals → 403 because role is now 'user'.
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	resp2, err := app.Test(req2)
	if err != nil {
		t.Fatalf("second request: %v", err)
	}
	if resp2.StatusCode != http.StatusForbidden {
		t.Errorf("after demotion: expected 403, got %d (HOTFIX-02 regression — role was read from JWT claim, not DB)", resp2.StatusCode)
	}
}

// TestAdminRequired_DeletedUserDuringSession: an admin whose row is
// deleted between AuthRequired and AdminRequired must get 401 (so the
// client's refresh path fires), not 403.
func TestAdminRequired_DeletedUserDuringSession(t *testing.T) {
	db := openAdminTestDB(t)
	user := seedAdminUser(t, db)
	app := newAdminApp(db, user.ID)

	// First request: admin row exists → 200.
	req1 := httptest.NewRequest(http.MethodGet, "/", nil)
	resp1, err := app.Test(req1)
	if err != nil {
		t.Fatalf("first request: %v", err)
	}
	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", resp1.StatusCode)
	}

	// Delete the user row.
	if err := db.Delete(&model.User{}, "id = ?", user.ID).Error; err != nil {
		t.Fatalf("failed to delete user: %v", err)
	}

	// Second request: row gone → 401 (NOT 403). 401 is what triggers the
	// client's refresh path; 403 would leave the client stuck.
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	resp2, err := app.Test(req2)
	if err != nil {
		t.Fatalf("second request: %v", err)
	}
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Errorf("deleted user: expected 401, got %d", resp2.StatusCode)
	}
	body, _ := io.ReadAll(resp2.Body)
	if !contains(string(body), "user no longer exists") {
		t.Errorf("deleted user: body should mention 'user no longer exists', got %q", string(body))
	}
}

// TestAdminRequired_EmptyLocals: c.Locals("user_id") empty (AuthRequired
// did not run or token was malformed) → 401 with body {"error":"unauthorized"}.
func TestAdminRequired_EmptyLocals(t *testing.T) {
	db := openAdminTestDB(t)
	app := newAdminApp(db, "") // empty userID → faux-auth does NOT set locals

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("empty locals: expected 401, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !contains(string(body), "unauthorized") {
		t.Errorf("empty locals: body should contain 'unauthorized', got %q", string(body))
	}
}

// contains is a tiny substring helper to keep tests dependency-free.
func contains(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i <= len(haystack)-len(needle); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
