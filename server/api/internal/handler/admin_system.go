package handler

import (
	"context"
	"errors"
	"strings"
	"time"

	"vpnapp/server/api/internal/cache"
	"vpnapp/server/api/internal/lava"
	"vpnapp/server/api/internal/model"
	"vpnapp/server/api/internal/repository"

	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// validBroadcastSeverities is the closed set a broadcast severity must belong
// to (T-07-30 — no arbitrary client-supplied severity reaches the DB).
var validBroadcastSeverities = map[string]bool{
	"info":     true,
	"warning":  true,
	"critical": true,
}

const maxFlagReasonLen = 500

// AdminListFeatureFlags handles GET /admin/feature-flags. Returns every flag
// row so the panel can render toggles with last-updated metadata.
func AdminListFeatureFlags(logger *zap.Logger, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		flags, err := repository.ListFeatureFlags(c.Context(), db)
		if err != nil {
			logger.Error("admin: failed to list feature flags", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		return c.JSON(fiber.Map{"data": fiber.Map{"flags": flags}})
	}
}

type setFeatureFlagRequest struct {
	Value  bool   `json:"value"`
	Reason string `json:"reason"`
}

// AdminSetFeatureFlag handles PUT /admin/feature-flags/:key. Body {value bool,
// reason string}. Writes the authoritative DB row with the acting admin as
// updated_by, busts the ~10s flag cache so the toggle propagates immediately
// (the TTL is only the backstop), and writes an explicit reason-carrying audit
// row (the AuditLog middleware also records the outer method+path row).
func AdminSetFeatureFlag(logger *zap.Logger, db *gorm.DB, redisClient *redis.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		key := strings.TrimSpace(c.Params("key"))
		if key == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "flag key required"})
		}
		var req setFeatureFlagRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
		}
		reason := strings.TrimSpace(req.Reason)
		if len(reason) > maxFlagReasonLen {
			reason = reason[:maxFlagReasonLen]
		}

		adminID, _ := c.Locals("user_id").(string)
		var updatedBy *string
		if adminID != "" {
			a := adminID
			updatedBy = &a
		}

		if err := repository.SetFeatureFlag(c.Context(), db, key, req.Value, updatedBy); err != nil {
			logger.Error("admin: failed to set feature flag", zap.String("flag", key), zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}

		// Bust the cache so the new value is visible on the next read. The ~10s
		// TTL is the backstop if this DEL fails (best-effort, never fails the write).
		if err := cache.BustFlag(c.Context(), redisClient, key); err != nil {
			logger.Warn("admin: BustFlag failed (10s TTL is the backstop)",
				zap.String("flag", key), zap.Error(err))
		}

		// Explicit reason-carrying audit row (mirrors the per-user control
		// writes). target_id is left NULL — the audit_log.target_id column is a
		// UUID (migration 014) and a flag key is not a UUID; the key + value
		// live in Details instead.
		writeSystemAudit(c, logger, db, "set_feature_flag", model.AuditDetails{
			"flag":   key,
			"value":  req.Value,
			"reason": reason,
		})

		logger.Info("admin: set feature flag", zap.String("flag", key), zap.Bool("value", req.Value))
		return c.JSON(fiber.Map{"data": fiber.Map{"key": key, "value": req.Value}})
	}
}

// AdminListBroadcasts handles GET /admin/broadcasts. Returns every broadcast
// (active, scheduled, expired) for the management view.
func AdminListBroadcasts(logger *zap.Logger, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		msgs, err := repository.ListAllBroadcasts(c.Context(), db)
		if err != nil {
			logger.Error("admin: failed to list broadcasts", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		return c.JSON(fiber.Map{"data": fiber.Map{"broadcasts": msgs}})
	}
}

type broadcastRequest struct {
	Title      string     `json:"title"`
	Body       string     `json:"body"`
	Severity   string     `json:"severity"`
	Locale     *string    `json:"locale"`
	TargetTier *string    `json:"target_tier"`
	StartsAt   *time.Time `json:"starts_at"`
	EndsAt     *time.Time `json:"ends_at"`
}

// AdminCreateBroadcast handles POST /admin/broadcasts. Validates the severity
// enum (T-07-30) and that title/body are non-empty, then inserts the row.
func AdminCreateBroadcast(logger *zap.Logger, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req broadcastRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
		}
		title := strings.TrimSpace(req.Title)
		body := strings.TrimSpace(req.Body)
		if title == "" || body == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "title and body are required"})
		}
		severity := strings.TrimSpace(req.Severity)
		if severity == "" {
			severity = "info"
		}
		if !validBroadcastSeverities[severity] {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "severity must be one of info, warning, critical"})
		}

		msg := &model.BroadcastMessage{
			Title:      title,
			Body:       body,
			Severity:   severity,
			Locale:     req.Locale,
			TargetTier: req.TargetTier,
			StartsAt:   req.StartsAt,
			EndsAt:     req.EndsAt,
		}
		if err := repository.CreateBroadcast(c.Context(), db, msg); err != nil {
			logger.Error("admin: failed to create broadcast", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{"data": msg})
	}
}

