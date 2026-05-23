package handler

// devices_test.go — covers the GuestLogin device fast-path, LinkDevice
// (happy/expired/cap/secret), CreateShareCode (active/cap), and
// DeleteMyDevice (ownership) endpoints.
//
// These tests reuse the in-memory SQLite fixture defined in auth_test.go
// (newAuthTestDB / authTestApp / doAuthRequest) so the DDL for `users`,
// `devices`, and `link_codes` lives in one place.

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"vpnapp/server/api/internal/config"

	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// devicesTestConfig builds a config with sensible defaults for the device
// tests, including a 5-minute link code TTL.
func devicesTestConfig() *config.Config {
	return &config.Config{
		JWTSecret:   "test-secret-32-bytes-at-minimum!!",
		LinkCodeTTL: 5 * time.Minute,
	}
}

// authedApp wraps a single handler in a Fiber app and injects user_id locals
// — used for tests of endpoints that normally sit behind AuthRequired.
func authedApp(h fiber.Handler, userID string) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user_id", userID)
		return c.Next()
	})
	app.Post("/", h)
	app.Delete("/:id", h)
	return app
}

// seedPlansForDevicesTests seeds free + premium + pro + ultimate rows in the
// plans table (idempotent). Returns the plan_id matching `tier`. Plan 03-04
// rewires CreateShareCode + LinkDevice to FindPlanByID(user.PlanID) instead
// of the deleted PlanLimits map, so device-cap tests must populate the plans
// table before exercising those handlers.
//
// Limits mirror the previous PlanLimits constants so existing test
// assertions ("premium has 3 device cap") keep working as-is.
func seedPlansForDevicesTests(t *testing.T, db *gorm.DB, tier string) string {
	t.Helper()
	if tier == "" {
		tier = "free"
	}
	specs := []struct {
		code, name                                string
		maxDevices, maxServers, speedLimitMbps int
		isSystem                                  bool
	}{
		{"free", "Free", 1, 3, 50, true},
		{"premium", "Premium", 3, -1, 0, false},
		{"ultimate", "Ultimate", 6, -1, 0, false},
		{"pro", "Pro", 3, -1, 0, false},
	}
	for _, s := range specs {
		id := "plan-" + s.code
		isSys := 0
		if s.isSystem {
			isSys = 1
		}
		if err := db.Exec(
			`INSERT OR IGNORE INTO plans
			 (id, code, name, description, max_devices, max_servers, speed_limit_mbps, is_active, is_system, sort_order, created_at, updated_at)
			 VALUES (?, ?, ?, '', ?, ?, ?, 1, ?, 0, datetime('now'), datetime('now'))`,
			id, s.code, s.name, s.maxDevices, s.maxServers, s.speedLimitMbps, isSys,
		).Error; err != nil {
			t.Fatalf("seedPlansForDevicesTests(%s): %v", s.code, err)
		}
	}
	return "plan-" + tier
}

// seedGuestUserWithTier inserts a free/premium/ultimate user row and
// returns its UUID. Used to set up an "owner" before a sharing test.
//
// PAY-11: also seeds the plans table and writes the matching plan_id onto
// the new row so handlers' FindPlanByID(user.PlanID) resolves.
func seedGuestUserWithTier(t *testing.T, db *gorm.DB, tier string) string {
	t.Helper()
	planID := seedPlansForDevicesTests(t, db, tier)
	id := fmt.Sprintf("user-%d-%s", time.Now().UnixNano(), tier)
	if err := db.Exec(
		`INSERT INTO users (id, full_name, subscription_tier, role, plan_id)
		 VALUES (?, ?, ?, 'user', ?)`,
		id, "guest", tier, planID,
	).Error; err != nil {
		t.Fatalf("seedGuestUserWithTier: %v", err)
	}
	return id
}

