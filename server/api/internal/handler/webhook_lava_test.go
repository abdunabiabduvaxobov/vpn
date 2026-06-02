package handler

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"vpnapp/server/api/internal/config"
	"vpnapp/server/api/internal/model"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// setupWebhookTestDB extends setupPaymentTestDB (plan 03-05) with the
// lava_webhook_events table required by the webhook handler. We duplicate
// the schema rather than calling setupPaymentTestDB so this test file is
// self-contained and survives any payment_test.go refactor.
func setupWebhookTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
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
			suspended_at TIMESTAMP,
			suspended_reason TEXT,
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
		// CR-02: mirror the production UNIQUE rule from migration 020 + 021.
		// Production uses COALESCE((payload->>'timestamp')::text,
		//                          (payload->>'cancelledAt')::text,
		//                          'no-timestamp')
		// SQLite equivalent: COALESCE(json_extract(payload,'$.timestamp'),
		//                             json_extract(payload,'$.cancelledAt'),
		//                             'no-timestamp')
		// SQLite supports CREATE UNIQUE INDEX on expressions, so we drop the
		// inline UNIQUE on (event_type, contract_id, payload) and use a
		// separate expression index that matches production semantics.
		// Without this alignment, the test suite cannot regression-test the
		// production idempotency rule (TestHandleLavaWebhook_DuplicateNoop
		// would pass on byte-identical bodies regardless of the prod rule).
		`CREATE TABLE lava_webhook_events (
			id TEXT PRIMARY KEY DEFAULT ` + uuidDefault + `,
			event_type TEXT NOT NULL,
			contract_id TEXT,
			invoice_id TEXT,
			payload TEXT NOT NULL,
			received_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			processed_at TIMESTAMP,
			error TEXT,
			-- migration 024 (ADMIN-06): webhook log status + replay counter.
			-- Mirrored here so MarkWebhookProcessed's status write succeeds on
			-- the SQLite test DB exactly as it does on production Postgres.
			status TEXT NOT NULL DEFAULT 'PENDING',
			retried_count INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE UNIQUE INDEX idx_lava_webhook_events_natural_key
			ON lava_webhook_events (
				event_type,
				contract_id,
				COALESCE(
					json_extract(payload, '$.timestamp'),
					json_extract(payload, '$.cancelledAt'),
					'no-timestamp'
				)
			)`,
		// HARD-02 (migration 026): per-user VLESS identities. Mirrored here so the
		// payment.success rotation (RotateVlessUUID inside the WithUserLock tx)
		// has a table to write to on the SQLite test DB exactly as on production.
		`CREATE TABLE user_vless_identities (
			id          TEXT PRIMARY KEY DEFAULT ` + uuidDefault + `,
			user_id     TEXT NOT NULL,
			vless_uuid  TEXT NOT NULL UNIQUE,
			is_active   INTEGER NOT NULL DEFAULT 1,
			created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			revoked_at  TIMESTAMP
		)`,
	}
	for _, s := range stmts {
		if err := db.Exec(s).Error; err != nil {
			t.Fatalf("DDL: %v", err)
		}
	}
	return db
}

// mkWebhookApp wires HandleLavaWebhook onto a fresh Fiber app. The lavaClient
// arg is nil because the handler never makes outbound calls — it only reads
// the DB and the X-Api-Key header.
func mkWebhookApp(t *testing.T, db *gorm.DB, secret, previous string) *fiber.App {
	t.Helper()
	app := fiber.New()
	cfg := &config.Config{LavaWebhookSecret: secret, LavaWebhookSecretPrevious: previous}
	app.Post("/api/v1/webhook/lava", HandleLavaWebhook(zap.NewNop(), cfg, db, nil, nil))
	return app
}

// seedWebhookFixture inserts a free + pro plan, an active Pro MONTHLY/USD
// offer with the given lava_offer_id, an SSO user with the given email, and
// an invoice owned by that user with lava_invoice_id == contractID.
type seedFixture struct {
	UserID    string
	ProPlanID string
	FreePlanID string
	OfferID   string // local plan_offers.id (UUID)
	LavaOfferID string
	InvoiceID string
}

