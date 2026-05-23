package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"vpnapp/server/api/internal/config"
	"vpnapp/server/api/internal/lava"
	"vpnapp/server/api/internal/model"
	"vpnapp/server/api/internal/repository"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// setupPaymentTestDB creates the minimum schema for payment handler tests.
// Includes users + plans + plan_offers + invoices + lava_contracts + subscriptions.
// Uses SQLite-compatible DDL; PostgreSQL gen_random_uuid() defaults are emulated
// via randomblob() so the same SetUserPlan transactional pattern that drives
// plan_repo tests works here as well.
func setupPaymentTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// Pin the connection pool to 1 so in-memory state is visible across
	// transactional helpers (same pattern as repository/plan_repo_test.go).
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	uuidDefault := `(lower(hex(randomblob(4))) || '-' || lower(hex(randomblob(2))) || '-4' || substr(lower(hex(randomblob(2))),2) || '-' || substr('89ab',abs(random()) % 4 + 1, 1) || substr(lower(hex(randomblob(2))),2) || '-' || lower(hex(randomblob(6))))`

	stmts := []string{
		`CREATE TABLE users (
			id TEXT PRIMARY KEY DEFAULT ` + uuidDefault + `,
			email TEXT,
			email_verified INTEGER DEFAULT 0,
			email_is_private_relay INTEGER DEFAULT 0,
			full_name TEXT NOT NULL DEFAULT '',
			subscription_tier TEXT NOT NULL DEFAULT 'free',
			subscription_expires_at TIMESTAMP,
			role TEXT NOT NULL DEFAULT 'user',
			auth_provider TEXT NOT NULL DEFAULT 'guest',
			apple_user_id TEXT,
			google_user_id TEXT,
			email_hash TEXT,
			password_hash TEXT,
			telegram_user_id INTEGER,
			telegram_linked_at TIMESTAMP,
			telegram_username TEXT,
			telegram_first_name TEXT,
			plan_id TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE plans (
			id TEXT PRIMARY KEY DEFAULT ` + uuidDefault + `,
			code TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			max_devices INTEGER NOT NULL,
			max_servers INTEGER NOT NULL,
			speed_limit_mbps INTEGER NOT NULL DEFAULT 0,
			is_active INTEGER NOT NULL DEFAULT 1,
			is_system INTEGER NOT NULL DEFAULT 0,
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE plan_offers (
			id TEXT PRIMARY KEY DEFAULT ` + uuidDefault + `,
			plan_id TEXT NOT NULL,
			periodicity TEXT NOT NULL,
			currency TEXT NOT NULL,
			amount REAL NOT NULL,
			lava_offer_id TEXT,
			is_active INTEGER NOT NULL DEFAULT 1,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE invoices (
			id TEXT PRIMARY KEY DEFAULT ` + uuidDefault + `,
			user_id TEXT NOT NULL,
			lava_invoice_id TEXT NOT NULL UNIQUE,
			offer_id TEXT NOT NULL,
			plan_id TEXT,
			plan_offer_id TEXT,
			plan TEXT NOT NULL,
			periodicity TEXT NOT NULL,
			currency TEXT NOT NULL,
			amount REAL NOT NULL,
			status TEXT NOT NULL,
			payment_url TEXT,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE lava_contracts (
			id TEXT PRIMARY KEY DEFAULT ` + uuidDefault + `,
			user_id TEXT NOT NULL,
			contract_id TEXT NOT NULL UNIQUE,
			parent_contract_id TEXT,
			offer_id TEXT NOT NULL,
			plan TEXT NOT NULL,
			periodicity TEXT NOT NULL,
			currency TEXT NOT NULL,
			is_active INTEGER NOT NULL DEFAULT 1,
			started_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			expires_at TIMESTAMP,
			cancelled_at TIMESTAMP,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE subscriptions (
			id TEXT PRIMARY KEY DEFAULT ` + uuidDefault + `,
			user_id TEXT NOT NULL,
			plan TEXT NOT NULL DEFAULT 'free',
			lava_contract_id TEXT,
			is_active INTEGER NOT NULL DEFAULT 1,
			started_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			expires_at TIMESTAMP
		)`,
	}
	for _, s := range stmts {
		if err := db.Exec(s).Error; err != nil {
			t.Fatalf("DDL: %v", err)
		}
	}
	return db
}

