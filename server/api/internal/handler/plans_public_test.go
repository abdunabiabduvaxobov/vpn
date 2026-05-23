package handler

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"vpnapp/server/api/internal/cache"
	"vpnapp/server/api/internal/model"

	"github.com/alicebob/miniredis/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupPublicPlansDB creates the minimum schema for ListPlansPublic tests.
// Mirrors setupPlanRepoDB (03-03 deviations carried forward):
//   - SetMaxOpenConns(1) so :memory: SQLite stays single-connection (tx visibility).
//   - vpn_servers extended with region/city/capacity columns the GORM model declares.
//   - subscriptions.id has DEFAULT(lower(hex(randomblob(16)))) so GORM's
//     default:gen_random_uuid() tag (Postgres-only) doesn't leave id=NULL.
//   - Seeded rows that need is_active=false are inserted active then UPDATEd —
//     GORM omits Go zero-value bool fields from INSERT, so the DDL DEFAULT (1) wins.
func setupPublicPlansDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db.DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	stmts := []string{
		`CREATE TABLE plans (
			id TEXT PRIMARY KEY,
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
			id TEXT PRIMARY KEY,
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
	// Seed: free + pro plans with 1 active offer on Pro in USD; 1 server NL attached to both.
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
	sid := uuid.NewString()
	if err := db.Create(&model.VPNServer{ID: sid, Hostname: "nl1", CountryCode: "NL", IsActive: true}).Error; err != nil {
		t.Fatalf("seed server: %v", err)
	}
	if err := db.Create(&model.PlanServer{PlanID: freeID, ServerID: sid}).Error; err != nil {
		t.Fatalf("seed free plan_server: %v", err)
	}
	if err := db.Create(&model.PlanServer{PlanID: proID, ServerID: sid}).Error; err != nil {
		t.Fatalf("seed pro plan_server: %v", err)
	}
	// Pro: MONTHLY USD 5.00
	if err := db.Create(&model.PlanOffer{ID: uuid.NewString(), PlanID: proID, Periodicity: "MONTHLY", Currency: "USD", Amount: 5.0, IsActive: true}).Error; err != nil {
		t.Fatalf("seed offer: %v", err)
	}
	return db
}

func newMRForHandler(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	return mr, redis.NewClient(&redis.Options{Addr: mr.Addr()})
}

// TestListPlansPublic_CacheHitMissBust is the PAY-12 named test from 03-VALIDATION.md.
// Sequence:
//  1. First call → miss → DB read → response cached.
//  2. Second call → hit → response served from cache (DB not consulted — proven by
//     mutating the DB between calls and expecting the OLD response).
//  3. BustPlansCache → next call hits DB again.
func TestListPlansPublic_CacheHitMissBust(t *testing.T) {
	db := setupPublicPlansDB(t)
	_, rdb := newMRForHandler(t)
	app := fiber.New()
	app.Get("/api/v1/plans", ListPlansPublic(zap.NewNop(), db, rdb))

	// First call — cache miss, DB read, cache populated.
	req := httptest.NewRequest("GET", "/api/v1/plans?currency=USD", nil)
	resp1, _ := app.Test(req)
	if resp1.StatusCode != 200 {
		t.Fatalf("first call: expected 200, got %d", resp1.StatusCode)
	}
	var body1 struct {
		Data struct {
			Currency string       `json:"currency"`
			Plans    []publicPlan `json:"plans"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp1.Body).Decode(&body1); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body1.Data.Currency != "USD" || len(body1.Data.Plans) != 2 {
		t.Errorf("expected 2 plans in USD, got %+v", body1.Data)
	}

	// Mutate DB — make Pro inactive. The cached response should still show Pro.
	if err := db.Model(&model.Plan{}).Where("code = ?", "pro").Update("is_active", false).Error; err != nil {
		t.Fatalf("mutate: %v", err)
	}

	// Second call within cache TTL — should HIT cache (still show 2 plans).
	resp2, _ := app.Test(httptest.NewRequest("GET", "/api/v1/plans?currency=USD", nil))
	var body2 struct {
		Data struct {
			Plans []publicPlan `json:"plans"`
		} `json:"data"`
	}
	_ = json.NewDecoder(resp2.Body).Decode(&body2)
	if len(body2.Data.Plans) != 2 {
		t.Errorf("expected cache hit (still 2 plans), got %d", len(body2.Data.Plans))
	}

	// Bust cache and re-request — should now see only 1 plan.
	if err := cache.BustPlansCache(req.Context(), rdb); err != nil {
		t.Fatalf("bust: %v", err)
	}
	resp3, _ := app.Test(httptest.NewRequest("GET", "/api/v1/plans?currency=USD", nil))
	var body3 struct {
		Data struct {
			Plans []publicPlan `json:"plans"`
		} `json:"data"`
	}
	_ = json.NewDecoder(resp3.Body).Decode(&body3)
	if len(body3.Data.Plans) != 1 {
		t.Errorf("after bust + Pro deactivation: expected 1 plan (free), got %d", len(body3.Data.Plans))
	}
}

func TestListPlansPublic_AcceptLanguageDerivesRUB(t *testing.T) {
	db := setupPublicPlansDB(t)
	_, rdb := newMRForHandler(t)
	app := fiber.New()
	app.Get("/api/v1/plans", ListPlansPublic(zap.NewNop(), db, rdb))
	req := httptest.NewRequest("GET", "/api/v1/plans", nil)
	req.Header.Set("Accept-Language", "ru-RU,ru;q=0.9")
	resp, _ := app.Test(req)
	var body struct {
		Data struct {
			Currency string `json:"currency"`
		} `json:"data"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body.Data.Currency != "RUB" {
		t.Errorf("D-27: expected RUB for ru Accept-Language, got %s", body.Data.Currency)
	}
}

func TestListPlansPublic_InvalidCurrency_400(t *testing.T) {
	db := setupPublicPlansDB(t)
	_, rdb := newMRForHandler(t)
	app := fiber.New()
	app.Get("/api/v1/plans", ListPlansPublic(zap.NewNop(), db, rdb))
	resp, _ := app.Test(httptest.NewRequest("GET", "/api/v1/plans?currency=BTC", nil))
	if resp.StatusCode != 400 {
		t.Errorf("expected 400 on invalid currency, got %d", resp.StatusCode)
	}
}

func TestListPlansPublic_ExcludesAdminOnlyFields(t *testing.T) {
	db := setupPublicPlansDB(t)
	_, rdb := newMRForHandler(t)
	app := fiber.New()
	app.Get("/api/v1/plans", ListPlansPublic(zap.NewNop(), db, rdb))
	resp, _ := app.Test(httptest.NewRequest("GET", "/api/v1/plans?currency=USD", nil))
	raw, _ := io.ReadAll(resp.Body)
	body := string(raw)
	for _, banned := range []string{`"id":`, `"lava_offer_id"`, `"active_user_count"`, `"plan_id":`} {
		if strings.Contains(body, banned) {
			t.Errorf("D-27: response must not contain %s; got %s", banned, body)
		}
	}
}
