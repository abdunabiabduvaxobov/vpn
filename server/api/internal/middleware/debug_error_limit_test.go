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

func debugErrorApp(client *redis.Client) *fiber.App {
	app := fiber.New()
	app.Post("/debug/error",
		middleware.DebugErrorLimit(client, zap.NewNop()),
		func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusNoContent) },
	)
	return app
}

// TestDebugErrorLimit_SixthCall429 verifies HARD-13 / S7-2: the 6th call within
// a minute from one IP returns 429 (limit 5/min/IP).
func TestDebugErrorLimit_SixthCall429(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	app := debugErrorApp(client)

	// First 5 calls succeed.
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("POST", "/debug/error", nil)
		req.RemoteAddr = "10.0.0.7:3333"
		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatalf("call %d error: %v", i, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != fiber.StatusNoContent {
			t.Fatalf("call %d: expected 204, got %d", i, resp.StatusCode)
		}
	}

	// 6th call is rate-limited.
	req := httptest.NewRequest("POST", "/debug/error", nil)
	req.RemoteAddr = "10.0.0.7:3333"
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("6th call error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusTooManyRequests {
		t.Fatalf("6th call: expected 429, got %d", resp.StatusCode)
	}
}

// TestDebugErrorLimit_RedisDown_FailsOpen verifies the asymmetry vs HARD-12:
// a logging endpoint must keep working when Redis is unreachable.
func TestDebugErrorLimit_RedisDown_FailsOpen(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: fmt.Sprintf("127.0.0.1:%d", 19997)})
	t.Cleanup(func() { _ = client.Close() })

	app := debugErrorApp(client)

	req := httptest.NewRequest("POST", "/debug/error", nil)
	req.RemoteAddr = "10.0.0.8:4444"
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("expected 204 when Redis down (fail open), got %d", resp.StatusCode)
	}
}
