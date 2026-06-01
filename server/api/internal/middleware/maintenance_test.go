// RED stub for ADMIN-05/08 maintenance mode (plan 07-04). Replace the t.Skip
// with the real assertion once middleware.Maintenance exists.
package middleware_test

import "testing"

// TestMaintenanceMiddleware is the ADMIN-05 maintenance-mode proof (SC-6).
//
// GREEN behavior (plan 07-04): middleware.Maintenance(db, redis, logger) reads
// the maintenance_mode feature flag; when ON, a non-admin request gets a
// friendly 503 while an admin request still passes through to 200.
//
// RED now: middleware.Maintenance does not exist yet.
func TestMaintenanceMiddleware(t *testing.T) {
	t.Skip("RED: pending 07-04 — asserts middleware.Maintenance(db, redis, logger) " +
		"returns 503 to non-admins when the maintenance_mode flag is on, 200 to admins")
}
