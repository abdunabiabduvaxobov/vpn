package handler

import (
	"errors"
	"runtime"
	"strings"
	"time"

	"vpnapp/server/api/internal/model"
	"vpnapp/server/api/internal/repository"

	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

var startTime = time.Now()

// Health handles GET /health.
func Health() fiber.Handler {
	return func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"status":     "healthy",
			"uptime":     time.Since(startTime).Round(time.Second).String(),
			"go_version": runtime.Version(),
			"timestamp":  time.Now().UTC(),
		})
	}
}

// GetSubscription handles GET /subscription.
// Returns the user's active subscription from the database.
func GetSubscription(logger *zap.Logger, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID := c.Locals("user_id").(string)

		sub, err := repository.FindSubscriptionByUserID(db, userID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				// No subscription found — return default free plan
				return c.JSON(fiber.Map{
					"data": fiber.Map{
						"plan":        "free",
						"is_active":   true,
						"max_devices": model.PlanLimits["free"].MaxDevices,
					},
				})
			}
			logger.Error("failed to get subscription", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}

		limits := model.PlanLimits[sub.Plan]

		return c.JSON(fiber.Map{
			"data": fiber.Map{
				"id":          sub.ID,
				"plan":        sub.Plan,
				"is_active":   sub.IsActive,
				"started_at":  sub.StartedAt,
				"expires_at":  sub.ExpiresAt,
				"max_devices": limits.MaxDevices,
			},
		})
	}
}

// GetAccount handles GET /account.
// Returns the user's account information from the database.
func GetAccount(logger *zap.Logger, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID := c.Locals("user_id").(string)

		user, err := repository.FindUserByID(db, userID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
					"error": "user not found",
				})
			}
			logger.Error("failed to get user", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}

		return c.JSON(fiber.Map{
			"data": fiber.Map{
				"id":                      user.ID,
				"full_name":               user.FullName,
				"subscription_tier":       user.SubscriptionTier,
				"subscription_expires_at": user.SubscriptionExpiresAt,
				"created_at":              user.CreatedAt,
			},
		})
	}
}

// patchAccountRequest defines the fields a user may update on their own account.
type patchAccountRequest struct {
	Name string `json:"name"`
}

// PatchAccount handles PATCH /account.
// Updates the authenticated user's profile (currently: full_name only).
func PatchAccount(logger *zap.Logger, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID := c.Locals("user_id").(string)

		var req patchAccountRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "invalid request body",
			})
		}

		name := strings.TrimSpace(req.Name)
		if len(name) < 2 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "name must be at least 2 characters",
			})
		}
		if len(name) > 255 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "name must not exceed 255 characters",
			})
		}

		if err := repository.UpdateUserName(db, userID, name); err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
					"error": "user not found",
				})
			}
			logger.Error("failed to update user name", zap.String("user_id", userID), zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}

		logger.Info("user updated name", zap.String("user_id", userID))

		return c.JSON(fiber.Map{
			"data": fiber.Map{
				"id":        userID,
				"full_name": name,
			},
		})
	}
}

// ErrorHandler returns a custom Fiber error handler that logs errors.
//
// HOTFIX-04 (D-05/D-06): 5xx responses return a generic body
// {"error":"internal server error","request_id":"<uuid>"} so internal error
// chains from GORM/bcrypt/etc. never reach the client (CR CRIT-04 / S9-1).
// 4xx responses keep their verbose err.Error() text because that surface is
// client-UX (e.g. "email required") and not a leak surface.
//
// Every error — 4xx or 5xx — is logged ONCE via the zap logger with the
// matching request_id so operators can correlate the scrubbed client response
// to the structured log line. request_id comes from c.Locals("requestid"),
// populated by the Fiber middleware/requestid wired in cmd/main.go BEFORE
// recover.New() so panic-recovery paths also carry the id.
func ErrorHandler(logger *zap.Logger) fiber.ErrorHandler {
	return func(c *fiber.Ctx, err error) error {
		code := fiber.StatusInternalServerError
		if e, ok := err.(*fiber.Error); ok {
			code = e.Code
		}

		requestID, _ := c.Locals("requestid").(string)

		logger.Error("request error",
			zap.Int("status", code),
			zap.String("path", c.Path()),
			zap.String("method", c.Method()),
			zap.String("request_id", requestID),
			zap.Error(err),
		)

		if code >= 500 {
			return c.Status(code).JSON(fiber.Map{
				"error":      "internal server error",
				"request_id": requestID,
			})
		}
		return c.Status(code).JSON(fiber.Map{
			"error": err.Error(),
		})
	}
}
