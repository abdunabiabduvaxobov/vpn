// Package middleware contains Fiber middleware shared across the API.
// LavaWebhookIPAllowlist is a route-scoped guard for POST /api/v1/webhook/lava.
//
// RESEARCH §2.1 documents that Fiber v2's EnableTrustedProxyCheck does NOT
// reject untrusted IPs — it silently ignores their X-Forwarded-* headers and
// falls back to RemoteIP(). To satisfy PAY-06 ("rejected at the IP allowlist
// layer regardless of X-Forwarded-For content") we need a dedicated middleware
// that reads c.Context().RemoteIP() (the TCP-layer source IP, immune to
// proxy-header spoofing) and 403s on mismatch.
package middleware

import (
	"fmt"
	"net"
	"strings"

	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
)

// LavaWebhookIPAllowlist returns a Fiber handler that 403s any request whose
// TCP RemoteIP is outside the supplied CIDR list. Bare IPs (without /CIDR
// suffix) are normalised to /32 (IPv4) or /128 (IPv6). The CIDR slice is
// parsed ONCE at startup; the returned handler is hot-path safe (no parsing
// per request — only IPNet.Contains).
//
// Error from this function (returned at startup) is fatal — cmd/main.go
// turns it into logger.Fatal so a malformed LAVA_WEBHOOK_ALLOWED_CIDRS env
// fails the process at boot rather than at the first webhook delivery.
func LavaWebhookIPAllowlist(cidrs []string, logger *zap.Logger) (fiber.Handler, error) {
	if len(cidrs) == 0 {
		return nil, fmt.Errorf("LavaWebhookIPAllowlist: cidrs slice is empty (LAVA_WEBHOOK_ALLOWED_CIDRS must contain at least one entry)")
	}
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, s := range cidrs {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		// Bare IP → /32 (v4) or /128 (v6).
		if !strings.Contains(s, "/") {
			if strings.Contains(s, ":") {
				s += "/128"
			} else {
				s += "/32"
			}
		}
		_, ipNet, err := net.ParseCIDR(s)
		if err != nil {
			return nil, fmt.Errorf("LavaWebhookIPAllowlist: parse %q: %w", s, err)
		}
		nets = append(nets, ipNet)
	}
	if len(nets) == 0 {
		return nil, fmt.Errorf("LavaWebhookIPAllowlist: no valid CIDRs after trimming")
	}

	return func(c *fiber.Ctx) error {
		// c.Context().RemoteIP() returns the raw TCP-connection IP — NOT
		// influenced by TrustedProxies / X-Forwarded-For (RESEARCH §2.4).
		remote := c.Context().RemoteIP()
		for _, n := range nets {
			if n.Contains(remote) {
				return c.Next()
			}
		}
		logger.Warn("lava webhook: IP allowlist reject",
			zap.String("remote_ip", remote.String()),
			zap.String("path", c.Path()),
		)
		return c.SendStatus(fiber.StatusForbidden)
	}, nil
}
