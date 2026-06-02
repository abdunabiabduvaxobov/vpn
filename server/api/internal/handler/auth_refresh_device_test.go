package handler

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// TestRefreshToken_DeviceBinding pins HARD-04 (SC#4): refresh tokens are bound
// to the device that issued them.
//
// Contract:
//   - A refresh session issued for device_id="A", refreshed with device_id="B"
//     -> HTTP 401 (a stolen refresh token cannot be replayed from another
//     device).
//   - The same session refreshed with device_id="A" -> HTTP 200.
//   - An IP mismatch alone does NOT 401 (mobile clients roam networks); only the
//     device binding is hard-enforced.
//
// GREEN since HARD-04: migration 025 adds device_id/issue_ip, storeRefreshSession
// threads them through, and RefreshToken hard-checks device_id (401 on mismatch)
// while soft-checking issue_ip (log-only). Exercised on the in-memory SQLite DB
// (newAuthTestDB) whose sessions table now carries the two binding columns.

// seedBoundSession inserts a user and a session row bound to the given device_id
// and issue_ip whose refresh_token_hash matches the SHA-256 of the returned
// plaintext refresh token. The plaintext need not be a real token — the refresh
// handler only hashes the body field and looks up the row by hash.
func seedBoundSession(t *testing.T, db *gorm.DB, deviceID, issueIP string) (userID, refreshToken string) {
	t.Helper()
	if err := db.Exec(
		`INSERT INTO users (full_name, role, subscription_tier) VALUES ('Device Bind Test', 'user', 'free')`,
	).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := db.Raw(`SELECT id FROM users WHERE full_name = 'Device Bind Test' LIMIT 1`).Row().Scan(&userID); err != nil {
		t.Fatalf("read seeded user id: %v", err)
	}

	refreshToken = "refresh-token-plaintext-for-device-bind-" + deviceID
	tokenHash := fmt.Sprintf("%x", sha256.Sum256([]byte(refreshToken)))

	if err := db.Exec(
		`INSERT INTO sessions (user_id, refresh_token_hash, device_id, issue_ip, expires_at)
		 VALUES (?, ?, ?, ?, datetime('now', '+30 days'))`,
		userID, tokenHash, deviceID, issueIP,
	).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
	return userID, refreshToken
}

func TestRefreshToken_DeviceBinding(t *testing.T) {
	// --- mismatched device_id -> 401 -----------------------------------------
	t.Run("mismatched_device_rejected_401", func(t *testing.T) {
		db := newAuthTestDB(t)
		_, refreshToken := seedBoundSession(t, db, "device-A", "203.0.113.7")
		app := authTestApp(RefreshToken(zap.NewNop(), testAuthConfig(), db))

		resp := doAuthRequest(t, app, map[string]string{
			"refresh_token": refreshToken,
			"device_id":     "device-B", // stolen-token replay from another device
		})
		if resp.StatusCode != fiber.StatusUnauthorized {
			t.Fatalf("HARD-04: device mismatch must 401, got %d", resp.StatusCode)
		}
	})

	// --- matching device_id -> 200 + rotated token ---------------------------
	t.Run("matching_device_allowed_200", func(t *testing.T) {
		db := newAuthTestDB(t)
		userID, refreshToken := seedBoundSession(t, db, "device-A", "203.0.113.7")
		app := authTestApp(RefreshToken(zap.NewNop(), testAuthConfig(), db))

		resp := doAuthRequest(t, app, map[string]string{
			"refresh_token": refreshToken,
			"device_id":     "device-A",
		})
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("HARD-04: matching device must 200, got %d", resp.StatusCode)
		}

		var body map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		data, _ := body["data"].(map[string]interface{})
		newRefresh, _ := data["refresh_token"].(string)
		if newRefresh == "" {
			t.Fatal("expected a rotated refresh_token in the response")
		}

		// The rotated session row must carry the device_id forward so the next
		// refresh from device-A still passes the hard check.
		var carriedDevice string
		if err := db.Raw(
			`SELECT device_id FROM sessions WHERE user_id = ? LIMIT 1`, userID,
		).Row().Scan(&carriedDevice); err != nil {
			t.Fatalf("read rotated session device_id: %v", err)
		}
		if carriedDevice != "device-A" {
			t.Errorf("rotated session device_id: want device-A, got %q", carriedDevice)
		}
	})

	// --- IP mismatch alone -> still 200 (soft check) -------------------------
	t.Run("ip_mismatch_still_allowed_200", func(t *testing.T) {
		db := newAuthTestDB(t)
		// issue_ip is a value that will NEVER equal the httptest client IP, so the
		// soft IP check fires. The device_id matches, so the hard check passes and
		// the refresh must still succeed (mobile roaming tolerance, D-10).
		_, refreshToken := seedBoundSession(t, db, "device-A", "198.51.100.99")
		app := authTestApp(RefreshToken(zap.NewNop(), testAuthConfig(), db))

		resp := doAuthRequest(t, app, map[string]string{
			"refresh_token": refreshToken,
			"device_id":     "device-A",
		})
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("HARD-04: IP mismatch must NOT 401 (soft check), got %d", resp.StatusCode)
		}
	})

	// --- empty bound device tolerates any presented device (admin-login path) -
	t.Run("empty_bound_device_skips_hard_check", func(t *testing.T) {
		db := newAuthTestDB(t)
		// A session with no device_id (e.g. admin login, which is not device-bound)
		// must not 401 just because a device_id is presented at refresh.
		_, refreshToken := seedBoundSession(t, db, "", "203.0.113.7")
		app := authTestApp(RefreshToken(zap.NewNop(), testAuthConfig(), db))

		resp := doAuthRequest(t, app, map[string]string{
			"refresh_token": refreshToken,
			"device_id":     "any-device",
		})
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("HARD-04: empty bound device must skip hard check, got %d", resp.StatusCode)
		}
	})
}
