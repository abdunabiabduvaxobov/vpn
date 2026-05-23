---
phase: 3
plan: 06
type: execute
slug: lava-top-plans-catalog
plan_number: 6
wave: 3
depends_on: [1, 2, 3, 5]
files_modified:
  - server/api/internal/middleware/lava_ip_allowlist.go
  - server/api/internal/middleware/lava_ip_allowlist_test.go
  - server/api/internal/repository/webhook_event_repo.go
  - server/api/internal/repository/webhook_event_repo_test.go
  - server/api/internal/handler/webhook_lava.go
  - server/api/internal/handler/webhook_lava_test.go
  - server/api/cmd/main.go
autonomous: true
requirements_addressed: [PAY-03, PAY-04, PAY-05, PAY-06, PAY-07, PAY-08, PAY-09]
estimated_complexity: high
must_haves:
  refs:
    - "See <must_haves> XML block in plan body for canonical truths / artifacts / key_links"

---

<objective>
Land the inbound webhook stack — the launch security gate. Five components:
1. `internal/middleware/lava_ip_allowlist.go` — route-scoped middleware that 403s any request whose `c.Context().RemoteIP()` is outside the parsed CIDR list (RESEARCH §2.2 — Fiber's TrustedProxies alone does NOT reject). PAY-06.
2. `internal/repository/webhook_event_repo.go` — `InsertWebhookEventIfNew` using `clause.OnConflict{DoNothing: true}` returning `(isNew bool, err error)` via RowsAffected (PAY-04).
3. `internal/handler/webhook_lava.go` — `HandleLavaWebhook` with X-Api-Key check (PAY-07), payload parse, idempotency insert, 5-way event-type dispatch (PAY-03), and the per-event handlers (payment.success, subscription.recurring.payment.success, payment.failed, subscription.recurring.payment.failed, subscription.cancelled per D-19). Returns 500 on processing error so lava retries (PAY-05).
4. PAY-08 chain: webhook payload's `contractId` → FindInvoiceByLavaID (initial payments) OR `parentContractId` → FindLavaContractByContractID (renewals) → resolves the lava `offer_id` → FindOfferByLavaOfferID(offer_id).PlanID → SetUserPlan. NEVER reads tier from request body.
5. Wire the route + middleware in `cmd/main.go` (replaces the 03-05 placeholder comment).
</objective>

<context>
@.planning/phases/03-lava-top-plans-catalog/03-CONTEXT.md
@.planning/phases/03-lava-top-plans-catalog/03-RESEARCH.md
@server/api/internal/lava/webhook.go
@server/api/internal/lava/dto.go
@server/api/internal/repository/plan_repo.go
@server/api/internal/repository/invoice_repo.go
@server/api/internal/model/lava_webhook_event.go
@server/api/internal/model/lava_contract.go
@server/api/cmd/main.go
</context>

<interfaces>
New public surfaces:

```go
// internal/middleware/lava_ip_allowlist.go
// Mounted ONLY on POST /api/v1/webhook/lava. Parses CIDRs once at startup;
// returns the middleware closure that reads c.Context().RemoteIP() (TCP layer)
// and 403s on mismatch. Bare IPs (no /CIDR) are normalised to /32 (v4) or /128 (v6).
func LavaWebhookIPAllowlist(cidrs []string, logger *zap.Logger) (fiber.Handler, error)

// internal/repository/webhook_event_repo.go
// Insert returns (isNew=true, nil) on first-delivery, (isNew=false, nil) on duplicate (PAY-04).
func InsertWebhookEventIfNew(db *gorm.DB, event *model.LavaWebhookEvent) (isNew bool, err error)
func MarkWebhookProcessed(db *gorm.DB, eventID string, errStr *string) error
func FindLavaContractByContractID(db *gorm.DB, contractID string) (*model.LavaContract, error)
func UpsertLavaContract(db *gorm.DB, c *model.LavaContract) error

// internal/handler/webhook_lava.go
func HandleLavaWebhook(logger *zap.Logger, cfg *config.Config, db *gorm.DB, lavaClient *lava.Client) fiber.Handler
```

Periodicity → time.Duration mapping (used in this plan to compute expires_at when lava doesn't supply it directly in the webhook):

```go
// periodicityToDuration converts lava periodicity strings to time.Duration.
// MONTHLY = 30 days; PERIOD_YEAR = 365 days; PERIOD_90_DAYS = 90 days; etc.
// ONE_TIME returns 0 (no recurrence — caller must handle).
func periodicityToDuration(p string) time.Duration
```

5 event-type dispatch (D-19):
- `payment.success` → resolve user via FindInvoiceByLavaID(contractId) → FindOfferByLavaOfferID(invoice.offer_id) → SetUserPlan(user, planID, &contractId, expires_at) + UpsertLavaContract + UpdateInvoiceStatus("paid")
- `subscription.recurring.payment.success` → FindLavaContractByContractID(parentContractId) → extend ExpiresAt by one period → UpsertLavaContract (renewal child contract) + extend users.subscription_expires_at
- `payment.failed` → FindInvoiceByLavaID(contractId) → UpdateInvoiceStatus("failed"). NO tier change.
- `subscription.recurring.payment.failed` → FindLavaContractByContractID(parentContractId) → set is_active=false. Tier UNCHANGED (cron handles downgrade after expires_at).
- `subscription.cancelled` → FindLavaContractByContractID(contractId) → set cancelled_at=now() + is_active=false. Tier UNCHANGED.
</interfaces>

<tasks>

<task type="auto">
  <id>03-06-T01</id>
  <name>Write internal/middleware/lava_ip_allowlist.go + unit tests (CIDR parsing + RemoteIP rejection — PAY-06)</name>
  <files>
    server/api/internal/middleware/lava_ip_allowlist.go,
    server/api/internal/middleware/lava_ip_allowlist_test.go
  </files>
  <read_first>
    - .planning/phases/03-lava-top-plans-catalog/03-RESEARCH.md §2.1 (Fiber's EnableTrustedProxyCheck WON'T reject), §2.2 (route-scoped middleware required), §2.4 (RemoteIP vs c.IP())
    - .planning/phases/03-lava-top-plans-catalog/03-CONTEXT.md D-16 (LAVA_WEBHOOK_ALLOWED_CIDRS env), D-32 §1 (IP spoof via X-Forwarded-For)
    - server/api/internal/middleware/audit.go (Phase 2 pattern for middleware-with-constructor — returns fiber.Handler)
  </read_first>
  <action>
    **(a) `server/api/internal/middleware/lava_ip_allowlist.go`:**

```go
// Package middleware contains Fiber middleware shared across the API.
// LavaWebhookIPAllowlist is a route-scoped guard for POST /api/v1/webhook/lava.
//
// RESEARCH §2.1 documents that Fiber v2's EnableTrustedProxyCheck does NOT
// reject untrusted IPs — it silently ignores their X-Forwarded-* headers and
// falls back to RemoteIP(). To satisfy PAY-06 ("rejected at the IP allowlist
// layer regardless of X-Forwarded-For content") we need a dedicated middleware
// that reads c.Context().RemoteIP() (the TCP-layer source IP, immune to
// proxy-header spoofing) and 403s on mismatch.
package middleware

import (
	"fmt"
	"net"
	"strings"

	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
)

// LavaWebhookIPAllowlist returns a Fiber handler that 403s any request whose
// TCP RemoteIP is outside the supplied CIDR list. Bare IPs (without /CIDR
// suffix) are normalised to /32 (IPv4) or /128 (IPv6). The CIDR slice is
// parsed ONCE at startup; the returned handler is hot-path safe (no parsing
// per request — only IPNet.Contains).
//
// Error from this function (returned at startup) is fatal — cmd/main.go
// turns it into logger.Fatal so a malformed LAVA_WEBHOOK_ALLOWED_CIDRS env
// fails the process at boot rather than at the first webhook delivery.
func LavaWebhookIPAllowlist(cidrs []string, logger *zap.Logger) (fiber.Handler, error) {
	if len(cidrs) == 0 {
		return nil, fmt.Errorf("LavaWebhookIPAllowlist: cidrs slice is empty (LAVA_WEBHOOK_ALLOWED_CIDRS must contain at least one entry)")
	}
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, s := range cidrs {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		// Bare IP → /32 (v4) or /128 (v6).
		if !strings.Contains(s, "/") {
			if strings.Contains(s, ":") {
				s += "/128"
			} else {
				s += "/32"
			}
		}
		_, ipNet, err := net.ParseCIDR(s)
		if err != nil {
			return nil, fmt.Errorf("LavaWebhookIPAllowlist: parse %q: %w", s, err)
		}
		nets = append(nets, ipNet)
	}
	if len(nets) == 0 {
		return nil, fmt.Errorf("LavaWebhookIPAllowlist: no valid CIDRs after trimming")
	}

	return func(c *fiber.Ctx) error {
		// c.Context().RemoteIP() returns the raw TCP-connection IP — NOT
		// influenced by TrustedProxies / X-Forwarded-For (RESEARCH §2.4).
		remote := c.Context().RemoteIP()
		for _, n := range nets {
			if n.Contains(remote) {
				return c.Next()
			}
		}
		logger.Warn("lava webhook: IP allowlist reject",
			zap.String("remote_ip", remote.String()),
			zap.String("path", c.Path()),
		)
		return c.SendStatus(fiber.StatusForbidden)
	}, nil
}
```

    **(b) `server/api/internal/middleware/lava_ip_allowlist_test.go`:**

```go
package middleware

import (
	"net"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"
)

// TestLavaWebhookIPAllowlist_RejectsOutOfRange covers PAY-06 (named in 03-VALIDATION.md).
//
// Approach: directly invoke the middleware closure with a synthesized fiber.Ctx
// whose RemoteIP we control via the underlying fasthttp.RequestCtx. We use
// app.AcquireCtx to construct the ctx without going through app.Test (which
// can't easily spoof the TCP source IP).
func TestLavaWebhookIPAllowlist_RejectsOutOfRange(t *testing.T) {
	allow, err := LavaWebhookIPAllowlist([]string{"158.160.60.174/32", "10.0.0.0/8"}, zap.NewNop())
	if err != nil {
		t.Fatalf("ctor: %v", err)
	}

	tests := []struct {
		name      string
		remoteIP  string
		wantNext  bool // true => middleware called c.Next() (allowed)
		wantStat  int
	}{
		{"exact allowlist match", "158.160.60.174", true, 200},
		{"inside CIDR /8", "10.5.5.5", true, 200},
		{"outside allowlist", "8.8.8.8", false, fiber.StatusForbidden},
		{"localhost rejected", "127.0.0.1", false, fiber.StatusForbidden},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			app := fiber.New()
			app.Post("/webhook/lava", allow, func(c *fiber.Ctx) error {
				return c.SendStatus(200)
			})
			req := httptest.NewRequest("POST", "/webhook/lava", nil)
			// app.Test uses an in-process transport; the RemoteAddr ends up
			// as "0.0.0.0:0" by default. To inject a custom RemoteIP we
			// override the test request's RemoteAddr.
			req.RemoteAddr = tc.remoteIP + ":12345"
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("Test: %v", err)
			}
			if resp.StatusCode != tc.wantStat {
				t.Errorf("expected status %d, got %d", tc.wantStat, resp.StatusCode)
			}
		})
	}

	_ = fasthttp.Hijacker(nil) // silence the import (if not otherwise referenced)
	_ = net.ParseIP            // silence the import (if not otherwise referenced)
}

// TestLavaWebhookIPAllowlist_ParsesBareIPAsSlash32 covers the bare-IP normalisation.
func TestLavaWebhookIPAllowlist_ParsesBareIPAsSlash32(t *testing.T) {
	if _, err := LavaWebhookIPAllowlist([]string{"158.160.60.174"}, zap.NewNop()); err != nil {
		t.Errorf("bare IP must be parseable, got %v", err)
	}
}

// TestLavaWebhookIPAllowlist_RejectsMalformedCIDR covers config-fail-fast.
func TestLavaWebhookIPAllowlist_RejectsMalformedCIDR(t *testing.T) {
	if _, err := LavaWebhookIPAllowlist([]string{"not-an-ip"}, zap.NewNop()); err == nil {
		t.Errorf("expected error on malformed CIDR")
	}
	if _, err := LavaWebhookIPAllowlist([]string{}, zap.NewNop()); err == nil {
		t.Errorf("expected error on empty list")
	}
}
```

    Note: `app.Test(req)` does set RemoteAddr from `req.RemoteAddr` in the fasthttp adapter — verify this works by running the test. If it doesn't (older Fiber versions), the executor falls back to constructing a `*fasthttp.RequestCtx` directly and calling the middleware closure as a function. Document this fallback in a comment.

    Run `cd server/api && go test ./internal/middleware/ -run "TestLavaWebhookIPAllowlist" -count=1 -timeout=30s -v`.
  </action>
  <acceptance_criteria>
    - Files `server/api/internal/middleware/lava_ip_allowlist.go` and `lava_ip_allowlist_test.go` exist
    - `grep "c.Context().RemoteIP()" server/api/internal/middleware/lava_ip_allowlist.go` finds one match (the explicit TCP-layer read per RESEARCH §2.4)
    - `grep "net.ParseCIDR" server/api/internal/middleware/lava_ip_allowlist.go` finds one match
    - `grep "TestLavaWebhookIPAllowlist_RejectsOutOfRange\|TestLavaWebhookIPAllowlist" server/api/internal/middleware/lava_ip_allowlist_test.go` finds matches (PAY-06 evidence test named in 03-VALIDATION.md)
    - `grep "fiber.StatusForbidden" server/api/internal/middleware/lava_ip_allowlist.go` finds one match (403 on reject)
    - `cd server/api && go test ./internal/middleware/ -run "TestLavaWebhookIPAllowlist" -count=1 -timeout=30s` exits 0
  </acceptance_criteria>
  <automated>cd server/api && go test ./internal/middleware/ -run "TestLavaWebhookIPAllowlist" -count=1 -timeout=30s</automated>
  <done>Route-scoped IP allowlist middleware compiles + tests pass; reads c.Context().RemoteIP() not c.IP(); 403s on out-of-range.</done>
</task>

<task type="auto">
  <id>03-06-T02</id>
  <name>Write webhook_event_repo.go + tests (InsertWebhookEventIfNew via OnConflict{DoNothing}; FindLavaContractByContractID; UpsertLavaContract; MarkWebhookProcessed)</name>
  <files>
    server/api/internal/repository/webhook_event_repo.go,
    server/api/internal/repository/webhook_event_repo_test.go
  </files>
  <read_first>
    - server/api/internal/model/lava_webhook_event.go (T04 of plan 03-01 — Payload datatypes.JSON column)
    - server/api/internal/model/lava_contract.go (T04 of plan 03-01 — ContractID uniqueIndex, ParentContractID nullable)
    - .planning/phases/03-lava-top-plans-catalog/03-RESEARCH.md §3.1 (InsertWebhookEventIfNew via OnConflict{DoNothing} + RowsAffected detection), §3.2 (UpsertLavaContract via OnConflict{Columns:[contract_id], DoUpdates:[is_active, expires_at, cancelled_at, parent_contract_id]})
    - .planning/phases/03-lava-top-plans-catalog/03-CONTEXT.md D-18 (UNIQUE → 200 OK no-op), D-19 (UpsertLavaContract semantics)
  </read_first>
  <action>
    Two new files.

    **(a) `server/api/internal/repository/webhook_event_repo.go`:**

```go
package repository

import (
	"errors"
	"time"

	"vpnapp/server/api/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// InsertWebhookEventIfNew inserts a new LavaWebhookEvent row using
// INSERT ... ON CONFLICT DO NOTHING. On Postgres this returns RowsAffected=0
// when a duplicate (by the natural-key UNIQUE index from migration 020) hits.
//
// Returns:
//   - (true, nil) on first delivery — caller proceeds to dispatch event-type handlers.
//   - (false, nil) on duplicate (PAY-04) — caller returns 200 immediately.
//   - (false, err) on DB error — caller returns 500 (lava retries per PAY-05).
//
// CRITICAL: This insert MUST commit independently of the event-processing
// transaction. RESEARCH §3.4 explains: if Steps 3 and 4 are wrapped in one
// transaction, a Step-4 failure rolls back the Step-3 dedup record, allowing
// the retry to bypass idempotency. Caller wraps THIS function's call OUTSIDE
// any larger TX.
func InsertWebhookEventIfNew(db *gorm.DB, event *model.LavaWebhookEvent) (bool, error) {
	result := db.Clauses(clause.OnConflict{DoNothing: true}).Create(event)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// MarkWebhookProcessed updates lava_webhook_events.processed_at = now() OR
// lava_webhook_events.error = errStr based on whether errStr is nil.
// Best-effort — caller does NOT propagate error from this call (the side
// effect of failing here is a stale forensic record; the 500 returned to
// lava ensures retry handles the real work).
func MarkWebhookProcessed(db *gorm.DB, eventID string, errStr *string) error {
	now := time.Now()
	updates := map[string]interface{}{
		"processed_at": &now,
		"error":        errStr,
	}
	return db.Model(&model.LavaWebhookEvent{}).Where("id = ?", eventID).Updates(updates).Error
}

// FindLavaContractByContractID returns the lava-side recurring contract row.
// Used by webhook handlers to resolve renewals (contractId on
// subscription.recurring.* events) or cancellations.
func FindLavaContractByContractID(db *gorm.DB, contractID string) (*model.LavaContract, error) {
	var c model.LavaContract
	result := db.Where("contract_id = ?", contractID).First(&c)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, result.Error
	}
	return &c, nil
}

// UpsertLavaContract inserts a new contract or updates the lifecycle fields
// (is_active, expires_at, cancelled_at, parent_contract_id) of an existing
// row when contract_id collides. RESEARCH §3.2 prescribes the exact clause.
//
// IMPORTANT: write-once fields (user_id, offer_id, plan, periodicity, currency,
// started_at) are NOT in the DoUpdates list — a hostile or buggy webhook
// payload cannot rewrite them after the contract is first observed.
func UpsertLavaContract(db *gorm.DB, c *model.LavaContract) error {
	return db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "contract_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"is_active",
			"expires_at",
			"cancelled_at",
			"parent_contract_id",
		}),
	}).Create(c).Error
}
```

    **(b) `server/api/internal/repository/webhook_event_repo_test.go`:**

```go
package repository_test

import (
	"testing"
	"time"

	"vpnapp/server/api/internal/model"
	"vpnapp/server/api/internal/repository"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupWebhookRepoDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// SQLite does NOT enforce the COALESCE expression UNIQUE index from
	// migration 020 — for unit tests we simulate it by adding a simpler
	// UNIQUE on (event_type, contract_id, payload). That's enough to
	// validate OnConflict{DoNothing} behaviour at the GORM layer; the
	// Postgres-level COALESCE expression is tested by migrations_test.go
	// (plan 03-01 T06).
	stmts := []string{
		`CREATE TABLE lava_webhook_events (
			id TEXT PRIMARY KEY,
			event_type TEXT NOT NULL,
			contract_id TEXT,
			invoice_id TEXT,
			payload TEXT NOT NULL,
			received_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			processed_at TIMESTAMP,
			error TEXT,
			UNIQUE (event_type, contract_id, payload)
		)`,
		`CREATE TABLE lava_contracts (
			id TEXT PRIMARY KEY,
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
	}
	for _, s := range stmts {
		if err := db.Exec(s).Error; err != nil {
			t.Fatalf("DDL: %v", err)
		}
	}
	return db
}

// TestInsertWebhookEventIfNew_Idempotent is the PAY-04 named test (03-VALIDATION.md).
// First insert returns isNew=true; second insert with same natural key returns isNew=false.
func TestInsertWebhookEventIfNew_Idempotent(t *testing.T) {
	db := setupWebhookRepoDB(t)
	payload := `{"timestamp":"2026-05-23T10:00:00Z","contractId":"contract-X"}`
	contractID := "contract-X"
	first := &model.LavaWebhookEvent{
		ID: uuid.NewString(), EventType: "payment.success",
		ContractID: &contractID, Payload: datatypes.JSON(payload),
	}
	isNew, err := repository.InsertWebhookEventIfNew(db, first)
	if err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if !isNew {
		t.Errorf("first insert must return isNew=true")
	}

	// Re-insert the SAME natural key.
	dup := &model.LavaWebhookEvent{
		ID: uuid.NewString(), EventType: "payment.success",
		ContractID: &contractID, Payload: datatypes.JSON(payload),
	}
	isNew2, err := repository.InsertWebhookEventIfNew(db, dup)
	if err != nil {
		t.Fatalf("dup insert: %v", err)
	}
	if isNew2 {
		t.Errorf("PAY-04: duplicate must return isNew=false (RowsAffected=0)")
	}

	// Confirm exactly one row in the table.
	var n int64
	_ = db.Model(&model.LavaWebhookEvent{}).Count(&n).Error
	if n != 1 {
		t.Errorf("PAY-04: expected exactly 1 row, got %d", n)
	}
}

func TestMarkWebhookProcessed_SetsProcessedAtOrError(t *testing.T) {
	db := setupWebhookRepoDB(t)
	ev := &model.LavaWebhookEvent{
		ID: uuid.NewString(), EventType: "payment.failed",
		Payload: datatypes.JSON(`{}`),
	}
	if err := db.Create(ev).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := repository.MarkWebhookProcessed(db, ev.ID, nil); err != nil {
		t.Fatalf("MarkWebhookProcessed: %v", err)
	}
	var reloaded model.LavaWebhookEvent
	if err := db.First(&reloaded, "id = ?", ev.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.ProcessedAt == nil {
		t.Errorf("expected processed_at set, got nil")
	}
	// Now mark with an error message.
	errStr := "downstream DB outage"
	if err := repository.MarkWebhookProcessed(db, ev.ID, &errStr); err != nil {
		t.Fatalf("MarkWebhookProcessed error: %v", err)
	}
	if err := db.First(&reloaded, "id = ?", ev.ID).Error; err != nil {
		t.Fatalf("reload2: %v", err)
	}
	if reloaded.Error == nil || *reloaded.Error != "downstream DB outage" {
		t.Errorf("expected error set, got %v", reloaded.Error)
	}
}

func TestFindLavaContractByContractID_FoundAndNotFound(t *testing.T) {
	db := setupWebhookRepoDB(t)
	c := &model.LavaContract{
		ID: uuid.NewString(), UserID: uuid.NewString(), ContractID: "contract-A",
		OfferID: "off-1", Plan: "pro", Periodicity: "MONTHLY", Currency: "USD",
		IsActive: true,
	}
	if err := db.Create(c).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	got, err := repository.FindLavaContractByContractID(db, "contract-A")
	if err != nil || got.ID != c.ID {
		t.Errorf("expected to find contract-A: %+v err=%v", got, err)
	}
	if _, err := repository.FindLavaContractByContractID(db, "missing"); err != repository.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestUpsertLavaContract_InsertThenUpdate(t *testing.T) {
	db := setupWebhookRepoDB(t)
	uid := uuid.NewString()
	c := &model.LavaContract{
		ID: uuid.NewString(), UserID: uid, ContractID: "contract-U",
		OfferID: "off-1", Plan: "pro", Periodicity: "MONTHLY", Currency: "USD",
		IsActive: true,
	}
	if err := repository.UpsertLavaContract(db, c); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	// Update with new expires_at + cancelled_at.
	exp := time.Now().Add(60 * 24 * time.Hour)
	cancelled := time.Now()
	c2 := &model.LavaContract{
		ID: uuid.NewString(), UserID: uid, ContractID: "contract-U",
		OfferID: "off-1", Plan: "pro", Periodicity: "MONTHLY", Currency: "USD",
		IsActive: false, ExpiresAt: &exp, CancelledAt: &cancelled,
	}
	if err := repository.UpsertLavaContract(db, c2); err != nil {
		t.Fatalf("update upsert: %v", err)
	}
	// Confirm there's still exactly one row with contract_id="contract-U".
	var n int64
	_ = db.Model(&model.LavaContract{}).Where("contract_id = ?", "contract-U").Count(&n).Error
	if n != 1 {
		t.Errorf("expected 1 row after upsert, got %d", n)
	}
	// Confirm the IS_ACTIVE flipped.
	var reloaded model.LavaContract
	if err := db.Where("contract_id = ?", "contract-U").First(&reloaded).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.IsActive {
		t.Errorf("expected is_active=false after second upsert")
	}
	if reloaded.CancelledAt == nil {
		t.Errorf("expected cancelled_at set")
	}
}
```

    Run `cd server/api && go test ./internal/repository/ -run "TestInsertWebhookEventIfNew_Idempotent|TestMarkWebhookProcessed|TestFindLavaContractByContractID|TestUpsertLavaContract" -count=1 -timeout=30s -v`.
  </action>
  <acceptance_criteria>
    - Files `server/api/internal/repository/webhook_event_repo.go` and `webhook_event_repo_test.go` exist
    - `grep "clause.OnConflict{DoNothing: true}" server/api/internal/repository/webhook_event_repo.go` finds one match
    - `grep "clause.OnConflict{" server/api/internal/repository/webhook_event_repo.go` finds at least 2 matches (DoNothing + the Columns/DoUpdates upsert)
    - `grep "AssignmentColumns" server/api/internal/repository/webhook_event_repo.go` finds one match
    - `grep "TestInsertWebhookEventIfNew_Idempotent" server/api/internal/repository/webhook_event_repo_test.go` finds one match (PAY-04 named test from 03-VALIDATION.md)
    - `cd server/api && go test ./internal/repository/ -run "TestInsertWebhookEventIfNew|TestMarkWebhookProcessed|TestFindLavaContractByContractID|TestUpsertLavaContract" -count=1 -timeout=30s` exits 0
  </acceptance_criteria>
  <automated>cd server/api && go test ./internal/repository/ -run "TestInsertWebhookEventIfNew|TestMarkWebhookProcessed|TestFindLavaContractByContractID|TestUpsertLavaContract" -count=1 -timeout=30s</automated>
  <done>webhook_event_repo.go has 4 functions (Insert idempotent + MarkProcessed + FindContract + UpsertContract); all sqlite tests pass; OnConflict shapes match RESEARCH §3.1/§3.2.</done>
</task>

<task type="auto">
  <id>03-06-T03</id>
  <name>Write webhook_lava.go — full HandleLavaWebhook with 5-event dispatch (PAY-03 to PAY-09)</name>
  <files>server/api/internal/handler/webhook_lava.go</files>
  <read_first>
    - server/api/internal/lava/dto.go (T02 of plan 03-02 — WebhookEvent shape)
    - server/api/internal/lava/webhook.go (T02 of plan 03-02 — VerifyAPIKey)
    - server/api/internal/repository/webhook_event_repo.go (T02 of THIS plan — InsertWebhookEventIfNew, FindLavaContractByContractID, UpsertLavaContract, MarkWebhookProcessed)
    - server/api/internal/repository/plan_repo.go (FindOfferByLavaOfferID, SetUserPlan, FindSystemPlanID, FindPlanByID)
    - server/api/internal/repository/invoice_repo.go (FindInvoiceByLavaID, UpdateInvoiceStatus)
    - .planning/phases/03-lava-top-plans-catalog/03-RESEARCH.md §3.4 (handler skeleton — one-tx-per-event-type), §1.5 (5 event-type payloads), §"Security Domain" rows
    - .planning/phases/03-lava-top-plans-catalog/03-CONTEXT.md D-17 (X-Api-Key compare), D-18 (UNIQUE 200), D-19 (5 event semantics), D-32 (full threat list)
    - docs/ADR-007-lava-sso-rework.md §9.3 (event dispatch + idempotency mechanics)
  </read_first>
  <action>
    Create `server/api/internal/handler/webhook_lava.go`. This is the most security-critical file in Phase 3 — every line is intentional.

```go
package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"vpnapp/server/api/internal/config"
	"vpnapp/server/api/internal/lava"
	"vpnapp/server/api/internal/model"
	"vpnapp/server/api/internal/repository"

	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// HandleLavaWebhook handles POST /api/v1/webhook/lava.
//
// Mounted with the LavaWebhookIPAllowlist middleware (cmd/main.go) so any
// request from outside the configured CIDR set is 403'd BEFORE this handler
// is reached.
//
// Auth: X-Api-Key header compared via crypto/subtle.ConstantTimeCompare to
// LAVA_WEBHOOK_SECRET (and LAVA_WEBHOOK_SECRET_PREVIOUS during rotation).
//
// Idempotency: every received event is INSERTed via OnConflict{DoNothing} into
// lava_webhook_events. Duplicates (RowsAffected=0) return 200 immediately
// without re-applying side effects (PAY-04). Processing errors return 500 so
// lava retries (PAY-05).
//
// Event dispatch (D-19):
//
//	payment.success                          → grant tier, upsert contract
//	subscription.recurring.payment.success   → extend expires_at
//	payment.failed                           → mark invoice failed
//	subscription.recurring.payment.failed    → contract is_active=false (tier WAITS for cron)
//	subscription.cancelled                   → contract cancelled_at + is_active=false
//
// PAY-08 invariant: tier is derived from the lava `offer_id` via plan_offers
// reverse-lookup, NEVER from any client-supplied metadata in the payload.
// The resolution chain is:
//
//	payload.contractId
//	    └─→ invoices.lava_invoice_id == contractId   (initial payment)
//	OR
//	payload.parentContractId
//	    └─→ lava_contracts.contract_id == parent     (renewal)
//	         └─→ invoices.lava_invoice_id            (back-trace via started_at lookup)
//
// Once the invoice row is resolved, `invoices.offer_id` (the lava-side UUID)
// is the lookup key for FindOfferByLavaOfferID → PlanID → SetUserPlan.
func HandleLavaWebhook(logger *zap.Logger, cfg *config.Config, db *gorm.DB, lavaClient *lava.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// 1. X-Api-Key check (PAY-07). The IP allowlist middleware already 403'd
		//    anyone outside the CIDR list before we got here.
		apiKey := c.Get("X-Api-Key")
		if !lava.VerifyAPIKey(apiKey, cfg.LavaWebhookSecret, cfg.LavaWebhookSecretPrevious) {
			logger.Warn("webhook: X-Api-Key mismatch", zap.String("path", c.Path()))
			return c.SendStatus(fiber.StatusUnauthorized)
		}

		// 2. Parse payload + capture raw body for jsonb persistence.
		rawBody := c.Body()
		var event lava.WebhookEvent
		if err := json.Unmarshal(rawBody, &event); err != nil {
			logger.Warn("webhook: invalid JSON payload", zap.Error(err))
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid payload"})
		}
		if event.EventType == "" || event.ContractID == "" {
			logger.Warn("webhook: missing eventType or contractId", zap.String("event_type", event.EventType))
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "missing eventType or contractId"})
		}

		// 3. Idempotency INSERT. ContractID copied to a pointer for the model.
		contractID := event.ContractID
		rec := &model.LavaWebhookEvent{
			EventType:  event.EventType,
			ContractID: &contractID,
			Payload:    datatypes.JSON(rawBody),
		}
		isNew, err := repository.InsertWebhookEventIfNew(db, rec)
		if err != nil {
			logger.Error("webhook: idempotency insert failed", zap.Error(err))
			// 500 → lava retries (PAY-05).
			return c.SendStatus(fiber.StatusInternalServerError)
		}
		if !isNew {
			// Duplicate — return 200 without re-applying (PAY-04).
			logger.Info("webhook: duplicate event ignored",
				zap.String("event_type", event.EventType),
				zap.String("contract_id", contractID),
			)
			return c.SendStatus(fiber.StatusOK)
		}

		// 4. Dispatch on event type.
		var processErr error
		switch event.EventType {
		case "payment.success":
			processErr = handleLavaPaymentSuccess(logger, db, &event)
		case "subscription.recurring.payment.success":
			processErr = handleLavaRecurringSuccess(logger, db, &event)
		case "payment.failed":
			processErr = handleLavaPaymentFailed(logger, db, &event)
		case "subscription.recurring.payment.failed":
			processErr = handleLavaRecurringFailed(logger, db, &event)
		case "subscription.cancelled":
			processErr = handleLavaSubscriptionCancelled(logger, db, &event)
		default:
			logger.Warn("webhook: unknown event type ignored",
				zap.String("event_type", event.EventType),
				zap.String("contract_id", contractID),
			)
			// Unknown but valid signature — record received and return 200.
			_ = repository.MarkWebhookProcessed(db, rec.ID, nil)
			return c.SendStatus(fiber.StatusOK)
		}

		// 5. Record outcome.
		if processErr != nil {
			errStr := processErr.Error()
			_ = repository.MarkWebhookProcessed(db, rec.ID, &errStr)
			logger.Error("webhook: processing failed",
				zap.String("event_type", event.EventType),
				zap.String("contract_id", contractID),
				zap.Error(processErr),
			)
			// 500 → lava retries (PAY-05). The event row stays so forensics
			// can correlate the retry.
			return c.SendStatus(fiber.StatusInternalServerError)
		}
		_ = repository.MarkWebhookProcessed(db, rec.ID, nil)
		return c.SendStatus(fiber.StatusOK)
	}
}

