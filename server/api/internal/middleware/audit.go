package middleware

import (
	"strings"

	"vpnapp/server/api/internal/model"
	"vpnapp/server/api/internal/repository"

	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// AuditLog wraps admin handlers so that any mutating request whose
// handler returns a 2xx status is persisted to the audit_log table.
// Must run AFTER AuthRequired and AdminRequired — it reads user_id
// from c.Locals.
//
// Why post-handler instead of pre-handler:
//
//	Running after c.Next() lets us skip audit writes for requests
//	that failed validation (400), auth (401), or not-found (404).
//	Otherwise every typo in a curl would spam the table.
//
// Only GET requests are free — writes (POST/PATCH/PUT/DELETE) are
// recorded. For write methods where the handler rejected the request
// with a non-2xx, we skip the write entirely.
func AuditLog(db *gorm.DB, logger *zap.Logger) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Not a mutation — nothing to audit.
		if c.Method() == fiber.MethodGet || c.Method() == fiber.MethodHead {
			return c.Next()
		}

		// Let the handler run first so we can short-circuit on failure.
		if err := c.Next(); err != nil {
			return err
		}
		if c.Response().StatusCode() >= 300 {
			return nil
		}

		// Best-effort extraction of the acting admin's UUID. If Locals
		// is missing the middleware stack is misconfigured (this
		// middleware must run after AuthRequired + AdminRequired).
		// Skip the audit write but log loudly — a compliance-grade
		// trail cannot tolerate silent gaps.
		adminID, _ := c.Locals("user_id").(string)
		if adminID == "" {
			logger.Error("audit: missing user_id in locals — middleware stack misordered",
				zap.String("method", c.Method()),
				zap.String("path", c.Path()),
				zap.String("remote", c.IP()),
			)
			return nil
		}

		action := describeAction(c.Method(), c.Path())
		target := extractTargetID(c.Path())

		var targetPtr *string
		if target != "" {
			t := target
			targetPtr = &t
		}

		// The request body is only reliably available post-handler
		// when BodyParser stored a copy; Fiber does not expose a
		// parsed map we can snapshot. Instead, we record the path
		// params and the query string — enough for a human to
		// reconstruct "what did this admin touch".
		details := model.AuditDetails{
			"method": c.Method(),
			"path":   c.Path(),
		}
		if q := c.Request().URI().QueryString(); len(q) > 0 {
			details["query"] = string(q)
		}
		for k, v := range c.AllParams() {
			details["param_"+k] = v
		}

		entry := &model.AuditLogEntry{
			AdminID:  adminID,
			Action:   action,
			TargetID: targetPtr,
			Details:  details,
			IP:       c.IP(),
		}
		if err := repository.CreateAuditEntry(c.Context(), db, entry); err != nil {
			// Never fail the admin action because audit persistence
			// failed — the mutation already succeeded. Log loudly.
			logger.Error("audit: failed to persist entry",
				zap.String("action", action),
				zap.String("admin_id", adminID),
				zap.Error(err),
			)
		}
		return nil
	}
}

