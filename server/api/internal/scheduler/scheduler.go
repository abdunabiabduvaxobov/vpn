// Package scheduler runs background maintenance tasks on a fixed interval.
package scheduler

import (
	"sync"
	"time"

	"vpnapp/server/api/internal/config"
	"vpnapp/server/api/internal/repository"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

const cleanupInterval = 1 * time.Minute

// scheduler is the internal state for the background worker.
type scheduler struct {
	ticker *time.Ticker
	done   chan struct{}
	wg     sync.WaitGroup
}

// global instance — only one scheduler is expected per process.
var instance *scheduler
var mu sync.Mutex

// Start launches the background goroutine that cleans up expired sessions
// once per cleanupInterval. Calling Start more than once is safe — subsequent
// calls are no-ops if a scheduler is already running.
func Start(db *gorm.DB, logger *zap.Logger, cfg *config.Config) {
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
		runCleanup(db, logger, cfg)

		for {
			select {
			case <-s.ticker.C:
				runCleanup(db, logger, cfg)
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
func runCleanup(db *gorm.DB, logger *zap.Logger, cfg *config.Config) {
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
	expiredCount, err := repository.DowngradeExpiredSubscriptions(db)
	if err != nil {
		logger.Error("subscription expiry check failed", zap.Error(err))
	} else if expiredCount > 0 {
		logger.Info("expired subscriptions downgraded to free", zap.Int64("count", expiredCount))
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
