package handler

import (
	"errors"
	"regexp"

	"vpnapp/server/api/internal/cache"
	"vpnapp/server/api/internal/model"
	"vpnapp/server/api/internal/repository"

	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// codeRegex enforces plans.code format per ADR §19.7.2 (^[a-z0-9][a-z0-9_-]*$, 1-40 chars).
var codeRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// adminPlanCreateReq is the POST /admin/plans body.
//
// NOTE: is_system is intentionally NOT a field here (D-32 §4 — defence in
// depth). JSON unmarshal silently ignores unknown keys, and the handler
// explicitly sets plan.IsSystem=false; the repository layer ALSO forces false.
type adminPlanCreateReq struct {
	Code           string                 `json:"code"`
	Name           string                 `json:"name"`
	Description    string                 `json:"description"`
	MaxDevices     int                    `json:"max_devices"`
	MaxServers     int                    `json:"max_servers"`
	SpeedLimitMbps int                    `json:"speed_limit_mbps"`
	SortOrder      int                    `json:"sort_order"`
	ServerIDs      []string               `json:"server_ids"`
	Offers         []adminPlanOfferCreate `json:"offers"`
}

// adminPlanOfferCreate is one element of adminPlanCreateReq.Offers AND
// the body for POST /admin/plans/:id/offers.
type adminPlanOfferCreate struct {
	Periodicity string  `json:"periodicity"`
	Currency    string  `json:"currency"`
	Amount      float64 `json:"amount"`
	LavaOfferID *string `json:"lava_offer_id,omitempty"`
}

// adminPlanUpdateReq is the PATCH /admin/plans/:id body. `code` and `is_system`
// are absent — both immutable (ADR §19.7.4 + D-32 §4). The repository layer
// ALSO strips these keys (defence in depth).
type adminPlanUpdateReq struct {
	Name           *string `json:"name,omitempty"`
	Description    *string `json:"description,omitempty"`
	MaxDevices     *int    `json:"max_devices,omitempty"`
	MaxServers     *int    `json:"max_servers,omitempty"`
	SpeedLimitMbps *int    `json:"speed_limit_mbps,omitempty"`
	SortOrder      *int    `json:"sort_order,omitempty"`
	IsActive       *bool   `json:"is_active,omitempty"`
}

// adminReplaceServersReq is the PUT /admin/plans/:id/servers body.
type adminReplaceServersReq struct {
	ServerIDs []string `json:"server_ids"`
}

// adminReplaceOfferReq is the POST /admin/plans/:id/offers/:offer_id/replace body
// (PAY-15 price versioning). periodicity + currency are NOT here — they're
// inherited from the old offer (immutable per ADR §19.7.7).
type adminReplaceOfferReq struct {
	Amount      float64 `json:"amount"`
	LavaOfferID *string `json:"lava_offer_id,omitempty"`
}

// --- Validation helpers ---

// validatePlanCode enforces the regex + length rule for plans.code.
func validatePlanCode(s string) error {
	if len(s) == 0 || len(s) > 40 {
		return errors.New("code must be 1-40 chars")
	}
	if !codeRegex.MatchString(s) {
		return errors.New("code must match ^[a-z0-9][a-z0-9_-]*$")
	}
	return nil
}

// validatePlanFields enforces the rest of the plan-creation rules.
// Returns an error message suitable for a 400 response when invalid.
func validatePlanFields(name string, maxDevices, maxServers, speedLimitMbps int) error {
	if len(name) == 0 || len(name) > 100 {
		return errors.New("name must be 1-100 chars")
	}
	if maxDevices != -1 && (maxDevices < 1 || maxDevices > 1000) {
		return errors.New("max_devices must be -1 or 1..1000")
	}
	if maxServers != -1 && (maxServers < 0 || maxServers > 9999) {
		return errors.New("max_servers must be -1 or 0..9999")
	}
	if speedLimitMbps < 0 || speedLimitMbps > 100000 {
		return errors.New("speed_limit_mbps must be 0..100000")
	}
	return nil
}

// validateOfferTuple enforces the periodicity / currency / amount rules.
// Reuses the package-level allowedPeriodicities and allowedCurrencies maps
// from payment.go (same package).
func validateOfferTuple(periodicity, currency string, amount float64) error {
	if _, ok := allowedPeriodicities[periodicity]; !ok {
		return errors.New("periodicity must be ONE_TIME|MONTHLY|PERIOD_90_DAYS|PERIOD_180_DAYS|PERIOD_YEAR")
	}
	if _, ok := allowedCurrencies[currency]; !ok {
		return errors.New("currency must be USD|EUR|RUB")
	}
	if amount < 0 {
		return errors.New("amount must be >= 0")
	}
	return nil
}

// validatePlanServerIDs checks that every server_id in the slice corresponds
// to a real, active VPN server. Returns the offending server_id and an error
// on the first miss; ("", nil) when every id resolves.
//
// WR-02: AdminCreatePlan and AdminReplacePlanServers share this validator
// so admins get the same 422 response (with the bad server_id echoed) from
// both endpoints. The FK constraint in migration 019 is the safety net, but
// catching the bad id in the handler avoids the 500-from-FK-violation UX
// and the wasted tx rollback.
func validatePlanServerIDs(db *gorm.DB, serverIDs []string) (string, error) {
	for _, sid := range serverIDs {
		var n int64
		if err := db.Table("vpn_servers").Where("id = ? AND is_active = ?", sid, true).Count(&n).Error; err != nil {
			return sid, err
		}
		if n == 0 {
			return sid, errors.New("server not found or inactive")
		}
	}
	return "", nil
}

// bustPlansCacheBest is a thin wrapper that swallows errors — the cache is
// best-effort. A bust failure means the next reader gets stale data for up
// to 60s (the cache TTL), which is acceptable degradation.
func bustPlansCacheBest(c *fiber.Ctx, redisClient *redis.Client, logger *zap.Logger, opName string) {
	if err := cache.BustPlansCache(c.Context(), redisClient); err != nil {
		logger.Warn("plans cache bust failed",
			zap.String("op", opName),
			zap.Error(err),
		)
	}
}

// --- Handlers ---

// AdminListPlans handles GET /admin/plans — returns ALL plans (active +
// inactive). Includes computed fields (server_count, offer_count,
// active_user_count) per ADR §19.7.1.
func AdminListPlans(logger *zap.Logger, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		plans, err := repository.ListAllPlans(c.Context(), db)
		if err != nil {
			logger.Error("AdminListPlans", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		out := make([]fiber.Map, 0, len(plans))
		for _, p := range plans {
			activeUsers, _ := repository.CountActiveUsersOnPlan(c.Context(), db, p.ID)
			var serverCount, offerCount int64
			_ = db.Table("plan_servers").Where("plan_id = ?", p.ID).Count(&serverCount).Error
			_ = db.Table("plan_offers").Where("plan_id = ?", p.ID).Count(&offerCount).Error
			out = append(out, fiber.Map{
				"id":                p.ID,
				"code":              p.Code,
				"name":              p.Name,
				"description":       p.Description,
				"max_devices":       p.MaxDevices,
				"max_servers":       p.MaxServers,
				"speed_limit_mbps":  p.SpeedLimitMbps,
				"is_active":         p.IsActive,
				"is_system":         p.IsSystem,
				"sort_order":        p.SortOrder,
				"server_count":      serverCount,
				"offer_count":       offerCount,
				"active_user_count": activeUsers,
				"created_at":        p.CreatedAt,
				"updated_at":        p.UpdatedAt,
			})
		}
		return c.JSON(fiber.Map{"data": out})
	}
}

// AdminCreatePlan handles POST /admin/plans (ADR §19.7.2).
// is_system is FORCED to false regardless of request body (D-32 §4 defence in depth).
func AdminCreatePlan(logger *zap.Logger, db *gorm.DB, redisClient *redis.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req adminPlanCreateReq
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
		}
		if err := validatePlanCode(req.Code); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}
		if err := validatePlanFields(req.Name, req.MaxDevices, req.MaxServers, req.SpeedLimitMbps); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}
		// Validate every offer BEFORE opening the tx so we fail fast with
		// a meaningful 400 instead of rolling back a partial create.
		for _, of := range req.Offers {
			if err := validateOfferTuple(of.Periodicity, of.Currency, of.Amount); err != nil {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
			}
		}
		// WR-02: validate server_ids BEFORE opening the tx so admins get the
		// same 422-with-bad-id response that AdminReplacePlanServers returns,
		// rather than a 500 from a FK violation inside the rolled-back tx.
		if badSID, err := validatePlanServerIDs(db, req.ServerIDs); err != nil {
			if badSID != "" {
				return c.Status(fiber.StatusUnprocessableEntity).JSON(fiber.Map{
					"error":     "server not found or inactive",
					"server_id": badSID,
				})
			}
			logger.Error("AdminCreatePlan validateServerIDs", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}

		plan := &model.Plan{
			Code:           req.Code,
			Name:           req.Name,
			Description:    req.Description,
			MaxDevices:     req.MaxDevices,
			MaxServers:     req.MaxServers,
			SpeedLimitMbps: req.SpeedLimitMbps,
			IsActive:       true,
			IsSystem:       false, // forced — repository ALSO forces false (defence in depth)
			SortOrder:      req.SortOrder,
		}

		// One transaction so plan + plan_servers + plan_offers all-or-nothing.
		err := db.Transaction(func(tx *gorm.DB) error {
			if err := repository.CreatePlan(c.Context(), tx, plan); err != nil {
				return err
			}
			for _, sid := range req.ServerIDs {
				if err := repository.AddPlanServer(c.Context(), tx, plan.ID, sid); err != nil {
					return err
				}
			}
			for _, of := range req.Offers {
				offer := &model.PlanOffer{
					PlanID:      plan.ID,
					Periodicity: of.Periodicity,
					Currency:    of.Currency,
					Amount:      of.Amount,
					LavaOfferID: of.LavaOfferID,
					IsActive:    true,
				}
				if err := repository.CreatePlanOffer(c.Context(), tx, offer); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			logger.Error("AdminCreatePlan", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "create plan failed"})
		}

		bustPlansCacheBest(c, redisClient, logger, "create_plan")
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{"data": plan})
	}
}

