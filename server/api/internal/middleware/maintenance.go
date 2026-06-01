package middleware

import (
	"strings"

	"vpnapp/server/api/internal/cache"

	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// maintenanceRetryAfter is the Retry-After header value (seconds) sent with the
// 503 so clients back off instead of hot-looping while maintenance is on.
const maintenanceRetryAfter = "60"

// Maintenance returns middleware that 503s every NON-exempt request while the
// maintenance_mode feature flag is ON (ADMIN-05/08, SC-6). It is mounted on the
// /api/v1 group right after RateLimit.
//
// The operator escape hatch (Pitfall 5 / T-07-27) — these paths ALWAYS pass
// through, so an operator who turned maintenance on can always turn it back off:
//   - any /api/v1/admin/ path           (the admin panel + the flag toggle itself)
//   - POST /api/v1/auth/admin-login      (so the operator can log in to reach it)
//   - GET  /api/v1/livez and /readyz     (probes / load balancers must keep working)
//   - any /api/v1/internal/ path         (tunnel heartbeat must keep working)
//
// Fail-open (T-07-27): if the flag read itself errors (Redis outage AND DB
// error), the request is ALLOWED through and the failure is logged — a flag we
// cannot read must never 503 the whole site. cache.GetFlag already fails open
// from Redis to the DB; this is the last-resort guard if the DB read also fails.
func Maintenance(db *gorm.DB, redis *redis.Client, logger *zap.Logger) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if isMaintenanceExempt(c.Method(), c.Path()) {
			return c.Next()
		}

		on, err := cache.GetFlag(c.Context(), redis, db, "maintenance_mode")
		if err != nil {
			// Fail open — do not lock the whole site out on a read failure.
			logger.Warn("maintenance: flag read failed, allowing request (fail-open)",
				zap.String("path", c.Path()), zap.Error(err))
			return c.Next()
		}
		if !on {
			return c.Next()
		}

		c.Set(fiber.HeaderRetryAfter, maintenanceRetryAfter)
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"error": "service temporarily unavailable for maintenance",
		})
	}
}

// isMaintenanceExempt reports whether (method, path) is on the operator escape
// hatch and must bypass the maintenance gate. Mirrors the AppVersion SkipRule
// precedent: exact-path matches for the auth-login + liveness probes, prefix
// matches for the admin + internal route trees.
func isMaintenanceExempt(method, path string) bool {
	// Normalize a trailing slash so a probe/LB configured as `/api/v1/livez/`
	// (or `/api/v1/readyz/`) still matches the exact-path escape hatch (WR-01).
	// Fiber's default StrictRouting:false treats `/x` and `/x/` as the same
	// route, but these comparisons run on the raw path, so we normalize here.
	path = strings.TrimRight(path, "/")
	if path == "" {
		path = "/"
	}
	switch {
	case path == "/api/v1/admin" || strings.HasPrefix(path, "/api/v1/admin/"):
		return true
	case path == "/api/v1/internal" || strings.HasPrefix(path, "/api/v1/internal/"):
		return true
	case method == fiber.MethodPost && path == "/api/v1/auth/admin-login":
		return true
	case method == fiber.MethodGet && path == "/api/v1/livez":
		return true
	case method == fiber.MethodGet && path == "/api/v1/readyz":
		return true
	}
	return false
}

// RequireFlagOff returns route-scoped middleware that 503s the request when the
// named feature flag is ON. Used to gate /auth/guest (signups_off) and
// /checkout (payments_off) without touching those handlers' signatures.
//
// Fail-open (T-07-27 spirit): a flag read error allows the request through and
// logs — a transient read failure must not block signups/payments. The 503
// body is flag-specific so the client shows a meaningful message.
func RequireFlagOff(db *gorm.DB, redis *redis.Client, logger *zap.Logger, key, message string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		on, err := cache.GetFlag(c.Context(), redis, db, key)
		if err != nil {
			logger.Warn("flag guard: read failed, allowing request (fail-open)",
				zap.String("flag", key), zap.String("path", c.Path()), zap.Error(err))
			return c.Next()
		}
		if on {
			c.Set(fiber.HeaderRetryAfter, maintenanceRetryAfter)
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
				"error": message,
			})
		}
		return c.Next()
	}
}
