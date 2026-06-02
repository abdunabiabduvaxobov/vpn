package handler

import (
	"errors"
	"strings"
	"time"

	"vpnapp/server/api/internal/cache"
	"vpnapp/server/api/internal/model"
	"vpnapp/server/api/internal/repository"

	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// maxSuspendReasonLen caps the operator's free-text justification before it is
// persisted into audit_log.details (JSONB). The cap is a tampering / abuse
// guard (T-07-16) — a bounded, parameterized value, never concatenated into SQL.
const maxSuspendReasonLen = 500

// disconnectThrottleWindow is the per-user force-disconnect throttle window.
// IncrRateLimit caps the action at one success per user per window; a second
// call inside the window returns 429 (T-07-14 — blast-radius / double-click DoS).
const disconnectThrottleWindow = 30 * time.Second

// reasonRequest is the shared body shape for the reason-carrying mutations
// (suspend / unsuspend / disconnect). The reason is trimmed, length-capped, and
// written into audit_log.details by the handler itself because the AuditLog
// middleware records only method+path+params, not the body (RESEARCH Pitfall 4 /
// T-07-15: a suspend without a recorded justification is a repudiation risk).
type reasonRequest struct {
	Reason string `json:"reason"`
}

// parseReason extracts, trims, and length-caps the reason from the request body.
// Returns ("", false) when the body is unparseable or the reason is empty after
// trimming — the caller then returns 400.
func parseReason(c *fiber.Ctx) (string, bool) {
	var req reasonRequest
	if err := c.BodyParser(&req); err != nil {
		return "", false
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		return "", false
	}
	if len(reason) > maxSuspendReasonLen {
		reason = reason[:maxSuspendReasonLen]
	}
	return reason, true
}

// AdminSuspendUser handles POST /admin/users/:id/suspend.
// Body: {reason}. Sets users.suspended_at=now()+suspended_reason, revokes every
// refresh session (DeleteUserSessions), busts the user:<id> entitlement cache,
// and writes an explicit audit row carrying the reason in Details. Returns
// 200 {data:{id, suspended_at}}.
//
// T-07-13 (suspended user keeps access): the three steps together close the
// window — DeleteUserSessions kills the refresh path, BustUserCache drops the
// stale existence cache (5s TTL backstop), and SuspendedRequired's per-request
// DB read of suspended_at is the hard gate on the next protected request.
func AdminSuspendUser(logger *zap.Logger, db *gorm.DB, redisClient *redis.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID := c.Params("id")
		if userID == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "user id required"})
		}
		reason, ok := parseReason(c)
		if !ok {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "reason is required"})
		}

		now := time.Now()
		updates := map[string]interface{}{
			"suspended_at":     now,
			"suspended_reason": reason,
		}
		if err := repository.UpdateUser(c.Context(), db, userID, updates); err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "user not found"})
			}
			logger.Error("admin: failed to suspend user", zap.String("user_id", userID), zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}

		// Revoke every refresh session so the user cannot mint a new access
		// token after suspension. Best-effort count for logging; a failure here
		// is logged but does not roll back the suspension (suspended_at is the
		// authority bit and SuspendedRequired enforces it regardless).
		revoked, err := repository.DeleteUserSessions(c.Context(), db, userID)
		if err != nil {
			logger.Warn("admin: failed to revoke sessions on suspend",
				zap.String("user_id", userID), zap.Error(err))
		}

		// Bust user:<id> so the suspension is reflected on the next AuthRequired
		// pass. Best-effort (5s TTL is the backstop) — mirror AdminUpdateUser.
		if err := cache.BustUserCache(c.Context(), redisClient, userID); err != nil {
			logger.Warn("admin: BustUserCache failed on suspend (5s TTL is the backstop)",
				zap.String("user_id", userID), zap.Error(err))
		}

		// Explicit audit row carrying the reason (Pitfall 4 / T-07-15). The
		// AuditLog middleware also records an outer method+path row; that
		// redundancy is acceptable — this entry is what carries Details["reason"].
		writeUserControlAudit(c, logger, db, "suspend_user", userID, model.AuditDetails{"reason": reason})

		logger.Info("admin: suspended user",
			zap.String("user_id", userID), zap.Int64("sessions_revoked", revoked))

		return c.JSON(fiber.Map{"data": fiber.Map{
			"id":           userID,
			"suspended_at": now.UTC().Format(time.RFC3339),
		}})
	}
}

