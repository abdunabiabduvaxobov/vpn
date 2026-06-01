package handler

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"vpnapp/server/api/internal/model"
	"vpnapp/server/api/internal/repository"

	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// emailRe matches an email address local-part@domain so the LIST view can
// redact buyer PII (T-07-34 / RESEARCH §9.4). It is intentionally permissive
// (it only needs to find the addresses lava puts in the payload's buyer.email),
// not an RFC-5322 validator.
var emailRe = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)

// redactEmails replaces every email address in s with a masked form that keeps
// only the first character of the local-part: alice@example.com -> a***@example.com.
// A single-character local-part is masked to ***@domain so even one-letter
// addresses leak nothing. Applied to the webhook-log LIST payload preview only;
// the audited DETAIL view may show the full payload (§9.4).
func redactEmails(s string) string {
	return emailRe.ReplaceAllStringFunc(s, func(addr string) string {
		at := strings.IndexByte(addr, '@')
		if at <= 0 {
			return addr
		}
		local := addr[:at]
		domain := addr[at:]
		if len(local) <= 1 {
			return "***" + domain
		}
		return local[:1] + "***" + domain
	})
}

// webhookEventListItem is the redacted LIST row for the admin webhook log. The
// payload is rendered as a redacted preview string (buyer emails masked) rather
// than the raw JSONB so the list endpoint never leaks buyer PII (T-07-34).
type webhookEventListItem struct {
	ID             string  `json:"id"`
	EventType      string  `json:"event_type"`
	ContractID     *string `json:"contract_id"`
	Status         string  `json:"status"`
	RetriedCount   int     `json:"retried_count"`
	ReceivedAt     string  `json:"received_at"`
	ProcessedAt    *string `json:"processed_at"`
	Error          *string `json:"error"`
	PayloadPreview string  `json:"payload_preview"`
}

// AdminListWebhookEvents handles GET /admin/webhook-events.
// Query params: page, limit, status, event_type. Returns a paginated, newest-first
// list of lava_webhook_events; each row's payload preview has buyer emails redacted
// (T-07-34 — full payload only on the audited DETAIL view).
func AdminListWebhookEvents(logger *zap.Logger, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		page, limit := parsePagination(c)
		status := c.Query("status")
		eventType := c.Query("event_type")

		events, total, err := repository.ListWebhookEvents(c.Context(), db, status, eventType, page, limit)
		if err != nil {
			logger.Error("admin: failed to list webhook events", zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}

		items := make([]webhookEventListItem, 0, len(events))
		for i := range events {
			ev := events[i]
			var processedAt *string
			if ev.ProcessedAt != nil {
				s := ev.ProcessedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
				processedAt = &s
			}
			items = append(items, webhookEventListItem{
				ID:             ev.ID,
				EventType:      ev.EventType,
				ContractID:     ev.ContractID,
				Status:         ev.Status,
				RetriedCount:   ev.RetriedCount,
				ReceivedAt:     ev.ReceivedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
				ProcessedAt:    processedAt,
				Error:          ev.Error,
				PayloadPreview: redactEmails(string(ev.Payload)),
			})
		}

		return c.JSON(fiber.Map{"data": fiber.Map{
			"events": items,
			"pagination": fiber.Map{
				"page":        page,
				"limit":       limit,
				"total":       total,
				"total_pages": (total + int64(limit) - 1) / int64(limit),
			},
		}})
	}
}

// AdminGetWebhookEvent handles GET /admin/webhook-events/:id.
// Returns the FULL event including the unredacted payload (detail view). This is a
// GET on the audited admin group, so the access itself is recorded (§9.4). 404 if
// the event does not exist.
func AdminGetWebhookEvent(logger *zap.Logger, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id := c.Params("id")
		if id == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "event id required"})
		}
		ev, err := repository.FindWebhookEventByID(c.Context(), db, id)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "webhook event not found"})
			}
			logger.Error("admin: failed to load webhook event", zap.String("event_id", id), zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		// Render the raw payload as parsed JSON so the detail view returns a JSON
		// object rather than an escaped string. Fall back to the raw bytes string
		// if it is not valid JSON (it always should be — it was stored from a
		// parsed body — but never fail the detail view on a forensic edge case).
		var payload json.RawMessage = json.RawMessage(ev.Payload)
		return c.JSON(fiber.Map{"data": fiber.Map{
			"id":            ev.ID,
			"event_type":    ev.EventType,
			"contract_id":   ev.ContractID,
			"invoice_id":    ev.InvoiceID,
			"status":        ev.Status,
			"retried_count": ev.RetriedCount,
			"received_at":   ev.ReceivedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
			"processed_at":  ev.ProcessedAt,
			"error":         ev.Error,
			"payload":       payload,
		}})
	}
}

