// Package scheduler runs background maintenance tasks on a fixed interval.
package scheduler

import (
	"context"
	"sync"
	"time"

	"vpnapp/server/api/internal/cache"
	"vpnapp/server/api/internal/config"
	"vpnapp/server/api/internal/repository"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

const cleanupInterval = 1 * time.Minute

// heartbeatFlushInterval is the dedicated cadence for draining the Redis
// hb:dirty set into one bulk Postgres UPDATE (PERF-02 / D-03). 10s is NOT
// reachable from the 1-min cleanup tick, so it runs on its OWN ticker +
// goroutine (RESEARCH Pitfall 2). A missed flush window costs at most ~10s of
// heartbeat freshness — well inside the 3-min stale-sweep grace.
const heartbeatFlushInterval = 10 * time.Second

// Per-job tick intervals (D-10c). The base cleanup ticker fires every minute;
// each job runs when cleanupTickCount % interval == 0, so rarely-needed jobs
// defer to less-frequent ticks instead of running every minute. Documented as
// "every N ticks at the 1-min cleanup cadence".
const (
	sessionCleanupEveryTicks  = 5     // DeleteExpiredSessions — every ~5 min
	staleDeviceEveryTicks     = 1440  // DeleteStaleDevices — daily (1440 min)
	expiryDowngradeEveryTicks = 10    // DowngradeExpiredPlans — every ~10 min (existing D-26 cadence)
	pruneConnectionEveryTicks = 10080 // 90-day connection prune — weekly (7*24*60 min)
)

// connectionPruneAge is the retention horizon for disconnected connection rows.
// Rows whose disconnected_at is older than this are pruned weekly (PERF-08).
const connectionPruneAge = 90 * 24 * time.Hour

// scheduler is the internal state for the background worker.
type scheduler struct {
	ticker      *time.Ticker // 1-min cleanup cadence
	flushTicker *time.Ticker // 10s heartbeat-flush cadence (PERF-02)
	done        chan struct{}
	wg          sync.WaitGroup
	// cleanupTickCount increments once per 1-min cleanup tick and drives the
	// per-job modulo registry in runCleanup (D-10c). It also subsumes the old
	// expiryTickCount (expiry downgrade every expiryDowngradeEveryTicks).
	cleanupTickCount int
}

// global instance — only one scheduler is expected per process.
var instance *scheduler
var mu sync.Mutex

// Start launches the background goroutine that cleans up expired sessions
// once per cleanupInterval. Calling Start more than once is safe — subsequent
// calls are no-ops if a scheduler is already running.
//
// PERF-04 / D-06: redisClient is threaded through so the bulk-downgrade passes
// can synchronously bust user:<id> for every user they flip to free (zero-lag
// entitlement freshness everywhere, not just admin/webhook). redisClient may
// be nil (Redis-disabled deployments / tests) — BustUserCache no-ops on nil.
func Start(db *gorm.DB, logger *zap.Logger, cfg *config.Config, redisClient *redis.Client) {
	mu.Lock()
	defer mu.Unlock()

	if instance != nil {
		return
	}

	s := &scheduler{
		ticker:      time.NewTicker(cleanupInterval),
		flushTicker: time.NewTicker(heartbeatFlushInterval),
		done:        make(chan struct{}),
	}

	// Goroutine 1: the 1-min cleanup / expiry / weekly-prune cadence.
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()

		// Run once immediately so the first cleanup does not wait a full interval.
		// The boot pass runs EVERY registry job once (tickCount 0 ⇒ 0 % N == 0
		// for all N), so a restart cleans expired sessions/devices/old
		// connections right away — preserving the long-standing "cleanup on
		// start" contract. cleanupTickCount stays 0 here; the per-job modulo
		// cadence (D-10c) then governs subsequent ticks (first ticker tick is 1).
		runCleanup(db, logger, cfg, redisClient, s.cleanupTickCount)

		for {
			select {
			case <-s.ticker.C:
				s.cleanupTickCount++
				runCleanup(db, logger, cfg, redisClient, s.cleanupTickCount)
				// D-26: run expiry downgrade every ~10 ticks (10 min at 1m interval).
				if s.cleanupTickCount%expiryDowngradeEveryTicks == 0 {
					runExpiryDowngrade(db, logger, redisClient)
				}
			case <-s.done:
				return
			}
		}
	}()

	// Goroutine 2: the dedicated 10s heartbeat-flush cadence (PERF-02 / Pitfall
	// 2 — 10s is not reachable from the 1-min tick). FlushHeartbeats is a no-op
	// when redisClient is nil (fail-open), so this is safe in Redis-disabled
	// deployments. Tracked by the SAME wg + done so Stop() waits both goroutines.
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			select {
			case <-s.flushTicker.C:
				count, err := cache.FlushHeartbeats(context.Background(), redisClient, db)
				if err != nil {
					logger.Warn("heartbeat flush failed (dirty set retained for next tick)", zap.Error(err))
				} else if count > 0 {
					logger.Debug("heartbeats flushed to Postgres", zap.Int("count", count))
				}
			case <-s.done:
				return
			}
		}
	}()

	instance = s
	logger.Info("session cleanup scheduler started",
		zap.Duration("cleanup_interval", cleanupInterval),
		zap.Duration("heartbeat_flush_interval", heartbeatFlushInterval),
	)
}

