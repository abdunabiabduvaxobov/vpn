package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"vpnapp/server/api/internal/model"

	"gorm.io/gorm"
)

// --- Read helpers ---

// FindPlanByID returns a plan by primary key. ErrNotFound when missing.
func FindPlanByID(ctx context.Context, db *gorm.DB, planID string) (*model.Plan, error) {
	var plan model.Plan
	result := db.WithContext(ctx).Where("id = ?", planID).First(&plan)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, result.Error
	}
	return &plan, nil
}

// FindPlanByCode is the slug-based lookup used by /checkout (validates plan_code body field).
// Resolves both active and inactive plans — grandfathering requires inactive resolution.
func FindPlanByCode(ctx context.Context, db *gorm.DB, code string) (*model.Plan, error) {
	var plan model.Plan
	result := db.WithContext(ctx).Where("code = ?", code).First(&plan)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, result.Error
	}
	return &plan, nil
}

// FindSystemPlanID returns the UUID of the single is_system=true plan.
// Used by:
//   - JWT middleware backward-compat fallback (D-29) when plan_id claim is empty
//   - Expiry cron (03-09 / D-26) to flip lapsed users back to the system plan
//   - Admin endpoints that need to identify "the free fallback" (e.g. delete refusal)
//
// The partial unique index idx_plans_one_system enforces exactly-one-row at the
// DB layer, so this is safe to use with First().
func FindSystemPlanID(ctx context.Context, db *gorm.DB) (string, error) {
	var plan model.Plan
	result := db.WithContext(ctx).Where("is_system = ?", true).First(&plan)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return "", ErrNotFound
		}
		return "", result.Error
	}
	return plan.ID, nil
}

// ListActivePlans returns all plans where is_active=true, ordered by sort_order ASC, id ASC.
// Used by /admin/plans and (via ListActiveOffersForPublic) /api/v1/plans (D-27).
func ListActivePlans(ctx context.Context, db *gorm.DB) ([]model.Plan, error) {
	var plans []model.Plan
	err := db.WithContext(ctx).Where("is_active = ?", true).Order("sort_order ASC, id ASC").Find(&plans).Error
	return plans, err
}

// ListAllPlans returns plans regardless of is_active. Admin-only via plan 03-08.
func ListAllPlans(ctx context.Context, db *gorm.DB) ([]model.Plan, error) {
	var plans []model.Plan
	err := db.WithContext(ctx).Order("sort_order ASC, id ASC").Find(&plans).Error
	return plans, err
}

// --- Server access enforcement (PAY-11, D-21) ---

// ListServersForPlan returns the active VPN servers granted by the plan.
// Non-admins call this; admins bypass at the handler layer (D-21).
// ORDER BY current_load ASC preserves the existing public listing order
// from handler/servers.go.
func ListServersForPlan(ctx context.Context, db *gorm.DB, planID string) ([]model.VPNServer, error) {
	var servers []model.VPNServer
	err := db.WithContext(ctx).
		Joins("JOIN plan_servers ps ON ps.server_id = vpn_servers.id").
		Where("ps.plan_id = ? AND vpn_servers.is_active = ?", planID, true).
		Order("vpn_servers.current_load ASC").
		Find(&servers).Error
	return servers, err
}

// IsServerAllowedForPlan returns true iff a (plan, server) pairing exists.
// Used by GET /servers/:id/config — returns 404 (not 403) on false (D-22).
func IsServerAllowedForPlan(ctx context.Context, db *gorm.DB, planID, serverID string) (bool, error) {
	var n int64
	err := db.WithContext(ctx).Table("plan_servers").
		Where("plan_id = ? AND server_id = ?", planID, serverID).
		Count(&n).Error
	return n > 0, err
}

// --- Offers ---

// FindActiveOffer is the /checkout path's offer lookup — strict on is_active=true.
// Returns ErrNotFound when no active offer matches (caller returns 404 to client).
func FindActiveOffer(ctx context.Context, db *gorm.DB, planID, periodicity, currency string) (*model.PlanOffer, error) {
	var offer model.PlanOffer
	result := db.WithContext(ctx).
		Where("plan_id = ? AND periodicity = ? AND currency = ? AND is_active = ?", planID, periodicity, currency, true).
		First(&offer)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, result.Error
	}
	return &offer, nil
}

// FindOfferByLavaOfferID is the webhook's tier-derivation lookup (PAY-08).
// NOT filtered on is_active — grandfathered renewals must still resolve (ADR §19.10).
// Plan ID extracted from the returned PlanOffer is the canonical "what tier did
// they pay for".
func FindOfferByLavaOfferID(ctx context.Context, db *gorm.DB, lavaOfferID string) (*model.PlanOffer, error) {
	var offer model.PlanOffer
	result := db.WithContext(ctx).Where("lava_offer_id = ?", lavaOfferID).First(&offer)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, result.Error
	}
	return &offer, nil
}

