package handler

import (
	"errors"
	"strconv"
	"time"

	"vpnapp/server/api/internal/cache"
	"vpnapp/server/api/internal/repository"

	"vpnapp/server/api/internal/model"

	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// parsePagination extracts page and limit from query params with safe defaults.
// page defaults to 1; limit defaults to 20 and is capped at 100.
func parsePagination(c *fiber.Ctx) (page, limit int) {
	page, _ = strconv.Atoi(c.Query("page", "1"))
	limit, _ = strconv.Atoi(c.Query("limit", "20"))

	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	return page, limit
}

// AdminListUsers handles GET /admin/users.
// Query params: page (default 1), limit (default 20, max 100), search (partial email_hash match).
func AdminListUsers(logger *zap.Logger, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		page, limit := parsePagination(c)
		search := c.Query("search")

		users, total, err := repository.ListUsers(c.Context(), db, page, limit, search)
		if err != nil {
			logger.Error("admin: failed to list users", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}

		// Debug level — the panel auto-refetches this endpoint and
		// logging at Info floods the backend log with one row per
		// dashboard interaction.
		logger.Debug("admin: listed users",
			zap.Int("page", page),
			zap.Int("limit", limit),
			zap.Int64("total", total),
		)

		return c.JSON(fiber.Map{
			"data": fiber.Map{
				"users": users,
				"pagination": fiber.Map{
					"page":        page,
					"limit":       limit,
					"total":       total,
					"total_pages": (total + int64(limit) - 1) / int64(limit),
				},
			},
		})
	}
}

// AdminGetUser handles GET /admin/users/:id.
// Returns the full user record for the given UUID.
func AdminGetUser(logger *zap.Logger, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID := c.Params("id")
		if userID == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "user id required",
			})
		}

		user, err := repository.FindUserByIDAdmin(c.Context(), db, userID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
					"error": "user not found",
				})
			}
			logger.Error("admin: failed to get user", zap.String("user_id", userID), zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}

		return c.JSON(fiber.Map{
			"data": user,
		})
	}
}

// adminUpdateUserRequest defines the fields an admin may change on a user.
// SubscriptionExpiresAt accepts either a full RFC3339 timestamp
// (e.g. "2026-05-10T00:00:00Z") or null to clear the expiration.
// ExtendDays is a convenience: adds N days to the existing expiration
// (or starts from now if none is set). When both are provided,
// SubscriptionExpiresAt wins.
type adminUpdateUserRequest struct {
	SubscriptionTier      string  `json:"subscription_tier"`
	Role                  string  `json:"role"`
	SubscriptionExpiresAt *string `json:"subscription_expires_at"`
	ExtendDays            int     `json:"extend_days"`
}