// mkPaymentApp wires the three handlers + the lava client (httptest-mocked)
// onto a fresh Fiber app. A thin middleware sets c.Locals("user_id") so
// tests don't need full JWT auth.
func mkPaymentApp(t *testing.T, db *gorm.DB, lavaClient *lava.Client, userID string) *fiber.App {
	t.Helper()
	logger := zap.NewNop()
	cfg := &config.Config{}
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user_id", userID)
		return c.Next()
	})
	app.Post("/api/v1/checkout", CreateCheckoutSession(logger, cfg, db, lavaClient))
	app.Post("/api/v1/subscription/cancel", CancelSubscription(logger, cfg, db, lavaClient))
	app.Get("/api/v1/invoices/:id", GetInvoice(logger, cfg, db, lavaClient))
	return app
}

// seedUserAndPlan inserts a free + pro plan, a SSO user with the given email,
// and an active Pro MONTHLY/USD offer with the supplied lava_offer_id (nil
// for the placeholder 409 test).
func seedUserAndPlan(t *testing.T, db *gorm.DB, email string, lavaOfferID *string) (userID, planID, offerID string) {
	t.Helper()
	freeID := uuid.NewString()
	proID := uuid.NewString()
	for _, p := range []model.Plan{
		{ID: freeID, Code: "free", Name: "Free", MaxDevices: 1, MaxServers: 3, IsActive: true, IsSystem: true, SortOrder: 0},
		{ID: proID, Code: "pro", Name: "Pro", MaxDevices: 3, MaxServers: -1, IsActive: true, IsSystem: false, SortOrder: 10},
	} {
		if err := db.Create(&p).Error; err != nil {
			t.Fatalf("seed plan: %v", err)
		}
	}
	offerID = uuid.NewString()
	if err := db.Create(&model.PlanOffer{
		ID: offerID, PlanID: proID, Periodicity: "MONTHLY", Currency: "USD",
		Amount: 5.0, LavaOfferID: lavaOfferID, IsActive: true,
	}).Error; err != nil {
		t.Fatalf("seed offer: %v", err)
	}
	userID = uuid.NewString()
	if err := db.Create(&model.User{
		ID: userID, Email: &email, EmailVerified: true, FullName: "u",
		SubscriptionTier: "free", PlanID: freeID, AuthProvider: "google",
	}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return userID, proID, offerID
}

// seedGuestUser inserts a guest user with Email=nil; used by the 403-guest
// rejection test.
func seedGuestUser(t *testing.T, db *gorm.DB, planID string) string {
	t.Helper()
	userID := uuid.NewString()
	if err := db.Create(&model.User{
		ID: userID, FullName: "guest", SubscriptionTier: "free",
		PlanID: planID, AuthProvider: "guest",
	}).Error; err != nil {
		t.Fatalf("seed guest user: %v", err)
	}
	return userID
}

// newLavaTestClient is a thin convenience wrapper around lava.NewForTest —
// keeps test bodies readable while remaining a pure consumer of the helper
// added in plan 03-02 T02.
func newLavaTestClient(t *testing.T, baseURL string) *lava.Client {
	t.Helper()
	return lava.NewForTest("test-key", baseURL)
}

// decodeJSONBody is a tiny helper to read+decode an httptest response body.
func decodeJSONBody(t *testing.T, resp *http.Response, out interface{}) string {
	t.Helper()
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(resp.Body)
	raw := buf.String()
	if out != nil {
		if err := json.Unmarshal(buf.Bytes(), out); err != nil {
			t.Fatalf("decode body %q: %v", raw, err)
		}
	}
	return raw
}

// --- Tests ---

func TestCreateCheckoutSession_HappyPath(t *testing.T) {
	db := setupPaymentTestDB(t)
	lavaOID := "lava-off-1"
	userID, _, _ := seedUserAndPlan(t, db, "alice@example.com", &lavaOID)

	lavaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/invoice" {
			t.Errorf("expected /api/v3/invoice, got %s", r.URL.Path)
		}
		w.WriteHeader(200)
		pu := "https://app.lava.top/pay/abc"
		_ = json.NewEncoder(w).Encode(lava.InvoiceResponse{
			ID:          "lava-inv-1",
			Status:      "in-progress",
			AmountTotal: lava.InvoiceAmount{Amount: 5.0, Currency: "USD"},
			PaymentURL:  &pu,
		})
	}))
	defer lavaServer.Close()
	client := newLavaTestClient(t, lavaServer.URL)

	app := mkPaymentApp(t, db, client, userID)
	body := strings.NewReader(`{"plan_code":"pro","periodicity":"MONTHLY","currency":"USD"}`)
	req := httptest.NewRequest("POST", "/api/v1/checkout", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	var body2 struct {
		Data struct {
			InvoiceID     string  `json:"invoice_id"`
			LavaInvoiceID string  `json:"lava_invoice_id"`
			PaymentURL    string  `json:"payment_url"`
			Amount        float64 `json:"amount"`
			Currency      string  `json:"currency"`
		} `json:"data"`
	}
	raw := decodeJSONBody(t, resp, &body2)
	if resp.StatusCode != 201 {
		t.Fatalf("expected 201, got %d body=%s", resp.StatusCode, raw)
	}
	if body2.Data.LavaInvoiceID != "lava-inv-1" {
		t.Errorf("expected lava_invoice_id=lava-inv-1, got %q", body2.Data.LavaInvoiceID)
	}
	if body2.Data.PaymentURL != "https://app.lava.top/pay/abc" {
		t.Errorf("expected payment_url, got %q", body2.Data.PaymentURL)
	}
	if body2.Data.Currency != "USD" || body2.Data.Amount != 5.0 {
		t.Errorf("expected USD/5.0, got %s/%v", body2.Data.Currency, body2.Data.Amount)
	}
	if body2.Data.InvoiceID == "" {
		t.Error("expected non-empty invoice_id")
	}

	// Verify the invoice row was actually written with status=pending.
	inv, err := repository.FindInvoiceByID(db, body2.Data.InvoiceID)
	if err != nil {
		t.Fatalf("FindInvoiceByID: %v", err)
	}
	if inv.Status != "pending" {
		t.Errorf("expected invoice.status=pending, got %q", inv.Status)
	}
	if inv.UserID != userID {
		t.Errorf("expected invoice.user_id=%s, got %s", userID, inv.UserID)
	}
}

