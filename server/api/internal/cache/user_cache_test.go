package cache

import (
	"context"
	"testing"
	"time"
)

// Wave 0 RED-first tests for the user:<id> existence+tier cache (PERF-04 / D-06).
//
// References plan 05's not-yet-built symbols: GetUserCache / SetUserCache /
// BustUserCache. File fails to compile until plan 05 lands them — the intended
// RED state. Signatures chosen per the plan's recommendation and RESEARCH §D-06:
//   SetUserCache(ctx, client, userID, tier string) error
//   GetUserCache(ctx, client, userID string) (tier string, found bool, err error)
//   BustUserCache(ctx, client, userID string) error
// where `found=false` means cache miss (or fail-open) → caller does the
// authoritative DB lookup. The key shape is exactly `user:<id>` and TTL ≤ 5s
// (stale Pro/tier must never be served long past a mutation).

// TestUserCacheRoundTrip: Set a tier, Get returns it with found=true.
func TestUserCacheRoundTrip(t *testing.T) {
	_, c := newMiniRedis(t)
	ctx := context.Background()
	if err := SetUserCache(ctx, c, "uid-1", "pro"); err != nil {
		t.Fatalf("SetUserCache: %v", err)
	}
	tier, found, err := GetUserCache(ctx, c, "uid-1")
	if err != nil {
		t.Fatalf("GetUserCache: %v", err)
	}
	if !found {
		t.Errorf("expected found=true after Set")
	}
	if tier != "pro" {
		t.Errorf("expected tier=pro, got %q", tier)
	}
}

// TestUserCacheMiss: Get on an unknown id returns found=false so the caller
// falls through to the authoritative DB read (never serves a fabricated tier).
func TestUserCacheMiss(t *testing.T) {
	_, c := newMiniRedis(t)
	tier, found, err := GetUserCache(context.Background(), c, "unknown")
	if err != nil {
		t.Fatalf("GetUserCache miss: %v", err)
	}
	if found {
		t.Errorf("expected found=false on miss, got tier=%q", tier)
	}
}

// TestUserCacheFailOpen: Redis down → found=false, nil error (fall through to
// DB; a Redis outage must not break AuthRequired).
func TestUserCacheFailOpen(t *testing.T) {
	mr, c := newMiniRedis(t)
	ctx := context.Background()
	if err := SetUserCache(ctx, c, "uid-1", "pro"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	mr.Close()
	tier, found, err := GetUserCache(ctx, c, "uid-1")
	if err != nil {
		t.Errorf("fail-open: expected nil error, got %v", err)
	}
	if found {
		t.Errorf("fail-open: expected found=false on outage, got tier=%q", tier)
	}
}

// TestBustUserCache: Set, Bust, then Get returns found=false. Asserts the key
// shape is exactly `user:uid-1` against the raw miniredis keyspace.
func TestBustUserCache(t *testing.T) {
	mr, c := newMiniRedis(t)
	ctx := context.Background()
	if err := SetUserCache(ctx, c, "uid-1", "pro"); err != nil {
		t.Fatalf("SetUserCache: %v", err)
	}
	if !keyExists(mr.Keys(), "user:uid-1") {
		t.Errorf("expected key user:uid-1 after Set, keys=%v", mr.Keys())
	}
	if err := BustUserCache(ctx, c, "uid-1"); err != nil {
		t.Fatalf("BustUserCache: %v", err)
	}
	_, found, _ := GetUserCache(ctx, c, "uid-1")
	if found {
		t.Errorf("expected found=false after bust")
	}
	if keyExists(mr.Keys(), "user:uid-1") {
		t.Errorf("expected user:uid-1 deleted after bust, keys=%v", mr.Keys())
	}
}

// TestUserCacheTTL: the user:<id> key must carry a short TTL (≤ 5s) so a stale
// tier self-heals quickly even if an explicit bust is missed (D-06).
func TestUserCacheTTL(t *testing.T) {
	mr, c := newMiniRedis(t)
	ctx := context.Background()
	if err := SetUserCache(ctx, c, "uid-1", "pro"); err != nil {
		t.Fatalf("SetUserCache: %v", err)
	}
	ttl := mr.TTL("user:uid-1")
	if ttl <= 0 {
		t.Fatalf("expected a positive TTL on user:uid-1, got %v", ttl)
	}
	if ttl > 5*time.Second {
		t.Errorf("expected TTL ≤ 5s on user:uid-1, got %v", ttl)
	}
}

// TestUserCacheNilClient: nil client → found=false, nil error (fail-open).
func TestUserCacheNilClient(t *testing.T) {
	tier, found, err := GetUserCache(context.Background(), nil, "uid-1")
	if err != nil || found || tier != "" {
		t.Errorf("nil client: expected (\"\", false, nil), got (%q, %v, %v)", tier, found, err)
	}
}
