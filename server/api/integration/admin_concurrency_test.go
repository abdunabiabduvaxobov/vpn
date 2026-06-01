// GREEN as of plan 07-05: TestForceCancelWebhookRace proves repository.WithUserLock
// serializes the admin force-cancel path against the lava webhook tier-grant path
// on the SAME per-user advisory-lock key, so the two can never interleave into a
// hybrid state (ADMIN-03 / SC-2 / T-07-18).
//
// Plain (untagged) test file in package integration_test so it runs under the
// default `go test ./integration/`. The existing lava_sandbox_test.go carries
// //go:build integration; this file deliberately does NOT, because SC-2 is part
// of the standard suite (it skips cleanly when Docker is unavailable).
package integration_test

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"vpnapp/server/api/internal/model"
	"vpnapp/server/api/internal/repository"
	"vpnapp/server/api/internal/testutil"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// TestForceCancelWebhookRace is the ADMIN-03 advisory-lock proof (SC-2).
//
// On a real Postgres DB it seeds a Pro user with an active lava_contract, then
// launches two goroutines that contend on the SAME user via repository.WithUserLock:
//
//	G1 (admin force-cancel): tier=free + lava_contracts.is_active=false + cancelled_at set
//	G2 (webhook tier-grant): tier=pro  + lava_contracts.is_active=true  + cancelled_at cleared
//
// Because both take pg_advisory_xact_lock(hashtextextended(user_id,0)) on the
// same key, one transaction fully commits before the other begins. The final
// state is therefore ALWAYS one of the two consistent outcomes — never a hybrid
// (tier=free while the contract is active, or tier=pro while the contract is
// cancelled). The contention is repeated N times to shake out ordering
// nondeterminism; any hybrid observation fails the test.
//
// Skips cleanly when Docker/testcontainers is unavailable (StartPostgres skips),
// so CI without Docker stays green.
func TestForceCancelWebhookRace(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: -short set")
	}
	db := testutil.StartPostgres(t)
	if db == nil {
		return // Docker/testcontainers unavailable — StartPostgres already skipped
	}

	ctx := context.Background()

	// Seed the two plans the two paths flip between: the system/free plan
	// (force-cancel target) and a paid pro plan (webhook-grant target).
	systemPlanID := uuid.NewString()
	proPlanID := uuid.NewString()
	mustExec(t, db, `INSERT INTO plans (id, code, name, is_active, is_system) VALUES (?, 'free', 'Free', true, true)`, systemPlanID)
	mustExec(t, db, `INSERT INTO plans (id, code, name, is_active, is_system) VALUES (?, 'pro', 'Pro', true, false)`, proPlanID)

	const iterations = 20
	for i := 0; i < iterations; i++ {
		// Fresh user + active contract each iteration so the starting state is
		// deterministic (Pro, active contract).
		userID := uuid.NewString()
		contractID := uuid.NewString()
		mustExec(t, db,
			`INSERT INTO users (id, subscription_tier, role, plan_id, auth_provider) VALUES (?, 'pro', 'user', ?, 'guest')`,
			userID, proPlanID)
		mustExec(t, db,
			`INSERT INTO lava_contracts (id, user_id, contract_id, offer_id, plan, periodicity, currency, is_active, started_at)
			 VALUES (?, ?, ?, 'offer-1', 'pro', 'MONTHLY', 'USD', true, now())`,
			uuid.NewString(), userID, contractID)
		mustExec(t, db,
			`INSERT INTO subscriptions (id, user_id, plan, lava_contract_id, is_active, started_at)
			 VALUES (?, ?, 'pro', ?, true, now())`,
			uuid.NewString(), userID, contractID)

		// Barrier so both goroutines reach the WithUserLock call as close to
		// simultaneously as possible, maximizing real contention on the lock.
		var ready sync.WaitGroup
		ready.Add(2)
		start := make(chan struct{})
		var done sync.WaitGroup
		done.Add(2)

		// G1 — admin force-cancel: reset to system plan + mark contract cancelled.
		go func() {
			defer done.Done()
			ready.Done()
			<-start
			_ = repository.WithUserLock(ctx, db, userID, func(tx *gorm.DB) error {
				if err := repository.SetUserPlanTx(tx, userID, systemPlanID, nil, nil); err != nil {
					return err
				}
				now := time.Now()
				return tx.Model(&model.LavaContract{}).
					Where("contract_id = ?", contractID).
					Updates(map[string]interface{}{"is_active": false, "cancelled_at": &now}).Error
			})
		}()

		// G2 — webhook tier-grant: grant pro + mark contract active (clear cancel).
		go func() {
			defer done.Done()
			ready.Done()
			<-start
			exp := time.Now().Add(30 * 24 * time.Hour)
			cid := contractID
			_ = repository.WithUserLock(ctx, db, userID, func(tx *gorm.DB) error {
				if err := repository.SetUserPlanTx(tx, userID, proPlanID, &cid, &exp); err != nil {
					return err
				}
				return tx.Model(&model.LavaContract{}).
					Where("contract_id = ?", contractID).
					Updates(map[string]interface{}{"is_active": true, "cancelled_at": nil}).Error
			})
		}()

		ready.Wait()
		close(start)
		done.Wait()

		// Read the final committed state and assert it is internally consistent.
		var tier string
		var contractActive bool
		var cancelledAt *time.Time
		if err := mustQueryRow(t, db, `SELECT subscription_tier FROM users WHERE id = ?`, userID).Scan(&tier); err != nil {
			t.Fatalf("iteration %d: scan tier: %v", i, err)
		}
		if err := mustQueryRow(t, db, `SELECT is_active, cancelled_at FROM lava_contracts WHERE contract_id = ?`, contractID).
			Scan(&contractActive, &cancelledAt); err != nil {
			t.Fatalf("iteration %d: scan contract: %v", i, err)
		}

		if !isConsistent(tier, contractActive, cancelledAt) {
			t.Fatalf("iteration %d: HYBRID STATE — tier=%q contract_active=%v cancelled_at=%v; "+
				"WithUserLock failed to serialize force-cancel against webhook grant",
				i, tier, contractActive, cancelledAt)
		}
	}
}

// isConsistent returns true iff the final (tier, contract) pair is one of the
// two valid outcomes:
//   - paid-everywhere:      tier=pro,  contract is_active=true,  cancelled_at IS NULL
//   - cancelled-everywhere: tier=free, contract is_active=false, cancelled_at set
//
// Any other combination is a hybrid (the bug the advisory lock prevents).
func isConsistent(tier string, contractActive bool, cancelledAt *time.Time) bool {
	switch tier {
	case "pro":
		return contractActive && cancelledAt == nil
	case "free":
		return !contractActive && cancelledAt != nil
	default:
		return false
	}
}

func mustExec(t *testing.T, db *gorm.DB, sql string, args ...interface{}) {
	t.Helper()
	if err := db.Exec(sql, args...).Error; err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

func mustQueryRow(t *testing.T, db *gorm.DB, query string, args ...interface{}) *sql.Row {
	t.Helper()
	return db.Raw(query, args...).Row()
}