// ListOffersForPlan returns all offers for a plan (active + inactive). Admin only.
func ListOffersForPlan(ctx context.Context, db *gorm.DB, planID string) ([]model.PlanOffer, error) {
	var offers []model.PlanOffer
	err := db.WithContext(ctx).Where("plan_id = ?", planID).Order("currency ASC, periodicity ASC, is_active DESC").Find(&offers).Error
	return offers, err
}

// ListActiveOffersForPublic returns ALL active offers across all active plans,
// used by /api/v1/plans (D-27). Caller groups by plan_id and filters by currency.
func ListActiveOffersForPublic(ctx context.Context, db *gorm.DB) ([]model.PlanOffer, error) {
	var offers []model.PlanOffer
	err := db.WithContext(ctx).
		Joins("JOIN plans p ON p.id = plan_offers.plan_id").
		Where("plan_offers.is_active = ? AND p.is_active = ?", true, true).
		Order("plan_offers.currency ASC, plan_offers.periodicity ASC").
		Find(&offers).Error
	return offers, err
}

// ListPlanServerCountries returns the distinct country_code values from the
// VPN servers attached to a plan. Used by public /plans `server_countries`.
//
// Returns sorted in alphabetical order (response stability).
func ListPlanServerCountries(ctx context.Context, db *gorm.DB, planID string) ([]string, error) {
	var countries []string
	err := db.WithContext(ctx).Table("vpn_servers").
		Distinct("country_code").
		Joins("JOIN plan_servers ps ON ps.server_id = vpn_servers.id").
		Where("ps.plan_id = ? AND vpn_servers.is_active = ?", planID, true).
		Order("country_code ASC").
		Pluck("country_code", &countries).Error
	return countries, err
}

// ListPlanServersJoined returns the full VPNServer rows attached to a plan
// (including inactive — admin needs to see them). For GET /admin/plans/:id.
func ListPlanServersJoined(ctx context.Context, db *gorm.DB, planID string) ([]model.VPNServer, error) {
	var servers []model.VPNServer
	err := db.WithContext(ctx).
		Joins("JOIN plan_servers ps ON ps.server_id = vpn_servers.id").
		Where("ps.plan_id = ?", planID).
		Order("vpn_servers.country_code ASC, vpn_servers.hostname ASC").
		Find(&servers).Error
	return servers, err
}

// --- SetUserPlan: transactional update ---

// SetUserPlan updates users.plan_id, users.subscription_tier,
// users.subscription_expires_at, AND the active subscriptions row, all in
// one transaction. Failing any one rolls back all.
//
// Called from:
//   - webhook payment.success (sets plan_id to paid plan; lavaContractID = &payload.contractId; expiresAt = computed period_end)
//   - webhook subscription.recurring.payment.success (extends expiresAt by one period)
//   - expiry cron (03-09): flips lapsed user to system plan (planID = FindSystemPlanID, lavaContractID = nil, expiresAt = nil)
//
// The subscription upsert uses the existing "find active, update OR insert"
// pattern (RESEARCH §3.3 option (a) — no partial-unique-index migration
// needed; matches subscription_repo.go::CreateOrUpdateSubscription style).
func SetUserPlan(ctx context.Context, db *gorm.DB, userID, planID string, lavaContractID *string, expiresAt *time.Time) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return SetUserPlanTx(tx, userID, planID, lavaContractID, expiresAt)
	})
}

