---
phase: 3
slug: lava-top-plans-catalog
plan_number: 7
wave: 3
depends_on: [1, 3]
files_modified:
  - server/api/internal/cache/plans_cache.go
  - server/api/internal/cache/plans_cache_test.go
  - server/api/internal/handler/plans_public.go
  - server/api/internal/handler/plans_public_test.go
  - server/api/internal/handler/auth.go
  - server/api/internal/middleware/auth.go
  - server/api/cmd/main.go
autonomous: true
requirements_addressed: [PAY-12]
estimated_complexity: medium
---

<objective>
Land the three pieces that landing-site `/pricing` and middleware-side plan_id resolution need:
1. `internal/cache/plans_cache.go` — Redis cache-aside wrapper for `cache:plans:public:{currency}` with TTL 60s + bust helper.
2. `internal/handler/plans_public.go` — `GET /api/v1/plans` (no auth) with `?currency=USD|EUR|RUB`, default-derive from `Accept-Language`, server_countries denormalized via plan_repo.ListPlanServerCountries.
3. JWT `plan_id` claim integration — `auth.go::generateTokens` adds the claim; `middleware/auth.go::AuthRequired` extracts to `c.Locals("plan_id")` with the FindUserByID backward-compat fallback (RESEARCH §7.5 — middleware already does the DB read for HOTFIX-02, reuse it).
4. Wire `GET /api/v1/plans` in `cmd/main.go`.
</objective>

<context>
@.planning/phases/03-lava-top-plans-catalog/03-CONTEXT.md
@.planning/phases/03-lava-top-plans-catalog/03-RESEARCH.md
@server/api/internal/cache/redis.go
@server/api/internal/handler/auth.go
@server/api/internal/middleware/auth.go
@server/api/internal/repository/plan_repo.go
@server/api/cmd/main.go
</context>

<interfaces>
```go
// internal/cache/plans_cache.go
const plansPublicKeyPrefix = "cache:plans:public:"

func GetPlansCache(ctx context.Context, client *redis.Client, currency string) (string, error)
func SetPlansCache(ctx context.Context, client *redis.Client, currency, jsonBody string) error
func BustPlansCache(ctx context.Context, client *redis.Client) error

// internal/handler/plans_public.go
func ListPlansPublic(logger *zap.Logger, db *gorm.DB, redisClient *redis.Client) fiber.Handler

// internal/handler/auth.go (AMEND)
func generateTokens(userID, tier, role, name, planID, secret string) (*authResponse, error) // adds planID param

// internal/middleware/auth.go (AMEND — Claims struct + c.Locals write)
type Claims struct {
    UserID string `json:"sub"`
    Tier   string `json:"tier"`
    Role   string `json:"role"`
    PlanID string `json:"plan_id,omitempty"` // NEW (omitempty for backward-compat)
    jwt.RegisteredClaims
}
```

Response shape for `GET /api/v1/plans` (ADR §19.9.1):
```json
{
  "data": {
    "currency": "USD",
    "plans": [
      {
        "code": "free",
        "name": "Free",
        "description": "...",
        "max_devices": 1,
        "max_servers": 3,
        "speed_limit_mbps": 50,
        "is_system": true,
        "sort_order": 0,
        "server_countries": ["NL","DE","US"],
        "offers": []
      },
      {
        "code": "pro",
        "name": "Pro",
        "description": "...",
        "max_devices": 3,
        "max_servers": -1,
        "speed_limit_mbps": 0,
        "is_system": false,
        "sort_order": 10,
        "server_countries": ["NL","DE","US","JP"],
        "offers": [
          {"periodicity":"MONTHLY","currency":"USD","amount":5.00},
          {"periodicity":"PERIOD_YEAR","currency":"USD","amount":49.99}
        ]
      }
    ]
  }
}
```
</interfaces>

<tasks>

<task type="auto">
  <id>03-07-T01</id>
  <name>Write internal/cache/plans_cache.go + tests (miniredis-backed cache-aside wrapper)</name>
  <files>
    server/api/internal/cache/plans_cache.go,
    server/api/internal/cache/plans_cache_test.go
  </files>
  <read_first>
    - server/api/internal/cache/redis.go (CURRENT — IsTokenBlacklisted / BlacklistToken / IncrRateLimit pattern; the fail-open behaviour on Redis errors)
    - .planning/phases/03-lava-top-plans-catalog/03-RESEARCH.md §6.1 (cache.GetPlansCache/SetPlansCache/BustPlansCache verbatim), §6.4 (fail-open contract)
    - .planning/phases/03-lava-top-plans-catalog/03-CONTEXT.md D-28 (cache key shape `cache:plans:public:{currency}`, TTL 60s, bust on admin write)
  </read_first>
  <action>
    **(a) `server/api/internal/cache/plans_cache.go`:**

