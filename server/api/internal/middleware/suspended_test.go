// RED stub for ADMIN-02 per-user suspend enforcement (plan 07-07). Replace the
// t.Skip with the real assertion once middleware.SuspendedRequired exists.
package middleware_test

import "testing"

// TestSuspendedMiddleware proves a suspended user is blocked (ADMIN-02).
//
// GREEN behavior (plan 07-07): middleware.SuspendedRequired(db) reads
// users.suspended_at for the authenticated user; if set, the request is
// rejected (403/account-suspended) before reaching protected handlers; an
// un-suspended user passes through.
//
// RED now: middleware.SuspendedRequired does not exist yet.
func TestSuspendedMiddleware(t *testing.T) {
	t.Skip("RED: pending 07-07 — asserts middleware.SuspendedRequired(db) rejects " +
		"a user whose suspended_at is set and passes an un-suspended user")
}
