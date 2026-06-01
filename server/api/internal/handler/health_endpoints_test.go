// RED stub for ADMIN-07 (readyz/livez, plan 07-03). Replace the t.Skip with the
// real assertion once handler.Livez and handler.Readyz exist.
package handler

import "testing"

// TestReadyzLivez is the ADMIN-07 health-probe proof (SC-5).
//
// GREEN behavior (plan 07-03):
//   - handler.Livez always returns 200 (process is up)
//   - handler.Readyz returns 200 when all deps (Postgres, Redis) are healthy
//     and 503 when any dep is forced unhealthy (mocked probes)
//
// RED now: handler.Livez / handler.Readyz do not exist yet.
func TestReadyzLivez(t *testing.T) {
	t.Skip("RED: pending 07-03 — asserts handler.Livez always 200 and " +
		"handler.Readyz returns 503 when a dependency probe is forced unhealthy")
}