// Stop signals the background goroutine to exit and waits for it to finish.
// Safe to call even if Start was never called.
func Stop() {
	mu.Lock()
	s := instance
	instance = nil
	mu.Unlock()

	if s == nil {
		return
	}

	s.ticker.Stop()
	s.flushTicker.Stop()
	close(s.done)
	s.wg.Wait()
}

// runCleanup runs the per-tick maintenance jobs. The per-job interval registry
// (D-10c) keys each job off tickCount so rarely-needed jobs defer to
// less-frequent ticks instead of running every minute:
//
//	every tick   (1 min)  — CleanupStaleReservations, CleanupStaleConnections,
//	                         DeleteExpiredLinkCodes, DowngradeExpiredSubscriptions
//	every 5 ticks (5 min) — DeleteExpiredSessions
//	every 1440    (daily) — DeleteStaleDevices
//	every 10080   (weekly)— PruneOldConnections (90-day prune, PERF-08)
//
// The expiry-PLAN downgrade (DowngradeExpiredPlans) is driven by the caller at
// expiryDowngradeEveryTicks (10 min) — see Start().
func runCleanup(db *gorm.DB, logger *zap.Logger, cfg *config.Config, redisClient *redis.Client, tickCount int) {
	// --- every tick (1 min) ---

	// Clean stale reservations (connecting for >2 min)
	reservationCount, err := repository.CleanupStaleReservations(db, 2*time.Minute)
	if err != nil {
		logger.Error("stale reservation cleanup failed", zap.Error(err))
	} else if reservationCount > 0 {
		logger.Info("stale reservations cleaned up", zap.Int64("count", reservationCount))
	}

	// Clean stale connections (no heartbeat for cfg.StaleConnectionAfter).
	// Stays at the 1-min cadence: this is the grace window that absorbs a
	// missed Redis flush, so it must run frequently.
	staleCount, err := repository.CleanupStaleConnections(db, cfg.StaleConnectionAfter)
	if err != nil {
		logger.Error("stale connection cleanup failed", zap.Error(err))
	} else if staleCount > 0 {
		logger.Info("stale connections cleaned up", zap.Int64("count", staleCount))
	}

	// Delete expired share/link codes so the table does not grow unbounded.
	// Each code is short-lived (cfg.LinkCodeTTL) and one-time-use anyway, so
	// this only catches codes that were generated but never redeemed.
	codeCount, err := repository.DeleteExpiredLinkCodes(db)
	if err != nil {
		logger.Error("expired link code cleanup failed", zap.Error(err))
	} else if codeCount > 0 {
		logger.Info("expired link codes cleaned up", zap.Int64("count", codeCount))
	}

	// Downgrade users whose paid subscription has expired. Changes
	// subscription_tier to "free" but keeps subscription_expires_at intact
	// so the admin panel can show "expired on X". Runs every cycle (~1 min)
	// so the worst-case window where an expired user still has premium
	// limits is 60 seconds — an acceptable trade-off vs per-request
	// expiry checks on every API call. Stays at 1-min: entitlement freshness.
	expiredIDs, err := repository.DowngradeExpiredSubscriptions(db)
	if err != nil {
		logger.Error("subscription expiry check failed", zap.Error(err))
	} else if len(expiredIDs) > 0 {
		// PERF-04 / D-06: bust user:<id> for each just-downgraded user so the
		// next AuthRequired pass sees the free tier instantly (the cron is the
		// authoritative downgrade trigger for time-based expiry). Best-effort
		// per id — a bust failure is backstopped by the 5s TTL.
		bustExpiredUsers(logger, redisClient, expiredIDs)
		logger.Info("expired subscriptions downgraded to free", zap.Int("count", len(expiredIDs)))
	}

	// --- every 5 ticks (~5 min): expired session rows (D-10c) ---
	// Sessions carry their own DB-side expiry; sweeping them every 5 min rather
	// than every minute trims redundant DELETEs with no correctness impact.
	if tickCount%sessionCleanupEveryTicks == 0 {
		count, err := repository.DeleteExpiredSessions(db)
		if err != nil {
			logger.Error("session cleanup failed", zap.Error(err))
		} else if count > 0 {
			logger.Info("expired sessions cleaned up", zap.Int64("count", count))
		}
	}

	// --- every 1440 ticks (~daily): stale device rows (D-10c) ---
	// Frees quota slots for devices not seen within cfg.StaleDeviceAfter (the
	// iOS-reinstall safety net). The window is measured in days, so a daily
	// sweep is more than frequent enough; owners can also remove devices
	// manually via DELETE /devices/:id.
	if tickCount%staleDeviceEveryTicks == 0 {
		deviceCount, err := repository.DeleteStaleDevices(db, time.Now().Add(-cfg.StaleDeviceAfter))
		if err != nil {
			logger.Error("stale device cleanup failed", zap.Error(err))
		} else if deviceCount > 0 {
			logger.Info("stale devices cleaned up", zap.Int64("count", deviceCount))
		}
	}

	// --- every 10080 ticks (~weekly): 90-day connection prune (PERF-08 / D-10c) ---
	// Permanently DELETEs disconnected connection rows older than 90 days to
	// bound table growth (audit §2.5). Active rows (disconnected_at IS NULL) are
	// never touched (T-06-PRUNE); the predicate rides idx_connections_connected_at.
	if tickCount%pruneConnectionEveryTicks == 0 {
		pruned, err := repository.PruneOldConnections(db, time.Now().Add(-connectionPruneAge))
		if err != nil {
			logger.Error("90-day connection prune failed", zap.Error(err))
		} else if pruned > 0 {
			logger.Info("old connections pruned (90-day retention)", zap.Int64("count", pruned))
		}
	}
}

