package repository

import (
	"time"

	"vpnapp/server/api/internal/model"

	"gorm.io/gorm"
)

// DowngradeExpiredPlans flips users with lapsed subscriptions back to the
// system plan + 'free' tier. The SQL is the ADR §19.10 cron, REVISED per the
// BLOCKER #1 fix in plan 03-06 — the `s.is_active = TRUE` qualifier was DROPPED
// because D-19 was tightened to flip subscriptions.is_active=false immediately
// on subscription.recurring.payment.failed. Without dropping is_active here,
// recurring-failed users would keep Pro forever past expires_at — the cron
// would never find them.
//
//   UPDATE users u
//      SET plan_id            = system_plan_id,
//          subscription_tier  = 'free'
//    WHERE u.plan_id != system_plan_id
//      AND EXISTS (
//          SELECT 1 FROM subscriptions s
//           WHERE s.user_id = u.id
//             AND s.expires_at IS NOT NULL
//             AND s.expires_at < now()
//      );
//
// The qualifier is now: "user is on a non-system plan AND their subscription's
// expires_at has lapsed", regardless of is_active state. This catches BOTH
// (a) users whose recurring payment failed (is_active flipped to false in 03-06
// T03) and (b) users on still-active subscriptions whose expires_at lapsed for
// any other reason (e.g. manual SQL cleanup, lava-side desync).
//
// Coordinated with plan 03-06: handleLavaRecurringFailed sets subscriptions.is_active=false per D-19.
// This cron must NOT filter on is_active or those users would keep Pro forever past expires_at.
//
// Idempotent: re-running immediately is a no-op (the first run flipped them;
// subsequent runs find no eligible rows because users.plan_id is now system).
//
// Returns the rows-affected count for logging. Safe on an empty table.
//
// Implementation note: we resolve system_plan_id once at function-entry and
// substitute it into the GORM query so the WHERE clause is a simple equality
// (vs a sub-SELECT). This makes the query plan stable across drivers — and
// crucially, makes the same call work on SQLite for unit tests (SQLite has
// limited support for correlated sub-SELECT in UPDATE FROM).
func DowngradeExpiredPlans(db *gorm.DB) (int64, error) {
	systemPlanID, err := FindSystemPlanID(db)
	if err != nil {
		return 0, err
	}
	// Resolve the user IDs first (driver-agnostic), then UPDATE them.
	// On Postgres this could be a single UPDATE ... FROM, but the two-step
	// approach is portable and the row count remains accurate.
	// NOTE: `is_active` is intentionally NOT in the WHERE clause — see the
	// doc comment above for the D-19 coordination with plan 03-06.
	var userIDs []string
	err = db.Model(&model.User{}).
		Joins("JOIN subscriptions s ON s.user_id = users.id").
		Where("users.plan_id != ? AND s.expires_at IS NOT NULL AND s.expires_at < ?",
			systemPlanID, time.Now()).
		Pluck("users.id", &userIDs).Error
	if err != nil {
		return 0, err
	}
	if len(userIDs) == 0 {
		return 0, nil
	}
	result := db.Model(&model.User{}).
		Where("id IN ?", userIDs).
		Updates(map[string]interface{}{
			"plan_id":           systemPlanID,
			"subscription_tier": "free",
		})
	return result.RowsAffected, result.Error
}
