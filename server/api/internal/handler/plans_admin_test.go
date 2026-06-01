package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"vpnapp/server/api/internal/model"

	"github.com/alicebob/miniredis/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// setupAdminPlansDB creates the minimum schema for plans-admin tests.
// Mirrors setupPaymentTestDB (same package) — uses SetMaxOpenConns(1) so
// :memory: state is visible across implicit GORM connection grabs, and
// includes randomblob() defaults so GORM's default:gen_random_uuid() tag
// (Postgres-only) doesn't leave id=NULL inside transactions.
func setupAdminPlansDB(t *testing.T) (*gorm.DB, *redis.Client) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db.DB: %v", err)
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
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE plan_servers (
			plan_id TEXT NOT NULL,
			server_id TEXT NOT NULL,
			PRIMARY KEY (plan_id, server_id)
		)`,
		`CREATE TABLE plan_offers (
			id TEXT PRIMARY KEY DEFAULT ` + uuidDefault + `,
			plan_id TEXT NOT NULL,
			periodicity TEXT NOT NULL,
			currency TEXT NOT NULL,
			amount REAL NOT NULL,
			lava_offer_id TEXT,
			is_active INTEGER NOT NULL DEFAULT 1,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE vpn_servers (
			id TEXT PRIMARY KEY,
			hostname TEXT NOT NULL,
			ip_address TEXT NOT NULL DEFAULT '',
			region TEXT NOT NULL DEFAULT '',
			city TEXT NOT NULL DEFAULT '',
			country_code TEXT NOT NULL DEFAULT '',
			country TEXT NOT NULL DEFAULT '',
			protocol TEXT NOT NULL DEFAULT 'vless',
			capacity INTEGER NOT NULL DEFAULT 500,
			is_active INTEGER NOT NULL DEFAULT 1,
			is_draining INTEGER NOT NULL DEFAULT 0,
			last_seen_at TIMESTAMP,
			current_load INTEGER NOT NULL DEFAULT 0,
			reality_public_key TEXT NOT NULL DEFAULT '',
			reality_short_id TEXT NOT NULL DEFAULT '',
			ws_enabled INTEGER NOT NULL DEFAULT 0,
			ws_host TEXT NOT NULL DEFAULT '',
			ws_path TEXT NOT NULL DEFAULT '',
			awg_public_key TEXT,
			awg_endpoint TEXT,
			awg_params TEXT,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
	}
	for _, s := range stmts {
		if err := db.Exec(s).Error; err != nil {
			t.Fatalf("DDL: %v", err)
		}
	}

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return db, rdb
}

// mkAdminPlansApp wires all 13 admin plan handlers onto a fresh Fiber app.
// No auth middleware — the actual /api/v1/admin group in cmd/main.go applies
// AuthRequired + AdminRequired + AuditLog before reaching these handlers.
func mkAdminPlansApp(t *testing.T, db *gorm.DB, rdb *redis.Client) *fiber.App {
	t.Helper()
	logger := zap.NewNop()
	app := fiber.New()
	app.Get("/api/v1/admin/plans", AdminListPlans(logger, db))
	app.Post("/api/v1/admin/plans", AdminCreatePlan(logger, db, rdb))
	app.Get("/api/v1/admin/plans/:id", AdminGetPlan(logger, db))
	app.Patch("/api/v1/admin/plans/:id", AdminUpdatePlan(logger, db, rdb))
	app.Delete("/api/v1/admin/plans/:id", AdminDeletePlan(logger, db, rdb))
	app.Put("/api/v1/admin/plans/:id/servers", AdminReplacePlanServers(logger, db, rdb))
	app.Post("/api/v1/admin/plans/:id/servers/:server_id", AdminAddPlanServer(logger, db, rdb))
	app.Delete("/api/v1/admin/plans/:id/servers/:server_id", AdminRemovePlanServer(logger, db, rdb))
	app.Get("/api/v1/admin/plans/:id/offers", AdminListPlanOffers(logger, db))
	app.Post("/api/v1/admin/plans/:id/offers", AdminCreatePlanOffer(logger, db, rdb))
	app.Patch("/api/v1/admin/plans/:id/offers/:offer_id", AdminUpdatePlanOffer(logger, db, rdb))
	app.Delete("/api/v1/admin/plans/:id/offers/:offer_id", AdminDeletePlanOffer(logger, db, rdb))
	app.Post("/api/v1/admin/plans/:id/offers/:offer_id/replace", AdminReplacePlanOffer(logger, db, rdb))
	return app
}

