// RED until plan 07-05 lands; replace the t.Skip with the real assertion once
// repository.WithUserLock exists.
//
// Plain (untagged) test file in package integration_test so it runs under the
// default `go test ./integration/`. The existing lava_sandbox_test.go carries
// //go:build integration; this file deliberately does NOT, because the RED
// stub must be visible to the standard suite as a skip (not silently excluded).
package integration_test

import (
	"testing"

	"vpnapp/server/api/internal/testutil"
)

// TestForceCancelWebhookRace is the ADMIN-03 advisory-lock proof (SC-2).
//
// GREEN behavior (implemented by plan 07-05 via repository.WithUserLock):
//   - seed a Pro user on a real Postgres DB
//   - launch two goroutines concurrently:
//       1. force-cancel path: repository.WithUserLock(ctx, db, userID, fn) that
//          downgrades tier=free + marks lava_contracts.is_active=false
//       2. webhook tier-grant path: WithUserLock(...) that grants tier=pro +
//          lava_contracts.is_active=true
//   - assert the final users.subscription_tier is DETERMINISTIC and consistent
//     with lava_contracts.is_active — never a hybrid (tier=free while
//     is_active=true, or tier=pro while is_active=false).
//
// RED now: repository.WithUserLock does not exist yet (compile-fail would block
// the whole integration package), so we express the contract as a t.Skip until
// plan 07-05 lands. The StartPostgres call below keeps the real-PG wiring honest.
func TestForceCancelWebhookRace(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: -short set")
	}
	db := testutil.StartPostgres(t)
	if db == nil {
		return // Docker/testcontainers unavailable — StartPostgres already skipped
	}

	t.Skip("RED: pending 07-05 — asserts repository.WithUserLock serializes " +
		"concurrent force-cancel + webhook tier-grant so final subscription_tier " +
		"is deterministic and never hybrid with lava_contracts.is_active")
}