func TestCreateCheckoutSession_409_OfferNotConfigured(t *testing.T) {
	db := setupPaymentTestDB(t)
	// nil lava_offer_id → D-09 placeholder offer.
	userID, _, _ := seedUserAndPlan(t, db, "alice@example.com", nil)

	// httptest server should NEVER be hit on the 409 path — fail loudly if it is.
	lavaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("lava should not be called for placeholder offer; got %s %s", r.Method, r.URL.Path)
		w.WriteHeader(500)
	}))
	defer lavaServer.Close()
	client := newLavaTestClient(t, lavaServer.URL)

	app := mkPaymentApp(t, db, client, userID)
	body := strings.NewReader(`{"plan_code":"pro","periodicity":"MONTHLY","currency":"USD"}`)
	req := httptest.NewRequest("POST", "/api/v1/checkout", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	var body2 map[string]string
	raw := decodeJSONBody(t, resp, &body2)
	if resp.StatusCode != 409 {
		t.Fatalf("expected 409, got %d body=%s", resp.StatusCode, raw)
	}
	if body2["error"] != "offer_not_configured" {
		t.Errorf("expected error=offer_not_configured, got %q (raw=%s)", body2["error"], raw)
	}
}

func TestCreateCheckoutSession_60sIdempotencyReuse(t *testing.T) {
	db := setupPaymentTestDB(t)
	lavaOID := "lava-off-2"
	userID, _, _ := seedUserAndPlan(t, db, "alice@example.com", &lavaOID)

	// Count lava POSTs so we can assert the SECOND call DOES NOT hit lava.
	var lavaPosts int
	lavaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lavaPosts++
		w.WriteHeader(200)
		pu := "https://app.lava.top/pay/idempotent"
		_ = json.NewEncoder(w).Encode(lava.InvoiceResponse{
			ID:          "lava-inv-idempotent",
			Status:      "in-progress",
			AmountTotal: lava.InvoiceAmount{Amount: 5.0, Currency: "USD"},
			PaymentURL:  &pu,
		})
	}))
	defer lavaServer.Close()
	client := newLavaTestClient(t, lavaServer.URL)

	app := mkPaymentApp(t, db, client, userID)
	body1 := strings.NewReader(`{"plan_code":"pro","periodicity":"MONTHLY","currency":"USD"}`)
	req1 := httptest.NewRequest("POST", "/api/v1/checkout", body1)
	req1.Header.Set("Content-Type", "application/json")
	resp1, err := app.Test(req1)
	if err != nil {
		t.Fatalf("first Test: %v", err)
	}
	var first struct {
		Data struct {
			InvoiceID string `json:"invoice_id"`
		} `json:"data"`
	}
	raw1 := decodeJSONBody(t, resp1, &first)
	if resp1.StatusCode != 201 {
		t.Fatalf("first call expected 201, got %d body=%s", resp1.StatusCode, raw1)
	}

	// Second call within 60s — should reuse, 200 OK, lavaPosts must remain 1.
	body2 := strings.NewReader(`{"plan_code":"pro","periodicity":"MONTHLY","currency":"USD"}`)
	req2 := httptest.NewRequest("POST", "/api/v1/checkout", body2)
	req2.Header.Set("Content-Type", "application/json")
	resp2, err := app.Test(req2)
	if err != nil {
		t.Fatalf("second Test: %v", err)
	}
	var second struct {
		Data struct {
			InvoiceID string `json:"invoice_id"`
		} `json:"data"`
	}
	raw2 := decodeJSONBody(t, resp2, &second)
	if resp2.StatusCode != 200 {
		t.Fatalf("second call expected 200 (reuse), got %d body=%s", resp2.StatusCode, raw2)
	}
	if first.Data.InvoiceID != second.Data.InvoiceID {
		t.Errorf("expected same invoice_id on reuse, got %s vs %s", first.Data.InvoiceID, second.Data.InvoiceID)
	}
	if lavaPosts != 1 {
		t.Errorf("expected lava called exactly once across two checkouts, got %d", lavaPosts)
	}
}

