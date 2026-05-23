---
phase: 3
plan: 08
type: execute
slug: lava-top-plans-catalog
plan_number: 8
wave: 4
depends_on: [1, 3, 7]
files_modified:
  - server/api/internal/handler/plans_admin.go
  - server/api/internal/handler/plans_admin_test.go
  - server/api/internal/middleware/audit.go
  - server/api/cmd/main.go
autonomous: true
requirements_addressed: [PAY-13, PAY-14, PAY-15]
estimated_complexity: high
must_haves:
  refs:
    - "See <must_haves> XML block in plan body for canonical truths / artifacts / key_links"

---

<objective>
Implement the admin-only plans CRUD endpoints per ADR §19.7. Two new files: `handler/plans_admin.go` with 13 handlers + tests. Wire all routes under the existing admin group in `cmd/main.go` (inherits AuthRequired + AdminRequired + AuditLog). Each write handler busts the public /plans cache after success (consuming the helper from plan 03-07). Extend `middleware/audit.go::describeAction` with plan-CRUD action names so audit-log rows have meaningful labels.
</objective>

<context>
@.planning/phases/03-lava-top-plans-catalog/03-CONTEXT.md
@.planning/phases/03-lava-top-plans-catalog/03-RESEARCH.md
@docs/ADR-007-lava-sso-rework.md
@server/api/internal/repository/plan_repo.go
@server/api/internal/middleware/audit.go
@server/api/internal/cache/plans_cache.go
@server/api/cmd/main.go
</context>

<interfaces>
13 handlers wired under the admin group:

```go
GET    /api/v1/admin/plans                                  -> AdminListPlans
POST   /api/v1/admin/plans                                  -> AdminCreatePlan
GET    /api/v1/admin/plans/:id                              -> AdminGetPlan
PATCH  /api/v1/admin/plans/:id                              -> AdminUpdatePlan
DELETE /api/v1/admin/plans/:id                              -> AdminDeletePlan   (?force=true)

PUT    /api/v1/admin/plans/:id/servers                      -> AdminReplacePlanServers
POST   /api/v1/admin/plans/:id/servers/:server_id           -> AdminAddPlanServer
DELETE /api/v1/admin/plans/:id/servers/:server_id           -> AdminRemovePlanServer

GET    /api/v1/admin/plans/:id/offers                       -> AdminListPlanOffers
POST   /api/v1/admin/plans/:id/offers                       -> AdminCreatePlanOffer
PATCH  /api/v1/admin/plans/:id/offers/:offer_id             -> AdminUpdatePlanOffer
DELETE /api/v1/admin/plans/:id/offers/:offer_id             -> AdminDeletePlanOffer
POST   /api/v1/admin/plans/:id/offers/:offer_id/replace     -> AdminReplacePlanOffer  (PAY-15 price versioning)
```

Validation rules (ADR §19.7.2 / §19.7.7):
- `code` regex `^[a-z0-9][a-z0-9_-]*$`, 1-40 chars, unique. IMMUTABLE on PATCH.
- `name` 1-100 chars.
- `max_devices` -1 OR 1..1000.
- `max_servers` -1 OR 0..9999.
- `speed_limit_mbps` 0..100000.
- `is_system` NOT settable via API (migration-only). Request body strips.
- `lava_offer_id` optional UUID format; unique across plan_offers when set.
- `periodicity` enum: ONE_TIME | MONTHLY | PERIOD_90_DAYS | PERIOD_180_DAYS | PERIOD_YEAR.
- `currency` enum: USD | EUR | RUB.
- `amount` >= 0.
</interfaces>

<tasks>

<task type="auto">
  <id>03-08-T01</id>
  <name>Write handler/plans_admin.go (13 handlers — read + write + delete + plan-servers + plan-offers + replace-offer)</name>
  <files>server/api/internal/handler/plans_admin.go</files>
  <read_first>
    - docs/ADR-007-lava-sso-rework.md §19.7 (all subsections — request/response schemas, validation rules, audit log)
    - server/api/internal/repository/plan_repo.go (Wave 2 — all CRUD functions available)
    - server/api/internal/cache/plans_cache.go (T01 of plan 03-07 — BustPlansCache helper)
    - .planning/phases/03-lava-top-plans-catalog/03-CONTEXT.md D-32 §4 (admin abuse: is_system immutable, code immutable, force-delete returns 403 on system plan)
    - .planning/phases/03-lava-top-plans-catalog/03-RESEARCH.md §10.6 (admin-web API surface — request struct shapes — these guide the JSON tags here)
  </read_first>
  <action>
    Create `server/api/internal/handler/plans_admin.go`. Use `validateXxx` helpers for each validation rule so they're reusable across Create + Update + ReplaceOffer. Every write handler calls `cache.BustPlansCache` (best-effort) on success.

    Skeleton (executor fills in the 13 handlers — patterns repeat):