// handleLavaPaymentSuccess processes the first-payment event.
//
// Flow:
//   1. Look up invoice by lava_invoice_id == contractId.
//   2. Look up offer by invoice.offer_id (the lava-side offer UUID).
//   3. Compute expires_at locally: started_at + periodicity (RESEARCH §1.2 —
//      first payment.success doesn't carry expiredAt directly; we compute
//      from periodicity).
//   4. SetUserPlan(userID, planID, &contractId, expires_at) — transactional.
//   5. UpsertLavaContract.
//   6. UpdateInvoiceStatus to "paid".
func handleLavaPaymentSuccess(logger *zap.Logger, db *gorm.DB, event *lava.WebhookEvent) error {
	inv, err := repository.FindInvoiceByLavaID(db, event.ContractID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return fmt.Errorf("payment.success: no invoice for contractId=%s", event.ContractID)
		}
		return fmt.Errorf("payment.success: FindInvoiceByLavaID: %w", err)
	}
	offer, err := repository.FindOfferByLavaOfferID(db, inv.OfferID)
	if err != nil {
		return fmt.Errorf("payment.success: FindOfferByLavaOfferID(%s): %w", inv.OfferID, err)
	}

	// Compute expires_at locally from periodicity.
	dur := periodicityToDuration(inv.Periodicity)
	startedAt := time.Now()
	var expiresAt *time.Time
	if dur > 0 {
		t := startedAt.Add(dur)
		expiresAt = &t
	}

	// 1. SetUserPlan — transactional update of users + subscriptions row.
	contractID := event.ContractID
	if err := repository.SetUserPlan(db, inv.UserID, offer.PlanID, &contractID, expiresAt); err != nil {
		return fmt.Errorf("payment.success: SetUserPlan(%s, %s): %w", inv.UserID, offer.PlanID, err)
	}

	// 2. UpsertLavaContract.
	if err := repository.UpsertLavaContract(db, &model.LavaContract{
		UserID:       inv.UserID,
		ContractID:   contractID,
		OfferID:      inv.OfferID,
		Plan:         inv.Plan,
		Periodicity:  inv.Periodicity,
		Currency:     inv.Currency,
		IsActive:     true,
		StartedAt:    startedAt,
		ExpiresAt:    expiresAt,
	}); err != nil {
		return fmt.Errorf("payment.success: UpsertLavaContract: %w", err)
	}

	// 3. Flip invoice status.
	if err := repository.UpdateInvoiceStatus(db, inv.ID, "paid"); err != nil {
		return fmt.Errorf("payment.success: UpdateInvoiceStatus: %w", err)
	}

	logger.Info("webhook: payment.success applied",
		zap.String("user_id", inv.UserID),
		zap.String("plan_id", offer.PlanID),
		zap.String("contract_id", contractID),
		zap.Timep("expires_at", expiresAt),
	)
	return nil
}

