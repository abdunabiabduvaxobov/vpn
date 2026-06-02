package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"vpnapp/server/api/internal/middleware"
	"vpnapp/server/api/internal/model"
	"vpnapp/server/api/internal/repository"

	"github.com/google/uuid"

	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// ctxBG is a tiny alias so the table-test bodies stay terse.
func ctxBG() context.Context { return context.Background() }

// mustSeedBareUser inserts a minimal free-tier user and returns its id, for the
// HARD-02 per-user-UUID tests that only need a user row to own an identity.
func mustSeedBareUser(t *testing.T, db *gorm.DB) string {
	t.Helper()
	id := uuid.NewString()
	if err := db.Create(&model.User{
		ID: id, FullName: "u", SubscriptionTier: "free", PlanID: uuid.NewString(),
		AuthProvider: "guest",
	}).Error; err != nil {
		t.Fatalf("seed bare user: %v", err)
	}
	return id
}

// activeVlessUUID returns the single active vless_uuid for a user (and asserts
// there is exactly one), so a rotation can be verified as old->inactive,
// new->active.
func activeVlessUUID(t *testing.T, db *gorm.DB, userID string) string {
	t.Helper()
	var rows []model.UserVlessIdentity
	if err := db.Where("user_id = ? AND is_active = ?", userID, true).Find(&rows).Error; err != nil {
		t.Fatalf("query active identities: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected exactly 1 active identity for user %s, got %d", userID, len(rows))
	}
	return rows[0].VlessUUID
}

// TestServersVLESS_PerUserUUIDAllocationAndRotation pins the HARD-02 API side
// (migration 026 + per-user VLESS UUID allocation/rotation + the
// /internal/servers/:id/vless-clients active-set endpoint).
//
// Three properties:
//  1. ISOLATION: two users get DIFFERENT UUIDs.
//  2. ROTATION: a lava payment.success rotates the user's UUID — the prior
//     identity row flips is_active=false and a NEW active UUID is issued.
//  3. ACTIVE-SET: GET /internal/servers/:id/vless-clients returns the active
//     UUID set and is rejected without the internal secret.
func TestServersVLESS_PerUserUUIDAllocationAndRotation(t *testing.T) {
	t.Run("isolation: two users get different UUIDs", func(t *testing.T) {
		db := setupWebhookTestDB(t)

		u1 := mustSeedBareUser(t, db)
		u2 := mustSeedBareUser(t, db)

		id1, err := repository.GetOrCreateActiveVlessUUID(ctxBG(), db, u1)
		if err != nil {
			t.Fatalf("alloc u1: %v", err)
		}
		id2, err := repository.GetOrCreateActiveVlessUUID(ctxBG(), db, u2)
		if err != nil {
			t.Fatalf("alloc u2: %v", err)
		}
		if id1 == "" || id2 == "" {
			t.Fatalf("empty UUID(s): %q %q", id1, id2)
		}
		if id1 == id2 {
			t.Errorf("ISOLATION: two users must get different UUIDs, both got %q", id1)
		}
		// Idempotent: re-fetching the same user returns the SAME active UUID.
		id1again, err := repository.GetOrCreateActiveVlessUUID(ctxBG(), db, u1)
		if err != nil {
			t.Fatalf("re-alloc u1: %v", err)
		}
		if id1again != id1 {
			t.Errorf("re-fetch must return the same active UUID, got %q want %q", id1again, id1)
		}
	})

	t.Run("rotation: lava payment.success rotates the UUID", func(t *testing.T) {
		db := setupWebhookTestDB(t)
		fix := seedWebhookFixture(t, db, "rot@example.com", "ctr-rot", "lava-off-rot")

		// Allocate the user's initial active UUID.
		before, err := repository.GetOrCreateActiveVlessUUID(ctxBG(), db, fix.UserID)
		if err != nil {
			t.Fatalf("initial alloc: %v", err)
		}

		// Deliver payment.success — the handler rotates the UUID inside the same
		// WithUserLock tx as the tier grant.
		app := mkWebhookApp(t, db, "secret", "")
		body := `{"eventType":"payment.success","contractId":"ctr-rot","timestamp":"2026-05-23T10:00:00Z","amount":5,"currency":"USD"}`
		got := deliverWebhook(t, app, "secret", body)
		if got.Status != 200 {
			t.Fatalf("payment.success expected 200, got %d body=%s", got.Status, got.Body)
		}

		after := activeVlessUUID(t, db, fix.UserID)
		if after == before {
			t.Errorf("ROTATION: payment.success must issue a NEW UUID, still %q", after)
		}
		// The prior identity must now be inactive (revoked).
		var oldRow model.UserVlessIdentity
		if err := db.Where("vless_uuid = ?", before).First(&oldRow).Error; err != nil {
			t.Fatalf("reload old identity: %v", err)
		}
		if oldRow.IsActive {
			t.Errorf("ROTATION: prior identity must be is_active=false after rotate")
		}
		if oldRow.RevokedAt == nil {
			t.Errorf("ROTATION: prior identity must have revoked_at set")
		}
	})

	t.Run("active-set endpoint returns active UUIDs and is internal-secret gated", func(t *testing.T) {
		db := setupWebhookTestDB(t)

		u1 := mustSeedBareUser(t, db)
		u2 := mustSeedBareUser(t, db)
		id1, _ := repository.GetOrCreateActiveVlessUUID(ctxBG(), db, u1)
		id2, _ := repository.GetOrCreateActiveVlessUUID(ctxBG(), db, u2)

		app := fiber.New()
		internal := app.Group("/internal", middleware.InternalSecret("int-secret", zap.NewNop()))
		internal.Get("/servers/:id/vless-clients", ListServerVlessClients(zap.NewNop(), db))

		// Missing secret → 401 (spoofing mitigation T-08-02c).
		reqBad := httptest.NewRequest("GET", "/internal/servers/srv-1/vless-clients", nil)
		respBad, err := app.Test(reqBad)
		if err != nil {
			t.Fatalf("Test bad: %v", err)
		}
		if respBad.StatusCode != 401 {
			t.Errorf("active-set without internal secret must be 401, got %d", respBad.StatusCode)
		}

		// Valid secret → 200 with both active UUIDs + an ETag.
		reqOK := httptest.NewRequest("GET", "/internal/servers/srv-1/vless-clients", nil)
		reqOK.Header.Set("X-Internal-Secret", "int-secret")
		respOK, err := app.Test(reqOK)
		if err != nil {
			t.Fatalf("Test ok: %v", err)
		}
		if respOK.StatusCode != 200 {
			t.Fatalf("active-set with secret expected 200, got %d", respOK.StatusCode)
		}
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(respOK.Body)
		var payload struct {
			UUIDs []string `json:"uuids"`
			ETag  string   `json:"etag"`
		}
		if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
			t.Fatalf("decode active-set: %v body=%s", err, buf.String())
		}
		if payload.ETag == "" {
			t.Errorf("active-set must return a non-empty ETag")
		}
		set := map[string]bool{}
		for _, u := range payload.UUIDs {
			set[u] = true
		}
		if !set[id1] || !set[id2] {
			t.Errorf("active-set must include both users' UUIDs %q %q, got %v", id1, id2, payload.UUIDs)
		}
	})
}