// AdminUnsuspendUser handles POST /admin/users/:id/unsuspend.
// Body: {reason}. Clears suspended_at/suspended_reason, busts the cache, and
// audits the action. Returns 200 {data:{id}}.
func AdminUnsuspendUser(logger *zap.Logger, db *gorm.DB, redisClient *redis.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID := c.Params("id")
		if userID == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "user id required"})
		}
		reason, ok := parseReason(c)
		if !ok {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "reason is required"})
		}

		updates := map[string]interface{}{
			"suspended_at":     nil,
			"suspended_reason": nil,
		}
		if err := repository.UpdateUser(c.Context(), db, userID, updates); err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "user not found"})
			}
			logger.Error("admin: failed to unsuspend user", zap.String("user_id", userID), zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}

		if err := cache.BustUserCache(c.Context(), redisClient, userID); err != nil {
			logger.Warn("admin: BustUserCache failed on unsuspend (5s TTL is the backstop)",
				zap.String("user_id", userID), zap.Error(err))
		}

		writeUserControlAudit(c, logger, db, "unsuspend_user", userID, model.AuditDetails{"reason": reason})

		logger.Info("admin: unsuspended user", zap.String("user_id", userID))

		return c.JSON(fiber.Map{"data": fiber.Map{"id": userID}})
	}
}

// AdminDisconnectUser handles POST /admin/users/:id/disconnect.
// Body: {reason}. Throttled to ≤1/user/30s (429 on the second call inside the
// window, T-07-14). Marks every live connection of the user as disconnected
// (Option-B — live tunnels die on the stale sweep), audits with the reason and
// the killed count. Returns 200 {data:{killed_count}}.
func AdminDisconnectUser(logger *zap.Logger, db *gorm.DB, redisClient *redis.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID := c.Params("id")
		if userID == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "user id required"})
		}
		reason, ok := parseReason(c)
		if !ok {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "reason is required"})
		}

		// Throttle BEFORE doing the work (T-07-14). IncrRateLimit is an atomic
		// Lua INCR+EXPIRE; the first call returns 1, a second within the window
		// returns 2 → 429. Skipped only when Redis is unconfigured (nil) — in
		// production redisClient is always set; the nil branch exists for tests
		// that do not exercise the throttle path.
		if redisClient != nil {
			count, err := cache.IncrRateLimit(c.Context(), redisClient, "disconnect:user:"+userID, disconnectThrottleWindow)
			if err != nil {
				logger.Error("admin: disconnect throttle check failed",
					zap.String("user_id", userID), zap.Error(err))
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
			}
			if count > 1 {
				return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
					"error": "force-disconnect throttled — try again shortly",
				})
			}
		}

		killed, err := repository.DisconnectConnectionsByUser(c.Context(), db, userID)
		if err != nil {
			logger.Error("admin: failed to disconnect user connections",
				zap.String("user_id", userID), zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}

		writeUserControlAudit(c, logger, db, "disconnect_user", userID, model.AuditDetails{
			"reason":       reason,
			"killed_count": killed,
		})

		logger.Info("admin: force-disconnected user",
			zap.String("user_id", userID), zap.Int64("killed_count", killed))

		return c.JSON(fiber.Map{"data": fiber.Map{"killed_count": killed}})
	}
}

// AdminGetUserAuditLog handles GET /admin/users/:id/audit-log.
// Paginated read of audit_log rows whose target_id matches the user.
func AdminGetUserAuditLog(logger *zap.Logger, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID := c.Params("id")
		if userID == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "user id required"})
		}
		page, limit := parsePagination(c)
		entries, total, err := repository.ListAuditEntriesByTarget(c.Context(), db, userID, page, limit)
		if err != nil {
			logger.Error("admin: failed to list user audit log",
				zap.String("user_id", userID), zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		return c.JSON(fiber.Map{"data": fiber.Map{
			"entries": entries,
			"pagination": fiber.Map{
				"page":        page,
				"limit":       limit,
				"total":       total,
				"total_pages": (total + int64(limit) - 1) / int64(limit),
			},
		}})
	}
}

