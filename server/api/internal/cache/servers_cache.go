package cache

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// serversActiveKey is the single global Redis key holding the JSON-encoded
// `ListActiveServers` blob (the full active-server list). Per CONTEXT.md D-05
// the /servers cache is one global blob — NOT per-plan keys. The plan-scoped
// filter stays live in Go (RESEARCH Fork 3), so there are no per-plan cache
// keys to track or bust: an admin plan↔server remap is automatically correct
// because the membership intersect runs post-cache, per request.
const serversActiveKey = "cache:servers:active"

// serversActiveTTL is the cache-aside TTL. Fork 4 fixed this at 60s to match
// plansPublicCacheTTL exactly: the synchronous BustServersCache on every admin
// server-write makes the TTL a pure safety net, bounding worst-case staleness
// to one minute if a DEL ever fails (Redis hiccup mid-write).
const serversActiveTTL = 60 * time.Second

// GetServersCache returns the JSON-encoded active-server blob, or "" with no
// error on cache miss / Redis outage. Callers fall through to a DB read on the
// empty result.
//
// Fail-open contract (matches GetPlansCache / IsTokenBlacklisted in this same
// package): a Redis outage MUST NOT break /servers — return empty so the
// handler falls through to the slower DB path. /servers never breaks.
func GetServersCache(ctx context.Context, client *redis.Client) (string, error) {
	if client == nil {
		return "", nil
	}
	val, err := client.Get(ctx, serversActiveKey).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return "", nil // miss is not an error
		}
		// Fail open — Redis transient errors must not break /servers.
		return "", nil
	}
	return val, nil
}

// SetServersCache writes the encoded blob with the 60s TTL. The returned error
// is informational only — the handler should not propagate it. The next request
// misses cache and re-populates.
func SetServersCache(ctx context.Context, client *redis.Client, jsonBody string) error {
	if client == nil {
		return nil
	}
	return client.Set(ctx, serversActiveKey, jsonBody, serversActiveTTL).Err()
}

// BustServersCache deletes the cache:servers:active key. Called synchronously by
// the three admin server-write handlers (AdminCreate/Update/DeleteServer) after
// a successful state change so the next /servers request reflects the change
// within one request.
//
// Single global key — no SCAN, no per-plan keys (D-05 / Fork 3). Server writes
// bust cache:servers:active ONLY; they never touch cache:plans:public:*
// (RESEARCH Pitfall 5 — distinct namespaces).
func BustServersCache(ctx context.Context, client *redis.Client) error {
	if client == nil {
		return nil
	}
	return client.Del(ctx, serversActiveKey).Err()
}
