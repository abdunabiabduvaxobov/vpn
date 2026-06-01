package handler

import (
	"vpnapp/server/api/internal/repository"

	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// publicBroadcast is the minimal projection exposed on the PUBLIC broadcasts
// endpoint (T-07-29): only {title, body, severity} leave the trust boundary —
// no id/target_tier/locale/updated_by/created_at, which are admin-side fields.
type publicBroadcast struct {
	Title    string `json:"title"`
	Body     string `json:"body"`
	Severity string `json:"severity"`
}

// ListBroadcastsPublic handles the PUBLIC GET /api/v1/broadcasts. Returns the
// currently-active broadcast banners (starts_at<=now<=ends_at, NULL bounds
// open-ended), newest-first, projected to the minimal client fields. No auth —
// landing + mobile read it (the AppVersion gate skips this route, like /plans).
func ListBroadcastsPublic(logger *zap.Logger, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		msgs, err := repository.ListActiveBroadcasts(c.Context(), db)
		if err != nil {
			logger.Error("public: failed to list active broadcasts", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		out := make([]publicBroadcast, 0, len(msgs))
		for _, m := range msgs {
			out = append(out, publicBroadcast{
				Title:    m.Title,
				Body:     m.Body,
				Severity: m.Severity,
			})
		}
		return c.JSON(fiber.Map{"data": fiber.Map{"broadcasts": out}})
	}
}
