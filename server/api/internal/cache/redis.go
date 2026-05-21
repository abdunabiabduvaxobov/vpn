package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// NewRedisClient parses a Redis URL and returns a connected client.
// The URL must be in the form redis://[user:password@]host[:port][/db].
// An error is returned if the URL is malformed or the server is unreachable.
func NewRedisClient(redisURL string) (*redis.Client, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parsing redis URL: %w", err)
	}

	client := redis.NewClient(opts)

	// Verify connectivity before returning.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("pinging redis: %w", err)
	}

	return client, nil
}

// blacklistKeyPrefix is the Redis key namespace for blacklisted JWT access tokens.
const blacklistKeyPrefix = "token:blacklist:"

// IsTokenBlacklisted reports whether the given token hash is present in the
// blacklist. It returns false on any Redis error so callers fail open rather
// than locking out all users during a Redis outage.
func IsTokenBlacklisted(ctx context.Context, client *redis.Client, tokenHash string) bool {
	key := blacklistKeyPrefix + tokenHash
	val, err := client.Exists(ctx, key).Result()
	if err != nil {
		// Fail open — Redis unavailability must not block all authenticated traffic.
		return false
	}
	return val > 0
}

// BlacklistToken marks a token hash as revoked with the given TTL.
// After ttl elapses the key is automatically removed by Redis.
func BlacklistToken(ctx context.Context, client *redis.Client, tokenHash string, ttl time.Duration) error {
	key := blacklistKeyPrefix + tokenHash
	if err := client.Set(ctx, key, 1, ttl).Err(); err != nil {
		return fmt.Errorf("blacklisting token: %w", err)
	}
	return nil
}

// rateLimitKeyPrefix is the Redis key namespace for per-user/per-IP rate limits.
const rateLimitKeyPrefix = "rate:"

// rateLimitScript is the canonical atomic INCR-or-SET-with-EXPIRE pattern.
// Increments the key by 1; on the very first increment (count == 1) sets the
// expiry to ARGV[1] seconds. Returns the post-increment count.
//
// Atomic by virtue of Redis's single-threaded script execution — either both
// INCR and EXPIRE run, or neither does. The previous pipeline implementation
// could leave a counter without a TTL if EXPIRE failed after INCR succeeded,
// causing permanent lockout. See CODE-REVIEW CRIT-02.
//
// go-redis v9's NewScript().Run() transparently uses EVALSHA after the first
// call (the script's SHA1 is cached server-side), so the common case is a
// single round-trip; first call after a Redis restart pays one extra EVAL.
var rateLimitScript = redis.NewScript(`
local n = redis.call('INCR', KEYS[1])
if n == 1 then
    redis.call('EXPIRE', KEYS[1], ARGV[1])
end
return n
`)

// IncrRateLimit atomically increments the request counter for the given
// rate-limit key and, on the very first increment, sets the TTL to window so
// the counter cannot persist without an expiry. Returns the current counter
// value after incrementing. Single Redis round-trip in the common case
// (EVALSHA after the script is cached).
//
// Closes CODE-REVIEW CRIT-02. Implements ROADMAP §Phase 1 success criterion
// #6 ("every IncrRateLimit call leaves the counter with a positive TTL"). The
// pre-fix two-step pipeline (INCR then conditional EXPIRE) is replaced with a
// Lua script so partial state — INCR succeeded but EXPIRE never ran — is
// structurally impossible.
//
// Window semantics: fixed-from-first-call, NOT sliding. EXPIRE only fires
// when n == 1, mirroring the original behaviour so existing callers in
// middleware/ratelimit.go don't shift behaviour.
func IncrRateLimit(ctx context.Context, client *redis.Client, key string, window time.Duration) (int64, error) {
	fullKey := rateLimitKeyPrefix + key
	seconds := int64(window.Seconds())

	result, err := rateLimitScript.Run(ctx, client, []string{fullKey}, seconds).Int64()
	if err != nil {
		return 0, fmt.Errorf("rate limit script: %w", err)
	}
	return result, nil
}
