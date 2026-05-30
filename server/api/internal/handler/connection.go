package handler

import (
	"errors"
	"time"

	"vpnapp/server/api/internal/cache"
	"vpnapp/server/api/internal/model"
	"vpnapp/server/api/internal/repository"

	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
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
		//
		// PERF-04 / D-07: AuthRequired already loaded this row on a cache
		// MISS and stashed it in c.Locals("user") — reuse it and skip the
		// redundant second lookup (audit §1.2). On a cache HIT the local is
		// absent (the user cache stores only tier, not the full row), so we
		// fall back to a single FindUserByID. Either way there is now at most
		// ONE users SELECT per connect instead of two.
		var userRecord *model.User
		if u, ok := c.Locals("user").(*model.User); ok && u != nil {
			userRecord = u
		} else {
			loaded, err := repository.FindUserByID(c.Context(), db, userID)
			if err != nil {
				logger.Error("RegisterConnection: failed to load user",
					zap.String("user_id", userID),
					zap.Error(err),
				)
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error": "internal server error",
				})
			}
			userRecord = loaded
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
		server, err := repository.FindServerByID(c.Context(), db, req.ServerID)
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
		//
		// Read device limit from the plan row via plan_id (PAY-11 / D-24).
		// resolveUserPlanID handles the JWT-claim-vs-DB-fallback during the
		// plan 03-07 backward-compat window.
		planID, perr := resolveUserPlanID(c, db)
		if perr != nil {
			logger.Error("failed to resolve plan_id for RegisterConnection",
				zap.String("user_id", userID), zap.Error(perr))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}
		plan, perr := repository.FindPlanByID(c.Context(), db, planID)
		if perr != nil {
			// Fallback to system plan limits on a stale-plan_id row — fail safe.
			logger.Warn("FindPlanByID failed; falling back to system plan",
				zap.String("plan_id", planID), zap.Error(perr))
			systemPlanID, sperr := repository.FindSystemPlanID(c.Context(), db)
			if sperr != nil {
				logger.Error("FindSystemPlanID failed", zap.Error(sperr))
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error": "internal server error",
				})
			}
			plan, sperr = repository.FindPlanByID(c.Context(), db, systemPlanID)
			if sperr != nil {
				logger.Error("FindPlanByID(system) failed", zap.Error(sperr))
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error": "internal server error",
				})
			}
		}
		// Compose a "limits" struct that the existing code below can keep using.
		limits := struct {
			MaxDevices int
			MaxServers int
		}{MaxDevices: plan.MaxDevices, MaxServers: plan.MaxServers}
		_ = tier // tier retained as a log-context variable

		conn := model.Connection{
			UserID:   userID,
			ServerID: req.ServerID,
		}

		// When MaxDevices is UnlimitedDevices (-1) skip the limit check entirely.
		if limits.MaxDevices == model.UnlimitedDevices {
			if err := repository.CreateConnection(c.Context(), db, &conn); err != nil {
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

		inserted, err := repository.CreateConnectionAtomic(c.Context(), db, &conn, limits.MaxDevices)
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
		existing, err := repository.FindConnectionByID(c.Context(), db, connID)
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

		if err := repository.DisconnectConnection(c.Context(), db, connID, req.BytesUp, req.BytesDown); err != nil {
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
//
// PERF-02 / D-03 / D-04: the heartbeat path no longer writes to Postgres per
// call (it was ~167 write-q/s at 10k connections — the hottest write path).
// Instead it pipelines the beat into Redis via cache.TouchHeartbeat and a
// dedicated 10s scheduler ticker collapses every dirty id into ONE bulk UPDATE.
// Postgres stays the source of truth.
//
// The FindConnectionByID ownership pre-read is RETAINED: it is a cheap indexed
// PK read that enforces per-request ownership (the 404 / 403) so a client cannot
// refresh a connection it does not own (T-06-HB-OWNERSHIP). The PERF-02 win is
// removing the WRITE, not the read.
func HeartbeatConnection(logger *zap.Logger, db *gorm.DB, redisClient *redis.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		connID := c.Params("id")
		userID := c.Locals("user_id").(string)

		if connID == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "connection id required"})
		}

		existing, err := repository.FindConnectionByID(c.Context(), db, connID)
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

		// Pipeline the beat into Redis (SET hb:<id> EX 600 + SADD hb:dirty). The
		// 10s scheduler ticker flushes the dirty set into one bulk UPDATE.
		//
		// Fail-open (T-06-HB-FAILOPEN): a Redis error is LOGGED but we still
		// return 204 — heartbeat is non-critical, the 3-min StaleConnectionAfter
		// grace plus the next beat cover a missed write, and PG remains the
		// source of truth. A Redis outage must never surface as a 5xx here.
		if err := cache.TouchHeartbeat(c.Context(), redisClient, connID); err != nil {
			logger.Warn("heartbeat touch failed (fail-open: returning 204; 3-min grace + next beat cover it)",
				zap.String("id", connID), zap.Error(err))
		}

		return c.Status(fiber.StatusNoContent).Send(nil)
	}
}

// ListActiveConnections handles GET /connections.
// Returns all live (not yet disconnected) connections for the authenticated user.
func ListActiveConnections(logger *zap.Logger, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID := c.Locals("user_id").(string)

		connections, err := repository.ListActiveConnectionsByUser(c.Context(), db, userID)
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
