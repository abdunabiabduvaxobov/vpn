// RED until plan 07-08 lands; replace the t.Skip with the real assertion once
// handler.applyLavaEvent and model.LavaWebhookEvent.Status/RetriedCount exist.
package integration_test

import (
	"testing"

	"vpnapp/server/api/internal/testutil"
)

// TestWebhookReplayIdempotent is the ADMIN-06 webhook-replay proof (SC-3).
//
// GREEN behavior (implemented by plan 07-08 via handler.applyLavaEvent +
// model.LavaWebhookEvent.Status/RetriedCount):
//   - insert a payment.success lava_webhook_events row with a stored payload
//   - call handler.applyLavaEvent(ctx, db, redis, logger, ev) TWICE with the
//     same stored payload
//   - assert exactly ONE tier grant: tier=pro after the first call, and
//     subscription_expires_at UNCHANGED on the second call
//   - assert the event status flips to 'REPLAYED' and retried_count increments
//
// RED now: handler.applyLavaEvent and the Status/RetriedCount model fields do
// not exist yet, so the contract is expressed as a t.Skip until plan 07-08.
func TestWebhookReplayIdempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("integration: -short set")
	}
	db := testutil.StartPostgres(t)
	if db == nil {
		return // Docker/testcontainers unavailable — StartPostgres already skipped
	}

	t.Skip("RED: pending 07-08 — asserts handler.applyLavaEvent is idempotent: " +
		"replaying a stored payment.success grants tier exactly once " +
		"(expires_at unchanged on 2nd call) and flips status to 'REPLAYED'")
}
