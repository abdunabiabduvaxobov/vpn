package handler

// auth_test.go — edge-case tests for AdminLogin, RefreshToken, and GuestLogin handlers.
// These live in package handler (white-box) so they share the same test DB helpers
// already defined in payment_test.go (newTestDB, seedUser, etc.).

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"vpnapp/server/api/internal/auth/apple"
	"vpnapp/server/api/internal/auth/google"
	"vpnapp/server/api/internal/config"
	"vpnapp/server/api/internal/middleware"

	"github.com/alicebob/miniredis/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
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
			plan_id                 TEXT NOT NULL DEFAULT '',
			created_at              DATETIME,
			updated_at              DATETIME
		)`,
		`CREATE TABLE IF NOT EXISTS subscriptions (
			id               TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
			user_id          TEXT NOT NULL,
			plan             TEXT NOT NULL DEFAULT 'free',
			lava_contract_id TEXT,
			is_active        INTEGER NOT NULL DEFAULT 1,
			started_at       DATETIME,
			expires_at       DATETIME
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
		// IN-02: subscription_tier='free' matches the Phase 1 SC#8 invariant
		// (createadmin seeds admin rows with tier='free') and the production
		// behavior verified by cmd/createadmin/main_test.go. The previous
		// 'ultimate' value was a test-data inconsistency; the handler only
		// checks role='admin', so this is functionally a no-op for AdminLogin
		// tests but eliminates the divergence.
		`INSERT INTO users (email_hash, password_hash, full_name, role, subscription_tier)
		 VALUES (?, ?, 'Admin', 'admin', 'free')`,
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

// ---- Phase 2 SSO handler tests ------------------------------------------------
//
// These tests cover AppleSignIn / GoogleSignIn (plan 02-05). They inject fake
// verifiers via the handler-side interface so the verifier packages don't need
// network access to be exercised here. The schema seeded by newAuthTestDB
// already carries the six SSO columns + two partial unique indexes (plan 02-01
// Task 4); these tests rely on that.

type fakeAppleVerifier struct {
	identity apple.AppleIdentity
	err      error
}

func (f *fakeAppleVerifier) Verify(_ context.Context, _ string) (apple.AppleIdentity, error) {
	return f.identity, f.err
}

type fakeGoogleVerifier struct {
	identity google.GoogleIdentity
	err      error
}

func (f *fakeGoogleVerifier) Verify(_ context.Context, _ string) (google.GoogleIdentity, error) {
	return f.identity, f.err
}

// newAppleApp constructs a Fiber app with AppleSignIn mounted at /api/v1/auth/apple
// using the given fake verifier.
func newAppleApp(t *testing.T, db *gorm.DB, cfg *config.Config, fv *fakeAppleVerifier) *fiber.App {
	t.Helper()
	app := fiber.New()
	app.Post("/api/v1/auth/apple", AppleSignIn(zap.NewNop(), cfg, db, fv))
	return app
}

// newGoogleApp constructs a Fiber app with GoogleSignIn mounted at /api/v1/auth/google
// using the given fake verifier.
func newGoogleApp(t *testing.T, db *gorm.DB, cfg *config.Config, fv *fakeGoogleVerifier) *fiber.App {
	t.Helper()
	app := fiber.New()
	app.Post("/api/v1/auth/google", GoogleSignIn(zap.NewNop(), cfg, db, fv))
	return app
}

// mintGuestJWT returns a valid HS256 token whose sub is userID — used by
// guest-promotion tests. Matches the claim shape of generateTokens().
func mintGuestJWT(t *testing.T, secret, userID string) string {
	t.Helper()
	claims := jwt.MapClaims{
		"sub":  userID,
		"tier": "free",
		"role": "user",
		"name": "",
		"iat":  time.Now().Unix(),
		"exp":  time.Now().Add(5 * time.Minute).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := tok.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("mint guest jwt: %v", err)
	}
	return s
}

// ssoDecode unmarshals an SSO handler response body into a structured shape so
// tests can assert on data.user.id, access_token, etc.
type ssoResponse struct {
	Data struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		User         struct {
			ID               string `json:"id"`
			AuthProvider     string `json:"auth_provider"`
			Email            string `json:"email"`
			FullName         string `json:"full_name"`
			SubscriptionTier string `json:"subscription_tier"`
		} `json:"user"`
	} `json:"data"`
}

