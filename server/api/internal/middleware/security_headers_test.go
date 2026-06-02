package middleware_test

import (
	"net/http/httptest"
	"testing"

	"vpnapp/server/api/internal/middleware"

	"github.com/gofiber/fiber/v2"
)

// TestAdminSecurityHeaders verifies HARD-08 / S2-5: an admin-group response
// carries Strict-Transport-Security, X-Content-Type-Options: nosniff, and a
// Content-Security-Policy header. The headers must be present even over plain
// HTTP (the API runs behind a TLS-terminating proxy), which is why HSTS is set
// unconditionally rather than via helmet's https-only path.
func TestAdminSecurityHeaders(t *testing.T) {
	app := fiber.New()
	group := app.Group("/admin", middleware.AdminSecurityHeaders()...)
	group.Get("/ping", func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })

	req := httptest.NewRequest("GET", "/admin/ping", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test error: %v", err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("Strict-Transport-Security"); got == "" {
		t.Errorf("missing Strict-Transport-Security header")
	}
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := resp.Header.Get("Content-Security-Policy"); got == "" {
		t.Errorf("missing Content-Security-Policy header")
	}
	if got := resp.Header.Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("X-Frame-Options = %q, want DENY", got)
	}
}