// seedSystemPlan inserts a system free plan and returns its id.
// Inserts with IsSystem=true then explicitly UPDATEs because GORM omits
// Go zero-value bool fields from INSERT and the DDL DEFAULT (0) would
// override an attempt to set it to true via Create. (Same trap that bit
// the 03-03 test suite — see plan_repo_test.go.)
func seedSystemPlan(t *testing.T, db *gorm.DB) string {
	t.Helper()
	id := uuid.NewString()
	if err := db.Create(&model.Plan{
		ID: id, Code: "free", Name: "Free", MaxDevices: 1, MaxServers: 3,
		IsActive: true, IsSystem: true, SortOrder: 0,
	}).Error; err != nil {
		t.Fatalf("seed system plan: %v", err)
	}
	// Force is_system=true (GORM dropped it on INSERT because IsSystem
	// is a bool field with a non-zero value but the column has a default;
	// belt-and-braces).
	if err := db.Model(&model.Plan{}).Where("id = ?", id).Update("is_system", true).Error; err != nil {
		t.Fatalf("force is_system: %v", err)
	}
	return id
}

// TestAdminPlansCRUD is the PAY-13 named test from 03-VALIDATION.md.
// Subtests cover the full CRUD cycle plus the D-32 §4 invariants:
//   - is_system is NOT settable via API (defence in depth)
//   - DELETE on system plan returns 403 EVEN WITH ?force=true
func TestAdminPlansCRUD(t *testing.T) {
	db, rdb := setupAdminPlansDB(t)
	app := mkAdminPlansApp(t, db, rdb)

	freeID := seedSystemPlan(t, db)

	t.Run("Create", func(t *testing.T) {
		body := `{"code":"trial","name":"Trial","description":"trial plan","max_devices":2,"max_servers":5,"speed_limit_mbps":10,"sort_order":5,"server_ids":[]}`
		req := httptest.NewRequest("POST", "/api/v1/admin/plans", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, _ := app.Test(req)
		if resp.StatusCode != 201 {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("Create: expected 201, got %d body=%s", resp.StatusCode, string(raw))
		}
		var got struct {
			Data model.Plan `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if got.Data.Code != "trial" || got.Data.Name != "Trial" {
			t.Errorf("Create: unexpected body %+v", got.Data)
		}
		if got.Data.IsSystem {
			t.Errorf("Create: new plan must NOT be is_system=true")
		}
	})

	t.Run("Get", func(t *testing.T) {
		// Get the system plan we seeded — full detail with servers + offers.
		req := httptest.NewRequest("GET", "/api/v1/admin/plans/"+freeID, nil)
		resp, _ := app.Test(req)
		if resp.StatusCode != 200 {
			t.Fatalf("Get: expected 200, got %d", resp.StatusCode)
		}
		var got struct {
			Data map[string]interface{} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if got.Data["code"] != "free" {
			t.Errorf("Get: expected code='free', got %v", got.Data["code"])
		}
	})

	t.Run("Patch", func(t *testing.T) {
		// Patch the system plan's name (allowed; only deactivation is refused).
		body := `{"name":"Free (renamed)"}`
		req := httptest.NewRequest("PATCH", "/api/v1/admin/plans/"+freeID, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, _ := app.Test(req)
		if resp.StatusCode != 200 {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("Patch: expected 200, got %d body=%s", resp.StatusCode, string(raw))
		}
		var reload model.Plan
		_ = db.Where("id = ?", freeID).First(&reload).Error
		if reload.Name != "Free (renamed)" {
			t.Errorf("Patch: name not updated, got %q", reload.Name)
		}
	})

	t.Run("Patch_System_Deactivate_403", func(t *testing.T) {
		body := `{"is_active":false}`
		req := httptest.NewRequest("PATCH", "/api/v1/admin/plans/"+freeID, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, _ := app.Test(req)
		if resp.StatusCode != 403 {
			t.Errorf("PATCH is_active=false on system plan: expected 403, got %d", resp.StatusCode)
		}
		// Confirm DB unchanged.
		var reload model.Plan
		_ = db.Where("id = ?", freeID).First(&reload).Error
		if !reload.IsActive {
			t.Errorf("system plan must remain is_active=true after 403 refusal; got %v", reload.IsActive)
		}
	})

	t.Run("Delete_System_403_EvenWithForce", func(t *testing.T) {
		// D-32 §4: system plan delete returns 403 EVEN WITH ?force=true.
		req := httptest.NewRequest("DELETE", "/api/v1/admin/plans/"+freeID+"?force=true", nil)
		resp, _ := app.Test(req)
		if resp.StatusCode != 403 {
			t.Errorf("D-32 §4: system plan delete with ?force=true must return 403, got %d", resp.StatusCode)
		}
		// Confirm DB unchanged.
		var reload model.Plan
		_ = db.Where("id = ?", freeID).First(&reload).Error
		if !reload.IsActive {
			t.Errorf("system plan must remain is_active=true after 403 force-delete refusal; got %v", reload.IsActive)
		}
	})

	t.Run("Delete_NonSystem_204_With_NoUsers", func(t *testing.T) {
		// Create a fresh non-system plan with no users, then delete it.
		nonSystemID := uuid.NewString()
		if err := db.Create(&model.Plan{
			ID: nonSystemID, Code: "ephemeral", Name: "Ephemeral",
			MaxDevices: 1, MaxServers: 1, IsActive: true, IsSystem: false,
		}).Error; err != nil {
			t.Fatalf("seed plan: %v", err)
		}
		req := httptest.NewRequest("DELETE", "/api/v1/admin/plans/"+nonSystemID, nil)
		resp, _ := app.Test(req)
		if resp.StatusCode != 200 {
			t.Errorf("Delete: expected 200, got %d", resp.StatusCode)
		}
		// Soft delete — row still exists but is_active=false.
		var reload model.Plan
		_ = db.Where("id = ?", nonSystemID).First(&reload).Error
		if reload.IsActive {
			t.Errorf("Delete: expected is_active=false, got %v", reload.IsActive)
		}
	})

	t.Run("Delete_409_OnActiveUsersWithoutForce", func(t *testing.T) {
		// Plan with an active user — should return 409 without ?force=true.
		planID := uuid.NewString()
		if err := db.Create(&model.Plan{
			ID: planID, Code: "withusers", Name: "WithUsers",
			MaxDevices: 1, MaxServers: 1, IsActive: true, IsSystem: false,
		}).Error; err != nil {
			t.Fatalf("seed plan: %v", err)
		}
		email := "user@example.com"
		if err := db.Create(&model.User{
			ID: uuid.NewString(), Email: &email, FullName: "u",
			SubscriptionTier: "free", PlanID: planID, AuthProvider: "google",
		}).Error; err != nil {
			t.Fatalf("seed user: %v", err)
		}
		req := httptest.NewRequest("DELETE", "/api/v1/admin/plans/"+planID, nil)
		resp, _ := app.Test(req)
		if resp.StatusCode != 409 {
			t.Errorf("Delete with active users (no force): expected 409, got %d", resp.StatusCode)
		}

		// Repeat with ?force=true — should succeed.
		req2 := httptest.NewRequest("DELETE", "/api/v1/admin/plans/"+planID+"?force=true", nil)
		resp2, _ := app.Test(req2)
		if resp2.StatusCode != 200 {
			t.Errorf("Delete with active users + ?force=true: expected 200, got %d", resp2.StatusCode)
		}
	})

	t.Run("IsSystem_Immutable_FromBody", func(t *testing.T) {
		// Body includes is_system:true — must be IGNORED (struct doesn't
		// have the field). The saved row must have is_system=false.
		body := `{"code":"another","name":"Another","description":"x","max_devices":1,"max_servers":1,"speed_limit_mbps":0,"is_system":true}`
		req := httptest.NewRequest("POST", "/api/v1/admin/plans", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, _ := app.Test(req)
		if resp.StatusCode != 201 {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 201, got %d body=%s", resp.StatusCode, string(raw))
		}
		var p model.Plan
		if err := db.Where("code = ?", "another").First(&p).Error; err != nil {
			t.Fatalf("reload: %v", err)
		}
		if p.IsSystem {
			t.Errorf("D-32 §4: is_system MUST NOT be settable via API; got is_system=true on 'another'")
		}
	})

	t.Run("Get_NotFound_404", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/admin/plans/"+uuid.NewString(), nil)
		resp, _ := app.Test(req)
		if resp.StatusCode != 404 {
			t.Errorf("Get with non-existent id: expected 404, got %d", resp.StatusCode)
		}
	})

	t.Run("List", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/admin/plans", nil)
		resp, _ := app.Test(req)
		if resp.StatusCode != 200 {
			t.Fatalf("List: expected 200, got %d", resp.StatusCode)
		}
		var got struct {
			Data []map[string]interface{} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		// List returns ALL plans (active + inactive). Expect at least the
		// system plan + the ones created above.
		if len(got.Data) < 2 {
			t.Errorf("List: expected >= 2 plans, got %d", len(got.Data))
		}
		// Confirm computed counts are present in the response.
		for _, p := range got.Data {
			if _, ok := p["active_user_count"]; !ok {
				t.Errorf("List: row missing active_user_count: %+v", p)
			}
			if _, ok := p["server_count"]; !ok {
				t.Errorf("List: row missing server_count: %+v", p)
			}
			if _, ok := p["offer_count"]; !ok {
				t.Errorf("List: row missing offer_count: %+v", p)
			}
		}
	})
}

// TestAdminPlanServers is the PAY-14 named test from 03-VALIDATION.md.
// Covers POST add (idempotent), DELETE remove, PUT replace, and the
// inactive-server 422 guard.
func TestAdminPlanServers(t *testing.T) {
	db, rdb := setupAdminPlansDB(t)
	app := mkAdminPlansApp(t, db, rdb)

	planID := uuid.NewString()
	if err := db.Create(&model.Plan{
		ID: planID, Code: "pro", Name: "Pro", MaxDevices: 3, MaxServers: -1,
		IsActive: true, IsSystem: false,
	}).Error; err != nil {
		t.Fatalf("seed plan: %v", err)
	}
	s1 := uuid.NewString()
	if err := db.Create(&model.VPNServer{
		ID: s1, Hostname: "s1", CountryCode: "NL", IsActive: true,
	}).Error; err != nil {
		t.Fatalf("seed server: %v", err)
	}

	t.Run("Add_201", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/admin/plans/"+planID+"/servers/"+s1, nil)
		resp, _ := app.Test(req)
		if resp.StatusCode != 201 {
			raw, _ := io.ReadAll(resp.Body)
			t.Errorf("expected 201, got %d body=%s", resp.StatusCode, string(raw))
		}
	})

	t.Run("Add_Idempotent_StillOK", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/admin/plans/"+planID+"/servers/"+s1, nil)
		resp, _ := app.Test(req)
		if resp.StatusCode != 201 {
			t.Errorf("ADR §19.7.6: idempotent — expected 201 on re-add, got %d", resp.StatusCode)
		}
	})

	t.Run("Add_InactiveServer_422", func(t *testing.T) {
		inactive := uuid.NewString()
		if err := db.Create(&model.VPNServer{
			ID: inactive, Hostname: "down", CountryCode: "DE", IsActive: true,
		}).Error; err != nil {
			t.Fatalf("seed inactive: %v", err)
		}
		// Flip to inactive via explicit UPDATE (GORM omits Go zero-value bool fields).
		if err := db.Model(&model.VPNServer{}).Where("id = ?", inactive).Update("is_active", false).Error; err != nil {
			t.Fatalf("update is_active: %v", err)
		}
		req := httptest.NewRequest("POST", "/api/v1/admin/plans/"+planID+"/servers/"+inactive, nil)
		resp, _ := app.Test(req)
		if resp.StatusCode != 422 {
			t.Errorf("expected 422 on inactive server, got %d", resp.StatusCode)
		}
	})

	t.Run("Add_NonExistentPlan_404", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/admin/plans/"+uuid.NewString()+"/servers/"+s1, nil)
		resp, _ := app.Test(req)
		if resp.StatusCode != 404 {
			t.Errorf("expected 404 on non-existent plan, got %d", resp.StatusCode)
		}
	})

	t.Run("Remove_204", func(t *testing.T) {
		req := httptest.NewRequest("DELETE", "/api/v1/admin/plans/"+planID+"/servers/"+s1, nil)
		resp, _ := app.Test(req)
		if resp.StatusCode != 204 {
			t.Errorf("expected 204, got %d", resp.StatusCode)
		}
	})

	t.Run("Remove_404OnAbsent", func(t *testing.T) {
		req := httptest.NewRequest("DELETE", "/api/v1/admin/plans/"+planID+"/servers/"+uuid.NewString(), nil)
		resp, _ := app.Test(req)
		if resp.StatusCode != 404 {
			t.Errorf("expected 404 on absent pairing, got %d", resp.StatusCode)
		}
	})

	t.Run("Replace_AtomicallyReplaces", func(t *testing.T) {
		// Seed two more active servers.
		s2 := uuid.NewString()
		s3 := uuid.NewString()
		if err := db.Create(&model.VPNServer{ID: s2, Hostname: "s2", CountryCode: "DE", IsActive: true}).Error; err != nil {
			t.Fatalf("seed s2: %v", err)
		}
		if err := db.Create(&model.VPNServer{ID: s3, Hostname: "s3", CountryCode: "JP", IsActive: true}).Error; err != nil {
			t.Fatalf("seed s3: %v", err)
		}
		// First add s1 so the plan starts with 1 pairing.
		req0 := httptest.NewRequest("POST", "/api/v1/admin/plans/"+planID+"/servers/"+s1, nil)
		_, _ = app.Test(req0)

		body, _ := json.Marshal(map[string]interface{}{"server_ids": []string{s2, s3}})
		req := httptest.NewRequest("PUT", "/api/v1/admin/plans/"+planID+"/servers", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, _ := app.Test(req)
		if resp.StatusCode != 200 {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("Replace: expected 200, got %d body=%s", resp.StatusCode, string(raw))
		}
		// Confirm only s2 + s3 remain — s1 deleted by the atomic replace.
		var pairings []model.PlanServer
		_ = db.Where("plan_id = ?", planID).Find(&pairings).Error
		if len(pairings) != 2 {
			t.Errorf("Replace: expected 2 pairings, got %d", len(pairings))
		}
		seen := map[string]bool{}
		for _, p := range pairings {
			seen[p.ServerID] = true
		}
		if !seen[s2] || !seen[s3] {
			t.Errorf("Replace: expected {s2,s3}, got %+v", pairings)
		}
		if seen[s1] {
			t.Errorf("Replace: s1 should have been removed atomically; still present")
		}
	})

	t.Run("Replace_RejectsInactiveServer_422", func(t *testing.T) {
		inactive := uuid.NewString()
		if err := db.Create(&model.VPNServer{ID: inactive, Hostname: "x", CountryCode: "FR", IsActive: true}).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
		if err := db.Model(&model.VPNServer{}).Where("id = ?", inactive).Update("is_active", false).Error; err != nil {
			t.Fatalf("update is_active: %v", err)
		}
		body, _ := json.Marshal(map[string]interface{}{"server_ids": []string{inactive}})
		req := httptest.NewRequest("PUT", "/api/v1/admin/plans/"+planID+"/servers", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, _ := app.Test(req)
		if resp.StatusCode != 422 {
			t.Errorf("Replace with inactive server: expected 422, got %d", resp.StatusCode)
		}
	})
}

// TestAdminReplaceOffer_Transactional is the PAY-15 named test from 03-VALIDATION.md.
// Verifies the price-versioning flow: old offer becomes is_active=false AND
// exactly one new active offer is inserted, all in one transaction.
func TestAdminReplaceOffer_Transactional(t *testing.T) {
	db, rdb := setupAdminPlansDB(t)
	app := mkAdminPlansApp(t, db, rdb)

	planID := uuid.NewString()
	if err := db.Create(&model.Plan{
		ID: planID, Code: "pro", Name: "Pro", MaxDevices: 3, MaxServers: -1,
		IsActive: true, IsSystem: false,
	}).Error; err != nil {
		t.Fatalf("seed plan: %v", err)
	}
	oldOfferID := uuid.NewString()
	lavaOff := "lava-old"
	if err := db.Create(&model.PlanOffer{
		ID: oldOfferID, PlanID: planID, Periodicity: "MONTHLY", Currency: "USD",
		Amount: 3.99, IsActive: true, LavaOfferID: &lavaOff,
	}).Error; err != nil {
		t.Fatalf("seed offer: %v", err)
	}

	body := `{"amount":5.99,"lava_offer_id":"lava-new"}`
	req := httptest.NewRequest("POST", "/api/v1/admin/plans/"+planID+"/offers/"+oldOfferID+"/replace", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	if resp.StatusCode != 201 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201, got %d body=%s", resp.StatusCode, string(raw))
	}

	// Old offer must be is_active=false.
	var old model.PlanOffer
	if err := db.Where("id = ?", oldOfferID).First(&old).Error; err != nil {
		t.Fatalf("reload old: %v", err)
	}
	if old.IsActive {
		t.Errorf("PAY-15: old offer must be is_active=false after replace")
	}

	// Exactly one active offer remains for the plan, with the new amount.
	var newOffers []model.PlanOffer
	if err := db.Where("plan_id = ? AND is_active = ?", planID, true).Find(&newOffers).Error; err != nil {
		t.Fatalf("list active offers: %v", err)
	}
	if len(newOffers) != 1 {
		t.Fatalf("expected exactly 1 active offer after replace, got %d", len(newOffers))
	}
	if newOffers[0].Amount != 5.99 {
		t.Errorf("expected new amount=5.99, got %v", newOffers[0].Amount)
	}
	// Periodicity + currency inherited from the old offer (immutable).
	if newOffers[0].Periodicity != "MONTHLY" || newOffers[0].Currency != "USD" {
		t.Errorf("ADR §19.7.7: new offer must inherit periodicity/currency from old; got %s/%s",
			newOffers[0].Periodicity, newOffers[0].Currency)
	}
	if newOffers[0].LavaOfferID == nil || *newOffers[0].LavaOfferID != "lava-new" {
		t.Errorf("expected lava_offer_id='lava-new', got %v", newOffers[0].LavaOfferID)
	}
}

// TestAdminCreatePlan_RejectsCodeRegexFailure verifies the regex + length
// rules from ADR §19.7.2.
func TestAdminCreatePlan_RejectsCodeRegexFailure(t *testing.T) {
	db, rdb := setupAdminPlansDB(t)
	app := mkAdminPlansApp(t, db, rdb)

	// Regex failure — uppercase + exclamation rejected.
	body := `{"code":"Pro!Invalid","name":"X","max_devices":1,"max_servers":1,"speed_limit_mbps":0}`
	req := httptest.NewRequest("POST", "/api/v1/admin/plans", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	if resp.StatusCode != 400 {
		t.Errorf("expected 400 on regex failure, got %d", resp.StatusCode)
	}

	// 41-char code rejected.
	body2, _ := json.Marshal(map[string]interface{}{
		"code": strings.Repeat("a", 41), "name": "X", "max_devices": 1, "max_servers": 1, "speed_limit_mbps": 0,
	})
	req2 := httptest.NewRequest("POST", "/api/v1/admin/plans", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	resp2, _ := app.Test(req2)
	if resp2.StatusCode != 400 {
		t.Errorf("expected 400 on 41-char code, got %d", resp2.StatusCode)
	}

	// Leading hyphen rejected (regex requires [a-z0-9] start).
	body3 := `{"code":"-leading","name":"X","max_devices":1,"max_servers":1,"speed_limit_mbps":0}`
	req3 := httptest.NewRequest("POST", "/api/v1/admin/plans", strings.NewReader(body3))
	req3.Header.Set("Content-Type", "application/json")
	resp3, _ := app.Test(req3)
	if resp3.StatusCode != 400 {
		t.Errorf("expected 400 on leading-hyphen code, got %d", resp3.StatusCode)
	}
}

// TestAdminCreatePlanOffer_RejectsInvalidPeriodicityAndCurrency verifies
// the enum guards on the POST /admin/plans/:id/offers handler.
func TestAdminCreatePlanOffer_RejectsInvalidPeriodicityAndCurrency(t *testing.T) {
	db, rdb := setupAdminPlansDB(t)
	app := mkAdminPlansApp(t, db, rdb)

	planID := uuid.NewString()
	if err := db.Create(&model.Plan{
		ID: planID, Code: "pro", Name: "Pro", MaxDevices: 3, MaxServers: -1,
		IsActive: true, IsSystem: false,
	}).Error; err != nil {
		t.Fatalf("seed plan: %v", err)
	}

	// Bad periodicity.
	body := `{"periodicity":"WEEKLY","currency":"USD","amount":5.0}`
	req := httptest.NewRequest("POST", "/api/v1/admin/plans/"+planID+"/offers", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	if resp.StatusCode != 400 {
		t.Errorf("expected 400 on invalid periodicity, got %d", resp.StatusCode)
	}

	// Bad currency.
	body2 := `{"periodicity":"MONTHLY","currency":"BTC","amount":5.0}`
	req2 := httptest.NewRequest("POST", "/api/v1/admin/plans/"+planID+"/offers", strings.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	resp2, _ := app.Test(req2)
	if resp2.StatusCode != 400 {
		t.Errorf("expected 400 on invalid currency, got %d", resp2.StatusCode)
	}

	// Negative amount.
	body3 := `{"periodicity":"MONTHLY","currency":"USD","amount":-1.0}`
	req3 := httptest.NewRequest("POST", "/api/v1/admin/plans/"+planID+"/offers", strings.NewReader(body3))
	req3.Header.Set("Content-Type", "application/json")
	resp3, _ := app.Test(req3)
	if resp3.StatusCode != 400 {
		t.Errorf("expected 400 on negative amount, got %d", resp3.StatusCode)
	}
}
