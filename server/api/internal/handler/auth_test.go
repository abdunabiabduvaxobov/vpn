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
	"sync"
	"testing"
	"time"

	"vpnapp/server/api/internal/auth/apple"
	"vpnapp/server/api/internal/auth/google"
	"vpnapp/server/api/internal/config"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
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
