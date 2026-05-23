package handler

import "vpnapp/server/api/internal/model"

// legacyStripeID returns the lava_contract_id stored on a Subscription as a
// plain string (empty if NULL). It bridges Stripe-era handler code that read
// `sub.StripeID` directly. Phase 3 plan 03-05 rewrites payment.go to drop
// every call site of this helper; delete it together with this file.
//
// HISTORY: this file previously also exposed `legacyPlanLimits` — a hardcoded
// map mirror of model.PlanLimits added in plan 03-01's Rule-3 backward-compat
// shim. Plan 03-04 (this commit's parent plan) deletes every handler
// reference to legacyPlanLimits in favour of repository.FindPlanByID /
// FindPlanByCode / FindSystemPlanID, so the map portion of the shim is gone.
// The legacyStripeID portion remains until 03-05 finishes rewiring payment.go.
func legacyStripeID(sub *model.Subscription) string {
	if sub == nil || sub.LavaContractID == nil {
		return ""
	}
	return *sub.LavaContractID
}