// AdminGetPlan handles GET /admin/plans/:id — full detail with servers + offers.
func AdminGetPlan(logger *zap.Logger, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id := c.Params("id")
		plan, err := repository.FindPlanByID(c.Context(), db, id)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "plan not found"})
			}
			logger.Error("AdminGetPlan", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		servers, _ := repository.ListPlanServersJoined(c.Context(), db, id)
		offers, _ := repository.ListOffersForPlan(c.Context(), db, id)
		activeUsers, _ := repository.CountActiveUsersOnPlan(c.Context(), db, id)
		return c.JSON(fiber.Map{
			"data": fiber.Map{
				"id":                plan.ID,
				"code":              plan.Code,
				"name":              plan.Name,
				"description":       plan.Description,
				"max_devices":       plan.MaxDevices,
				"max_servers":       plan.MaxServers,
				"speed_limit_mbps":  plan.SpeedLimitMbps,
				"is_active":         plan.IsActive,
				"is_system":         plan.IsSystem,
				"sort_order":        plan.SortOrder,
				"servers":           servers,
				"offers":            offers,
				"active_user_count": activeUsers,
				"created_at":        plan.CreatedAt,
				"updated_at":        plan.UpdatedAt,
			},
		})
	}
}

// AdminUpdatePlan handles PATCH /admin/plans/:id.
// code + is_system are absent from adminPlanUpdateReq — IMMUTABLE.
// Repository layer ALSO strips them (defence in depth).
func AdminUpdatePlan(logger *zap.Logger, db *gorm.DB, redisClient *redis.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id := c.Params("id")
		var req adminPlanUpdateReq
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
		}
		updates := map[string]interface{}{}
		if req.Name != nil {
			if len(*req.Name) == 0 || len(*req.Name) > 100 {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "name must be 1-100 chars"})
			}
			updates["name"] = *req.Name
		}
		if req.Description != nil {
			updates["description"] = *req.Description
		}
		if req.MaxDevices != nil {
			if *req.MaxDevices != -1 && (*req.MaxDevices < 1 || *req.MaxDevices > 1000) {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "max_devices must be -1 or 1..1000"})
			}
			updates["max_devices"] = *req.MaxDevices
		}
		if req.MaxServers != nil {
			if *req.MaxServers != -1 && (*req.MaxServers < 0 || *req.MaxServers > 9999) {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "max_servers must be -1 or 0..9999"})
			}
			updates["max_servers"] = *req.MaxServers
		}
		if req.SpeedLimitMbps != nil {
			if *req.SpeedLimitMbps < 0 || *req.SpeedLimitMbps > 100000 {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "speed_limit_mbps must be 0..100000"})
			}
			updates["speed_limit_mbps"] = *req.SpeedLimitMbps
		}
		if req.SortOrder != nil {
			updates["sort_order"] = *req.SortOrder
		}
		if req.IsActive != nil {
			// D-32 §4 / ADR §19.7.4: cannot deactivate the system plan.
			plan, ferr := repository.FindPlanByID(c.Context(), db, id)
			if ferr != nil {
				if errors.Is(ferr, repository.ErrNotFound) {
					return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "plan not found"})
				}
				logger.Error("AdminUpdatePlan find", zap.Error(ferr))
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
			}
			if plan.IsSystem && !*req.IsActive {
				return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "cannot deactivate system plan"})
			}
			updates["is_active"] = *req.IsActive
		}

		updated, err := repository.UpdatePlan(c.Context(), db, id, updates)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "plan not found"})
			}
			logger.Error("AdminUpdatePlan", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		bustPlansCacheBest(c, redisClient, logger, "update_plan")
		return c.JSON(fiber.Map{"data": updated})
	}
}

