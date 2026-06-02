package middleware_test

import (
	"net/http/httptest"
	"testing"

	"vpnapp/server/api/internal/middleware"

	"github.com/alicebob/miniredis/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// linkLimitApp mounts LinkAttemptLimit and a 200 handler.
func linkLimitApp(redisClient *redis.Client) *fiber.App {
	app := fiber.New()
	app.Use(middleware.LinkAttemptLimit(redisClient, zap.NewNop()))
	app.Post("/auth/link", func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})
	return app
}

// TestLinkAttemptLimit_RedisDown_FailsClosed pins HARD-12 (D-20): when Redis is
// unavailable, the link-code limiter must FAIL CLOSED — return 503, not let the
// request through. LinkAttemptLimit is the brute-force defence on /auth/link; a
// fail-OPEN limiter (its current behavior) lets an attacker knock Redis over and
// then guess 6-digit link codes at unlimited rate (S7-1).
//
// RED now: LinkAttemptLimit logs Warn and `return c.Next()` on a Redis error
// (ratelimit.go:103-108) → the request reaches the 200 handler. This test
// stands up a miniredis, closes it to simulate the outage, and asserts 503.
// Flips GREEN when HARD-12 flips this single limiter to fail-closed.
func TestLinkAttemptLimit_RedisDown_FailsClosed(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	// Simulate the Redis outage: the limiter's IncrRateLimit will now error.
	mr.Close()

	app := linkLimitApp(client)

	req := httptest.NewRequest("POST", "/auth/link", nil)
	req.RemoteAddr = "10.0.0.7:5555"
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != fiber.StatusServiceUnavailable {
		t.Errorf("HARD-12: LinkAttemptLimit returned %d when Redis is down, want 503 (fail-closed)", resp.StatusCode)
	}
}
