package handler

import "vpnapp/server/api/internal/model"

// legacyPlanLimits is a TEMPORARY package-private shim that replaces the
// previous global `model.PlanLimits` map (deleted in Phase 3 plan 03-01 per
// CONTEXT D-08 / D-24). Handlers in this package still read plan-tier
// constants synchronously, but the model package no longer owns that data —
// the canonical source is now the dynamic `plans` table populated by
// migration 019. Plans 03-03 (plan_repo) and 03-04 (server-access enforcement)
// migrate each handler to read from `repository.FindPlanByCode` /
// `repository.FindPlanByID`; once every reference below is gone, delete
// this file along with the temporary `legacyStripeID` helper. Tracked in
// the phase deferred-items / SUMMARY "Deferred Issues" list.
var legacyPlanLimits = map[string]struct {
	MaxDevices     int
	MaxServers     int // model.UnlimitedServers (-1) = no cap
	SpeedLimitMbps int // 0 = unlimited
}{
	"free":     {MaxDevices: 1, MaxServers: 3, SpeedLimitMbps: 50},
	"premium":  {MaxDevices: 3, MaxServers: model.UnlimitedServers, SpeedLimitMbps: 0},
	"ultimate": {MaxDevices: 6, MaxServers: model.UnlimitedServers, SpeedLimitMbps: 0},
	// Phase 3 introduces "pro" tier via migration 019 D-08 (max_devices=3).
	"pro": {MaxDevices: 3, MaxServers: model.UnlimitedServers, SpeedLimitMbps: 0},
}

// legacyStripeID returns the lava_contract_id stored on a Subscription as a
// plain string (empty if NULL). It bridges Stripe-era handler code that read
// `sub.StripeID` directly. Phase 3 plan 03-05 rewrites payment.go to drop
// every call site of this helper; delete it together with this file.
func legacyStripeID(sub *model.Subscription) string {
	if sub == nil || sub.LavaContractID == nil {
		return ""
	}
	return *sub.LavaContractID
}
