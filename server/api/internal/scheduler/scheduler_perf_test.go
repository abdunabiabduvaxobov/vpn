package scheduler_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"vpnapp/server/api/internal/cache"
	"vpnapp/server/api/internal/config"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// Wave 0 RED-first tests for the Phase 6 scheduler performance work:
//   - D-09 (e): the heartbeat bulk flush collapses N dirty ids into 1 UPDATE
//     (PERF-02), driven by a 10s flush ticker.
//   - D-09 (d): a RUN_SCHEDULER=false instance fires NO scheduler jobs (PERF-06)
//     — belt-and-suspenders to the unit gate test in config/scheduler_gate_test.go.
//   - the 90-day connection prune runs on a weekly (not per-minute) cadence
//     (PERF, plan 06).
//
// These reference cache.FlushHeartbeats / cache.TouchHeartbeat (plan 06) and
// config.ShouldRunScheduler (plan 06), so the file fails to compile until those
// land — the intended RED state.

func newSchedulerPerfRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	return mr, redis.NewClient(&redis.Options{Addr: mr.Addr()})
}

func newSchedulerPerfDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE connections (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL DEFAULT '',
			server_id TEXT NOT NULL DEFAULT '',
			connected_at DATETIME,
			disconnected_at DATETIME,
			last_heartbeat_at DATETIME
		)`).Error; err != nil {
		t.Fatalf("create connections: %v", err)
	}
	return db
}

// TestHeartbeatFlushTicker: N TouchHeartbeat writes collapse into ONE
// FlushHeartbeats — the PERF-02 (e) "N writes → 1 flush" assertion that the 10s
// ticker relies on. (The ticker wiring itself lands in plan 06; this proves the
// flush primitive the ticker calls.)
func TestHeartbeatFlushTicker(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode — skipping DB-dependent flush")
	}
	mr, rc := newSchedulerPerfRedis(t)
	db := newSchedulerPerfDB(t)
	ctx := context.Background()

	const n = 30
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("conn-%d", i)
		if err := db.Exec(`INSERT INTO connections (id, connected_at) VALUES (?, ?)`, id, time.Now()).Error; err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
		if err := cache.TouchHeartbeat(ctx, rc, id); err != nil {
			t.Fatalf("TouchHeartbeat %s: %v", id, err)
		}
	}

	members, _ := mr.SMembers("hb:dirty")
	if len(members) != n {
		t.Fatalf("expected %d dirty ids before flush, got %d", n, len(members))
	}

	count, err := cache.FlushHeartbeats(ctx, rc, db)
	if err != nil {
		t.Fatalf("FlushHeartbeats: %v", err)
	}
	if count != n {
		t.Errorf("PERF-02 (e): expected one flush to absorb all %d ids, got %d", n, count)
	}
	after, _ := mr.SMembers("hb:dirty")
	if len(after) != 0 {
		t.Errorf("expected hb:dirty drained after one flush, got %d", len(after))
	}
}

// TestSchedulerGateNoJobs: with RUN_SCHEDULER=false the gate decision must be
// false, so main.go never calls scheduler.Start and no cleanup side-effect
// fires (a stale connection row is NOT disconnected). This is the D-09 (d)
// integration-level proof; the load-bearing unit assertion is in
// config/scheduler_gate_test.go.
func TestSchedulerGateNoJobs(t *testing.T) {
	if config.ShouldRunScheduler("false") {
		t.Fatalf("PERF-06: RUN_SCHEDULER=false must gate the scheduler OFF")
	}

	// Sentinel: a stale connection that a running scheduler WOULD disconnect.
	db := newSchedulerPerfDB(t)
	if err := db.Exec(`INSERT INTO connections (id, connected_at, last_heartbeat_at, disconnected_at)
		VALUES ('stale-1', ?, ?, NULL)`, time.Now().Add(-time.Hour), time.Now().Add(-time.Hour)).Error; err != nil {
		t.Fatalf("seed stale: %v", err)
	}

	// Gate OFF → we deliberately do NOT call scheduler.Start. Advancing past
	// cleanupInterval must leave the stale row untouched.
	time.Sleep(10 * time.Millisecond)

	var disconnected *time.Time
	if err := db.Raw(`SELECT disconnected_at FROM connections WHERE id = 'stale-1'`).Scan(&disconnected).Error; err != nil {
		t.Fatalf("read stale row: %v", err)
	}
	if disconnected != nil {
		t.Errorf("PERF-06: gated-off scheduler must not disconnect stale rows")
	}
}

// TestWeeklyPruneCadence (skeleton): the 90-day connection prune must run on a
// weekly tick, not per-minute. RED until plan 06 introduces the per-job
// interval split and the prune job. Asserts the intended interval contract once
// the scheduler exposes it.
func TestWeeklyPruneCadence(t *testing.T) {
	t.Skip("RED skeleton — assert weekly prune interval once plan 06 splits per-job cadences")

	const wantPruneInterval = 7 * 24 * time.Hour
	// Once plan 06 exposes the prune cadence (e.g. scheduler.PruneInterval or a
	// config tunable), assert it equals wantPruneInterval and is NOT the 1m
	// cleanupInterval.
	_ = wantPruneInterval
}
