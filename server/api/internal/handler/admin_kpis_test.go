// RED stubs for ADMIN-01 (KPI dashboard, plan 07-02) and ADMIN-04 (server
// drain, plan 07-06). Replace each t.Skip with the real assertion once the
// downstream symbols/fields exist.
//
// Package handler (matches webhook_lava_test.go) so these can use the existing
// in-memory SQLite setup helpers once implemented.
package handler

import "testing"

// TestAdminGetStatsKPIs is the ADMIN-01 live-KPI proof (SC-1).
//
// GREEN behavior (plan 07-02): GET /admin/stats returns a JSON body that
// includes the keys: paid_users, mrr, active_connections, signups_today,
// signups_week, signups_month, churn_30d, failed_payments_30d.
//
// RED now: those fields are absent from the /admin/stats response.
func TestAdminGetStatsKPIs(t *testing.T) {
	t.Skip("RED: pending 07-02 — asserts GET /admin/stats body includes " +
		"paid_users, mrr, active_connections, signups_today, signups_week, " +
		"signups_month, churn_30d, failed_payments_30d")
}

// TestServerDrainHidesFromPublic is the ADMIN-04 server-drain proof (SC-4).
//
// GREEN behavior (plan 07-06): after setting vpn_servers.is_draining=true, a
// non-admin GET /servers omits the draining server (repository.ListActiveServers
// applies an is_draining=false filter), while an admin still sees it.
//
// RED now: the is_draining=false filter / repository.ListActiveServers do not
// exist yet.
func TestServerDrainHidesFromPublic(t *testing.T) {
	t.Skip("RED: pending 07-06 — asserts repository.ListActiveServers applies " +
		"is_draining=false so a draining server is omitted from non-admin /servers")
}