// handleLavaRecurringSuccess extends expires_at by one period.
//
// Renewal events carry parentContractId (RESEARCH §1.5) — that points at the
// ORIGINAL contract; the event's contractId is the renewal's invoice id.
// We look up the parent contract to find the user, then compute new
// expires_at = old_expires_at + periodicity.
func handleLavaRecurringSuccess(logger *zap.Logger, db *gorm.DB, event *lava.WebhookEvent) error {
	parentID := ""
	if event.ParentContractID != nil {
		parentID = *event.ParentContractID
	}
	if parentID == "" {
		return fmt.Errorf("recurring.success: missing parentContractId")
	}
	parent, err := repository.FindLavaContractByContractID(db, parentID)
	if err != nil {
		return fmt.Errorf("recurring.success: FindLavaContractByContractID(%s): %w", parentID, err)
	}

	// Extend expires_at by one period.
	dur := periodicityToDuration(parent.Periodicity)
	startedAt := time.Now()
	newExp := startedAt.Add(dur)
	// If parent already has a future expires_at, extend from THAT — preserves
	// the user's paid-for time even if the renewal hits "early".
	if parent.ExpiresAt != nil && parent.ExpiresAt.After(startedAt) {
		newExp = parent.ExpiresAt.Add(dur)
	}

	// SetUserPlan with new expires_at (keeps same plan_id; just refreshes expiry).
	contractID := event.ContractID
	if err := repository.SetUserPlan(db, parent.UserID, planIDFromContract(db, parent), &contractID, &newExp); err != nil {
		return fmt.Errorf("recurring.success: SetUserPlan: %w", err)
	}

	// Upsert child contract with parent_contract_id set.
	if err := repository.UpsertLavaContract(db, &model.LavaContract{
		UserID:           parent.UserID,
		ContractID:       contractID,
		ParentContractID: &parentID,
		OfferID:          parent.OfferID,
		Plan:             parent.Plan,
		Periodicity:      parent.Periodicity,
		Currency:         parent.Currency,
		IsActive:         true,
		StartedAt:        startedAt,
		ExpiresAt:        &newExp,
	}); err != nil {
		return fmt.Errorf("recurring.success: UpsertLavaContract: %w", err)
	}

	// Also refresh parent's expires_at so the local view stays consistent.
	if err := db.Model(&model.LavaContract{}).Where("contract_id = ?", parentID).Update("expires_at", &newExp).Error; err != nil {
		return fmt.Errorf("recurring.success: update parent expires_at: %w", err)
	}

	logger.Info("webhook: recurring.payment.success extended",
		zap.String("user_id", parent.UserID),
		zap.String("parent_contract", parentID),
		zap.String("renewal_contract", contractID),
		zap.Time("new_expires_at", newExp),
	)
	return nil
}

