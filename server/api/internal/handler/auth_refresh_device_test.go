package handler

import (
	"testing"
)

// TestRefreshToken_DeviceBinding pins HARD-04 (SC#4): refresh tokens are bound
// to the device that issued them.
//
// Contract:
//   - A refresh session issued for device_id="A", refreshed with device_id="B"
//     -> HTTP 401 (a stolen refresh token cannot be replayed from another
//     device).
//   - The same session refreshed with device_id="A" -> HTTP 200.
//   - An IP mismatch alone does NOT 401 (mobile clients roam networks); only the
//     device binding is hard-enforced.
//
// SKIP (compiling) now: the device_id/issue_ip binding columns do not exist
// (HARD-04 adds them via migration 025) and storeRefreshSession (auth.go:578)
// does not yet thread device_id, nor does RefreshToken (auth.go:227) check it.
// When HARD-04 lands, replace this skip with: issue a session bound to
// device "A" on a testcontainer Postgres, POST /auth/refresh with device_id="B"
// (assert 401), then device_id="A" (assert 200), then assert an IP-only
// mismatch still yields 200. Flips GREEN when HARD-04 lands.
func TestRefreshToken_DeviceBinding(t *testing.T) {
	t.Skip("GREEN when HARD-04 migration 025 (device_id/issue_ip) + RefreshToken device binding land")
}