// AdminUpdateUser handles PATCH /admin/users/:id.
// Accepts subscription_tier, role, subscription_expires_at, and/or extend_days.
// Only provided fields are updated.
//
// PERF-04 / D-06: after a successful update this synchronously busts the
// user:<id> entitlement cache so a tier/role/expiry change is reflected on
// the very next AuthRequired pass (no waiting for the 5s TTL). The bust
// failing must NOT fail the update — log and continue (the TTL is the
// backstop).
func AdminUpdateUser(logger *zap.Logger, db *gorm.DB, redisClient *redis.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID := c.Params("id")
		if userID == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "user id required",
			})
		}

		var req adminUpdateUserRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "invalid request body",
			})
		}

		updates := make(map[string]interface{})
		if req.SubscriptionTier != "" {
			// PAY-11 / D-24: validate tier against the plans table (no hardcoded enum).
			// Lookup BY CODE — both active and inactive plans resolve so admins can
			// re-attach a user to a soft-deleted plan if needed (grandfathering use case).
			plan, perr := repository.FindPlanByCode(c.Context(), db, req.SubscriptionTier)
			if perr != nil {
				if errors.Is(perr, repository.ErrNotFound) {
					return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
						"error": "subscription_tier must match an existing plan code",
					})
				}
				logger.Error("AdminUpdateUser: FindPlanByCode failed",
					zap.String("code", req.SubscriptionTier), zap.Error(perr))
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error": "internal server error",
				})
			}
			updates["subscription_tier"] = req.SubscriptionTier
			// Keep users.plan_id in sync with the denormalised tier string.
			updates["plan_id"] = plan.ID
		}
		if req.Role != "" {
			if req.Role != "user" && req.Role != "admin" {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"error": "role must be 'user' or 'admin'",
				})
			}
			updates["role"] = req.Role
		}

		// Handle subscription expiration — either explicit timestamp or extend_days.
		if req.SubscriptionExpiresAt != nil {
			if *req.SubscriptionExpiresAt == "" {
				updates["subscription_expires_at"] = nil
			} else {
				expires, err := time.Parse(time.RFC3339, *req.SubscriptionExpiresAt)
				if err != nil {
					return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
						"error": "subscription_expires_at must be RFC3339 (e.g. 2026-05-10T00:00:00Z)",
					})
				}
				updates["subscription_expires_at"] = expires
			}
		} else if req.ExtendDays > 0 {
			// Extend from the current expiration or from now, whichever is later.
			user, err := repository.FindUserByIDAdmin(c.Context(), db, userID)
			if err != nil {
				if errors.Is(err, repository.ErrNotFound) {
					return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
						"error": "user not found",
					})
				}
				logger.Error("admin: failed to load user for extend", zap.Error(err))
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error": "internal server error",
				})
			}
			base := time.Now()
			if user.SubscriptionExpiresAt != nil && user.SubscriptionExpiresAt.After(base) {
				base = *user.SubscriptionExpiresAt
			}
			updates["subscription_expires_at"] = base.Add(time.Duration(req.ExtendDays) * 24 * time.Hour)
		}

		if len(updates) == 0 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "no updatable fields provided",
			})
		}

		if err := repository.UpdateUser(c.Context(), db, userID, updates); err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
					"error": "user not found",
				})
			}
			logger.Error("admin: failed to update user", zap.String("user_id", userID), zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}

		// PERF-04 / D-06: synchronously bust user:<id> so the tier/role/expiry
		// change takes effect on the next AuthRequired pass. Best-effort —
		// a bust failure must not fail the update (the 5s TTL is the backstop).
		if err := cache.BustUserCache(c.Context(), redisClient, userID); err != nil {
			logger.Warn("admin: BustUserCache failed (5s TTL is the backstop)",
				zap.String("user_id", userID), zap.Error(err))
		}

		logger.Info("admin: updated user", zap.String("user_id", userID), zap.Any("updates", updates))

		return c.JSON(fiber.Map{
			"data": fiber.Map{
				"id":      userID,
				"updated": updates,
			},
		})
	}
}

// adminServerResponse exposes fields that are marked json:"-" on the
// shared model.VPNServer (because the mobile client must never see
// them). Used only on admin GET endpoints so the panel can display
// capacity, load, and REALITY key material.
type adminServerResponse struct {
	ID               string `json:"id"`
	Hostname         string `json:"hostname"`
	IPAddress        string `json:"ip_address"`
	Region           string `json:"region"`
	City             string `json:"city"`
	Country          string `json:"country"`
	CountryCode      string `json:"country_code"`
	Protocol         string `json:"protocol"`
	Capacity         int    `json:"capacity"`
	CurrentLoad      int    `json:"load_percent"`
	IsActive         bool   `json:"is_active"`
	RealityPublicKey string `json:"reality_public_key"`
	RealityShortID   string `json:"reality_short_id"`
	CreatedAt        string `json:"created_at"`
}