// seedDevice binds an existing user to a device row with an optional secret.
func seedDevice(t *testing.T, db *gorm.DB, userID, deviceID, secretHash string) {
	t.Helper()
	now := time.Now().UTC()
	if err := db.Exec(
		`INSERT INTO devices (id, user_id, device_id, device_secret_hash, platform, model, first_seen_at, last_seen_at)
		 VALUES (?, ?, ?, ?, 'android', 'TestModel', ?, ?)`,
		fmt.Sprintf("dev-%d", time.Now().UnixNano()),
		userID, deviceID, secretHash, now, now,
	).Error; err != nil {
		t.Fatalf("seedDevice: %v", err)
	}
}

// seedLinkCode inserts a non-expired share code for the given owner.
func seedLinkCode(t *testing.T, db *gorm.DB, code, ownerID string) {
	t.Helper()
	if err := db.Exec(
		`INSERT INTO link_codes (code, user_id, created_at, expires_at)
		 VALUES (?, ?, ?, ?)`,
		code, ownerID, time.Now(), time.Now().Add(5*time.Minute),
	).Error; err != nil {
		t.Fatalf("seedLinkCode: %v", err)
	}
}

func hashSecret(s string) string {
	sum := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", sum)
}

// ---- GuestLogin device fast path ----

func TestGuestLogin_KnownDeviceWithMatchingSecret_ReturnsExistingUser(t *testing.T) {
	db := newAuthTestDB(t)
	owner := seedGuestUserWithTier(t, db, "premium")
	seedDevice(t, db, owner, "dev-A", hashSecret("secret-A"))

	app := authTestApp(GuestLogin(zap.NewNop(), db, devicesTestConfig()))
	resp := doAuthRequest(t, app, map[string]string{
		"device_id":     "dev-A",
		"device_secret": "secret-A",
	})
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 (existing device), got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	tok := body["data"].(map[string]interface{})["access_token"].(string)
	if tok == "" {
		t.Error("expected access_token for known device")
	}

	// Verify no NEW user row was created — there should still be exactly 1 user.
	var count int64
	db.Raw("SELECT COUNT(*) FROM users").Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 user (the owner), got %d", count)
	}
}

func TestGuestLogin_KnownDeviceWithWrongSecret_MintsFreshUser(t *testing.T) {
	db := newAuthTestDB(t)
	owner := seedGuestUserWithTier(t, db, "premium")
	seedDevice(t, db, owner, "dev-B", hashSecret("real-secret"))

	app := authTestApp(GuestLogin(zap.NewNop(), db, devicesTestConfig()))
	resp := doAuthRequest(t, app, map[string]string{
		"device_id":     "dev-B",
		"device_secret": "wrong-secret",
	})
	if resp.StatusCode != fiber.StatusCreated {
		t.Fatalf("expected 201 (fresh user mint), got %d", resp.StatusCode)
	}

	// User count should now be 2 — the owner plus the new fresh guest.
	var count int64
	db.Raw("SELECT COUNT(*) FROM users").Scan(&count)
	if count != 2 {
		t.Errorf("expected 2 users after fresh mint, got %d", count)
	}

	// The original device row must still belong to the original owner.
	var ownerID string
	db.Raw("SELECT user_id FROM devices WHERE device_id = 'dev-B'").Scan(&ownerID)
	if ownerID != owner {
		t.Errorf("expected device dev-B still owned by %s, got %s", owner, ownerID)
	}
}

func TestGuestLogin_LegacyDevicePopulatesSecretOnFirstCall(t *testing.T) {
	db := newAuthTestDB(t)
	owner := seedGuestUserWithTier(t, db, "free")
	// Legacy row: no secret hash on file.
	seedDevice(t, db, owner, "dev-legacy", "")

	app := authTestApp(GuestLogin(zap.NewNop(), db, devicesTestConfig()))
	resp := doAuthRequest(t, app, map[string]string{
		"device_id":     "dev-legacy",
		"device_secret": "freshly-generated",
	})
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// The hash should now be populated.
	var stored string
	db.Raw("SELECT device_secret_hash FROM devices WHERE device_id = 'dev-legacy'").Scan(&stored)
	if stored != hashSecret("freshly-generated") {
		t.Errorf("expected legacy row to have secret stored, got %q", stored)
	}
}

// ---- LinkDevice happy path + cap + expired ----