```go
package cache

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// plansPublicKeyPrefix is the Redis key namespace for the public /plans cache.
// Per CONTEXT.md D-28 the full key shape is `cache:plans:public:{currency}` —
// callers append the currency string.
const plansPublicKeyPrefix = "cache:plans:public:"

// plansPublicCacheTTL is the cache-aside TTL — short enough that an admin
// publish becomes visible within a minute even if the explicit BustPlansCache
// call from the admin handler fails for any reason.
const plansPublicCacheTTL = 60 * time.Second

// GetPlansCache returns the JSON-encoded cached body for the given currency,
// or "" with no error on cache miss / Redis outage. Callers fall through to
// a DB read on empty result.
//
// Fail-open contract (matches IsTokenBlacklisted in this same package): a
// Redis outage MUST NOT break the public /pricing page — return empty so
// the handler falls through to the slower DB path.
func GetPlansCache(ctx context.Context, client *redis.Client, currency string) (string, error) {
	if client == nil {
		return "", nil
	}
	key := plansPublicKeyPrefix + currency
	val, err := client.Get(ctx, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return "", nil // miss is not an error
		}
		// Fail open — Redis transient errors must not break /plans.
		return "", nil
	}
	return val, nil
}

// SetPlansCache writes the encoded body with the 60s TTL. The returned error
// is informational only — the handler should not propagate it. The next
// request misses cache and re-populates.
func SetPlansCache(ctx context.Context, client *redis.Client, currency, jsonBody string) error {
	if client == nil {
		return nil
	}
	key := plansPublicKeyPrefix + currency
	return client.Set(ctx, key, jsonBody, plansPublicCacheTTL).Err()
}

// BustPlansCache deletes every cache:plans:public:* key. Called by admin
// /plans/* write handlers (plan 03-08) after a successful state change.
//
// Cardinality is bounded by the currency count (3 today: USD, EUR, RUB) +
// any future locale extensions — the explicit DEL on each known currency
// is cheaper than SCAN at this scale, but SCAN scales without changes.
// Per RESEARCH §6.3 (a) we use explicit DEL.
func BustPlansCache(ctx context.Context, client *redis.Client) error {
	if client == nil {
		return nil
	}
	// Explicit DEL — bounded by the currency enum.
	keys := []string{
		plansPublicKeyPrefix + "USD",
		plansPublicKeyPrefix + "EUR",
		plansPublicKeyPrefix + "RUB",
	}
	return client.Del(ctx, keys...).Err()
}
```

    **(b) `server/api/internal/cache/plans_cache_test.go`** (miniredis-backed — already in go.mod per RESEARCH "Environment Availability"):

```go
package cache

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newMiniRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	c := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return mr, c
}

func TestSetAndGetPlansCache_RoundTrip(t *testing.T) {
	_, c := newMiniRedis(t)
	ctx := context.Background()
	if err := SetPlansCache(ctx, c, "USD", `{"hello":"world"}`); err != nil {
		t.Fatalf("SetPlansCache: %v", err)
	}
	got, err := GetPlansCache(ctx, c, "USD")
	if err != nil {
		t.Fatalf("GetPlansCache: %v", err)
	}
	if got != `{"hello":"world"}` {
		t.Errorf("expected payload back, got %q", got)
	}
}

func TestGetPlansCache_MissReturnsEmpty(t *testing.T) {
	_, c := newMiniRedis(t)
	got, err := GetPlansCache(context.Background(), c, "EUR")
	if err != nil {
		t.Fatalf("GetPlansCache miss: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty on miss, got %q", got)
	}
}

func TestBustPlansCache_DeletesAllCurrencies(t *testing.T) {
	mr, c := newMiniRedis(t)
	ctx := context.Background()
	for _, cur := range []string{"USD", "EUR", "RUB"} {
		if err := SetPlansCache(ctx, c, cur, `{"x":1}`); err != nil {
			t.Fatalf("seed %s: %v", cur, err)
		}
	}
	if err := BustPlansCache(ctx, c); err != nil {
		t.Fatalf("BustPlansCache: %v", err)
	}
	for _, cur := range []string{"USD", "EUR", "RUB"} {
		got, _ := GetPlansCache(ctx, c, cur)
		if got != "" {
			t.Errorf("expected %s busted, got %q", cur, got)
		}
	}
	// Sanity: no other keys leaked.
	keys := mr.Keys()
	if len(keys) != 0 {
		t.Errorf("expected empty Redis after bust, got %d keys", len(keys))
	}
}

func TestGetPlansCache_NilClient_ReturnsEmptyNoError(t *testing.T) {
	got, err := GetPlansCache(context.Background(), nil, "USD")
	if err != nil || got != "" {
		t.Errorf("nil client: expected empty + no error, got %q err=%v", got, err)
	}
}

func TestSetPlansCache_TTLExpires(t *testing.T) {
	mr, c := newMiniRedis(t)
	ctx := context.Background()
	if err := SetPlansCache(ctx, c, "USD", `{"x":1}`); err != nil {
		t.Fatalf("set: %v", err)
	}
	// Advance miniredis clock past TTL.
	mr.FastForward(61 * time.Second)
	got, _ := GetPlansCache(ctx, c, "USD")
	if got != "" {
		t.Errorf("expected expiry after 60s, got %q", got)
	}
}
```

    Run `cd server/api && go test ./internal/cache/ -count=1 -timeout=30s -v`.
  </action>
  <acceptance_criteria>
    - Files `server/api/internal/cache/plans_cache.go` and `plans_cache_test.go` exist
    - `grep 'plansPublicKeyPrefix = "cache:plans:public:"' server/api/internal/cache/plans_cache.go` finds one match (D-28 key shape)
    - `grep "plansPublicCacheTTL = 60 \\* time.Second" server/api/internal/cache/plans_cache.go` finds one match
    - `grep "func GetPlansCache\\|func SetPlansCache\\|func BustPlansCache" server/api/internal/cache/plans_cache.go` finds 3 matches
    - `cd server/api && go test ./internal/cache/ -count=1 -timeout=30s` exits 0
  </acceptance_criteria>
  <automated>cd server/api && go test ./internal/cache/ -count=1 -timeout=30s</automated>
  <done>Cache wrapper has 3 functions + 5 test cases; miniredis verifies roundtrip, miss, bust-all, nil-client fail-open, and 60s expiry.</done>
