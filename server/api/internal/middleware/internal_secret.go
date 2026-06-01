package middleware

import (
	"crypto/subtle"

	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
)

// InternalSecret guards the /internal route group (machine-to-machine, e.g. the
// tunnel-server heartbeat). It compares the X-Internal-Secret request header
// against the configured shared secret using crypto/subtle.ConstantTimeCompare
// so a timing oracle cannot leak the secret prefix-by-prefix (T-07-08). It is a
// shared-secret gate, NOT IP allowlisting — the tunnel may roam.
//
// Fail-closed: if the server-side secret is empty (misconfiguration) every
// request is rejected — an unauthenticated /internal endpoint must never be
// reachable. The secret is required at boot via config.RequireEnv(), so an
// empty secret here means a boot-time guard was bypassed; we still 401 rather
// than fail open.
func InternalSecret(secret string, logger *zap.Logger) fiber.Handler {
	return func(c *fiber.Ctx) error {
		received := c.Get("X-Internal-Secret")
		if secret == "" || subtle.ConstantTimeCompare([]byte(received), []byte(secret)) != 1 {
			logger.Warn("internal endpoint rejected: bad or missing X-Internal-Secret",
				zap.String("path", c.Path()),
				zap.String("remote_ip", c.Context().RemoteIP().String()),
			)
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "unauthorized",
			})
		}
		return c.Next()
	}
}
