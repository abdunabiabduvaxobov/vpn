package handler

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// TestWarnIfMobileSessionUnbound is the WR-01 observability guard. A
// mobile-shaped request (X-App-Version present) that mints a session with an
// empty device_id must emit exactly one security WARN — because that lineage's
// HARD-04 refresh check is permanently skipped. Web/admin shapes (no
// X-App-Version) and properly-bound mobile requests must stay silent.
func TestWarnIfMobileSessionUnbound(t *testing.T) {
	cases := []struct {
		name       string
		appVersion string // X-App-Version header ("" = absent => web/admin shape)
		deviceID   string
		wantWarn   bool
	}{
		{
			name:       "mobile shape, empty device_id -> WARN",
			appVersion: "2.2.0",
			deviceID:   "",
			wantWarn:   true,
		},
		{
			name:       "mobile shape, bound device_id -> silent",
			appVersion: "2.2.0",
			deviceID:   "android-abc123",
			wantWarn:   false,
		},
		{
			name:       "web/admin shape (no X-App-Version), empty device_id -> silent",
			appVersion: "",
			deviceID:   "",
			wantWarn:   false,
		},
		{
			name:       "web/admin shape, bound device_id -> silent",
			appVersion: "",
			deviceID:   "browser-xyz",
			wantWarn:   false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			core, observed := observer.New(zapcore.WarnLevel)
			logger := zap.New(core)

			app := fiber.New()
			app.Get("/", func(c *fiber.Ctx) error {
				warnIfMobileSessionUnbound(c, logger, "user-123", tc.deviceID)
				return c.SendStatus(fiber.StatusOK)
			})

			req := httptest.NewRequest("GET", "/", nil)
			if tc.appVersion != "" {
				req.Header.Set("X-App-Version", tc.appVersion)
			}
			resp, err := app.Test(req, -1)
			if err != nil {
				t.Fatalf("app.Test: %v", err)
			}
			_ = resp.Body.Close()

			warns := observed.FilterMessageSnippet("without device binding").Len()
			if tc.wantWarn && warns != 1 {
				t.Fatalf("expected exactly 1 device-binding WARN, got %d", warns)
			}
			if !tc.wantWarn && warns != 0 {
				t.Fatalf("expected no device-binding WARN, got %d", warns)
			}
		})
	}
}
