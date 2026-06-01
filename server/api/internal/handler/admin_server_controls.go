package handler

import (
	"errors"
	"time"

	"vpnapp/server/api/internal/cache"
	"vpnapp/server/api/internal/repository"

	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// disconnectServerThrottleWindow is the per-server force-disconnect throttle
// window. IncrRateLimit caps the action at one success per server per window; a
// second call inside the window returns 429 (T-07-23 — blast-radius / double-click
// DoS on a busy server). Mirrors the per-user disconnectThrottleWindow but is a
// full minute because draining/force-disconnecting a whole server is a much
// heavier, rarer operation than a single user.
const disconnectServerThrottleWindow = 60 * time.Second

// drainServerRequest is the body for POST /admin/servers/:id/drain. When force
// is true the drain ALSO force-disconnects every live connection on the server
// (subject to the throttle); otherwise existing tunnels are left to drain
// naturally — is_draining only stops NEW connections (ADMIN-04 drain mode).
type drainServerRequest struct {
	Force bool `json:"force"`
}

// AdminDrainServer handles POST /admin/servers/:id/drain.
// Body: {force bool}. Sets vpn_servers.is_draining=true (UpdateServer) and
// synchronously busts cache:servers:active so a non-admin GET /servers drops the
// server within one request (T-07-24). When force=true it ALSO force-disconnects
// every live connection on the server by server_id (Option-B), subject to the
// per-server throttle. Returns 200 {data:{id, is_draining:true, killed_count:n}}.
func AdminDrainServer(logger *zap.Logger, db *gorm.DB, redisClient *redis.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		serverID := c.Params("id")
		if serverID == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "server id required"})
		}
		var req drainServerRequest
		// Body is optional — a bare POST means drain without force. A parse error
		// on a malformed body is ignored (defaults to force=false) so the common
		// no-body drain still works.
		_ = c.BodyParser(&req)

		if err := repository.UpdateServer(c.Context(), db, serverID, map[string]interface{}{"is_draining": true}); err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "server not found"})
			}
			logger.Error("admin: failed to drain server", zap.String("server_id", serverID), zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}

		// Synchronously bust the shared /servers cache so the drained server is
		// gone from the next /servers read (PERF-01 / T-07-24). Best-effort — the
		// 60s TTL plus the ListActiveServers DB filter are the backstops; never
		// fail the drain because the cache DEL failed (mirror AdminUpdateServer).
		if err := cache.BustServersCache(c.Context(), redisClient); err != nil {
			logger.Warn("admin: failed to bust servers cache after drain",
				zap.String("server_id", serverID), zap.Error(err))
		}

		var killed int64
		if req.Force {
			throttled, k, err := forceDisconnectServer(c, logger, db, redisClient, serverID)
			if err != nil {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
			}
			if throttled {
				return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
					"error": "force-disconnect throttled — try again shortly",
				})
			}
			killed = k
		}

		logger.Info("admin: drained server",
			zap.String("server_id", serverID), zap.Bool("force", req.Force), zap.Int64("killed_count", killed))

		return c.JSON(fiber.Map{"data": fiber.Map{
			"id":           serverID,
			"is_draining":  true,
			"killed_count": killed,
		}})
	}
}

// AdminUndrainServer handles POST /admin/servers/:id/undrain.
// Clears vpn_servers.is_draining and busts the servers cache so the server is
// handed out for new connections again within one request. Returns
// 200 {data:{id, is_draining:false}}.
func AdminUndrainServer(logger *zap.Logger, db *gorm.DB, redisClient *redis.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		serverID := c.Params("id")
		if serverID == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "server id required"})
		}

		if err := repository.UpdateServer(c.Context(), db, serverID, map[string]interface{}{"is_draining": false}); err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "server not found"})
			}
			logger.Error("admin: failed to undrain server", zap.String("server_id", serverID), zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}

		if err := cache.BustServersCache(c.Context(), redisClient); err != nil {
			logger.Warn("admin: failed to bust servers cache after undrain",
				zap.String("server_id", serverID), zap.Error(err))
		}

		logger.Info("admin: undrained server", zap.String("server_id", serverID))

		return c.JSON(fiber.Map{"data": fiber.Map{
			"id":          serverID,
			"is_draining": false,
		}})
	}
}