// AdminDeletePlan handles DELETE /admin/plans/:id.
// Soft delete. Returns 403 on system plan EVEN WITH ?force=true (D-32 §4).
// Returns 409 on active_user_count > 0 without ?force=true.
func AdminDeletePlan(logger *zap.Logger, db *gorm.DB, redisClient *redis.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id := c.Params("id")
		force := c.Query("force") == "true"

		plan, err := repository.FindPlanByID(c.Context(), db, id)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "plan not found"})
			}
			logger.Error("AdminDeletePlan find", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		// D-32 §4: system plan delete returns 403 EVEN WITH ?force=true.
		// Two-layer check: handler refuses first; repository also refuses
		// with ErrSystemPlan (defence in depth).
		if plan.IsSystem {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "cannot delete system plan"})
		}

		activeUsers, _ := repository.CountActiveUsersOnPlan(c.Context(), db, id)
		if activeUsers > 0 && !force {
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{
				"error":          "plan has active users — use ?force=true to confirm",
				"affected_users": activeUsers,
			})
		}

		if err := repository.SoftDeletePlan(c.Context(), db, id); err != nil {
			if errors.Is(err, repository.ErrSystemPlan) {
				return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "cannot delete system plan"})
			}
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "plan not found"})
			}
			logger.Error("AdminDeletePlan", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		bustPlansCacheBest(c, redisClient, logger, "delete_plan")
		return c.JSON(fiber.Map{
			"data": fiber.Map{
				"id":             id,
				"deleted":        true,
				"affected_users": activeUsers,
			},
		})
	}
}

