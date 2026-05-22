package handler

// auth_test.go — edge-case tests for AdminLogin, RefreshToken, and GuestLogin handlers.
// These live in package handler (white-box) so they share the same test DB helpers
// already defined in payment_test.go (newTestDB, seedUser, etc.).

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"vpnapp/server/api/internal/config"

	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// authTestApp wraps a single handler in a minimal Fiber app used by auth tests.
func authTestApp(h fiber.Handler) *fiber.App {
	app := fiber.New()
	app.Post("/", h)
	return app
}

// newAuthTestDB opens an in-memory SQLite database with the users, subscriptions,
// and sessions tables needed by the auth handler tests.
func newAuthTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open auth test db: %v", err)
	}

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id                      TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
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
		)`,
		`CREATE TABLE IF NOT EXISTS subscriptions (
			id         TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
			user_id    TEXT NOT NULL,
			plan       TEXT NOT NULL DEFAULT 'free',
			stripe_id  TEXT,
			is_active  INTEGER NOT NULL DEFAULT 1,
			started_at DATETIME,
			expires_at DATETIME
		)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			id                  TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
			user_id             TEXT NOT NULL,
			refresh_token_hash  TEXT NOT NULL,
			device_info         TEXT,
			created_at          DATETIME,
			expires_at          DATETIME NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS devices (
			id                  TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
			user_id             TEXT NOT NULL,
			device_id           TEXT NOT NULL UNIQUE,
			device_secret_hash  TEXT,
			platform            TEXT NOT NULL DEFAULT '',
			model               TEXT NOT NULL DEFAULT '',
			first_seen_at       DATETIME,
			last_seen_at        DATETIME
		)`,
		`CREATE TABLE IF NOT EXISTS link_codes (
			code        TEXT PRIMARY KEY,
			user_id     TEXT NOT NULL,
			created_at  DATETIME,
			expires_at  DATETIME NOT NULL
		)`,
		// Partial unique indexes mirror the Postgres-side
		// migration 018 (AUTH-03 / CONTEXT.md D-09). SQLite 3.8+
		// supports `WHERE col IS NOT NULL` on indexes (RESEARCH.md
		// §Existing auth_test.go SQLite Pattern A5), so the
		// in-memory test schema enforces the same single-row-per-
		// provider-sub invariant as production.
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_apple_user_id ON users(apple_user_id) WHERE apple_user_id IS NOT NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_google_user_id ON users(google_user_id) WHERE google_user_id IS NOT NULL`,
	}
	for _, stmt := range stmts {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("failed to create auth test table: %v", err)
		}
	}
	return db
}

func doAuthRequest(t *testing.T, app *fiber.App, body interface{}) *http.Response {
	t.Helper()
	var reqBody *bytes.Buffer
	if body != nil {
		b, _ := json.Marshal(body)
		reqBody = bytes.NewBuffer(b)
	} else {
		reqBody = bytes.NewBuffer(nil)
	}
	req := httptest.NewRequest(http.MethodPost, "/", reqBody)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test error: %v", err)
	}
	return resp
}

func testAuthConfig() *config.Config {
	return &config.Config{JWTSecret: "test-secret-32-bytes-at-minimum!!"}
}

// seedAdminUser inserts an admin user directly into the users table and
// returns its email and the plaintext password for use in AdminLogin tests.
func seedAdminUser(t *testing.T, db *gorm.DB) (email, password string) {
	t.Helper()
	email = "admin@vpnapp.local"
	password = "admin-test-password-42"

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	emailHash := fmt.Sprintf("%x", sha256.Sum256([]byte(email)))

	if err := db.Exec(
		`INSERT INTO users (email_hash, password_hash, full_name, role, subscription_tier)
		 VALUES (?, ?, 'Admin', 'admin', 'ultimate')`,
		emailHash, string(hash),
	).Error; err != nil {
		t.Fatalf("seeding admin: %v", err)
	}
	return email, password
}

// seedRegularUser inserts a non-admin user for negative AdminLogin tests.
func seedRegularUser(t *testing.T, db *gorm.DB) (email, password string) {
	t.Helper()
	email = "regular@example.com"
	password = "regular-password-42"

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	emailHash := fmt.Sprintf("%x", sha256.Sum256([]byte(email)))

	if err := db.Exec(
		`INSERT INTO users (email_hash, password_hash, full_name, role, subscription_tier)
		 VALUES (?, ?, 'User', 'user', 'free')`,
		emailHash, string(hash),
	).Error; err != nil {
		t.Fatalf("seeding user: %v", err)
	}
	return email, password
}

// ---- AdminLogin ----

func TestAdminLogin_EmptyEmail_Returns400(t *testing.T) {
	db := newAuthTestDB(t)
	app := authTestApp(AdminLogin(zap.NewNop(), testAuthConfig(), db))
	resp := doAuthRequest(t, app, map[string]string{"email": "", "password": "any"})
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("empty email: expected 400, got %d", resp.StatusCode)
	}
}

