---
phase: 3
plan: 09
type: execute
slug: lava-top-plans-catalog
plan_number: 9
wave: 4
depends_on: [1, 3]
files_modified:
  - server/api/internal/scheduler/scheduler.go
  - server/api/internal/repository/expiry_repo.go
  - server/api/internal/repository/expiry_repo_test.go
autonomous: true
requirements_addressed: [PAY-09]
estimated_complexity: low
must_haves:
  refs:
    - "See <must_haves> XML block in plan body for canonical truths / artifacts / key_links"

---

<objective>
Add the expiry-downgrade job to `internal/scheduler/scheduler.go`. Every 10 minutes (per D-26 / ADR §19.10), run a single idempotent SQL `UPDATE` that flips users to the system plan when their `subscriptions.expires_at` is in the past AND they're not already on the system plan. Encapsulate the SQL in `repository/expiry_repo.go::DowngradeExpiredPlans` so the scheduler stays thin and the operation is testable.
</objective>

<context>
@.planning/phases/03-lava-top-plans-catalog/03-CONTEXT.md
@.planning/phases/03-lava-top-plans-catalog/03-RESEARCH.md
@docs/ADR-007-lava-sso-rework.md
@server/api/internal/scheduler/scheduler.go
@server/api/internal/repository/plan_repo.go
</context>

<interfaces>
```go
// internal/repository/expiry_repo.go (NEW)
//
// DowngradeExpiredPlans runs ADR §19.10 idempotent SQL: flips users.plan_id to
// the system plan AND users.subscription_tier='free' WHEN:
//   - users.plan_id != system_plan_id, AND
//   - they have an active subscription with expires_at < now()
//
// Returns the number of rows updated (for logging). Safe to call on an empty
// table — returns 0, nil.
func DowngradeExpiredPlans(db *gorm.DB) (int64, error)
```

Scheduler additions:
```go
// internal/scheduler/scheduler.go — runCleanup adds runExpiryDowngrade as
// one more periodic step. The scheduler interval stays at 1 minute (current
// cleanupInterval); D-26 says "every 10 minutes", so we use modulo to gate
// downgrade to roughly every 10 ticks. The cost is one extra UPDATE per 10
// minutes which is negligible.
//
// Alternative: bump the scheduler interval to 10 minutes — but that delays
// session cleanup + stale connection sweep by 9 minutes, which we don't want.
//
// Cleanest: keep the 1m cleanup interval; gate downgrade with a counter
// (every 10th tick).
```
</interfaces>

<tasks>

<task type="auto">
  <id>03-09-T01</id>
  <name>Write internal/repository/expiry_repo.go (idempotent ADR §19.10 SQL)</name>
  <files>
    server/api/internal/repository/expiry_repo.go,
    server/api/internal/repository/expiry_repo_test.go
  </files>
  <read_first>
    - docs/ADR-007-lava-sso-rework.md §19.10 (Expiry cron SQL block — verbatim)
    - server/api/internal/repository/plan_repo.go (FindSystemPlanID — used by the cron to identify the fallback plan)
    - .planning/phases/03-lava-top-plans-catalog/03-CONTEXT.md D-26 (every 10 min; SQL idempotent — no-op when nobody eligible)
  </read_first>
  <action>
    **(a) `server/api/internal/repository/expiry_repo.go`:**

```go
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
```

    **(b) `server/api/internal/repository/expiry_repo_test.go`:**