// AdminReplacePlanServers handles PUT /admin/plans/:id/servers.
// Atomically replaces the entire plan-server set.
func AdminReplacePlanServers(logger *zap.Logger, db *gorm.DB, redisClient *redis.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		planID := c.Params("id")
		var req adminReplaceServersReq
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
		}
		if _, err := repository.FindPlanByID(c.Context(), db, planID); err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "plan not found"})
			}
			logger.Error("AdminReplacePlanServers find", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		// ADR §19.7.6: every server_id MUST exist + be is_active=true.
		// WR-02: shared helper with AdminCreatePlan so both endpoints behave
		// identically on bad server_ids (422 with the offending id echoed).
		if badSID, err := validatePlanServerIDs(db, req.ServerIDs); err != nil {
			if badSID != "" {
				return c.Status(fiber.StatusUnprocessableEntity).JSON(fiber.Map{
					"error":     "server not found or inactive",
					"server_id": badSID,
				})
			}
			logger.Error("AdminReplacePlanServers validateServerIDs", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		if err := repository.ReplacePlanServers(c.Context(), db, planID, req.ServerIDs); err != nil {
			logger.Error("AdminReplacePlanServers", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		bustPlansCacheBest(c, redisClient, logger, "replace_plan_servers")
		return c.JSON(fiber.Map{"data": fiber.Map{"plan_id": planID, "server_ids": req.ServerIDs}})
	}
}

// AdminAddPlanServer handles POST /admin/plans/:id/servers/:server_id.
// Idempotent — ADR §19.7.6 specifies 201 on re-add of an existing pairing.
func AdminAddPlanServer(logger *zap.Logger, db *gorm.DB, redisClient *redis.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		planID := c.Params("id")
		serverID := c.Params("server_id")
		if _, err := repository.FindPlanByID(c.Context(), db, planID); err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "plan not found"})
			}
			logger.Error("AdminAddPlanServer find plan", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		var n int64
		_ = db.Table("vpn_servers").Where("id = ? AND is_active = ?", serverID, true).Count(&n).Error
		if n == 0 {
			return c.Status(fiber.StatusUnprocessableEntity).JSON(fiber.Map{"error": "server not found or inactive"})
		}
		if err := repository.AddPlanServer(c.Context(), db, planID, serverID); err != nil {
			logger.Error("AdminAddPlanServer", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		bustPlansCacheBest(c, redisClient, logger, "add_plan_server")
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{
			"data": fiber.Map{"plan_id": planID, "server_id": serverID},
		})
	}
}

// AdminRemovePlanServer handles DELETE /admin/plans/:id/servers/:server_id.
// Does NOT force-disconnect active users (D-23).
func AdminRemovePlanServer(logger *zap.Logger, db *gorm.DB, redisClient *redis.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		planID := c.Params("id")
		serverID := c.Params("server_id")
		if err := repository.RemovePlanServer(c.Context(), db, planID, serverID); err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "pairing not found"})
			}
			logger.Error("AdminRemovePlanServer", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		bustPlansCacheBest(c, redisClient, logger, "remove_plan_server")
		return c.SendStatus(fiber.StatusNoContent)
	}
}

