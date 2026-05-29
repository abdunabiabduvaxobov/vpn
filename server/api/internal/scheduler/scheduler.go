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

// scheduler is the internal state for the background worker.
type scheduler struct {
	ticker          *time.Ticker
	done            chan struct{}
	wg              sync.WaitGroup
	expiryTickCount int // incremented per tick; downgrade runs every 10
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
		ticker: time.NewTicker(cleanupInterval),
		done:   make(chan struct{}),
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()

		// Run once immediately so the first cleanup does not wait a full interval.
		runCleanup(db, logger, cfg, redisClient)

		for {
			select {
			case <-s.ticker.C:
				runCleanup(db, logger, cfg, redisClient)
				s.expiryTickCount++
				// D-26: run expiry downgrade every ~10 minutes (10 ticks at 1m interval).
				if s.expiryTickCount%10 == 0 {
					runExpiryDowngrade(db, logger, redisClient)
				}
			case <-s.done:
				return
			}
		}
	}()

	instance = s
	logger.Info("session cleanup scheduler started", zap.Duration("interval", cleanupInterval))
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
	close(s.done)
	s.wg.Wait()
}

// runCleanup deletes expired sessions and stale connections, and logs results.
func runCleanup(db *gorm.DB, logger *zap.Logger, cfg *config.Config, redisClient *redis.Client) {
	// Clean expired sessions
	count, err := repository.DeleteExpiredSessions(db)
	if err != nil {
		logger.Error("session cleanup failed", zap.Error(err))
	} else if count > 0 {
		logger.Info("expired sessions cleaned up", zap.Int64("count", count))
	}

	// Clean stale reservations (connecting for >2 min)
	reservationCount, err := repository.CleanupStaleReservations(db, 2*time.Minute)
	if err != nil {
		logger.Error("stale reservation cleanup failed", zap.Error(err))
	} else if reservationCount > 0 {
		logger.Info("stale reservations cleaned up", zap.Int64("count", reservationCount))
	}

	// Clean stale connections (no heartbeat for cfg.StaleConnectionAfter)
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
	// expiry checks on every API call.
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

	// Free quota slots occupied by devices that have not been seen for the
	// configured stale-device window. This is the safety net for the iOS
	// reinstall edge case (and for any device the user has stopped using).
	// Owners can also remove devices manually via DELETE /devices/:id.
	deviceCount, err := repository.DeleteStaleDevices(db, time.Now().Add(-cfg.StaleDeviceAfter))
	if err != nil {
		logger.Error("stale device cleanup failed", zap.Error(err))
	} else if deviceCount > 0 {
		logger.Info("stale devices cleaned up", zap.Int64("count", deviceCount))
	}
}

// runExpiryDowngrade flips users with lapsed paid subscriptions back to the
// system plan. Per ADR §19.10 / D-26. The repository function is idempotent —
// safe to call on an empty/cold DB.
//
// PERF-06 RUN_SCHEDULER gate (Phase 6): single-replica deployment in v2.2.0
// means this runs in the only API replica. When Phase 6 introduces multi-replica,
// the scheduler will be gated by the env var so only one replica runs it.
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
