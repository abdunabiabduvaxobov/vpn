package middleware_test

import (
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"vpnapp/server/api/internal/middleware"

	"github.com/alicebob/miniredis/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// rateLimitBearerStatus fires GET / with a Bearer token and a fixed source IP,
// returning the status. Each call shares the same IP so the IP bucket
// accumulates across calls when the token does NOT select a user bucket.
func rateLimitBearerStatus(t *testing.T, app *fiber.App, token string) int {
	t.Helper()
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.9.9.9:5555"
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test error: %v", err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

// TestRateLimit_ExpiredTokenFallsBackToIPBucket is the WR-05 guard: an
// expired-but-validly-signed access token must NOT keep the higher 200/min
// authenticated bucket. It must fall back to the 30/min per-IP bucket, so the
// 31st request from the same IP is 429ed. Previously WithoutClaimsValidation
// kept the dead token in the user bucket, inflating its abuse budget.
func TestRateLimit_ExpiredTokenFallsBackToIPBucket(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	app := fiber.New()
	app.Use(middleware.RateLimit(client, zap.NewNop(), testSecret))
	app.Get("/", func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })

	// Validly signed but expired one hour ago.
	expired := buildToken(t, "ghost-user", "free", "user", -time.Hour)

	// 30 requests should pass on the IP bucket (unauthenticatedRateLimit=30).
	for i := 0; i < 30; i++ {
		if got := rateLimitBearerStatus(t, app, expired); got != fiber.StatusOK {
			t.Fatalf("expired-token request %d: expected 200 (IP bucket), got %d", i, got)
		}
	}
	// The 31st must 429 — proving the expired token did NOT get the 200/min user bucket.
	if got := rateLimitBearerStatus(t, app, expired); got != fiber.StatusTooManyRequests {
		t.Fatalf("expired token did not fall back to IP bucket: expected 429 on 31st request, got %d", got)
	}
}

// TestRateLimit_ValidTokenUsesUserBucket is the positive control: a fresh,
// fully-valid token selects the 200/min authenticated bucket, so 31 requests
// from the same IP all succeed (they would 429 at 31 on the IP bucket).
func TestRateLimit_ValidTokenUsesUserBucket(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	app := fiber.New()
	app.Use(middleware.RateLimit(client, zap.NewNop(), testSecret))
	app.Get("/", func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })

	valid := buildToken(t, "live-user", "pro", "user", time.Hour)

	for i := 0; i < 31; i++ {
		if got := rateLimitBearerStatus(t, app, valid); got != fiber.StatusOK {
			t.Fatalf("valid-token request %d: expected 200 (user bucket), got %d (%s)",
				i, got, fmt.Sprintf("req %d of 31", i+1))
		}
	}
}
