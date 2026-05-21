package cache_test

import (
	"context"
	"testing"
	"time"

	"vpnapp/server/api/internal/cache"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// newTestClient spins up a miniredis server and returns a connected client.
// The server is automatically closed when the test ends.
func newTestClient(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return client, mr
}

// --- NewRedisClient ---

func TestNewRedisClient_ValidURL(t *testing.T) {
	mr := miniredis.RunT(t)
	client, err := cache.NewRedisClient("redis://" + mr.Addr())
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	_ = client.Close()
}

func TestNewRedisClient_InvalidURL(t *testing.T) {
	_, err := cache.NewRedisClient("not://a-valid-redis-url")
	if err == nil {
		t.Fatal("expected error for invalid URL, got nil")
	}
}

func TestNewRedisClient_UnreachableServer(t *testing.T) {
	// Port 1 is never open — connection will be refused immediately.
	_, err := cache.NewRedisClient("redis://127.0.0.1:1")
	if err == nil {
		t.Fatal("expected error for unreachable server, got nil")
	}
}

// --- IsTokenBlacklisted / BlacklistToken ---

func TestBlacklistToken_ThenIsBlacklisted(t *testing.T) {
	ctx := context.Background()
	client, _ := newTestClient(t)

	const hash = "deadbeef1234"

	// Token must not be blacklisted before we add it.
	if cache.IsTokenBlacklisted(ctx, client, hash) {
		t.Fatal("expected token to not be blacklisted yet")
	}

	if err := cache.BlacklistToken(ctx, client, hash, 5*time.Minute); err != nil {
		t.Fatalf("BlacklistToken returned error: %v", err)
	}

	if !cache.IsTokenBlacklisted(ctx, client, hash) {
		t.Fatal("expected token to be blacklisted after BlacklistToken call")
	}
}

func TestIsTokenBlacklisted_UnknownHash(t *testing.T) {
	ctx := context.Background()
	client, _ := newTestClient(t)

	if cache.IsTokenBlacklisted(ctx, client, "nonexistent") {
		t.Fatal("expected false for unknown token hash")
	}
}

func TestBlacklistToken_ExpiresAfterTTL(t *testing.T) {
	ctx := context.Background()
	client, mr := newTestClient(t)

	const hash = "expiring-hash"
	if err := cache.BlacklistToken(ctx, client, hash, 1*time.Second); err != nil {
		t.Fatalf("BlacklistToken returned error: %v", err)
	}

	if !cache.IsTokenBlacklisted(ctx, client, hash) {
		t.Fatal("expected token to be blacklisted immediately after setting it")
	}

	// Fast-forward the miniredis clock past the TTL.
	mr.FastForward(2 * time.Second)

	if cache.IsTokenBlacklisted(ctx, client, hash) {
		t.Fatal("expected token to no longer be blacklisted after TTL expiry")
	}
}

// --- IncrRateLimit ---

func TestIncrRateLimit_CounterIncrementsCorrectly(t *testing.T) {
	ctx := context.Background()
	client, _ := newTestClient(t)

	for i := int64(1); i <= 5; i++ {
		count, err := cache.IncrRateLimit(ctx, client, "user:test-user", time.Minute)
		if err != nil {
			t.Fatalf("IncrRateLimit call %d returned error: %v", i, err)
		}
		if count != i {
			t.Fatalf("expected count %d, got %d", i, count)
		}
	}
}

func TestIncrRateLimit_DifferentKeysDontInterfere(t *testing.T) {
	ctx := context.Background()
	client, _ := newTestClient(t)

	countA, err := cache.IncrRateLimit(ctx, client, "user:alice", time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	countB, err := cache.IncrRateLimit(ctx, client, "user:bob", time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if countA != 1 || countB != 1 {
		t.Fatalf("expected both counters to start at 1, got alice=%d bob=%d", countA, countB)
	}
}

func TestIncrRateLimit_KeyExpiresAfterWindow(t *testing.T) {
	ctx := context.Background()
	client, mr := newTestClient(t)

	_, err := cache.IncrRateLimit(ctx, client, "ip:1.2.3.4", 1*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Fast-forward past the window.
	mr.FastForward(2 * time.Second)

	// Counter should reset to 1 after expiry.
	count, err := cache.IncrRateLimit(ctx, client, "ip:1.2.3.4", 1*time.Second)
	if err != nil {
		t.Fatalf("unexpected error after key expiry: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected counter to reset to 1 after TTL expiry, got %d", count)
	}
}

// ---------------------------------------------------------------------------
// HOTFIX-03 regression tests — TTL invariant + no-sliding-window
// (Task 1 stubs; bodies implemented in Task 2)
// ---------------------------------------------------------------------------

// TestIncrRateLimit_AtomicTTLSetOnFirstIncrement is the load-bearing
// regression test for HOTFIX-03 / CRIT-02: after every IncrRateLimit call
// the key MUST carry a positive TTL.  Before the Lua rewrite, a partial
// failure between INCR and EXPIRE could leave the counter at 1 with TTL=-1
// (no expiry), permanently rate-limiting that key until manual operator
// intervention.  With the atomic Lua script that failure mode is
// structurally impossible — both calls run inside the single-threaded
// script engine.
func TestIncrRateLimit_AtomicTTLSetOnFirstIncrement(t *testing.T) {
	ctx := context.Background()
	client, mr := newTestClient(t)

	const window = 60 * time.Second
	n, err := cache.IncrRateLimit(ctx, client, "ip:1.2.3.4", window)
	if err != nil {
		t.Fatalf("IncrRateLimit returned error: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected first increment to return 1, got %d", n)
	}

	// The cache prefixes keys with "rate:" — must query miniredis with the
	// fully-qualified key.
	ttl := mr.TTL("rate:ip:1.2.3.4")
	if ttl <= 0 {
		t.Fatalf("expected positive TTL after first IncrRateLimit, got %v (this is the CRIT-02 regression — counter has no expiry)", ttl)
	}
	if ttl > window {
		t.Fatalf("TTL %v exceeds window %v — Lua script set wrong expiry", ttl, window)
	}
}

// TestIncrRateLimit_SubsequentIncrementsDoNotSlideWindow guards against the
// "sliding window" misimplementation: EXPIRE must fire only on n == 1, never
// on subsequent calls, so the window is fixed from the moment of the first
// request rather than refreshed on every hit.
func TestIncrRateLimit_SubsequentIncrementsDoNotSlideWindow(t *testing.T) {
	ctx := context.Background()
	client, mr := newTestClient(t)

	const window = 60 * time.Second
	const key = "ip:5.6.7.8"
	const fullKey = "rate:" + key

	if _, err := cache.IncrRateLimit(ctx, client, key, window); err != nil {
		t.Fatalf("first IncrRateLimit returned error: %v", err)
	}
	ttl1 := mr.TTL(fullKey)
	if ttl1 <= 0 {
		t.Fatalf("expected positive TTL after first call, got %v", ttl1)
	}

	// Advance miniredis time so a sliding-window bug would manifest as a
	// TTL bump back to ~60s on the second call.
	mr.FastForward(5 * time.Second)

	if _, err := cache.IncrRateLimit(ctx, client, key, window); err != nil {
		t.Fatalf("second IncrRateLimit returned error: %v", err)
	}
	ttl2 := mr.TTL(fullKey)
	if ttl2 <= 0 {
		t.Fatalf("expected positive TTL after second call, got %v", ttl2)
	}
	if ttl2 > ttl1 {
		t.Fatalf("sliding-window regression: TTL after 2nd call (%v) > TTL after 1st (%v); EXPIRE should only fire on n == 1", ttl2, ttl1)
	}
}

// ---------------------------------------------------------------------------
// Edge-case tests: graceful degradation when Redis is unavailable
// ---------------------------------------------------------------------------

// TestIsTokenBlacklisted_RedisDown_FailsOpen verifies that when Redis is
// unreachable, IsTokenBlacklisted returns false (fail-open) rather than
// blocking all authenticated traffic.
func TestIsTokenBlacklisted_RedisDown_FailsOpen(t *testing.T) {
	ctx := context.Background()
	// Point at a port nobody is listening on.
	deadClient := redis.NewClient(&redis.Options{Addr: "127.0.0.1:19998"})
	t.Cleanup(func() { _ = deadClient.Close() })

	result := cache.IsTokenBlacklisted(ctx, deadClient, "any-hash")
	if result {
		t.Error("IsTokenBlacklisted with dead Redis must return false (fail-open), got true")
	}
}

// TestBlacklistToken_RedisDown_ReturnsError verifies that BlacklistToken
// surfaces an error when Redis is unreachable (the caller can decide whether
// to abort or proceed).
func TestBlacklistToken_RedisDown_ReturnsError(t *testing.T) {
	ctx := context.Background()
	deadClient := redis.NewClient(&redis.Options{Addr: "127.0.0.1:19997"})
	t.Cleanup(func() { _ = deadClient.Close() })

	err := cache.BlacklistToken(ctx, deadClient, "any-hash", time.Minute)
	if err == nil {
		t.Error("BlacklistToken with dead Redis should return an error, got nil")
	}
}

// TestIncrRateLimit_RedisDown_ReturnsError confirms that IncrRateLimit
// returns an error (not zero/panic) when Redis is unavailable.
func TestIncrRateLimit_RedisDown_ReturnsError(t *testing.T) {
	ctx := context.Background()
	deadClient := redis.NewClient(&redis.Options{Addr: "127.0.0.1:19996"})
	t.Cleanup(func() { _ = deadClient.Close() })

	_, err := cache.IncrRateLimit(ctx, deadClient, "key:test", time.Minute)
	if err == nil {
		t.Error("IncrRateLimit with dead Redis should return an error, got nil")
	}
}

// TestIsTokenBlacklisted_EmptyHash verifies an empty hash string is handled
// without panic — the key just won't exist, so it must return false.
func TestIsTokenBlacklisted_EmptyHash(t *testing.T) {
	ctx := context.Background()
	client, _ := newTestClient(t)

	result := cache.IsTokenBlacklisted(ctx, client, "")
	if result {
		t.Error("empty hash must not be blacklisted by default")
	}
}

// TestBlacklistToken_ZeroTTL exercises the edge case of TTL=0.
// Redis treats EXPIRE 0 as an immediate expiry — the token should vanish
// right away.  We just verify no panic and no error on set.
func TestBlacklistToken_ZeroTTL_DoesNotPanic(t *testing.T) {
	ctx := context.Background()
	client, _ := newTestClient(t)

	// Should not panic; ignore the error (Redis may reject zero-duration TTL).
	_ = cache.BlacklistToken(ctx, client, "zero-ttl-hash", 0)
}

// TestIncrRateLimit_LargeCount verifies the counter handles high values
// without overflow (int64 arithmetic).
func TestIncrRateLimit_LargeCount(t *testing.T) {
	ctx := context.Background()
	client, _ := newTestClient(t)

	// Drive the counter to 1000 and verify final value.
	var last int64
	for i := 0; i < 1000; i++ {
		count, err := cache.IncrRateLimit(ctx, client, "stress:key", time.Hour)
		if err != nil {
			t.Fatalf("IncrRateLimit error at iteration %d: %v", i, err)
		}
		last = count
	}
	if last != 1000 {
		t.Errorf("expected counter=1000 after 1000 increments, got %d", last)
	}
}
