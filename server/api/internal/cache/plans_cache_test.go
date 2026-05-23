package cache

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newMiniRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	c := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return mr, c
}

func TestSetAndGetPlansCache_RoundTrip(t *testing.T) {
	_, c := newMiniRedis(t)
	ctx := context.Background()
	if err := SetPlansCache(ctx, c, "USD", `{"hello":"world"}`); err != nil {
		t.Fatalf("SetPlansCache: %v", err)
	}
	got, err := GetPlansCache(ctx, c, "USD")
	if err != nil {
		t.Fatalf("GetPlansCache: %v", err)
	}
	if got != `{"hello":"world"}` {
		t.Errorf("expected payload back, got %q", got)
	}
}

func TestGetPlansCache_MissReturnsEmpty(t *testing.T) {
	_, c := newMiniRedis(t)
	got, err := GetPlansCache(context.Background(), c, "EUR")
	if err != nil {
		t.Fatalf("GetPlansCache miss: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty on miss, got %q", got)
	}
}

func TestBustPlansCache_DeletesAllCurrencies(t *testing.T) {
	mr, c := newMiniRedis(t)
	ctx := context.Background()
	for _, cur := range []string{"USD", "EUR", "RUB"} {
		if err := SetPlansCache(ctx, c, cur, `{"x":1}`); err != nil {
			t.Fatalf("seed %s: %v", cur, err)
		}
	}
	if err := BustPlansCache(ctx, c); err != nil {
		t.Fatalf("BustPlansCache: %v", err)
	}
	for _, cur := range []string{"USD", "EUR", "RUB"} {
		got, _ := GetPlansCache(ctx, c, cur)
		if got != "" {
			t.Errorf("expected %s busted, got %q", cur, got)
		}
	}
	// Sanity: no other keys leaked.
	keys := mr.Keys()
	if len(keys) != 0 {
		t.Errorf("expected empty Redis after bust, got %d keys", len(keys))
	}
}

func TestGetPlansCache_NilClient_ReturnsEmptyNoError(t *testing.T) {
	got, err := GetPlansCache(context.Background(), nil, "USD")
	if err != nil || got != "" {
		t.Errorf("nil client: expected empty + no error, got %q err=%v", got, err)
	}
}

func TestSetPlansCache_TTLExpires(t *testing.T) {
	mr, c := newMiniRedis(t)
	ctx := context.Background()
	if err := SetPlansCache(ctx, c, "USD", `{"x":1}`); err != nil {
		t.Fatalf("set: %v", err)
	}
	// Advance miniredis clock past TTL.
	mr.FastForward(61 * time.Second)
	got, _ := GetPlansCache(ctx, c, "USD")
	if got != "" {
		t.Errorf("expected expiry after 60s, got %q", got)
	}
}