func toAdminServerResponse(s model.VPNServer) adminServerResponse {
	return adminServerResponse{
		ID:               s.ID,
		Hostname:         s.Hostname,
		IPAddress:        s.IPAddress,
		Region:           s.Region,
		City:             s.City,
		Country:          s.Country,
		CountryCode:      s.CountryCode,
		Protocol:         s.Protocol,
		Capacity:         s.Capacity,
		CurrentLoad:      s.CurrentLoad,
		IsActive:         s.IsActive,
		RealityPublicKey: s.RealityPublicKey,
		RealityShortID:   s.RealityShortID,
		CreatedAt:        s.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// AdminListServers handles GET /admin/servers.
// Returns all VPN servers including inactive ones. Uses the
// adminServerResponse DTO so the panel sees capacity and REALITY
// fields hidden from the mobile client.
func AdminListServers(logger *zap.Logger, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		servers, err := repository.ListAllServers(c.Context(), db)
		if err != nil {
			logger.Error("admin: failed to list servers", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}

		logger.Debug("admin: listed servers", zap.Int("count", len(servers)))

		out := make([]adminServerResponse, 0, len(servers))
		for _, s := range servers {
			out = append(out, toAdminServerResponse(s))
		}
		return c.JSON(fiber.Map{"data": out})
	}
}

// adminCreateServerRequest holds all required and optional fields for a new server.
type adminCreateServerRequest struct {
	Hostname         string `json:"hostname"`
	IPAddress        string `json:"ip_address"`
	Region           string `json:"region"`
	City             string `json:"city"`
	Country          string `json:"country"`
	CountryCode      string `json:"country_code"`
	Protocol         string `json:"protocol"`
	Capacity         int    `json:"capacity"`
	RealityPublicKey string `json:"reality_public_key"`
	RealityShortID   string `json:"reality_short_id"`
}

// AdminCreateServer handles POST /admin/servers.
// Creates a new VPN server with all fields; capacity defaults to 500 if omitted.
// On a successful create it synchronously busts cache:servers:active (PERF-01)
// so the next /servers request reflects the new server within one request.
func AdminCreateServer(logger *zap.Logger, db *gorm.DB, redisClient *redis.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req adminCreateServerRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "invalid request body",
			})
		}

		if req.Hostname == "" || req.IPAddress == "" || req.Region == "" ||
			req.City == "" || req.Country == "" || req.CountryCode == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "hostname, ip_address, region, city, country, and country_code are required",
			})
		}

		if len(req.CountryCode) != 2 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "country_code must be a 2-character ISO code",
			})
		}

		protocol := req.Protocol
		if protocol == "" {
			protocol = "vless-reality"
		}
		capacity := req.Capacity
		if capacity <= 0 {
			capacity = 500
		}

		server := model.VPNServer{
			Hostname:         req.Hostname,
			IPAddress:        req.IPAddress,
			Region:           req.Region,
			City:             req.City,
			Country:          req.Country,
			CountryCode:      req.CountryCode,
			Protocol:         protocol,
			Capacity:         capacity,
			IsActive:         true,
			RealityPublicKey: req.RealityPublicKey,
			RealityShortID:   req.RealityShortID,
		}

		if err := repository.CreateServer(c.Context(), db, &server); err != nil {
			if errors.Is(err, repository.ErrDuplicate) {
				return c.Status(fiber.StatusConflict).JSON(fiber.Map{
					"error": "hostname already exists",
				})
			}
			logger.Error("admin: failed to create server", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}

		// Synchronously invalidate the shared /servers cache BEFORE returning so
		// the next /servers request sees the new server within one request
		// (PERF-01 invalidation criterion). Best-effort: log on error but never
		// fail the write — the 60s TTL is the safety net (T-06-SRVCACHE-02).
		// Server writes bust cache:servers:active ONLY, never cache:plans:public:*
		// (RESEARCH Pitfall 5 — distinct namespaces).
		if err := cache.BustServersCache(c.Context(), redisClient); err != nil {
			logger.Warn("admin: failed to bust servers cache after create",
				zap.String("server_id", server.ID), zap.Error(err))
		}

		logger.Info("admin: created server",
			zap.String("server_id", server.ID),
			zap.String("hostname", server.Hostname),
		)

		return c.Status(fiber.StatusCreated).JSON(fiber.Map{
			"data": server,
		})
	}
}