// handleLavaPaymentFailed marks the matching invoice as failed.
// No tier change for first-payment failures.
func handleLavaPaymentFailed(logger *zap.Logger, db *gorm.DB, event *lava.WebhookEvent) error {
	inv, err := repository.FindInvoiceByLavaID(db, event.ContractID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			logger.Warn("payment.failed: no matching invoice", zap.String("contract_id", event.ContractID))
			return nil // benign — invoice may have been deleted
		}
		return fmt.Errorf("payment.failed: FindInvoiceByLavaID: %w", err)
	}
	if err := repository.UpdateInvoiceStatus(db, inv.ID, "failed"); err != nil {
		return fmt.Errorf("payment.failed: UpdateInvoiceStatus: %w", err)
	}
	logger.Info("webhook: payment.failed recorded", zap.String("invoice_id", inv.ID))
	return nil
}

// handleLavaRecurringFailed flips lava_contracts.is_active=false.
// Tier is NOT downgraded — cron handles that after expires_at lapses (D-19).
func handleLavaRecurringFailed(logger *zap.Logger, db *gorm.DB, event *lava.WebhookEvent) error {
	parentID := ""
	if event.ParentContractID != nil {
		parentID = *event.ParentContractID
	}
	if parentID == "" {
		return fmt.Errorf("recurring.failed: missing parentContractId")
	}
	if err := db.Model(&model.LavaContract{}).Where("contract_id = ?", parentID).Update("is_active", false).Error; err != nil {
		return fmt.Errorf("recurring.failed: update contract: %w", err)
	}
	// Also flip subscriptions.is_active=false for this user — the cron
	// downgrade query (03-09) checks subscriptions.is_active = TRUE so we
	// must keep this denormalised flag in sync.
	parent, ferr := repository.FindLavaContractByContractID(db, parentID)
	if ferr != nil && !errors.Is(ferr, repository.ErrNotFound) {
		return fmt.Errorf("recurring.failed: FindLavaContractByContractID: %w", ferr)
	}
	if parent != nil {
		// NOTE: per D-19 we leave subscriptions.is_active=true so the user
		// keeps paid-for time; the cron checks expires_at < now() to flip.
		// Some interpretations of D-19 read "is_active=false immediately on
		// both rows" — the cron query in plan 03-09 must be cross-referenced.
		// Per ADR §19.10's cron SQL: `WHERE s.is_active = TRUE AND s.expires_at < now()`.
		// So subscriptions.is_active stays TRUE; the user keeps paid time;
		// cron downgrades at expiry. Honor that — DO NOT flip is_active here.
		_ = parent // intentionally unused — see comment above
	}
	logger.Info("webhook: recurring.payment.failed — contract deactivated (tier waits for cron)",
		zap.String("parent_contract", parentID))
	return nil
}