```go
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

// codeRegex enforces plans.code format per ADR §19.7.2.
var codeRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// adminPlanCreateReq is the POST /admin/plans body.
type adminPlanCreateReq struct {
	Code           string                  `json:"code"`
	Name           string                  `json:"name"`
	Description    string                  `json:"description"`
	MaxDevices     int                     `json:"max_devices"`
	MaxServers     int                     `json:"max_servers"`
	SpeedLimitMbps int                     `json:"speed_limit_mbps"`
	SortOrder      int                     `json:"sort_order"`
	ServerIDs      []string                `json:"server_ids"`
	Offers         []adminPlanOfferCreate  `json:"offers"`
	// NOTE: is_system is NOT in this struct — defence in depth (D-32 §4).
}

type adminPlanOfferCreate struct {
	Periodicity string  `json:"periodicity"`
	Currency    string  `json:"currency"`
	Amount      float64 `json:"amount"`
	LavaOfferID *string `json:"lava_offer_id,omitempty"`
}

// adminPlanUpdateReq is the PATCH /admin/plans/:id body. `code` and `is_system`
// are absent — both immutable (ADR §19.7.4 + D-32 §4).
type adminPlanUpdateReq struct {
	Name           *string `json:"name,omitempty"`
	Description    *string `json:"description,omitempty"`
	MaxDevices     *int    `json:"max_devices,omitempty"`
	MaxServers     *int    `json:"max_servers,omitempty"`
	SpeedLimitMbps *int    `json:"speed_limit_mbps,omitempty"`
	SortOrder      *int    `json:"sort_order,omitempty"`
	IsActive       *bool   `json:"is_active,omitempty"`
}

type adminReplaceServersReq struct {
	ServerIDs []string `json:"server_ids"`
}

type adminReplaceOfferReq struct {
	Amount      float64 `json:"amount"`
	LavaOfferID *string `json:"lava_offer_id,omitempty"`
}

// --- Validation helpers ---

func validatePlanCode(s string) error {
	if len(s) == 0 || len(s) > 40 {
		return errors.New("code must be 1-40 chars")
	}
	if !codeRegex.MatchString(s) {
		return errors.New("code must match ^[a-z0-9][a-z0-9_-]*$")
	}
	return nil
}

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

// --- Handlers ---

// AdminListPlans handles GET /admin/plans — returns ALL plans (active + inactive).
// Includes computed fields (server_count, offer_count, active_user_count) per ADR §19.7.1.
func AdminListPlans(logger *zap.Logger, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		plans, err := repository.ListAllPlans(db)
		if err != nil {
			logger.Error("AdminListPlans", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		// Build response with computed counts.
		out := make([]fiber.Map, 0, len(plans))
		for _, p := range plans {
			activeUsers, _ := repository.CountActiveUsersOnPlan(db, p.ID)
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
func AdminCreatePlan(logger *zap.Logger, db *gorm.DB, redisClient *redis.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req adminPlanCreateReq
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
		}
		if err := validatePlanCode(req.Code); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}
		if len(req.Name) == 0 || len(req.Name) > 100 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "name must be 1-100 chars"})
		}
		if req.MaxDevices != -1 && (req.MaxDevices < 1 || req.MaxDevices > 1000) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "max_devices must be -1 or 1..1000"})
		}
		if req.MaxServers != -1 && (req.MaxServers < 0 || req.MaxServers > 9999) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "max_servers must be -1 or 0..9999"})
		}
		if req.SpeedLimitMbps < 0 || req.SpeedLimitMbps > 100000 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "speed_limit_mbps must be 0..100000"})
		}

		// Build plan struct — is_system FORCED to false (D-32 §4 defence in depth).
		plan := &model.Plan{
			Code:           req.Code,
			Name:           req.Name,
			Description:    req.Description,
			MaxDevices:     req.MaxDevices,
			MaxServers:     req.MaxServers,
			SpeedLimitMbps: req.SpeedLimitMbps,
			IsActive:       true,
			IsSystem:       false, // forced — repository ALSO forces false
			SortOrder:      req.SortOrder,
		}

		// Whole-thing in one tx so plan_servers + plan_offers all-or-nothing.
		err := db.Transaction(func(tx *gorm.DB) error {
			if err := repository.CreatePlan(tx, plan); err != nil {
				return err
			}
			for _, sid := range req.ServerIDs {
				if err := repository.AddPlanServer(tx, plan.ID, sid); err != nil {
					return err
				}
			}
			for _, of := range req.Offers {
				if err := validateOfferTuple(of.Periodicity, of.Currency, of.Amount); err != nil {
					return err
				}
				offer := &model.PlanOffer{
					PlanID:      plan.ID,
					Periodicity: of.Periodicity,
					Currency:    of.Currency,
					Amount:      of.Amount,
					LavaOfferID: of.LavaOfferID,
					IsActive:    true,
				}
				if err := repository.CreatePlanOffer(tx, offer); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			logger.Error("AdminCreatePlan", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "create plan failed"})
		}

		_ = cache.BustPlansCache(c.Context(), redisClient)
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{"data": plan})
	}
}

// AdminGetPlan handles GET /admin/plans/:id — full detail with servers + offers.
func AdminGetPlan(logger *zap.Logger, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id := c.Params("id")
		plan, err := repository.FindPlanByID(db, id)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "plan not found"})
			}
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		servers, _ := repository.ListPlanServersJoined(db, id)
		offers, _ := repository.ListOffersForPlan(db, id)
		activeUsers, _ := repository.CountActiveUsersOnPlan(db, id)
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
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "name 1-100 chars"})
			}
			updates["name"] = *req.Name
		}
		if req.Description != nil {
			updates["description"] = *req.Description
		}
		if req.MaxDevices != nil {
			if *req.MaxDevices != -1 && (*req.MaxDevices < 1 || *req.MaxDevices > 1000) {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "max_devices -1 or 1..1000"})
			}
			updates["max_devices"] = *req.MaxDevices
		}
		if req.MaxServers != nil {
			if *req.MaxServers != -1 && (*req.MaxServers < 0 || *req.MaxServers > 9999) {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "max_servers -1 or 0..9999"})
			}
			updates["max_servers"] = *req.MaxServers
		}
		if req.SpeedLimitMbps != nil {
			if *req.SpeedLimitMbps < 0 || *req.SpeedLimitMbps > 100000 {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "speed_limit_mbps 0..100000"})
			}
			updates["speed_limit_mbps"] = *req.SpeedLimitMbps
		}
		if req.SortOrder != nil {
			updates["sort_order"] = *req.SortOrder
		}
		if req.IsActive != nil {
			// 403 if attempting to deactivate the system plan (ADR §19.7.4).
			plan, _ := repository.FindPlanByID(db, id)
			if plan != nil && plan.IsSystem && !*req.IsActive {
				return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "cannot deactivate system plan"})
			}
			updates["is_active"] = *req.IsActive
		}

		updated, err := repository.UpdatePlan(db, id, updates)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "plan not found"})
			}
			logger.Error("AdminUpdatePlan", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		_ = cache.BustPlansCache(c.Context(), redisClient)
		return c.JSON(fiber.Map{"data": updated})
	}
}

// AdminDeletePlan handles DELETE /admin/plans/:id.
// Soft delete. 403 on system plan even with ?force=true. 409 on active_user_count>0
// without ?force=true.
func AdminDeletePlan(logger *zap.Logger, db *gorm.DB, redisClient *redis.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id := c.Params("id")
		force := c.Query("force") == "true"

		plan, err := repository.FindPlanByID(db, id)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "plan not found"})
			}
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		// D-32 §4: system plan delete returns 403 EVEN WITH ?force=true.
		if plan.IsSystem {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "cannot delete system plan"})
		}

		activeUsers, _ := repository.CountActiveUsersOnPlan(db, id)
		if activeUsers > 0 && !force {
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{
				"error":          "plan has active users — use ?force=true to confirm",
				"affected_users": activeUsers,
			})
		}

		if err := repository.SoftDeletePlan(db, id); err != nil {
			if errors.Is(err, repository.ErrSystemPlan) {
				return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "cannot delete system plan"})
			}
			logger.Error("AdminDeletePlan", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		_ = cache.BustPlansCache(c.Context(), redisClient)
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
func AdminReplacePlanServers(logger *zap.Logger, db *gorm.DB, redisClient *redis.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		planID := c.Params("id")
		var req adminReplaceServersReq
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
		}
		// Verify plan exists.
		if _, err := repository.FindPlanByID(db, planID); err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "plan not found"})
			}
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		// Verify each server_id exists and is_active=true (ADR §19.7.6).
		for _, sid := range req.ServerIDs {
			var n int64
			_ = db.Table("vpn_servers").Where("id = ? AND is_active = ?", sid, true).Count(&n).Error
			if n == 0 {
				return c.Status(fiber.StatusUnprocessableEntity).JSON(fiber.Map{
					"error":     "server not found or inactive",
					"server_id": sid,
				})
			}
		}
		if err := repository.ReplacePlanServers(db, planID, req.ServerIDs); err != nil {
			logger.Error("AdminReplacePlanServers", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		_ = cache.BustPlansCache(c.Context(), redisClient)
		return c.JSON(fiber.Map{"data": fiber.Map{"plan_id": planID, "server_ids": req.ServerIDs}})
	}
}

// AdminAddPlanServer handles POST /admin/plans/:id/servers/:server_id.
// Idempotent — returns 200 if pairing already exists, 201 on insert.
func AdminAddPlanServer(logger *zap.Logger, db *gorm.DB, redisClient *redis.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		planID := c.Params("id")
		serverID := c.Params("server_id")
		// Verify plan + server exist.
		if _, err := repository.FindPlanByID(db, planID); err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "plan not found"})
		}
		var n int64
		_ = db.Table("vpn_servers").Where("id = ? AND is_active = ?", serverID, true).Count(&n).Error
		if n == 0 {
			return c.Status(fiber.StatusUnprocessableEntity).JSON(fiber.Map{"error": "server not found or inactive"})
		}
		if err := repository.AddPlanServer(db, planID, serverID); err != nil {
			logger.Error("AdminAddPlanServer", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		_ = cache.BustPlansCache(c.Context(), redisClient)
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{"data": fiber.Map{"plan_id": planID, "server_id": serverID}})
	}
}

// AdminRemovePlanServer handles DELETE /admin/plans/:id/servers/:server_id.
// Does NOT force-disconnect active users (D-23).
func AdminRemovePlanServer(logger *zap.Logger, db *gorm.DB, redisClient *redis.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		planID := c.Params("id")
		serverID := c.Params("server_id")
		if err := repository.RemovePlanServer(db, planID, serverID); err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "pairing not found"})
			}
			logger.Error("AdminRemovePlanServer", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		_ = cache.BustPlansCache(c.Context(), redisClient)
		return c.SendStatus(fiber.StatusNoContent)
	}
}

// AdminListPlanOffers handles GET /admin/plans/:id/offers.
func AdminListPlanOffers(logger *zap.Logger, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		offers, err := repository.ListOffersForPlan(db, c.Params("id"))
		if err != nil {
			logger.Error("AdminListPlanOffers", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		return c.JSON(fiber.Map{"data": offers})
	}
}

// AdminCreatePlanOffer handles POST /admin/plans/:id/offers.
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
		// Verify plan exists.
		if _, err := repository.FindPlanByID(db, planID); err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "plan not found"})
			}
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
		if err := repository.CreatePlanOffer(db, offer); err != nil {
			// Likely partial-unique violation on (plan, periodicity, currency) WHERE is_active=true.
			// Map to 409.
			logger.Warn("AdminCreatePlanOffer rejected (likely duplicate active)", zap.Error(err))
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{
				"error": "active offer already exists for this (periodicity, currency); use POST .../offers/:offer_id/replace to update price",
			})
		}
		_ = cache.BustPlansCache(c.Context(), redisClient)
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{"data": offer})
	}
}

// AdminUpdatePlanOffer handles PATCH /admin/plans/:id/offers/:offer_id.
// periodicity + currency immutable (ADR §19.7.7).
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
		updated, err := repository.UpdatePlanOffer(db, offerID, updates)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "offer not found"})
			}
			logger.Error("AdminUpdatePlanOffer", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		_ = cache.BustPlansCache(c.Context(), redisClient)
		return c.JSON(fiber.Map{"data": updated})
	}
}

// AdminDeletePlanOffer handles DELETE /admin/plans/:id/offers/:offer_id (soft).
func AdminDeletePlanOffer(logger *zap.Logger, db *gorm.DB, redisClient *redis.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		offerID := c.Params("offer_id")
		if err := repository.DeletePlanOffer(db, offerID); err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "offer not found"})
			}
			logger.Error("AdminDeletePlanOffer", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		_ = cache.BustPlansCache(c.Context(), redisClient)
		return c.SendStatus(fiber.StatusNoContent)
	}
}

// AdminReplacePlanOffer handles POST /admin/plans/:id/offers/:offer_id/replace.
// PAY-15 price versioning — deactivate old + insert new in one tx.
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
		// Load the old offer to inherit periodicity + currency.
		oldOffer, err := repository.UpdatePlanOffer(db, oldOfferID, map[string]interface{}{})
		// Note: UpdatePlanOffer with empty updates returns the current row; if it
		// were a hot find-by-id, we'd use that. The pattern matches plan 03-03.
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "offer not found"})
			}
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
		saved, err := repository.ReplaceOffer(db, oldOfferID, newOffer)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "offer not found"})
			}
			logger.Error("AdminReplacePlanOffer", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		_ = cache.BustPlansCache(c.Context(), redisClient)
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{"data": saved})
	}
}
```

    Run `cd server/api && go build ./internal/handler/...`. Verify all 13 handlers compile.
  </action>
  <acceptance_criteria>
    - File `server/api/internal/handler/plans_admin.go` exists
    - `grep -c "^func Admin" server/api/internal/handler/plans_admin.go` returns 13
    - `grep "AdminCreatePlan\|AdminUpdatePlan\|AdminDeletePlan\|AdminReplacePlanServers\|AdminAddPlanServer\|AdminRemovePlanServer\|AdminCreatePlanOffer\|AdminUpdatePlanOffer\|AdminDeletePlanOffer\|AdminReplacePlanOffer" server/api/internal/handler/plans_admin.go` finds 10 matches
    - `grep "cache.BustPlansCache" server/api/internal/handler/plans_admin.go` finds at least 8 matches (every write handler busts)
    - `grep "fiber.StatusForbidden\|StatusForbidden" server/api/internal/handler/plans_admin.go` finds at least 2 matches (system plan delete + system plan deactivate)
    - `grep "cannot delete system plan\|cannot deactivate system plan" server/api/internal/handler/plans_admin.go` finds at least 2 matches
    - `grep "is_system" server/api/internal/handler/plans_admin.go` returns 0 hits in struct definitions (defence in depth — body never accepts is_system)
    - `cd server/api && go build ./internal/handler/...` exits 0
  </acceptance_criteria>
  <automated>cd server/api && go build ./internal/handler/...</automated>
  <done>13 admin plan handlers compile; every write handler busts cache; is_system never accepted from body; system plan delete/deactivate forbidden with 403.</done>
