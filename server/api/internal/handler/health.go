package handler

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"time"

	"vpnapp/server/api/internal/cache"
	"vpnapp/server/api/internal/lava"
	"vpnapp/server/api/internal/repository"

	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
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

// depCheckTimeout bounds each individual readyz dependency probe (DB / Redis).
// 500ms per ADMIN-IMPROVEMENTS §4.1 — a hung dep yields "down", never a hung probe.
const depCheckTimeout = 500 * time.Millisecond

// lavaDialTimeout bounds the lazy lava-reachability refresh (only on cache miss).
const lavaDialTimeout = 2 * time.Second

// tunnelFreshWindow is how recent a tunnel's last_seen_at must be to count as alive.
const tunnelFreshWindow = 90 * time.Second

// Livez handles GET /livez — the K8s-style liveness probe (ADMIN-07).
// Returns 200 immediately with ZERO I/O: no DB, no Redis, no network. As long
// as the process can serve this handler it is "alive". Never gate it on deps —
// that is what /readyz is for.
func Livez() fiber.Handler {
	return func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "alive"})
	}
}

// Readyz handles GET /readyz — the K8s-style readiness probe (ADMIN-07).
//
// Returns 200 {data:{status:"ready"}} only when ALL four deps are healthy:
//   - Postgres: SELECT 1 within depCheckTimeout
//   - Redis:    PING within depCheckTimeout
//   - lava:     cached reachability verdict (≤60s); lazy single dial on miss
//     wrapped in lavaDialTimeout — NEVER a fresh dial per probe
//   - tunnel:   at least one active vpn_servers row with last_seen_at within
//     tunnelFreshWindow (cheap DB read, no network)
//
// Otherwise returns 503 {data:{deps:{postgres,redis,lava,tunnel}}} where each
// value is the status WORD "ok" or "down" ONLY. T-07-09: the body NEVER carries
// underlying error strings, hostnames, or versions — readyz is anonymous-facing.
func Readyz(db *gorm.DB, redisClient *redis.Client, lavaClient *lava.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		ctx := c.Context()

		deps := map[string]string{
			"postgres": statusWord(checkPostgres(ctx, db)),
			"redis":    statusWord(checkRedis(ctx, redisClient)),
			"lava":     statusWord(checkLava(ctx, redisClient, lavaClient)),
			"tunnel":   statusWord(checkTunnel(ctx, db)),
		}

		for _, status := range deps {
			if status != "ok" {
				return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
					"data": fiber.Map{"deps": deps},
				})
			}
		}
		return c.JSON(fiber.Map{"data": fiber.Map{"status": "ready"}})
	}
}

// statusWord maps a dep-check outcome to the only two words the readyz body may
// expose. Keeping the mapping here guarantees no error string ever leaks (T-07-09).
func statusWord(healthy bool) string {
	if healthy {
		return "ok"
	}
	return "down"
}

// checkPostgres runs SELECT 1 with a tight per-probe timeout.
func checkPostgres(parent context.Context, db *gorm.DB) bool {
	if db == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(parent, depCheckTimeout)
	defer cancel()
	return db.WithContext(ctx).Exec("SELECT 1").Error == nil
}

// checkRedis pings Redis with a tight per-probe timeout.
func checkRedis(parent context.Context, redisClient *redis.Client) bool {
	if redisClient == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(parent, depCheckTimeout)
	defer cancel()
	return redisClient.Ping(ctx).Err() == nil
}

// checkLava returns lava reachability from the Redis cache (≤60s). On a cache
// miss it performs ONE lazy dial bounded by lavaDialTimeout and caches the
// verdict — it NEVER dials lava on a cache hit (T-07-10 DoS mitigation). A cache
// miss with no usable client treats lava as reachable-unknown → down only if the
// single dial fails.
func checkLava(parent context.Context, redisClient *redis.Client, lavaClient *lava.Client) bool {
	verdict, _ := cache.GetLavaReachable(parent, redisClient)
	switch verdict {
	case "ok":
		return true
	case "down":
		return false
	}
	// Cache miss → ONE lazy dial with a hard timeout; never block the probe.
	if lavaClient == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(parent, lavaDialTimeout)
	defer cancel()
	_, err := lavaClient.ListProducts(ctx)
	reachable := err == nil
	_ = cache.SetLavaReachable(parent, redisClient, reachable)
	return reachable
}

// checkTunnel reports whether at least one active tunnel posted a heartbeat
// within tunnelFreshWindow. Cheap DB read of last_seen_at — no network call.
func checkTunnel(parent context.Context, db *gorm.DB) bool {
	if db == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(parent, depCheckTimeout)
	defer cancel()
	n, err := repository.CountFreshServers(ctx, db, tunnelFreshWindow)
	return err == nil && n > 0
}

// GetSubscription handles GET /subscription.
// Returns the user's active subscription from the database.
//
// PAY-11 / D-24: defaults come from the system plan (via FindSystemPlanID); paid-plan
// limits come from FindPlanByCode(sub.Plan). No more legacy in-Go limits map.
func GetSubscription(logger *zap.Logger, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID := c.Locals("user_id").(string)

		sub, err := repository.FindSubscriptionByUserID(c.Context(), db, userID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				// No subscription — return system-plan defaults.
				systemPlanID, sperr := repository.FindSystemPlanID(c.Context(), db)
				if sperr != nil {
					logger.Error("GetSubscription: FindSystemPlanID failed", zap.Error(sperr))
					return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
						"error": "internal server error",
					})
				}
				systemPlan, sperr := repository.FindPlanByID(c.Context(), db, systemPlanID)
				if sperr != nil {
					logger.Error("GetSubscription: FindPlanByID(system) failed", zap.Error(sperr))
					return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
						"error": "internal server error",
					})
				}
				return c.JSON(fiber.Map{
					"data": fiber.Map{
						"plan":        systemPlan.Code,
						"is_active":   true,
						"max_devices": systemPlan.MaxDevices,
					},
				})
			}
			logger.Error("failed to get subscription", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}

		// Active subscription — read limits from the matching plan row.
		plan, perr := repository.FindPlanByCode(c.Context(), db, sub.Plan)
		if perr != nil {
			// Plan was soft-deleted — fall back to system plan for the limits
			// (the user keeps Pro until expiry per ADR §19.10; display the
			// numeric limits from the system plan to avoid 500).
			logger.Warn("GetSubscription: FindPlanByCode failed; falling back to system plan",
				zap.String("plan", sub.Plan), zap.Error(perr))
			systemPlanID, sperr := repository.FindSystemPlanID(c.Context(), db)
			if sperr != nil {
				logger.Error("GetSubscription: FindSystemPlanID failed (fallback)", zap.Error(sperr))
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error": "internal server error",
				})
			}
			plan, sperr = repository.FindPlanByID(c.Context(), db, systemPlanID)
			if sperr != nil {
				logger.Error("GetSubscription: FindPlanByID(system) failed (fallback)", zap.Error(sperr))
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error": "internal server error",
				})
			}
		}

		return c.JSON(fiber.Map{
			"data": fiber.Map{
				"id":          sub.ID,
				"plan":        sub.Plan,
				"is_active":   sub.IsActive,
				"started_at":  sub.StartedAt,
				"expires_at":  sub.ExpiresAt,
				"max_devices": plan.MaxDevices,
			},
		})
	}
}

// GetAccount handles GET /account.
// Returns the user's account information from the database.
func GetAccount(logger *zap.Logger, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID := c.Locals("user_id").(string)

		user, err := repository.FindUserByID(c.Context(), db, userID)
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

		if err := repository.UpdateUserName(c.Context(), db, userID, name); err != nil {
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
