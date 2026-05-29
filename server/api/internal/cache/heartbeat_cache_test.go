package cache

import (
	"context"
	"fmt"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// Wave 0 RED-first tests for the Redis heartbeat pipeline + bulk flush
// (PERF-02 / D-03 / D-04). References plan 06's not-yet-built symbols
// TouchHeartbeat and FlushHeartbeats — file fails to compile until they land.
//
// Contract under test (RESEARCH §Code Examples):
//   TouchHeartbeat(ctx, client, connID) error
//     → pipeline: SET hb:<connID> <unix_ts> EX 600  +  SADD hb:dirty <connID>
//   FlushHeartbeats(ctx, client, db) (count int, err error)
//     → SMEMBERS hb:dirty → ONE bulk UPDATE connections → SREM flushed ids
//
// The "N writes → 1 flush" assertion is PERF-02 (e): 50 distinct TouchHeartbeat
// calls collapse to a single FlushHeartbeats that drains hb:dirty.

// newHeartbeatTestDB opens an in-memory SQLite DB with a connections table
// matching the production columns the flush UPDATE touches. The flush helper's
// real query uses Postgres array binding; the sqlite stand-in here proves the
// helper drains the dirty set and reports a count — the EXPLAIN/array-binding
// integration proof lives in scheduler_perf_test.go against a testcontainer.
func newHeartbeatTestDB(t *testing.T) *gorm.DB {
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

// TestTouchHeartbeatWritesKeyAndDirtySet: a single TouchHeartbeat writes the
// hb:<id> key (with a ~600s TTL) AND adds the id to the hb:dirty set (D-04).
func TestTouchHeartbeatWritesKeyAndDirtySet(t *testing.T) {
	mr, c := newMiniRedis(t)
	ctx := context.Background()
	if err := TouchHeartbeat(ctx, c, "conn-1"); err != nil {
		t.Fatalf("TouchHeartbeat: %v", err)
	}

	// hb:conn-1 must exist with a value (the unix ts) and a ~600s TTL.
	if !keyExists(mr.Keys(), "hb:conn-1") {
		t.Errorf("expected key hb:conn-1, keys=%v", mr.Keys())
	}
	ttl := mr.TTL("hb:conn-1")
	if ttl <= 0 || ttl > 600*time.Second {
		t.Errorf("expected hb:conn-1 TTL in (0, 600s], got %v", ttl)
	}

	// conn-1 must be a member of the hb:dirty set.
	members, err := mr.SMembers("hb:dirty")
	if err != nil {
		t.Fatalf("SMembers hb:dirty: %v", err)
	}
	if !contains(members, "conn-1") {
		t.Errorf("expected conn-1 in hb:dirty, got %v", members)
	}
}

// TestFlushHeartbeatsCollapsesNto1: 50 distinct TouchHeartbeat calls, then ONE
// FlushHeartbeats. Asserts hb:dirty held 50 ids before the flush, the flush
// returns count=50, and hb:dirty is drained to empty afterward — the PERF-02
// "N writes → 1 flush" assertion. The DB-touching flush runs only outside
// -short (sqlite stand-in); under -short we still assert the dirty-set state.
func TestFlushHeartbeatsCollapsesNto1(t *testing.T) {
	mr, c := newMiniRedis(t)
	ctx := context.Background()

	const n = 50
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("conn-%d", i)
		if err := TouchHeartbeat(ctx, c, id); err != nil {
			t.Fatalf("TouchHeartbeat %s: %v", id, err)
		}
	}

	before, err := mr.SMembers("hb:dirty")
	if err != nil {
		t.Fatalf("SMembers before flush: %v", err)
	}
	if len(before) != n {
		t.Fatalf("expected %d ids in hb:dirty before flush, got %d", n, len(before))
	}

	if testing.Short() {
		t.Skip("short mode — skipping DB-dependent flush; dirty-set N-collapse asserted above")
	}

	db := newHeartbeatTestDB(t)
	// Seed the connections so the bulk UPDATE has live rows to touch.
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("conn-%d", i)
		if err := db.Exec(`INSERT INTO connections (id, connected_at) VALUES (?, ?)`, id, time.Now()).Error; err != nil {
			t.Fatalf("seed connection %s: %v", id, err)
		}
	}

	count, err := FlushHeartbeats(ctx, c, db)
	if err != nil {
		t.Fatalf("FlushHeartbeats: %v", err)
	}
	if count != n {
		t.Errorf("expected flush count=%d (N→1 collapse), got %d", n, count)
	}

	after, err := mr.SMembers("hb:dirty")
	if err != nil {
		t.Fatalf("SMembers after flush: %v", err)
	}
	if len(after) != 0 {
		t.Errorf("expected hb:dirty drained to empty after flush, got %d members", len(after))
	}
}

// TestFlushHeartbeatsEmptyDirtySet: with nothing dirty, FlushHeartbeats is a
// no-op returning count=0, nil error (the 10s ticker fires harmlessly when no
// heartbeats arrived).
func TestFlushHeartbeatsEmptyDirtySet(t *testing.T) {
	_, c := newMiniRedis(t)
	db := newHeartbeatTestDB(t)
	count, err := FlushHeartbeats(context.Background(), c, db)
	if err != nil {
		t.Fatalf("FlushHeartbeats empty: %v", err)
	}
	if count != 0 {
		t.Errorf("expected count=0 on empty dirty set, got %d", count)
	}
}

// TestTouchHeartbeatFailOpen: a nil client must not panic and returns nil —
// heartbeat is non-critical (the cleanup grace window absorbs a missed beat).
func TestTouchHeartbeatFailOpen(t *testing.T) {
	if err := TouchHeartbeat(context.Background(), nil, "conn-1"); err != nil {
		t.Errorf("nil client: expected nil error (fail-open), got %v", err)
	}
}

// contains reports whether want is in the slice (local helper; the cache
// package has no strings dependency in its test files).
func contains(s []string, want string) bool {
	for _, v := range s {
		if v == want {
			return true
		}
	}
	return false
}