func TestCreateCheckoutSession_GuestRejected(t *testing.T) {
	db := setupPaymentTestDB(t)
	// Need plans seeded so the lookup chain would proceed if email check failed.
	freeID := uuid.NewString()
	proID := uuid.NewString()
	if err := db.Create(&model.Plan{ID: freeID, Code: "free", Name: "Free", MaxDevices: 1, MaxServers: 3, IsActive: true, IsSystem: true}).Error; err != nil {
		t.Fatalf("seed free plan: %v", err)
	}
	if err := db.Create(&model.Plan{ID: proID, Code: "pro", Name: "Pro", MaxDevices: 3, MaxServers: -1, IsActive: true}).Error; err != nil {
		t.Fatalf("seed pro plan: %v", err)
	}
	guestID := seedGuestUser(t, db, freeID)

	lavaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("lava should not be called for guest user; got %s %s", r.Method, r.URL.Path)
		w.WriteHeader(500)
	}))
	defer lavaServer.Close()
	client := newLavaTestClient(t, lavaServer.URL)

	app := mkPaymentApp(t, db, client, guestID)
	body := strings.NewReader(`{"plan_code":"pro","periodicity":"MONTHLY","currency":"USD"}`)
	req := httptest.NewRequest("POST", "/api/v1/checkout", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	if resp.StatusCode != 403 {
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(resp.Body)
		t.Fatalf("expected 403, got %d body=%s", resp.StatusCode, buf.String())
	}
}