```go
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
	if rows != 1 {
		t.Errorf("expected 1 row flipped, got %d", rows)
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
	if rows2 != 0 {
		t.Errorf("D-26 idempotent: second call must return 0 rows, got %d", rows2)
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
	if err := db.Create(&model.Subscription{
		ID: uuid.NewString(), UserID: userD, Plan: "pro", IsActive: false, ExpiresAt: &yesterday,
	}).Error; err != nil {
		t.Fatalf("seed sub D: %v", err)
	}

	rows, err := repository.DowngradeExpiredPlans(db)
	if err != nil {
		t.Fatalf("DowngradeExpiredPlans: %v", err)
	}
	if rows != 1 {
		t.Errorf("BLOCKER #1 / D-19: expected 1 row flipped (sub.is_active=FALSE must NOT exclude the user), got %d", rows)
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
	if rows != 0 {
		t.Errorf("empty table: expected 0 rows, got %d", rows)
	}
}
```

    Run `cd server/api && go test ./internal/repository/ -run "TestDowngradeExpiredPlans|TestRunExpiryDowngrade" -count=1 -timeout=30s -v`.
  </action>
  <acceptance_criteria>
    - Files `server/api/internal/repository/expiry_repo.go` and `expiry_repo_test.go` exist
    - `grep "FindSystemPlanID" server/api/internal/repository/expiry_repo.go` finds one match
    - `grep "expires_at < ?\|expires_at < \\?" server/api/internal/repository/expiry_repo.go` finds at least one match
    - BLOCKER #1 fix: `grep -n "is_active" server/api/internal/repository/expiry_repo.go` finds matches ONLY in doc comments (lines starting with `//`) — NOT inside the `.Where(...)` chain. Verify with `awk '/Where\(/,/\)/' server/api/internal/repository/expiry_repo.go | grep -c "is_active"` returns 0 (zero is_active references INSIDE the GORM Where call).
    - `grep -n "Coordinated with plan 03-06" server/api/internal/repository/expiry_repo.go` finds one match (the cross-link comment to handleLavaRecurringFailed)
    - `grep "TestDowngradeExpiredPlans_FlipsLapsedUsers" server/api/internal/repository/expiry_repo_test.go` finds one match
    - `grep "TestRunExpiryDowngrade_FindsUsersRegardlessOfSubActive" server/api/internal/repository/expiry_repo_test.go` finds one match (D-19 BLOCKER #1 fix proof: sub.is_active=FALSE + lapsed expires_at still gets downgraded)
    - `grep "second call must return 0" server/api/internal/repository/expiry_repo_test.go` finds one match (idempotency proof)
    - `cd server/api && go test ./internal/repository/ -run "TestDowngradeExpiredPlans|TestRunExpiryDowngrade" -count=1 -timeout=30s` exits 0
  </acceptance_criteria>
  <automated>cd server/api && go test ./internal/repository/ -run "TestDowngradeExpiredPlans|TestRunExpiryDowngrade" -count=1 -timeout=30s</automated>
  <done>DowngradeExpiredPlans flips lapsed users to system plan + tier=free regardless of subscriptions.is_active state (D-19 BLOCKER #1 coordination with plan 03-06); idempotent on re-run; tests pass on sqlite.</done>
</task>

<task type="auto">
  <id>03-09-T02</id>
  <name>Add runExpiryDowngrade to scheduler.go (every 10 ticks → ~every 10 min)</name>
  <files>server/api/internal/scheduler/scheduler.go</files>
  <read_first>
    - server/api/internal/scheduler/scheduler.go (CURRENT — runCleanup function + 1m cleanupInterval)
    - server/api/internal/repository/expiry_repo.go (T01 of THIS plan — DowngradeExpiredPlans)
    - .planning/phases/03-lava-top-plans-catalog/03-CONTEXT.md D-26 (every 10 min; PERF-06 RUN_SCHEDULER gate is Phase 6 — Phase 3 single-replica so no-op concern here)
  </read_first>
  <action>
    Edit `server/api/internal/scheduler/scheduler.go`. Two changes:

    **(a) Add a counter field to the `scheduler` struct (lines ~18-22) and bump it in the loop:**

```go
type scheduler struct {
	ticker          *time.Ticker
	done            chan struct{}
	wg              sync.WaitGroup
	expiryTickCount int // incremented per tick; downgrade runs every 10
}
```

    **(b) Inside the goroutine loop (~lines 51-58), increment the counter and call runExpiryDowngrade on every 10th tick:**

```go
		for {
			select {
			case <-s.ticker.C:
				runCleanup(db, logger, cfg)
				s.expiryTickCount++
				// D-26: run expiry downgrade every ~10 minutes (10 ticks at 1m interval).
				if s.expiryTickCount%10 == 0 {
					runExpiryDowngrade(db, logger)
				}
			case <-s.done:
				return
			}
		}
```

    **(c) Add the `runExpiryDowngrade` helper at the BOTTOM of the file:**

```go
// runExpiryDowngrade flips users with lapsed paid subscriptions back to the
// system plan. Per ADR §19.10 / D-26. The repository function is idempotent —
// safe to call on an empty/cold DB.
//
// PERF-06 RUN_SCHEDULER gate (Phase 6): single-replica deployment in v2.2.0
// means this runs in the only API replica. When Phase 6 introduces multi-replica,
// the scheduler will be gated by the env var so only one replica runs it.
func runExpiryDowngrade(db *gorm.DB, logger *zap.Logger) {
	rows, err := repository.DowngradeExpiredPlans(db)
	if err != nil {
		logger.Error("expiry downgrade failed", zap.Error(err))
		return
	}
	if rows > 0 {
		logger.Info("expired plans downgraded to system plan", zap.Int64("count", rows))
	}
}
```

    Then `cd server/api && go build ./...` and `cd server/api && go test ./internal/scheduler/... -count=1 -timeout=30s`.
  </action>
  <acceptance_criteria>
    - `grep "expiryTickCount" server/api/internal/scheduler/scheduler.go` finds at least 2 matches (struct field + increment)
    - `grep "runExpiryDowngrade" server/api/internal/scheduler/scheduler.go` finds at least 2 matches (call site + definition)
    - `grep "expiryTickCount%10" server/api/internal/scheduler/scheduler.go` finds one match (~10 minute cadence)
    - `grep "DowngradeExpiredPlans" server/api/internal/scheduler/scheduler.go` finds one match
    - `cd server/api && go build ./...` exits 0
    - `cd server/api && go test ./internal/scheduler/... -count=1 -timeout=30s` exits 0 (existing scheduler tests still pass)
  </acceptance_criteria>
  <automated>cd server/api && go build ./... && go test ./internal/scheduler/... -count=1 -timeout=30s</automated>
  <done>scheduler.go runs DowngradeExpiredPlans every 10 ticks (~10 min); single new helper + tick-counter; existing cleanup pipeline unchanged.</done>
</task>

</tasks>

<verification>
- `cd server/api && go build ./...` exits 0
- `cd server/api && go test ./... -count=1 -timeout=300s` exits 0
- `TestDowngradeExpiredPlans_FlipsLapsedUsers` passes (PAY-09 evidence — expired users flip to system plan; non-expired users untouched)
- `TestRunExpiryDowngrade_FindsUsersRegardlessOfSubActive` passes (BLOCKER #1 D-19 coordination — sub.is_active=FALSE + lapsed expires_at users still downgrade)
- `TestDowngradeExpiredPlans_EmptyTable_NoOp` passes (D-26 idempotency on empty)
- `grep "expiryTickCount%10" server/api/internal/scheduler/scheduler.go` confirms ~10 min cadence
- `awk '/Where\(/,/\)/' server/api/internal/repository/expiry_repo.go | grep -c "is_active"` returns 0 (is_active NOT present inside the cron's GORM Where call — BLOCKER #1 fix)
</verification>

<must_haves>
truths:
  - "DowngradeExpiredPlans flips lapsed users to the system plan + tier='free' in ONE SQL statement (PAY-09 + ADR §19.10)."
  - "DowngradeExpiredPlans is idempotent — re-running returns 0 rows immediately."
  - "DowngradeExpiredPlans WHERE clause does NOT filter on subscriptions.is_active (BLOCKER #1 D-19 coordination with plan 03-06): a recurring-failed user whose subscriptions.is_active was flipped to false still gets downgraded once expires_at lapses. Without this, such users would keep Pro forever."
  - "scheduler.go runs runExpiryDowngrade approximately every 10 minutes (every 10 ticks of the existing 1-minute cleanup loop)."
  - "Existing scheduler cleanup pipeline (session cleanup, stale connections, stale devices, expired link codes, subscription expiry from HOTFIX-01) is unchanged — Phase 3 adds the plan-id flip as an additional step."
artifacts:
  - path: "server/api/internal/repository/expiry_repo.go"
    provides: "Idempotent SQL plan-downgrade"
    contains: "DowngradeExpiredPlans"
  - path: "server/api/internal/scheduler/scheduler.go"
    provides: "Periodic invocation every ~10 minutes"
    contains: "runExpiryDowngrade"
key_links:
  - from: "server/api/internal/scheduler/scheduler.go::runExpiryDowngrade"
    to: "server/api/internal/repository/expiry_repo.go::DowngradeExpiredPlans"
    via: "scheduler triggers the SQL flip every 10 ticks"
    pattern: "repository.DowngradeExpiredPlans"
</must_haves>

<threat_model>
## Trust Boundaries
| Boundary | Description |
|----------|-------------|
| In-process scheduler | No external input; reads + writes DB only. |

## STRIDE Threat Register
| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-03-70 | DoS | Scheduler fires too aggressively and floods DB | accept | Every 10 minutes; one UPDATE that touches O(lapsed_users) rows. At any reasonable scale this is bounded. |
| T-03-71 | Tampering | Scheduler downgrades a paying user mid-period | mitigate | WHERE clause requires `s.expires_at < now()` (is_active intentionally dropped per BLOCKER #1 D-19 coordination with plan 03-06). A paid user's expires_at is in the future (set by webhook payment.success / extended by recurring.success); they're never matched. A user mid-cancellation but still inside their paid period also has expires_at in the future and is unaffected. |
| T-03-72 | Repudiation | User claims their plan was downgraded incorrectly | mitigate | Each downgrade logs via `logger.Info("expired plans downgraded ... count=N")`. Phase 7 ADMIN-06 surfaces this in the UI; until then, ops can grep logs. |
| T-03-73 | Tampering | Multi-replica race (each replica downgrades the same user) | accept | Single-replica v2.2.0. Phase 6 PERF-06 introduces RUN_SCHEDULER env gate. Even today, the operation is idempotent — concurrent UPDATEs converge on the same final state. |

ASVS L1 scoping for this plan (background job, no external surface). No L2 controls needed.
</threat_model>

<success_criteria>
1. `cd server/api && go build ./...` exits 0.
2. `cd server/api && go test ./... -count=1 -timeout=300s` exits 0.
3. PAY-09 cron path verified via `TestDowngradeExpiredPlans_FlipsLapsedUsers` AND `TestRunExpiryDowngrade_FindsUsersRegardlessOfSubActive` (BLOCKER #1 D-19 coordination).
4. Scheduler tick-counter gates the downgrade to every 10 minutes (1m × 10 ticks).
5. Existing scheduler pipeline (HOTFIX-01 expired-subscription downgrade, stale connections, etc.) is intact.
</success_criteria>

<output>
T01 + T02 land as 2 atomic commits (`feat(03-09): ...`); planner commits this plan file once with `docs(03): plan expiry-cron`.
</output>