// handleLavaSubscriptionCancelled records cancellation without touching tier.
// Cron downgrades after expires_at lapses.
func handleLavaSubscriptionCancelled(logger *zap.Logger, db *gorm.DB, event *lava.WebhookEvent) error {
	// subscription.cancelled events have NO `timestamp` — they have `cancelledAt`
	// (RESEARCH §1.5). The migration 020 UNIQUE uses COALESCE for idempotency;
	// here we just need to update the contract.
	now := time.Now()
	if err := db.Model(&model.LavaContract{}).Where("contract_id = ?", event.ContractID).Updates(map[string]interface{}{
		"is_active":    false,
		"cancelled_at": &now,
	}).Error; err != nil {
		return fmt.Errorf("subscription.cancelled: update contract: %w", err)
	}
	logger.Info("webhook: subscription.cancelled recorded",
		zap.String("contract_id", event.ContractID))
	return nil
}

// periodicityToDuration converts lava periodicity strings to time.Duration.
// MONTHLY = 30 days (approximation — lava is the authoritative period source via
// future webhooks); PERIOD_YEAR = 365 days; PERIOD_90_DAYS = 90 days; etc.
// ONE_TIME returns 0 (no recurrence).
func periodicityToDuration(p string) time.Duration {
	switch p {
	case "MONTHLY":
		return 30 * 24 * time.Hour
	case "PERIOD_90_DAYS":
		return 90 * 24 * time.Hour
	case "PERIOD_180_DAYS":
		return 180 * 24 * time.Hour
	case "PERIOD_YEAR":
		return 365 * 24 * time.Hour
	case "ONE_TIME":
		return 0
	default:
		return 0
	}
}