func TestAdminLogin_NonAdminUser_Returns401(t *testing.T) {
	db := newAuthTestDB(t)
	email, password := seedRegularUser(t, db)

	app := authTestApp(AdminLogin(zap.NewNop(), testAuthConfig(), db))
	resp := doAuthRequest(t, app, map[string]string{"email": email, "password": password})
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("non-admin login: expected 401, got %d", resp.StatusCode)
	}
}

func TestAdminLogin_WrongPassword_Returns401(t *testing.T) {
	db := newAuthTestDB(t)
	email, _ := seedAdminUser(t, db)

	app := authTestApp(AdminLogin(zap.NewNop(), testAuthConfig(), db))
	resp := doAuthRequest(t, app, map[string]string{"email": email, "password": "wrong-password"})
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("wrong password: expected 401, got %d", resp.StatusCode)
	}
}

func TestAdminLogin_HappyPath_Returns200WithTokens(t *testing.T) {
	db := newAuthTestDB(t)
	email, password := seedAdminUser(t, db)

	app := authTestApp(AdminLogin(zap.NewNop(), testAuthConfig(), db))
	resp := doAuthRequest(t, app, map[string]string{"email": email, "password": password})
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("happy path: expected 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)

	data, ok := body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected 'data' object in login response")
	}
	if data["access_token"] == "" || data["access_token"] == nil {
		t.Error("expected non-empty access_token in login response")
	}
}

// ---- RefreshToken ----

func TestRefreshToken_MissingToken_Returns400(t *testing.T) {
	db := newAuthTestDB(t)
	app := authTestApp(RefreshToken(zap.NewNop(), testAuthConfig(), db))

	resp := doAuthRequest(t, app, map[string]string{})
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("missing refresh_token: expected 400, got %d", resp.StatusCode)
	}
}

func TestRefreshToken_MalformedToken_Returns401(t *testing.T) {
	db := newAuthTestDB(t)
	app := authTestApp(RefreshToken(zap.NewNop(), testAuthConfig(), db))

	resp := doAuthRequest(t, app, map[string]string{
		"refresh_token": "not-a-real-token-at-all",
	})
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("malformed token: expected 401, got %d", resp.StatusCode)
	}
}

// ---- GuestLogin ----

func TestGuestLogin_HappyPath_CreatesUserAndReturnsTokens(t *testing.T) {
	db := newAuthTestDB(t)
	app := authTestApp(GuestLogin(zap.NewNop(), db, testAuthConfig()))

	resp := doAuthRequest(t, app, nil)
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("guest login: expected 201, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	data, _ := body["data"].(map[string]interface{})
	if data["access_token"] == "" || data["access_token"] == nil {
		t.Error("expected non-empty access_token in guest login response")
	}

	var count int64
	db.Raw("SELECT COUNT(*) FROM users WHERE role = 'user'").Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 guest user row, got %d", count)
	}
}

// HOTFIX-05 regression tests — wrap refresh-token rotation in a single
// db.Transaction so a failed CreateSession after DeleteSession does not leave
// the user with no session row. See SECURITY-AUDIT S1-1 and 01-06-PLAN.md.

// seedRefreshSessionForUser inserts a user and a session row whose
// refresh_token_hash matches the SHA-256 of the returned plaintext refresh
// token. The plaintext token need not be a real JWT — the refresh handler
// only hashes the body field and looks up the session row by hash.
func seedRefreshSessionForUser(t *testing.T, db *gorm.DB) (userID, refreshToken string) {
	t.Helper()
	if err := db.Exec(
		`INSERT INTO users (full_name, role, subscription_tier) VALUES ('Refresh Test', 'user', 'free')`,
	).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := db.Raw(`SELECT id FROM users WHERE full_name = 'Refresh Test' LIMIT 1`).Row().Scan(&userID); err != nil {
		t.Fatalf("read seeded user id: %v", err)
	}

	refreshToken = "refresh-token-plaintext-for-rotation-test"
	tokenHash := fmt.Sprintf("%x", sha256.Sum256([]byte(refreshToken)))

	// expires_at must be in the future so FindSessionByTokenHash returns it.
	if err := db.Exec(
		`INSERT INTO sessions (user_id, refresh_token_hash, expires_at)
		 VALUES (?, ?, datetime('now', '+30 days'))`,
		userID, tokenHash,
	).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
	return userID, refreshToken
}

// countSessions returns the number of session rows for the given user.
func countSessions(t *testing.T, db *gorm.DB, userID string) int64 {
	t.Helper()
	var n int64
	if err := db.Raw(`SELECT COUNT(*) FROM sessions WHERE user_id = ?`, userID).Scan(&n).Error; err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	return n
}

// sessionHashFor returns the (single) refresh_token_hash currently stored for
// the given user, or "" if there is none. Used to confirm rotation actually
// replaced the row (vs. the same row sticking around).
func sessionHashFor(t *testing.T, db *gorm.DB, userID string) string {
	t.Helper()
	var hash string
	row := db.Raw(`SELECT refresh_token_hash FROM sessions WHERE user_id = ? LIMIT 1`, userID).Row()
	_ = row.Scan(&hash) // empty when no row
	return hash
}

