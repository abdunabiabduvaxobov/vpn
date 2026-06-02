package handler

import (
	"testing"
)

// TestServerOrder_PerUserStablePermutation pins HARD-14 (D-22): server listing
// order is a deterministic per-user permutation.
//
// Contract (three properties):
//  1. STABLE: the same user gets the same order across repeated calls.
//  2. DISTINCT: two different users get different orders (defeats S5-2 fleet
//     enumeration via a shared, predictable order).
//  3. PERMUTATION: the ordered set is exactly the input set — no servers
//     dropped or added; ordering is applied per-request AFTER the cache read,
//     never baked into the shared cached blob.
//
// SKIP (compiling) now: the per-user HMAC ordering does not exist yet. HARD-14
// adds a deterministic sort in ListServersCached (servers.go:122) keyed by
// hmac-sha256(secret, userID+":"+serverID). When that lands, replace this skip
// with: assemble a fixed server slice, order it for userA twice (assert equal),
// order it for userB (assert different from userA), and assert both orderings
// are permutations of the original ID set. Flips GREEN when HARD-14 lands.
func TestServerOrder_PerUserStablePermutation(t *testing.T) {
	t.Skip("GREEN when HARD-14 per-user HMAC server ordering lands in ListServersCached")
}