</task>

<task type="auto">
  <id>03-08-T02</id>
  <name>Write plans_admin_test.go — 3 PAY-evidence tests (PAY-13, PAY-14, PAY-15) + system plan guards + audit-log integration</name>
  <files>server/api/internal/handler/plans_admin_test.go</files>
  <read_first>
    - server/api/internal/handler/plans_admin.go (T01 of THIS plan — handler signatures)
    - server/api/internal/handler/payment_test.go (T02 of plan 03-05 — sqlite setup pattern)
    - .planning/phases/03-lava-top-plans-catalog/03-VALIDATION.md PAY-13 (TestAdminPlansCRUD), PAY-14 (TestAdminPlanServers), PAY-15 (TestAdminReplaceOffer_Transactional)
  </read_first>
  <action>
    Create `server/api/internal/handler/plans_admin_test.go` with the 3 PAY-evidence tests required by 03-VALIDATION.md plus additional defence-in-depth tests:
    - `TestAdminPlansCRUD` (PAY-13) — covers create + get + patch + delete via subtests; specifically verifies that is_system is NOT settable via API + DELETE on system plan returns 403 even with ?force=true.
    - `TestAdminPlanServers` (PAY-14) — covers PUT replace + POST add + DELETE remove; validates server existence + idempotent re-add.
    - `TestAdminReplaceOffer_Transactional` (PAY-15) — verifies old offer is_active=false AND new offer is_active=true after one call; both visible in same tx.
    - `TestAdminCreatePlan_RejectsCodeRegexFailure` — code "Pro!" fails 400.
    - `TestAdminCreatePlan_DoesNotAcceptIsSystemFromBody` — even if a request body includes `"is_system":true`, the saved row has is_system=false (the struct doesn't have the field; the test confirms a JSON with extra is_system field is ignored).

    Skeleton (executor expands):

```go
package handler

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"vpnapp/server/api/internal/model"

	"github.com/alicebob/miniredis/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupAdminPlansDB(t *testing.T) (*gorm.DB, *redis.Client) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	stmts := []string{ /* same as setupPublicPlansDB in plans_public_test.go */ }
	_ = stmts // executor: copy CREATE TABLEs for plans, plan_servers, plan_offers, vpn_servers, users
	for _, s := range stmts {
		_ = db.Exec(s).Error
	}
	mr, _ := miniredis.Run()
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return db, rdb
}

func mkAdminPlansApp(t *testing.T, db *gorm.DB, rdb *redis.Client) *fiber.App {
	logger := zap.NewNop()
	app := fiber.New()
	app.Get("/api/v1/admin/plans", AdminListPlans(logger, db))
	app.Post("/api/v1/admin/plans", AdminCreatePlan(logger, db, rdb))
	app.Get("/api/v1/admin/plans/:id", AdminGetPlan(logger, db))
	app.Patch("/api/v1/admin/plans/:id", AdminUpdatePlan(logger, db, rdb))
	app.Delete("/api/v1/admin/plans/:id", AdminDeletePlan(logger, db, rdb))
	app.Put("/api/v1/admin/plans/:id/servers", AdminReplacePlanServers(logger, db, rdb))
	app.Post("/api/v1/admin/plans/:id/servers/:server_id", AdminAddPlanServer(logger, db, rdb))
	app.Delete("/api/v1/admin/plans/:id/servers/:server_id", AdminRemovePlanServer(logger, db, rdb))
	app.Get("/api/v1/admin/plans/:id/offers", AdminListPlanOffers(logger, db))
	app.Post("/api/v1/admin/plans/:id/offers", AdminCreatePlanOffer(logger, db, rdb))
	app.Patch("/api/v1/admin/plans/:id/offers/:offer_id", AdminUpdatePlanOffer(logger, db, rdb))
	app.Delete("/api/v1/admin/plans/:id/offers/:offer_id", AdminDeletePlanOffer(logger, db, rdb))
	app.Post("/api/v1/admin/plans/:id/offers/:offer_id/replace", AdminReplacePlanOffer(logger, db, rdb))
	return app
}

// TestAdminPlansCRUD is the PAY-13 named test from 03-VALIDATION.md.
// Subtests: Create, Get, Patch, Delete-with-force, Delete-system-403, IsSystem-immutable.
func TestAdminPlansCRUD(t *testing.T) {
	db, rdb := setupAdminPlansDB(t)
	app := mkAdminPlansApp(t, db, rdb)

	// Seed a system plan (free) and a non-system server.
	freeID := uuid.NewString()
	_ = db.Create(&model.Plan{ID: freeID, Code: "free", Name: "Free", MaxDevices: 1, MaxServers: 3, IsActive: true, IsSystem: true}).Error

	t.Run("Create", func(t *testing.T) {
		body := `{"code":"trial","name":"Trial","max_devices":2,"max_servers":5,"speed_limit_mbps":10,"server_ids":[]}`
		req := httptest.NewRequest("POST", "/api/v1/admin/plans", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, _ := app.Test(req)
		if resp.StatusCode != 201 {
			buf := new(bytes.Buffer)
			_, _ = buf.ReadFrom(resp.Body)
			t.Fatalf("expected 201, got %d body=%s", resp.StatusCode, buf.String())
		}
	})

	t.Run("Delete_System_403_EvenWithForce", func(t *testing.T) {
		req := httptest.NewRequest("DELETE", "/api/v1/admin/plans/"+freeID+"?force=true", nil)
		resp, _ := app.Test(req)
		if resp.StatusCode != 403 {
			t.Errorf("D-32 §4: system plan delete with ?force=true must return 403, got %d", resp.StatusCode)
		}
	})

	t.Run("IsSystem_Immutable_FromBody", func(t *testing.T) {
		// Body includes is_system:true — must be IGNORED (struct doesn't have field).
		body := `{"code":"another","name":"Another","max_devices":1,"max_servers":1,"speed_limit_mbps":0,"is_system":true}`
		req := httptest.NewRequest("POST", "/api/v1/admin/plans", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, _ := app.Test(req)
		if resp.StatusCode != 201 {
			t.Fatalf("expected 201, got %d", resp.StatusCode)
		}
		// Reload from DB — confirm is_system stayed false.
		var p model.Plan
		_ = db.Where("code = ?", "another").First(&p).Error
		if p.IsSystem {
			t.Errorf("D-32 §4: is_system MUST NOT be settable via API; got is_system=true on 'another'")
		}
	})

	// executor: add Get, Patch, Delete-force-true subtests using the same pattern
}

// TestAdminPlanServers is the PAY-14 named test from 03-VALIDATION.md.
func TestAdminPlanServers(t *testing.T) {
	db, rdb := setupAdminPlansDB(t)
	app := mkAdminPlansApp(t, db, rdb)
	planID := uuid.NewString()
	_ = db.Create(&model.Plan{ID: planID, Code: "pro", Name: "Pro", MaxDevices: 3, MaxServers: -1, IsActive: true}).Error
	s1 := uuid.NewString()
	_ = db.Create(&model.VPNServer{ID: s1, Hostname: "s1", IsActive: true}).Error

	t.Run("Add_201", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/admin/plans/"+planID+"/servers/"+s1, nil)
		resp, _ := app.Test(req)
		if resp.StatusCode != 201 {
			t.Errorf("expected 201, got %d", resp.StatusCode)
		}
	})

	t.Run("Add_Idempotent_StillOK", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/admin/plans/"+planID+"/servers/"+s1, nil)
		resp, _ := app.Test(req)
		if resp.StatusCode != 201 {
			t.Errorf("ADR §19.7.6: idempotent — expected 201 on re-add, got %d", resp.StatusCode)
		}
	})

	t.Run("Add_InactiveServer_422", func(t *testing.T) {
		inactive := uuid.NewString()
		_ = db.Create(&model.VPNServer{ID: inactive, Hostname: "down", IsActive: false}).Error
		req := httptest.NewRequest("POST", "/api/v1/admin/plans/"+planID+"/servers/"+inactive, nil)
		resp, _ := app.Test(req)
		if resp.StatusCode != 422 {
			t.Errorf("expected 422 on inactive server, got %d", resp.StatusCode)
		}
	})

	t.Run("Remove_204", func(t *testing.T) {
		req := httptest.NewRequest("DELETE", "/api/v1/admin/plans/"+planID+"/servers/"+s1, nil)
		resp, _ := app.Test(req)
		if resp.StatusCode != 204 {
			t.Errorf("expected 204, got %d", resp.StatusCode)
		}
	})
}

// TestAdminReplaceOffer_Transactional is the PAY-15 named test from 03-VALIDATION.md.
func TestAdminReplaceOffer_Transactional(t *testing.T) {
	db, rdb := setupAdminPlansDB(t)
	app := mkAdminPlansApp(t, db, rdb)
	planID := uuid.NewString()
	_ = db.Create(&model.Plan{ID: planID, Code: "pro", Name: "Pro", MaxDevices: 3, MaxServers: -1, IsActive: true}).Error
	oldOfferID := uuid.NewString()
	lavaOff := "lava-old"
	_ = db.Create(&model.PlanOffer{ID: oldOfferID, PlanID: planID, Periodicity: "MONTHLY", Currency: "USD", Amount: 3.99, IsActive: true, LavaOfferID: &lavaOff}).Error

	body := `{"amount":5.99,"lava_offer_id":"lava-new"}`
	req := httptest.NewRequest("POST", "/api/v1/admin/plans/"+planID+"/offers/"+oldOfferID+"/replace", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	if resp.StatusCode != 201 {
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(resp.Body)
		t.Fatalf("expected 201, got %d body=%s", resp.StatusCode, buf.String())
	}
	// Old offer must be is_active=false; new offer must exist with amount=5.99 + is_active=true.
	var old model.PlanOffer
	_ = db.Where("id = ?", oldOfferID).First(&old).Error
	if old.IsActive {
		t.Errorf("PAY-15: old offer must be is_active=false after replace")
	}
	var newOffers []model.PlanOffer
	_ = db.Where("plan_id = ? AND is_active = ?", planID, true).Find(&newOffers).Error
	if len(newOffers) != 1 {
		t.Fatalf("expected exactly 1 active offer after replace, got %d", len(newOffers))
	}
	if newOffers[0].Amount != 5.99 {
		t.Errorf("expected new amount=5.99, got %v", newOffers[0].Amount)
	}
}

// TestAdminCreatePlan_RejectsCodeRegexFailure verifies the regex validation.
func TestAdminCreatePlan_RejectsCodeRegexFailure(t *testing.T) {
	db, rdb := setupAdminPlansDB(t)
	app := mkAdminPlansApp(t, db, rdb)
	body := `{"code":"Pro!Invalid","name":"X","max_devices":1,"max_servers":1,"speed_limit_mbps":0}`
	req := httptest.NewRequest("POST", "/api/v1/admin/plans", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	if resp.StatusCode != 400 {
		t.Errorf("expected 400 on regex failure, got %d", resp.StatusCode)
	}
	body2, _ := json.Marshal(map[string]interface{}{
		"code": strings.Repeat("a", 41), "name": "X", "max_devices": 1, "max_servers": 1, "speed_limit_mbps": 0,
	})
	req2 := httptest.NewRequest("POST", "/api/v1/admin/plans", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	resp2, _ := app.Test(req2)
	if resp2.StatusCode != 400 {
		t.Errorf("expected 400 on 41-char code, got %d", resp2.StatusCode)
	}
}
```

    Run `cd server/api && go test ./internal/handler/ -run "TestAdminPlans|TestAdminPlanServers|TestAdminReplaceOffer|TestAdminCreatePlan" -count=1 -timeout=60s -v`.
  </action>
  <acceptance_criteria>
    - File `server/api/internal/handler/plans_admin_test.go` exists
    - `grep "TestAdminPlansCRUD" server/api/internal/handler/plans_admin_test.go` finds one match (PAY-13)
    - `grep "TestAdminPlanServers" server/api/internal/handler/plans_admin_test.go` finds one match (PAY-14)
    - `grep "TestAdminReplaceOffer_Transactional" server/api/internal/handler/plans_admin_test.go` finds one match (PAY-15)
    - `grep "Delete_System_403_EvenWithForce\|IsSystem_Immutable_FromBody" server/api/internal/handler/plans_admin_test.go` finds at least 2 matches (D-32 §4 evidence)
    - `cd server/api && go test ./internal/handler/ -run "TestAdminPlans|TestAdminPlanServers|TestAdminReplaceOffer|TestAdminCreatePlan" -count=1 -timeout=60s` exits 0
  </acceptance_criteria>
  <automated>cd server/api && go test ./internal/handler/ -run "TestAdminPlans|TestAdminPlanServers|TestAdminReplaceOffer|TestAdminCreatePlan" -count=1 -timeout=60s</automated>
  <done>3 PAY-evidence tests pass; D-32 §4 invariants (is_system immutable, system plan force-delete forbidden) verified.</done>
</task>

<task type="auto">
  <id>03-08-T03</id>
  <name>Extend middleware/audit.go::describeAction with plan-CRUD action names</name>
  <files>server/api/internal/middleware/audit.go</files>
  <read_first>
    - server/api/internal/middleware/audit.go (CURRENT — lines 105-140 describeAction with branches for /admin/users, /admin/servers, /admin/change-password)
    - .planning/phases/03-lava-top-plans-catalog/03-RESEARCH.md §5.3 (current describeAction gap; recommended explicit cases)
  </read_first>
  <action>
    Edit `server/api/internal/middleware/audit.go` — extend `describeAction(method, path)` (currently at lines ~105-140) with explicit cases for the plan-CRUD URLs added in T01.

    Add the following cases inside the existing switch-or-if-else structure (preserve existing cases for /admin/users, /admin/servers, /admin/change-password):

```go
// Plan CRUD (Phase 3 PAY-13/14/15).
case method == "POST"   && strings.HasPrefix(path, "/api/v1/admin/plans") && !strings.Contains(path, "/servers/") && !strings.Contains(path, "/offers"):
    return "create_plan"
case method == "PATCH"  && strings.HasPrefix(path, "/api/v1/admin/plans") && !strings.Contains(path, "/servers") && !strings.Contains(path, "/offers"):
    return "update_plan"
case method == "DELETE" && strings.HasPrefix(path, "/api/v1/admin/plans") && !strings.Contains(path, "/servers") && !strings.Contains(path, "/offers"):
    return "delete_plan"
// Plan-servers sub-resource.
case method == "PUT"    && strings.Contains(path, "/admin/plans/") && strings.HasSuffix(path, "/servers"):
    return "replace_plan_servers"
case method == "POST"   && strings.Contains(path, "/admin/plans/") && strings.Contains(path, "/servers/"):
    return "add_plan_server"
case method == "DELETE" && strings.Contains(path, "/admin/plans/") && strings.Contains(path, "/servers/"):
    return "remove_plan_server"
// Plan-offers sub-resource.
case method == "POST"   && strings.Contains(path, "/admin/plans/") && strings.HasSuffix(path, "/replace"):
    return "replace_plan_offer"
case method == "POST"   && strings.Contains(path, "/admin/plans/") && strings.Contains(path, "/offers"):
    return "create_plan_offer"
case method == "PATCH"  && strings.Contains(path, "/admin/plans/") && strings.Contains(path, "/offers/"):
    return "update_plan_offer"
case method == "DELETE" && strings.Contains(path, "/admin/plans/") && strings.Contains(path, "/offers/"):
    return "delete_plan_offer"
```

    Note: the EXACT shape of describeAction depends on whether the current code uses `switch` with `case ...`/case-clauses or `if-else`. Read the current file BEFORE editing and ADAPT the pattern. The CASE ordering matters: longer/more specific URLs must come BEFORE shorter ones (the /servers and /offers sub-resources before the plain /plans).

    Also extend `/admin/lava/products` (added in plan 03-05 T03):
```go
case method == "GET" && strings.HasSuffix(path, "/admin/lava/products"):
    return "list_lava_products"
```
    (GET typically isn't audited per the existing pattern of "only state-changing" — verify the existing describeAction's GET handling; if GETs aren't audited at all, skip this addition.)

    Run `cd server/api && go build ./...` and `cd server/api && go test ./internal/middleware/...`.
  </action>
  <acceptance_criteria>
    - `grep "create_plan\|update_plan\|delete_plan\|replace_plan_servers\|add_plan_server\|remove_plan_server\|create_plan_offer\|update_plan_offer\|delete_plan_offer\|replace_plan_offer" server/api/internal/middleware/audit.go` finds at least 10 matches
    - `cd server/api && go build ./internal/middleware/...` exits 0
    - `cd server/api && go test ./internal/middleware/... -count=1 -timeout=30s` exits 0
  </acceptance_criteria>
  <automated>cd server/api && go test ./internal/middleware/... -count=1 -timeout=30s</automated>
  <done>describeAction recognises 10 new plan-related action names; audit log rows for plan CRUD have meaningful labels.</done>
</task>

<task type="auto">
  <id>03-08-T04</id>
  <name>Wire all 13 admin plan routes in cmd/main.go</name>
  <files>server/api/cmd/main.go</files>
  <read_first>
    - server/api/cmd/main.go (post-plan 03-07; has admin group already defined ~line 259-281)
    - server/api/internal/handler/plans_admin.go (T01 of THIS plan — 13 handler functions)
    - .planning/phases/03-lava-top-plans-catalog/03-RESEARCH.md §5.2 (route registration order)
  </read_first>
  <action>
    Edit `server/api/cmd/main.go`. Find the existing admin route block (around lines 264-281, after `admin.Get("/change-password", ...)`). Add the 13 new routes immediately AFTER the existing `admin.Get("/lava/products", ...)` line (which was added in plan 03-05 T04).

```go
	// Phase 3 admin plans CRUD (PAY-13/14/15). All routes inherit AuthRequired +
	// AdminRequired + AuditLog from the admin group. Every write handler busts
	// the public /plans cache via cache.BustPlansCache.
	admin.Get("/plans", handler.AdminListPlans(logger, db))
	admin.Post("/plans", handler.AdminCreatePlan(logger, db, redisClient))
	admin.Get("/plans/:id", handler.AdminGetPlan(logger, db))
	admin.Patch("/plans/:id", handler.AdminUpdatePlan(logger, db, redisClient))
	admin.Delete("/plans/:id", handler.AdminDeletePlan(logger, db, redisClient))
	admin.Put("/plans/:id/servers", handler.AdminReplacePlanServers(logger, db, redisClient))
	admin.Post("/plans/:id/servers/:server_id", handler.AdminAddPlanServer(logger, db, redisClient))
	admin.Delete("/plans/:id/servers/:server_id", handler.AdminRemovePlanServer(logger, db, redisClient))
	admin.Get("/plans/:id/offers", handler.AdminListPlanOffers(logger, db))
	admin.Post("/plans/:id/offers", handler.AdminCreatePlanOffer(logger, db, redisClient))
	admin.Patch("/plans/:id/offers/:offer_id", handler.AdminUpdatePlanOffer(logger, db, redisClient))
	admin.Delete("/plans/:id/offers/:offer_id", handler.AdminDeletePlanOffer(logger, db, redisClient))
	admin.Post("/plans/:id/offers/:offer_id/replace", handler.AdminReplacePlanOffer(logger, db, redisClient))
```

    Then `cd server/api && go build ./...` and `cd server/api && go test ./... -count=1 -timeout=300s`.
  </action>
  <acceptance_criteria>
    - `grep -c "admin.\\(Get\\|Post\\|Patch\\|Put\\|Delete\\)(\"/plans" server/api/cmd/main.go` returns at least 13
    - `grep "AdminListPlans\\|AdminCreatePlan\\|AdminGetPlan\\|AdminUpdatePlan\\|AdminDeletePlan\\|AdminReplacePlanServers\\|AdminAddPlanServer\\|AdminRemovePlanServer\\|AdminListPlanOffers\\|AdminCreatePlanOffer\\|AdminUpdatePlanOffer\\|AdminDeletePlanOffer\\|AdminReplacePlanOffer" server/api/cmd/main.go` finds 13 matches
    - `cd server/api && go build ./...` exits 0
    - `cd server/api && go test ./... -count=1 -timeout=300s` exits 0
  </acceptance_criteria>
  <automated>cd server/api && go build ./... && go test ./... -count=1 -timeout=300s</automated>
  <done>13 admin plan routes mounted on the admin group; AuditLog records them with the new describeAction labels; cache busts on every write; full suite green.</done>
</task>

</tasks>

<verification>
- `cd server/api && go build ./...` exits 0
- `cd server/api && go test ./... -count=1 -timeout=300s` exits 0
- 3 PAY-evidence tests pass (`TestAdminPlansCRUD`, `TestAdminPlanServers`, `TestAdminReplaceOffer_Transactional`)
- `grep -c "admin.\\(Get\\|Post\\|Patch\\|Put\\|Delete\\)(\"/plans" server/api/cmd/main.go` returns at least 13
- `grep "is_system" server/api/internal/handler/plans_admin.go` shows is_system never appears in request struct fields (only in response payloads where it's read-only)
</verification>

<must_haves>
truths:
  - "POST /admin/plans creates a plan; is_system is hardcoded to false (request body's is_system field is ignored — struct doesn't include it; D-32 §4)."
  - "DELETE /admin/plans/:id refuses system plans with 403 EVEN WITH ?force=true (D-32 §4)."
  - "PATCH /admin/plans/:id strips code + is_system from updates (immutable per ADR §19.7.4 — handler struct doesn't expose them)."
  - "DELETE /admin/plans/:id?force=true does NOT cascade-delete plan_servers; the soft-delete sets is_active=false on the plan AND its offers (grandfathering per ADR §19.10)."
  - "POST /admin/plans/:id/offers/:offer_id/replace deactivates old + inserts new in one transaction (PAY-15)."
  - "POST /admin/plans/:id/servers/:server_id is idempotent: re-add of an existing pairing returns 201 with the same body (ADR §19.7.6)."
  - "DELETE /admin/plans/:id/servers/:server_id does NOT force-disconnect active users (D-23)."
  - "Every write handler busts cache:plans:public:* via cache.BustPlansCache."
  - "AuditLog middleware records plan-CRUD actions with meaningful labels (create_plan, update_plan, replace_plan_servers, etc.) — defence in depth + operator visibility."
artifacts:
  - path: "server/api/internal/handler/plans_admin.go"
    provides: "13 admin plan-CRUD handlers"
    contains: "AdminReplacePlanOffer"
  - path: "server/api/internal/handler/plans_admin_test.go"
    provides: "PAY-13/14/15 evidence tests + D-32 §4 invariants"
    contains: "Delete_System_403_EvenWithForce"
  - path: "server/api/internal/middleware/audit.go"
    provides: "describeAction recognises plan-CRUD action names"
    contains: "replace_plan_offer"
key_links:
  - from: "server/api/internal/handler/plans_admin.go (every write handler)"
    to: "server/api/internal/cache/plans_cache.go::BustPlansCache"
    via: "cache.BustPlansCache(c.Context(), redisClient) on 2xx response"
    pattern: "cache.BustPlansCache"
  - from: "server/api/internal/handler/plans_admin.go::AdminDeletePlan"
    to: "server/api/internal/repository/plan_repo.go::ErrSystemPlan"
    via: "Repository-layer guard returns ErrSystemPlan; handler maps to 403"
    pattern: "errors.Is\\(err, repository.ErrSystemPlan\\)"
</must_haves>

<threat_model>
## Trust Boundaries
| Boundary | Description |
|----------|-------------|
| Admin browser → /admin/plans/* | Admin JWT + AdminRequired middleware + audit log; request body parsed into structs with explicit field whitelists (is_system never present). |

## STRIDE Threat Register
| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-03-57 | Elevation of Privilege | Admin POST with body `{"code":"root","is_system":true}` | mitigate | adminPlanCreateReq struct has NO is_system field — JSON unmarshal ignores unknown keys. Handler explicitly sets `plan.IsSystem = false`. Repository.CreatePlan ALSO forces false (defence in depth ×2). PAY-13 evidence test `IsSystem_Immutable_FromBody` confirms. |
| T-03-58 | Elevation of Privilege | Admin PATCH with body `{"code":"newcode","is_system":true}` | mitigate | adminPlanUpdateReq struct has NO code OR is_system fields. Repository.UpdatePlan ALSO strips those keys. Code is immutable post-creation (ADR §19.7.4). |
| T-03-59 | Tampering | Admin DELETE on system plan with ?force=true | mitigate | Two-layer check: (1) handler checks `plan.IsSystem` BEFORE calling repository; (2) repository.SoftDeletePlan returns ErrSystemPlan; handler maps to 403. PAY-13 evidence test `Delete_System_403_EvenWithForce` confirms. |
| T-03-60 | Tampering | Code injection via `code` field (SQL injection, regex bypass) | mitigate | `validatePlanCode` enforces regex `^[a-z0-9][a-z0-9_-]*$` BEFORE the DB write. Any payload outside this regex returns 400. GORM parameterises the INSERT — even if regex were bypassed, the value is bound, not interpolated. |
| T-03-61 | Information disclosure | Admin reads other tenants' data | accept | Single-tenant deployment per CLAUDE.md. No multi-tenant boundary to enforce. |
| T-03-62 | Repudiation | Admin denies making a change | mitigate | AuditLog middleware records every write with admin user ID + action label + request body diff (existing middleware, extended in T03). Phase 7 ADMIN-06 surfaces this in the UI. |
| T-03-63 | DoS | Admin issues many CREATE plans rapidly | mitigate | Global per-IP rate limiter (HOTFIX-03) caps. Admin endpoints are not exempt from rate-limiting. |
| T-03-64 | Tampering | Admin POST plan with negative max_devices | mitigate | Validation in handler: max_devices must be -1 or 1..1000. DB-side: no CHECK constraint on max_devices (plans.max_devices INT NOT NULL only); handler validation IS the boundary. |
| T-03-65 | Tampering | Admin AddPlanServer for inactive server | mitigate | Handler checks `vpn_servers.is_active = TRUE` BEFORE calling repository. ADR §19.7.6 requirement enforced. |
| T-03-66 | Elevation of Privilege | Admin sets plan.is_system=true via DB direct write | accept | Out of scope — direct DB access by admins is operational and audited via Postgres-side logging. The PARTIAL UNIQUE INDEX `idx_plans_one_system` would still reject creating a SECOND system plan even with raw SQL. |
| T-03-67 | Tampering | ReplaceOffer races against an in-flight checkout | mitigate | ReplaceOffer is wrapped in a tx (repository.ReplaceOffer). FindActiveOffer in /checkout reads the current state — if it lands BEFORE ReplaceOffer's commit, it gets the old offer; if AFTER, it gets the new one. The user pays the price visible at /checkout time; new sign-ups after the commit see the new price (ADR §19.10 grandfathering). |
| T-03-68 | Tampering | Admin sets lava_offer_id to another offer's lava_offer_id (creating a collision) | mitigate | Migration 019's partial unique index `idx_plan_offers_lava_offer_id ON plan_offers(lava_offer_id) WHERE lava_offer_id IS NOT NULL` rejects the duplicate at the DB layer with constraint violation → handler returns 409. |
| T-03-69 | DoS | Cache bust on every write floods Redis | accept | Bounded by `len([USD,EUR,RUB]) * write_rate`. At admin-write frequencies (~10/day) and Redis MULTI-DEL semantics, the cost is negligible. |

ASVS L2 scoping per D-31 (admin endpoints are payment-adjacent). Controls applied: V2 authentication (AdminRequired re-reads role per request — HOTFIX-02), V4 access control (audit log + role re-check), V5 input validation (regex + length + enum + range), V8 data protection (is_system never accepted from body), V11 business logic (system plan immutable, grandfathering via soft-delete + ReplaceOffer transactional).
</threat_model>

<success_criteria>
1. `cd server/api && go build ./...` exits 0.
2. `cd server/api && go test ./... -count=1 -timeout=300s` exits 0.
3. PAY-13 evidence: `TestAdminPlansCRUD` passes with subtests covering Create, IsSystem_Immutable_FromBody, Delete_System_403_EvenWithForce.
4. PAY-14 evidence: `TestAdminPlanServers` passes with Add/Idempotent/InactiveServer-422/Remove subtests.
5. PAY-15 evidence: `TestAdminReplaceOffer_Transactional` passes — old offer is_active=false AND exactly 1 active offer remains.
6. All 13 admin routes mounted in cmd/main.go on the admin group (inheriting AuditLog).
7. describeAction recognises 10 new action names (create_plan, replace_plan_offer, etc.).
</success_criteria>

<output>
T01..T04 land as 4 atomic commits (`feat(03-08): ...`); planner commits this plan file once with `docs(03): plan admin-plans-crud`.
</output>