// adminUserSessionResponse is the read-only session view for the admin panel.
// The refresh-token hash is deliberately NOT serialized.
type adminUserSessionResponse struct {
	ID         string `json:"id"`
	DeviceInfo string `json:"device_info"`
	CreatedAt  string `json:"created_at"`
	ExpiresAt  string `json:"expires_at"`
}

// AdminListUserSessions handles GET /admin/users/:id/sessions.
// Returns the user's active refresh sessions (id, device_info, created_at,
// expires_at) — never the refresh_token_hash.
func AdminListUserSessions(logger *zap.Logger, db *gorm.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID := c.Params("id")
		if userID == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "user id required"})
		}
		sessions, err := repository.ListSessionsByUser(c.Context(), db, userID)
		if err != nil {
			logger.Error("admin: failed to list user sessions",
				zap.String("user_id", userID), zap.Error(err))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}
		out := make([]adminUserSessionResponse, 0, len(sessions))
		for _, s := range sessions {
			out = append(out, adminUserSessionResponse{
				ID:         s.ID,
				DeviceInfo: s.DeviceInfo,
				CreatedAt:  s.CreatedAt.UTC().Format(time.RFC3339),
				ExpiresAt:  s.ExpiresAt.UTC().Format(time.RFC3339),
			})
		}
		return c.JSON(fiber.Map{"data": out})
	}
}

// cancelSubscriptionRequest is the force-cancel body. refund records ONLY a
// local refund INTENT (A3 — internal/lava/ has no refund method, so no lava
// refund endpoint is called); reason is the operator's required justification.
type cancelSubscriptionRequest struct {
	Refund bool   `json:"refund"`
	Reason string `json:"reason"`
}

// errAlreadyCancelled is the sentinel returned from inside the WithUserLock
// closure when the user's subscription is already cancelled, so the handler can
// respond 409 without performing a (duplicate) cancel. It is NOT a DB error —
// the transaction commits with no writes.
var errAlreadyCancelled = errors.New("subscription already cancelled")