</task>

<task type="auto">
  <id>03-07-T02</id>
  <name>Write internal/handler/plans_public.go + tests (GET /api/v1/plans with currency derivation + cache-aside)</name>
  <files>
    server/api/internal/handler/plans_public.go,
    server/api/internal/handler/plans_public_test.go
  </files>
  <read_first>
    - server/api/internal/repository/plan_repo.go (ListActivePlans, ListActiveOffersForPublic, ListPlanServerCountries — all written in plan 03-03)
    - server/api/internal/cache/plans_cache.go (T01 of THIS plan)
    - .planning/phases/03-lava-top-plans-catalog/03-RESEARCH.md §6.2 (handler pattern), §6.3 (admin-side bust — happens in plan 03-08)
    - .planning/phases/03-lava-top-plans-catalog/03-CONTEXT.md D-27 (currency derivation from Accept-Language: RU→RUB else USD), D-28 (cache key)
    - docs/ADR-007-lava-sso-rework.md §19.9.1 (response shape)
  </read_first>
  <action>
    **(a) `server/api/internal/handler/plans_public.go`:**

```go
package handler

import (
	"encoding/json"
	"strings"

	"vpnapp/server/api/internal/cache"
	"vpnapp/server/api/internal/model"
	"vpnapp/server/api/internal/repository"

	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// allowedPublicCurrencies enumerates the currencies the public /plans endpoint
// can be queried with. Mirrors the plan_offers.currency CHECK constraint.
var allowedPublicCurrencies = map[string]struct{}{
	"USD": {}, "EUR": {}, "RUB": {},
}

// publicOffer is the trimmed offer shape returned by /api/v1/plans (no id,
// no lava_offer_id, no is_active — those are admin-only).
type publicOffer struct {
	Periodicity string  `json:"periodicity"`
	Currency    string  `json:"currency"`
	Amount      float64 `json:"amount"`
}

// publicPlan is the trimmed plan shape returned by /api/v1/plans.
type publicPlan struct {
	Code            string        `json:"code"`
	Name            string        `json:"name"`
	Description     string        `json:"description"`
	MaxDevices      int           `json:"max_devices"`
	MaxServers      int           `json:"max_servers"`
	SpeedLimitMbps  int           `json:"speed_limit_mbps"`
	IsSystem        bool          `json:"is_system"`
	SortOrder       int           `json:"sort_order"`
	ServerCountries []string      `json:"server_countries"`
	Offers          []publicOffer `json:"offers"`
}

// ListPlansPublic handles GET /api/v1/plans (PUBLIC, no auth).
//
// Query param: ?currency=USD|EUR|RUB (default: derived from Accept-Language —
// RU → RUB else USD per D-27). Invalid currency → 400.
//
// Cache: cache:plans:public:{currency}, TTL 60s. Cache miss or Redis outage
// falls through to a DB-backed build. Admin write handlers (plan 03-08) bust
// the cache on successful mutation.
//
// Excludes admin-only fields per D-27 (id, lava_offer_id, active_user_count).
func ListPlansPublic(logger *zap.Logger, db *gorm.DB, redisClient *redis.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		currency := strings.ToUpper(strings.TrimSpace(c.Query("currency")))
		if currency == "" {
			currency = deriveCurrencyFromAcceptLanguage(c.Get("Accept-Language"))
		}
		if _, ok := allowedPublicCurrencies[currency]; !ok {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid currency"})
		}

		// Cache-aside read.
		if cached, _ := cache.GetPlansCache(c.Context(), redisClient, currency); cached != "" {
			c.Set("Content-Type", "application/json")
			return c.SendString(cached)
		}

		// Miss — query DB.
		plans, err := repository.ListActivePlans(db)
		if err != nil {
			logger.Error("/plans: ListActivePlans", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		offers, err := repository.ListActiveOffersForPublic(db)
		if err != nil {
			logger.Error("/plans: ListActiveOffersForPublic", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}

		// Build offers-by-plan-id map (filter to requested currency only).
		offersByPlan := make(map[string][]publicOffer, len(plans))
		for _, o := range offers {
			if o.Currency != currency {
				continue
			}
			offersByPlan[o.PlanID] = append(offersByPlan[o.PlanID], publicOffer{
				Periodicity: o.Periodicity,
				Currency:    o.Currency,
				Amount:      o.Amount,
			})
		}

		out := make([]publicPlan, 0, len(plans))
		for _, p := range plans {
			countries, err := repository.ListPlanServerCountries(db, p.ID)
			if err != nil {
				logger.Warn("/plans: ListPlanServerCountries failed (using empty)",
					zap.String("plan_id", p.ID), zap.Error(err))
				countries = []string{}
			}
			out = append(out, publicPlan{
				Code:            p.Code,
				Name:            p.Name,
				Description:     p.Description,
				MaxDevices:      p.MaxDevices,
				MaxServers:      p.MaxServers,
				SpeedLimitMbps:  p.SpeedLimitMbps,
				IsSystem:        p.IsSystem,
				SortOrder:       p.SortOrder,
				ServerCountries: countries,
				Offers:          offersByPlan[p.ID],
			})
		}

		body, mErr := json.Marshal(fiber.Map{
			"data": fiber.Map{
				"currency": currency,
				"plans":    out,
			},
		})
		if mErr != nil {
			logger.Error("/plans: marshal failed", zap.Error(mErr))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}

		// Best-effort cache write.
		_ = cache.SetPlansCache(c.Context(), redisClient, currency, string(body))

		c.Set("Content-Type", "application/json")
		return c.Send(body)
	}
}

// deriveCurrencyFromAcceptLanguage returns "RUB" for any Accept-Language starting
// with "ru" (case-insensitive), "USD" otherwise. D-27.
func deriveCurrencyFromAcceptLanguage(header string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(header)), "ru") {
		return "RUB"
	}
	return "USD"
}

// _ keeps the model import valid even if no model.* type is referenced directly
// in this file (the repository functions return model.* types but Go's type
// inference handles them). Removed if unused.
var _ = model.Plan{}
```

    **(b) `server/api/internal/handler/plans_public_test.go`** — required to include `TestListPlansPublic_CacheHitMissBust` (PAY-12 named test from 03-VALIDATION.md):

