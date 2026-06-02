package handler

import (
	"testing"

	"vpnapp/server/api/internal/model"
)

// makeServers builds n VPNServers with ids s0..s(n-1).
func makeServers(ids []string) []model.VPNServer {
	out := make([]model.VPNServer, len(ids))
	for i, id := range ids {
		out[i] = model.VPNServer{ID: id}
	}
	return out
}

func idOrder(servers []model.VPNServer) []string {
	out := make([]string, len(servers))
	for i, s := range servers {
		out[i] = s.ID
	}
	return out
}

func sameOrder(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := map[string]int{}
	for _, x := range a {
		counts[x]++
	}
	for _, x := range b {
		counts[x]--
	}
	for _, c := range counts {
		if c != 0 {
			return false
		}
	}
	return true
}

// TestServerOrderStablePerUser verifies HARD-14: the same user gets the same
// order across repeated calls.
func TestServerOrderStablePerUser(t *testing.T) {
	ids := []string{"s0", "s1", "s2", "s3", "s4", "s5"}
	secret := "server-side-hmac-key"

	a := makeServers(ids)
	orderServersForUser(a, "user-A", secret)

	b := makeServers(ids)
	orderServersForUser(b, "user-A", secret)

	if !sameOrder(idOrder(a), idOrder(b)) {
		t.Fatalf("same user produced different orders: %v vs %v", idOrder(a), idOrder(b))
	}
}

// TestServerOrderDiffersBetweenUsers verifies two users get different orders
// (the whole anti-enumeration point). With 6 servers a collision is possible
// but improbable; we assert at least one of several user pairs differs.
func TestServerOrderDiffersBetweenUsers(t *testing.T) {
	ids := []string{"s0", "s1", "s2", "s3", "s4", "s5"}
	secret := "server-side-hmac-key"

	base := makeServers(ids)
	orderServersForUser(base, "user-A", secret)
	baseOrder := idOrder(base)

	differs := false
	for _, u := range []string{"user-B", "user-C", "user-D", "user-E"} {
		other := makeServers(ids)
		orderServersForUser(other, u, secret)
		if !sameOrder(baseOrder, idOrder(other)) {
			differs = true
			break
		}
	}
	if !differs {
		t.Fatalf("no other user produced a different order from user-A (%v)", baseOrder)
	}
}

// TestServerOrderPreservesSet verifies the reorder is a pure permutation — no
// server is added or dropped.
func TestServerOrderPreservesSet(t *testing.T) {
	ids := []string{"s0", "s1", "s2", "s3", "s4", "s5", "s6", "s7"}
	secret := "server-side-hmac-key"

	servers := makeServers(ids)
	orderServersForUser(servers, "user-Z", secret)

	if !sameSet(ids, idOrder(servers)) {
		t.Fatalf("set not preserved: before %v after %v", ids, idOrder(servers))
	}
}

// TestServerOrderEmptyAndSingle verifies degenerate slices don't panic.
func TestServerOrderEmptyAndSingle(t *testing.T) {
	var empty []model.VPNServer
	orderServersForUser(empty, "user-A", "secret") // must not panic

	single := makeServers([]string{"only"})
	orderServersForUser(single, "user-A", "secret")
	if len(single) != 1 || single[0].ID != "only" {
		t.Fatalf("single-element slice corrupted: %v", idOrder(single))
	}
}