func seedWebhookFixture(t *testing.T, db *gorm.DB, email, contractID, lavaOfferID string) seedFixture {
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
	offerID := uuid.NewString()
	lavaOID := lavaOfferID
	if err := db.Create(&model.PlanOffer{
		ID: offerID, PlanID: proID, Periodicity: "MONTHLY", Currency: "USD",
		Amount: 5.0, LavaOfferID: &lavaOID, IsActive: true,
	}).Error; err != nil {
		t.Fatalf("seed offer: %v", err)
	}
	userID := uuid.NewString()
	emailVal := email
	if err := db.Create(&model.User{
		ID: userID, Email: &emailVal, EmailVerified: true, FullName: "u",
		SubscriptionTier: "free", PlanID: freeID, AuthProvider: "google",
	}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	invID := uuid.NewString()
	if err := db.Create(&model.Invoice{
		ID:            invID,
		UserID:        userID,
		LavaInvoiceID: contractID,
		OfferID:       lavaOID,
		PlanOfferID:   &offerID,
		PlanID:        &proID,
		Plan:          "pro",
		Periodicity:   "MONTHLY",
		Currency:      "USD",
		Amount:        5.0,
		Status:        "pending",
	}).Error; err != nil {
		t.Fatalf("seed invoice: %v", err)
	}
	return seedFixture{
		UserID: userID, ProPlanID: proID, FreePlanID: freeID,
		OfferID: offerID, LavaOfferID: lavaOID, InvoiceID: invID,
	}
}

// deliverWebhook is a small helper for executing a POST against the test app.
type httpResp struct {
	Status int
	Body   string
}

func deliverWebhook(t *testing.T, app *fiber.App, secret, body string) httpResp {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/v1/webhook/lava", strings.NewReader(body))
	req.Header.Set("X-Api-Key", secret)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(resp.Body)
	return httpResp{Status: resp.StatusCode, Body: buf.String()}
}

// --- Tests ---

// TestHandleLavaWebhook_BadSignature_401 covers PAY-07.
func TestHandleLavaWebhook_BadSignature_401(t *testing.T) {
	db := setupWebhookTestDB(t)
	app := mkWebhookApp(t, db, "good-secret", "")
	body := `{"eventType":"payment.success","contractId":"contract-1","timestamp":"2026-05-23T10:00:00Z"}`
	got := deliverWebhook(t, app, "wrong-secret", body)
	if got.Status != 401 {
		t.Errorf("expected 401 on bad X-Api-Key, got %d body=%s", got.Status, got.Body)
	}
	// And no event row recorded (we never got past auth).
	var n int64
	_ = db.Model(&model.LavaWebhookEvent{}).Count(&n).Error
	if n != 0 {
		t.Errorf("expected 0 event rows after bad-signature reject, got %d", n)
	}
}

// TestHandleLavaWebhook_DuplicateNoop covers PAY-04 — identical natural-key
// payload delivered twice triggers side effects once, returns 200 both times.
func TestHandleLavaWebhook_DuplicateNoop(t *testing.T) {
	db := setupWebhookTestDB(t)
	contractID := "contract-dup-1"
	fix := seedWebhookFixture(t, db, "alice@example.com", contractID, "lava-off-dup")
	_ = fix

	app := mkWebhookApp(t, db, "good-secret", "")
	payload := `{"eventType":"payment.success","contractId":"contract-dup-1","timestamp":"2026-05-23T10:00:00Z","amount":5.0,"currency":"USD"}`

	first := deliverWebhook(t, app, "good-secret", payload)
	if first.Status != 200 {
		t.Errorf("first delivery expected 200, got %d body=%s", first.Status, first.Body)
	}
	second := deliverWebhook(t, app, "good-secret", payload)
	if second.Status != 200 {
		t.Errorf("PAY-04: duplicate delivery must also return 200, got %d body=%s", second.Status, second.Body)
	}
	// Exactly one event row in lava_webhook_events for this natural key.
	var n int64
	_ = db.Model(&model.LavaWebhookEvent{}).Where("contract_id = ? AND event_type = ?", contractID, "payment.success").Count(&n).Error
	if n != 1 {
		t.Errorf("PAY-04: expected exactly 1 event row, got %d", n)
	}
	// Exactly one lava_contracts row (UpsertLavaContract idempotent on contract_id).
	var ncon int64
	_ = db.Model(&model.LavaContract{}).Where("contract_id = ?", contractID).Count(&ncon).Error
	if ncon != 1 {
		t.Errorf("PAY-04: expected exactly 1 lava_contracts row, got %d", ncon)
	}
	// Invoice flipped to paid exactly once.
	var inv model.Invoice
	if err := db.Where("lava_invoice_id = ?", contractID).First(&inv).Error; err != nil {
		t.Fatalf("reload invoice: %v", err)
	}
	if inv.Status != "paid" {
		t.Errorf("expected invoice.status=paid after first delivery, got %q", inv.Status)
	}
}

// TestHandleLavaWebhook_AllEvents covers PAY-03 — all 5 event types dispatch
// to the correct branch and produce the expected side effect.
func TestHandleLavaWebhook_AllEvents(t *testing.T) {
	t.Run("payment.success", func(t *testing.T) {
		db := setupWebhookTestDB(t)
		fix := seedWebhookFixture(t, db, "alice@example.com", "ctr-ps", "lava-off-ps")
		app := mkWebhookApp(t, db, "secret", "")
		body := `{"eventType":"payment.success","contractId":"ctr-ps","timestamp":"2026-05-23T10:00:00Z","amount":5,"currency":"USD"}`
		got := deliverWebhook(t, app, "secret", body)
		if got.Status != 200 {
			t.Fatalf("expected 200, got %d body=%s", got.Status, got.Body)
		}
		var u model.User
		if err := db.First(&u, "id = ?", fix.UserID).Error; err != nil {
			t.Fatalf("reload user: %v", err)
		}
		if u.SubscriptionTier != "pro" {
			t.Errorf("payment.success must grant pro, got %q", u.SubscriptionTier)
		}
		if u.PlanID != fix.ProPlanID {
			t.Errorf("payment.success must set plan_id=pro plan, got %q", u.PlanID)
		}
		if u.SubscriptionExpiresAt == nil {
			t.Errorf("payment.success must set subscription_expires_at")
		}
	})

	t.Run("subscription.recurring.payment.success", func(t *testing.T) {
		db := setupWebhookTestDB(t)
		fix := seedWebhookFixture(t, db, "alice@example.com", "ctr-rs-parent", "lava-off-rs")
		// Seed parent contract + an existing future expires_at to verify
		// extension preserves paid-for time.
		oldExp := time.Now().Add(15 * 24 * time.Hour)
		if err := db.Create(&model.LavaContract{
			ID: uuid.NewString(), UserID: fix.UserID, ContractID: "ctr-rs-parent",
			OfferID: fix.LavaOfferID, Plan: "pro", Periodicity: "MONTHLY", Currency: "USD",
			IsActive: true, ExpiresAt: &oldExp,
		}).Error; err != nil {
			t.Fatalf("seed parent contract: %v", err)
		}
		app := mkWebhookApp(t, db, "secret", "")
		body := `{"eventType":"subscription.recurring.payment.success","contractId":"ctr-rs-child","parentContractId":"ctr-rs-parent","timestamp":"2026-05-23T10:00:00Z","amount":5,"currency":"USD"}`
		got := deliverWebhook(t, app, "secret", body)
		if got.Status != 200 {
			t.Fatalf("expected 200, got %d body=%s", got.Status, got.Body)
		}
		// Renewal child contract exists with parent_contract_id set.
		var child model.LavaContract
		if err := db.Where("contract_id = ?", "ctr-rs-child").First(&child).Error; err != nil {
			t.Fatalf("reload child contract: %v", err)
		}
		if child.ParentContractID == nil || *child.ParentContractID != "ctr-rs-parent" {
			t.Errorf("expected parent_contract_id=ctr-rs-parent, got %v", child.ParentContractID)
		}
		// User's expires_at is now > the old one.
		var u model.User
		if err := db.First(&u, "id = ?", fix.UserID).Error; err != nil {
			t.Fatalf("reload user: %v", err)
		}
		if u.SubscriptionExpiresAt == nil || !u.SubscriptionExpiresAt.After(oldExp) {
			t.Errorf("expected subscription_expires_at > %v, got %v", oldExp, u.SubscriptionExpiresAt)
		}
	})

	t.Run("payment.failed", func(t *testing.T) {
		db := setupWebhookTestDB(t)
		fix := seedWebhookFixture(t, db, "alice@example.com", "ctr-pf", "lava-off-pf")
		app := mkWebhookApp(t, db, "secret", "")
		body := `{"eventType":"payment.failed","contractId":"ctr-pf","timestamp":"2026-05-23T10:00:00Z","amount":5,"currency":"USD","errorMessage":"declined"}`
		got := deliverWebhook(t, app, "secret", body)
		if got.Status != 200 {
			t.Fatalf("expected 200, got %d body=%s", got.Status, got.Body)
		}
		// Invoice flipped to failed.
		var inv model.Invoice
		if err := db.First(&inv, "id = ?", fix.InvoiceID).Error; err != nil {
			t.Fatalf("reload invoice: %v", err)
		}
		if inv.Status != "failed" {
			t.Errorf("expected invoice.status=failed, got %q", inv.Status)
		}
		// User STILL on free — payment.failed doesn't grant anything.
		var u model.User
		if err := db.First(&u, "id = ?", fix.UserID).Error; err != nil {
			t.Fatalf("reload user: %v", err)
		}
		if u.SubscriptionTier != "free" {
			t.Errorf("payment.failed must not change tier; expected free, got %q", u.SubscriptionTier)
		}
	})

	t.Run("subscription.recurring.payment.failed", func(t *testing.T) {
		db := setupWebhookTestDB(t)
		fix := seedWebhookFixture(t, db, "alice@example.com", "ctr-rf", "lava-off-rf")
		// Seed an active parent contract + an active subscription row with lava_contract_id set.
		if err := db.Create(&model.LavaContract{
			ID: uuid.NewString(), UserID: fix.UserID, ContractID: "ctr-rf-parent",
			OfferID: fix.LavaOfferID, Plan: "pro", Periodicity: "MONTHLY", Currency: "USD",
			IsActive: true,
		}).Error; err != nil {
			t.Fatalf("seed parent contract: %v", err)
		}
		lavaCID := "ctr-rf-parent"
		if err := db.Create(&model.Subscription{
			ID: uuid.NewString(), UserID: fix.UserID, Plan: "pro",
			LavaContractID: &lavaCID, IsActive: true,
		}).Error; err != nil {
			t.Fatalf("seed subscription: %v", err)
		}
		// Bump user to pro so we can assert tier is NOT downgraded.
		if err := db.Exec(`UPDATE users SET subscription_tier='pro' WHERE id=?`, fix.UserID).Error; err != nil {
			t.Fatalf("bump user pro: %v", err)
		}
		app := mkWebhookApp(t, db, "secret", "")
		body := `{"eventType":"subscription.recurring.payment.failed","contractId":"ctr-rf-attempt","parentContractId":"ctr-rf-parent","timestamp":"2026-05-23T10:00:00Z","amount":5,"currency":"USD"}`
		got := deliverWebhook(t, app, "secret", body)
		if got.Status != 200 {
			t.Fatalf("expected 200, got %d body=%s", got.Status, got.Body)
		}
		// Tier UNCHANGED — cron handles downgrade.
		var u model.User
		if err := db.First(&u, "id = ?", fix.UserID).Error; err != nil {
			t.Fatalf("reload user: %v", err)
		}
		if u.SubscriptionTier != "pro" {
			t.Errorf("recurring.failed must NOT downgrade tier; expected pro, got %q", u.SubscriptionTier)
		}
	})

	t.Run("subscription.cancelled", func(t *testing.T) {
		db := setupWebhookTestDB(t)
		fix := seedWebhookFixture(t, db, "alice@example.com", "ctr-sc", "lava-off-sc")
		if err := db.Create(&model.LavaContract{
			ID: uuid.NewString(), UserID: fix.UserID, ContractID: "ctr-sc",
			OfferID: fix.LavaOfferID, Plan: "pro", Periodicity: "MONTHLY", Currency: "USD",
			IsActive: true,
		}).Error; err != nil {
			t.Fatalf("seed contract: %v", err)
		}
		// Bump user to pro to verify tier untouched.
		if err := db.Exec(`UPDATE users SET subscription_tier='pro' WHERE id=?`, fix.UserID).Error; err != nil {
			t.Fatalf("bump user pro: %v", err)
		}
		app := mkWebhookApp(t, db, "secret", "")
		body := `{"eventType":"subscription.cancelled","contractId":"ctr-sc","cancelledAt":"2026-05-23T10:00:00Z","willExpireAt":"2026-06-23T10:00:00Z"}`
		got := deliverWebhook(t, app, "secret", body)
		if got.Status != 200 {
			t.Fatalf("expected 200, got %d body=%s", got.Status, got.Body)
		}
		var c model.LavaContract
		if err := db.Where("contract_id = ?", "ctr-sc").First(&c).Error; err != nil {
			t.Fatalf("reload contract: %v", err)
		}
		if c.CancelledAt == nil {
			t.Errorf("expected cancelled_at set after subscription.cancelled")
		}
		// Tier untouched.
		var u model.User
		if err := db.First(&u, "id = ?", fix.UserID).Error; err != nil {
			t.Fatalf("reload user: %v", err)
		}
		if u.SubscriptionTier != "pro" {
			t.Errorf("subscription.cancelled must NOT downgrade tier; expected pro, got %q", u.SubscriptionTier)
		}
	})
}

// TestHandleLavaWebhook_ProcessingError_Returns500 covers PAY-05 — when the
// dispatched handler errors mid-processing, the wrapper returns 500 so lava
// retries, and the event row stays with `error` populated for forensics.
//
// Induce-failure approach: deliver payment.success for a contractId that
// has NO matching invoice — handleLavaPaymentSuccess returns an error wrapping
// repository.ErrNotFound, which the wrapper converts to 500.
func TestHandleLavaWebhook_ProcessingError_Returns500(t *testing.T) {
	db := setupWebhookTestDB(t)
	// Seed plans + a user, but NO invoice for the contractId we'll deliver.
	freeID := uuid.NewString()
	proID := uuid.NewString()
	for _, p := range []model.Plan{
		{ID: freeID, Code: "free", Name: "Free", MaxDevices: 1, MaxServers: 3, IsActive: true, IsSystem: true},
		{ID: proID, Code: "pro", Name: "Pro", MaxDevices: 3, MaxServers: -1, IsActive: true},
	} {
		if err := db.Create(&p).Error; err != nil {
			t.Fatalf("seed plan: %v", err)
		}
	}
	app := mkWebhookApp(t, db, "secret", "")
	body := `{"eventType":"payment.success","contractId":"ctr-orphan","timestamp":"2026-05-23T10:00:00Z","amount":5,"currency":"USD"}`
	got := deliverWebhook(t, app, "secret", body)
	if got.Status != 500 {
		t.Errorf("PAY-05: orphan contractId must surface 500 to lava (triggers retry); got %d body=%s", got.Status, got.Body)
	}
	// Event row still persists with `error` populated.
	var ev model.LavaWebhookEvent
	if err := db.Where("contract_id = ?", "ctr-orphan").First(&ev).Error; err != nil {
		t.Fatalf("reload event: %v", err)
	}
	if ev.Error == nil || *ev.Error == "" {
		t.Errorf("expected event.error populated for forensics, got %v", ev.Error)
	}
}

// TestHandleLavaWebhook_TierFromOfferIDNotClient covers PAY-08 — even if the
// payload stuffs `plan:"root"` or claims another plan, tier is derived strictly
// from the invoice → offer → plan chain.
//
// Setup: pro plan + offer pinned to that plan. Payload includes nonsense
// `product.title` / `amount` — handler ignores them. Result: user gets the
// pro plan via the offer chain, NOT some attacker-supplied tier.
func TestHandleLavaWebhook_TierFromOfferIDNotClient(t *testing.T) {
	db := setupWebhookTestDB(t)
	fix := seedWebhookFixture(t, db, "alice@example.com", "ctr-tier", "lava-off-tier")
	app := mkWebhookApp(t, db, "secret", "")
	// Payload contains nonsense fields meant to subvert tier resolution.
	// The handler MUST ignore them and derive tier from invoice.offer_id chain.
	body := `{"eventType":"payment.success","contractId":"ctr-tier","timestamp":"2026-05-23T10:00:00Z","amount":99999,"currency":"USD","product":{"id":"root","title":"root"},"buyer":{"email":"alice@example.com"}}`
	got := deliverWebhook(t, app, "secret", body)
	if got.Status != 200 {
		t.Fatalf("expected 200, got %d body=%s", got.Status, got.Body)
	}
	var u model.User
	if err := db.First(&u, "id = ?", fix.UserID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	// User got the pro plan from the offer-chain — NOT "root" or anything else.
	if u.SubscriptionTier != "pro" {
		t.Errorf("PAY-08: tier must derive from offer chain (pro), got %q", u.SubscriptionTier)
	}
	if u.PlanID != fix.ProPlanID {
		t.Errorf("PAY-08: plan_id must derive from offer.plan_id; expected %q, got %q", fix.ProPlanID, u.PlanID)
	}
	// And the lava_contract.Plan field carries the FindPlanByID-resolved plan.Code,
	// NOT some payload-supplied value.
	var c model.LavaContract
	if err := db.Where("contract_id = ?", "ctr-tier").First(&c).Error; err != nil {
		t.Fatalf("reload contract: %v", err)
	}
	if c.Plan != "pro" {
		t.Errorf("PAY-08: lava_contracts.plan must come from FindPlanByID(offer.plan_id); expected pro, got %q", c.Plan)
	}
}

// TestHandleLavaWebhook_ExpiresAt_FirstAndRenewal covers PAY-09 — first
// payment.success populates expires_at; recurring.success extends it.
func TestHandleLavaWebhook_ExpiresAt_FirstAndRenewal(t *testing.T) {
	db := setupWebhookTestDB(t)
	fix := seedWebhookFixture(t, db, "alice@example.com", "ctr-exp", "lava-off-exp")
	app := mkWebhookApp(t, db, "secret", "")

	// First delivery: payment.success.
	body1 := `{"eventType":"payment.success","contractId":"ctr-exp","timestamp":"2026-05-23T10:00:00Z","amount":5,"currency":"USD"}`
	r1 := deliverWebhook(t, app, "secret", body1)
	if r1.Status != 200 {
		t.Fatalf("first delivery expected 200, got %d body=%s", r1.Status, r1.Body)
	}
	var u model.User
	if err := db.First(&u, "id = ?", fix.UserID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if u.SubscriptionExpiresAt == nil {
		t.Fatalf("PAY-09: first payment.success must populate subscription_expires_at")
	}
	firstExp := *u.SubscriptionExpiresAt

	// Second delivery: subscription.recurring.payment.success — extend by one period.
	body2 := `{"eventType":"subscription.recurring.payment.success","contractId":"ctr-exp-renewal","parentContractId":"ctr-exp","timestamp":"2026-06-23T10:00:00Z","amount":5,"currency":"USD"}`
	r2 := deliverWebhook(t, app, "secret", body2)
	if r2.Status != 200 {
		t.Fatalf("renewal delivery expected 200, got %d body=%s", r2.Status, r2.Body)
	}
	if err := db.First(&u, "id = ?", fix.UserID).Error; err != nil {
		t.Fatalf("reload user2: %v", err)
	}
	if u.SubscriptionExpiresAt == nil {
		t.Fatalf("PAY-09: renewal must keep subscription_expires_at non-nil")
	}
	if !u.SubscriptionExpiresAt.After(firstExp) {
		t.Errorf("PAY-09: renewal must extend expires_at; first=%v new=%v", firstExp, *u.SubscriptionExpiresAt)
	}
}

// TestHandleLavaWebhook_RecurringFailed_FlipsBothRows covers the D-19 BLOCKER #1
// fix — both subscriptions.is_active AND lava_contracts.is_active flip to
// false atomically when recurring.payment.failed lands.
func TestHandleLavaWebhook_RecurringFailed_FlipsBothRows(t *testing.T) {
	db := setupWebhookTestDB(t)
	fix := seedWebhookFixture(t, db, "alice@example.com", "ctr-flip", "lava-off-flip")

	parentID := "ctr-flip-parent"
	// Seed active parent contract.
	contractRow := &model.LavaContract{
		ID: uuid.NewString(), UserID: fix.UserID, ContractID: parentID,
		OfferID: fix.LavaOfferID, Plan: "pro", Periodicity: "MONTHLY", Currency: "USD",
		IsActive: true,
	}
	if err := db.Create(contractRow).Error; err != nil {
		t.Fatalf("seed parent contract: %v", err)
	}
	// Seed active subscription row keyed by the parent contract id.
	lavaCID := parentID
	subRow := &model.Subscription{
		ID: uuid.NewString(), UserID: fix.UserID, Plan: "pro",
		LavaContractID: &lavaCID, IsActive: true,
	}
	if err := db.Create(subRow).Error; err != nil {
		t.Fatalf("seed subscription: %v", err)
	}

	app := mkWebhookApp(t, db, "secret", "")
	body := `{"eventType":"subscription.recurring.payment.failed","contractId":"ctr-flip-attempt","parentContractId":"ctr-flip-parent","timestamp":"2026-05-23T10:00:00Z","amount":5,"currency":"USD"}`
	got := deliverWebhook(t, app, "secret", body)
	if got.Status != 200 {
		t.Fatalf("expected 200, got %d body=%s", got.Status, got.Body)
	}

	// Assert BOTH rows flipped to is_active=false in the same tx.
	var contractReload model.LavaContract
	if err := db.Where("contract_id = ?", parentID).First(&contractReload).Error; err != nil {
		t.Fatalf("reload contract: %v", err)
	}
	if contractReload.IsActive {
		t.Errorf("D-19 BLOCKER #1: lava_contracts.is_active must flip to false, got true")
	}
	var subReload model.Subscription
	if err := db.Where("id = ?", subRow.ID).First(&subReload).Error; err != nil {
		t.Fatalf("reload subscription: %v", err)
	}
	if subReload.IsActive {
		t.Errorf("D-19 BLOCKER #1: subscriptions.is_active must flip to false, got true")
	}
}

// TestHandleLavaWebhook_NaturalKey_NoTimestampCollision covers CR-02 —
// two payloads with the SAME (event_type, contract_id) and BOTH missing
// `timestamp` AND `cancelledAt` must dedup on the second delivery. Without
// the migration 021 sentinel ('no-timestamp'), the COALESCE third column
// resolves to NULL on each insert and Postgres treats NULL<>NULL in unique
// indexes — both inserts would succeed, breaking idempotency. The SQLite
// test schema mirrors the production COALESCE rule (see setupWebhookTestDB).
func TestHandleLavaWebhook_NaturalKey_NoTimestampCollision(t *testing.T) {
	db := setupWebhookTestDB(t)
	// Seed plans/user/contract so payment.failed has an invoice to act on
	// (handler logs Warn for missing invoice but returns nil — we just need
	// the natural-key dedup path to fire).
	_ = seedWebhookFixture(t, db, "alice@example.com", "ctr-nonull", "lava-off-nonull")

	app := mkWebhookApp(t, db, "secret", "")
	// Payload lacks BOTH `timestamp` AND `cancelledAt`. payment.failed branch
	// is benign-on-missing-invoice so this exercises the dedup-only path.
	body := `{"eventType":"payment.failed","contractId":"ctr-nonull-key","amount":5,"currency":"USD"}`

	r1 := deliverWebhook(t, app, "secret", body)
	if r1.Status != 200 {
		t.Fatalf("first delivery expected 200, got %d body=%s", r1.Status, r1.Body)
	}
	r2 := deliverWebhook(t, app, "secret", body)
	if r2.Status != 200 {
		t.Fatalf("duplicate delivery expected 200, got %d body=%s", r2.Status, r2.Body)
	}

	// Exactly ONE event row must exist — proves the sentinel collision works.
	var n int64
	if err := db.Model(&model.LavaWebhookEvent{}).
		Where("contract_id = ? AND event_type = ?", "ctr-nonull-key", "payment.failed").
		Count(&n).Error; err != nil {
		t.Fatalf("count events: %v", err)
	}
	if n != 1 {
		t.Errorf("CR-02: NULL-hole regression — expected exactly 1 row for no-timestamp payload, got %d", n)
	}
}

// TestHandleLavaWebhook_NaturalKey_DistinctTimestampsInsertBoth covers the
// inverse: two events sharing (event_type, contract_id) but with DIFFERENT
// `timestamp` values must BOTH insert — proves the natural key still
// discriminates real lava retries that carry distinct timestamps.
func TestHandleLavaWebhook_NaturalKey_DistinctTimestampsInsertBoth(t *testing.T) {
	db := setupWebhookTestDB(t)
	fix := seedWebhookFixture(t, db, "alice@example.com", "ctr-distinct", "lava-off-distinct")
	_ = fix

	app := mkWebhookApp(t, db, "secret", "")
	// First payload: payment.failed at t=10:00:00Z
	body1 := `{"eventType":"payment.failed","contractId":"ctr-distinct-key","timestamp":"2026-05-23T10:00:00Z","amount":5,"currency":"USD"}`
	// Second payload: SAME natural-key-prefix but timestamp differs by 1s.
	body2 := `{"eventType":"payment.failed","contractId":"ctr-distinct-key","timestamp":"2026-05-23T10:00:01Z","amount":5,"currency":"USD"}`

	if got := deliverWebhook(t, app, "secret", body1); got.Status != 200 {
		t.Fatalf("first delivery expected 200, got %d body=%s", got.Status, got.Body)
	}
	if got := deliverWebhook(t, app, "secret", body2); got.Status != 200 {
		t.Fatalf("second delivery expected 200, got %d body=%s", got.Status, got.Body)
	}

	// BOTH rows must exist — distinct timestamps are not deduplicated.
	var n int64
	if err := db.Model(&model.LavaWebhookEvent{}).
		Where("contract_id = ? AND event_type = ?", "ctr-distinct-key", "payment.failed").
		Count(&n).Error; err != nil {
		t.Fatalf("count events: %v", err)
	}
	if n != 2 {
		t.Errorf("CR-02: natural-key discriminator broken — distinct timestamps must produce 2 rows, got %d", n)
	}
}

// silence unused-import warnings when subsets of tests are commented out.
var (
	_ = json.Marshal
	_ = bytes.NewBuffer
)
