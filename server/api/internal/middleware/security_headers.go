package middleware

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/helmet"
)

// AdminSecurityHeaders returns the security-header middleware mounted first on
// the admin group (HARD-08 / SECURITY-AUDIT S2-5).
//
// It composes Fiber's helmet (X-Content-Type-Options: nosniff, X-Frame-Options:
// DENY, Content-Security-Policy) with an UNCONDITIONAL Strict-Transport-Security
// header.
//
// Why a custom HSTS setter instead of relying on helmet's HSTSMaxAge:
// helmet only emits Strict-Transport-Security when c.Protocol() == "https"
// (see fiber/middleware/helmet/helmet.go). In this deployment the API binds
// plain HTTP on localhost:3000 behind a TLS-terminating reverse proxy, so
// c.Protocol() is "http" and helmet would silently drop HSTS. The must-have
// requires admin responses to carry Strict-Transport-Security regardless of
// the in-process protocol, so we set it ourselves. The proxy terminates TLS,
// so advertising HSTS to clients is correct.
//
// CSP `default-src 'none'; frame-ancestors 'none'` is correct for a JSON API:
// there is no inline script or framed content to break. The admin SPA is
// served by a separate host and is out of scope (research §4.4 / Open-Risk 6).
func AdminSecurityHeaders() []fiber.Handler {
	helmetMW := helmet.New(helmet.Config{
		// HSTSMaxAge is set so helmet ALSO emits HSTS when the request does
		// arrive over HTTPS directly; the hstsSetter below guarantees the
		// header over plain HTTP behind the proxy.
		HSTSMaxAge:            31536000,
		ContentSecurityPolicy: "default-src 'none'; frame-ancestors 'none'",
		XFrameOptions:         "DENY",
		// ContentTypeNosniff defaults to "nosniff".
	})

	hstsSetter := func(c *fiber.Ctx) error {
		c.Set(fiber.HeaderStrictTransportSecurity, "max-age=31536000; includeSubDomains")
		return c.Next()
	}

	return []fiber.Handler{helmetMW, hstsSetter}
}
