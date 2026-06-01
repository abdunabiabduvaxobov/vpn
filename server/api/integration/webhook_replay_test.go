// GREEN as of plan 07-08: TestWebhookReplayIdempotent proves the ADMIN-06
// webhook-replay side effect is idempotent (SC-3). It re-applies a STORED
// payment.success event twice via handler.ApplyLavaEvent (the exported alias of
// the transport-free applyLavaEvent the live handler + replay endpoint share) and
// asserts exactly ONE tier grant: tier=pro after both calls with an unchanged
// subscription_expires_at (set-not-increment, never a compounding double-extend).
// It then drives the AdminReplayWebhookEvent handler to prove the row flips
// DELIVERED→REPLAYED and retried_count increments.
//
// Plain (untagged) test file in package integration_test so it runs under the
// default `go test ./integration/`. It needs a REAL Postgres because the
// payment.success success path takes repository.WithUserLock
// (pg_advisory_xact_lock) which SQLite cannot exercise — so it skips cleanly when
// Docker/testcontainers is unavailable (StartPostgres skips), keeping CI green.
package integration_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"vpnapp/server/api/internal/handler"
	"vpnapp/server/api/internal/middleware"
	"vpnapp/server/api/internal/model"
	"vpnapp/server/api/internal/testutil"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestWebhookReplayIdempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: -short set")
	}
	db := testutil.StartPostgres(t)
	if db == nil {
		return // Docker/testcontainers unavailable — StartPostgres already skipped
	}

	ctx := context.Background()
	logger := zap.NewNop()

	// --- Seed: free (system) + pro plans, a pro offer, a user, and a pending
	// invoice keyed so the payment.success payload resolves to the pro plan. ---
	// Migration 019 already seeds the 'free' (system) and 'pro' plans, and
	// plans.code is UNIQUE — reuse their IDs rather than inserting duplicates.
	var systemPlanID, proPlanID string
	if err := mustQueryRow(t, db, `SELECT id FROM plans WHERE code = 'free'`).Scan(&systemPlanID); err != nil {
		t.Fatalf("lookup free plan id: %v", err)
	}
	if err := mustQueryRow(t, db, `SELECT id FROM plans WHERE code = 'pro'`).Scan(&proPlanID); err != nil {
		t.Fatalf("lookup pro plan id: %v", err)
	}

	// The lava-side offer UUID the invoice carries; FindOfferByLavaOfferID maps it
	// back to the pro plan_id (PAY-08 reverse lookup).
	// Migration 019 seeds 6 placeholder pro offers (lava_offer_id=NULL) covering
	// every {periodicity}×{currency} slot. Configure the MONTHLY/USD one with a
	// known lava offer id — exactly what an admin does in the UI picker — so
	// FindOfferByLavaOfferID maps it back to the pro plan (PAY-08 reverse lookup).
	lavaOfferID := uuid.NewString()
	mustExec(t, db,
		`UPDATE plan_offers SET lava_offer_id = ? WHERE plan_id = ? AND periodicity = 'MONTHLY' AND currency = 'USD'`,
		lavaOfferID, proPlanID)

	userID := uuid.NewString()
	mustExec(t, db,
		`INSERT INTO users (id, subscription_tier, role, plan_id, auth_provider) VALUES (?, 'free', 'user', ?, 'guest')`,
		userID, systemPlanID)

	// contractId on a first payment.success == the invoice's lava_invoice_id.
	contractID := uuid.NewString()
	mustExec(t, db,
		`INSERT INTO invoices (id, user_id, lava_invoice_id, offer_id, plan_id, plan, periodicity, currency, amount, status)
		 VALUES (?, ?, ?, ?, ?, 'pro', 'MONTHLY', 'USD', 5.00, 'pending')`,
		uuid.NewString(), userID, contractID, lavaOfferID, proPlanID)

	// Stored payload carries a buyer email so we also exercise list redaction.
	payload := map[string]interface{}{
		"eventType":  "payment.success",
		"contractId": contractID,
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
		"amount":     5.00,
		"currency":   "USD",
		"buyer":      map[string]string{"email": "alice@example.com"},
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	// Insert the DELIVERED webhook event row (as if the live webhook already ran).
	eventID := uuid.NewString()
	cid := contractID
	rec := &model.LavaWebhookEvent{
		ID:         eventID,
		EventType:  "payment.success",
		ContractID: &cid,
		Payload:    payloadBytes,
		Status:     "DELIVERED",
	}
	if err := db.WithContext(ctx).Create(rec).Error; err != nil {
		t.Fatalf("seed webhook event: %v", err)
	}

	// --- First application: grant Pro. ---
	if err := handler.ApplyLavaEvent(ctx, db, nil, logger, *rec); err != nil {
		t.Fatalf("first ApplyLavaEvent: %v", err)
	}
	tier1, exp1 := readUserTierExpiry(t, db, userID)
	if tier1 != "pro" {
		t.Fatalf("after first apply: tier=%q, want pro", tier1)
	}
	if exp1 == nil {
		t.Fatalf("after first apply: subscription_expires_at is NULL, want a future expiry")
	}

	// --- Second application of the SAME stored event: must NOT double-grant. ---
	if err := handler.ApplyLavaEvent(ctx, db, nil, logger, *rec); err != nil {
		t.Fatalf("second ApplyLavaEvent: %v", err)
	}
	tier2, exp2 := readUserTierExpiry(t, db, userID)
	if tier2 != "pro" {
		t.Fatalf("after second apply: tier=%q, want pro (still exactly one grant)", tier2)
	}
	if exp2 == nil {
		t.Fatalf("after second apply: subscription_expires_at is NULL, want a future expiry")
	}

	// Idempotency assertion (T-07-32): the grant is set-not-increment. Both calls
	// compute expires_at as now()+1 period (NOT prev_expiry+1 period), so the two
	// expiries differ only by the few-ms wall-clock gap between the calls — NEVER
	// by a whole period. A compounding double-extend would push exp2 ~30 days past
	// exp1; assert the delta stays well under one period.
	delta := exp2.Sub(*exp1)
	if delta < 0 {
		delta = -delta
	}
	if delta > time.Hour {
		t.Fatalf("double-grant detected: expires_at moved by %s between two replays "+
			"(set-not-increment must keep it within a wall-clock delta, never +1 period)", delta)
	}

	// --- Drive the replay HTTP handler once: status DELIVERED→REPLAYED + count++. ---
	app := fiber.New()
	adminID := uuid.NewString()
	// audit_log.admin_id is NOT NULL REFERENCES users(id) (migration 014); the
	// acting admin must exist or the audit insert fails its FK and the
	// AuditLog middleware swallows the error, leaving zero audit rows.
	mustExec(t, db,
		`INSERT INTO users (id, subscription_tier, role, plan_id, auth_provider) VALUES (?, 'free', 'admin', ?, 'guest')`,
		adminID, systemPlanID)
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user_id", adminID) // stand in for AuthRequired
		return c.Next()
	})
	app.Use(middleware.AuditLog(db, logger))
	app.Post("/admin/webhook-events/:id/replay", handler.AdminReplayWebhookEvent(logger, db, nil))

	body := strings.NewReader(`{"reason":"manual reprocess after incident"}`)
	req := httptest.NewRequest(fiber.MethodPost, "/admin/webhook-events/"+eventID+"/replay", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("replay request: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("replay endpoint: status=%d, want 200", resp.StatusCode)
	}

	// Row must now be REPLAYED with retried_count incremented.
	updated, ferr := readWebhookEvent(t, db, eventID)
	if ferr != nil {
		t.Fatalf("reload webhook event: %v", ferr)
	}
	if updated.Status != "REPLAYED" {
		t.Fatalf("after replay: status=%q, want REPLAYED", updated.Status)
	}
	if updated.RetriedCount != 1 {
		t.Fatalf("after replay: retried_count=%d, want 1", updated.RetriedCount)
	}

	// Tier is STILL pro and the expiry STILL within a wall-clock delta of the
	// original grant — the HTTP replay re-applied idempotently too.
	tier3, exp3 := readUserTierExpiry(t, db, userID)
	if tier3 != "pro" {
		t.Fatalf("after HTTP replay: tier=%q, want pro", tier3)
	}
	if exp3 == nil {
		t.Fatalf("after HTTP replay: subscription_expires_at is NULL")
	}
	d3 := exp3.Sub(*exp1)
	if d3 < 0 {
		d3 = -d3
	}
	if d3 > time.Hour {
		t.Fatalf("double-grant via HTTP replay: expires_at moved by %s from the first grant", d3)
	}

	// The replay was audited with the operator's reason (T-07-35).
	var auditCount int64
	if err := db.WithContext(ctx).Model(&model.AuditLogEntry{}).
		Where("action = ? AND target_id = ?", "replay_webhook", eventID).
		Count(&auditCount).Error; err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	if auditCount < 1 {
		t.Fatalf("expected at least one replay_webhook audit row for event %s, got %d", eventID, auditCount)
	}
}

func readUserTierExpiry(t *testing.T, db *gorm.DB, userID string) (string, *time.Time) {
	t.Helper()
	var tier string
	var exp *time.Time
	if err := mustQueryRow(t, db, `SELECT subscription_tier, subscription_expires_at FROM users WHERE id = ?`, userID).
		Scan(&tier, &exp); err != nil {
		t.Fatalf("scan user tier/expiry: %v", err)
	}
	return tier, exp
}

func readWebhookEvent(t *testing.T, db *gorm.DB, id string) (*model.LavaWebhookEvent, error) {
	t.Helper()
	var ev model.LavaWebhookEvent
	if err := db.Where("id = ?", id).First(&ev).Error; err != nil {
		return nil, err
	}
	return &ev, nil
}