// runExpiryDowngrade flips users with lapsed paid subscriptions back to the
// system plan. Per ADR §19.10 / D-26. The repository function is idempotent —
// safe to call on an empty/cold DB.
//
// PERF-06 RUN_SCHEDULER gate (Phase 6): main.go now gates the ENTIRE scheduler
// (including this job) behind config.ShouldRunScheduler — set RUN_SCHEDULER=false
// on non-primary replicas so only one replica runs periodic jobs (D-09b).
func runExpiryDowngrade(db *gorm.DB, logger *zap.Logger, redisClient *redis.Client) {
	downgradedIDs, err := repository.DowngradeExpiredPlans(db)
	if err != nil {
		logger.Error("expiry downgrade failed", zap.Error(err))
		return
	}
	if len(downgradedIDs) > 0 {
		// PERF-04 / D-06: synchronously bust user:<id> for each plan-downgraded
		// user (RETURNING-id source) so a lapsed subscription never keeps Pro
		// in cache past the cron pass.
		bustExpiredUsers(logger, redisClient, downgradedIDs)
		logger.Info("expired plans downgraded to system plan", zap.Int("count", len(downgradedIDs)))
	}
}

// bustExpiredUsers synchronously busts user:<id> for every id a bulk-downgrade
// pass flipped to free (PERF-04 / D-06 — "zero-lag everywhere"). Each bust is
// best-effort: a DEL failure is logged and the 5s user-cache TTL is the
// backstop, so a single Redis hiccup never leaves a downgraded user with stale
// Pro for more than the TTL. context.Background() is fine — these are
// fire-and-forget DELs on the scheduler goroutine, not request-scoped.
func bustExpiredUsers(logger *zap.Logger, redisClient *redis.Client, ids []string) {
	if redisClient == nil || len(ids) == 0 {
		return
	}
	ctx := context.Background()
	for _, id := range ids {
		if err := cache.BustUserCache(ctx, redisClient, id); err != nil {
			logger.Warn("scheduler: BustUserCache failed for downgraded user (5s TTL is the backstop)",
				zap.String("user_id", id), zap.Error(err))
		}
	}
}
