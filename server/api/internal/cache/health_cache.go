package cache

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// healthLavaKey holds the last-known lava.top reachability verdict so /readyz
// never dials lava on every probe (RESEARCH Pitfall 3 / T-07-10 DoS mitigation).
// Value is the literal status word "ok" or "down".
const healthLavaKey = "cache:health:lava"

// healthLavaTTL bounds how long a lava-reachability verdict is trusted. 60s
// matches the ADMIN-07 ceiling: readyz reflects a lava outage within a minute
// without turning every probe into a network dial.
const healthLavaTTL = 60 * time.Second

// GetLavaReachable returns the cached lava-reachability verdict ("ok" / "down")
// or "" on cache miss / Redis outage.
//
// Fail-open contract (mirrors GetServersCache / GetPlansCache): a Redis hiccup
// returns "" so the caller treats it as a miss and performs ONE lazy refresh
// dial. It MUST NOT surface the Redis error to readyz — readyz reports the
// redis dep separately via its own Ping.
func GetLavaReachable(ctx context.Context, client *redis.Client) (string, error) {
	if client == nil {
		return "", nil
	}
	val, err := client.Get(ctx, healthLavaKey).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return "", nil // miss is not an error
		}
		// Fail open — a Redis blip must not fabricate a lava verdict.
		return "", nil
	}
	return val, nil
}

// SetLavaReachable caches the lava-reachability verdict with the 60s TTL.
// `reachable` true → "ok", false → "down". The returned error is informational;
// the caller should not propagate it (the next probe re-dials on miss).
func SetLavaReachable(ctx context.Context, client *redis.Client, reachable bool) error {
	if client == nil {
		return nil
	}
	val := "down"
	if reachable {
		val = "ok"
	}
	return client.Set(ctx, healthLavaKey, val, healthLavaTTL).Err()
}