```go
package handler

import (
	"encoding/json"
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

func setupPublicPlansDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	stmts := []string{
		`CREATE TABLE plans (id TEXT PRIMARY KEY, code TEXT NOT NULL UNIQUE, name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '', max_devices INTEGER NOT NULL, max_servers INTEGER NOT NULL,
			speed_limit_mbps INTEGER NOT NULL DEFAULT 0, is_active INTEGER NOT NULL DEFAULT 1,
			is_system INTEGER NOT NULL DEFAULT 0, sort_order INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP, updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE plan_servers (plan_id TEXT NOT NULL, server_id TEXT NOT NULL, PRIMARY KEY (plan_id, server_id))`,
		`CREATE TABLE plan_offers (id TEXT PRIMARY KEY, plan_id TEXT NOT NULL, periodicity TEXT NOT NULL,
			currency TEXT NOT NULL, amount REAL NOT NULL, lava_offer_id TEXT, is_active INTEGER NOT NULL DEFAULT 1,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP, updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE vpn_servers (id TEXT PRIMARY KEY, hostname TEXT NOT NULL, ip_address TEXT NOT NULL DEFAULT '',
			country_code TEXT NOT NULL DEFAULT '', country TEXT NOT NULL DEFAULT '', protocol TEXT NOT NULL DEFAULT 'vless',
			is_active INTEGER NOT NULL DEFAULT 1, current_load INTEGER NOT NULL DEFAULT 0,
			reality_public_key TEXT NOT NULL DEFAULT '', reality_short_id TEXT NOT NULL DEFAULT '',
			ws_enabled INTEGER NOT NULL DEFAULT 0, ws_host TEXT NOT NULL DEFAULT '', ws_path TEXT NOT NULL DEFAULT '',
			awg_public_key TEXT, awg_endpoint TEXT)`,
	}
	for _, s := range stmts {
		if err := db.Exec(s).Error; err != nil {
			t.Fatalf("DDL: %v", err)
		}
	}
	// Seed: free + pro plans with 1 active offer each in USD; 1 server NL attached to both.
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
	_ = db.Create(&model.VPNServer{ID: sid, Hostname: "nl1", CountryCode: "NL", IsActive: true}).Error
	_ = db.Create(&model.PlanServer{PlanID: freeID, ServerID: sid}).Error
	_ = db.Create(&model.PlanServer{PlanID: proID, ServerID: sid}).Error
	// Pro: MONTHLY USD 5.00
	_ = db.Create(&model.PlanOffer{ID: uuid.NewString(), PlanID: proID, Periodicity: "MONTHLY", Currency: "USD", Amount: 5.0, IsActive: true}).Error
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
//   1. First call → miss → DB read → response cached.
//   2. Second call → hit → response served from cache (DB not consulted — proven by
//      mutating the DB between calls and expecting the OLD response).
//   3. BustPlansCache → next call hits DB again.
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
	buf := new(strings.Builder)
	_, _ = buf.ReadFrom(resp.Body)
	body := buf.String()
	for _, banned := range []string{`"id":`, `"lava_offer_id"`, `"active_user_count"`, `"plan_id":`} {
		if strings.Contains(body, banned) {
			t.Errorf("D-27: response must not contain %s; got %s", banned, body)
		}
	}
}
```

    Run `cd server/api && go test ./internal/handler/ -run "TestListPlansPublic" -count=1 -timeout=30s -v`.
  </action>
  <acceptance_criteria>
    - Files `server/api/internal/handler/plans_public.go` and `plans_public_test.go` exist
    - `grep "deriveCurrencyFromAcceptLanguage" server/api/internal/handler/plans_public.go` finds matches
    - `grep "cache.GetPlansCache\\|cache.SetPlansCache" server/api/internal/handler/plans_public.go` finds at least 2 matches
    - `grep "lava_offer_id\\|active_user_count" server/api/internal/handler/plans_public.go` returns 0 hits (D-27 — admin-only fields excluded)
    - `grep "TestListPlansPublic_CacheHitMissBust" server/api/internal/handler/plans_public_test.go` finds one match (PAY-12 named test)
    - `grep "TestListPlansPublic_ExcludesAdminOnlyFields" server/api/internal/handler/plans_public_test.go` finds one match
    - `cd server/api && go test ./internal/handler/ -run "TestListPlansPublic" -count=1 -timeout=30s` exits 0
  </acceptance_criteria>
  <automated>cd server/api && go test ./internal/handler/ -run "TestListPlansPublic" -count=1 -timeout=30s</automated>
  <done>GET /api/v1/plans serves currency-aware response from cache (60s); excludes admin-only fields; D-27 Accept-Language → RUB tested.</done>
</task>

<task type="auto">
  <id>03-07-T03</id>
  <name>Amend handler/auth.go::generateTokens + every call site (5 places) to add plan_id claim</name>
  <files>server/api/internal/handler/auth.go</files>
  <read_first>
    - server/api/internal/handler/auth.go (lines 99 AdminLogin, 279 RefreshToken, 407 GuestLogin, 520 GuestLogin alt, 574-611 generateTokens, 957 AppleSignIn, 1010 GoogleSignIn — read each call site)
    - server/api/internal/repository/plan_repo.go (FindSystemPlanID — fallback for users without plan_id set)
    - .planning/phases/03-lava-top-plans-catalog/03-CONTEXT.md D-29 (JWT mint adds plan_id claim)
    - .planning/phases/03-lava-top-plans-catalog/03-RESEARCH.md §7.1 (current signature), §7.2 (required amendment)
  </read_first>
  <action>
    Two edits to `server/api/internal/handler/auth.go`:

    **(a) Change the `generateTokens` signature** at line ~574 — add `planID string` parameter before `secret`. Update the access claims map to include it:

```go
// generateTokens creates a JWT access token (5 min) and refresh token (30 days).
//
// Phase 3 (D-29): adds the `plan_id` claim so server-access enforcement at the
// handler layer can skip a DB lookup per request. Backward-compat: empty
// planID is OK — the middleware (auth.go) falls back to FindUserByID.
func generateTokens(userID, tier, role, name, planID, secret string) (*authResponse, error) {
	now := time.Now()
	accessExpiry := now.Add(5 * time.Minute)

	accessClaims := jwt.MapClaims{
		"sub":     userID,
		"tier":    tier,
		"role":    role,
		"name":    name,
		"plan_id": planID,
		"iat":     now.Unix(),
		"exp":     accessExpiry.Unix(),
	}
	// ... rest of body unchanged: SignedString, refresh claims, etc.
```

    **(b) Update EVERY caller** (5 sites in auth.go) to pass the user's plan_id. The pattern: after loading the user, pass `user.PlanID`. If the call site doesn't have a User loaded, load it (or use `repository.FindSystemPlanID(db)` as a last-resort fallback).

    Use `git grep -n 'generateTokens(' server/api/internal/handler/auth.go` to find all callers and update each to add `user.PlanID` (or equivalent) as the 5th argument. Concretely:

    - **AdminLogin (~line 99):** has `user` loaded; pass `user.PlanID`.
    - **RefreshToken (~line 279):** has `user` loaded; pass `user.PlanID`.
    - **GuestLogin (~lines 407 + 520):** new guest users get the system plan. After `repository.CreateUser(...)` succeeds, call:
       ```go
       systemPlanID, _ := repository.FindSystemPlanID(db)
       if systemPlanID != "" {
           _ = db.Model(&model.User{}).Where("id = ?", user.ID).Update("plan_id", systemPlanID).Error
           user.PlanID = systemPlanID
       }
       ```
       BEFORE the generateTokens call, and pass `user.PlanID`. For the alt-GuestLogin path do the same.
    - **AppleSignIn (~line 957):** the user struct is loaded/created via PromoteGuestToSSO / FindUserByAppleID / etc. — pass `user.PlanID` (which is populated either from migration 019 backfill or from the GuestLogin step above).
    - **GoogleSignIn (~line 1010):** same as Apple.

    Then `cd server/api && go build ./internal/handler/...`. Existing tests in `auth_test.go` may break because they call `generateTokens` directly with the old 4-arg signature. Update them — typically add `""` or a fixed UUID as the 5th argument.

    Also check `internal/handler/auth_test.go` for direct calls to `generateTokens(...)` and update them.
  </action>
  <acceptance_criteria>
    - `grep "func generateTokens(userID, tier, role, name, planID, secret string)" server/api/internal/handler/auth.go` finds one match
    - `grep '"plan_id": planID' server/api/internal/handler/auth.go` finds one match
    - `grep -c "generateTokens(" server/api/internal/handler/auth.go` returns at least 6 (5 callers + 1 definition)
    - `cd server/api && go build ./internal/handler/...` exits 0
    - `cd server/api && go test ./internal/handler/ -run "TestAdminLogin|TestGuestLogin|TestRefreshToken|TestAppleSignIn|TestGoogleSignIn" -count=1 -timeout=60s` exits 0
  </acceptance_criteria>
  <automated>cd server/api && go test ./internal/handler/ -run "TestAdminLogin|TestGuestLogin|TestRefreshToken|TestAppleSignIn|TestGoogleSignIn" -count=1 -timeout=60s</automated>
  <done>generateTokens emits plan_id claim; all 5 callers pass user.PlanID; new guests get system plan_id assigned before token mint.</done>
</task>

<task type="auto">
  <id>03-07-T04</id>
  <name>Amend middleware/auth.go (Claims.PlanID + c.Locals("plan_id") with FindUserByID backward-compat fallback)</name>
  <files>server/api/internal/middleware/auth.go</files>
  <read_first>
    - server/api/internal/middleware/auth.go (CURRENT — Claims struct + AuthRequired body)
    - .planning/phases/03-lava-top-plans-catalog/03-RESEARCH.md §7.3 (Claims amendment), §7.5 (middleware-side fallback using existing FindUserByID — single DB read, no extra cost)
    - .planning/phases/03-lava-top-plans-catalog/03-CONTEXT.md D-29 (backward-compat: empty claim → middleware falls back to DB read)
  </read_first>
  <action>
    Edit `server/api/internal/middleware/auth.go`:

    **(a) Add `PlanID string` to the Claims struct (line ~18-24):**

```go
type Claims struct {
	UserID string `json:"sub"`
	Tier   string `json:"tier"`
	Role   string `json:"role"`
	PlanID string `json:"plan_id,omitempty"` // Phase 3 D-29 — backward-compat: omitempty for in-flight Phase 2 JWTs
	jwt.RegisteredClaims
}
```

    **(b) After the existing `c.Locals("role", claims.Role)` line (~line 103), add the plan_id write. Use the existing FindUserByID call (already done at line 88 for HOTFIX-02) as the fallback source:**

```go
		// Store user info in context for downstream handlers.
		c.Locals("user_id", claims.UserID)
		c.Locals("tier", claims.Tier)
		c.Locals("role", claims.Role)

		// Phase 3 D-29: plan_id from JWT, fall back to DB on empty claim
		// (5-minute backward-compat window for in-flight Phase 2 JWTs).
		// The FindUserByID call ABOVE (HOTFIX-02 existence check) loads the
		// user already — we re-use that result by re-calling here only when
		// the claim is empty. The HOTFIX-02 path doesn't preserve `user` in
		// a variable; the simplest plan is to call FindUserByID a second
		// time here. The cost is one indexed PK lookup (~0.5ms) DURING the
		// 5-min transition window. After that window the JWT carries the
		// claim and this fallback path is dead.
		planID := claims.PlanID
		if planID == "" && db != nil {
			if u, ferr := repository.FindUserByID(db, claims.UserID); ferr == nil {
				planID = u.PlanID
			}
		}
		c.Locals("plan_id", planID)

		return c.Next()
```

    **Optimization note (do NOT take this in-plan but document):** RESEARCH §7.5 suggests folding the HOTFIX-02 FindUserByID call AND the plan_id read into one call. Doing so would save the second DB read but requires reshaping the existing HOTFIX-02 code block. Per CONTEXT.md "minimise blast radius" — we accept one extra DB read during the 5-min window. After plan 03-07 ships and operators have rotated JWTs (5 min after deploy), this extra read is dead code.

    Run `cd server/api && go build ./...` and `cd server/api && go test ./internal/middleware/... -count=1 -timeout=30s`.
  </action>
  <acceptance_criteria>
    - `grep 'PlanID string\s*\`json:"plan_id' server/api/internal/middleware/auth.go` finds one match (Claims struct)
    - `grep 'c.Locals("plan_id", planID)' server/api/internal/middleware/auth.go` finds one match
    - `grep 'omitempty' server/api/internal/middleware/auth.go` finds one match (backward-compat marker)
    - `cd server/api && go build ./...` exits 0
    - `cd server/api && go test ./internal/middleware/... -count=1 -timeout=30s` exits 0
  </acceptance_criteria>
  <automated>cd server/api && go build ./... && go test ./internal/middleware/... -count=1 -timeout=30s</automated>
  <done>Claims struct has PlanID json:"plan_id,omitempty"; AuthRequired sets c.Locals("plan_id") with DB fallback when claim empty.</done>
</task>

<task type="auto">
  <id>03-07-T05</id>
  <name>Wire GET /api/v1/plans in cmd/main.go (public, no auth) + add SkipRule for AppVersion gate</name>
  <files>server/api/cmd/main.go</files>
  <read_first>
    - server/api/cmd/main.go (post-plans 03-05 + 03-06; has lavaClient + lavaIPAllowlist constructed)
    - server/api/internal/handler/plans_public.go (T02 of THIS plan — ListPlansPublic signature)
    - .planning/phases/03-lava-top-plans-catalog/03-CONTEXT.md D-02 (route mounted public, no auth)
  </read_first>
  <action>
    Edit `server/api/cmd/main.go`. Two additions:

    **(a) After the existing `api.Get("/health", handler.Health())` line (~line 192), add:**

```go
	// Phase 3 public plans endpoint (PAY-12). No auth. Cached via Redis
	// (cache:plans:public:{currency}, TTL 60s) — admin writes (plan 03-08) bust it.
	api.Get("/plans", handler.ListPlansPublic(logger, db, redisClient))
```

    **(b) Add a SkipRule for the AppVersion middleware so /plans doesn't require X-App-Version (mobile would never call /plans — but landing browsers MIGHT, and they don't send X-App-Version):**

```go
		middleware.SkipRule{Method: fiber.MethodGet, Path: "/api/v1/plans"},
```

    Place it in lexical order in the existing SkipRule list (alongside the existing `/api/v1/health` GET skip).

    Then `cd server/api && go build ./...` and `cd server/api && go test ./... -count=1 -timeout=180s`.
  </action>
  <acceptance_criteria>
    - `grep 'api.Get("/plans"' server/api/cmd/main.go` finds one match
    - `grep "ListPlansPublic(logger, db, redisClient)" server/api/cmd/main.go` finds one match
    - `grep 'Path: "/api/v1/plans"' server/api/cmd/main.go` finds one match (SkipRule)
    - `cd server/api && go build ./...` exits 0
    - `cd server/api && go test ./... -count=1 -timeout=180s` exits 0
  </acceptance_criteria>
  <automated>cd server/api && go build ./... && go test ./... -count=1 -timeout=180s</automated>
  <done>GET /api/v1/plans is mounted public, with the SkipRule so version gate doesn't block it; full suite green.</done>
</task>

</tasks>

<verification>
- `cd server/api && go build ./...` exits 0
- `cd server/api && go vet ./...` exits 0
- `cd server/api && go test ./... -count=1 -timeout=300s` exits 0
- `grep "TestListPlansPublic_CacheHitMissBust" server/api/internal/handler/plans_public_test.go` finds one match (PAY-12 evidence)
- `grep "plan_id" server/api/internal/handler/auth.go` finds at least 2 matches (claim + caller passes)
- `grep "plan_id" server/api/internal/middleware/auth.go` finds at least 2 matches (Claims field + c.Locals)
</verification>

<must_haves>
truths:
  - "GET /api/v1/plans (PUBLIC, no auth) returns the seeded free + pro plans with active offers in the caller's preferred currency (PAY-12)."
  - "Currency derived from ?currency=USD|EUR|RUB query param OR Accept-Language header (RU → RUB else USD per D-27); invalid currency returns 400."
  - "Response excludes admin-only fields (id, lava_offer_id, active_user_count) per D-27."
  - "Redis cache key cache:plans:public:{currency} with 60s TTL; admin writes bust via cache.BustPlansCache (consumed in plan 03-08)."
  - "JWT access tokens include plan_id claim (D-29); middleware extracts to c.Locals('plan_id'); empty claim → DB fallback via FindUserByID for the 5-min backward-compat window."
artifacts:
  - path: "server/api/internal/cache/plans_cache.go"
    provides: "Cache-aside wrapper with bust helper"
    contains: "BustPlansCache"
  - path: "server/api/internal/handler/plans_public.go"
    provides: "GET /api/v1/plans with currency derivation"
    contains: "deriveCurrencyFromAcceptLanguage"
  - path: "server/api/internal/handler/auth.go"
    provides: "JWT mint with plan_id claim"
    contains: '"plan_id": planID'
  - path: "server/api/internal/middleware/auth.go"
    provides: "Claims.PlanID + c.Locals('plan_id') with DB fallback"
    contains: 'c.Locals("plan_id"'
key_links:
  - from: "server/api/internal/handler/plans_public.go::ListPlansPublic"
    to: "server/api/internal/cache/plans_cache.go::GetPlansCache"
    via: "Cache-aside: GET → cache → DB on miss → SetPlansCache"
    pattern: "cache.GetPlansCache"
  - from: "server/api/internal/middleware/auth.go::AuthRequired"
    to: "server/api/internal/repository/user_repo.go::FindUserByID"
    via: "Backward-compat fallback: empty plan_id claim → DB read"
    pattern: "repository.FindUserByID\\(db, claims.UserID\\)"
</must_haves>

<threat_model>
## Trust Boundaries
| Boundary | Description |
|----------|-------------|
| Unauth browser → /api/v1/plans | Public read; no PII; cached. Input is the `currency` query param + Accept-Language header. |
| JWT-bearing client → middleware | JWT signed by us; plan_id is one more claim. Tier elevation via tampered claim is blocked by signature. |

## STRIDE Threat Register
| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-03-49 | Tampering | Client tampers ?currency=BTC | mitigate | allowedPublicCurrencies map whitelists USD/EUR/RUB; invalid → 400. Even SQL-injection-shaped query strings can't reach the DB because allowedPublicCurrencies is checked BEFORE the DB lookup. |
| T-03-50 | Information disclosure | Response leaks admin-only fields | mitigate | `publicPlan` struct EXPLICITLY OMITS id, lava_offer_id, active_user_count (D-27). PAY-12 evidence test `TestListPlansPublic_ExcludesAdminOnlyFields` greps the response body for banned substrings. |
| T-03-51 | DoS | Cache poisoning via crafted Accept-Language | mitigate | deriveCurrencyFromAcceptLanguage uses strings.HasPrefix on the lowercased trimmed header — only "ru*" → RUB, everything else → USD. The cache key is derived from the RESOLVED currency (after whitelist), not the raw input. |
| T-03-52 | Elevation of Privilege | Client tampers JWT plan_id claim | mitigate | JWT is HS256-signed; tampering invalidates the signature → 401. The middleware extracts plan_id ONLY when signature verification passes. Defence in depth: tier remains the source of truth at the handler layer (servers.go branches on role + plan_id; plan_id is used for queries, not for permission gating). |
| T-03-53 | Elevation of Privilege | Client crafts JWT with valid HMAC for a paid plan_id | accept | Only the server has the JWT secret. If the JWT secret leaks, the whole auth model collapses — out of scope for THIS plan. JWT_SECRET is required at startup (HOTFIX-08) and rotated per ADR §15. |
| T-03-54 | DoS | /plans amplified traffic | accept | Cache TTL 60s caps DB load to 1 query per currency per 60 seconds. Global per-IP rate limiter (HOTFIX-03) applies. |
| T-03-55 | Tampering | Stale cache after admin updates a plan | accept (mitigation in plan 03-08) | TTL 60s bounds staleness; explicit cache.BustPlansCache call from admin write handlers (plan 03-08) provides immediate invalidation. Single-replica deployment makes this trivial. |
| T-03-56 | Information disclosure | Empty plan_id in JWT triggers DB read on every protected request | accept | 5-minute transition window only — after access TTL elapses, all JWTs carry the claim. The DB read is one indexed PK lookup (~0.5ms) per request; the existing HOTFIX-02 path already does FindUserByID per request so the cost is doubled but still sub-millisecond. |

ASVS L1 scoping for this plan (public /plans endpoint + middleware change); JWT mint adjustment is L2 (payment-adjacent). Controls applied: V4 access control (currency whitelist), V5 input validation (Accept-Language → enum), V8 data protection (admin-only field omission), V13 API contract (cache-aside fail-open).
</threat_model>

<success_criteria>
1. `cd server/api && go build ./...` exits 0.
2. `cd server/api && go test ./... -count=1 -timeout=300s` exits 0.
3. `TestListPlansPublic_CacheHitMissBust` passes (PAY-12 named test).
4. `TestListPlansPublic_ExcludesAdminOnlyFields` passes (D-27).
5. JWT mint includes plan_id (verified by grepping `"plan_id": planID`).
6. Middleware sets c.Locals("plan_id") with backward-compat DB fallback.
</success_criteria>

<output>
T01..T05 land as 5 atomic commits (`feat(03-07): ...`); planner commits this plan file once with `docs(03): plan public-plans-jwt-cache`.
</output>
