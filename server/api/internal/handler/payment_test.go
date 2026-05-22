package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"vpnapp/server/api/internal/config"
	"vpnapp/server/api/internal/model"

	"github.com/gofiber/fiber/v2"
	stripe "github.com/stripe/stripe-go/v81"
	stripewebhook "github.com/stripe/stripe-go/v81/webhook"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// generateStripeSignature produces a valid Stripe-Signature header value for
// the given payload and webhook secret using the Stripe library's own signing
// algorithm. Tests can use this to produce headers that ConstructEventWithOptions
// will accept without hitting the real Stripe API.
func generateStripeSignature(payload []byte, secret string) string {
	signed := stripewebhook.GenerateTestSignedPayload(&stripewebhook.UnsignedPayload{
		Payload:   payload,
		Secret:    secret,
		Timestamp: time.Now(),
	})
	return signed.Header
}

// newTestDB opens an in-memory SQLite database and creates the tables required
// by the payment handler tests using SQLite-compatible DDL (the GORM models
// use gen_random_uuid() defaults that are PostgreSQL-only, so AutoMigrate is
// not used here).
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id                      TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6)))),
			email_hash              TEXT NOT NULL UNIQUE,
			password_hash           TEXT NOT NULL,
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
	}

	for _, stmt := range stmts {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("failed to create test table: %v", err)
		}
	}
	return db
}

// seedUser inserts a user row and returns it.
func seedUser(t *testing.T, db *gorm.DB, tier string) *model.User {
	t.Helper()
	emailHash := "testhash"
	passwordHash := "ph"
	user := &model.User{
		EmailHash:        &emailHash,
		PasswordHash:     &passwordHash,
		SubscriptionTier: tier,
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("failed to seed user: %v", err)
	}
	return user
}

// seedSubscription inserts a subscription row and returns it.
func seedSubscription(t *testing.T, db *gorm.DB, userID, plan, stripeID string, active bool) *model.Subscription {
	t.Helper()
	sub := &model.Subscription{
		UserID:    userID,
		Plan:      plan,
		StripeID:  stripeID,
		IsActive:  active,
		StartedAt: time.Now(),
	}
	if err := db.Create(sub).Error; err != nil {
		t.Fatalf("failed to seed subscription: %v", err)
	}
	return sub
}

// newTestApp wraps a single handler in a minimal Fiber app and sets context
// locals so the JWT middleware is not required in unit tests.
func newTestApp(handler fiber.Handler, userID, tier string) *fiber.App {
	app := fiber.New()
	app.Post("/test", func(c *fiber.Ctx) error {
		c.Locals("user_id", userID)
		c.Locals("tier", tier)
		return c.Next()
	}, handler)
	return app
}

// ---- planToPriceID ----
// planToPriceID now takes a cfg argument so tests must supply one.

func planToPriceIDTestCfg() *config.Config {
	return &config.Config{
		StripePricePremium:  "price_TEST_PREMIUM",
		StripePriceUltimate: "price_TEST_ULTIMATE",
	}
}