// SetUserPlanTx performs the exact user + subscription writes SetUserPlan does,
// but on a caller-supplied *gorm.DB transaction (tx) instead of opening its own.
//
// This is the tx-aware variant used by code paths that already hold a
// transaction — notably WithUserLock (ADMIN-03): the lava webhook tier-grant
// and the admin force-cancel both run their writes on the lock-holding tx, so
// calling SetUserPlan(db, ...) there would open a SECOND, un-locked transaction
// outside the advisory lock and defeat the serialization. Callers inside a lock
// MUST use SetUserPlanTx(tx, ...); everyone else keeps calling SetUserPlan,
// which simply wraps this in its own transaction.
//
// The subscription upsert uses the existing "find active, update OR insert"
// pattern (matches the original SetUserPlan body verbatim).
func SetUserPlanTx(tx *gorm.DB, userID, planID string, lavaContractID *string, expiresAt *time.Time) error {
	// 1. Resolve the plan's code so we can write the denormalized subscription_tier.
	var plan model.Plan
	if err := tx.Where("id = ?", planID).First(&plan).Error; err != nil {
		return err
	}

	// 2. Update users row.
	updates := map[string]interface{}{
		"plan_id":           planID,
		"subscription_tier": plan.Code,
	}
	// nil expiresAt clears the column (cron path); non-nil sets it.
	updates["subscription_expires_at"] = expiresAt
	if err := tx.Model(&model.User{}).Where("id = ?", userID).Updates(updates).Error; err != nil {
		return err
	}

	// 3. Upsert the subscriptions row (manual find+update OR insert).
	var existing model.Subscription
	findErr := tx.Where("user_id = ? AND is_active = ?", userID, true).Order("started_at DESC").First(&existing).Error
	if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
		return findErr
	}
	if errors.Is(findErr, gorm.ErrRecordNotFound) {
		// No active subscription — insert one.
		sub := &model.Subscription{
			UserID:         userID,
			Plan:           plan.Code,
			LavaContractID: lavaContractID,
			IsActive:       true,
			ExpiresAt:      expiresAt,
		}
		return tx.Create(sub).Error
	}
	// Update existing.
	return tx.Model(&existing).Updates(map[string]interface{}{
		"plan":             plan.Code,
		"lava_contract_id": lavaContractID,
		"expires_at":       expiresAt,
		"is_active":        true,
	}).Error
}

// --- Plan CRUD (admin) ---

// CreatePlan inserts a plan. Caller validates input (handler layer in 03-08).
// is_system is NOT settable through this function — it's a migration-only invariant.
// Handlers MUST zero plan.IsSystem before calling.
func CreatePlan(ctx context.Context, db *gorm.DB, plan *model.Plan) error {
	plan.IsSystem = false // defence in depth — never trust caller for is_system
	return db.WithContext(ctx).Create(plan).Error
}

// UpdatePlan applies a partial update. `updates` MUST NOT contain "code" or
// "is_system" keys — handlers strip these per D-32 §4 + ADR §19.7.4 (code immutable).
// Returns the updated plan.
func UpdatePlan(ctx context.Context, db *gorm.DB, planID string, updates map[string]interface{}) (*model.Plan, error) {
	// Thread the request ctx onto the connection once; the UPDATE and the
	// FindPlanByID re-read below reuse the same context-bound session.
	db = db.WithContext(ctx)
	// Defence in depth — strip immutable keys at the repo layer too.
	delete(updates, "code")
	delete(updates, "is_system")
	delete(updates, "id")
	if len(updates) == 0 {
		return FindPlanByID(ctx, db, planID)
	}
	result := db.Model(&model.Plan{}).Where("id = ?", planID).Updates(updates)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, ErrNotFound
	}
	return FindPlanByID(ctx, db, planID)
}

// SoftDeletePlan sets plans.is_active=false AND deactivates all plan_offers.
// Refuses (returns ErrSystemPlan) when the target is_system=true.
// CountActiveUsersOnPlan is callable separately for the force-delete flow (03-08).
func SoftDeletePlan(ctx context.Context, db *gorm.DB, planID string) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var plan model.Plan
		if err := tx.Where("id = ?", planID).First(&plan).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if plan.IsSystem {
			return ErrSystemPlan
		}
		if err := tx.Model(&model.Plan{}).Where("id = ?", planID).Update("is_active", false).Error; err != nil {
			return err
		}
		// Deactivate all offers so no new checkouts succeed (grandfathered renewals still resolve via FindOfferByLavaOfferID).
		return tx.Model(&model.PlanOffer{}).Where("plan_id = ?", planID).Update("is_active", false).Error
	})
}

// CountActiveUsersOnPlan returns the number of users with plan_id = planID.
// Used by /admin/plans listing AND the soft-delete safety check (?force=true bypass).
func CountActiveUsersOnPlan(ctx context.Context, db *gorm.DB, planID string) (int64, error) {
	var n int64
	err := db.WithContext(ctx).Model(&model.User{}).Where("plan_id = ?", planID).Count(&n).Error
	return n, err
}

// --- Plan-server CRUD ---

// ReplacePlanServers atomically replaces the entire plan-server set.
// Empty serverIDs is valid (a plan with no servers).
func ReplacePlanServers(ctx context.Context, db *gorm.DB, planID string, serverIDs []string) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("plan_id = ?", planID).Delete(&model.PlanServer{}).Error; err != nil {
			return err
		}
		if len(serverIDs) == 0 {
			return nil
		}
		rows := make([]model.PlanServer, 0, len(serverIDs))
		for _, sid := range serverIDs {
			rows = append(rows, model.PlanServer{PlanID: planID, ServerID: sid})
		}
		return tx.Create(&rows).Error
	})
}