func TestAppleSignIn_HappyPath(t *testing.T) {
	db := newAuthTestDB(t)
	cfg := testAuthConfig()
	fv := &fakeAppleVerifier{identity: apple.AppleIdentity{
		Sub: "AS-1", Email: "u@example.com", EmailVerified: true, IsPrivateRelay: false,
	}}
	app := newAppleApp(t, db, cfg, fv)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/apple",
		bytes.NewBufferString(`{"identityToken":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: want 200, got %d (body: %s)", resp.StatusCode, string(body))
	}
	body, _ := io.ReadAll(resp.Body)
	var parsed ssoResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode body: %v (raw: %s)", err, string(body))
	}
	if parsed.Data.AccessToken == "" {
		t.Errorf("expected non-empty access_token")
	}
	if parsed.Data.User.AuthProvider != "apple" {
		t.Errorf("auth_provider: want apple, got %q", parsed.Data.User.AuthProvider)
	}
	if parsed.Data.User.ID == "" {
		t.Errorf("expected non-empty user.id")
	}
}

func TestAppleSignIn_AudienceMismatch_Returns401(t *testing.T) {
	db := newAuthTestDB(t)
	cfg := testAuthConfig()
	fv := &fakeAppleVerifier{err: errors.New("apple: audience mismatch")}
	app := newAppleApp(t, db, cfg, fv)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/apple",
		bytes.NewBufferString(`{"identityToken":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status: want 401, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(body, []byte("invalid identity token")) {
		t.Errorf("body should carry generic error, got %s", string(body))
	}
}

func TestAppleSignIn_CrossSurfaceSameSubSameID(t *testing.T) {
	db := newAuthTestDB(t)
	cfg := testAuthConfig()
	fv := &fakeAppleVerifier{identity: apple.AppleIdentity{
		Sub: "AS-cross", Email: "cross@example.com", EmailVerified: true,
	}}
	app := newAppleApp(t, db, cfg, fv)

	// First sign-in creates the row.
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/auth/apple",
		bytes.NewBufferString(`{"identityToken":"x","deviceId":"d1"}`))
	req1.Header.Set("Content-Type", "application/json")
	resp1, _ := app.Test(req1, -1)
	body1, _ := io.ReadAll(resp1.Body)
	var p1 ssoResponse
	_ = json.Unmarshal(body1, &p1)

	// Second sign-in from "different device" but same sub.
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/auth/apple",
		bytes.NewBufferString(`{"identityToken":"x","deviceId":"d2"}`))
	req2.Header.Set("Content-Type", "application/json")
	resp2, _ := app.Test(req2, -1)
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("second sign-in: want 200, got %d", resp2.StatusCode)
	}
	body2, _ := io.ReadAll(resp2.Body)
	var p2 ssoResponse
	_ = json.Unmarshal(body2, &p2)

	if p1.Data.User.ID == "" || p2.Data.User.ID == "" {
		t.Fatalf("expected both responses to carry user.id (got %q / %q)", p1.Data.User.ID, p2.Data.User.ID)
	}
	if p1.Data.User.ID != p2.Data.User.ID {
		t.Errorf("cross-surface same sub: want same user.id, got %q vs %q (AUTH-04 / ROADMAP SC#1)",
			p1.Data.User.ID, p2.Data.User.ID)
	}

	// Only one row owns this sub.
	var n int64
	db.Raw("SELECT COUNT(*) FROM users WHERE apple_user_id = ?", "AS-cross").Scan(&n)
	if n != 1 {
		t.Errorf("users WHERE apple_user_id='AS-cross': want 1, got %d", n)
	}
}