// describeAction maps (method, path) to a short stable action name like
// "update_user" or "delete_server". The name is what the UI's audit
// log page displays in the Action column, so keep it readable.
func describeAction(method, path string) string {
	// Strip the /api/v1 prefix so the keys are independent of
	// versioning. Keep the verb short — "update", "delete",
	// "create", "logout", "password".
	stripped := strings.TrimPrefix(path, "/api/v1")

	switch {
	case method == fiber.MethodPost && strings.HasSuffix(stripped, "/admin/change-password"):
		return "change_password"
	// ADMIN-02 per-user controls. These POST /admin/users/:id/<action> routes
	// come BEFORE the generic PATCH/DELETE /admin/users/ cases so the readable
	// label wins over the post_admin_users_<uuid>_<action> fallback.
	case method == fiber.MethodPost && strings.HasPrefix(stripped, "/admin/users/") && strings.HasSuffix(stripped, "/suspend"):
		return "suspend_user"
	case method == fiber.MethodPost && strings.HasPrefix(stripped, "/admin/users/") && strings.HasSuffix(stripped, "/unsuspend"):
		return "unsuspend_user"
	case method == fiber.MethodPost && strings.HasPrefix(stripped, "/admin/users/") && strings.HasSuffix(stripped, "/disconnect"):
		return "disconnect_user"
	case method == fiber.MethodPost && strings.HasPrefix(stripped, "/admin/users/") && strings.HasSuffix(stripped, "/cancel-subscription"):
		return "cancel_subscription"
	case method == fiber.MethodPatch && strings.HasPrefix(stripped, "/admin/users/"):
		return "update_user"
	// Device delete must come before user delete because the device URL
	// is nested under /admin/users/:id/ and contains "/devices/".
	case method == fiber.MethodDelete && strings.Contains(stripped, "/devices/"):
		return "delete_device"
	case method == fiber.MethodDelete && strings.HasPrefix(stripped, "/admin/users/"):
		return "delete_user"
	case method == fiber.MethodDelete && strings.HasPrefix(stripped, "/admin/servers/"):
		return "delete_server"
	case method == fiber.MethodPatch && strings.HasPrefix(stripped, "/admin/servers/"):
		return "update_server"
	// ADMIN-04 server controls. These POST /admin/servers/:id/<action> routes
	// come BEFORE the generic create_server case (which matches the
	// /admin/servers POST prefix) so the readable label wins over the
	// post_admin_servers_<uuid>_<action> fallback.
	case method == fiber.MethodPost && strings.HasPrefix(stripped, "/admin/servers/") && strings.HasSuffix(stripped, "/drain"):
		return "drain_server"
	case method == fiber.MethodPost && strings.HasPrefix(stripped, "/admin/servers/") && strings.HasSuffix(stripped, "/undrain"):
		return "undrain_server"
	case method == fiber.MethodPost && strings.HasPrefix(stripped, "/admin/servers/") && strings.HasSuffix(stripped, "/disconnect"):
		return "disconnect_server"
	case method == fiber.MethodPost && strings.HasPrefix(stripped, "/admin/servers"):
		return "create_server"

	// --- Plan-CRUD (Phase 3 PAY-13/14/15). ---
	// More-specific URLs (sub-resources, /replace) come BEFORE shorter
	// /admin/plans matches so the switch resolves to the correct label.
	//
	// /replace is the price-versioning sub-resource (ADR §19.10 / PAY-15).
	case method == fiber.MethodPost && strings.Contains(stripped, "/admin/plans/") && strings.HasSuffix(stripped, "/replace"):
		return "replace_plan_offer"
	// Plan-offers sub-resource. Order: more-specific (offer_id segment) before less.
	case method == fiber.MethodPatch && strings.Contains(stripped, "/admin/plans/") && strings.Contains(stripped, "/offers/"):
		return "update_plan_offer"
	case method == fiber.MethodDelete && strings.Contains(stripped, "/admin/plans/") && strings.Contains(stripped, "/offers/"):
		return "delete_plan_offer"
	case method == fiber.MethodPost && strings.Contains(stripped, "/admin/plans/") && strings.HasSuffix(stripped, "/offers"):
		return "create_plan_offer"
	// Plan-servers sub-resource.
	case method == fiber.MethodPut && strings.Contains(stripped, "/admin/plans/") && strings.HasSuffix(stripped, "/servers"):
		return "replace_plan_servers"
	case method == fiber.MethodPost && strings.Contains(stripped, "/admin/plans/") && strings.Contains(stripped, "/servers/"):
		return "add_plan_server"
	case method == fiber.MethodDelete && strings.Contains(stripped, "/admin/plans/") && strings.Contains(stripped, "/servers/"):
		return "remove_plan_server"
	// Top-level plans CRUD. These must come AFTER the sub-resource cases
	// because /admin/plans/:id/offers also has the /admin/plans/ prefix.
	case method == fiber.MethodPost && stripped == "/admin/plans":
		return "create_plan"
	case method == fiber.MethodPatch && strings.HasPrefix(stripped, "/admin/plans/"):
		return "update_plan"
	case method == fiber.MethodDelete && strings.HasPrefix(stripped, "/admin/plans/"):
		return "delete_plan"

	// --- ADMIN-05 system controls. ---
	// Feature-flag set (PUT /admin/feature-flags/:key) + broadcast CRUD.
	case method == fiber.MethodPut && strings.HasPrefix(stripped, "/admin/feature-flags/"):
		return "set_feature_flag"
	case method == fiber.MethodPost && stripped == "/admin/broadcasts":
		return "create_broadcast"
	case method == fiber.MethodPatch && strings.HasPrefix(stripped, "/admin/broadcasts/"):
		return "update_broadcast"
	case method == fiber.MethodDelete && strings.HasPrefix(stripped, "/admin/broadcasts/"):
		return "delete_broadcast"
	}
	// Fallback: sanitise the path into a snake_case action name so the
	// audit_log.action column (VARCHAR(64)) can't overflow on deep URLs
	// and so human readers aren't confronted with slashes in an
	// otherwise-snake-case column.
	sanitised := strings.TrimPrefix(stripped, "/")
	sanitised = strings.ReplaceAll(sanitised, "/", "_")
	action := strings.ToLower(method) + "_" + sanitised
	if len(action) > 60 {
		action = action[:60]
	}
	return action
}

// extractTargetID pulls the primary :id path parameter for the common
// URL shapes under /admin/. Returns an empty string when there is no
// obvious target (create, list, stats endpoints).
func extractTargetID(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	// Look for the first UUID-shaped (36-char) segment after "admin".
	for i, p := range parts {
		if p == "admin" && i+2 < len(parts) {
			id := parts[i+2]
			if len(id) == 36 && strings.Count(id, "-") == 4 {
				return id
			}
		}
	}
	return ""
}
