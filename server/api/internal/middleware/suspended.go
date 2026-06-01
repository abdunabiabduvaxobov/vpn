package middleware

import (
	"errors"

	"vpnapp/server/api/internal/repository"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// SuspendedRequired blocks suspended users from the protected route group
// (ADMIN-02, T-07-13). It must run AFTER AuthRequired, which populates
// c.Locals("user_id").
//
// It does its own cheap FindUserByIDAdmin(user_id) PK lookup and 403s when
// users.suspended_at IS NOT NULL. This is deliberately a per-request DB read,
// mirroring AdminRequired: AuthRequired's user:<id> cache stores only the tier
// (an existence/entitlement signal), so a stale cache could otherwise let a
// just-suspended user's still-valid JWT through. Suspension is rare and the
// lookup is a single warm PK query — correctness over the micro-optimization
// (T-07-13: DeleteUserSessions kills the refresh path, BustUserCache is the
// 5s backstop, and this read is the hard gate that stale cache cannot bypass).
//
// This is mounted ONLY on the protected user group, never on the admin group:
// an admin must never be able to lock themselves out, and admins are not
// suspendable in v1 (T-07-17).
func SuspendedRequired(db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID, _ := c.Locals("user_id").(string)
		if userID == "" {
			// AuthRequired didn't run or token was malformed.
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "unauthorized",
			})
		}

		user, err := repository.FindUserByIDAdmin(c.Context(), db, userID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				// User deleted between AuthRequired's lookup and now.
				// 401 (not 403) so the client refresh path fires — matches
				// AdminRequired's deleted-user handling.
				return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
					"error": "user no longer exists",
				})
			}
			// Other DB error propagates to Fiber's ErrorHandler.
			return err
		}

		if user.SuspendedAt != nil {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "account suspended",
			})
		}

		return c.Next()
	}
}