// adminUpdateServerRequest defines fields an admin may update on a server.
type adminUpdateServerRequest struct {
	IPAddress        string `json:"ip_address"`
	Capacity         int    `json:"capacity"`
	IsActive         *bool  `json:"is_active"`
	Protocol         string `json:"protocol"`
	RealityPublicKey string `json:"reality_public_key"`
	RealityShortID   string `json:"reality_short_id"`
	CurrentLoad      *int   `json:"current_load"`
}

// AdminUpdateServer handles PATCH /admin/servers/:id.
// Only explicitly-provided fields are updated. Supports toggling is_active.
// On a successful update it synchronously busts cache:servers:active (PERF-01)
// so a toggled is_active / changed field is reflected on the next /servers read.
func AdminUpdateServer(logger *zap.Logger, db *gorm.DB, redisClient *redis.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		serverID := c.Params("id")
		if serverID == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "server id required",
			})
		}

		var req adminUpdateServerRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "invalid request body",
			})
		}

		updates := make(map[string]interface{})
		if req.IPAddress != "" {
			updates["ip_address"] = req.IPAddress
		}
		if req.Capacity > 0 {
			updates["capacity"] = req.Capacity
		}
		if req.IsActive != nil {
			updates["is_active"] = *req.IsActive
		}
		if req.Protocol != "" {
			updates["protocol"] = req.Protocol
		}
		if req.RealityPublicKey != "" {
			updates["reality_public_key"] = req.RealityPublicKey
		}
		if req.RealityShortID != "" {
			updates["reality_short_id"] = req.RealityShortID
		}
		if req.CurrentLoad != nil {
			updates["current_load"] = *req.CurrentLoad
		}

		if len(updates) == 0 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "no updatable fields provided",
			})
		}

		if err := repository.UpdateServer(c.Context(), db, serverID, updates); err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
					"error": "server not found",
				})
			}
			logger.Error("admin: failed to update server", zap.String("server_id", serverID), zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}

		// Synchronously bust the shared /servers cache before returning so the
		// update (including is_active toggles that add/remove a server from the
		// active list) is visible on the next /servers read (PERF-01).
		if err := cache.BustServersCache(c.Context(), redisClient); err != nil {
			logger.Warn("admin: failed to bust servers cache after update",
				zap.String("server_id", serverID), zap.Error(err))
		}

		logger.Info("admin: updated server", zap.String("server_id", serverID), zap.Any("updates", updates))

		return c.JSON(fiber.Map{
			"data": fiber.Map{
				"id":      serverID,
				"updated": updates,
			},
		})
	}
}

// AdminDeleteServer handles DELETE /admin/servers/:id.
// Performs a soft delete by setting is_active = false.
// On a successful soft-delete it synchronously busts cache:servers:active
// (PERF-01) so the removed server disappears from the next /servers read.
func AdminDeleteServer(logger *zap.Logger, db *gorm.DB, redisClient *redis.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		serverID := c.Params("id")
		if serverID == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "server id required",
			})
		}

		if err := repository.DeleteServer(c.Context(), db, serverID); err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
					"error": "server not found",
				})
			}
			logger.Error("admin: failed to delete server", zap.String("server_id", serverID), zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}

		// Synchronously bust the shared /servers cache before returning so the
		// soft-deleted server is gone from the next /servers read (PERF-01).
		if err := cache.BustServersCache(c.Context(), redisClient); err != nil {
			logger.Warn("admin: failed to bust servers cache after delete",
				zap.String("server_id", serverID), zap.Error(err))
		}

		logger.Info("admin: soft-deleted server", zap.String("server_id", serverID))

		return c.JSON(fiber.Map{
			"data": fiber.Map{
				"id":      serverID,
				"deleted": true,
			},
		})
	}
}