func TestCreateCheckoutSession_InvalidCurrency(t *testing.T) {
	db := setupPaymentTestDB(t)
	lavaOID := "lava-off-3"
	userID, _, _ := seedUserAndPlan(t, db, "alice@example.com", &lavaOID)

	lavaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("lava should not be called for invalid currency; got %s %s", r.Method, r.URL.Path)
		w.WriteHeader(500)
	}))
	defer lavaServer.Close()
	client := newLavaTestClient(t, lavaServer.URL)

	app := mkPaymentApp(t, db, client, userID)
	body := strings.NewReader(`{"plan_code":"pro","periodicity":"MONTHLY","currency":"XXX"}`)
	req := httptest.NewRequest("POST", "/api/v1/checkout", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestCancelSubscription_KeepsProUntilExpiry(t *testing.T) {
	db := setupPaymentTestDB(t)
	lavaOID := "lava-off-cancel"
	userID, _, _ := seedUserAndPlan(t, db, "alice@example.com", &lavaOID)

	// Bump user to Pro so we can verify it STAYS Pro after cancel.
	if err := db.Exec(`UPDATE users SET subscription_tier='pro' WHERE id=?`, userID).Error; err != nil {
		t.Fatalf("set user pro: %v", err)
	}

	// Seed an active lava_contract for this user with a future expires_at.
	expiresAt := time.Now().Add(20 * 24 * time.Hour) // 20 days out
	contractRow := &model.LavaContract{
		ID:          uuid.NewString(),
		UserID:      userID,
		ContractID:  "ctr-abc-123",
		OfferID:     lavaOID,
		Plan:        "pro",
		Periodicity: "MONTHLY",
		Currency:    "USD",
		IsActive:    true,
		ExpiresAt:   &expiresAt,
	}
	if err := db.Create(contractRow).Error; err != nil {
		t.Fatalf("seed contract: %v", err)
	}

	// Lava DELETE mock — 200 success.
	lavaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if !strings.HasPrefix(r.URL.Path, "/api/v1/subscriptions") {
			t.Errorf("expected /api/v1/subscriptions path, got %s", r.URL.Path)
		}
		// contractId + email must be present in query.
		if r.URL.Query().Get("contractId") != "ctr-abc-123" {
			t.Errorf("expected contractId=ctr-abc-123, got %q", r.URL.Query().Get("contractId"))
		}
		if r.URL.Query().Get("email") != "alice@example.com" {
			t.Errorf("expected email=alice@example.com, got %q", r.URL.Query().Get("email"))
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer lavaServer.Close()
	client := newLavaTestClient(t, lavaServer.URL)

	app := mkPaymentApp(t, db, client, userID)
	req := httptest.NewRequest("POST", "/api/v1/subscription/cancel", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	var body struct {
		Data struct {
			Cancelled bool `json:"cancelled"`
		} `json:"data"`
	}
	raw := decodeJSONBody(t, resp, &body)
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d body=%s", resp.StatusCode, raw)
	}
	if !body.Data.Cancelled {
		t.Errorf("expected data.cancelled=true, raw=%s", raw)
	}

	// Verify local contract row was marked cancelled.
	var reloaded model.LavaContract
	if err := db.Where("id = ?", contractRow.ID).First(&reloaded).Error; err != nil {
		t.Fatalf("reload contract: %v", err)
	}
	if reloaded.IsActive {
		t.Error("expected contract.is_active=false after cancel")
	}
	if reloaded.CancelledAt == nil {
		t.Error("expected contract.cancelled_at to be set")
	}

	// CRITICAL (PAY-10): users.subscription_tier MUST remain 'pro' — expiry cron downgrades.
	var u model.User
	if err := db.Where("id = ?", userID).First(&u).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if u.SubscriptionTier != "pro" {
		t.Errorf("PAY-10 violation: expected user.subscription_tier=pro after cancel, got %q", u.SubscriptionTier)
	}
}

func TestGetInvoice_DBOnly(t *testing.T) {
	db := setupPaymentTestDB(t)
	lavaOID := "lava-off-get-db"
	userID, _, offerUUID := seedUserAndPlan(t, db, "alice@example.com", &lavaOID)

	// Seed an invoice owned by this user, status=pending.
	invID := uuid.NewString()
	inv := &model.Invoice{
		ID:            invID,
		UserID:        userID,
		LavaInvoiceID: "lava-inv-getdb",
		OfferID:       lavaOID,
		PlanOfferID:   &offerUUID,
		Plan:          "pro",
		Periodicity:   "MONTHLY",
		Currency:      "USD",
		Amount:        5.0,
		Status:        "pending",
	}
	if err := db.Create(inv).Error; err != nil {
		t.Fatalf("seed invoice: %v", err)
	}

	// Lava MUST NOT be called when escalate is not requested.
	lavaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("lava should not be called for db-only GET; got %s %s", r.Method, r.URL.Path)
		w.WriteHeader(500)
	}))
	defer lavaServer.Close()
	client := newLavaTestClient(t, lavaServer.URL)

	app := mkPaymentApp(t, db, client, userID)
	req := httptest.NewRequest("GET", "/api/v1/invoices/"+invID, nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	if resp.StatusCode != 200 {
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(resp.Body)
		t.Fatalf("expected 200, got %d body=%s", resp.StatusCode, buf.String())
	}
	var body struct {
		Data struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"data"`
	}
	decodeJSONBody(t, resp, &body)
	if body.Data.Status != "pending" {
		t.Errorf("expected status=pending, got %q", body.Data.Status)
	}
	if body.Data.ID != invID {
		t.Errorf("expected id=%s, got %s", invID, body.Data.ID)
	}
}

func TestGetInvoice_EscalateUpdatesPendingToPaid(t *testing.T) {
	db := setupPaymentTestDB(t)
	lavaOID := "lava-off-escalate"
	userID, _, offerUUID := seedUserAndPlan(t, db, "alice@example.com", &lavaOID)

	invID := uuid.NewString()
	inv := &model.Invoice{
		ID:            invID,
		UserID:        userID,
		LavaInvoiceID: "lava-inv-escalate",
		OfferID:       lavaOID,
		PlanOfferID:   &offerUUID,
		Plan:          "pro",
		Periodicity:   "MONTHLY",
		Currency:      "USD",
		Amount:        5.0,
		Status:        "pending",
	}
	if err := db.Create(inv).Error; err != nil {
		t.Fatalf("seed invoice: %v", err)
	}

	// Lava reports COMPLETED (uppercase per RESEARCH §1.2 — handled by both-case mapper).
	lavaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("expected GET, got %s", r.Method)
		}
		w.WriteHeader(200)
		_ = json.NewEncoder(w).Encode(lava.InvoiceDetailResponse{
			ID:     "lava-inv-escalate",
			Status: "COMPLETED",
			Receipt: lava.InvoiceReceipt{
				Amount: 5.0, Currency: "USD",
			},
		})
	}))
	defer lavaServer.Close()
	client := newLavaTestClient(t, lavaServer.URL)

	app := mkPaymentApp(t, db, client, userID)
	req := httptest.NewRequest("GET", "/api/v1/invoices/"+invID+"?escalate=true", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body struct {
		Data struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	decodeJSONBody(t, resp, &body)
	if body.Data.Status != "paid" {
		t.Errorf("expected status=paid after escalate reconciles COMPLETED, got %q", body.Data.Status)
	}

	// CRITICAL (D-32 §2): users.subscription_tier MUST remain 'free' — escalate path
	// must NEVER call SetUserPlan (webhook authoritative).
	var u model.User
	if err := db.Where("id = ?", userID).First(&u).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if u.SubscriptionTier != "free" {
		t.Errorf("D-32 §2 violation: escalate path must not grant tier; expected free, got %q", u.SubscriptionTier)
	}

	// Verify local invoice row was flipped to paid.
	reloaded, err := repository.FindInvoiceByID(db, invID)
	if err != nil {
		t.Fatalf("FindInvoiceByID: %v", err)
	}
	if reloaded.Status != "paid" {
		t.Errorf("expected stored invoice.status=paid, got %q", reloaded.Status)
	}
}

func TestGetInvoice_OwnershipCheck_Returns404OnMismatch(t *testing.T) {
	db := setupPaymentTestDB(t)
	lavaOID := "lava-off-own"
	ownerID, _, offerUUID := seedUserAndPlan(t, db, "owner@example.com", &lavaOID)

	// Seed a second user (attacker).
	attackerID := uuid.NewString()
	attackerEmail := "attacker@example.com"
	freePlanID := ""
	if err := db.Raw(`SELECT id FROM plans WHERE code='free'`).Scan(&freePlanID).Error; err != nil {
		t.Fatalf("look up free plan id: %v", err)
	}
	if err := db.Create(&model.User{
		ID: attackerID, Email: &attackerEmail, EmailVerified: true, FullName: "x",
		SubscriptionTier: "free", PlanID: freePlanID, AuthProvider: "google",
	}).Error; err != nil {
		t.Fatalf("seed attacker: %v", err)
	}

	// Seed invoice owned by ownerID.
	invID := uuid.NewString()
	inv := &model.Invoice{
		ID:            invID,
		UserID:        ownerID,
		LavaInvoiceID: "lava-inv-own",
		OfferID:       lavaOID,
		PlanOfferID:   &offerUUID,
		Plan:          "pro",
		Periodicity:   "MONTHLY",
		Currency:      "USD",
		Amount:        5.0,
		Status:        "pending",
	}
	if err := db.Create(inv).Error; err != nil {
		t.Fatalf("seed invoice: %v", err)
	}

	// Lava should NOT be called — request is rejected at ownership check.
	lavaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("lava should not be called for ownership-denied request; got %s %s", r.Method, r.URL.Path)
		w.WriteHeader(500)
	}))
	defer lavaServer.Close()
	client := newLavaTestClient(t, lavaServer.URL)

	// App is built with attacker's user_id — attempting to fetch owner's invoice.
	app := mkPaymentApp(t, db, client, attackerID)
	req := httptest.NewRequest("GET", "/api/v1/invoices/"+invID+"?escalate=true", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	if resp.StatusCode != 404 {
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(resp.Body)
		t.Fatalf("expected 404 (D-32 §2 — no existence leak), got %d body=%s", resp.StatusCode, buf.String())
	}
}
