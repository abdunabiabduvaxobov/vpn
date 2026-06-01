package cache

import (
	"context"
	"errors"
	"time"

	"vpnapp/server/api/internal/repository"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// flagKeyPrefix is the Redis namespace for the per-flag value cache. The full
// key shape is exactly `cache:flag:<key>` where <key> is one of the known,
// server-controlled flag names (maintenance_mode / signups_off / payments_off)
// — never a raw client string.
const flagKeyPrefix = "cache:flag:"

// flagCacheTTL is the short cache-aside TTL (T-07-28). The DB feature_flags row
// is the SOURCE OF TRUTH; this ~10s cache is the fast read path so a per-request
// flag check (the Maintenance middleware runs on every request) does not hit
// the DB each time. An admin write busts the key explicitly (BustFlag); the TTL
// is the backstop so a toggle propagates within ~10s even if the bust is missed.
const flagCacheTTL = 10 * time.Second

// GetFlag returns the boolean value of feature flag `key` using the two-layer
// read: Redis cache first, falling through to the authoritative DB row on a
// miss (and populating the cache).
//
// Fail-open contract (mirrors GetUserCache / GetPlansCache in this package):
//   - Redis hit  → return the cached bool, no DB read.
//   - Redis miss → read DB (repository.GetFeatureFlag), populate cache, return.
//   - Redis OUTAGE (non-Nil error) → read DB DIRECTLY (do not error out), so a
//     Redis failure never 503s the whole site (T-07-27). Only a real DB error
//     after the fall-through is returned.
//   - nil client → DB directly (tests / Redis-disabled deployments).
//
// The caller (Maintenance / RequireFlagOff) ALSO fails open on a returned
// error: a flag we cannot read must not lock the operator out.
func GetFlag(ctx context.Context, client *redis.Client, db *gorm.DB, key string) (bool, error) {
	if client != nil {
		val, gerr := client.Get(ctx, flagKeyPrefix+key).Result()
		switch {
		case gerr == nil:
			// Hit. "1" → true, anything else → false.
			return val == "1", nil
		case errors.Is(gerr, redis.Nil):
			// Miss → fall through to DB and populate below.
		default:
			// Redis outage — fall through to DB directly, do NOT populate.
			return repository.GetFeatureFlag(ctx, db, key)
		}
	}

	// Cache miss (or nil client) → authoritative DB read.
	value, err := repository.GetFeatureFlag(ctx, db, key)
	if err != nil {
		return false, err
	}

	// Best-effort populate so subsequent reads hit cache. A populate failure
	// is non-fatal — the next read just misses again.
	if client != nil {
		cv := "0"
		if value {
			cv = "1"
		}
		_ = client.Set(ctx, flagKeyPrefix+key, cv, flagCacheTTL).Err()
	}
	return value, nil
}

// BustFlag synchronously deletes cache:flag:<key> after an admin write so the
// new value is visible on the very next read (the ~10s TTL is only the backstop
// if this DEL fails). No-op on a nil client. A DEL error should be LOGGED by
// the caller but MUST NOT fail the admin write — the TTL bounds the staleness.
func BustFlag(ctx context.Context, client *redis.Client, key string) error {
	if client == nil {
		return nil
	}
	return client.Del(ctx, flagKeyPrefix+key).Err()
}