// AdminUpdateBroadcast handles PATCH /admin/broadcasts/:id. Applies a partial
// update; an invalid severity is rejected. 404 when the id does not exist.
func AdminUpdateBroadcast(logger *zap.Logger, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id := c.Params("id")
		if id == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "broadcast id required"})
		}
		var req broadcastRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
		}

		updates := map[string]interface{}{}
		if title := strings.TrimSpace(req.Title); title != "" {
			updates["title"] = title
		}
		if body := strings.TrimSpace(req.Body); body != "" {
			updates["body"] = body
		}
		if severity := strings.TrimSpace(req.Severity); severity != "" {
			if !validBroadcastSeverities[severity] {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "severity must be one of info, warning, critical"})
			}
			updates["severity"] = severity
		}
		if req.Locale != nil {
			updates["locale"] = req.Locale
		}
		if req.TargetTier != nil {
			updates["target_tier"] = req.TargetTier
		}
		if req.StartsAt != nil {
			updates["starts_at"] = req.StartsAt
		}
		if req.EndsAt != nil {
			updates["ends_at"] = req.EndsAt
		}
		if len(updates) == 0 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "no updatable fields provided"})
		}

		if err := repository.UpdateBroadcast(c.Context(), db, id, updates); err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "broadcast not found"})
			}
			logger.Error("admin: failed to update broadcast", zap.String("id", id), zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		return c.JSON(fiber.Map{"data": fiber.Map{"id": id}})
	}
}

// AdminDeleteBroadcast handles DELETE /admin/broadcasts/:id. 404 when missing.
func AdminDeleteBroadcast(logger *zap.Logger, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id := c.Params("id")
		if id == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "broadcast id required"})
		}
		if err := repository.DeleteBroadcast(c.Context(), db, id); err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "broadcast not found"})
			}
			logger.Error("admin: failed to delete broadcast", zap.String("id", id), zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		return c.JSON(fiber.Map{"data": fiber.Map{"id": id}})
	}
}

// AdminDepsHealth handles GET /admin/system/deps-health (ADMIN-08).
//
// It returns a DETAILED dependency map for the admin System page: live status
// for postgres / redis / lava plus a per-tunnel-server list carrying
// last_seen_at, current_load, and a fresh flag (last_seen_at within
// tunnelFreshWindow). Unlike the public /readyz — which returns status WORDS
// only so anonymous callers never see topology (T-07-09) — this route is on the
// AdminRequired group, so the per-server detail is intentionally exposed
// (T-07-37: gated behind admin auth).
//
// It reuses the SAME probe building blocks as Readyz (checkPostgres, checkRedis,
// checkLava) so the DB/Redis checks carry the same 500ms timeouts and the lava
// verdict comes from the ≤60s Redis cache — never a flaky per-poll dial
// (T-07-38). Unlike Readyz it never 503s: it always returns 200 with whatever
// the current per-dep status is, because the System page wants to RENDER a
// degraded dep, not be told the request failed.
func AdminDepsHealth(logger *zap.Logger, db *gorm.DB, redisClient *redis.Client, lavaClient *lava.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		ctx := c.Context()

		servers, err := repository.ListServerHealth(ctx, db)
		if err != nil {
			logger.Error("admin: failed to list server health", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}

		cutoff := time.Now().Add(-tunnelFreshWindow)
		tunnelServers := make([]fiber.Map, 0, len(servers))
		for _, s := range servers {
			fresh := s.LastSeenAt != nil && s.LastSeenAt.After(cutoff)
			tunnelServers = append(tunnelServers, fiber.Map{
				"id":           s.ID,
				"hostname":     s.Hostname,
				"is_active":    s.IsActive,
				"current_load": s.CurrentLoad,
				"last_seen_at": s.LastSeenAt,
				"fresh":        fresh,
			})
		}

		return c.JSON(fiber.Map{"data": fiber.Map{
			"postgres":       statusWord(checkPostgres(ctx, db)),
			"redis":          statusWord(checkRedis(ctx, redisClient)),
			"lava":           lavaStatusWord(checkLavaVerdict(ctx, redisClient)),
			"tunnel_servers": tunnelServers,
		}})
	}
}

// checkLavaVerdict reads the cached lava reachability verdict WITHOUT triggering
// a dial. It mirrors the cache-read half of checkLava but, for the admin
// deps-health view, distinguishes a cache miss ("unknown") from a definitive
// "down" — the System page can render "unknown" honestly instead of falsely
// claiming lava is down before the first verdict has been cached. It never
// dials lava itself (T-07-38: reuses the same ≤60s cache as readyz).
func checkLavaVerdict(ctx context.Context, redisClient *redis.Client) string {
	verdict, _ := cache.GetLavaReachable(ctx, redisClient)
	switch verdict {
	case "ok", "down":
		return verdict
	default:
		return "unknown"
	}
}

// lavaStatusWord maps a cached lava verdict to the deps-health value. "ok"/"down"
// pass through; anything else (cache miss) is "unknown".
func lavaStatusWord(verdict string) string {
	switch verdict {
	case "ok", "down":
		return verdict
	default:
		return "unknown"
	}
}

// writeSystemAudit persists an explicit, reason-carrying audit row for a system
// control action. Mirrors writeUserControlAudit in admin_user_controls.go: the
// acting admin is read from c.Locals("user_id"); a persistence failure is logged
// but never fails the action (the mutation already succeeded). target_id is left
// NULL — system actions (flag toggles) have no UUID target; the subject lives in
// Details.
func writeSystemAudit(c *fiber.Ctx, logger *zap.Logger, db *gorm.DB, action string, details model.AuditDetails) {
	adminID, _ := c.Locals("user_id").(string)
	if adminID == "" {
		logger.Error("admin: missing user_id in locals for system audit write",
			zap.String("action", action))
		return
	}
	entry := &model.AuditLogEntry{
		AdminID: adminID,
		Action:  action,
		Details: details,
		IP:      c.IP(),
	}
	if err := repository.CreateAuditEntry(c.Context(), db, entry); err != nil {
		logger.Error("admin: failed to persist system audit entry",
			zap.String("action", action), zap.Error(err))
	}
}