// TestRefreshToken_RollbackOnInsertFailure proves that if the CreateSession
// call inside the rotation transaction fails, the prior DeleteSession is
// rolled back — the original session row stays in place and the user can
// refresh again later. Without the transaction wrap (the pre-HOTFIX-05 code
// path), the deletion would have committed and the user would be silently
// logged out.
func TestRefreshToken_RollbackOnInsertFailure(t *testing.T) {
	db := newAuthTestDB(t)
	userID, refreshToken := seedRefreshSessionForUser(t, db)
	originalHash := sessionHashFor(t, db, userID)

	// Inject a callback that fails on every INSERT into sessions. This
	// triggers inside the transaction at the storeRefreshSession step,
	// AFTER DeleteSession has already removed the original row within the
	// same tx. The transaction MUST roll back so the row reappears.
	cbName := "hotfix05_fail_session_insert"
	if err := db.Callback().Create().Before("gorm:create").Register(cbName, func(tx *gorm.DB) {
		if tx.Statement.Table == "sessions" {
			_ = tx.AddError(fmt.Errorf("forced insert failure for test"))
		}
	}); err != nil {
		t.Fatalf("register callback: %v", err)
	}
	defer func() { _ = db.Callback().Create().Remove(cbName) }()

	app := authTestApp(RefreshToken(zap.NewNop(), testAuthConfig(), db))
	resp := doAuthRequest(t, app, map[string]string{"refresh_token": refreshToken})
	if resp.StatusCode != fiber.StatusInternalServerError {
		t.Fatalf("expected 500 on insert failure, got %d", resp.StatusCode)
	}

	// THE proof: the original session row is still there because the
	// transaction rolled back. Pre-HOTFIX-05 code would leave count = 0.
	if got := countSessions(t, db, userID); got != 1 {
		t.Fatalf("expected 1 session row after rollback (original preserved), got %d", got)
	}
	if got := sessionHashFor(t, db, userID); got != originalHash {
		t.Fatalf("expected original session hash preserved after rollback; got %q want %q", got, originalHash)
	}
}

// TestRefreshToken_HappyPath confirms the rotation still works end-to-end:
// the old session row is gone, exactly one new session row exists, and its
// hash differs from the original (proving rotation occurred — not just a
// silent no-op).
func TestRefreshToken_HappyPath(t *testing.T) {
	db := newAuthTestDB(t)
	userID, refreshToken := seedRefreshSessionForUser(t, db)
	originalHash := sessionHashFor(t, db, userID)

	app := authTestApp(RefreshToken(zap.NewNop(), testAuthConfig(), db))
	resp := doAuthRequest(t, app, map[string]string{"refresh_token": refreshToken})
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 on happy path, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	data, ok := body["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected 'data' object in refresh response")
	}
	if data["access_token"] == "" || data["access_token"] == nil {
		t.Error("expected non-empty access_token in refresh response")
	}
	if data["refresh_token"] == "" || data["refresh_token"] == nil {
		t.Error("expected non-empty refresh_token in refresh response")
	}

	// Exactly one session row, and it must be the NEW one (different hash).
	if got := countSessions(t, db, userID); got != 1 {
		t.Fatalf("expected 1 session row after rotation, got %d", got)
	}
	if got := sessionHashFor(t, db, userID); got == originalHash {
		t.Fatal("session row did not rotate — hash unchanged after refresh")
	}
}

// TestRefreshToken_UserDeletedDuringRotation simulates the user row
// disappearing before the rotation completes (e.g. admin deletion racing a
// refresh). The transaction MUST roll back so the original session row is
// preserved, and the HTTP response MUST be 401 (not 500) so the client knows
// to re-authenticate rather than retry.
func TestRefreshToken_UserDeletedDuringRotation(t *testing.T) {
	db := newAuthTestDB(t)
	userID, refreshToken := seedRefreshSessionForUser(t, db)
	originalHash := sessionHashFor(t, db, userID)

	// Delete the user row before the refresh fires. Inside the transaction
	// the handler calls repository.FindUserByID(tx, ...) which returns
	// ErrNotFound. The closure returns the wrapped ErrNotFound; the outer
	// errors.Is branch must respond 401 and the tx must roll back so the
	// session row that the closure already deleted is restored.
	if err := db.Exec(`DELETE FROM users WHERE id = ?`, userID).Error; err != nil {
		t.Fatalf("delete user: %v", err)
	}

	app := authTestApp(RefreshToken(zap.NewNop(), testAuthConfig(), db))
	resp := doAuthRequest(t, app, map[string]string{"refresh_token": refreshToken})
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("expected 401 on user-deleted-mid-rotation, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if msg, _ := body["error"].(string); msg != "user not found" {
		t.Errorf("expected error 'user not found', got %q", msg)
	}

	// THE rollback proof: the original session row must still exist even
	// though DeleteSession ran inside the closure. Without the transaction
	// wrap this would be 0.
	if got := countSessions(t, db, userID); got != 1 {
		t.Fatalf("expected 1 session row after rollback (user-deleted path), got %d", got)
	}
	if got := sessionHashFor(t, db, userID); got != originalHash {
		t.Fatalf("expected original session hash preserved after rollback; got %q want %q", got, originalHash)
	}
}