// AdminListPlanOffers handles GET /admin/plans/:id/offers.
func AdminListPlanOffers(logger *zap.Logger, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		planID := c.Params("id")
		if _, err := repository.FindPlanByID(c.Context(), db, planID); err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "plan not found"})
			}
			logger.Error("AdminListPlanOffers find plan", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		offers, err := repository.ListOffersForPlan(c.Context(), db, planID)
		if err != nil {
			logger.Error("AdminListPlanOffers", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		return c.JSON(fiber.Map{"data": offers})
	}
}

// AdminCreatePlanOffer handles POST /admin/plans/:id/offers.
// Returns 409 on partial-unique violation (active offer for this
// (plan, periodicity, currency) already exists — caller should use
// the /replace endpoint to version the price instead).
func AdminCreatePlanOffer(logger *zap.Logger, db *gorm.DB, redisClient *redis.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		planID := c.Params("id")
		var req adminPlanOfferCreate
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
		}
		if err := validateOfferTuple(req.Periodicity, req.Currency, req.Amount); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}
		if _, err := repository.FindPlanByID(c.Context(), db, planID); err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "plan not found"})
			}
			logger.Error("AdminCreatePlanOffer find plan", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		offer := &model.PlanOffer{
			PlanID:      planID,
			Periodicity: req.Periodicity,
			Currency:    req.Currency,
			Amount:      req.Amount,
			LavaOfferID: req.LavaOfferID,
			IsActive:    true,
		}
		if err := repository.CreatePlanOffer(c.Context(), db, offer); err != nil {
			// Likely partial-unique violation on (plan, periodicity, currency)
			// WHERE is_active=true. Surface as 409 so the admin UI can prompt
			// the operator to use /replace for price versioning.
			logger.Warn("AdminCreatePlanOffer rejected (likely duplicate active)",
				zap.String("plan_id", planID),
				zap.String("periodicity", req.Periodicity),
				zap.String("currency", req.Currency),
				zap.Error(err),
			)
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{
				"error": "active offer already exists for this (periodicity, currency); use POST .../offers/:offer_id/replace to update price",
			})
		}
		bustPlansCacheBest(c, redisClient, logger, "create_plan_offer")
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{"data": offer})
	}
}

