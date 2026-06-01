// TestGuestOnboardingSetsPlanID is the quick-260602-214 regression guard for the
// launch-blocking guest-onboarding 500. users.plan_id is `uuid NOT NULL` (migration
// 019) with no DB default, and model.User.PlanID is a non-pointer string with no
// `default` GORM tag — so before the fix GuestLogin inserted plan_id='' and Postgres
// rejected the INSERT with "invalid input syntax for type uuid" (SQLSTATE 22P02).
//
// SQLite (used by the handler unit tests) silently accepts plan_id='' and so cannot
// surface this failure; only a real Postgres with migration 019 applied reproduces it.
// This test therefore drives GuestLogin over a live Postgres (via testutil.StartPostgres,
// which applies ALL migrations including 019's is_system 'free' seed) and asserts BOTH
// that POST /auth/guest succeeds AND that the persisted guest row carries the system
// plan_id. Against the pre-fix code this test fails (500 / empty plan_id).
//
// Plain (untagged) test file in package integration_test so it runs under the default
// `go test ./integration/`. It skips cleanly when Docker/testcontainers is unavailable.
package integration_test

import (
	"net/http/httptest"
	"testing"

	"vpnapp/server/api/internal/config"
	"vpnapp/server/api/internal/handler"
	"vpnapp/server/api/internal/testutil"

	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
)

func TestGuestOnboardingSetsPlanID(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: -short set")
	}
	db := testutil.StartPostgres(t)
	if db == nil {
		return // Docker/testcontainers unavailable — StartPostgres already skipped
	}

	// Migration 019 seeds the single is_system='free' plan; FindSystemPlanID (and
	// thus GuestLogin) resolves it. Capture the expected id for the row assertion.
	var systemPlanID string
	if err := mustQueryRow(t, db, `SELECT id FROM plans WHERE is_system = TRUE`).Scan(&systemPlanID); err != nil {
		t.Fatalf("lookup system plan id: %v", err)
	}
	if systemPlanID == "" {
		t.Fatal("expected migration 019 to seed a non-empty is_system plan id")
	}

	cfg := &config.Config{JWTSecret: "test-secret-32-bytes-at-minimum!!"}
	app := fiber.New()
	app.Post("/auth/guest", handler.GuestLogin(zap.NewNop(), db, cfg))

	// Empty body — the guest request body is optional; the fresh-user path mints a
	// brand-new anonymous account. This is the exact path that 500'd before the fix.
	req := httptest.NewRequest(fiber.MethodPost, "/auth/guest", nil)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// GuestLogin returns 201 Created on the fresh-user path; accept 200 as well so
	// the assertion does not over-couple to the status code.
	if resp.StatusCode != fiber.StatusOK && resp.StatusCode != fiber.StatusCreated {
		body := make([]byte, 512)
		n, _ := resp.Body.Read(body)
		t.Fatalf("POST /auth/guest: expected 200/201, got %d (body: %s) — this is the SQLSTATE 22P02 regression",
			resp.StatusCode, string(body[:n]))
	}

	// Core guard: the persisted guest row must carry the system plan_id. SQLite
	// would accept plan_id='' and mask the bug; real Postgres + this assertion catches it.
	var persistedPlanID string
	if err := mustQueryRow(t, db,
		`SELECT plan_id FROM users WHERE full_name LIKE 'guest_%' ORDER BY created_at DESC LIMIT 1`,
	).Scan(&persistedPlanID); err != nil {
		t.Fatalf("read minted guest user plan_id: %v", err)
	}
	if persistedPlanID == "" {
		t.Fatal("minted guest user has empty plan_id — the bug is present")
	}
	if persistedPlanID != systemPlanID {
		t.Fatalf("minted guest user plan_id = %q, want system plan id %q", persistedPlanID, systemPlanID)
	}
}
