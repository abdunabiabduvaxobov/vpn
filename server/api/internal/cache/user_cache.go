package cache

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// userKeyPrefix is the Redis key namespace for the per-user existence+tier
// cache (PERF-04 / D-06). The full key shape is exactly `user:<id>` where
// <id> is the validated user UUID (the JWT `sub` on the read path, the
// :id path/inv.UserID/parent.UserID on the bust paths) — never a raw,
// unvalidated client string (T-06-USERKEY: accept).
const userKeyPrefix = "user:"

// userCacheTTL is the cache-aside TTL — deliberately tiny (≤5s) so a stale
// tier self-heals fast even if an explicit BustUserCache call is missed.
//
// The TTL is the BACKSTOP, not the freshness guarantee: the real guarantee
// is the SYNCHRONOUS BustUserCache on every mutation path (admin update,
// webhook Pro-grant, delete/restore, both bulk downgrades). The short TTL
// bounds the worst-case entitlement-staleness window to one bust-failure +
// 5s — which is why a downgraded/expired/deleted user can never keep Pro
// (T-06-USERCACHE: mitigate). role/tier for AUTHZ still come from the JWT
// claim + the AdminRequired DB re-read (HOTFIX-02); this cache only fronts
// the EXISTENCE gate, so a stale cache value cannot grant admin.
const userCacheTTL = 5 * time.Second

// GetUserCache returns the cached subscription tier for userID and found=true
// on a cache hit, or ("", false, nil) on a miss / Redis outage.
//
// Fail-open contract (mirrors GetPlansCache / GetServersCache / IsTokenBlacklisted
// in this same package): ANY Redis error — redis.Nil (miss) OR a transient
// connection error (outage) — returns found=false with a nil error, so the
// caller falls through to the authoritative DB existence check. AuthRequired
// must never break on a Redis outage, and the deleted-user 401 guard still
// fires from the DB on the fall-through path (T-06-USERCACHE-FAILOPEN).
//
// RESEARCH Pitfall 6 — never cache a negative: a miss (key absent) means
// "ask the DB", NOT "user does not exist". Presence of the key = the user
// existed as of the last populate, within the 5s TTL.
func GetUserCache(ctx context.Context, client *redis.Client, userID string) (tier string, found bool, err error) {
	if client == nil {
		return "", false, nil
	}
	val, gerr := client.Get(ctx, userKeyPrefix+userID).Result()
	if gerr != nil {
		if errors.Is(gerr, redis.Nil) {
			return "", false, nil // miss → DB authoritative
		}
		// Fail open — a Redis outage must not break AuthRequired.
		return "", false, nil
	}
	return val, true, nil
}

// SetUserCache writes user:<id> = tier with the short TTL. Best-effort: the
// returned error is informational only — a failed populate just means the
// next request misses cache and re-reads the DB (no correctness impact).
//
// The value is the bare tier string (e.g. "pro"); the PRESENCE of the key is
// the existence signal. We deliberately do NOT serialize the full user row —
// that would add a second staleness surface for fields the cache does not
// need (D-07 keeps the full *model.User in c.Locals on the miss-load path
// instead).
func SetUserCache(ctx context.Context, client *redis.Client, userID, tier string) error {
	if client == nil {
		return nil
	}
	return client.Set(ctx, userKeyPrefix+userID, tier, userCacheTTL).Err()
}

// BustUserCache synchronously deletes user:<id>. Called by EVERY mutation path
// that changes a user's existence/tier/expiry (admin update, webhook Pro-grant,
// user delete / DeleteOrphanGuestUser / PerformRestore, and both bulk expiry
// downgrades) so a downgraded/expired/deleted user never keeps stale Pro
// (the EoP threat — T-06-USERCACHE).
//
// On a nil client this is a no-op (tests / Redis-disabled deployments). A DEL
// error should be LOGGED by the caller but MUST NOT alter the caller's
// success contract (e.g. the webhook 200/500 idempotency contract) — the 5s
// TTL is the backstop if a single DEL fails.
func BustUserCache(ctx context.Context, client *redis.Client, userID string) error {
	if client == nil {
		return nil
	}
	return client.Del(ctx, userKeyPrefix+userID).Err()
}