// AdminGetStats handles GET /admin/stats.
// Returns aggregate dashboard numbers: user counts, subscription counts, server counts.
func AdminGetStats(logger *zap.Logger, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		stats, err := repository.GetGlobalStats(c.Context(), db)
		if err != nil {
			logger.Error("admin: failed to get stats", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}

		return c.JSON(fiber.Map{
			"data": stats,
		})
	}
}

// AdminGetAnalytics handles GET /admin/analytics.
// Returns everything the dashboard's "deep-dive" analytics section
// needs in a single response so the panel makes one round-trip
// instead of four. Query params: days (default 30, max 180),
// top_servers_limit (default 5, max 50).
//
// The payload is a fixed-shape object with a nullable field per
// sub-query; a failure in one sub-query does not fail the whole
// endpoint — the UI renders a targeted error card for just that
// widget. This keeps the dashboard mostly-working even when, say,
// the top-servers join hits a cold cache.
func AdminGetAnalytics(logger *zap.Logger, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		days, _ := strconv.Atoi(c.Query("days", "30"))
		topLimit, _ := strconv.Atoi(c.Query("top_servers_limit", "5"))

		type payload struct {
			Traffic    interface{} `json:"traffic"`
			Platforms  interface{} `json:"platforms"`
			Tiers      interface{} `json:"tiers"`
			TopServers interface{} `json:"top_servers"`
			Errors     fiber.Map   `json:"errors,omitempty"`
		}
		var out payload
		errs := fiber.Map{}

		if traffic, err := repository.GetBytesTimeseries(c.Context(), db, days); err != nil {
			logger.Error("analytics: bytes timeseries failed", zap.Error(err))
			errs["traffic"] = err.Error()
		} else {
			out.Traffic = traffic
		}

		if platforms, err := repository.GetPlatformBreakdown(c.Context(), db); err != nil {
			logger.Error("analytics: platform breakdown failed", zap.Error(err))
			errs["platforms"] = err.Error()
		} else {
			out.Platforms = platforms
		}

		if tiers, err := repository.GetTierBreakdown(c.Context(), db); err != nil {
			logger.Error("analytics: tier breakdown failed", zap.Error(err))
			errs["tiers"] = err.Error()
		} else {
			out.Tiers = tiers
		}

		if top, err := repository.GetTopServers(c.Context(), db, days, topLimit); err != nil {
			logger.Error("analytics: top servers failed", zap.Error(err))
			errs["top_servers"] = err.Error()
		} else {
			out.TopServers = top
		}

		if len(errs) > 0 {
			out.Errors = errs
		}
		return c.JSON(fiber.Map{"data": out})
	}
}

// AdminGetStatsTimeseries handles GET /admin/stats/timeseries.
// Query params: days (default 30, max 180).
// Returns per-day signup and connection counts, zero-padded so the
// frontend never has to fill gaps. Backed by repository.GetTimeseries.
func AdminGetStatsTimeseries(logger *zap.Logger, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		days, _ := strconv.Atoi(c.Query("days", "30"))
		signups, connections, err := repository.GetTimeseries(c.Context(), db, days)
		if err != nil {
			logger.Error("admin: failed to get timeseries", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}
		return c.JSON(fiber.Map{
			"data": fiber.Map{
				"signups":     signups,
				"connections": connections,
			},
		})
	}
}

// AdminListUserDevices handles GET /admin/users/:id/devices.
// Returns the list of devices bound to the given user, newest first.
// Admins can view devices on any account (unlike the user-facing
// /devices endpoint which is self-scoped).
func AdminListUserDevices(logger *zap.Logger, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID := c.Params("id")
		if userID == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "user id required",
			})
		}
		// Cheap 404 so the panel shows "user not found" instead of an
		// empty device list when the admin pastes a bad ID.
		if _, err := repository.FindUserByIDAdmin(c.Context(), db, userID); err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
					"error": "user not found",
				})
			}
			logger.Error("admin: failed to lookup user for devices", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}
		devices, err := repository.ListDevicesByUser(c.Context(), db, userID)
		if err != nil {
			logger.Error("admin: failed to list user devices",
				zap.String("user_id", userID),
				zap.Error(err),
			)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}
		return c.JSON(fiber.Map{"data": devices})
	}
}