func TestLinkDevice_HappyPath_AttachesFriendToOwner(t *testing.T) {
	db := newAuthTestDB(t)
	owner := seedGuestUserWithTier(t, db, "premium")
	seedDevice(t, db, owner, "owner-dev", hashSecret("owner-secret"))
	seedLinkCode(t, db, "111111", owner)

	app := authTestApp(LinkDevice(zap.NewNop(), devicesTestConfig(), db))
	resp := doAuthRequest(t, app, map[string]string{
		"code":          "111111",
		"device_id":     "friend-dev",
		"device_secret": "friend-secret",
	})
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Friend's device should now be bound to the owner.
	var boundUserID string
	db.Raw("SELECT user_id FROM devices WHERE device_id = 'friend-dev'").Scan(&boundUserID)
	if boundUserID != owner {
		t.Errorf("expected friend device bound to %s, got %s", owner, boundUserID)
	}

	// Code should be consumed.
	var codeCount int64
	db.Raw("SELECT COUNT(*) FROM link_codes WHERE code = '111111'").Scan(&codeCount)
	if codeCount != 0 {
		t.Error("expected code to be consumed (deleted)")
	}
}

func TestLinkDevice_ExpiredCode_Returns404(t *testing.T) {
	db := newAuthTestDB(t)
	owner := seedGuestUserWithTier(t, db, "premium")
	// Insert an already-expired code by hand.
	if err := db.Exec(
		`INSERT INTO link_codes (code, user_id, created_at, expires_at)
		 VALUES (?, ?, ?, ?)`,
		"222222", owner, time.Now().Add(-10*time.Minute), time.Now().Add(-5*time.Minute),
	).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	app := authTestApp(LinkDevice(zap.NewNop(), devicesTestConfig(), db))
	resp := doAuthRequest(t, app, map[string]string{
		"code":          "222222",
		"device_id":     "friend-dev-2",
		"device_secret": "x",
	})
	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("expected 404 for expired code, got %d", resp.StatusCode)
	}
}

// Regression test for the round-2 hardening pass: a redeemer who knows a
// victim's device_id but NOT their secret must not be able to rebind the
// row to a new owner.
func TestLinkDevice_ExistingDeviceWithMismatchedSecret_Returns403(t *testing.T) {
	db := newAuthTestDB(t)
	victim := seedGuestUserWithTier(t, db, "free")
	seedDevice(t, db, victim, "victim-dev", hashSecret("victim-secret"))

	// Attacker holds a code from their own premium plan and tries to
	// claim the victim's device row.
	attacker := seedGuestUserWithTier(t, db, "premium")
	seedLinkCode(t, db, "555555", attacker)

	app := authTestApp(LinkDevice(zap.NewNop(), devicesTestConfig(), db))
	resp := doAuthRequest(t, app, map[string]string{
		"code":          "555555",
		"device_id":     "victim-dev",
		"device_secret": "attacker-guess",
	})
	if resp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("expected 403 (device claimed), got %d", resp.StatusCode)
	}

	// Verify the device row is STILL bound to the victim — not the attacker.
	var boundUserID string
	db.Raw("SELECT user_id FROM devices WHERE device_id = 'victim-dev'").Scan(&boundUserID)
	if boundUserID != victim {
		t.Errorf("expected victim-dev still bound to %s, got %s", victim, boundUserID)
	}

	// And the code must have been consumed since the transaction commits
	// the consume before the secret check happens — wait actually no,
	// the consume runs first inside the same transaction, so on rollback
	// the code SHOULD survive. Verify that.
	var codeCount int64
	db.Raw("SELECT COUNT(*) FROM link_codes WHERE code = '555555'").Scan(&codeCount)
	if codeCount != 1 {
		t.Errorf("expected code to survive transaction rollback, got count %d", codeCount)
	}
}