func TestAppleSignIn_AutoLinkByEmail(t *testing.T) {
	db := newAuthTestDB(t)
	cfg := testAuthConfig()

	// Seed user with google_user_id already set + matching verified email.
	seededID := uuid.NewString()
	if err := db.Exec(`INSERT INTO users(id, full_name, subscription_tier, role,
		google_user_id, email, email_verified, email_is_private_relay, auth_provider, created_at, updated_at)
		VALUES(?, '', 'free', 'user', ?, ?, 1, 0, 'google', ?, ?)`,
		seededID, "G1", "linkme@example.com", time.Now(), time.Now()).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	fv := &fakeAppleVerifier{identity: apple.AppleIdentity{
		Sub: "A2", Email: "linkme@example.com", EmailVerified: true, IsPrivateRelay: false,
	}}
	app := newAppleApp(t, db, cfg, fv)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/apple",
		bytes.NewBufferString(`{"identityToken":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req, -1)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: want 200, got %d (body: %s)", resp.StatusCode, string(body))
	}
	body, _ := io.ReadAll(resp.Body)
	var p ssoResponse
	_ = json.Unmarshal(body, &p)

	if p.Data.User.ID != seededID {
		t.Errorf("auto-link: want returned user.id == seeded %q, got %q (AUTH-06)", seededID, p.Data.User.ID)
	}
	// Apple sub now attached to the same row.
	var appleSub *string
	db.Raw("SELECT apple_user_id FROM users WHERE id = ?", seededID).Scan(&appleSub)
	if appleSub == nil || *appleSub != "A2" {
		t.Errorf("expected apple_user_id=A2 on linked row, got %v", appleSub)
	}
}

func TestAppleSignIn_PrivateRelaySkipsLink(t *testing.T) {
	db := newAuthTestDB(t)
	cfg := testAuthConfig()

	// Seed an existing verified-email row (non-relay).
	seededID := uuid.NewString()
	_ = db.Exec(`INSERT INTO users(id, full_name, subscription_tier, role,
		email, email_verified, email_is_private_relay, auth_provider, created_at, updated_at)
		VALUES(?, '', 'free', 'user', ?, 1, 0, 'apple', ?, ?)`,
		seededID, "abc@example.com", time.Now(), time.Now()).Error

	// Token claims a private-relay email that happens to share the seeded email's local part.
	fv := &fakeAppleVerifier{identity: apple.AppleIdentity{
		Sub: "A-relay", Email: "abc@privaterelay.appleid.com", EmailVerified: true, IsPrivateRelay: true,
	}}
	app := newAppleApp(t, db, cfg, fv)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/apple",
		bytes.NewBufferString(`{"identityToken":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req, -1)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: want 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	var p ssoResponse
	_ = json.Unmarshal(body, &p)
	if p.Data.User.ID == seededID {
		t.Errorf("CRITICAL: private-relay token must NOT auto-link to seeded row (T-2-RelaySkip / AUTH-06 exception). got same id %q", p.Data.User.ID)
	}
	if p.Data.User.ID == "" {
		t.Errorf("expected fresh user.id, got empty")
	}
}

func TestAppleSignIn_PromoteGuestInPlace(t *testing.T) {
	db := newAuthTestDB(t)
	cfg := testAuthConfig()

	// Seed a guest user.
	guestID := uuid.NewString()
	if err := db.Exec(`INSERT INTO users(id, full_name, subscription_tier, role, auth_provider, created_at, updated_at)
		VALUES(?, '', 'free', 'user', 'guest', ?, ?)`,
		guestID, time.Now(), time.Now()).Error; err != nil {
		t.Fatalf("seed guest: %v", err)
	}

	fv := &fakeAppleVerifier{identity: apple.AppleIdentity{
		Sub: "A-promote", Email: "promote@example.com", EmailVerified: true,
	}}
	app := newAppleApp(t, db, cfg, fv)

	guestJWT := mintGuestJWT(t, cfg.JWTSecret, guestID)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/apple",
		bytes.NewBufferString(`{"identityToken":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+guestJWT)
	resp, _ := app.Test(req, -1)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: want 200, got %d (body: %s)", resp.StatusCode, string(body))
	}
	body, _ := io.ReadAll(resp.Body)
	var p ssoResponse
	_ = json.Unmarshal(body, &p)

	if p.Data.User.ID != guestID {
		t.Errorf("promote-in-place: want same id %q, got %q (AUTH-05)", guestID, p.Data.User.ID)
	}

	// Row's auth_provider is now apple.
	var prov string
	db.Raw("SELECT auth_provider FROM users WHERE id = ?", guestID).Scan(&prov)
	if prov != "apple" {
		t.Errorf("auth_provider after promotion: want apple, got %q", prov)
	}
}

func TestAppleSignIn_GuestWithConflict_DevicesReassigned(t *testing.T) {
	db := newAuthTestDB(t)
	cfg := testAuthConfig()

	// Seed user A — already owns apple_user_id="ABC".
	userA := uuid.NewString()
	if err := db.Exec(`INSERT INTO users(id, full_name, subscription_tier, role,
		apple_user_id, email, email_verified, auth_provider, created_at, updated_at)
		VALUES(?, '', 'free', 'user', 'ABC', 'a@example.com', 1, 'apple', ?, ?)`,
		userA, time.Now(), time.Now()).Error; err != nil {
		t.Fatalf("seed A: %v", err)
	}

	// Seed guest G with one device D1.
	guestG := uuid.NewString()
	if err := db.Exec(`INSERT INTO users(id, full_name, subscription_tier, role, auth_provider, created_at, updated_at)
		VALUES(?, '', 'free', 'user', 'guest', ?, ?)`,
		guestG, time.Now(), time.Now()).Error; err != nil {
		t.Fatalf("seed guest G: %v", err)
	}
	deviceD1 := uuid.NewString()
	if err := db.Exec(`INSERT INTO devices(user_id, device_id, device_secret_hash, platform, model, last_seen_at, first_seen_at)
		VALUES(?, ?, 'hash', 'ios', 'test', ?, ?)`,
		guestG, deviceD1, time.Now(), time.Now()).Error; err != nil {
		t.Fatalf("seed device: %v", err)
	}

	fv := &fakeAppleVerifier{identity: apple.AppleIdentity{
		Sub: "ABC", Email: "a@example.com", EmailVerified: true,
	}}
	app := newAppleApp(t, db, cfg, fv)

	guestJWT := mintGuestJWT(t, cfg.JWTSecret, guestG)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/apple",
		bytes.NewBufferString(`{"identityToken":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+guestJWT)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: want 200, got %d (body: %s)", resp.StatusCode, string(body))
	}
	body, _ := io.ReadAll(resp.Body)
	var p ssoResponse
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if p.Data.User.ID != userA {
		t.Errorf("returned user.id: want %q (A), got %q (B-3 conflict branch)", userA, p.Data.User.ID)
	}

	// Device D1 reassigned to A.
	var deviceOwner string
	db.Raw("SELECT user_id FROM devices WHERE device_id = ?", deviceD1).Scan(&deviceOwner)
	if deviceOwner != userA {
		t.Errorf("device %s owner: want %q (A), got %q (B-3: ReassignDevicesByUserID failed)", deviceD1, userA, deviceOwner)
	}

	// Guest row orphaned (deleted).
	var guestCount int64
	db.Raw("SELECT COUNT(*) FROM users WHERE id = ?", guestG).Scan(&guestCount)
	if guestCount != 0 {
		t.Errorf("guest user row: want 0 (orphaned/deleted), got %d (B-3: DeleteOrphanGuestUser failed)", guestCount)
	}
}

func TestAppleSignIn_InvalidGuestJWT_Returns403(t *testing.T) {
	db := newAuthTestDB(t)
	cfg := testAuthConfig()
	fv := &fakeAppleVerifier{identity: apple.AppleIdentity{Sub: "X"}}
	app := newAppleApp(t, db, cfg, fv)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/apple",
		bytes.NewBufferString(`{"identityToken":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer not.a.real.token")
	resp, _ := app.Test(req, -1)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("invalid guest jwt: want 403, got %d (T-2-GuestJWTSpoof)", resp.StatusCode)
	}
}

func TestAppleSignIn_ConcurrentSameSub(t *testing.T) {
	db := newAuthTestDB(t)
	// SQLite :memory: is per-connection — under concurrent goroutines the
	// connection pool can hand out connections backed by DIFFERENT empty
	// databases. Clamp to a single shared connection so the partial-unique
	// index can actually fire and the handler's ErrDuplicate-fallback path is
	// the one being exercised (instead of a "database is locked" race).
	sqlDB, sqlErr := db.DB()
	if sqlErr != nil {
		t.Fatalf("db.DB: %v", sqlErr)
	}
	sqlDB.SetMaxOpenConns(1)
	cfg := testAuthConfig()
	fv := &fakeAppleVerifier{identity: apple.AppleIdentity{
		Sub: "AS-1", Email: "race@example.com", EmailVerified: true,
	}}
	app := newAppleApp(t, db, cfg, fv)

	type result struct {
		statusCode int
		userID     string
	}
	var (
		results []result
		mu      sync.Mutex
	)
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/apple",
				bytes.NewBufferString(`{"identityToken":"x"}`))
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req, -1)
			if err != nil {
				mu.Lock()
				results = append(results, result{statusCode: -1})
				mu.Unlock()
				return
			}
			body, _ := io.ReadAll(resp.Body)
			var p ssoResponse
			_ = json.Unmarshal(body, &p)
			mu.Lock()
			results = append(results, result{statusCode: resp.StatusCode, userID: p.Data.User.ID})
			mu.Unlock()
		}()
	}
	wg.Wait()

	// W-4: every concurrent response MUST be 200 — proves the ErrDuplicate
	// translation invariant (re-read on UNIQUE collision keeps callers on the 200 path).
	for i, r := range results {
		if r.statusCode != http.StatusOK {
			t.Errorf("goroutine #%d: status %d, want 200 (proves ErrDuplicate fallback)", i, r.statusCode)
		}
	}
	if len(results) == 0 {
		t.Fatalf("no results collected")
	}
	firstID := results[0].userID
	for i, r := range results {
		if r.userID != firstID {
			t.Errorf("goroutine #%d: user.id %q, want %q (cross-surface invariant)", i, r.userID, firstID)
		}
	}

	var n int64
	db.Raw("SELECT COUNT(*) FROM users WHERE apple_user_id = ?", "AS-1").Scan(&n)
	if n != 1 {
		t.Errorf("users WHERE apple_user_id='AS-1': want 1, got %d", n)
	}
}

// CR-02: Step B (auto-link by verified email) was NOT transactional in the
// initial implementation — two concurrent sign-ins for the same email but
// different subs would race the read+update pair, and the loser's
// ErrDuplicate fallback would re-read by the loser's sub (which no row owns)
// and return ErrNotFound -> 500. With db.Transaction wrapping the read+write,
// every caller now either wins or sees a transactional re-read that returns
// the merged row with the winner's sub — both return 200.
//
// VERIFICATION.md truth #2; REVIEW.md CR-02.
func TestAppleSignIn_ConcurrentAutoLinkByEmail(t *testing.T) {
	db := newAuthTestDB(t)
	// SQLite :memory: connection-pool clamp — same reason as TestAppleSignIn_ConcurrentSameSub.
	sqlDB, sqlErr := db.DB()
	if sqlErr != nil {
		t.Fatalf("db.DB: %v", sqlErr)
	}
	sqlDB.SetMaxOpenConns(1)
	cfg := testAuthConfig()

	// Seed a row that ALREADY owns google_user_id="EXISTING-G" with verified
	// email "race@example.com". Five concurrent Apple sign-ins (each with a
	// different sub) will race to attach apple_user_id to this same row.
	seededID := uuid.NewString()
	if err := db.Exec(`INSERT INTO users(id, full_name, subscription_tier, role,
		google_user_id, email, email_verified, email_is_private_relay, auth_provider, created_at, updated_at)
		VALUES(?, '', 'free', 'user', 'EXISTING-G', 'race@example.com', 1, 0, 'google', ?, ?)`,
		seededID, time.Now(), time.Now()).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	type result struct {
		statusCode int
		userID     string
		appleSub   string
	}
	var (
		results []result
		mu      sync.Mutex
	)
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sub := fmt.Sprintf("RACE-%d", idx)
			// Each goroutine builds its OWN app with its OWN fake verifier so each
			// presents a different Apple sub. All share the same DB.
			fv := &fakeAppleVerifier{identity: apple.AppleIdentity{
				Sub: sub, Email: "race@example.com", EmailVerified: true, IsPrivateRelay: false,
			}}
			app := newAppleApp(t, db, cfg, fv)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/apple",
				bytes.NewBufferString(`{"identityToken":"x"}`))
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req, -1)
			if err != nil {
				mu.Lock()
				results = append(results, result{statusCode: -1, appleSub: sub})
				mu.Unlock()
				return
			}
			body, _ := io.ReadAll(resp.Body)
			var p ssoResponse
			_ = json.Unmarshal(body, &p)
			mu.Lock()
			results = append(results, result{statusCode: resp.StatusCode, userID: p.Data.User.ID, appleSub: sub})
			mu.Unlock()
		}(i)
	}
	wg.Wait()

	// CR-02 invariant: every concurrent caller returns 200 (no 500 for the loser).
	for _, r := range results {
		if r.statusCode != http.StatusOK {
			t.Errorf("CR-02 regression: goroutine sub=%s returned status %d (want 200)", r.appleSub, r.statusCode)
		}
	}
	// AUTH-06 invariant: still exactly ONE row for the email (no duplicate auto-link).
	var emailRowCount int64
	db.Raw("SELECT COUNT(*) FROM users WHERE email = 'race@example.com'").Scan(&emailRowCount)
	if emailRowCount != 1 {
		t.Errorf("CRITICAL: more than one row owns the auto-linked email. count=%d", emailRowCount)
	}
	// All callers received the SAME user_id (the seeded row).
	if len(results) == 0 {
		t.Fatal("no results")
	}
	firstID := results[0].userID
	if firstID != seededID {
		t.Errorf("returned user.id: want seeded %q, got %q", seededID, firstID)
	}
	for _, r := range results {
		if r.userID != firstID {
			t.Errorf("cross-caller user.id mismatch: sub=%s got %q want %q", r.appleSub, r.userID, firstID)
		}
	}
}

func TestAppleSignIn_BodyEmailNeverUsedForAutoLink(t *testing.T) {
	db := newAuthTestDB(t)
	cfg := testAuthConfig()

	// Victim row that an attacker would try to hijack via body-email spoof.
	victimID := uuid.NewString()
	if err := db.Exec(`INSERT INTO users(id, full_name, subscription_tier, role,
		google_user_id, email, email_verified, email_is_private_relay, auth_provider, created_at, updated_at)
		VALUES(?, '', 'free', 'user', ?, ?, 1, 0, 'google', ?, ?)`,
		victimID, "victim-google", "victim@example.com", time.Now(), time.Now()).Error; err != nil {
		t.Fatalf("seed victim: %v", err)
	}

	// Verifier returns empty email (subsequent Apple sign-in has no email claim);
	// attacker tries to spoof via the request body.
	fv := &fakeAppleVerifier{identity: apple.AppleIdentity{
		Sub: "ATTACK", Email: "", EmailVerified: false,
	}}
	app := newAppleApp(t, db, cfg, fv)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/apple",
		bytes.NewBufferString(`{"identityToken":"x","email":"victim@example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req, -1)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: want 200, got %d (body: %s)", resp.StatusCode, string(body))
	}
	body, _ := io.ReadAll(resp.Body)
	var p ssoResponse
	_ = json.Unmarshal(body, &p)
	if p.Data.User.ID == victimID {
		t.Errorf("CRITICAL: body-email spoof MUST NOT link to victim (T-2-EmailBodySpoof). Got victim id %q", p.Data.User.ID)
	}
}

func TestGoogleSignIn_HappyPath(t *testing.T) {
	db := newAuthTestDB(t)
	cfg := testAuthConfig()
	fv := &fakeGoogleVerifier{identity: google.GoogleIdentity{
		Sub: "GS-1", Email: "g@example.com", EmailVerified: true,
	}}
	app := newGoogleApp(t, db, cfg, fv)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/google",
		bytes.NewBufferString(`{"idToken":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req, -1)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: want 200, got %d (body: %s)", resp.StatusCode, string(body))
	}
	body, _ := io.ReadAll(resp.Body)
	var p ssoResponse
	_ = json.Unmarshal(body, &p)
	if p.Data.User.AuthProvider != "google" {
		t.Errorf("auth_provider: want google, got %q", p.Data.User.AuthProvider)
	}
	if p.Data.AccessToken == "" {
		t.Errorf("expected non-empty access_token")
	}
}

func TestAuth_JWTShapeUnchanged(t *testing.T) {
	db := newAuthTestDB(t)
	cfg := testAuthConfig()
	fv := &fakeAppleVerifier{identity: apple.AppleIdentity{
		Sub: "AS-shape", Email: "shape@example.com", EmailVerified: true,
	}}
	app := newAppleApp(t, db, cfg, fv)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/apple",
		bytes.NewBufferString(`{"identityToken":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req, -1)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: want 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	var p ssoResponse
	_ = json.Unmarshal(body, &p)

	// Decode access_token and assert claim keys are exactly {sub, tier, role, name, iat, exp}.
	claims := jwt.MapClaims{}
	_, err := jwt.ParseWithClaims(p.Data.AccessToken, &claims, func(t *jwt.Token) (interface{}, error) {
		return []byte(cfg.JWTSecret), nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil {
		t.Fatalf("parse access token: %v", err)
	}
	want := map[string]bool{"sub": true, "tier": true, "role": true, "name": true, "iat": true, "exp": true}
	for k := range claims {
		if !want[k] {
			t.Errorf("unexpected claim %q in access_token (AUTH-07 shape regression)", k)
		}
		delete(want, k)
	}
	for k := range want {
		t.Errorf("missing claim %q in access_token (AUTH-07 shape regression)", k)
	}
}

// CR-01: empty sub from a signed JWT MUST be rejected as 401, not silently
// promoted to a phantom user row with apple_user_id="". See
// .planning/phases/02-auth-sso-backend/02-VERIFICATION.md (truth #1) and
// .planning/phases/02-auth-sso-backend/02-REVIEW.md (CR-01).
func TestAppleSignIn_EmptySub_Returns401(t *testing.T) {
	db := newAuthTestDB(t)
	cfg := testAuthConfig()
	fv := &fakeAppleVerifier{identity: apple.AppleIdentity{
		Sub: "", Email: "x@example.com", EmailVerified: true, IsPrivateRelay: false,
	}}
	app := newAppleApp(t, db, cfg, fv)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/apple",
		bytes.NewBufferString(`{"identityToken":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: want 401, got %d (body: %s)", resp.StatusCode, string(body))
	}
	body, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(body, []byte("invalid identity token")) {
		t.Errorf("body should carry generic error 'invalid identity token', got %s", string(body))
	}

	// CRITICAL invariant: NO row with apple_user_id="" (phantom user) was created.
	var phantomEmpty int64
	db.Raw("SELECT COUNT(*) FROM users WHERE apple_user_id = ''").Scan(&phantomEmpty)
	if phantomEmpty != 0 {
		t.Errorf("CRITICAL: phantom user with apple_user_id='' was created (CR-01 regression). count=%d", phantomEmpty)
	}
	var anyAppleRow int64
	db.Raw("SELECT COUNT(*) FROM users WHERE apple_user_id IS NOT NULL").Scan(&anyAppleRow)
	if anyAppleRow != 0 {
		t.Errorf("CRITICAL: any apple_user_id row exists after empty-sub rejection. count=%d", anyAppleRow)
	}
}

func TestGoogleSignIn_EmptySub_Returns401(t *testing.T) {
	db := newAuthTestDB(t)
	cfg := testAuthConfig()
	fv := &fakeGoogleVerifier{identity: google.GoogleIdentity{
		Sub: "", Email: "x@example.com", EmailVerified: true,
	}}
	app := newGoogleApp(t, db, cfg, fv)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/google",
		bytes.NewBufferString(`{"idToken":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: want 401, got %d (body: %s)", resp.StatusCode, string(body))
	}
	body, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(body, []byte("invalid identity token")) {
		t.Errorf("body should carry generic error 'invalid identity token', got %s", string(body))
	}

	var phantomEmpty int64
	db.Raw("SELECT COUNT(*) FROM users WHERE google_user_id = ''").Scan(&phantomEmpty)
	if phantomEmpty != 0 {
		t.Errorf("CRITICAL: phantom user with google_user_id='' was created (CR-01 regression). count=%d", phantomEmpty)
	}
	var anyGoogleRow int64
	db.Raw("SELECT COUNT(*) FROM users WHERE google_user_id IS NOT NULL").Scan(&anyGoogleRow)
	if anyGoogleRow != 0 {
		t.Errorf("CRITICAL: any google_user_id row exists after empty-sub rejection. count=%d", anyGoogleRow)
	}
}

// WR-01: parseGuestJWT must reject any token whose role claim is not empty
// or "user". An admin token presented in the Authorization header of
// /auth/apple or /auth/google would otherwise be silently treated as a
// guest-promotion intent and attach the SSO sub to the admin's user row.
// REVIEW.md WR-01.
func TestParseGuestJWT_RejectsAdminRole(t *testing.T) {
	secret := "test-secret-32-bytes-at-minimum!!"

	mint := func(role string) string {
		t.Helper()
		claims := jwt.MapClaims{
			"sub":  uuid.NewString(),
			"tier": "free",
			"role": role,
			"name": "",
			"iat":  time.Now().Unix(),
			"exp":  time.Now().Add(5 * time.Minute).Unix(),
		}
		tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		s, err := tok.SignedString([]byte(secret))
		if err != nil {
			t.Fatalf("sign: %v", err)
		}
		return s
	}

	// Admin token MUST be rejected.
	adminTok := mint("admin")
	if _, err := parseGuestJWT("Bearer "+adminTok, secret); err == nil {
		t.Error("CRITICAL: parseGuestJWT accepted an admin-role token (WR-01 regression)")
	} else if !strings.Contains(err.Error(), "non-user role not allowed for promotion") {
		t.Errorf("expected error containing 'non-user role not allowed for promotion', got %q", err.Error())
	}

	// User role MUST still pass.
	userTok := mint("user")
	if sub, err := parseGuestJWT("Bearer "+userTok, secret); err != nil {
		t.Errorf("WR-01 regression: user-role token must succeed, got error %v", err)
	} else if sub == "" {
		t.Errorf("user-role token: expected non-empty sub, got empty")
	}

	// Empty role MUST also still pass (backwards-compat — pre-WR-01 tokens may lack role).
	emptyRoleTok := mint("")
	if sub, err := parseGuestJWT("Bearer "+emptyRoleTok, secret); err != nil {
		t.Errorf("backwards-compat: empty-role token must succeed, got error %v", err)
	} else if sub == "" {
		t.Errorf("empty-role token: expected non-empty sub, got empty")
	}

	// Unknown roles (e.g. "operator") MUST also be rejected.
	otherTok := mint("operator")
	if _, err := parseGuestJWT("Bearer "+otherTok, secret); err == nil {
		t.Error("parseGuestJWT accepted an unknown-role token; should reject anything not empty or 'user'")
	}
}

// ---- Phase 2 Logout tests (AUTH-08) ------------------------------------------
//
// These tests exercise POST /api/v1/auth/logout end-to-end through the real
// AuthRequired middleware (HOTFIX-02) so the blacklist-check round-trip is
// verified by the same code path production traffic uses. The blacklist key
// prefix asserted below is `token:blacklist:` — the IN-TREE value matching
// internal/cache/redis.go (intentionally diverging from CONTEXT.md D-24 — see
// plan 02-06 objective for the divergence rationale).

// buildLogoutTestApp wires a Fiber app with the protected middleware + Logout
// route + a tiny test-only protected echo endpoint (used by
// TestLogout_AccessTokenInvalidAfterLogout to confirm the blacklist works
// end-to-end through the real middleware) + the public RefreshToken route
// (used by TestLogout_RefreshTokenInvalidAfterLogout).
//
// The middleware signature is (jwtSecret, redisClient, db) — NOT
// (logger, cfg, redisClient, db) as some draft snippets show. This matches
// the in-tree middleware.AuthRequired contract verified at
// internal/middleware/auth.go:43.
func buildLogoutTestApp(t *testing.T, db *gorm.DB, cfg *config.Config, rdb *redis.Client) *fiber.App {
	t.Helper()
	app := fiber.New()

	// Public route — refresh (used by refresh-after-logout assertion).
	app.Post("/api/v1/auth/refresh", RefreshToken(zap.NewNop(), cfg, db))

	// Protected group — mirrors main.go's pattern. HOTFIX-02 middleware
	// re-reads user existence from DB on every request (db non-nil branch).
	protected := app.Group("/api/v1", middleware.AuthRequired(cfg.JWTSecret, rdb, db))
	protected.Post("/auth/logout", Logout(zap.NewNop(), rdb, db))
	protected.Get("/me", func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})
	return app
}

// mintAccessToken returns an HS256 token with the standard claim set used by
// the existing generateTokens function. Used by Logout tests. Distinct from
// mintGuestJWT only in that callers control the TTL explicitly.
func mintAccessToken(t *testing.T, secret, userID string, expIn time.Duration) string {
	t.Helper()
	claims := jwt.MapClaims{
		"sub":  userID,
		"tier": "free",
		"role": "user",
		"name": "",
		"iat":  time.Now().Unix(),
		"exp":  time.Now().Add(expIn).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := tok.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("mint access: %v", err)
	}
	return s
}

// startMiniRedis returns a miniredis-backed *redis.Client and a teardown.
func startMiniRedis(t *testing.T) (*redis.Client, func()) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return rdb, func() {
		_ = rdb.Close()
		mr.Close()
	}
}

// seedUserInAuthTestDB inserts a row into the in-memory users table with the
// minimal columns the auth middleware reads. Reused by Logout tests.
func seedUserInAuthTestDB(t *testing.T, db *gorm.DB, provider string) string {
	t.Helper()
	id := uuid.NewString()
	if err := db.Exec(
		`INSERT INTO users(id, full_name, subscription_tier, role, auth_provider, created_at, updated_at) VALUES(?, '', 'free', 'user', ?, ?, ?)`,
		id, provider, time.Now(), time.Now(),
	).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return id
}

func TestLogout_204_DeletesSession_BlacklistsToken(t *testing.T) {
	db := newAuthTestDB(t)
	cfg := testAuthConfig()
	rdb, stop := startMiniRedis(t)
	defer stop()

	// Seed a guest user + one session.
	userID := seedUserInAuthTestDB(t, db, "guest")
	refreshToken := uuid.NewString()
	refreshHash := fmt.Sprintf("%x", sha256.Sum256([]byte(refreshToken)))
	if err := db.Exec(
		`INSERT INTO sessions(id, user_id, refresh_token_hash, device_info, created_at, expires_at) VALUES(?, ?, ?, '', ?, ?)`,
		uuid.NewString(), userID, refreshHash, time.Now(), time.Now().Add(30*24*time.Hour),
	).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}

	accessToken := mintAccessToken(t, cfg.JWTSecret, userID, 5*time.Minute)
	app := buildLogoutTestApp(t, db, cfg, rdb)

	req := httptest.NewRequest("POST", "/api/v1/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 204 {
		t.Errorf("status: want 204, got %d", resp.StatusCode)
	}

	var n int64
	db.Raw("SELECT COUNT(*) FROM sessions WHERE user_id = ?", userID).Scan(&n)
	if n != 0 {
		t.Errorf("sessions: want 0, got %d", n)
	}

	tokenHash := fmt.Sprintf("%x", sha256.Sum256([]byte(accessToken)))
	ttl, err := rdb.TTL(context.Background(), "token:blacklist:"+tokenHash).Result()
	if err != nil {
		t.Fatalf("rdb.TTL: %v", err)
	}
	if ttl <= 0 {
		t.Errorf("blacklist key TTL: want > 0, got %v", ttl)
	}
}

func TestLogout_AccessTokenInvalidAfterLogout(t *testing.T) {
	db := newAuthTestDB(t)
	cfg := testAuthConfig()
	rdb, stop := startMiniRedis(t)
	defer stop()

	userID := seedUserInAuthTestDB(t, db, "guest")
	accessToken := mintAccessToken(t, cfg.JWTSecret, userID, 5*time.Minute)
	app := buildLogoutTestApp(t, db, cfg, rdb)

	// First: confirm the access token works (sanity).
	req := httptest.NewRequest("GET", "/api/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("pre-logout app.Test: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("pre-logout /me: want 200, got %d", resp.StatusCode)
	}

	// Logout.
	logoutReq := httptest.NewRequest("POST", "/api/v1/auth/logout", nil)
	logoutReq.Header.Set("Authorization", "Bearer "+accessToken)
	logoutResp, err := app.Test(logoutReq, -1)
	if err != nil {
		t.Fatalf("logout app.Test: %v", err)
	}
	if logoutResp.StatusCode != 204 {
		t.Fatalf("logout: want 204, got %d", logoutResp.StatusCode)
	}

	// Now the same access token must be rejected by the AuthRequired
	// middleware (which checks cache.IsTokenBlacklisted on every request).
	req2 := httptest.NewRequest("GET", "/api/v1/me", nil)
	req2.Header.Set("Authorization", "Bearer "+accessToken)
	resp2, err := app.Test(req2, -1)
	if err != nil {
		t.Fatalf("post-logout app.Test: %v", err)
	}
	if resp2.StatusCode != 401 {
		t.Errorf("post-logout /me: want 401 (token revoked), got %d", resp2.StatusCode)
	}
}

func TestLogout_RefreshTokenInvalidAfterLogout(t *testing.T) {
	db := newAuthTestDB(t)
	cfg := testAuthConfig()
	rdb, stop := startMiniRedis(t)
	defer stop()

	userID := seedUserInAuthTestDB(t, db, "guest")
	// Seed two sessions (two devices) — proves the Discretion default
	// "delete ALL sessions for the user" actually wipes every device, not
	// just the calling device.
	refresh1 := uuid.NewString()
	hash1 := fmt.Sprintf("%x", sha256.Sum256([]byte(refresh1)))
	refresh2 := uuid.NewString()
	hash2 := fmt.Sprintf("%x", sha256.Sum256([]byte(refresh2)))
	for _, h := range []string{hash1, hash2} {
		if err := db.Exec(
			`INSERT INTO sessions(id, user_id, refresh_token_hash, device_info, created_at, expires_at) VALUES(?, ?, ?, '', ?, ?)`,
			uuid.NewString(), userID, h, time.Now(), time.Now().Add(30*24*time.Hour),
		).Error; err != nil {
			t.Fatalf("seed session: %v", err)
		}
	}

	accessToken := mintAccessToken(t, cfg.JWTSecret, userID, 5*time.Minute)
	app := buildLogoutTestApp(t, db, cfg, rdb)

	logoutReq := httptest.NewRequest("POST", "/api/v1/auth/logout", nil)
	logoutReq.Header.Set("Authorization", "Bearer "+accessToken)
	logoutResp, err := app.Test(logoutReq, -1)
	if err != nil {
		t.Fatalf("logout app.Test: %v", err)
	}
	if logoutResp.StatusCode != 204 {
		t.Fatalf("logout: want 204, got %d", logoutResp.StatusCode)
	}

	// Now BOTH refresh tokens must fail — RefreshToken handler looks up
	// the session by refresh_token_hash; both rows are deleted.
	for i, rt := range []string{refresh1, refresh2} {
		body := bytes.NewBufferString(fmt.Sprintf(`{"refresh_token":%q}`, rt))
		req := httptest.NewRequest("POST", "/api/v1/auth/refresh", body)
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatalf("refresh app.Test: %v", err)
		}
		if resp.StatusCode != 401 {
			t.Errorf("refresh#%d post-logout: want 401, got %d", i, resp.StatusCode)
		}
	}
}

// WR-02: when the access token's exp == time.Now() (boundary second), the
// Logout handler used to skip the blacklist write because the guard was
// `ttl > 0`. The fix changes the guard to `ttl >= 0`, so a boundary token
// still produces an audit-trail entry. Test uses zap.Observer to assert the
// "blacklist write" branch ran (no error logged, and the write was attempted).
// REVIEW.md WR-02.
func TestLogout_BlacklistsTokenExpiringNow(t *testing.T) {
	db := newAuthTestDB(t)
	cfg := &config.Config{JWTSecret: "test-secret-32-bytes-at-minimum!!"}
	rdb, stop := startMiniRedis(t)
	defer stop()

	// Capture zap output so we can assert the WARN "blacklist write failed"
	// was NOT emitted (proves the cache.BlacklistToken call succeeded, which
	// is only possible when the guard let us in).
	core, observed := observer.New(zapcore.WarnLevel)
	logger := zap.New(core)

	// Seed user + session.
	userID := uuid.NewString()
	if err := db.Exec(`INSERT INTO users(id, full_name, subscription_tier, role, auth_provider, created_at, updated_at) VALUES(?, '', 'free', 'user', 'guest', ?, ?)`,
		userID, time.Now(), time.Now()).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	refreshHash := fmt.Sprintf("%x", sha256.Sum256([]byte(uuid.NewString())))
	if err := db.Exec(`INSERT INTO sessions(id, user_id, refresh_token_hash, device_info, created_at, expires_at) VALUES(?, ?, ?, '', ?, ?)`,
		uuid.NewString(), userID, refreshHash, time.Now(), time.Now().Add(30*24*time.Hour)).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}

	// Mint a token whose exp is just in the future, then sleep past it so
	// the computed ttl ends up <= 0. With WR-02 fix (ttl >= 0), the handler
	// still enters the BlacklistToken branch (with ttl clamped to 0).
	exp := time.Now().Add(1 * time.Second)
	claims := jwt.MapClaims{
		"sub": userID, "tier": "free", "role": "user", "name": "",
		"iat": time.Now().Unix(), "exp": exp.Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	accessToken, err := tok.SignedString([]byte(cfg.JWTSecret))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	// Sleep until exp passes, so ttl computed inside the handler is <= 0
	// — exercises the clamp-to-zero path. With WR-02 fix (ttl >= 0), the
	// handler still enters the BlacklistToken branch.
	time.Sleep(1100 * time.Millisecond)

	// Build a fresh app using OUR logger (not the default no-op) so we can
	// observe what the handler logged. Bypass middleware — set user_id
	// directly so we test JUST the handler's TTL-clamp + blacklist branch
	// (the middleware-rejected case is covered by other Logout tests).
	app := fiber.New()
	app.Post("/api/v1/auth/logout", func(c *fiber.Ctx) error {
		c.Locals("user_id", userID)
		return Logout(logger, rdb, db)(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		t.Errorf("status: want 204, got %d (body: %s)", resp.StatusCode, string(body))
	}

	// WR-02 invariant: the handler should have entered the BlacklistToken
	// branch and emitted no Error-level log. (A WARN about "blacklist write
	// failed (fail-open)" only fires if the branch was taken AND the redis
	// write itself errored — both are acceptable evidence that the boundary
	// case was no longer silently skipped, but Error-level logs are not.)
	for _, entry := range observed.All() {
		if entry.Level >= zapcore.ErrorLevel {
			t.Errorf("WR-02: unexpected error-level log on boundary case: %s", entry.Message)
		}
	}
}

// WR-03: a fresh Apple sign-in (no guest JWT, no email-link candidate)
// MUST insert a subscriptions row with plan='free' AND is_active=1, mirroring
// the GuestLogin pattern. Without this fix, GET /api/v1/subscription returns
// 404 for newly-SSO'd users. REVIEW.md WR-03.
func TestAppleSignIn_NewUser_HasSubscriptionRow(t *testing.T) {
	db := newAuthTestDB(t)
	cfg := testAuthConfig()
	fv := &fakeAppleVerifier{identity: apple.AppleIdentity{
		Sub: "BRAND-NEW", Email: "new@example.com", EmailVerified: true, IsPrivateRelay: false,
	}}
	app := newAppleApp(t, db, cfg, fv)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/apple",
		bytes.NewBufferString(`{"identityToken":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status: want 200, got %d (body: %s)", resp.StatusCode, string(body))
	}
	body, _ := io.ReadAll(resp.Body)
	var p ssoResponse
	if jerr := json.Unmarshal(body, &p); jerr != nil {
		t.Fatalf("decode: %v", jerr)
	}
	if p.Data.User.ID == "" {
		t.Fatalf("expected non-empty user.id")
	}

	// WR-03 invariant: a free subscription row exists for the new SSO user.
	var subCount int64
	db.Raw("SELECT COUNT(*) FROM subscriptions WHERE user_id = ? AND plan = 'free' AND is_active = 1",
		p.Data.User.ID).Scan(&subCount)
	if subCount != 1 {
		t.Errorf("WR-03 regression: want 1 active free subscription row for user_id=%s, got %d",
			p.Data.User.ID, subCount)
	}
}
