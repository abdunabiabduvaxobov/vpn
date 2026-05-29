package cache

import (
	"context"
	"time"

	"vpnapp/server/api/internal/model"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// heartbeatKeyPrefix is the Redis key namespace for per-connection heartbeat
// freshness markers. The full key shape is exactly `hb:<connID>`; its value is
// the unix timestamp of the most recent beat and it carries a 600s TTL so a
// connection that stops beating self-evicts well inside the stale-sweep grace.
//
// PERF-02 / D-03 / D-04: the heartbeat handler no longer writes to Postgres per
// call (~167 write-q/s at 10k connections). Instead TouchHeartbeat pipelines the
// beat into Redis and marks the id dirty; a 10s scheduler ticker drains the
// dirty set into ONE bulk UPDATE. Postgres stays the source of truth — Redis is
// a write-coalescing buffer, not a replacement.
const heartbeatKeyPrefix = "hb:"

// heartbeatDirtySet is the Redis SET of connection ids that have beaten since
// the last flush. The 10s flush goroutine reads it (SMEMBERS), bulk-UPDATEs the
// matching connections rows, then drains the flushed ids (SREM). The shared set
// makes the flush multi-replica safe (any replica's flush is idempotent).
const heartbeatDirtySet = "hb:dirty"

// heartbeatTTL bounds how long a stale hb:<id> key survives without a beat.
// 600s sits comfortably above the 3-min StaleConnectionAfter grace, so the key
// only matters as a freshness signal — its expiry never races the sweep.
const heartbeatTTL = 600 * time.Second

// TouchHeartbeat records a heartbeat for connID entirely in Redis: it pipelines
// SET hb:<connID> <unix_ts> EX 600 together with SADD hb:dirty <connID> in a
// single round-trip. The 10s flush ticker later collapses every dirty id into
// one bulk Postgres UPDATE (the N→1 write-coalescing win, D-09 (e)).
//
// Fail-open contract (mirrors GetUserCache / GetServersCache / IsTokenBlacklisted
// in this same package): a nil client returns nil — heartbeat is NON-critical,
// the 3-min cleanup grace absorbs a missed beat and the next beat re-marks the
// id. The handler logs a non-nil error but still returns 204 regardless: a Redis
// outage on the heartbeat path must never surface as a client-visible failure
// (T-06-HB-FAILOPEN).
func TouchHeartbeat(ctx context.Context, client *redis.Client, connID string) error {
	if client == nil {
		return nil
	}
	pipe := client.Pipeline()
	pipe.Set(ctx, heartbeatKeyPrefix+connID, time.Now().Unix(), heartbeatTTL)
	pipe.SAdd(ctx, heartbeatDirtySet, connID)
	_, err := pipe.Exec(ctx)
	return err
}

// FlushHeartbeats drains the hb:dirty set into a single bulk UPDATE of the
// connections table — the heart of PERF-02's N-writes-to-1-flush collapse. It
// reads every dirty id (SMEMBERS), runs ONE
//
//	UPDATE connections SET last_heartbeat_at = now WHERE id IN (...) AND disconnected_at IS NULL
//
// using GORM's native `IN ?` slice expansion (A3 — avoids a Postgres ::uuid[]
// cast), then SREMs the flushed ids. It returns the number of ids that were
// dirty (the collapse factor for this window) and is intended to be called once
// every 10s by the scheduler.
//
// Correctness / crash semantics:
//   - nil client → (0, nil): Redis-disabled deployments / tests no-op (fail-open).
//   - empty dirty set → (0, nil): the ticker fires harmlessly between beats and
//     runs no UPDATE.
//   - on UPDATE error → (0, err) WITHOUT draining hb:dirty: the ids stay dirty so
//     the NEXT tick retries them (at-least-once flush). A crash between SMEMBERS
//     and the UPDATE loses at most one 10s window of heartbeat freshness — well
//     inside the minutes-scale stale-sweep tolerance, and Postgres remains the
//     source of truth.
//   - SREM error after a successful UPDATE is non-fatal: the same-value UPDATE is
//     idempotent, so re-flushing those ids next tick is harmless. We surface the
//     count (not the SREM error) so the ticker logs progress.
//
// The disconnected_at IS NULL guard ensures a beat for an already-disconnected
// connection is silently dropped (it never resurrects a closed session).
func FlushHeartbeats(ctx context.Context, client *redis.Client, db *gorm.DB) (int, error) {
	if client == nil {
		return 0, nil
	}

	ids, err := client.SMembers(ctx, heartbeatDirtySet).Result()
	if err != nil {
		// Fail-open read: a transient Redis error just means this tick flushes
		// nothing; the ids (if any) remain dirty for the next tick.
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}

	now := time.Now()
	res := db.WithContext(ctx).
		Model(&model.Connection{}).
		Where("id IN ? AND disconnected_at IS NULL", ids).
		Update("last_heartbeat_at", now)
	if res.Error != nil {
		// Leave hb:dirty intact so the next tick retries (at-least-once flush).
		return 0, res.Error
	}

	// Drain the flushed ids. A SREM failure is non-fatal: the idempotent UPDATE
	// re-flushes the same values next tick at zero correctness cost.
	if rerr := client.SRem(ctx, heartbeatDirtySet, ids).Err(); rerr != nil {
		// Surface the count, not the SREM error — the flush itself succeeded.
		return len(ids), nil
	}
	return len(ids), nil
}