func TestLinkDevice_OwnerAtCap_Returns403(t *testing.T) {
	db := newAuthTestDB(t)
	// Premium = 3 devices. Seed 3 already-bound devices, then try to link a 4th.
	owner := seedGuestUserWithTier(t, db, "premium")
	for i := 0; i < 3; i++ {
		seedDevice(t, db, owner, fmt.Sprintf("owner-dev-%d", i), hashSecret("s"))
	}
	seedLinkCode(t, db, "333333", owner)

	app := authTestApp(LinkDevice(zap.NewNop(), devicesTestConfig(), db))
	resp := doAuthRequest(t, app, map[string]string{
		"code":          "333333",
		"device_id":     "friend-overflow",
		"device_secret": "x",
	})
	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("expected 403 for owner at cap, got %d", resp.StatusCode)
	}

	// The code must still have been consumed even when the cap rejected the
	// link — otherwise an attacker could probe the cap without burning codes.
	// Actually no: in the current implementation we want the code to be
	// burned only on success. Let's check the implementation choice and
	// verify it: the transaction rolls back on errDeviceLimitReached, so
	// the code SHOULD survive. Verify that.
	var codeCount int64
	db.Raw("SELECT COUNT(*) FROM link_codes WHERE code = '333333'").Scan(&codeCount)
	if codeCount != 1 {
		t.Errorf("expected code to remain after cap rejection (transaction rollback), got %d", codeCount)
	}
}

// ---- CreateShareCode ----

func TestCreateShareCode_AlreadyHasActiveCode_Returns429(t *testing.T) {
	db := newAuthTestDB(t)
	owner := seedGuestUserWithTier(t, db, "premium")
	seedDevice(t, db, owner, "owner-dev-active", hashSecret("s"))
	seedLinkCode(t, db, "444444", owner)

	app := authedApp(CreateShareCode(zap.NewNop(), devicesTestConfig(), db), owner)
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusTooManyRequests {
		t.Errorf("expected 429 when active code exists, got %d", resp.StatusCode)
	}
}

func TestCreateShareCode_OwnerAtCap_Returns403(t *testing.T) {
	db := newAuthTestDB(t)
	// Free = 1 device. Seed the device, then try to share — there is no
	// slot to fill so the endpoint refuses.
	owner := seedGuestUserWithTier(t, db, "free")
	seedDevice(t, db, owner, "only-dev", hashSecret("s"))

	app := authedApp(CreateShareCode(zap.NewNop(), devicesTestConfig(), db), owner)
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("expected 403 when owner at cap, got %d", resp.StatusCode)
	}
}

// ---- DeleteMyDevice ----

func TestDeleteMyDevice_OwnDevice_Returns204(t *testing.T) {
	db := newAuthTestDB(t)
	owner := seedGuestUserWithTier(t, db, "premium")
	seedDevice(t, db, owner, "my-dev", hashSecret("s"))
	var rowID string
	db.Raw("SELECT id FROM devices WHERE device_id = 'my-dev'").Scan(&rowID)

	app := authedApp(DeleteMyDevice(zap.NewNop(), db), owner)
	req := httptest.NewRequest(http.MethodDelete, "/"+rowID, nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusNoContent {
		t.Errorf("expected 204, got %d", resp.StatusCode)
	}

	var count int64
	db.Raw("SELECT COUNT(*) FROM devices WHERE id = ?", rowID).Scan(&count)
	if count != 0 {
		t.Errorf("expected device row to be deleted, got count %d", count)
	}
}

func TestDeleteMyDevice_OtherUserDevice_Returns404(t *testing.T) {
	db := newAuthTestDB(t)
	ownerA := seedGuestUserWithTier(t, db, "premium")
	ownerB := seedGuestUserWithTier(t, db, "premium")
	seedDevice(t, db, ownerA, "a-dev", hashSecret("s"))
	var rowID string
	db.Raw("SELECT id FROM devices WHERE device_id = 'a-dev'").Scan(&rowID)

	// Try to delete A's device while authenticated as B.
	app := authedApp(DeleteMyDevice(zap.NewNop(), db), ownerB)
	req := httptest.NewRequest(http.MethodDelete, "/"+rowID, nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("expected 404 (ownership filter), got %d", resp.StatusCode)
	}

	// Confirm A's device row was NOT deleted.
	var count int64
	db.Raw("SELECT COUNT(*) FROM devices WHERE id = ?", rowID).Scan(&count)
	if count != 1 {
		t.Errorf("A's device should be intact, got count %d", count)
	}
}
