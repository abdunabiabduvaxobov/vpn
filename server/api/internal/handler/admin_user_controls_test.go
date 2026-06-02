package handler_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"vpnapp/server/api/internal/handler"
	"vpnapp/server/api/internal/model"

	"github.com/alicebob/miniredis/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// newUserControlsDB opens an in-memory SQLite DB with the users (incl. the
// migration-024 suspension columns), connections, audit_log, sessions, and
// plans tables the ADMIN-02 user-control handlers touch.
func newUserControlsDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	ddl := `
		CREATE TABLE IF NOT EXISTS users (
			id                      TEXT PRIMARY KEY,
			email_hash              TEXT,
			password_hash           TEXT,
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
			plan_id                 TEXT NOT NULL DEFAULT '',
			suspended_at            DATETIME,
			suspended_reason        TEXT,
			created_at              DATETIME,
			updated_at              DATETIME
		);
		CREATE TABLE IF NOT EXISTS connections (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			server_id TEXT NOT NULL,
			connected_at DATETIME,
			disconnected_at DATETIME,
			bytes_up INTEGER NOT NULL DEFAULT 0,
			bytes_down INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'connected',
			last_heartbeat_at DATETIME
		);
		CREATE TABLE IF NOT EXISTS audit_log (
			id         TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
			admin_id   TEXT NOT NULL,
			action     TEXT NOT NULL,
			target_id  TEXT,
			details    TEXT,
			ip         TEXT,
			created_at DATETIME
		);
		CREATE TABLE IF NOT EXISTS sessions (
			id                  TEXT PRIMARY KEY,
			user_id             TEXT NOT NULL,
			refresh_token_hash  TEXT NOT NULL,
			device_info         TEXT,
			device_id           TEXT,
			issue_ip            TEXT,
			created_at          DATETIME,
			expires_at          DATETIME NOT NULL
		);
		CREATE TABLE IF NOT EXISTS plans (
			id                TEXT PRIMARY KEY,
			code              TEXT NOT NULL UNIQUE,
			name              TEXT NOT NULL,
			description       TEXT NOT NULL DEFAULT '',
			max_devices       INTEGER NOT NULL DEFAULT 1,
			max_servers       INTEGER NOT NULL DEFAULT 1,
			speed_limit_mbps  INTEGER NOT NULL DEFAULT 0,
			is_active         INTEGER NOT NULL DEFAULT 1,
			is_system         INTEGER NOT NULL DEFAULT 0,
			sort_order        INTEGER NOT NULL DEFAULT 0,
			created_at        DATETIME,
			updated_at        DATETIME
		);
		CREATE TABLE IF NOT EXISTS subscriptions (
			id               TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
			user_id          TEXT NOT NULL,
			plan             TEXT NOT NULL DEFAULT 'free',
			lava_contract_id TEXT,
			is_active        INTEGER NOT NULL DEFAULT 1,
			started_at       DATETIME,
			expires_at       DATETIME
		);
		CREATE TABLE IF NOT EXISTS lava_contracts (
			id                 TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
			user_id            TEXT NOT NULL,
			contract_id        TEXT NOT NULL UNIQUE,
			parent_contract_id TEXT,
			offer_id           TEXT NOT NULL,
			plan               TEXT NOT NULL,
			periodicity        TEXT NOT NULL,
			currency           TEXT NOT NULL,
			is_active          INTEGER NOT NULL DEFAULT 1,
			started_at         DATETIME,
			expires_at         DATETIME,
			cancelled_at       DATETIME,
			created_at         DATETIME
		);
	`
	if err := db.Exec(ddl).Error; err != nil {
		t.Fatalf("failed to create test tables: %v", err)
	}
	return db
}

func newUserControlsRedis(t *testing.T) *redis.Client {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	return redis.NewClient(&redis.Options{Addr: mr.Addr()})
}

// adminApp wires a single user-control handler behind a faux-auth injector that
// sets c.Locals("user_id") to a fixed admin UUID (so explicit audit writes have
// an actor), on a route that exposes :id.
func adminApp(method, path string, h fiber.Handler) (*fiber.App, string) {
	adminID := uuid.NewString()
	app := fiber.New(fiber.Config{
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		},
	})
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user_id", adminID)
		return c.Next()
	})
	app.Add(method, path, h)
	return app, adminID
}

