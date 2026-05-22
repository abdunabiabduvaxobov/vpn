package middleware

import (
	"errors"

	"vpnapp/server/api/internal/repository"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// AdminRequired is middleware that enforces admin-only access.
//
// It must run after AuthRequired, which populates c.Locals("user_id").
// Unlike the previous JWT-claim version, this re-reads the role from
// the database on every admin request so admin demotion takes effect
// on the very next request (not 5 minutes later when the JWT expires).
//
// Cost: one PK lookup on the users table per admin request. Admin
// traffic is low (tens of requests/minute typical), so the absolute
// p99 cost is bounded by the single-PK-lookup latency (sub-ms warm).
// Reviewers: this is the price of correct privilege revocation. If
// it ever shows up in a profile, cache TIER+ROLE behind AuthRequired
// with a ≤1s Redis TTL — but do not push past 1s, see HOTFIX-02 spec.
// PERF-04 in Phase 6 lands the unified user+tier+role cache (TTL ≤ 5s)
// if and when admin traffic grows past the bounded regime.
//
// Closes CODE-REVIEW CRIT-03 and SECURITY-AUDIT S2-2. Implements
// ROADMAP §Phase 1 success criterion #1 ("admin demotion takes effect
// on the very next request").
func AdminRequired(db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID, _ := c.Locals("user_id").(string)
		if userID == "" {
			// AuthRequired didn't run or token was malformed.
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "unauthorized",
			})
		}

		// Use the admin-scoped lookup so this middleware and the
		// AdminChangePassword handler (handler/auth.go) read users
		// through the same code path. FindUserByIDAdmin also wraps
		// non-ErrNotFound DB errors with context, which gives operators
		// a self-describing log line at the ErrorHandler boundary.
		user, err := repository.FindUserByIDAdmin(db, userID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				// User was deleted between AuthRequired's lookup and now.
				// 401 (not 403) so the client refresh path fires correctly.
				return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
					"error": "user no longer exists",
				})
			}
			// Other DB error — propagates to Fiber's ErrorHandler from
			// HOTFIX-04 (returns scrubbed 500 + X-Request-ID).
			return err
		}

		if user.Role != "admin" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "forbidden",
			})
		}

		return c.Next()
	}
}
