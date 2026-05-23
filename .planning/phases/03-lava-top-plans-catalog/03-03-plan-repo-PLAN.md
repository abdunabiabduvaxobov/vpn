---
phase: 3
plan: 03
type: execute
slug: lava-top-plans-catalog
plan_number: 3
wave: 2
depends_on: [1]
files_modified:
  - server/api/internal/repository/plan_repo.go
  - server/api/internal/repository/plan_repo_test.go
  - server/api/internal/repository/invoice_repo.go
  - server/api/internal/repository/invoice_repo_test.go
autonomous: true
requirements_addressed: [PAY-01, PAY-08, PAY-09, PAY-11]
estimated_complexity: high
must_haves:
  refs:
    - "See <must_haves> XML block in plan body for canonical truths / artifacts / key_links"

---

<objective>
Implement the repository layer that handlers consume in Wave 3+: `plan_repo.go` (FindPlanByID, FindPlanByCode, FindSystemPlanID, ListActivePlans, ListPlansForPublic, ListServersForPlan, IsServerAllowedForPlan, SetUserPlan (transactional), and the full plan/offer/plan_server CRUD that the admin handlers in 03-08 will call) and `invoice_repo.go` (CreateInvoice, FindInvoiceByID, FindActivePendingInvoice for 60s checkout idempotency, FindInvoiceByLavaID for webhook reverse-lookup, UpdateInvoiceStatus). All functions are sqlite-test-compatible (handlers' unit tests run on `:memory:` sqlite) and Postgres-production-correct.
</objective>

<context>
@.planning/phases/03-lava-top-plans-catalog/03-CONTEXT.md
@.planning/phases/03-lava-top-plans-catalog/03-RESEARCH.md
@server/api/internal/repository/subscription_repo.go
@server/api/internal/repository/user_repo.go
@server/api/internal/model/plan.go
@server/api/internal/model/invoice.go
</context>

<interfaces>
Function signatures the handlers in plans 03-04, 03-05, 03-06, 03-07, 03-08, 03-09 will consume:

```go
package repository

// --- Read helpers ---
func FindPlanByID(db *gorm.DB, planID string) (*model.Plan, error)
func FindPlanByCode(db *gorm.DB, code string) (*model.Plan, error)
func FindSystemPlanID(db *gorm.DB) (string, error)   // resolves "the free plan" — for backward-compat JWT fallback (D-29) and expiry cron (03-09)
func ListActivePlans(db *gorm.DB) ([]model.Plan, error)
func ListAllPlans(db *gorm.DB) ([]model.Plan, error) // includes inactive — admin only

// --- Server access enforcement (PAY-11, D-21) ---
func ListServersForPlan(db *gorm.DB, planID string) ([]model.VPNServer, error)
func IsServerAllowedForPlan(db *gorm.DB, planID, serverID string) (bool, error)

// --- Offers ---
func FindActiveOffer(db *gorm.DB, planID, periodicity, currency string) (*model.PlanOffer, error)
func FindOfferByLavaOfferID(db *gorm.DB, lavaOfferID string) (*model.PlanOffer, error) // for webhook PAY-08 reverse-lookup
func ListOffersForPlan(db *gorm.DB, planID string) ([]model.PlanOffer, error)
func ListActiveOffersForPublic(db *gorm.DB) ([]model.PlanOffer, error)                  // for public /plans
func ListPlanServerCountries(db *gorm.DB, planID string) ([]string, error)              // for public /plans `server_countries`
func ListPlanServersJoined(db *gorm.DB, planID string) ([]model.VPNServer, error)       // for admin GET /plans/:id

// --- SetUserPlan: transactional update of users.plan_id + users.subscription_tier + users.subscription_expires_at + subscriptions row ---
// Called from the webhook handler's payment.success / subscription.recurring.payment.success branches.
// expiresAt is *time.Time so callers can pass nil (e.g. for downgrade to system plan).
// lavaContractID is *string so callers can pass nil when reverting to the system plan via the expiry cron.
func SetUserPlan(db *gorm.DB, userID, planID string, lavaContractID *string, expiresAt *time.Time) error

// --- CRUD for admin handlers (plan 03-08) ---
// All admin CRUD takes either *gorm.DB (auto-tx) or runs inside a passed tx.
// Validation lives in the handlers; these are pure DB ops.

// Plans
func CreatePlan(db *gorm.DB, plan *model.Plan) error
func UpdatePlan(db *gorm.DB, planID string, updates map[string]interface{}) (*model.Plan, error)
func SoftDeletePlan(db *gorm.DB, planID string) error // sets is_active=false on plan AND all its offers
func CountActiveUsersOnPlan(db *gorm.DB, planID string) (int64, error) // for /admin/plans listing's active_user_count + force-delete safety

// Plan-servers
func ReplacePlanServers(db *gorm.DB, planID string, serverIDs []string) error // single tx: DELETE then bulk INSERT
func AddPlanServer(db *gorm.DB, planID, serverID string) error                // upsert (idempotent — D-37 ADR §19.7.6)
func RemovePlanServer(db *gorm.DB, planID, serverID string) error             // returns ErrNotFound on missing pairing

// Plan-offers
func CreatePlanOffer(db *gorm.DB, offer *model.PlanOffer) error
func UpdatePlanOffer(db *gorm.DB, offerID string, updates map[string]interface{}) (*model.PlanOffer, error)
func DeletePlanOffer(db *gorm.DB, offerID string) error    // soft — is_active=false
func ReplaceOffer(db *gorm.DB, oldOfferID string, newOffer *model.PlanOffer) (*model.PlanOffer, error) // single tx: old.is_active=false + new INSERT
```

Invoice repo signatures consumed by 03-05 + 03-06:

```go
func CreateInvoice(db *gorm.DB, inv *model.Invoice) error
func FindInvoiceByID(db *gorm.DB, id string) (*model.Invoice, error)
func FindInvoiceByLavaID(db *gorm.DB, lavaInvoiceID string) (*model.Invoice, error)
// 60s idempotency window (ADR §9.2): returns the most recent pending invoice
// for (user_id, lava-side offer_id) where created_at > now() - 60s.
func FindActivePendingInvoice(db *gorm.DB, userID, lavaOfferID string, within time.Duration) (*model.Invoice, error)
func UpdateInvoiceStatus(db *gorm.DB, id, status string) error
```
</interfaces>

<tasks>

<task type="auto">
  <id>03-03-T01</id>
  <name>Write plan_repo.go (read helpers + SetUserPlan transactional + CRUD)</name>
  <files>server/api/internal/repository/plan_repo.go</files>
  <read_first>
    - server/api/internal/repository/subscription_repo.go (T05 of plan 03-01 — pattern for tx + ErrNotFound + Updates map)
    - server/api/internal/repository/user_repo.go (existing pattern for FindUserByID — keep style)
    - server/api/internal/repository/server_repo.go (existing ListActiveServers — referenced by ListServersForPlan join)
    - server/api/internal/model/plan.go (T04 of plan 03-01 — Plan, PlanServer, PlanOffer struct tags)
    - .planning/phases/03-lava-top-plans-catalog/03-RESEARCH.md §3.3 (SetUserPlan tx skeleton — choose option (a): manual find+update vs insert, no partial unique index)
    - .planning/phases/03-lava-top-plans-catalog/03-CONTEXT.md D-19 (SetUserPlan semantics), D-21 (server-access enforcement), D-37 (ADR §19.7.6 — idempotent POST add)
    - docs/ADR-007-lava-sso-rework.md §19.5 (ListServersForPlan JOIN), §19.6 (FindOfferByLavaOfferID reverse-lookup)
  </read_first>
  <action>
    Create `server/api/internal/repository/plan_repo.go` with the following functions. Each function's responsibility is documented at the top of its body to match the canonical reference (ADR §19 / RESEARCH §). NO new GORM imports needed beyond `gorm.io/gorm` and `gorm.io/gorm/clause` (already used by other repos).

```go
package repository

import (
	"errors"
	"strings"
	"time"

	"vpnapp/server/api/internal/model"

	"gorm.io/gorm"
)

// --- Read helpers ---

// FindPlanByID returns a plan by primary key. ErrNotFound when missing.
func FindPlanByID(db *gorm.DB, planID string) (*model.Plan, error) {
	var plan model.Plan
	result := db.Where("id = ?", planID).First(&plan)
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
func FindPlanByCode(db *gorm.DB, code string) (*model.Plan, error) {
	var plan model.Plan
	result := db.Where("code = ?", code).First(&plan)
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
func FindSystemPlanID(db *gorm.DB) (string, error) {
	var plan model.Plan
	result := db.Where("is_system = ?", true).First(&plan)
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
func ListActivePlans(db *gorm.DB) ([]model.Plan, error) {
	var plans []model.Plan
	err := db.Where("is_active = ?", true).Order("sort_order ASC, id ASC").Find(&plans).Error
	return plans, err
}

// ListAllPlans returns plans regardless of is_active. Admin-only via plan 03-08.
func ListAllPlans(db *gorm.DB) ([]model.Plan, error) {
	var plans []model.Plan
	err := db.Order("sort_order ASC, id ASC").Find(&plans).Error
	return plans, err
}

// --- Server access enforcement (PAY-11, D-21) ---

// ListServersForPlan returns the active VPN servers granted by the plan.
// Non-admins call this; admins bypass at the handler layer (D-21).
// ORDER BY current_load ASC preserves the existing public listing order
// from handler/servers.go.
func ListServersForPlan(db *gorm.DB, planID string) ([]model.VPNServer, error) {
	var servers []model.VPNServer
	err := db.
		Joins("JOIN plan_servers ps ON ps.server_id = vpn_servers.id").
		Where("ps.plan_id = ? AND vpn_servers.is_active = ?", planID, true).
		Order("vpn_servers.current_load ASC").
		Find(&servers).Error
	return servers, err
}

// IsServerAllowedForPlan returns true iff a (plan, server) pairing exists.
// Used by GET /servers/:id/config — returns 404 (not 403) on false (D-22).
func IsServerAllowedForPlan(db *gorm.DB, planID, serverID string) (bool, error) {
	var n int64
	err := db.Table("plan_servers").
		Where("plan_id = ? AND server_id = ?", planID, serverID).
		Count(&n).Error
	return n > 0, err
}

// --- Offers ---

// FindActiveOffer is the /checkout path's offer lookup — strict on is_active=true.
// Returns ErrNotFound when no active offer matches (caller returns 404 to client).
func FindActiveOffer(db *gorm.DB, planID, periodicity, currency string) (*model.PlanOffer, error) {
	var offer model.PlanOffer
	result := db.
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
func FindOfferByLavaOfferID(db *gorm.DB, lavaOfferID string) (*model.PlanOffer, error) {
	var offer model.PlanOffer
	result := db.Where("lava_offer_id = ?", lavaOfferID).First(&offer)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, result.Error
	}
	return &offer, nil
}

// ListOffersForPlan returns all offers for a plan (active + inactive). Admin only.
func ListOffersForPlan(db *gorm.DB, planID string) ([]model.PlanOffer, error) {
	var offers []model.PlanOffer
	err := db.Where("plan_id = ?", planID).Order("currency ASC, periodicity ASC, is_active DESC").Find(&offers).Error
	return offers, err
}

// ListActiveOffersForPublic returns ALL active offers across all active plans,
// used by /api/v1/plans (D-27). Caller groups by plan_id and filters by currency.
func ListActiveOffersForPublic(db *gorm.DB) ([]model.PlanOffer, error) {
	var offers []model.PlanOffer
	err := db.
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
func ListPlanServerCountries(db *gorm.DB, planID string) ([]string, error) {
	var countries []string
	err := db.Table("vpn_servers").
		Distinct("country_code").
		Joins("JOIN plan_servers ps ON ps.server_id = vpn_servers.id").
		Where("ps.plan_id = ? AND vpn_servers.is_active = ?", planID, true).
		Order("country_code ASC").
		Pluck("country_code", &countries).Error
	return countries, err
}

// ListPlanServersJoined returns the full VPNServer rows attached to a plan
// (including inactive — admin needs to see them). For GET /admin/plans/:id.
func ListPlanServersJoined(db *gorm.DB, planID string) ([]model.VPNServer, error) {
	var servers []model.VPNServer
	err := db.
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
func SetUserPlan(db *gorm.DB, userID, planID string, lavaContractID *string, expiresAt *time.Time) error {
	return db.Transaction(func(tx *gorm.DB) error {
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
	})
}

// --- Plan CRUD (admin) ---

// CreatePlan inserts a plan. Caller validates input (handler layer in 03-08).
// is_system is NOT settable through this function — it's a migration-only invariant.
// Handlers MUST zero plan.IsSystem before calling.
func CreatePlan(db *gorm.DB, plan *model.Plan) error {
	plan.IsSystem = false // defence in depth — never trust caller for is_system
	return db.Create(plan).Error
}

// UpdatePlan applies a partial update. `updates` MUST NOT contain "code" or
// "is_system" keys — handlers strip these per D-32 §4 + ADR §19.7.4 (code immutable).
// Returns the updated plan.
func UpdatePlan(db *gorm.DB, planID string, updates map[string]interface{}) (*model.Plan, error) {
	// Defence in depth — strip immutable keys at the repo layer too.
	delete(updates, "code")
	delete(updates, "is_system")
	delete(updates, "id")
	if len(updates) == 0 {
		return FindPlanByID(db, planID)
	}
	result := db.Model(&model.Plan{}).Where("id = ?", planID).Updates(updates)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, ErrNotFound
	}
	return FindPlanByID(db, planID)
}

// SoftDeletePlan sets plans.is_active=false AND deactivates all plan_offers.
// Refuses (returns ErrSystemPlan) when the target is_system=true.
// CountActiveUsersOnPlan is callable separately for the force-delete flow (03-08).
func SoftDeletePlan(db *gorm.DB, planID string) error {
	return db.Transaction(func(tx *gorm.DB) error {
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
func CountActiveUsersOnPlan(db *gorm.DB, planID string) (int64, error) {
	var n int64
	err := db.Model(&model.User{}).Where("plan_id = ?", planID).Count(&n).Error
	return n, err
}

// --- Plan-server CRUD ---

// ReplacePlanServers atomically replaces the entire plan-server set.
// Empty serverIDs is valid (a plan with no servers).
func ReplacePlanServers(db *gorm.DB, planID string, serverIDs []string) error {
	return db.Transaction(func(tx *gorm.DB) error {
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
func AddPlanServer(db *gorm.DB, planID, serverID string) error {
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
func RemovePlanServer(db *gorm.DB, planID, serverID string) error {
	result := db.Where("plan_id = ? AND server_id = ?", planID, serverID).Delete(&model.PlanServer{})
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
func CreatePlanOffer(db *gorm.DB, offer *model.PlanOffer) error {
	return db.Create(offer).Error
}

// UpdatePlanOffer applies a partial update. periodicity + currency are stripped
// per ADR §19.7.7 (immutable).
func UpdatePlanOffer(db *gorm.DB, offerID string, updates map[string]interface{}) (*model.PlanOffer, error) {
	delete(updates, "periodicity")
	delete(updates, "currency")
	delete(updates, "id")
	delete(updates, "plan_id")
	if len(updates) == 0 {
		return findOfferByID(db, offerID)
	}
	result := db.Model(&model.PlanOffer{}).Where("id = ?", offerID).Updates(updates)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, ErrNotFound
	}
	return findOfferByID(db, offerID)
}

// DeletePlanOffer is soft — sets is_active=false. Returns ErrNotFound on miss.
func DeletePlanOffer(db *gorm.DB, offerID string) error {
	result := db.Model(&model.PlanOffer{}).Where("id = ?", offerID).Update("is_active", false)
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
func ReplaceOffer(db *gorm.DB, oldOfferID string, newOffer *model.PlanOffer) (*model.PlanOffer, error) {
	var saved *model.PlanOffer
	err := db.Transaction(func(tx *gorm.DB) error {
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

func findOfferByID(db *gorm.DB, offerID string) (*model.PlanOffer, error) {
	var offer model.PlanOffer
	result := db.Where("id = ?", offerID).First(&offer)
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

// normalizeCode is a small helper for handlers that want lowercased lookup;
// kept here so all callers use the same normalisation.
func NormalizePlanCode(code string) string {
	return strings.ToLower(strings.TrimSpace(code))
}
```

    Then run `cd server/api && go build ./internal/repository/...` to confirm the package compiles.
  </action>
  <acceptance_criteria>
    - File `server/api/internal/repository/plan_repo.go` exists
    - `grep -c "^func " server/api/internal/repository/plan_repo.go` returns at least 17 (FindPlanByID, FindPlanByCode, FindSystemPlanID, ListActivePlans, ListAllPlans, ListServersForPlan, IsServerAllowedForPlan, FindActiveOffer, FindOfferByLavaOfferID, ListOffersForPlan, ListActiveOffersForPublic, ListPlanServerCountries, ListPlanServersJoined, SetUserPlan, CreatePlan, UpdatePlan, SoftDeletePlan, CountActiveUsersOnPlan, ReplacePlanServers, AddPlanServer, RemovePlanServer, CreatePlanOffer, UpdatePlanOffer, DeletePlanOffer, ReplaceOffer)
    - `grep "db.Transaction(func(tx \*gorm.DB) error" server/api/internal/repository/plan_repo.go` finds at least 3 matches (SetUserPlan, SoftDeletePlan, ReplacePlanServers, ReplaceOffer)
    - `grep "ErrSystemPlan" server/api/internal/repository/plan_repo.go` finds matches (var declaration + usage in SoftDeletePlan)
    - `grep "delete(updates, \"code\")" server/api/internal/repository/plan_repo.go` finds one match (immutable per ADR §19.7.4)
    - `grep "delete(updates, \"is_system\")" server/api/internal/repository/plan_repo.go` finds one match (D-32 §4)
    - `grep "delete(updates, \"periodicity\")" server/api/internal/repository/plan_repo.go` finds one match (ADR §19.7.7)
    - `grep "FindOfferByLavaOfferID" server/api/internal/repository/plan_repo.go` finds matches (PAY-08 reverse-lookup)
    - `cd server/api && go build ./internal/repository/...` exits 0
    - `cd server/api && go vet ./internal/repository/...` exits 0
  </acceptance_criteria>
  <automated>cd server/api && go build ./internal/repository/... && go vet ./internal/repository/...</automated>
  <done>plan_repo.go compiles standalone with all 17+ functions; ErrSystemPlan exported for handler error mapping.</done>
</task>

<task type="auto">
  <id>03-03-T02</id>
  <name>Write plan_repo_test.go (sqlite-backed unit tests for read helpers + SetUserPlan + CRUD)</name>
  <files>server/api/internal/repository/plan_repo_test.go</files>
  <read_first>
    - server/api/internal/repository/subscription_repo_test.go (existing pattern — sqlite :memory:, hand-rolled CREATE TABLE strings, ptr helper)
    - server/api/internal/repository/plan_repo.go (just written in T01)
    - .planning/phases/03-lava-top-plans-catalog/03-VALIDATION.md PAY-11 row (`TestListServersForPlan` is the named test, called out in the verification map)
    - .planning/phases/03-lava-top-plans-catalog/03-RESEARCH.md §9.1 (test schema must include subscriptions DDL; SQLite cannot use gen_random_uuid — use hand-generated UUID strings)
  </read_first>
  <action>
    Create `server/api/internal/repository/plan_repo_test.go` with sqlite in-memory tests covering the high-traffic functions. The test file mirrors the existing `subscription_repo_test.go` pattern: a `setupTestDB(t)` helper that opens sqlite and CREATE TABLEs the minimum schema for plan_repo work (plans, plan_servers, plan_offers, users, subscriptions, vpn_servers). Use a `ptr[T any]` helper for pointer-typed columns.

    The file MUST include these test functions (names called out in 03-VALIDATION.md / RESEARCH §"Wave 0"):
    - `TestFindPlanByID_FoundAndNotFound`
    - `TestFindPlanByCode_FoundAndNotFound`
    - `TestFindSystemPlanID_HappyPath`
    - `TestListActivePlans_FiltersInactive`
    - `TestListServersForPlan_FiltersByPlanAndActive` (PAY-11 named test in 03-VALIDATION.md)
    - `TestIsServerAllowedForPlan_TrueFalse`
    - `TestFindActiveOffer_ReturnsActiveOnly`
    - `TestFindOfferByLavaOfferID_GrandfatheredInactive` (PAY-08 — must resolve is_active=false rows for renewal webhooks)
    - `TestSetUserPlan_UpdatesUserAndUpsertsSubscription` (PAY-09 critical test — uses fresh user + nil expiresAt then user with expiresAt)
    - `TestSoftDeletePlan_RefusesSystemPlan` (D-32 §4)
    - `TestUpdatePlan_StripsImmutableFields` (code + is_system + id are immutable)
    - `TestReplaceOffer_DeactivatesOldInsertsNewInOneTx` (PAY-15)
    - `TestReplacePlanServers_AtomicReplacement` (PAY-14)
    - `TestAddPlanServer_IdempotentOnReinsert` (ADR §19.7.6 — POST returns 200 on already-present)
    - `TestRemovePlanServer_ReturnsErrNotFoundWhenMissing`

    File body (verbatim):

```go
package repository_test

import (
	"testing"
	"time"

	"vpnapp/server/api/internal/model"
	"vpnapp/server/api/internal/repository"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func ptrStr(v string) *string { return &v }

// setupPlanRepoDB creates the minimum schema for plan_repo tests.
// SQLite does NOT support `gen_random_uuid()` — tests pass explicit UUIDs.
func setupPlanRepoDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	stmts := []string{
		`CREATE TABLE plans (
			id TEXT PRIMARY KEY,
			code TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			max_devices INTEGER NOT NULL,
			max_servers INTEGER NOT NULL,
			speed_limit_mbps INTEGER NOT NULL DEFAULT 0,
			is_active INTEGER NOT NULL DEFAULT 1,
			is_system INTEGER NOT NULL DEFAULT 0,
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE plan_servers (
			plan_id TEXT NOT NULL,
			server_id TEXT NOT NULL,
			PRIMARY KEY (plan_id, server_id)
		)`,
		`CREATE TABLE plan_offers (
			id TEXT PRIMARY KEY,
			plan_id TEXT NOT NULL,
			periodicity TEXT NOT NULL,
			currency TEXT NOT NULL,
			amount REAL NOT NULL,
			lava_offer_id TEXT,
			is_active INTEGER NOT NULL DEFAULT 1,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE users (
			id TEXT PRIMARY KEY,
			email_hash TEXT,
			password_hash TEXT,
			full_name TEXT NOT NULL DEFAULT '',
			subscription_tier TEXT NOT NULL DEFAULT 'free',
			subscription_expires_at TIMESTAMP,
			role TEXT NOT NULL DEFAULT 'user',
			telegram_user_id INTEGER,
			telegram_linked_at TIMESTAMP,
			telegram_username TEXT,
			telegram_first_name TEXT,
			apple_user_id TEXT,
			google_user_id TEXT,
			email TEXT,
			email_verified INTEGER NOT NULL DEFAULT 0,
			email_is_private_relay INTEGER NOT NULL DEFAULT 0,
			auth_provider TEXT NOT NULL DEFAULT 'guest',
			plan_id TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE subscriptions (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			plan TEXT NOT NULL DEFAULT 'free',
			lava_contract_id TEXT,
			is_active INTEGER NOT NULL DEFAULT 1,
			started_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			expires_at TIMESTAMP
		)`,
		`CREATE TABLE vpn_servers (
			id TEXT PRIMARY KEY,
			hostname TEXT NOT NULL,
			ip_address TEXT NOT NULL DEFAULT '',
			country_code TEXT NOT NULL DEFAULT '',
			country TEXT NOT NULL DEFAULT '',
			protocol TEXT NOT NULL DEFAULT 'vless',
			is_active INTEGER NOT NULL DEFAULT 1,
			current_load INTEGER NOT NULL DEFAULT 0,
			reality_public_key TEXT NOT NULL DEFAULT '',
			reality_short_id TEXT NOT NULL DEFAULT '',
			ws_enabled INTEGER NOT NULL DEFAULT 0,
			ws_host TEXT NOT NULL DEFAULT '',
			ws_path TEXT NOT NULL DEFAULT '',
			awg_public_key TEXT,
			awg_endpoint TEXT
		)`,
	}
	for _, s := range stmts {
		if err := db.Exec(s).Error; err != nil {
			t.Fatalf("create schema: %v", err)
		}
	}
	return db
}

func seedTwoPlans(t *testing.T, db *gorm.DB) (free, pro model.Plan) {
	t.Helper()
	free = model.Plan{ID: uuid.NewString(), Code: "free", Name: "Free", MaxDevices: 1, MaxServers: 3, SpeedLimitMbps: 50, IsActive: true, IsSystem: true, SortOrder: 0}
	pro = model.Plan{ID: uuid.NewString(), Code: "pro", Name: "Pro", MaxDevices: 3, MaxServers: -1, SpeedLimitMbps: 0, IsActive: true, IsSystem: false, SortOrder: 10}
	if err := db.Create(&free).Error; err != nil {
		t.Fatalf("seed free: %v", err)
	}
	if err := db.Create(&pro).Error; err != nil {
		t.Fatalf("seed pro: %v", err)
	}
	return
}

func TestFindPlanByID_FoundAndNotFound(t *testing.T) {
	db := setupPlanRepoDB(t)
	_, pro := seedTwoPlans(t, db)
	got, err := repository.FindPlanByID(db, pro.ID)
	if err != nil {
		t.Fatalf("FindPlanByID: %v", err)
	}
	if got.Code != "pro" {
		t.Errorf("expected pro, got %s", got.Code)
	}
	if _, err := repository.FindPlanByID(db, "missing"); err != repository.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestFindPlanByCode_FoundAndNotFound(t *testing.T) {
	db := setupPlanRepoDB(t)
	seedTwoPlans(t, db)
	got, err := repository.FindPlanByCode(db, "pro")
	if err != nil || got.Code != "pro" {
		t.Errorf("expected pro, got %+v err=%v", got, err)
	}
	if _, err := repository.FindPlanByCode(db, "ultimate"); err != repository.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestFindSystemPlanID_HappyPath(t *testing.T) {
	db := setupPlanRepoDB(t)
	free, _ := seedTwoPlans(t, db)
	id, err := repository.FindSystemPlanID(db)
	if err != nil {
		t.Fatalf("FindSystemPlanID: %v", err)
	}
	if id != free.ID {
		t.Errorf("expected free plan id %s, got %s", free.ID, id)
	}
}

func TestListActivePlans_FiltersInactive(t *testing.T) {
	db := setupPlanRepoDB(t)
	free, pro := seedTwoPlans(t, db)
	// Deactivate pro.
	if err := db.Model(&model.Plan{}).Where("id = ?", pro.ID).Update("is_active", false).Error; err != nil {
		t.Fatalf("deactivate pro: %v", err)
	}
	plans, err := repository.ListActivePlans(db)
	if err != nil {
		t.Fatalf("ListActivePlans: %v", err)
	}
	if len(plans) != 1 || plans[0].ID != free.ID {
		t.Errorf("expected [free], got %+v", plans)
	}
}

func TestListServersForPlan_FiltersByPlanAndActive(t *testing.T) {
	db := setupPlanRepoDB(t)
	_, pro := seedTwoPlans(t, db)
	s1 := model.VPNServer{ID: uuid.NewString(), Hostname: "s1", CountryCode: "NL", IsActive: true, CurrentLoad: 10}
	s2 := model.VPNServer{ID: uuid.NewString(), Hostname: "s2", CountryCode: "DE", IsActive: true, CurrentLoad: 5}
	s3 := model.VPNServer{ID: uuid.NewString(), Hostname: "s3", CountryCode: "US", IsActive: false, CurrentLoad: 1}
	for _, s := range []model.VPNServer{s1, s2, s3} {
		if err := db.Create(&s).Error; err != nil {
			t.Fatalf("seed server: %v", err)
		}
	}
	// Plan pro gets all three (including inactive s3 in plan_servers).
	for _, s := range []model.VPNServer{s1, s2, s3} {
		if err := db.Create(&model.PlanServer{PlanID: pro.ID, ServerID: s.ID}).Error; err != nil {
			t.Fatalf("seed plan_servers: %v", err)
		}
	}
	servers, err := repository.ListServersForPlan(db, pro.ID)
	if err != nil {
		t.Fatalf("ListServersForPlan: %v", err)
	}
	if len(servers) != 2 {
		t.Fatalf("expected 2 active servers, got %d", len(servers))
	}
	// Ordered by current_load ASC.
	if servers[0].Hostname != "s2" || servers[1].Hostname != "s1" {
		t.Errorf("expected order [s2, s1], got [%s, %s]", servers[0].Hostname, servers[1].Hostname)
	}
}

func TestIsServerAllowedForPlan_TrueFalse(t *testing.T) {
	db := setupPlanRepoDB(t)
	_, pro := seedTwoPlans(t, db)
	sid := uuid.NewString()
	if err := db.Create(&model.VPNServer{ID: sid, Hostname: "s", IsActive: true}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := db.Create(&model.PlanServer{PlanID: pro.ID, ServerID: sid}).Error; err != nil {
		t.Fatalf("link: %v", err)
	}
	ok, err := repository.IsServerAllowedForPlan(db, pro.ID, sid)
	if err != nil || !ok {
		t.Errorf("expected true, got %v err=%v", ok, err)
	}
	ok, err = repository.IsServerAllowedForPlan(db, pro.ID, "non-existent")
	if err != nil || ok {
		t.Errorf("expected false, got %v err=%v", ok, err)
	}
}

func TestFindActiveOffer_ReturnsActiveOnly(t *testing.T) {
	db := setupPlanRepoDB(t)
	_, pro := seedTwoPlans(t, db)
	active := model.PlanOffer{ID: uuid.NewString(), PlanID: pro.ID, Periodicity: "MONTHLY", Currency: "USD", Amount: 5.0, IsActive: true, LavaOfferID: ptrStr("off-new")}
	inactive := model.PlanOffer{ID: uuid.NewString(), PlanID: pro.ID, Periodicity: "MONTHLY", Currency: "USD", Amount: 3.99, IsActive: false, LavaOfferID: ptrStr("off-old")}
	if err := db.Create(&active).Error; err != nil {
		t.Fatalf("seed active: %v", err)
	}
	if err := db.Create(&inactive).Error; err != nil {
		t.Fatalf("seed inactive: %v", err)
	}
	got, err := repository.FindActiveOffer(db, pro.ID, "MONTHLY", "USD")
	if err != nil {
		t.Fatalf("FindActiveOffer: %v", err)
	}
	if got.ID != active.ID {
		t.Errorf("expected active offer, got %+v", got)
	}
}

func TestFindOfferByLavaOfferID_GrandfatheredInactive(t *testing.T) {
	db := setupPlanRepoDB(t)
	_, pro := seedTwoPlans(t, db)
	// Inactive offer with lava_offer_id — must still resolve (renewal webhook).
	off := model.PlanOffer{ID: uuid.NewString(), PlanID: pro.ID, Periodicity: "MONTHLY", Currency: "USD", Amount: 3.99, IsActive: false, LavaOfferID: ptrStr("lava-old")}
	if err := db.Create(&off).Error; err != nil {
		t.Fatalf("seed offer: %v", err)
	}
	got, err := repository.FindOfferByLavaOfferID(db, "lava-old")
	if err != nil {
		t.Fatalf("FindOfferByLavaOfferID: %v", err)
	}
	if got.ID != off.ID {
		t.Errorf("PAY-08 grandfathering: expected to resolve inactive offer for renewals, got %+v", got)
	}
}

func TestSetUserPlan_UpdatesUserAndUpsertsSubscription(t *testing.T) {
	db := setupPlanRepoDB(t)
	free, pro := seedTwoPlans(t, db)
	uid := uuid.NewString()
	if err := db.Create(&model.User{ID: uid, FullName: "u", SubscriptionTier: "free", PlanID: free.ID, AuthProvider: "google"}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}

	exp := time.Now().Add(31 * 24 * time.Hour)
	contractID := "contract-abc"
	if err := repository.SetUserPlan(db, uid, pro.ID, &contractID, &exp); err != nil {
		t.Fatalf("SetUserPlan: %v", err)
	}

	// User row should reflect new plan.
	var u model.User
	if err := db.First(&u, "id = ?", uid).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if u.PlanID != pro.ID || u.SubscriptionTier != "pro" {
		t.Errorf("expected plan_id=%s tier=pro, got plan_id=%s tier=%s", pro.ID, u.PlanID, u.SubscriptionTier)
	}
	if u.SubscriptionExpiresAt == nil {
		t.Errorf("PAY-09: subscription_expires_at must be populated")
	}

	// A subscriptions row should be inserted (no prior active row).
	var sub model.Subscription
	if err := db.Where("user_id = ? AND is_active = ?", uid, true).First(&sub).Error; err != nil {
		t.Fatalf("reload subscription: %v", err)
	}
	if sub.Plan != "pro" {
		t.Errorf("expected plan=pro, got %s", sub.Plan)
	}
	if sub.LavaContractID == nil || *sub.LavaContractID != "contract-abc" {
		t.Errorf("expected lava_contract_id=contract-abc, got %v", sub.LavaContractID)
	}

	// Call again with a NEW expires_at — must update the existing row, not insert.
	newExp := exp.Add(30 * 24 * time.Hour)
	if err := repository.SetUserPlan(db, uid, pro.ID, &contractID, &newExp); err != nil {
		t.Fatalf("SetUserPlan (renewal): %v", err)
	}
	var subs []model.Subscription
	if err := db.Where("user_id = ?", uid).Find(&subs).Error; err != nil {
		t.Fatalf("count subs: %v", err)
	}
	if len(subs) != 1 {
		t.Errorf("renewal must update in place, got %d sub rows", len(subs))
	}
}

func TestSoftDeletePlan_RefusesSystemPlan(t *testing.T) {
	db := setupPlanRepoDB(t)
	free, pro := seedTwoPlans(t, db)
	if err := repository.SoftDeletePlan(db, free.ID); err != repository.ErrSystemPlan {
		t.Errorf("expected ErrSystemPlan, got %v", err)
	}
	// Non-system plan deletes fine.
	if err := repository.SoftDeletePlan(db, pro.ID); err != nil {
		t.Errorf("expected success, got %v", err)
	}
	var p model.Plan
	if err := db.First(&p, "id = ?", pro.ID).Error; err != nil {
		t.Fatalf("reload pro: %v", err)
	}
	if p.IsActive {
		t.Errorf("expected pro is_active=false after soft delete")
	}
}

func TestUpdatePlan_StripsImmutableFields(t *testing.T) {
	db := setupPlanRepoDB(t)
	_, pro := seedTwoPlans(t, db)
	updates := map[string]interface{}{
		"code":        "newcode",
		"is_system":   true,
		"id":          "tampered",
		"name":        "New Pro",
		"max_devices": 10,
	}
	got, err := repository.UpdatePlan(db, pro.ID, updates)
	if err != nil {
		t.Fatalf("UpdatePlan: %v", err)
	}
	if got.Code != "pro" {
		t.Errorf("code must remain immutable, got %s", got.Code)
	}
	if got.IsSystem {
		t.Errorf("is_system must remain false")
	}
	if got.Name != "New Pro" || got.MaxDevices != 10 {
		t.Errorf("mutable fields not updated: %+v", got)
	}
}

func TestReplaceOffer_DeactivatesOldInsertsNewInOneTx(t *testing.T) {
	db := setupPlanRepoDB(t)
	_, pro := seedTwoPlans(t, db)
	old := model.PlanOffer{ID: uuid.NewString(), PlanID: pro.ID, Periodicity: "MONTHLY", Currency: "USD", Amount: 3.99, IsActive: true, LavaOfferID: ptrStr("off-1")}
	if err := db.Create(&old).Error; err != nil {
		t.Fatalf("seed old: %v", err)
	}
	newOffer := &model.PlanOffer{ID: uuid.NewString(), PlanID: pro.ID, Periodicity: "MONTHLY", Currency: "USD", Amount: 5.0, LavaOfferID: ptrStr("off-2")}
	saved, err := repository.ReplaceOffer(db, old.ID, newOffer)
	if err != nil {
		t.Fatalf("ReplaceOffer: %v", err)
	}
	if !saved.IsActive {
		t.Errorf("new offer must be active")
	}
	// Old must be inactive now.
	var oldReloaded model.PlanOffer
	if err := db.First(&oldReloaded, "id = ?", old.ID).Error; err != nil {
		t.Fatalf("reload old: %v", err)
	}
	if oldReloaded.IsActive {
		t.Errorf("old offer must be deactivated after replace")
	}
}

func TestReplacePlanServers_AtomicReplacement(t *testing.T) {
	db := setupPlanRepoDB(t)
	_, pro := seedTwoPlans(t, db)
	s1, s2, s3 := uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, id := range []string{s1, s2, s3} {
		_ = db.Create(&model.VPNServer{ID: id, Hostname: id[:6], IsActive: true}).Error
	}
	if err := repository.ReplacePlanServers(db, pro.ID, []string{s1, s2}); err != nil {
		t.Fatalf("replace initial: %v", err)
	}
	if err := repository.ReplacePlanServers(db, pro.ID, []string{s2, s3}); err != nil {
		t.Fatalf("replace new: %v", err)
	}
	var rows []model.PlanServer
	if err := db.Where("plan_id = ?", pro.ID).Find(&rows).Error; err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 pairings, got %d", len(rows))
	}
	got := map[string]bool{}
	for _, r := range rows {
		got[r.ServerID] = true
	}
	if !got[s2] || !got[s3] || got[s1] {
		t.Errorf("expected {s2, s3}, got %+v", got)
	}
}

func TestAddPlanServer_IdempotentOnReinsert(t *testing.T) {
	db := setupPlanRepoDB(t)
	_, pro := seedTwoPlans(t, db)
	sid := uuid.NewString()
	_ = db.Create(&model.VPNServer{ID: sid, Hostname: "s", IsActive: true}).Error
	if err := repository.AddPlanServer(db, pro.ID, sid); err != nil {
		t.Fatalf("first add: %v", err)
	}
	if err := repository.AddPlanServer(db, pro.ID, sid); err != nil {
		t.Errorf("second add must be idempotent, got %v", err)
	}
	var n int64
	_ = db.Model(&model.PlanServer{}).Where("plan_id = ? AND server_id = ?", pro.ID, sid).Count(&n).Error
	if n != 1 {
		t.Errorf("expected exactly 1 pairing, got %d", n)
	}
}

func TestRemovePlanServer_ReturnsErrNotFoundWhenMissing(t *testing.T) {
	db := setupPlanRepoDB(t)
	_, pro := seedTwoPlans(t, db)
	if err := repository.RemovePlanServer(db, pro.ID, "nope"); err != repository.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}
```

    Run `cd server/api && go test ./internal/repository/ -run "TestFindPlan|TestList|TestIs|TestFindActive|TestFindOfferByLava|TestSetUserPlan|TestSoftDeletePlan|TestUpdatePlan|TestReplaceOffer|TestReplacePlanServers|TestAddPlanServer|TestRemovePlanServer" -count=1 -timeout=60s -v`.
  </action>
  <acceptance_criteria>
    - File `server/api/internal/repository/plan_repo_test.go` exists
    - `grep -c "^func Test" server/api/internal/repository/plan_repo_test.go` returns at least 13
    - `grep "TestListServersForPlan_FiltersByPlanAndActive\|TestListServersForPlan" server/api/internal/repository/plan_repo_test.go` finds one match (named test for PAY-11 in 03-VALIDATION.md)
    - `grep "TestSetUserPlan_UpdatesUserAndUpsertsSubscription" server/api/internal/repository/plan_repo_test.go` finds one match
    - `grep "TestFindOfferByLavaOfferID_GrandfatheredInactive" server/api/internal/repository/plan_repo_test.go` finds one match (PAY-08 grandfathering)
    - `cd server/api && go test ./internal/repository/ -run "TestFindPlan|TestListActivePlans|TestListServersForPlan|TestIsServerAllowedForPlan|TestFindActiveOffer|TestFindOfferByLava|TestSetUserPlan|TestSoftDelete|TestUpdatePlan|TestReplaceOffer|TestReplacePlanServers|TestAddPlanServer|TestRemovePlanServer|TestFindSystemPlanID" -count=1 -timeout=60s` exits 0
  </acceptance_criteria>
  <automated>cd server/api && go test ./internal/repository/ -run "TestFindPlan|TestListActivePlans|TestListServersForPlan|TestIsServerAllowedForPlan|TestFindActiveOffer|TestFindOfferByLava|TestSetUserPlan|TestSoftDelete|TestUpdatePlan|TestReplaceOffer|TestReplacePlanServers|TestAddPlanServer|TestRemovePlanServer|TestFindSystemPlanID" -count=1 -timeout=60s</automated>
  <done>plan_repo_test.go covers all read helpers + SetUserPlan tx + CRUD + grandfathering reverse-lookup; all tests pass on sqlite :memory:.</done>
</task>

<task type="auto">
  <id>03-03-T03</id>
  <name>Write invoice_repo.go + invoice_repo_test.go (CreateInvoice, FindInvoiceByID, FindInvoiceByLavaID, FindActivePendingInvoice 60s idempotency, UpdateInvoiceStatus)</name>
  <files>
    server/api/internal/repository/invoice_repo.go,
    server/api/internal/repository/invoice_repo_test.go
  </files>
  <read_first>
    - server/api/internal/model/invoice.go (T04 of plan 03-01)
    - server/api/internal/repository/plan_repo.go (T01 — same package; reuse ErrNotFound conventions)
    - .planning/phases/03-lava-top-plans-catalog/03-RESEARCH.md §1.1 (InvoiceResponse fields → invoices columns), §"Security Domain" row "concurrent /checkout taps double-charge" (60s idempotency window comes from ADR §9.2)
    - docs/ADR-007-lava-sso-rework.md §9.2 (60s idempotency reuse-pending), §19.6 (invoices.plan_id + plan_offer_id additions)
  </read_first>
  <action>
    Create two new files.

    **(a) `server/api/internal/repository/invoice_repo.go`:**

```go
package repository

import (
	"errors"
	"time"

	"vpnapp/server/api/internal/model"

	"gorm.io/gorm"
)

// CreateInvoice inserts an invoice row. Caller populates all required fields
// (UserID, LavaInvoiceID, OfferID, PlanID, PlanOfferID, Plan, Periodicity,
// Currency, Amount, Status="pending", PaymentURL).
func CreateInvoice(db *gorm.DB, inv *model.Invoice) error {
	if inv.Status == "" {
		inv.Status = "pending"
	}
	return db.Create(inv).Error
}

// FindInvoiceByID returns the invoice with the given primary-key UUID.
func FindInvoiceByID(db *gorm.DB, id string) (*model.Invoice, error) {
	var inv model.Invoice
	result := db.Where("id = ?", id).First(&inv)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, result.Error
	}
	return &inv, nil
}

// FindInvoiceByLavaID is the webhook handler's reverse-lookup: given the
// lava-side invoice/contract id from the payload, find the local row to update.
func FindInvoiceByLavaID(db *gorm.DB, lavaInvoiceID string) (*model.Invoice, error) {
	var inv model.Invoice
	result := db.Where("lava_invoice_id = ?", lavaInvoiceID).First(&inv)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, result.Error
	}
	return &inv, nil
}

// FindActivePendingInvoice implements the ADR §9.2 60-second idempotency
// window for /checkout double-tap protection. Returns the most recent
// invoice for (user_id, lava-side offer_id) where:
//   - status = "pending"
//   - created_at > now() - within
//
// `within` is the caller-provided window (typically 60 seconds). Returns
// ErrNotFound when no eligible invoice exists.
func FindActivePendingInvoice(db *gorm.DB, userID, lavaOfferID string, within time.Duration) (*model.Invoice, error) {
	cutoff := time.Now().Add(-within)
	var inv model.Invoice
	result := db.
		Where("user_id = ? AND offer_id = ? AND status = ? AND created_at > ?", userID, lavaOfferID, "pending", cutoff).
		Order("created_at DESC").
		First(&inv)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, result.Error
	}
	return &inv, nil
}

// UpdateInvoiceStatus sets invoices.status. Webhook handler maps lava status
// to local enum (`pending` | `paid` | `failed` | `cancelled`).
func UpdateInvoiceStatus(db *gorm.DB, id, status string) error {
	result := db.Model(&model.Invoice{}).Where("id = ?", id).Update("status", status)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
```

    **(b) `server/api/internal/repository/invoice_repo_test.go`:**

```go
package repository_test

import (
	"testing"
	"time"

	"vpnapp/server/api/internal/model"
	"vpnapp/server/api/internal/repository"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupInvoiceRepoDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec(`CREATE TABLE invoices (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		lava_invoice_id TEXT NOT NULL UNIQUE,
		offer_id TEXT NOT NULL,
		plan_id TEXT,
		plan_offer_id TEXT,
		plan TEXT NOT NULL,
		periodicity TEXT NOT NULL,
		currency TEXT NOT NULL,
		amount REAL NOT NULL,
		status TEXT NOT NULL,
		payment_url TEXT,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`).Error; err != nil {
		t.Fatalf("create invoices: %v", err)
	}
	return db
}

func TestCreateInvoice_DefaultsStatusToPending(t *testing.T) {
	db := setupInvoiceRepoDB(t)
	inv := &model.Invoice{
		ID:            uuid.NewString(),
		UserID:        uuid.NewString(),
		LavaInvoiceID: "lava-1",
		OfferID:       "off-1",
		Plan:          "pro",
		Periodicity:   "MONTHLY",
		Currency:      "USD",
		Amount:        5.0,
		// Status left empty intentionally.
	}
	if err := repository.CreateInvoice(db, inv); err != nil {
		t.Fatalf("CreateInvoice: %v", err)
	}
	if inv.Status != "pending" {
		t.Errorf("expected default status=pending, got %q", inv.Status)
	}
}

func TestFindInvoiceByID_FoundAndNotFound(t *testing.T) {
	db := setupInvoiceRepoDB(t)
	inv := &model.Invoice{ID: uuid.NewString(), UserID: uuid.NewString(), LavaInvoiceID: "lava-x", OfferID: "off", Plan: "pro", Periodicity: "MONTHLY", Currency: "USD", Amount: 5, Status: "pending"}
	_ = repository.CreateInvoice(db, inv)
	got, err := repository.FindInvoiceByID(db, inv.ID)
	if err != nil || got.LavaInvoiceID != "lava-x" {
		t.Errorf("unexpected: %+v err=%v", got, err)
	}
	if _, err := repository.FindInvoiceByID(db, "missing"); err != repository.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestFindInvoiceByLavaID_HappyPath(t *testing.T) {
	db := setupInvoiceRepoDB(t)
	inv := &model.Invoice{ID: uuid.NewString(), UserID: uuid.NewString(), LavaInvoiceID: "lava-reverse", OfferID: "off", Plan: "pro", Periodicity: "MONTHLY", Currency: "USD", Amount: 5, Status: "pending"}
	_ = repository.CreateInvoice(db, inv)
	got, err := repository.FindInvoiceByLavaID(db, "lava-reverse")
	if err != nil || got.ID != inv.ID {
		t.Errorf("unexpected: %+v err=%v", got, err)
	}
}

func TestFindActivePendingInvoice_WithinAndOutsideWindow(t *testing.T) {
	db := setupInvoiceRepoDB(t)
	uid := uuid.NewString()
	// Recent pending — inside the 60s window.
	recent := &model.Invoice{ID: uuid.NewString(), UserID: uid, LavaInvoiceID: "rec-1", OfferID: "off-a", Plan: "pro", Periodicity: "MONTHLY", Currency: "USD", Amount: 5, Status: "pending"}
	if err := repository.CreateInvoice(db, recent); err != nil {
		t.Fatalf("seed recent: %v", err)
	}
	// Outside-window pending: manually backdate by raw UPDATE.
	old := &model.Invoice{ID: uuid.NewString(), UserID: uid, LavaInvoiceID: "old-1", OfferID: "off-b", Plan: "pro", Periodicity: "MONTHLY", Currency: "USD", Amount: 5, Status: "pending"}
	if err := repository.CreateInvoice(db, old); err != nil {
		t.Fatalf("seed old: %v", err)
	}
	if err := db.Exec("UPDATE invoices SET created_at = ? WHERE id = ?", time.Now().Add(-5*time.Minute), old.ID).Error; err != nil {
		t.Fatalf("backdate: %v", err)
	}

	got, err := repository.FindActivePendingInvoice(db, uid, "off-a", 60*time.Second)
	if err != nil || got.ID != recent.ID {
		t.Errorf("expected to find recent, got %+v err=%v", got, err)
	}

	// Outside the window — must return ErrNotFound.
	if _, err := repository.FindActivePendingInvoice(db, uid, "off-b", 60*time.Second); err != repository.ErrNotFound {
		t.Errorf("expected ErrNotFound for backdated row, got %v", err)
	}
}

func TestUpdateInvoiceStatus_HappyAndMissing(t *testing.T) {
	db := setupInvoiceRepoDB(t)
	inv := &model.Invoice{ID: uuid.NewString(), UserID: uuid.NewString(), LavaInvoiceID: "lava-u", OfferID: "off", Plan: "pro", Periodicity: "MONTHLY", Currency: "USD", Amount: 5, Status: "pending"}
	_ = repository.CreateInvoice(db, inv)
	if err := repository.UpdateInvoiceStatus(db, inv.ID, "paid"); err != nil {
		t.Fatalf("UpdateInvoiceStatus: %v", err)
	}
	got, _ := repository.FindInvoiceByID(db, inv.ID)
	if got.Status != "paid" {
		t.Errorf("expected status=paid, got %q", got.Status)
	}
	if err := repository.UpdateInvoiceStatus(db, "missing", "paid"); err != repository.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}
```

    Run `cd server/api && go test ./internal/repository/ -run "TestCreateInvoice|TestFindInvoiceByID|TestFindInvoiceByLavaID|TestFindActivePendingInvoice|TestUpdateInvoiceStatus" -count=1 -timeout=30s -v`.
  </action>
  <acceptance_criteria>
    - Files `server/api/internal/repository/invoice_repo.go` and `invoice_repo_test.go` exist
    - `grep -c "^func " server/api/internal/repository/invoice_repo.go` returns 5
    - `grep "FindActivePendingInvoice" server/api/internal/repository/invoice_repo.go` finds matches (60s idempotency)
    - `grep "60\\*time.Second\\|60 \\* time.Second" server/api/internal/repository/invoice_repo_test.go` finds at least one match (window test)
    - `cd server/api && go test ./internal/repository/ -run "TestCreateInvoice|TestFindInvoiceByID|TestFindInvoiceByLavaID|TestFindActivePendingInvoice|TestUpdateInvoiceStatus" -count=1 -timeout=30s` exits 0
  </acceptance_criteria>
  <automated>cd server/api && go test ./internal/repository/ -run "TestCreateInvoice|TestFindInvoiceByID|TestFindInvoiceByLavaID|TestFindActivePendingInvoice|TestUpdateInvoiceStatus" -count=1 -timeout=30s</automated>
  <done>invoice_repo.go has 5 functions covering /checkout (CreateInvoice + 60s reuse) and webhook (FindInvoiceByLavaID + UpdateInvoiceStatus); all tests pass.</done>
</task>

</tasks>

<verification>
- `cd server/api && go build ./...` exits 0
- `cd server/api && go test ./internal/repository/ -count=1 -timeout=60s` exits 0 (all repo tests pass — existing subscription_repo + new plan_repo + new invoice_repo)
- `grep -c "^func " server/api/internal/repository/plan_repo.go` returns at least 17
- `grep -c "^func " server/api/internal/repository/invoice_repo.go` returns 5
- `grep "ErrSystemPlan" server/api/internal/repository/` (recursive) finds the var declaration in plan_repo.go AND the test assertion in plan_repo_test.go
</verification>

<must_haves>
truths:
  - "plan_repo.go exposes 17+ functions including the PAY-08 reverse lookup (FindOfferByLavaOfferID) and the PAY-11 server access enforcement (ListServersForPlan + IsServerAllowedForPlan)."
  - "SetUserPlan updates users.plan_id + users.subscription_tier + users.subscription_expires_at + subscriptions row in a single transaction (PAY-09)."
  - "FindOfferByLavaOfferID does NOT filter on is_active — grandfathered renewals must still resolve (ADR §19.10)."
  - "UpdatePlan strips `code`, `is_system`, `id` from updates map at the repository layer (defence in depth — handlers also strip)."
  - "SoftDeletePlan refuses system plans with ErrSystemPlan; D-32 §4 mitigation."
  - "invoice_repo.FindActivePendingInvoice returns the most recent pending invoice within the caller-supplied window — ADR §9.2 60s idempotency."
artifacts:
  - path: "server/api/internal/repository/plan_repo.go"
    provides: "Repository layer for plans/plan_servers/plan_offers"
    contains: "FindOfferByLavaOfferID"
  - path: "server/api/internal/repository/plan_repo_test.go"
    provides: "Sqlite-backed unit tests covering all critical paths"
    contains: "TestListServersForPlan_FiltersByPlanAndActive"
  - path: "server/api/internal/repository/invoice_repo.go"
    provides: "Invoice CRUD + 60s idempotency reuse helper"
    contains: "FindActivePendingInvoice"
key_links:
  - from: "server/api/internal/repository/plan_repo.go::SetUserPlan"
    to: "server/api/internal/repository/subscription_repo.go::CreateOrUpdateSubscription"
    via: "Same pattern (manual find+update vs insert); SetUserPlan composes user-table update with subscription upsert in one tx"
    pattern: "db.Transaction\\(func\\(tx \\*gorm.DB\\) error"
  - from: "server/api/internal/repository/plan_repo.go::FindOfferByLavaOfferID"
    to: "server/api/migrations/019_plans_catalog.sql::idx_plan_offers_lava_offer_id"
    via: "Partial unique index makes the reverse-lookup O(log n)"
    pattern: "lava_offer_id = \\?"
</must_haves>

<threat_model>
## Trust Boundaries
| Boundary | Description |
|----------|-------------|
| Handlers (Wave 3+) → repository | Handlers pass user input (plan_code, offer_id) into these functions; repository must reject mass-assignment via Updates map. |
| Webhook handler → SetUserPlan | Webhook calls SetUserPlan with planID derived from FindOfferByLavaOfferID; the chain MUST be reverse-lookup, not client-trust. |

## STRIDE Threat Register
| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-03-16 | Elevation of Privilege | UpdatePlan mass-assignment | mitigate | `UpdatePlan` deletes `code`, `is_system`, `id` keys from the `updates` map at the repository entry — defence in depth (handler ALSO validates per D-32 §4). |
| T-03-17 | Elevation of Privilege | CreatePlan force is_system=true | mitigate | `CreatePlan` forces `plan.IsSystem = false` regardless of the caller's input. Migration is the only path that sets is_system=true (D-32 §4). |
| T-03-18 | Tampering | SetUserPlan called with client-controlled planID | mitigate | This is a repository function — the THREAT is that a webhook handler passes a planID derived from client input. The downstream contract (plan 03-06) requires `planID` to come from `FindOfferByLavaOfferID(payload.contractId-resolved-offerId).PlanID`, NEVER from the webhook body. SetUserPlan itself is safe to call with any valid plan UUID — the caller's responsibility is the lookup chain. |
| T-03-19 | Tampering | SoftDeletePlan force-deletes the system plan | mitigate | `SoftDeletePlan` checks `plan.IsSystem` BEFORE the deactivation and returns `ErrSystemPlan` — handler maps to HTTP 403. Defence in depth: the partial unique index `idx_plans_one_system` at the DB layer prevents creating a second system plan even if the check is bypassed. |
| T-03-20 | Tampering | Concurrent SetUserPlan races vs admin force-cancel | accept | Phase 7 ADMIN-03 adds the per-user advisory lock. Phase 3 documents the gap — see CONTEXT.md D-32 §1 "race documented; transactional UPSERT + GORM OnConflict as best-effort". SetUserPlan IS transactional; the subscription upsert + user update commit atomically. |
| T-03-21 | DoS | ListServersForPlan join cost on plans with thousands of servers | accept | The vpn_servers table is bounded (~50 rows in production); the join is O(servers × log(plan_servers)) at the worst. No mitigation needed at this scale. Phase 6 PERF-01 caches /servers separately. |
| T-03-22 | Tampering | Negative amount in plan_offers via repository | accept | Migration 019 has `CHECK (amount >= 0)` at the DB layer; CreatePlanOffer relies on that constraint. Handler validation adds the explicit error message. |

ASVS L2 scoping per D-31: this plan IS in L2 scope (payment-path repository). Controls applied: V5 input validation via field stripping, V11 business logic (system-plan immutability + grandfathered-offer resolution), V13 access control (handlers branch on role per D-21).
</threat_model>

<success_criteria>
1. `cd server/api && go build ./...` exits 0.
2. `cd server/api && go test ./internal/repository/ -count=1 -timeout=60s` exits 0.
3. `TestListServersForPlan_FiltersByPlanAndActive` exists in plan_repo_test.go (matches PAY-11 entry in 03-VALIDATION.md).
4. `TestFindOfferByLavaOfferID_GrandfatheredInactive` exists — proves PAY-08 grandfathering works at the repo layer.
5. `TestSetUserPlan_UpdatesUserAndUpsertsSubscription` proves PAY-09 (expires_at) and the transactional upsert.
6. `grep "ErrSystemPlan" server/api/internal/repository/plan_repo.go` finds the var declaration AND its use in SoftDeletePlan.
</success_criteria>

<output>
T01, T02, T03 land as 3 atomic commits in execution mode (`feat(03-03): ...`); planner commits this plan file once with `docs(03): plan plan-repo`.
</output>