func seedControlsUser(t *testing.T, db *gorm.DB, tier string) string {
	t.Helper()
	id := uuid.NewString()
	if err := db.Exec(
		`INSERT INTO users (id, subscription_tier, role, plan_id, created_at, updated_at) VALUES (?, ?, 'user', ?, ?, ?)`,
		id, tier, uuid.NewString(), time.Now(), time.Now(),
	).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return id
}

func postReason(t *testing.T, app *fiber.App, path, reason string) *http.Response {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"reason": reason})
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request %s: %v", path, err)
	}
	return resp
}

func TestAdminUserControls(t *testing.T) {
	t.Run("suspend sets suspended_at, revokes sessions, writes reason to audit details", func(t *testing.T) {
		db := newUserControlsDB(t)
		rdb := newUserControlsRedis(t)
		userID := seedControlsUser(t, db, "free")
		// Seed two sessions to prove revocation.
		for i := 0; i < 2; i++ {
			db.Exec(`INSERT INTO sessions (id, user_id, refresh_token_hash, expires_at, created_at) VALUES (?, ?, ?, ?, ?)`,
				uuid.NewString(), userID, "hash", time.Now().Add(time.Hour), time.Now())
		}

		app, _ := adminApp(http.MethodPost, "/admin/users/:id/suspend",
			handler.AdminSuspendUser(zap.NewNop(), db, rdb))
		resp := postReason(t, app, "/admin/users/"+userID+"/suspend", "abuse of service")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("suspend: expected 200, got %d", resp.StatusCode)
		}

		// suspended_at is set.
		var u model.User
		if err := db.First(&u, "id = ?", userID).Error; err != nil {
			t.Fatalf("reload user: %v", err)
		}
		if u.SuspendedAt == nil {
			t.Error("suspend: suspended_at should be set")
		}
		if u.SuspendedReason == nil || *u.SuspendedReason != "abuse of service" {
			t.Errorf("suspend: suspended_reason mismatch: %v", u.SuspendedReason)
		}

		// Sessions revoked.
		var sessionCount int64
		db.Model(&model.Session{}).Where("user_id = ?", userID).Count(&sessionCount)
		if sessionCount != 0 {
			t.Errorf("suspend: expected 0 sessions after revoke, got %d", sessionCount)
		}

		// Audit row carries the reason in details.
		var entries []model.AuditLogEntry
		if err := db.Where("action = ? AND target_id = ?", "suspend_user", userID).Find(&entries).Error; err != nil {
			t.Fatalf("load audit: %v", err)
		}
		if len(entries) != 1 {
			t.Fatalf("suspend: expected 1 suspend_user audit row, got %d", len(entries))
		}
		if got, _ := entries[0].Details["reason"].(string); got != "abuse of service" {
			t.Errorf("suspend: audit details reason = %q, want %q", got, "abuse of service")
		}
	})

	t.Run("empty reason is rejected with 400", func(t *testing.T) {
		db := newUserControlsDB(t)
		rdb := newUserControlsRedis(t)
		userID := seedControlsUser(t, db, "free")
		app, _ := adminApp(http.MethodPost, "/admin/users/:id/suspend",
			handler.AdminSuspendUser(zap.NewNop(), db, rdb))
		resp := postReason(t, app, "/admin/users/"+userID+"/suspend", "   ")
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("empty reason: expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("unsuspend clears suspension", func(t *testing.T) {
		db := newUserControlsDB(t)
		rdb := newUserControlsRedis(t)
		userID := seedControlsUser(t, db, "free")
		db.Exec(`UPDATE users SET suspended_at = ?, suspended_reason = ? WHERE id = ?`,
			time.Now(), "old reason", userID)

		app, _ := adminApp(http.MethodPost, "/admin/users/:id/unsuspend",
			handler.AdminUnsuspendUser(zap.NewNop(), db, rdb))
		resp := postReason(t, app, "/admin/users/"+userID+"/unsuspend", "appeal granted")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("unsuspend: expected 200, got %d", resp.StatusCode)
		}
		var u model.User
		db.First(&u, "id = ?", userID)
		if u.SuspendedAt != nil {
			t.Error("unsuspend: suspended_at should be nil")
		}
	})

	t.Run("disconnect marks live connections then 429s on second call within window", func(t *testing.T) {
		db := newUserControlsDB(t)
		rdb := newUserControlsRedis(t)
		userID := seedControlsUser(t, db, "free")
		// Two live connections + one already-disconnected (should be untouched).
		for i := 0; i < 2; i++ {
			db.Exec(`INSERT INTO connections (id, user_id, server_id, connected_at, status) VALUES (?, ?, ?, ?, 'connected')`,
				uuid.NewString(), userID, uuid.NewString(), time.Now())
		}
		db.Exec(`INSERT INTO connections (id, user_id, server_id, connected_at, disconnected_at, status) VALUES (?, ?, ?, ?, ?, 'connected')`,
			uuid.NewString(), userID, uuid.NewString(), time.Now().Add(-time.Hour), time.Now().Add(-time.Hour))

		app, _ := adminApp(http.MethodPost, "/admin/users/:id/disconnect",
			handler.AdminDisconnectUser(zap.NewNop(), db, rdb))

		// First call: 200, kills the 2 live connections.
		resp1 := postReason(t, app, "/admin/users/"+userID+"/disconnect", "force kick")
		if resp1.StatusCode != http.StatusOK {
			t.Fatalf("disconnect #1: expected 200, got %d", resp1.StatusCode)
		}
		var body struct {
			Data struct {
				KilledCount int64 `json:"killed_count"`
			} `json:"data"`
		}
		json.NewDecoder(resp1.Body).Decode(&body)
		if body.Data.KilledCount != 2 {
			t.Errorf("disconnect #1: killed_count = %d, want 2", body.Data.KilledCount)
		}
		// All connections for the user are now disconnected.
		var live int64
		db.Model(&model.Connection{}).Where("user_id = ? AND disconnected_at IS NULL", userID).Count(&live)
		if live != 0 {
			t.Errorf("disconnect #1: expected 0 live connections, got %d", live)
		}

		// Second call within 30s window: 429.
		resp2 := postReason(t, app, "/admin/users/"+userID+"/disconnect", "force kick again")
		if resp2.StatusCode != http.StatusTooManyRequests {
			t.Errorf("disconnect #2: expected 429, got %d", resp2.StatusCode)
		}

		// Audit row records the reason and killed_count.
		var entries []model.AuditLogEntry
		db.Where("action = ? AND target_id = ?", "disconnect_user", userID).Find(&entries)
		if len(entries) < 1 {
			t.Fatalf("disconnect: expected at least 1 disconnect_user audit row, got %d", len(entries))
		}
		if got, _ := entries[0].Details["reason"].(string); got != "force kick" {
			t.Errorf("disconnect: audit reason = %q, want %q", got, "force kick")
		}
	})

	t.Run("force-grant Pro via PATCH still flips tier (regression guard)", func(t *testing.T) {
		db := newUserControlsDB(t)
		rdb := newUserControlsRedis(t)
		// AdminUpdateUser validates the tier against the plans table.
		proPlanID := uuid.NewString()
		db.Exec(`INSERT INTO plans (id, code, name, max_devices, max_servers) VALUES (?, 'pro', 'Pro', 5, 5)`, proPlanID)
		userID := seedControlsUser(t, db, "free")

		app := fiber.New(fiber.Config{
			ErrorHandler: func(c *fiber.Ctx, err error) error {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
			},
		})
		app.Use(func(c *fiber.Ctx) error { c.Locals("user_id", uuid.NewString()); return c.Next() })
		app.Patch("/admin/users/:id", handler.AdminUpdateUser(zap.NewNop(), db, rdb))

		body, _ := json.Marshal(map[string]string{"subscription_tier": "pro"})
		req := httptest.NewRequest(http.MethodPatch, "/admin/users/"+userID, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("patch: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("force-grant: expected 200, got %d", resp.StatusCode)
		}
		var u model.User
		db.First(&u, "id = ?", userID)
		if u.SubscriptionTier != "pro" {
			t.Errorf("force-grant: tier = %q, want pro", u.SubscriptionTier)
		}
	})

	t.Run("audit-log and sessions history endpoints return data", func(t *testing.T) {
		db := newUserControlsDB(t)
		userID := seedControlsUser(t, db, "free")
		db.Exec(`INSERT INTO audit_log (admin_id, action, target_id, details, created_at) VALUES (?, 'suspend_user', ?, '{}', ?)`,
			uuid.NewString(), userID, time.Now())
		db.Exec(`INSERT INTO sessions (id, user_id, refresh_token_hash, device_info, expires_at, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			uuid.NewString(), userID, "h", "iPhone", time.Now().Add(time.Hour), time.Now())

		alApp, _ := adminApp(http.MethodGet, "/admin/users/:id/audit-log",
			handler.AdminGetUserAuditLog(zap.NewNop(), db))
		alResp, _ := alApp.Test(httptest.NewRequest(http.MethodGet, "/admin/users/"+userID+"/audit-log", nil))
		if alResp.StatusCode != http.StatusOK {
			t.Errorf("audit-log: expected 200, got %d", alResp.StatusCode)
		}

		sApp, _ := adminApp(http.MethodGet, "/admin/users/:id/sessions",
			handler.AdminListUserSessions(zap.NewNop(), db))
		sResp, _ := sApp.Test(httptest.NewRequest(http.MethodGet, "/admin/users/"+userID+"/sessions", nil))
		if sResp.StatusCode != http.StatusOK {
			t.Errorf("sessions: expected 200, got %d", sResp.StatusCode)
		}
	})
}

// seedSystemPlan inserts the single is_system=true (free) plan the force-cancel
// reset downgrades to, and returns its id.
func seedSystemPlan(t *testing.T, db *gorm.DB) string {
	t.Helper()
	id := uuid.NewString()
	if err := db.Exec(
		`INSERT INTO plans (id, code, name, is_active, is_system, created_at, updated_at) VALUES (?, 'free', 'Free', 1, 1, ?, ?)`,
		id, time.Now(), time.Now(),
	).Error; err != nil {
		t.Fatalf("seed system plan: %v", err)
	}
	return id
}

// seedPaidContract gives a user an active lava_contract + active subscription so
// force-cancel has something to cancel. Returns the lava contract_id.
func seedPaidContract(t *testing.T, db *gorm.DB, userID string) string {
	t.Helper()
	contractID := uuid.NewString()
	if err := db.Exec(
		`INSERT INTO lava_contracts (id, user_id, contract_id, offer_id, plan, periodicity, currency, is_active, started_at, created_at)
		 VALUES (?, ?, ?, 'offer-1', 'pro', 'MONTHLY', 'USD', 1, ?, ?)`,
		uuid.NewString(), userID, contractID, time.Now(), time.Now(),
	).Error; err != nil {
		t.Fatalf("seed lava contract: %v", err)
	}
	if err := db.Exec(
		`INSERT INTO subscriptions (id, user_id, plan, lava_contract_id, is_active, started_at) VALUES (?, ?, 'pro', ?, 1, ?)`,
		uuid.NewString(), userID, contractID, time.Now(),
	).Error; err != nil {
		t.Fatalf("seed subscription: %v", err)
	}
	return contractID
}

// postCancel POSTs the force-cancel body {refund, reason}.
func postCancel(t *testing.T, app *fiber.App, path, reason string, refund bool) *http.Response {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{"reason": reason, "refund": refund})
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request %s: %v", path, err)
	}
	return resp
}

// TestAdminCancelSubscription exercises the ADMIN-03 force-cancel handler on
// SQLite (the advisory-lock SELECT is skipped on non-postgres but the write
// logic runs). The live serialization proof is integration.TestForceCancelWebhookRace.
func TestAdminCancelSubscription(t *testing.T) {
	t.Run("force-cancel downgrades to system plan, cancels contract, records refund intent + audit", func(t *testing.T) {
		db := newUserControlsDB(t)
		rdb := newUserControlsRedis(t)
		systemPlanID := seedSystemPlan(t, db)
		userID := seedControlsUser(t, db, "pro")
		contractID := seedPaidContract(t, db, userID)

		app, _ := adminApp(http.MethodPost, "/admin/users/:id/cancel-subscription",
			handler.AdminCancelSubscription(zap.NewNop(), db, rdb))
		resp := postCancel(t, app, "/admin/users/"+userID+"/cancel-subscription", "chargeback fraud", true)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("cancel: expected 200, got %d", resp.StatusCode)
		}

		// User reset to the system (free) plan.
		var u model.User
		if err := db.First(&u, "id = ?", userID).Error; err != nil {
			t.Fatalf("reload user: %v", err)
		}
		if u.PlanID != systemPlanID {
			t.Errorf("cancel: plan_id = %q, want system %q", u.PlanID, systemPlanID)
		}
		if u.SubscriptionTier != "free" {
			t.Errorf("cancel: subscription_tier = %q, want free", u.SubscriptionTier)
		}

		// Contract marked cancelled.
		var contract model.LavaContract
		if err := db.First(&contract, "contract_id = ?", contractID).Error; err != nil {
			t.Fatalf("reload contract: %v", err)
		}
		if contract.IsActive {
			t.Error("cancel: contract should be is_active=false")
		}
		if contract.CancelledAt == nil {
			t.Error("cancel: contract cancelled_at should be set")
		}

		// Audit row carries reason + refund_intent.
		var entries []model.AuditLogEntry
		if err := db.Where("action = ? AND target_id = ?", "cancel_subscription", userID).Find(&entries).Error; err != nil {
			t.Fatalf("load audit: %v", err)
		}
		if len(entries) != 1 {
			t.Fatalf("cancel: expected 1 cancel_subscription audit row, got %d", len(entries))
		}
		if got, _ := entries[0].Details["reason"].(string); got != "chargeback fraud" {
			t.Errorf("cancel: audit reason = %q, want %q", got, "chargeback fraud")
		}
		if got, _ := entries[0].Details["refund_intent"].(bool); !got {
			t.Errorf("cancel: audit refund_intent = %v, want true", entries[0].Details["refund_intent"])
		}
	})

	t.Run("already-cancelled (no active contract) returns 409", func(t *testing.T) {
		db := newUserControlsDB(t)
		rdb := newUserControlsRedis(t)
		seedSystemPlan(t, db)
		userID := seedControlsUser(t, db, "free") // no active contract

		app, _ := adminApp(http.MethodPost, "/admin/users/:id/cancel-subscription",
			handler.AdminCancelSubscription(zap.NewNop(), db, rdb))
		resp := postCancel(t, app, "/admin/users/"+userID+"/cancel-subscription", "no contract here", false)
		if resp.StatusCode != http.StatusConflict {
			t.Fatalf("already-cancelled: expected 409, got %d", resp.StatusCode)
		}
	})

	t.Run("empty reason is rejected with 400", func(t *testing.T) {
		db := newUserControlsDB(t)
		rdb := newUserControlsRedis(t)
		seedSystemPlan(t, db)
		userID := seedControlsUser(t, db, "pro")
		seedPaidContract(t, db, userID)

		app, _ := adminApp(http.MethodPost, "/admin/users/:id/cancel-subscription",
			handler.AdminCancelSubscription(zap.NewNop(), db, rdb))
		resp := postCancel(t, app, "/admin/users/"+userID+"/cancel-subscription", "   ", false)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("empty reason: expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("refund=false records refund_status none", func(t *testing.T) {
		db := newUserControlsDB(t)
		rdb := newUserControlsRedis(t)
		seedSystemPlan(t, db)
		userID := seedControlsUser(t, db, "pro")
		seedPaidContract(t, db, userID)

		app, _ := adminApp(http.MethodPost, "/admin/users/:id/cancel-subscription",
			handler.AdminCancelSubscription(zap.NewNop(), db, rdb))
		resp := postCancel(t, app, "/admin/users/"+userID+"/cancel-subscription", "downgrade request", false)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("cancel refund=false: expected 200, got %d", resp.StatusCode)
		}
		var body struct {
			Data struct {
				RefundStatus string `json:"refund_status"`
			} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if body.Data.RefundStatus != "none" {
			t.Errorf("refund=false: refund_status = %q, want none", body.Data.RefundStatus)
		}
	})
}
