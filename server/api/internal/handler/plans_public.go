package handler

import (
	"encoding/json"
	"strings"

	"vpnapp/server/api/internal/cache"
	"vpnapp/server/api/internal/repository"

	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// allowedPublicCurrencies enumerates the currencies the public /plans endpoint
// can be queried with. Mirrors the plan_offers.currency CHECK constraint.
var allowedPublicCurrencies = map[string]struct{}{
	"USD": {}, "EUR": {}, "RUB": {},
}

// publicOffer is the trimmed offer shape returned by /api/v1/plans (no id,
// no lava_offer_id, no is_active — those are admin-only).
type publicOffer struct {
	Periodicity string  `json:"periodicity"`
	Currency    string  `json:"currency"`
	Amount      float64 `json:"amount"`
}

// publicPlan is the trimmed plan shape returned by /api/v1/plans.
type publicPlan struct {
	Code            string        `json:"code"`
	Name            string        `json:"name"`
	Description     string        `json:"description"`
	MaxDevices      int           `json:"max_devices"`
	MaxServers      int           `json:"max_servers"`
	SpeedLimitMbps  int           `json:"speed_limit_mbps"`
	IsSystem        bool          `json:"is_system"`
	SortOrder       int           `json:"sort_order"`
	ServerCountries []string      `json:"server_countries"`
	Offers          []publicOffer `json:"offers"`
}

// ListPlansPublic handles GET /api/v1/plans (PUBLIC, no auth).
//
// Query param: ?currency=USD|EUR|RUB (default: derived from Accept-Language —
// RU → RUB else USD per D-27). Invalid currency → 400.
//
// Cache: cache:plans:public:{currency}, TTL 60s. Cache miss or Redis outage
// falls through to a DB-backed build. Admin write handlers (plan 03-08) bust
// the cache on successful mutation.
//
// Excludes admin-only fields per D-27 (id, lava_offer_id, active_user_count).
func ListPlansPublic(logger *zap.Logger, db *gorm.DB, redisClient *redis.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		currency := strings.ToUpper(strings.TrimSpace(c.Query("currency")))
		if currency == "" {
			currency = deriveCurrencyFromAcceptLanguage(c.Get("Accept-Language"))
		}
		if _, ok := allowedPublicCurrencies[currency]; !ok {
			// WR-03: log rejected currencies at Debug so abuse / probing for
			// extra currency support is detectable without spamming Info.
			logger.Debug("/plans: invalid currency rejected",
				zap.String("currency", currency),
				zap.String("accept_language", c.Get("Accept-Language")),
			)
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid currency"})
		}

		// Cache-aside read.
		if cached, _ := cache.GetPlansCache(c.Context(), redisClient, currency); cached != "" {
			c.Set("Content-Type", "application/json")
			return c.SendString(cached)
		}

		// Miss — query DB.
		plans, err := repository.ListActivePlans(db)
		if err != nil {
			logger.Error("/plans: ListActivePlans", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		offers, err := repository.ListActiveOffersForPublic(db)
		if err != nil {
			logger.Error("/plans: ListActiveOffersForPublic", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}

		// Build offers-by-plan-id map (filter to requested currency only).
		offersByPlan := make(map[string][]publicOffer, len(plans))
		for _, o := range offers {
			if o.Currency != currency {
				continue
			}
			offersByPlan[o.PlanID] = append(offersByPlan[o.PlanID], publicOffer{
				Periodicity: o.Periodicity,
				Currency:    o.Currency,
				Amount:      o.Amount,
			})
		}

		out := make([]publicPlan, 0, len(plans))
		for _, p := range plans {
			countries, err := repository.ListPlanServerCountries(db, p.ID)
			if err != nil {
				logger.Warn("/plans: ListPlanServerCountries failed (using empty)",
					zap.String("plan_id", p.ID), zap.Error(err))
				countries = []string{}
			}
			out = append(out, publicPlan{
				Code:            p.Code,
				Name:            p.Name,
				Description:     p.Description,
				MaxDevices:      p.MaxDevices,
				MaxServers:      p.MaxServers,
				SpeedLimitMbps:  p.SpeedLimitMbps,
				IsSystem:        p.IsSystem,
				SortOrder:       p.SortOrder,
				ServerCountries: countries,
				Offers:          offersByPlan[p.ID],
			})
		}

		body, mErr := json.Marshal(fiber.Map{
			"data": fiber.Map{
				"currency": currency,
				"plans":    out,
			},
		})
		if mErr != nil {
			logger.Error("/plans: marshal failed", zap.Error(mErr))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}

		// Best-effort cache write.
		_ = cache.SetPlansCache(c.Context(), redisClient, currency, string(body))

		c.Set("Content-Type", "application/json")
		return c.Send(body)
	}
}

// deriveCurrencyFromAcceptLanguage returns "RUB" for any Accept-Language starting
// with "ru" (case-insensitive), "USD" otherwise. D-27.
func deriveCurrencyFromAcceptLanguage(header string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(header)), "ru") {
		return "RUB"
	}
	return "USD"
}