// AdminCancelSubscription handles POST /admin/users/:id/cancel-subscription.
// Body: {refund bool, reason string}. This is the ADMIN-03 force-cancel: it
// resets the user to the system (free) plan and marks their active lava_contract
// cancelled, all inside repository.WithUserLock keyed on the user_id — the SAME
// key the lava webhook tier-grant path takes. The advisory lock serializes the
// two writers so a force-cancel racing a payment.success can never leave a
// hybrid state (tier=free with an active contract, or tier=pro with a cancelled
// contract); the final state is always one of the two consistent outcomes
// (proven by integration.TestForceCancelWebhookRace).
//
// A3 (LOCKED): refund=true records refund INTENT in the audit row only — there
// is NO lava refund method and none is called. already-cancelled → 409 (no
// double-cancel). The action is audited with the reason + refund_intent.
func AdminCancelSubscription(logger *zap.Logger, db *gorm.DB, redisClient *redis.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID := c.Params("id")
		if userID == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "user id required"})
		}
		var req cancelSubscriptionRequest
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
		adminID, _ := c.Locals("user_id").(string)

		var cancelledAt time.Time
		var subscriptionID string

		// All state reads + writes happen inside the advisory lock so the
		// re-read of current state and the downgrade are serialized against a
		// concurrent webhook tier-grant on the same user (ADMIN-03 / T-07-18).
		lockErr := repository.WithUserLock(c.Context(), db, userID, func(tx *gorm.DB) error {
			// 1. Re-read the user ON tx so we see the latest committed state
			//    (a webhook that just granted Pro is now visible / serialized).
			var u model.User
			if err := tx.Where("id = ?", userID).First(&u).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return repository.ErrNotFound
				}
				return err
			}

			// 2. Find the user's active lava contract (if any). already-cancelled
			//    means there is no active contract AND the user is already on a
			//    non-paid tier — respond 409 (no double-cancel).
			var contract model.LavaContract
			contractErr := tx.Where("user_id = ? AND is_active = ?", userID, true).
				Order("started_at DESC").First(&contract).Error
			hasActiveContract := contractErr == nil
			if contractErr != nil && !errors.Is(contractErr, gorm.ErrRecordNotFound) {
				return contractErr
			}
			if !hasActiveContract {
				// No active contract to cancel. If the user is already on the
				// system/free plan there is nothing to do → 409.
				return errAlreadyCancelled
			}

			// 3. Reset the user to the system (free) plan on tx (mirror
			//    SetUserPlanTx with the system plan id, no contract, nil expiry).
			var systemPlan model.Plan
			if err := tx.Where("is_system = ?", true).First(&systemPlan).Error; err != nil {
				return err
			}
			if err := repository.SetUserPlanTx(tx, userID, systemPlan.ID, nil, nil); err != nil {
				return err
			}

			// 3b. Revoke the user's per-user VLESS UUIDs (HARD-02 wire
			//     enforcement). A Pro->free force-cancel is a plan change, so the
			//     premium UUID must stop being admitted at the wire — this mirrors
			//     the AdminUpdateUser tier-change path (which rotates on
			//     tierChanged). Without it, a cancelled user keeps premium-server
			//     wire access until an unrelated tunnel reload (v2.2.0 audit gap).
			if err := repository.RevokeAllVlessUUIDs(c.Context(), tx, userID); err != nil {
				return err
			}

			// 4. Mark the active contract cancelled.
			now := time.Now()
			if err := tx.Model(&model.LavaContract{}).
				Where("id = ?", contract.ID).
				Updates(map[string]interface{}{
					"is_active":    false,
					"cancelled_at": &now,
				}).Error; err != nil {
				return err
			}
			cancelledAt = now
			subscriptionID = contract.ContractID

			// 5. Audit the force-cancel ON tx so the row is part of the same
			//    serialized transaction (reason + refund INTENT — A3).
			if adminID != "" {
				t := userID
				entry := &model.AuditLogEntry{
					AdminID:  adminID,
					Action:   "cancel_subscription",
					TargetID: &t,
					Details: model.AuditDetails{
						"reason":        reason,
						"refund_intent": req.Refund,
					},
					IP: c.IP(),
				}
				if err := repository.CreateAuditEntry(c.Context(), tx, entry); err != nil {
					return err
				}
			}
			return nil
		})

		if lockErr != nil {
			if errors.Is(lockErr, errAlreadyCancelled) {
				return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": "subscription already cancelled"})
			}
			if errors.Is(lockErr, repository.ErrNotFound) {
				return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "user not found"})
			}
			logger.Error("admin: failed to force-cancel subscription",
				zap.String("user_id", userID), zap.Error(lockErr))
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
		}

		// After the lock commits: bust the entitlement cache so the downgrade is
		// reflected on the next AuthRequired pass. Best-effort (5s TTL backstop).
		if err := cache.BustUserCache(c.Context(), redisClient, userID); err != nil {
			logger.Warn("admin: BustUserCache failed on cancel-subscription (5s TTL is the backstop)",
				zap.String("user_id", userID), zap.Error(err))
		}

		refundStatus := "none"
		if req.Refund {
			refundStatus = "intent_recorded"
		}
		logger.Info("admin: force-cancelled subscription",
			zap.String("user_id", userID), zap.Bool("refund_intent", req.Refund))

		return c.JSON(fiber.Map{"data": fiber.Map{
			"subscription_id": subscriptionID,
			"cancelled_at":    cancelledAt.UTC().Format(time.RFC3339),
			"refund_status":   refundStatus,
		}})
	}
}

// writeUserControlAudit persists the explicit, reason-carrying audit row for a
// per-user control action. The acting admin is read from c.Locals("user_id")
// (set by AuthRequired). A persistence failure is logged but never fails the
// action — the mutation already succeeded (mirror AuditLog middleware).
func writeUserControlAudit(c *fiber.Ctx, logger *zap.Logger, db *gorm.DB, action, targetID string, details model.AuditDetails) {
	adminID, _ := c.Locals("user_id").(string)
	if adminID == "" {
		logger.Error("admin: missing user_id in locals for explicit audit write",
			zap.String("action", action), zap.String("target_id", targetID))
		return
	}
	t := targetID
	entry := &model.AuditLogEntry{
		AdminID:  adminID,
		Action:   action,
		TargetID: &t,
		Details:  details,
		IP:       c.IP(),
	}
	if err := repository.CreateAuditEntry(c.Context(), db, entry); err != nil {
		logger.Error("admin: failed to persist explicit audit entry",
			zap.String("action", action), zap.String("target_id", targetID), zap.Error(err))
	}
}