// replayRequest is the replay endpoint body — the operator's required reason for
// re-applying the stored event (recorded in the replay_webhook audit row).
type replayRequest struct {
	Reason string `json:"reason"`
}

// AdminReplayWebhookEvent handles POST /admin/webhook-events/:id/replay.
// Body: {reason}. Loads the stored event, re-applies its side effect idempotently
// via applyLavaEvent (the SAME transport-free dispatch the live handler runs — so
// it inherits the per-user WithUserLock from 07-05 and cannot race a live webhook
// into a double grant), flips status DELIVERED→REPLAYED, bumps retried_count, and
// writes a replay_webhook audit row carrying the reason. Returns 200
// {data:{outcome, retried_count}}.
//
// Only DELIVERED or FAILED events are replayable. A PENDING (never-dispatched) or
// already-REPLAYED row, or an unknown status, is rejected with 400 (T-07-36) — an
// operator should not "replay" an event whose original dispatch never resolved.
//
// Idempotency (T-07-32): re-applying a payment.success yields the SAME tier and an
// expiry anchored to now+periodicity (set-not-increment), never a second grant.
func AdminReplayWebhookEvent(logger *zap.Logger, db *gorm.DB, redisClient *redis.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id := c.Params("id")
		if id == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "event id required"})
		}
		var req replayRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request body"})
		}
		reason := strings.TrimSpace(req.Reason)
		if reason == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "reason is required"})
		}
		if len(reason) > maxSuspendReasonLen {
			reason = reason[:maxSuspendReasonLen]
		}

		ev, err := repository.FindWebhookEventByID(c.Context(), db, id)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "webhook event not found"})
			}
			logger.Error("admin: failed to load webhook event for replay", zap.String("event_id", id), zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}

		// Only a previously-resolved event may be replayed (T-07-36). PENDING
		// means the original dispatch never completed; REPLAYED-only / unknown
		// statuses are likewise not valid replay sources.
		if ev.Status != "DELIVERED" && ev.Status != "FAILED" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "only DELIVERED or FAILED events can be replayed",
			})
		}

		// Re-apply the stored event idempotently. applyLavaEvent's success path
		// takes the same WithUserLock(user_id) as the live webhook, so a replay
		// racing a live webhook serializes and cannot reopen the ADMIN-03 race.
		if err := applyLavaEvent(c.Context(), db, redisClient, logger, *ev); err != nil {
			logger.Error("admin: webhook replay re-application failed",
				zap.String("event_id", id), zap.String("event_type", ev.EventType), zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "replay failed"})
		}

		// Flip status DELIVERED→REPLAYED and bump retried_count.
		if err := repository.MarkWebhookReplayed(c.Context(), db, id); err != nil {
			logger.Error("admin: failed to mark webhook event replayed",
				zap.String("event_id", id), zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}

		// Explicit audit row carrying the operator's reason (T-07-35). The
		// AuditLog middleware also records an outer method+path row; this entry is
		// what carries Details["reason"]. Best-effort — a persistence failure is
		// logged but does not undo the (already-committed) replay.
		writeUserControlAudit(c, logger, db, "replay_webhook", id, model.AuditDetails{
			"reason":     reason,
			"event_type": ev.EventType,
		})

		// Re-read for the response so retried_count reflects the bump.
		newCount := ev.RetriedCount + 1
		if updated, ferr := repository.FindWebhookEventByID(c.Context(), db, id); ferr == nil {
			newCount = updated.RetriedCount
		}

		logger.Info("admin: replayed webhook event",
			zap.String("event_id", id), zap.String("event_type", ev.EventType), zap.Int("retried_count", newCount))

		return c.JSON(fiber.Map{"data": fiber.Map{
			"outcome":       "REPLAYED",
			"retried_count": newCount,
		}})
	}
}
