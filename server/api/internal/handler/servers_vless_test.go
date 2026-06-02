package handler

import (
	"testing"
)

// TestServersVLESS_PerUserUUIDAllocationAndRotation pins the HARD-02 API side.
//
// Today GetServerConfig (servers.go:226) hands every user the SAME shared
// cfg.TunnelVLESSUUID (servers.go:293), so a single leaked/revoked UUID admits
// anyone and there is no per-user revocation. HARD-02 allocates a per-user VLESS
// identity, rotates it on plan change, and exposes the active set to the tunnel.
//
// Contract (three properties):
//  1. ISOLATION: two users on the SAME plan get DIFFERENT UserID UUIDs from
//     GET /servers/:id/config.
//  2. ROTATION: after a plan change the user's UUID rotates and the prior
//     identity row is marked is_active=false (so the tunnel stops admitting it).
//  3. ACTIVE-SET: GET /internal/servers/:id/vless-clients returns exactly the
//     currently-active UUID set the tunnel should admit.
//
// SKIP (compiling) now: per-user UUID storage (migration 026), the rotation
// path, and the active-set internal endpoint do not exist yet. When HARD-02
// lands, replace this skip with the three assertions above against a
// testcontainer Postgres. Flips GREEN when HARD-02 (API side) lands.
func TestServersVLESS_PerUserUUIDAllocationAndRotation(t *testing.T) {
	t.Skip("GREEN when HARD-02 migration 026 + per-user VLESS UUID allocation/rotation + /internal/servers/:id/vless-clients land")
}
