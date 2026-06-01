package repository

import (
	"context"

	"gorm.io/gorm"
)

// WithUserLock runs fn inside a GORM transaction that first acquires a
// Postgres transaction-level advisory lock keyed on userID, so that any two
// callers using the SAME userID are serialized — the second blocks until the
// first transaction commits or rolls back. This is the ADMIN-03 mutual-exclusion
// primitive: the admin force-cancel path and the lava webhook tier-grant path
// both take WithUserLock(ctx, db, userID, ...) on the SAME key, so they can
// never interleave into a hybrid state (e.g. tier=free while the contract is
// still is_active=true, or tier=pro while the contract is cancelled).
//
// Lock key derivation: pg_advisory_xact_lock(hashtextextended(userID, 0)).
// hashtextextended maps the user_id UUID *string* to a bigint key that
// pg_advisory_xact_lock requires. The userID is passed as a bound parameter —
// never string-concatenated into the SQL — so it is injection-safe. The two
// code paths derive the SAME key because they pass the SAME resolved user_id
// (the webhook resolves inv.UserID / parent.UserID before calling this), which
// is what makes them contend (T-07-21: keys must not diverge).
//
// Auto-release: pg_advisory_xact_lock is *transaction-scoped*, so the lock is
// released automatically on COMMIT or ROLLBACK — including on a crash, because
// the backend connection's transaction ends. There is no unlock bookkeeping to
// leak (T-07-20), and a panic inside fn that GORM converts to a rollback still
// releases the lock.
//
// Idempotency note (T-07-19): WithUserLock is ADDITIVE. The webhook's
// lava_webhook_events UNIQUE dedup INSERT still provides event idempotency and
// MUST stay OUTSIDE this lock transaction (it has to commit independently — see
// webhook_event_repo.go). The advisory lock only serializes the WRITE block.
//
// Dialect handling: pg_advisory_xact_lock / hashtextextended exist ONLY on
// Postgres. Production is always Postgres, so the lock always runs there. On a
// non-Postgres dialect (the SQLite-backed unit tests that exercise the
// force-cancel handler without contending against a live webhook) the advisory
// SELECT is skipped and fn still runs inside the transaction — the test asserts
// the single-path write behavior, while the real serialization proof
// (TestForceCancelWebhookRace) runs against a real Postgres. WithUserLock NEVER
// silently degrades on Postgres; the skip is scoped strictly to non-pg dialects.
func WithUserLock(ctx context.Context, db *gorm.DB, userID string, fn func(tx *gorm.DB) error) error {
	if db == nil {
		return errNilDB
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// pg_advisory_xact_lock only exists on Postgres. Skip on other
		// dialects (test-only SQLite) so the write logic is still reachable;
		// production is always Postgres and always takes the lock.
		if tx.Dialector != nil && tx.Dialector.Name() == "postgres" {
			if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", userID).Error; err != nil {
				return err
			}
		}
		return fn(tx)
	})
}