// AdminDisconnectServer handles POST /admin/servers/:id/disconnect.
// Throttled to ≤1/server/60s (429 on the second call inside the window, T-07-23).
// Marks every live connection on the server disconnected by server_id (Option-B —
// live tunnels die on the existing ~3-min stale sweep). Returns
// 200 {data:{killed_count:n}}.
func AdminDisconnectServer(logger *zap.Logger, db *gorm.DB, redisClient *redis.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		serverID := c.Params("id")
		if serverID == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "server id required"})
		}

		throttled, killed, err := forceDisconnectServer(c, logger, db, redisClient, serverID)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		if throttled {
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"error": "force-disconnect throttled — try again shortly",
			})
		}

		logger.Info("admin: force-disconnected server",
			zap.String("server_id", serverID), zap.Int64("killed_count", killed))

		return c.JSON(fiber.Map{"data": fiber.Map{"killed_count": killed}})
	}
}

// forceDisconnectServer applies the per-server throttle then marks every live
// connection on the server disconnected (Option-B, by server_id). It is shared
// by AdminDisconnectServer and the force=true branch of AdminDrainServer so the
// throttle and the blast-radius behaviour are identical on both paths.
//
// Returns (throttled, killedCount, err):
//   - throttled=true  → the caller should respond 429 (second call within window)
//   - err!=nil        → the caller should respond 500
//
// The throttle is skipped only when redisClient is nil (test convenience); in
// production a real client is always wired (T-07-23).
func forceDisconnectServer(c *fiber.Ctx, logger *zap.Logger, db *gorm.DB, redisClient *redis.Client, serverID string) (bool, int64, error) {
	if redisClient != nil {
		count, err := cache.IncrRateLimit(c.Context(), redisClient, "disconnect:server:"+serverID, disconnectServerThrottleWindow)
		if err != nil {
			logger.Error("admin: server disconnect throttle check failed",
				zap.String("server_id", serverID), zap.Error(err))
			return false, 0, err
		}
		if count > 1 {
			return true, 0, nil
		}
	}

	killed, err := repository.DisconnectConnectionsByServer(c.Context(), db, serverID)
	if err != nil {
		logger.Error("admin: failed to disconnect server connections",
			zap.String("server_id", serverID), zap.Error(err))
		return false, 0, err
	}
	return false, killed, nil
}

// AdminServerHealth handles GET /admin/servers/:id/health.
// Returns the operational snapshot for one server: concurrent_conns (live
// connection count by server_id), last_seen_at (last tunnel heartbeat), and
// current_load (the server's reported load). Returns 200 {data:{...}}; 404 when
// the server does not exist.
func AdminServerHealth(logger *zap.Logger, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		serverID := c.Params("id")
		if serverID == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "server id required"})
		}

		server, err := repository.FindServerByID(c.Context(), db, serverID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "server not found"})
			}
			logger.Error("admin: failed to load server for health", zap.String("server_id", serverID), zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}

		conns, err := repository.CountActiveConnectionsByServer(c.Context(), db, serverID)
		if err != nil {
			logger.Error("admin: failed to count server connections", zap.String("server_id", serverID), zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}

		var lastSeen interface{}
		if server.LastSeenAt != nil {
			lastSeen = server.LastSeenAt.UTC().Format(time.RFC3339)
		}

		return c.JSON(fiber.Map{"data": fiber.Map{
			"concurrent_conns": conns,
			"last_seen_at":     lastSeen,
			"current_load":     server.CurrentLoad,
		}})
	}
}