// AdminUpdatePlanOffer handles PATCH /admin/plans/:id/offers/:offer_id.
// periodicity + currency immutable (ADR §19.7.7). Repository layer ALSO
// strips them (defence in depth).
func AdminUpdatePlanOffer(logger *zap.Logger, db *gorm.DB, redisClient *redis.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		offerID := c.Params("offer_id")
		var req struct {
			Amount      *float64 `json:"amount,omitempty"`
			LavaOfferID *string  `json:"lava_offer_id,omitempty"`
			IsActive    *bool    `json:"is_active,omitempty"`
		}
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
		}
		updates := map[string]interface{}{}
		if req.Amount != nil {
			if *req.Amount < 0 {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "amount must be >= 0"})
			}
			updates["amount"] = *req.Amount
		}
		if req.LavaOfferID != nil {
			updates["lava_offer_id"] = *req.LavaOfferID
		}
		if req.IsActive != nil {
			updates["is_active"] = *req.IsActive
		}
		updated, err := repository.UpdatePlanOffer(c.Context(), db, offerID, updates)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "offer not found"})
			}
			logger.Error("AdminUpdatePlanOffer", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		bustPlansCacheBest(c, redisClient, logger, "update_plan_offer")
		return c.JSON(fiber.Map{"data": updated})
	}
}

// AdminDeletePlanOffer handles DELETE /admin/plans/:id/offers/:offer_id (soft).
func AdminDeletePlanOffer(logger *zap.Logger, db *gorm.DB, redisClient *redis.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		offerID := c.Params("offer_id")
		if err := repository.DeletePlanOffer(c.Context(), db, offerID); err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "offer not found"})
			}
			logger.Error("AdminDeletePlanOffer", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		bustPlansCacheBest(c, redisClient, logger, "delete_plan_offer")
		return c.SendStatus(fiber.StatusNoContent)
	}
}

// AdminReplacePlanOffer handles POST /admin/plans/:id/offers/:offer_id/replace.
// PAY-15 price versioning — deactivate old + insert new in one tx
// (repository.ReplaceOffer). periodicity + currency inherited from the old
// offer (immutable per ADR §19.7.7).
func AdminReplacePlanOffer(logger *zap.Logger, db *gorm.DB, redisClient *redis.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		planID := c.Params("id")
		oldOfferID := c.Params("offer_id")
		var req adminReplaceOfferReq
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
		}
		if req.Amount < 0 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "amount must be >= 0"})
		}
		// Load the old offer to inherit periodicity + currency. UpdatePlanOffer
		// with an empty updates map short-circuits to a pure SELECT via the
		// findOfferByID internal helper (see plan_repo.go::UpdatePlanOffer:373).
		oldOffer, err := repository.UpdatePlanOffer(c.Context(), db, oldOfferID, map[string]interface{}{})
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "offer not found"})
			}
			logger.Error("AdminReplacePlanOffer find", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		if oldOffer.PlanID != planID {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "offer does not belong to plan"})
		}

		newOffer := &model.PlanOffer{
			PlanID:      planID,
			Periodicity: oldOffer.Periodicity, // inherited — immutable
			Currency:    oldOffer.Currency,    // inherited — immutable
			Amount:      req.Amount,
			LavaOfferID: req.LavaOfferID,
			IsActive:    true,
		}
		saved, err := repository.ReplaceOffer(c.Context(), db, oldOfferID, newOffer)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "offer not found"})
			}
			logger.Error("AdminReplacePlanOffer", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		bustPlansCacheBest(c, redisClient, logger, "replace_plan_offer")
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{"data": saved})
	}
}
