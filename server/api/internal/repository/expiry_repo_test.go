package repository_test

import (
	"testing"
	"time"

	"vpnapp/server/api/internal/model"
	"vpnapp/server/api/internal/repository"

	"github.com/google/uuid"
)

func TestDowngradeExpiredPlans_FlipsLapsedUsers(t *testing.T) {
	db := setupPlanRepoDB(t)
	free, pro := seedTwoPlans(t, db) // helper from plan_repo_test.go (T02 of plan 03-03)
	// User A: on pro with expired subscription → should flip to free.
	userA := uuid.NewString()
	if err := db.Create(&model.User{ID: userA, FullName: "A", SubscriptionTier: "pro", PlanID: pro.ID}).Error; err != nil {
		t.Fatalf("seed A: %v", err)
	}
	yesterday := time.Now().Add(-24 * time.Hour)
	if err := db.Create(&model.Subscription{
		ID: uuid.NewString(), UserID: userA, Plan: "pro", IsActive: true, ExpiresAt: &yesterday,
	}).Error; err != nil {
		t.Fatalf("seed sub A: %v", err)
	}

	// User B: on pro with FUTURE subscription → should stay on pro.
	userB := uuid.NewString()
	_ = db.Create(&model.User{ID: userB, FullName: "B", SubscriptionTier: "pro", PlanID: pro.ID}).Error
	future := time.Now().Add(24 * time.Hour)
	_ = db.Create(&model.Subscription{
		ID: uuid.NewString(), UserID: userB, Plan: "pro", IsActive: true, ExpiresAt: &future,
	}).Error

	// User C: already on free → should be skipped.
	userC := uuid.NewString()
	_ = db.Create(&model.User{ID: userC, FullName: "C", SubscriptionTier: "free", PlanID: free.ID}).Error

	rows, err := repository.DowngradeExpiredPlans(db)
	if err != nil {
		t.Fatalf("DowngradeExpiredPlans: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("expected 1 row flipped, got %d (%v)", len(rows), rows)
	}
	// PERF-04 / D-06: the returned id must be the downgraded user (A), so the
	// scheduler can bust user:<A>.
	if len(rows) == 1 && rows[0] != userA {
		t.Errorf("expected returned id to be userA=%s, got %s", userA, rows[0])
	}
	// Verify A flipped, B + C unchanged.
	var a, b, c model.User
	_ = db.First(&a, "id = ?", userA).Error
	_ = db.First(&b, "id = ?", userB).Error
	_ = db.First(&c, "id = ?", userC).Error
	if a.PlanID != free.ID || a.SubscriptionTier != "free" {
		t.Errorf("A should be on free: plan_id=%s tier=%s", a.PlanID, a.SubscriptionTier)
	}
	if b.PlanID != pro.ID || b.SubscriptionTier != "pro" {
		t.Errorf("B should stay on pro: plan_id=%s tier=%s", b.PlanID, b.SubscriptionTier)
	}
	if c.PlanID != free.ID {
		t.Errorf("C should remain on free: %+v", c)
	}

	// Idempotent: second call returns 0 rows.
	rows2, err := repository.DowngradeExpiredPlans(db)
	if err != nil {
		t.Fatalf("second DowngradeExpiredPlans: %v", err)
	}
	if len(rows2) != 0 {
		t.Errorf("D-26 idempotent: second call must return 0 rows, got %d (%v)", len(rows2), rows2)
	}
}

// TestRunExpiryDowngrade_FindsUsersRegardlessOfSubActive proves the D-19 BLOCKER #1
// coordination: a user whose recurring payment failed has subscriptions.is_active=FALSE
// (set in plan 03-06 T03 handleLavaRecurringFailed) but their expires_at is still in
// the past. The cron MUST still downgrade them. Before the BLOCKER #1 fix, the cron
// filtered `s.is_active = TRUE` and these users would keep Pro forever.
func TestRunExpiryDowngrade_FindsUsersRegardlessOfSubActive(t *testing.T) {
	db := setupPlanRepoDB(t)
	free, pro := seedTwoPlans(t, db)
	// User D: on pro; expires_at in the past; sub.is_active = FALSE (simulates
	// post-recurring-failed state per plan 03-06 T03 D-19 literal reading).
	userD := uuid.NewString()
	if err := db.Create(&model.User{ID: userD, FullName: "D", SubscriptionTier: "pro", PlanID: pro.ID}).Error; err != nil {
		t.Fatalf("seed D: %v", err)
	}
	yesterday := time.Now().Add(-24 * time.Hour)
	// Insert with IsActive=true so GORM emits the column at all (zero-value-bool
	// trap documented in plan 03-03 SUMMARY deviation #2), then flip to false
	// via an explicit UPDATE that includes the column in the SET clause.
	subID := uuid.NewString()
	if err := db.Create(&model.Subscription{
		ID: subID, UserID: userD, Plan: "pro", IsActive: true, ExpiresAt: &yesterday,
	}).Error; err != nil {
		t.Fatalf("seed sub D: %v", err)
	}
	if err := db.Model(&model.Subscription{}).Where("id = ?", subID).Update("is_active", false).Error; err != nil {
		t.Fatalf("flip sub D inactive: %v", err)
	}

	rows, err := repository.DowngradeExpiredPlans(db)
	if err != nil {
		t.Fatalf("DowngradeExpiredPlans: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("BLOCKER #1 / D-19: expected 1 row flipped (sub.is_active=FALSE must NOT exclude the user), got %d (%v)", len(rows), rows)
	}
	var d model.User
	_ = db.First(&d, "id = ?", userD).Error
	if d.PlanID != free.ID || d.SubscriptionTier != "free" {
		t.Errorf("BLOCKER #1 / D-19: User D with sub.is_active=FALSE + lapsed expires_at must be downgraded — got plan_id=%s tier=%s", d.PlanID, d.SubscriptionTier)
	}
}

func TestDowngradeExpiredPlans_EmptyTable_NoOp(t *testing.T) {
	db := setupPlanRepoDB(t)
	seedTwoPlans(t, db) // need system plan for FindSystemPlanID
	rows, err := repository.DowngradeExpiredPlans(db)
	if err != nil {
		t.Errorf("empty users: expected no error, got %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("empty table: expected 0 rows, got %d (%v)", len(rows), rows)
	}
}
