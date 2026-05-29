package cache

import (
	"context"
	"testing"
)

// Wave 0 RED-first tests for the cache:servers:active blob (PERF-01 / D-05).
//
// These reference symbols that plan 04 will create — GetServersCache,
// SetServersCache, BustServersCache — so this file fails to COMPILE until the
// implementation lands. That compile failure (undefined: GetServersCache ...)
// is the intended Wave 0 state; it proves the test is wired to the exact
// production symbol names the implementation must provide.
//
// Pattern mirrors plans_cache_test.go exactly (miniredis + cache-aside +
// fail-open + bust). The key constant under test is `cache:servers:active`
// (RESEARCH §Pattern 1). newMiniRedis is shared from plans_cache_test.go in
// this same package.

// TestServersCacheRoundTrip: Set then Get returns the exact JSON blob.
func TestServersCacheRoundTrip(t *testing.T) {
	_, c := newMiniRedis(t)
	ctx := context.Background()
	body := `[{"id":"s1"}]`
	if err := SetServersCache(ctx, c, body); err != nil {
		t.Fatalf("SetServersCache: %v", err)
	}
	got, err := GetServersCache(ctx, c)
	if err != nil {
		t.Fatalf("GetServersCache: %v", err)
	}
	if got != body {
		t.Errorf("expected %q back, got %q", body, got)
	}
}

// TestServersCacheMissReturnsEmpty: Get on empty Redis returns "" + nil error
// (a miss is NOT an error — the handler falls through to the DB).
func TestServersCacheMissReturnsEmpty(t *testing.T) {
	_, c := newMiniRedis(t)
	got, err := GetServersCache(context.Background(), c)
	if err != nil {
		t.Fatalf("GetServersCache miss: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty on miss, got %q", got)
	}
}

// TestServersCacheFailOpen: with Redis down (miniredis closed), Get must return
// "" + nil error — never propagate a Redis error (fail-open contract, D-05).
func TestServersCacheFailOpen(t *testing.T) {
	mr, c := newMiniRedis(t)
	ctx := context.Background()
	if err := SetServersCache(ctx, c, `[{"id":"s1"}]`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	mr.Close() // simulate Redis outage
	got, err := GetServersCache(ctx, c)
	if err != nil {
		t.Errorf("fail-open: expected nil error on Redis outage, got %v", err)
	}
	if got != "" {
		t.Errorf("fail-open: expected empty on Redis outage, got %q", got)
	}
}

// TestBustServersCache: Set, then Bust, then Get returns "". Also asserts the
// key shape is exactly `cache:servers:active` by inspecting the raw miniredis
// keyspace before and after the bust (T-06-W0-01: the test must validate the
// production key, not a test-local alias).
func TestBustServersCache(t *testing.T) {
	mr, c := newMiniRedis(t)
	ctx := context.Background()
	if err := SetServersCache(ctx, c, `[{"id":"s1"}]`); err != nil {
		t.Fatalf("SetServersCache: %v", err)
	}
	// The exact production key constant must be cache:servers:active.
	if !keyExists(mr.Keys(), "cache:servers:active") {
		t.Errorf("expected key cache:servers:active to exist after Set, keys=%v", mr.Keys())
	}
	if err := BustServersCache(ctx, c); err != nil {
		t.Fatalf("BustServersCache: %v", err)
	}
	got, _ := GetServersCache(ctx, c)
	if got != "" {
		t.Errorf("expected empty after bust, got %q", got)
	}
	if keyExists(mr.Keys(), "cache:servers:active") {
		t.Errorf("expected cache:servers:active deleted after bust, keys=%v", mr.Keys())
	}
}

// TestServersCacheNilClient: a nil Redis client is treated as a permanent
// miss (fail-open) — no panic, "" + nil error.
func TestServersCacheNilClient(t *testing.T) {
	got, err := GetServersCache(context.Background(), nil)
	if err != nil || got != "" {
		t.Errorf("nil client: expected empty + no error, got %q err=%v", got, err)
	}
}

// keyExists is a tiny helper to assert presence of a key in miniredis.Keys().
func keyExists(keys []string, want string) bool {
	for _, k := range keys {
		if k == want {
			return true
		}
	}
	return false
}
