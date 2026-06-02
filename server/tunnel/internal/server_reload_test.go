package internal

import (
	"testing"

	"go.uber.org/zap/zaptest"
)

// xrayConfigClientIDs extracts the VLESS inbound client "id" values from the map
// that buildXRayConfig produces, so a test can assert which UUIDs the generated
// xray config admits.
func xrayConfigClientIDs(t *testing.T, cfg map[string]interface{}) []string {
	t.Helper()
	inbounds, ok := cfg["inbounds"].([]map[string]interface{})
	if !ok || len(inbounds) == 0 {
		t.Fatalf("xray config has no inbounds: %#v", cfg["inbounds"])
	}
	settings, ok := inbounds[0]["settings"].(map[string]interface{})
	if !ok {
		t.Fatalf("inbound[0] has no settings map: %#v", inbounds[0]["settings"])
	}
	clients, ok := settings["clients"].([]map[string]interface{})
	if !ok {
		t.Fatalf("settings has no clients slice: %#v", settings["clients"])
	}
	ids := make([]string, 0, len(clients))
	for _, c := range clients {
		id, _ := c["id"].(string)
		ids = append(ids, id)
	}
	return ids
}

// TestBuildXRayConfig_ClientListMatchesInputUUIDs is the buildable half of the
// HARD-02 tunnel-side contract: the generated xray VLESS inbound must admit
// EXACTLY the configured UUID set — no more, no fewer. This is the invariant
// ReloadClients must preserve when it swaps the active set. RED/real now: it
// asserts the current builder already maps Config.Clients 1:1 into the inbound
// (a guardrail that ReloadClients must not break).
func TestBuildXRayConfig_ClientListMatchesInputUUIDs(t *testing.T) {
	want := []string{
		"11111111-1111-4111-8111-111111111111",
		"22222222-2222-4222-8222-222222222222",
	}
	s, err := NewTunnelServer(&Config{
		Port:     443,
		Protocol: "vless-reality",
		Clients:  want,
		Reality: RealityConfig{
			Dest:        "example.com:443",
			ServerNames: []string{"example.com"},
			PrivateKey:  "test-private-key",
			ShortIDs:    []string{"0123abcd"},
		},
	}, zaptest.NewLogger(t))
	if err != nil {
		t.Fatalf("NewTunnelServer: %v", err)
	}

	got := xrayConfigClientIDs(t, s.buildXRayConfig())
	if len(got) != len(want) {
		t.Fatalf("xray inbound admits %d clients, want %d (%v)", len(got), len(want), got)
	}
	for i, id := range want {
		if got[i] != id {
			t.Errorf("client[%d] = %q, want %q", i, got[i], id)
		}
	}
}

// TestTunnelServer_ReloadClients pins the dynamic half of HARD-02: a
// (*TunnelServer).ReloadClients(uuids []string) error method that rebuilds the
// xray config from a NEW active-UUID set and restarts the running instance, so a
// revoked user's UUID stops being admitted without a full process restart.
//
// Intended signature:
//
//	func (s *TunnelServer) ReloadClients(uuids []string) error
//
// Behavior: replace s.config.Clients with uuids, rebuild via buildXRayConfig,
// and hot-swap the core.Instance (Close old, Start new) under s.mu — xray-core
// has no in-place hot reload, so a controlled instance swap is required.
//
// SKIP (compiling) now: ReloadClients does not exist yet. When HARD-02 (tunnel
// side) lands, replace this skip with: build a TunnelServer with set {U1},
// ReloadClients({U2}), and assert buildXRayConfig now admits exactly {U2}. Flips
// GREEN when ReloadClients lands.
func TestTunnelServer_ReloadClients(t *testing.T) {
	t.Skip("GREEN when ReloadClients lands (HARD-02): rebuilds config from new UUID set + hot-swaps the xray instance")
}
