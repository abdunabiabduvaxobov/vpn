package handler

import (
	"errors"
	"time"

	"vpnapp/server/api/internal/model"
	"vpnapp/server/api/internal/repository"

	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type registerConnectionRequest struct {
	ServerID string `json:"server_id"`
}

type disconnectConnectionRequest struct {
	BytesUp   int64 `json:"bytes_up"`
	BytesDown int64 `json:"bytes_down"`
}

// RegisterConnection handles POST /connections.
// Enforces the plan's MaxDevices limit before creating the connection record.
// Returns 429 if the user is already at their device limit.
func RegisterConnection(logger *zap.Logger, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID := c.Locals("user_id").(string)

		// Read the tier from the database, NOT from the JWT claim. The JWT
		// embeds the tier at login time and lives for the access-token TTL,
		// so admin upgrades/downgrades would otherwise take up to that long
		// to take effect — bad for paying users and for revoking abuse.
		// One extra DB lookup per connect is cheap.
		userRecord, err := repository.FindUserByID(db, userID)
		if err != nil {
			logger.Error("RegisterConnection: failed to load user",
				zap.String("user_id", userID),
				zap.Error(err),
			)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}
		tier := userRecord.SubscriptionTier
		if tier == "" {
			tier = "free"
		}
		// Enforce subscription expiration in real time. The scheduler
		// downgrades expired subscriptions every ~1 minute, but a user
		// could start a connection in the window between expiry and the
		// next scheduler tick. Treating an expired tier as "free" here
		// closes that gap without a second DB write — the scheduler
		// will permanently downgrade the tier on its next pass.
		if tier != "free" && userRecord.SubscriptionExpiresAt != nil &&
			userRecord.SubscriptionExpiresAt.Before(time.Now()) {
			tier = "free"
		}

		var req registerConnectionRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "invalid request body",
			})
		}

		if req.ServerID == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "server_id is required",
			})
		}

		// Verify the target server exists and is active.
		server, err := repository.FindServerByID(db, req.ServerID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
					"error": "server not found",
				})
			}
			logger.Error("failed to find server for connection", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}

		if !server.IsActive {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "server unavailable",
			})
		}

		// Enforce device limit using an atomic INSERT … SELECT so that the
		// count check and the row insertion happen in a single statement,
		// eliminating the TOCTOU race between COUNT and INSERT.
		limits, ok := model.PlanLimits[tier]
		if !ok {
			// Unknown tier — fall back to the free plan limits.
			limits = model.PlanLimits["free"]
		}

		conn := model.Connection{
			UserID:   userID,
			ServerID: req.ServerID,
		}

		// When MaxDevices is UnlimitedDevices (-1) skip the limit check entirely.
		if limits.MaxDevices == model.UnlimitedDevices {
			if err := repository.CreateConnection(db, &conn); err != nil {
				logger.Error("failed to create connection",
					zap.String("user_id", userID),
					zap.Error(err),
				)
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error": "internal server error",
				})
			}
			logger.Info("connection registered (unlimited tier)",
				zap.String("connection_id", conn.ID),
				zap.String("user_id", userID),
				zap.String("server_id", req.ServerID),
			)
			return c.Status(fiber.StatusCreated).JSON(fiber.Map{
				"data": fiber.Map{
					"id":           conn.ID,
					"server_id":    conn.ServerID,
					"connected_at": conn.ConnectedAt,
				},
			})
		}

		inserted, err := repository.CreateConnectionAtomic(db, &conn, limits.MaxDevices)
		if err != nil {
			logger.Error("failed to create connection",
				zap.String("user_id", userID),
				zap.Error(err),
			)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}

		if !inserted {
			logger.Warn("device limit reached",
				zap.String("user_id", userID),
				zap.String("tier", tier),
				zap.Int("limit", limits.MaxDevices),
			)
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"error":       "device limit reached",
				"max_devices": limits.MaxDevices,
			})
		}

		logger.Info("connection registered",
			zap.String("connection_id", conn.ID),
			zap.String("user_id", userID),
			zap.String("server_id", req.ServerID),
		)

		return c.Status(fiber.StatusCreated).JSON(fiber.Map{
			"data": fiber.Map{
				"id":           conn.ID,
				"server_id":    conn.ServerID,
				"connected_at": conn.ConnectedAt,
			},
		})
	}
}

// UnregisterConnection handles DELETE /connections/:id.
// Sets disconnected_at and records the final byte counts for the session.
func UnregisterConnection(logger *zap.Logger, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		connID := c.Params("id")
		userID := c.Locals("user_id").(string)

		if connID == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "connection id is required",
			})
		}

		// Ensure the connection belongs to the calling user before modifying it.
		existing, err := repository.FindConnectionByID(db, connID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
					"error": "connection not found",
				})
			}
			logger.Error("failed to find connection",
				zap.String("connection_id", connID),
				zap.Error(err),
			)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}

		if existing.UserID != userID {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "forbidden",
			})
		}

		var req disconnectConnectionRequest
		// Body is optional — byte counts default to 0 if not provided.
		_ = c.BodyParser(&req)

		if err := repository.DisconnectConnection(db, connID, req.BytesUp, req.BytesDown); err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				// Already disconnected — treat as idempotent success.
				return c.Status(fiber.StatusNoContent).Send(nil)
			}
			logger.Error("failed to disconnect connection",
				zap.String("connection_id", connID),
				zap.Error(err),
			)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}

		logger.Info("connection unregistered",
			zap.String("connection_id", connID),
			zap.String("user_id", userID),
			zap.Int64("bytes_up", req.BytesUp),
			zap.Int64("bytes_down", req.BytesDown),
		)

		return c.Status(fiber.StatusNoContent).Send(nil)
	}
}

// HeartbeatConnection handles PATCH /connections/:id/heartbeat.
// Updates last_heartbeat_at for an active connection so the stale cleanup
// scheduler does not mark it as disconnected.
func HeartbeatConnection(logger *zap.Logger, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		connID := c.Params("id")
		userID := c.Locals("user_id").(string)

		if connID == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "connection id required"})
		}

		existing, err := repository.FindConnectionByID(db, connID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "connection not found"})
			}
			logger.Error("heartbeat lookup failed", zap.String("id", connID), zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}

		if existing.UserID != userID {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "forbidden"})
		}

		if err := repository.UpdateHeartbeat(db, connID); err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "connection already disconnected"})
			}
			logger.Error("heartbeat update failed", zap.String("id", connID), zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}

		return c.Status(fiber.StatusNoContent).Send(nil)
	}
}

// ListActiveConnections handles GET /connections.
// Returns all live (not yet disconnected) connections for the authenticated user.
func ListActiveConnections(logger *zap.Logger, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID := c.Locals("user_id").(string)

		connections, err := repository.ListActiveConnectionsByUser(db, userID)
		if err != nil {
			logger.Error("failed to list active connections",
				zap.String("user_id", userID),
				zap.Error(err),
			)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}

		logger.Debug("listing active connections",
			zap.String("user_id", userID),
			zap.Int("count", len(connections)),
		)

		return c.JSON(fiber.Map{
			"data": connections,
		})
	}
}
