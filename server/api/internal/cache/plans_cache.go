package cache

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// plansPublicKeyPrefix is the Redis key namespace for the public /plans cache.
// Per CONTEXT.md D-28 the full key shape is `cache:plans:public:{currency}` —
// callers append the currency string.
const plansPublicKeyPrefix = "cache:plans:public:"

// plansPublicCacheTTL is the cache-aside TTL — short enough that an admin
// publish becomes visible within a minute even if the explicit BustPlansCache
// call from the admin handler fails for any reason.
const plansPublicCacheTTL = 60 * time.Second

// GetPlansCache returns the JSON-encoded cached body for the given currency,
// or "" with no error on cache miss / Redis outage. Callers fall through to
// a DB read on empty result.
//
// Fail-open contract (matches IsTokenBlacklisted in this same package): a
// Redis outage MUST NOT break the public /pricing page — return empty so
// the handler falls through to the slower DB path.
func GetPlansCache(ctx context.Context, client *redis.Client, currency string) (string, error) {
	if client == nil {
		return "", nil
	}
	key := plansPublicKeyPrefix + currency
	val, err := client.Get(ctx, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return "", nil // miss is not an error
		}
		// Fail open — Redis transient errors must not break /plans.
		return "", nil
	}
	return val, nil
}

// SetPlansCache writes the encoded body with the 60s TTL. The returned error
// is informational only — the handler should not propagate it. The next
// request misses cache and re-populates.
func SetPlansCache(ctx context.Context, client *redis.Client, currency, jsonBody string) error {
	if client == nil {
		return nil
	}
	key := plansPublicKeyPrefix + currency
	return client.Set(ctx, key, jsonBody, plansPublicCacheTTL).Err()
}

// BustPlansCache deletes every cache:plans:public:* key. Called by admin
// /plans/* write handlers (plan 03-08) after a successful state change.
//
// Cardinality is bounded by the currency count (3 today: USD, EUR, RUB) +
// any future locale extensions — the explicit DEL on each known currency
// is cheaper than SCAN at this scale, but SCAN scales without changes.
// Per RESEARCH §6.3 (a) we use explicit DEL.
func BustPlansCache(ctx context.Context, client *redis.Client) error {
	if client == nil {
		return nil
	}
	// Explicit DEL — bounded by the currency enum.
	keys := []string{
		plansPublicKeyPrefix + "USD",
		plansPublicKeyPrefix + "EUR",
		plansPublicKeyPrefix + "RUB",
	}
	return client.Del(ctx, keys...).Err()
}