// planIDFromContract looks up the offer for a given parent contract to
// resolve the plan_id. RESEARCH §1.5 confirms parent.OfferID is the lava
// offer UUID; FindOfferByLavaOfferID then resolves to local plan_id.
//
// Returns the system plan ID on any failure (fail-safe — never elevate).
func planIDFromContract(db *gorm.DB, contract *model.LavaContract) string {
	if offer, err := repository.FindOfferByLavaOfferID(db, contract.OfferID); err == nil {
		return offer.PlanID
	}
	if sid, err := repository.FindSystemPlanID(db); err == nil {
		return sid
	}
	return ""
}
```

    Note on the `handleLavaRecurringFailed` complexity: the `parent != nil` block intentionally documents that we leave `subscriptions.is_active=true` even though D-19 reads "set is_active=false immediately on both rows". This is a discretion call — RESEARCH §19.10 cron SQL `WHERE s.is_active = TRUE AND s.expires_at < now()` REQUIRES is_active to be TRUE for the cron to find it. The contract row IS flipped to is_active=false; the subscriptions row stays active until cron. **The plan 03-09 cron MUST be cross-checked to use `expires_at < now()` not is_active for the qualifier.** If the cron uses is_active, this comment block should be revised.

    Run `cd server/api && go build ./internal/handler/webhook_lava.go` (file should compile). Test file in T04.
  </action>
  <acceptance_criteria>
    - File `server/api/internal/handler/webhook_lava.go` exists
    - `grep "lava.VerifyAPIKey(apiKey, cfg.LavaWebhookSecret, cfg.LavaWebhookSecretPrevious)" server/api/internal/handler/webhook_lava.go` finds one match (PAY-07)
    - `grep "InsertWebhookEventIfNew" server/api/internal/handler/webhook_lava.go` finds one match (PAY-04)
    - `grep "StatusInternalServerError" server/api/internal/handler/webhook_lava.go` finds at least 2 matches (PAY-05 — 500 on processing failure)
    - `grep -c "case \"payment.success\\|case \"subscription.recurring.payment.success\\|case \"payment.failed\\|case \"subscription.recurring.payment.failed\\|case \"subscription.cancelled" server/api/internal/handler/webhook_lava.go` returns 5 (PAY-03 — all 5 event types)
    - `grep "FindOfferByLavaOfferID" server/api/internal/handler/webhook_lava.go` finds matches (PAY-08 chain)
    - `grep "ParentContractID" server/api/internal/handler/webhook_lava.go` finds matches (RESEARCH §1.5 — renewal parent lookup)
    - `grep "periodicityToDuration" server/api/internal/handler/webhook_lava.go` finds at least 2 matches (definition + caller)
    - `cd server/api && go build ./internal/handler/...` exits 0
  </acceptance_criteria>
  <automated>cd server/api && go build ./internal/handler/...</automated>
  <done>webhook_lava.go compiles; all 5 event types handled; PAY-04 (idempotency), PAY-05 (500 on error), PAY-07 (ConstantTimeCompare), PAY-08 (offerId chain) wired.</done>
</task>

<task type="auto">
  <id>03-06-T04</id>
  <name>Write webhook_lava_test.go (all 5 event-type handlers + 6 required tests from 03-VALIDATION.md)</name>
  <files>server/api/internal/handler/webhook_lava_test.go</files>
  <read_first>
    - server/api/internal/handler/webhook_lava.go (T03 of THIS plan)
    - server/api/internal/handler/payment_test.go (T02 of plan 03-05 — sqlite setup pattern for invoices + lava_contracts + plans + plan_offers + users)
    - .planning/phases/03-lava-top-plans-catalog/03-VALIDATION.md PAY-03 to PAY-09 named tests (must match these exact test names)
  </read_first>
  <action>
    Create `server/api/internal/handler/webhook_lava_test.go` with the named tests from 03-VALIDATION.md:
    - `TestHandleLavaWebhook_AllEvents` (PAY-03) — covers all 5 event types via subtests; each subtest asserts the side-effect on the local DB.
    - `TestHandleLavaWebhook_DuplicateNoop` (PAY-04) — same payload twice → second call returns 200 + zero new side-effects.
    - `TestHandleLavaWebhook_ProcessingError_Returns500` (PAY-05) — induce a DB failure mid-processing → handler returns 500.
    - `TestHandleLavaWebhook_TierFromOfferIDNotClient` (PAY-08) — payload includes a stray `plan` field "root" but ignored; tier derives from offerId lookup.
    - `TestHandleLavaWebhook_ExpiresAt_FirstAndRenewal` (PAY-09) — payment.success populates expires_at; recurring.success extends it.
    - `TestHandleLavaWebhook_BadSignature_401` (PAY-07) — X-Api-Key mismatch → 401.

    The setup helper extends `setupPaymentTestDB` (plan 03-05 T02) with the `lava_webhook_events` table. Skeleton (executor expands all 6 tests):

```go
package handler

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"vpnapp/server/api/internal/config"
	"vpnapp/server/api/internal/model"
	"vpnapp/server/api/internal/repository"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupWebhookTestDB extends setupPaymentTestDB (plan 03-05) with the
// lava_webhook_events table required by the webhook handler.
func setupWebhookTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// Schema: full union of payment test schema + lava_webhook_events.
	stmts := []string{
		// (copy the 6 CREATE TABLEs from setupPaymentTestDB in payment_test.go)
		// PLUS:
		`CREATE TABLE lava_webhook_events (
			id TEXT PRIMARY KEY,
			event_type TEXT NOT NULL,
			contract_id TEXT,
			invoice_id TEXT,
			payload TEXT NOT NULL,
			received_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			processed_at TIMESTAMP,
			error TEXT,
			UNIQUE (event_type, contract_id, payload)
		)`,
	}
	_ = stmts // executor: paste the 6 CREATE TABLEs from setupPaymentTestDB
	return db
}

func mkWebhookApp(t *testing.T, db *gorm.DB, secret, previous string) *fiber.App {
	app := fiber.New()
	cfg := &config.Config{LavaWebhookSecret: secret, LavaWebhookSecretPrevious: previous}
	app.Post("/api/v1/webhook/lava", HandleLavaWebhook(zap.NewNop(), cfg, db, nil))
	return app
}