// AddPlanServer inserts a (plan, server) pairing. Idempotent — if the pairing
// already exists, returns nil (matches ADR §19.7.6 "POST returns 200 on already-present").
// Caller validates that the server exists + is_active=true.
func AddPlanServer(ctx context.Context, db *gorm.DB, planID, serverID string) error {
	// Thread the request ctx onto the connection once; the existence check
	// and the insert below reuse the same context-bound session.
	db = db.WithContext(ctx)
	// Idempotent insert: check first, then insert. Using ON CONFLICT DO NOTHING
	// would also work, but sqlite-test compatibility prefers the find-first pattern
	// (sqlite supports it, but composite-PK ON CONFLICT semantics across drivers
	// differ subtly).
	var existing model.PlanServer
	err := db.Where("plan_id = ? AND server_id = ?", planID, serverID).First(&existing).Error
	if err == nil {
		return nil // already exists — idempotent
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return db.Create(&model.PlanServer{PlanID: planID, ServerID: serverID}).Error
}

// RemovePlanServer deletes one pairing. ErrNotFound when not found.
func RemovePlanServer(ctx context.Context, db *gorm.DB, planID, serverID string) error {
	result := db.WithContext(ctx).Where("plan_id = ? AND server_id = ?", planID, serverID).Delete(&model.PlanServer{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// --- Offer CRUD ---

// CreatePlanOffer inserts a new offer. periodicity/currency are immutable
// (ADR §19.7.7) so they're set once here. Caller validates the partial unique
// constraint outcome (409 on dup active).
func CreatePlanOffer(ctx context.Context, db *gorm.DB, offer *model.PlanOffer) error {
	return db.WithContext(ctx).Create(offer).Error
}

// UpdatePlanOffer applies a partial update. periodicity + currency are stripped
// per ADR §19.7.7 (immutable).
func UpdatePlanOffer(ctx context.Context, db *gorm.DB, offerID string, updates map[string]interface{}) (*model.PlanOffer, error) {
	// Thread the request ctx onto the connection once; the UPDATE and the
	// findOfferByID re-read below reuse the same context-bound session.
	db = db.WithContext(ctx)
	delete(updates, "periodicity")
	delete(updates, "currency")
	delete(updates, "id")
	delete(updates, "plan_id")
	if len(updates) == 0 {
		return findOfferByID(ctx, db, offerID)
	}
	result := db.Model(&model.PlanOffer{}).Where("id = ?", offerID).Updates(updates)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, ErrNotFound
	}
	return findOfferByID(ctx, db, offerID)
}

// DeletePlanOffer is soft — sets is_active=false. Returns ErrNotFound on miss.
func DeletePlanOffer(ctx context.Context, db *gorm.DB, offerID string) error {
	result := db.WithContext(ctx).Model(&model.PlanOffer{}).Where("id = ?", offerID).Update("is_active", false)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// ReplaceOffer is the ADR §19.10 price-versioning flow: deactivate the old offer
// and create a new one in a single transaction. Existing subscribers keep
// renewing on the old (now is_active=false) row; new sign-ups see only the new
// active offer. The new offer inherits periodicity + currency from the old —
// the caller's `newOffer` MUST have plan_id + periodicity + currency already
// populated to match; this function does NOT copy them.
func ReplaceOffer(ctx context.Context, db *gorm.DB, oldOfferID string, newOffer *model.PlanOffer) (*model.PlanOffer, error) {
	var saved *model.PlanOffer
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Deactivate the old.
		result := tx.Model(&model.PlanOffer{}).Where("id = ?", oldOfferID).Update("is_active", false)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrNotFound
		}
		// Insert the new (force is_active=true).
		newOffer.IsActive = true
		if err := tx.Create(newOffer).Error; err != nil {
			return err
		}
		saved = newOffer
		return nil
	})
	if err != nil {
		return nil, err
	}
	return saved, nil
}

// --- internal helpers ---

func findOfferByID(ctx context.Context, db *gorm.DB, offerID string) (*model.PlanOffer, error) {
	var offer model.PlanOffer
	result := db.WithContext(ctx).Where("id = ?", offerID).First(&offer)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, result.Error
	}
	return &offer, nil
}

// --- errors ---

// ErrSystemPlan is returned by SoftDeletePlan when called on a system plan.
// Handler maps to HTTP 403.
var ErrSystemPlan = errors.New("system plan cannot be deleted")

// NormalizePlanCode is a small helper for handlers that want lowercased lookup;
// kept here so all callers use the same normalisation.
func NormalizePlanCode(code string) string {
	return strings.ToLower(strings.TrimSpace(code))
}
