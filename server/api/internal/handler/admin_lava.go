package handler

import (
	"vpnapp/server/api/internal/lava"

	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
)

// lavaProductRow is the flattened admin-dropdown source row (D-12 Option B).
// One row per (product, offer, price) tuple — the admin UI presents these as
// a dropdown so admin selects WITHOUT typing or pasting a UUID.
type lavaProductRow struct {
	ProductID   string  `json:"productId"`
	ProductName string  `json:"productName"`
	OfferID     string  `json:"offerId"`
	OfferName   string  `json:"offerName"`
	Periodicity string  `json:"periodicity"`
	Currency    string  `json:"currency"`
	Amount      float64 `json:"amount"`
}

// AdminListLavaProducts handles GET /api/v1/admin/lava/products (D-12).
//
// Proxies GET /api/v2/products via the server-side lava client (admin
// API key NEVER reaches the browser) and flattens the response into a
// dropdown-friendly array. Admin UI (plan 03-10) calls this on plan-offer
// dialog mount.
//
// Mounted on the admin route group in cmd/main.go — inherits AuthRequired
// + AdminRequired + AuditLog middleware automatically.
func AdminListLavaProducts(logger *zap.Logger, lavaClient *lava.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		products, err := lavaClient.ListProducts(c.Context())
		if err != nil {
			logger.Error("admin: ListLavaProducts failed", zap.Error(err))
			return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{
				"error": "payment provider unavailable",
			})
		}

		// Flatten products × offers × prices into a single dropdown source.
		rows := make([]lavaProductRow, 0, len(products)*2)
		for _, p := range products {
			productName := ""
			if p.Title != nil {
				productName = *p.Title
			}
			for _, offer := range p.Offers {
				for _, price := range offer.Prices {
					rows = append(rows, lavaProductRow{
						ProductID:   p.ID,
						ProductName: productName,
						OfferID:     offer.ID,
						OfferName:   offer.Name,
						Periodicity: price.Periodicity,
						Currency:    price.Currency,
						Amount:      price.Amount,
					})
				}
			}
		}

		return c.JSON(fiber.Map{
			"data": rows,
		})
	}
}