// AdminListUserConnections handles GET /admin/users/:id/connections.
// Returns up to 50 most-recent connection rows for the given user.
// Read-only — the audit-relevant "connection activity" surface, not the
// active-connections management handler used by the mobile client.
func AdminListUserConnections(logger *zap.Logger, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID := c.Params("id")
		if userID == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "user id required",
			})
		}
		// Cheap 404 on missing user so the panel shows "user not
		// found" instead of an empty list — matches the UX
		// precedent set by AdminListUserDevices.
		if _, err := repository.FindUserByIDAdmin(c.Context(), db, userID); err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
					"error": "user not found",
				})
			}
			logger.Error("admin: failed to lookup user for connections", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}
		limit, _ := strconv.Atoi(c.Query("limit", "50"))
		conns, err := repository.ListConnectionsByUser(c.Context(), db, userID, limit)
		if err != nil {
			logger.Error("admin: failed to list user connections",
				zap.String("user_id", userID),
				zap.Error(err),
			)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}
		return c.JSON(fiber.Map{"data": conns})
	}
}

// AdminGetAuditLog handles GET /admin/audit-log.
// Paginated read of the audit_log table. Query params: page, limit.
func AdminGetAuditLog(logger *zap.Logger, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		page, limit := parsePagination(c)
		entries, total, err := repository.ListAuditEntries(c.Context(), db, page, limit)
		if err != nil {
			logger.Error("admin: failed to list audit entries", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}
		return c.JSON(fiber.Map{
			"data": fiber.Map{
				"entries": entries,
				"pagination": fiber.Map{
					"page":        page,
					"limit":       limit,
					"total":       total,
					"total_pages": (total + int64(limit) - 1) / int64(limit),
				},
			},
		})
	}
}

// AdminDeleteUserDevice handles DELETE /admin/users/:id/devices/:device_id.
// Removes a single device row. Admin override — no ownership check from
// the caller's perspective — but we DO cross-check that the device row
// currently belongs to the user named in the URL. Without that check,
// a stale panel tab or a typo'd URL could delete a device belonging to
// a different user while the audit log records the action against the
// wrong target, defeating the compliance purpose of the log.
func AdminDeleteUserDevice(logger *zap.Logger, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		targetUserID := c.Params("id")
		deviceRowID := c.Params("device_id")
		if targetUserID == "" || deviceRowID == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "user id and device id required",
			})
		}

		// Load the device first so we can verify its current owner
		// matches the URL-encoded user id. FindDeviceByID returns
		// ErrNotFound for both "no such row" and "row exists but
		// belongs to a different user" — the admin sees the same
		// 404 either way, which is fine.
		dev, err := repository.FindDeviceByID(c.Context(), db, deviceRowID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
					"error": "device not found",
				})
			}
			logger.Error("admin: failed to load device",
				zap.String("device_row_id", deviceRowID),
				zap.Error(err),
			)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}
		if dev.UserID != targetUserID {
			logger.Warn("admin: device/user mismatch on delete",
				zap.String("device_row_id", deviceRowID),
				zap.String("url_user_id", targetUserID),
				zap.String("real_owner_id", dev.UserID),
			)
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"error": "device not found",
			})
		}

		if err := repository.AdminDeleteDevice(c.Context(), db, deviceRowID); err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
					"error": "device not found",
				})
			}
			logger.Error("admin: failed to delete device",
				zap.String("device_row_id", deviceRowID),
				zap.Error(err),
			)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}
		logger.Info("admin: device removed",
			zap.String("device_row_id", deviceRowID),
			zap.String("target_user_id", targetUserID),
		)
		return c.SendStatus(fiber.StatusNoContent)
	}
}