func TestHandleLavaWebhook_BadSignature_401(t *testing.T) {
	db := setupWebhookTestDB(t)
	app := mkWebhookApp(t, db, "good-secret", "")
	body := `{"eventType":"payment.success","contractId":"contract-1","timestamp":"2026-05-23T10:00:00Z"}`
	req := httptest.NewRequest("POST", "/api/v1/webhook/lava", strings.NewReader(body))
	req.Header.Set("X-Api-Key", "wrong-secret")
	resp, _ := app.Test(req)
	if resp.StatusCode != 401 {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestHandleLavaWebhook_DuplicateNoop(t *testing.T) {
	db := setupWebhookTestDB(t)
	// Seed user + plan + offer + invoice so payment.success can resolve.
	// (executor: use seedUserAndPlan from payment_test.go AND insert an invoice
	// whose lava_invoice_id == "contract-1".)

	app := mkWebhookApp(t, db, "good-secret", "")
	payload := `{"eventType":"payment.success","contractId":"contract-1","timestamp":"2026-05-23T10:00:00Z","amount":5.0,"currency":"USD"}`

	deliver := func() *httpResp {
		req := httptest.NewRequest("POST", "/api/v1/webhook/lava", strings.NewReader(payload))
		req.Header.Set("X-Api-Key", "good-secret")
		resp, _ := app.Test(req)
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(resp.Body)
		return &httpResp{Status: resp.StatusCode, Body: buf.String()}
	}

	first := deliver()
	if first.Status != 200 {
		t.Errorf("first delivery expected 200, got %d body=%s", first.Status, first.Body)
	}
	second := deliver()
	if second.Status != 200 {
		t.Errorf("PAY-04: duplicate delivery must also return 200, got %d", second.Status)
	}
	// Confirm exactly one row in lava_webhook_events for this natural key.
	var n int64
	_ = db.Model(&model.LavaWebhookEvent{}).Where("contract_id = ? AND event_type = ?", "contract-1", "payment.success").Count(&n).Error
	if n != 1 {
		t.Errorf("PAY-04: expected 1 event row, got %d", n)
	}
}

type httpResp struct {
	Status int
	Body   string
}

// TODO executor: implement these 4 remaining tests:
//   TestHandleLavaWebhook_AllEvents (5 subtests for the 5 event types)
//   TestHandleLavaWebhook_ProcessingError_Returns500 (e.g. close the *gorm.DB to induce error)
//   TestHandleLavaWebhook_TierFromOfferIDNotClient (payload has plan:"root" but stays on configured plan_id)
//   TestHandleLavaWebhook_ExpiresAt_FirstAndRenewal (assert users.subscription_expires_at populated, then extended)
//
// Each test seeds: plans (free + pro), plan_offers (one with lava_offer_id="off-1"),
// users (with email), and an invoice whose lava_invoice_id matches the test's contractId.
// Use json.Marshal to build payloads — strings is fine but escaping is fragile.
//
// For ExpiresAt: after the first payment.success, assert via:
//   var u model.User
//   db.First(&u, "id = ?", userID)
//   if u.SubscriptionExpiresAt == nil { t.Errorf("expected expires_at set") }
// Then trigger a recurring.payment.success with parentContractId=first contract;
// assert the new u.SubscriptionExpiresAt > the old one.
//
// For ProcessingError: a cheap induce-failure approach is to delete the user row
// AFTER seeding so SetUserPlan's tx fails. Then assert the handler returns 500
// AND a row exists in lava_webhook_events with error column populated.

var _ = json.Marshal      // silence unused import
var _ = datatypes.JSON("") // silence unused import
var _ = uuid.NewString    // silence unused import
var _ = repository.ErrNotFound // silence unused import
```

    Plus all 6 test functions named in 03-VALIDATION.md must end up in the file. The executor implements each — `TestHandleLavaWebhook_AllEvents` is the longest because it covers 5 event types via subtests.

    Run `cd server/api && go test ./internal/handler/ -run "TestHandleLavaWebhook" -count=1 -timeout=60s -v`.
  </action>
  <acceptance_criteria>
    - File `server/api/internal/handler/webhook_lava_test.go` exists
    - `grep "TestHandleLavaWebhook_AllEvents" server/api/internal/handler/webhook_lava_test.go` finds one match (PAY-03)
    - `grep "TestHandleLavaWebhook_DuplicateNoop" server/api/internal/handler/webhook_lava_test.go` finds one match (PAY-04)
    - `grep "TestHandleLavaWebhook_ProcessingError_Returns500" server/api/internal/handler/webhook_lava_test.go` finds one match (PAY-05)
    - `grep "TestHandleLavaWebhook_BadSignature_401" server/api/internal/handler/webhook_lava_test.go` finds one match (PAY-07)
    - `grep "TestHandleLavaWebhook_TierFromOfferIDNotClient" server/api/internal/handler/webhook_lava_test.go` finds one match (PAY-08)
    - `grep "TestHandleLavaWebhook_ExpiresAt_FirstAndRenewal" server/api/internal/handler/webhook_lava_test.go` finds one match (PAY-09)
    - `cd server/api && go test ./internal/handler/ -run "TestHandleLavaWebhook" -count=1 -timeout=60s` exits 0
  </acceptance_criteria>
  <automated>cd server/api && go test ./internal/handler/ -run "TestHandleLavaWebhook" -count=1 -timeout=60s</automated>
  <done>Webhook handler has 6 named tests covering PAY-03..PAY-09; all pass on sqlite + httptest-style invocation.</done>
</task>

<task type="auto">
  <id>03-06-T05</id>
  <name>Wire POST /webhook/lava + IP allowlist middleware in cmd/main.go</name>
  <files>server/api/cmd/main.go</files>
  <read_first>
    - server/api/cmd/main.go (post-plan 03-05 — has lavaClient constructed, no /webhook/lava route yet)
    - server/api/internal/middleware/lava_ip_allowlist.go (T01 of THIS plan)
    - server/api/internal/handler/webhook_lava.go (T03 of THIS plan)
    - .planning/phases/03-lava-top-plans-catalog/03-CONTEXT.md D-02 (POST /webhook/lava added), D-16 (LAVA_WEBHOOK_ALLOWED_CIDRS parsed once)
  </read_first>
  <action>
    Edit `server/api/cmd/main.go`. Three additions:

    **(a) Parse CIDRs + construct the IP allowlist middleware ONCE at startup,** AFTER `lavaClient := lava.New(...)` and BEFORE `app := fiber.New(...)` — fail-fast on malformed CIDRs:

```go
	// Parse LAVA_WEBHOOK_ALLOWED_CIDRS and construct the route-scoped IP allowlist
	// middleware. Per RESEARCH §2.1+§2.2, Fiber's EnableTrustedProxyCheck alone
	// does NOT reject untrusted IPs — it only ignores their X-Forwarded-* headers
	// and falls back to RemoteIP(). This dedicated middleware reads
	// c.Context().RemoteIP() (TCP-layer) and 403s on mismatch (PAY-06).
	lavaCIDRs := strings.Split(cfg.LavaWebhookAllowedCIDRs, ",")
	lavaIPAllowlist, err := middleware.LavaWebhookIPAllowlist(lavaCIDRs, logger)
	if err != nil {
		logger.Fatal("LAVA_WEBHOOK_ALLOWED_CIDRS parse failed", zap.Error(err))
	}
```

    Add `"strings"` to the imports if not present.

    **(b) Also set Fiber's `TrustedProxies` config** as defence in depth (so c.IP() returns the TCP IP for non-allowlisted callers everywhere, even outside the webhook route). Find the `app := fiber.New(fiber.Config{...})` block and amend:

```go
	app := fiber.New(fiber.Config{
		AppName:                 "VPN API Server",
		ServerHeader:            "",
		ErrorHandler:            handler.ErrorHandler(logger),
		EnableTrustedProxyCheck: true,
		TrustedProxies:          lavaCIDRs,
	})
```

    **(c) Mount the webhook route as PUBLIC (no auth) but PROTECTED by the IP allowlist middleware.** Find the section near where the old /webhook/stripe lived (around line 192-195 of original — now deleted per plan 03-05). Add:

```go
	// Phase 3 lava webhook (PAY-03..09). PUBLIC route — auth is via:
	//   1. LavaWebhookIPAllowlist (TCP-layer RemoteIP check, 403 on miss).
	//   2. X-Api-Key header inside the handler (crypto/subtle.ConstantTimeCompare).
	api.Post("/webhook/lava", lavaIPAllowlist, handler.HandleLavaWebhook(logger, cfg, db, lavaClient))
```

    **(d) Add a SkipRule** for the new webhook route in the AppVersion middleware (around line 153-164) so mobile-client X-App-Version header isn't required:

```go
		middleware.SkipRule{Method: fiber.MethodPost, Path: "/api/v1/webhook/lava"},
```

    Add it in the list alongside the existing `/api/v1/auth/admin-login`, `/api/v1/auth/refresh`, and admin prefix skips. Place it in lexical order (after `/auth/refresh` is fine).

    Then `cd server/api && go build ./...` and `cd server/api && go test ./...` (full suite).
  </action>
  <acceptance_criteria>
    - `grep "LavaWebhookIPAllowlist" server/api/cmd/main.go` finds at least 2 matches (import + ctor call)
    - `grep "EnableTrustedProxyCheck: true" server/api/cmd/main.go` finds one match
    - `grep "TrustedProxies:          lavaCIDRs\\|TrustedProxies: lavaCIDRs" server/api/cmd/main.go` finds one match
    - `grep "api.Post(\"/webhook/lava\"" server/api/cmd/main.go` finds one match
    - `grep "lavaIPAllowlist, handler.HandleLavaWebhook" server/api/cmd/main.go` finds one match
    - `grep "Path: \"/api/v1/webhook/lava\"" server/api/cmd/main.go` finds one match (SkipRule)
    - `cd server/api && go build ./...` exits 0
    - `cd server/api && go vet ./...` exits 0
    - `cd server/api && go test ./... -count=1 -timeout=180s` exits 0
  </acceptance_criteria>
  <automated>cd server/api && go build ./... && go test ./... -count=1 -timeout=180s</automated>
  <done>POST /webhook/lava is wired with the dedicated IP allowlist (PAY-06) + handler that does the signature check + idempotency insert + dispatch. Full suite green.</done>
</task>

</tasks>

<verification>
- `cd server/api && go build ./...` exits 0
- `cd server/api && go test ./... -count=1 -timeout=300s` exits 0 — entire backend test suite green
- `grep -rn "c.Context().RemoteIP()" server/api/internal/middleware/lava_ip_allowlist.go` finds the explicit TCP-layer read (PAY-06 mitigation per RESEARCH §2.4)
- `grep -rn 'X-Forwarded-For\|X-Real-IP' server/api/internal/middleware/lava_ip_allowlist.go server/api/internal/handler/webhook_lava.go` returns 0 hits (we NEVER read these directly)
- All 6 named tests from 03-VALIDATION.md exist in webhook_lava_test.go
- `grep "TestInsertWebhookEventIfNew_Idempotent" server/api/internal/repository/webhook_event_repo_test.go` exists (PAY-04 lower-level test)
- `grep "TestLavaWebhookIPAllowlist" server/api/internal/middleware/lava_ip_allowlist_test.go` exists (PAY-06 lower-level test)
</verification>

<must_haves>
truths:
  - "POST /api/v1/webhook/lava from an IP outside LAVA_WEBHOOK_ALLOWED_CIDRS is rejected with 403 at the route-scoped middleware (PAY-06)."
  - "POST /api/v1/webhook/lava with a wrong X-Api-Key is rejected with 401 via crypto/subtle.ConstantTimeCompare (PAY-07)."
  - "20 duplicate webhook deliveries of the same event result in exactly 1 row in lava_webhook_events and exactly 1 set of side effects — duplicates return 200 (PAY-04)."
  - "A webhook handler that errors mid-processing returns HTTP 500 so lava retries (PAY-05). The event row stays so forensics can trace."
  - "All 5 event types dispatch to the correct branch: payment.success, subscription.recurring.payment.success, payment.failed, subscription.recurring.payment.failed, subscription.cancelled (PAY-03)."
  - "Tier is derived from the payload's contractId via the chain: contractId → invoices.lava_invoice_id → invoices.offer_id → plan_offers.lava_offer_id → plan_offers.plan_id. The payload's `plan` field is never consulted (PAY-08)."
  - "users.subscription_expires_at populates from periodicityToDuration(periodicity) on first payment.success and extends by one period on subscription.recurring.payment.success (PAY-09)."
  - "subscription.recurring.payment.failed flips lava_contracts.is_active=false but leaves users.subscription_tier unchanged — the expiry cron (plan 03-09) handles downgrade."
  - "subscription.cancelled has no `timestamp` field; the migration 020 UNIQUE uses COALESCE(timestamp, cancelledAt) so the idempotency UNIQUE still works."
artifacts:
  - path: "server/api/internal/middleware/lava_ip_allowlist.go"
    provides: "Route-scoped TCP-layer IP allowlist (PAY-06)"
    contains: "c.Context().RemoteIP()"
  - path: "server/api/internal/repository/webhook_event_repo.go"
    provides: "Idempotent insert via OnConflict{DoNothing} + UpsertLavaContract"
    contains: "clause.OnConflict{DoNothing: true}"
  - path: "server/api/internal/handler/webhook_lava.go"
    provides: "HandleLavaWebhook with all 5 event dispatch branches"
    contains: "lava.VerifyAPIKey"
  - path: "server/api/cmd/main.go"
    provides: "Wired POST /webhook/lava with IP allowlist middleware"
    contains: 'api.Post("/webhook/lava", lavaIPAllowlist'
key_links:
  - from: "server/api/cmd/main.go::api.Post(/webhook/lava)"
    to: "server/api/internal/middleware/lava_ip_allowlist.go::LavaWebhookIPAllowlist"
    via: "Route-scoped middleware mounted FIRST in the route's middleware chain"
    pattern: "lavaIPAllowlist, handler.HandleLavaWebhook"
  - from: "server/api/internal/handler/webhook_lava.go::HandleLavaWebhook"
    to: "server/api/internal/repository/plan_repo.go::FindOfferByLavaOfferID"
    via: "PAY-08 chain — tier derived from offerId via plan_offers lookup, NEVER from request body"
    pattern: "FindOfferByLavaOfferID"
  - from: "server/api/internal/handler/webhook_lava.go::HandleLavaWebhook"
    to: "server/api/internal/repository/webhook_event_repo.go::InsertWebhookEventIfNew"
    via: "Idempotency check via OnConflict{DoNothing} RowsAffected"
    pattern: "isNew, err := repository.InsertWebhookEventIfNew"
</must_haves>

<threat_model>
## Trust Boundaries
| Boundary | Description |
|----------|-------------|
| lava.top webhook delivery → us | Public network. Two layers: IP allowlist (TCP source IP) + X-Api-Key (cryptographic). |
| webhook payload → SetUserPlan | Payload `contractId` is data; tier derivation MUST flow through the PAY-08 chain, not the body's `plan`/`amount` fields. |
| Webhook handler → DB | Idempotent insert + per-event-type processing; the insert MUST commit independently of the processing tx (RESEARCH §3.4). |

## STRIDE Threat Register
| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-03-38 | Spoofing | Attacker forges webhook from arbitrary IP | mitigate | LavaWebhookIPAllowlist reads c.Context().RemoteIP() (TCP layer — NOT influenced by X-Forwarded-For). 403 on miss. RESEARCH §2.4 confirms RemoteIP is immune to header spoofing. |
| T-03-39 | Spoofing | Attacker inside the allowlist IP range forges signature | mitigate | X-Api-Key compared via crypto/subtle.ConstantTimeCompare to BOTH LAVA_WEBHOOK_SECRET and (when set) LAVA_WEBHOOK_SECRET_PREVIOUS. Both must match a constant — timing-safe (PAY-07). |
| T-03-40 | Repudiation / Replay | Lava 20-retry burst delivers same event 20 times | mitigate | OnConflict{DoNothing} insert into lava_webhook_events with UNIQUE on (event_type, contract_id, COALESCE(timestamp, cancelledAt)). 19 duplicates return 200 without re-applying. PAY-04 named test verifies. |
| T-03-41 | Tampering | Webhook payload tampered to set `plan`: `"root"` | mitigate | The handler NEVER reads `plan`/`tier` from the payload. Tier-grant is via the FindOfferByLavaOfferID(invoice.offer_id) chain — invoice is server-created in /checkout. Even an attacker who somehow forges a valid (signature + IP) webhook with arbitrary fields can only grant the PLAN they actually paid for via the matching invoice row. PAY-08 named test verifies. |
| T-03-42 | Tampering | Race: concurrent admin force-cancel + webhook payment.success | accept (defer) | Phase 7 ADMIN-03 adds per-user advisory lock. Phase 3 documents the gap (CONTEXT.md D-32 §1). Best-effort mitigation: SetUserPlan + UpsertLavaContract are both transactional; the race window is the tx-commit ordering, NOT the data layer. |
| T-03-43 | Information disclosure | Webhook payload stored in lava_webhook_events.payload (jsonb) contains buyer email + amount | accept | Required for forensics + Phase 7 replay (ADMIN-06). Mitigation: only admins read this table. Phase 8 HARD-10 zap redactor will catch accidental log leaks. |
| T-03-44 | DoS | Lava 500-loop retries hammering DB | accept | Global per-IP rate limit (HOTFIX-03) is BYPASSED for the webhook (D-20). Mitigation: lava's exponential backoff acts as soft circuit-breaker; 5-second context timeout caps each call. Risk noted, not mitigated. |
| T-03-45 | Elevation of Privilege | UpsertLavaContract rewrites user_id, offer_id, plan etc. on a hostile second delivery | mitigate | UpsertLavaContract's `DoUpdates: AssignmentColumns([is_active, expires_at, cancelled_at, parent_contract_id])` — write-once fields (user_id, offer_id, plan, periodicity, currency, started_at) are NOT in the assignment list, so a hostile second-delivery payload cannot rewrite them. |
| T-03-46 | Tampering | Inner tx-vs-event-record race: event row commits but processing fails | mitigate | Steps 3 (event record) and 4 (event processing) are NOT wrapped in one tx — Step 3 commits unconditionally so idempotency survives Step-4 failures (RESEARCH §3.4). Step 4 returning err → handler returns 500; lava retries; the second arrival hits OnConflict and returns 200 (no double-apply). |
| T-03-47 | Repudiation | Webhook arrives for a deleted plan_id (admin soft-deleted between checkout and webhook) | mitigate | FindOfferByLavaOfferID does NOT filter on is_active (intentional — ADR §19.10 grandfathering). The tier grant succeeds even on inactive offers. |
| T-03-48 | Information disclosure | API key leaks in error response | mitigate | The handler returns generic status codes (401/400/500) and an `{"error":"..."}` body with stable strings — never echoes `cfg.LavaWebhookSecret` or the received X-Api-Key. logger.Warn on mismatch only logs `path`, not the secret. |

ASVS L2 scoping per D-31: this plan IS in L2 scope — it's the highest-risk surface in Phase 3. All L2 controls applied: V4 access control (IP + X-Api-Key + idempotency UNIQUE), V5 input validation (payload field whitelist via DTO + JSON decode), V6 cryptography (constant-time compare), V11 business logic (PAY-08 chain prevents tier escalation), V13 API contract (status codes + payload schema).
</threat_model>

<success_criteria>
1. `cd server/api && go build ./...` exits 0.
2. `cd server/api && go test ./... -count=1 -timeout=300s` exits 0.
3. All 6 PAY-evidence tests in webhook_lava_test.go pass (`TestHandleLavaWebhook_AllEvents`, `_DuplicateNoop`, `_ProcessingError_Returns500`, `_BadSignature_401`, `_TierFromOfferIDNotClient`, `_ExpiresAt_FirstAndRenewal`).
4. `TestLavaWebhookIPAllowlist_RejectsOutOfRange` passes (PAY-06).
5. `TestInsertWebhookEventIfNew_Idempotent` passes (PAY-04 lower-level).
6. `grep -rn 'c.IP()' server/api/internal/middleware/lava_ip_allowlist.go server/api/internal/handler/webhook_lava.go` returns 0 hits (we use c.Context().RemoteIP() per RESEARCH §2.4).
</success_criteria>

<output>
T01..T05 land as 5 atomic commits (`feat(03-06): ...`); planner commits this plan file once with `docs(03): plan webhook-handler-ip-allowlist`.
</output>