func TestPlanToPriceID_Premium(t *testing.T) {
	cfg := planToPriceIDTestCfg()
	id, err := planToPriceID("premium", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != cfg.StripePricePremium {
		t.Errorf("expected %q, got %q", cfg.StripePricePremium, id)
	}
}

func TestPlanToPriceID_Ultimate(t *testing.T) {
	cfg := planToPriceIDTestCfg()
	id, err := planToPriceID("ultimate", cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != cfg.StripePriceUltimate {
		t.Errorf("expected %q, got %q", cfg.StripePriceUltimate, id)
	}
}

func TestPlanToPriceID_InvalidPlan(t *testing.T) {
	_, err := planToPriceID("free", planToPriceIDTestCfg())
	if err == nil {
		t.Fatal("expected error for plan=free, got nil")
	}
}

func TestPlanToPriceID_EmptyPlan(t *testing.T) {
	_, err := planToPriceID("", planToPriceIDTestCfg())
	if err == nil {
		t.Fatal("expected error for empty plan, got nil")
	}
}

// ---- CreateCheckoutSession ----

func TestCreateCheckoutSession_InvalidBody(t *testing.T) {
	logger := zap.NewNop()
	db := newTestDB(t)
	cfg := &config.Config{StripeKey: "sk_test_placeholder", AppDeepLinkScheme: "vpnapp"}

	app := newTestApp(CreateCheckoutSession(logger, cfg, db), "user-1", "free")

	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString("not-json"))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestCreateCheckoutSession_InvalidPlan(t *testing.T) {
	logger := zap.NewNop()
	db := newTestDB(t)
	cfg := &config.Config{StripeKey: "sk_test_placeholder", AppDeepLinkScheme: "vpnapp"}

	app := newTestApp(CreateCheckoutSession(logger, cfg, db), "user-1", "free")

	body, _ := json.Marshal(map[string]string{"plan": "basic"})
	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestCreateCheckoutSession_StripeError(t *testing.T) {
	// With a clearly invalid key Stripe will reject the request — we expect 500.
	logger := zap.NewNop()
	db := newTestDB(t)
	cfg := &config.Config{StripeKey: "sk_test_invalid_key_for_test", AppDeepLinkScheme: "vpnapp"}

	app := newTestApp(CreateCheckoutSession(logger, cfg, db), "user-1", "free")

	body, _ := json.Marshal(map[string]string{"plan": "premium"})
	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusInternalServerError {
		t.Errorf("expected 500 from Stripe rejection, got %d", resp.StatusCode)
	}
}

// ---- HandleStripeWebhook ----

func TestHandleStripeWebhook_MissingSignatureHeader(t *testing.T) {
	logger := zap.NewNop()
	db := newTestDB(t)
	cfg := &config.Config{StripeWebhookSecret: "whsec_test"}

	app := fiber.New()
	app.Post("/webhook/stripe", HandleStripeWebhook(logger, cfg, db))

	req := httptest.NewRequest(http.MethodPost, "/webhook/stripe", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestHandleStripeWebhook_InvalidSignature(t *testing.T) {
	logger := zap.NewNop()
	db := newTestDB(t)
	cfg := &config.Config{StripeWebhookSecret: "whsec_test_secret"}

	app := fiber.New()
	app.Post("/webhook/stripe", HandleStripeWebhook(logger, cfg, db))

	req := httptest.NewRequest(http.MethodPost, "/webhook/stripe", bytes.NewBufferString(`{"type":"checkout.session.completed"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Stripe-Signature", "t=bad,v1=invalid")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestHandleStripeWebhook_UnhandledEventType(t *testing.T) {
	// A valid-signature webhook with an unknown event type should return 200.
	logger := zap.NewNop()
	db := newTestDB(t)
	secret := "whsec_testsecret12345678901234"
	cfg := &config.Config{StripeWebhookSecret: secret}

	app := fiber.New()
	app.Post("/webhook/stripe", HandleStripeWebhook(logger, cfg, db))

	payload := []byte(`{"id":"evt_test","type":"customer.created","data":{"object":{}}}`)
	signedHeader := generateStripeSignature(payload, secret)

	req := httptest.NewRequest(http.MethodPost, "/webhook/stripe", bytes.NewBuffer(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Stripe-Signature", signedHeader)

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

// ---- CancelSubscription ----

func TestCancelSubscription_NoSubscription(t *testing.T) {
	logger := zap.NewNop()
	db := newTestDB(t)
	cfg := &config.Config{StripeKey: "sk_test_placeholder"}

	user := seedUser(t, db, "premium")
	app := newTestApp(CancelSubscription(logger, cfg, db), user.ID, "premium")

	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestCancelSubscription_NoStripeID(t *testing.T) {
	logger := zap.NewNop()
	db := newTestDB(t)
	cfg := &config.Config{StripeKey: "sk_test_placeholder"}

	user := seedUser(t, db, "premium")
	seedSubscription(t, db, user.ID, "premium", "", true)

	app := newTestApp(CancelSubscription(logger, cfg, db), user.ID, "premium")

	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Errorf("expected 400 for missing stripe ID, got %d", resp.StatusCode)
	}
}

func TestCancelSubscription_StripeAPIError(t *testing.T) {
	// An invalid key causes the Stripe API call to fail; expect 500.
	logger := zap.NewNop()
	db := newTestDB(t)
	cfg := &config.Config{StripeKey: "sk_test_invalid_for_cancel"}

	user := seedUser(t, db, "premium")
	seedSubscription(t, db, user.ID, "premium", "sub_placeholder_id", true)

	app := newTestApp(CancelSubscription(logger, cfg, db), user.ID, "premium")

	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusInternalServerError {
		t.Errorf("expected 500 from Stripe rejection, got %d", resp.StatusCode)
	}
}

// ---- handleCheckoutCompleted (internal) ----

func TestHandleCheckoutCompleted_MissingUserID(t *testing.T) {
	db := newTestDB(t)
	logger := zap.NewNop()

	sess := stripe.CheckoutSession{
		ID:       "cs_test",
		Metadata: map[string]string{"plan": "premium"},
	}
	raw, _ := json.Marshal(sess)

	err := handleCheckoutCompleted(logger, db, raw)
	if err == nil {
		t.Fatal("expected error for missing user_id in metadata")
	}
}

func TestHandleCheckoutCompleted_MissingPlan(t *testing.T) {
	db := newTestDB(t)
	logger := zap.NewNop()

	sess := stripe.CheckoutSession{
		ID:       "cs_test",
		Metadata: map[string]string{"user_id": "uid-1"},
	}
	raw, _ := json.Marshal(sess)

	err := handleCheckoutCompleted(logger, db, raw)
	if err == nil {
		t.Fatal("expected error for missing plan in metadata")
	}
}

func TestHandleCheckoutCompleted_CreatesSubscription(t *testing.T) {
	db := newTestDB(t)
	logger := zap.NewNop()

	user := seedUser(t, db, "free")

	sess := stripe.CheckoutSession{
		ID: "cs_test",
		Metadata: map[string]string{
			"user_id": user.ID,
			"plan":    "premium",
		},
	}
	raw, _ := json.Marshal(sess)

	if err := handleCheckoutCompleted(logger, db, raw); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify user tier was upgraded.
	var updated model.User
	db.First(&updated, "id = ?", user.ID)
	if updated.SubscriptionTier != "premium" {
		t.Errorf("expected tier=premium, got %q", updated.SubscriptionTier)
	}

	// Verify subscription row exists.
	var sub model.Subscription
	if err := db.Where("user_id = ? AND is_active = ?", user.ID, true).First(&sub).Error; err != nil {
		t.Fatalf("expected subscription row, got error: %v", err)
	}
	if sub.Plan != "premium" {
		t.Errorf("expected plan=premium, got %q", sub.Plan)
	}
}

// ---- handleSubscriptionDeleted (internal) ----

func TestHandleSubscriptionDeleted_UnknownStripeID(t *testing.T) {
	db := newTestDB(t)
	logger := zap.NewNop()

	stripeSub := stripe.Subscription{ID: "sub_unknown"}
	raw, _ := json.Marshal(stripeSub)

	// Should not return an error — unknown IDs are logged and skipped.
	if err := handleSubscriptionDeleted(logger, db, raw); err != nil {
		t.Fatalf("unexpected error for unknown stripe ID: %v", err)
	}
}

func TestHandleSubscriptionDeleted_DeactivatesSubscription(t *testing.T) {
	db := newTestDB(t)
	logger := zap.NewNop()

	user := seedUser(t, db, "premium")
	seedSubscription(t, db, user.ID, "premium", "sub_abc123", true)

	stripeSub := stripe.Subscription{ID: "sub_abc123"}
	raw, _ := json.Marshal(stripeSub)

	if err := handleSubscriptionDeleted(logger, db, raw); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var sub model.Subscription
	db.Where("user_id = ?", user.ID).First(&sub)
	if sub.IsActive {
		t.Error("expected subscription to be inactive after deletion event")
	}

	var updated model.User
	db.First(&updated, "id = ?", user.ID)
	if updated.SubscriptionTier != "free" {
		t.Errorf("expected tier=free after cancellation, got %q", updated.SubscriptionTier)
	}
}

// ---- handlePaymentFailed (internal) ----

func TestHandlePaymentFailed_NoSubscriptionField(t *testing.T) {
	db := newTestDB(t)
	logger := zap.NewNop()

	invoice := stripe.Invoice{ID: "in_test"}
	raw, _ := json.Marshal(invoice)

	// No subscription attached — should be a no-op, not an error.
	if err := handlePaymentFailed(logger, db, raw); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHandlePaymentFailed_DeactivatesSubscription(t *testing.T) {
	db := newTestDB(t)
	logger := zap.NewNop()

	user := seedUser(t, db, "ultimate")
	seedSubscription(t, db, user.ID, "ultimate", "sub_fail123", true)

	invoice := stripe.Invoice{
		ID:           "in_fail",
		Subscription: &stripe.Subscription{ID: "sub_fail123"},
	}
	raw, _ := json.Marshal(invoice)

	if err := handlePaymentFailed(logger, db, raw); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var sub model.Subscription
	db.Where("user_id = ?", user.ID).First(&sub)
	if sub.IsActive {
		t.Error("expected subscription to be inactive after payment failure")
	}
}
