package middleware_test

import (
	"fmt"
	"net/http/httptest"
	"testing"

	"vpnapp/server/api/internal/middleware"

	"github.com/alicebob/miniredis/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// TestLinkAttemptLimit_RedisDown_FailsClosed verifies HARD-12 / S7-1: when
// Redis is unreachable, LinkAttemptLimit returns 503 (fail closed) so an
// attacker cannot brute-force link codes during a Redis outage.
func TestLinkAttemptLimit_RedisDown_FailsClosed(t *testing.T) {
	// Client pointing at a port nobody listens on -> IncrRateLimit errors.
	client := redis.NewClient(&redis.Options{Addr: fmt.Sprintf("127.0.0.1:%d", 19998)})
	t.Cleanup(func() { _ = client.Close() })

	app := fiber.New()
	app.Use(middleware.LinkAttemptLimit(client, zap.NewNop()))
	app.Post("/auth/link", func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })

	req := httptest.NewRequest("POST", "/auth/link", nil)
	req.RemoteAddr = "10.0.0.5:1111"
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != fiber.StatusServiceUnavailable {
		t.Fatalf("expected 503 when Redis down (fail closed), got %d", resp.StatusCode)
	}
}

// TestLinkAttemptLimit_RedisUp_AllowsBelowLimit verifies the limiter still
// permits a normal request when Redis is healthy.
func TestLinkAttemptLimit_RedisUp_AllowsBelowLimit(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	app := fiber.New()
	app.Use(middleware.LinkAttemptLimit(client, zap.NewNop()))
	app.Post("/auth/link", func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })

	req := httptest.NewRequest("POST", "/auth/link", nil)
	req.RemoteAddr = "10.0.0.6:2222"
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 below limit with Redis up, got %d", resp.StatusCode)
	}
}
