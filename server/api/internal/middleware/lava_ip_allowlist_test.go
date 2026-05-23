package middleware

import (
	"net"
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
// in Fiber v2 cannot reliably spoof the TCP source IP — req.RemoteAddr is
// not propagated to the fasthttp adapter's connection metadata).
func TestLavaWebhookIPAllowlist_RejectsOutOfRange(t *testing.T) {
	allow, err := LavaWebhookIPAllowlist([]string{"158.160.60.174/32", "10.0.0.0/8"}, zap.NewNop())
	if err != nil {
		t.Fatalf("ctor: %v", err)
	}

	tests := []struct {
		name     string
		remoteIP string
		wantStat int
	}{
		{"exact allowlist match", "158.160.60.174", fiber.StatusOK},
		{"inside CIDR /8", "10.5.5.5", fiber.StatusOK},
		{"outside allowlist", "8.8.8.8", fiber.StatusForbidden},
		{"localhost rejected", "127.0.0.1", fiber.StatusForbidden},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			app := fiber.New()
			// Mount the middleware in a small handler chain. A trailing
			// handler returns 200 so we can distinguish allow (200) from
			// reject (403).
			app.Post("/webhook/lava", allow, func(c *fiber.Ctx) error {
				return c.SendStatus(fiber.StatusOK)
			})
			// Build the fasthttp.RequestCtx directly and pin the
			// remote address — this is the only way to spoof the
			// TCP RemoteIP in Fiber v2 (httptest req.RemoteAddr is
			// dropped by the in-process Test adapter).
			fctx := &fasthttp.RequestCtx{}
			fctx.Request.Header.SetMethod(fiber.MethodPost)
			fctx.Request.SetRequestURI("/webhook/lava")
			fctx.SetRemoteAddr(&net.TCPAddr{IP: net.ParseIP(tc.remoteIP), Port: 12345})
			app.Handler()(fctx)
			if got := fctx.Response.StatusCode(); got != tc.wantStat {
				t.Errorf("remote=%s: expected status %d, got %d", tc.remoteIP, tc.wantStat, got)
			}
		})
	}
}

// TestLavaWebhookIPAllowlist_ParsesBareIPAsSlash32 covers the bare-IP normalisation.
func TestLavaWebhookIPAllowlist_ParsesBareIPAsSlash32(t *testing.T) {
	if _, err := LavaWebhookIPAllowlist([]string{"158.160.60.174"}, zap.NewNop()); err != nil {
		t.Errorf("bare IPv4 must be parseable, got %v", err)
	}
	if _, err := LavaWebhookIPAllowlist([]string{"2001:db8::1"}, zap.NewNop()); err != nil {
		t.Errorf("bare IPv6 must be parseable, got %v", err)
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
	if _, err := LavaWebhookIPAllowlist([]string{"", "   "}, zap.NewNop()); err == nil {
		t.Errorf("expected error when all entries trim to empty")
	}
}
