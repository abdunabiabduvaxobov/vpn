package repository

import (
	"errors"
	"time"

	"vpnapp/server/api/internal/model"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// CreateConnection inserts a new active connection record.
// The ID is generated in Go so the function works with any database backend.
func CreateConnection(db *gorm.DB, conn *model.Connection) error {
	if conn.ID == "" {
		conn.ID = uuid.NewString()
	}
	if conn.ConnectedAt.IsZero() {
		conn.ConnectedAt = time.Now()
	}
	now := time.Now()
	conn.LastHeartbeatAt = &now
	conn.Status = "connecting"
	result := db.Create(conn)
	return result.Error
}

// CreateConnectionAtomic inserts a connection record only when the caller's
// current active-connection count is below maxDevices.  The check and the
// insert are performed in a single statement so there is no TOCTOU window
// between counting and inserting.
//
// Returns (true, nil) when the row was inserted successfully.
// Returns (false, nil) when the device limit has already been reached (the
// caller should return HTTP 429).
// Returns (false, err) on any other database error.
func CreateConnectionAtomic(db *gorm.DB, conn *model.Connection, maxDevices int) (bool, error) {
	if conn.ID == "" {
		conn.ID = uuid.NewString()
	}
	if conn.ConnectedAt.IsZero() {
		conn.ConnectedAt = time.Now()
	}
	now := time.Now()
	conn.LastHeartbeatAt = &now
	conn.Status = "connecting"

	// The INSERT … SELECT pattern makes the limit check and the insert atomic.
	// No row is written when the sub-query count equals or exceeds maxDevices.
	result := db.Exec(
		`INSERT INTO connections (id, user_id, server_id, connected_at, bytes_up, bytes_down, status, last_heartbeat_at)
		 SELECT ?, ?, ?, ?, 0, 0, 'connecting', ?
		 WHERE (
		   SELECT COUNT(*) FROM connections
		   WHERE user_id = ? AND disconnected_at IS NULL
		 ) < ?`,
		conn.ID, conn.UserID, conn.ServerID, conn.ConnectedAt, conn.LastHeartbeatAt,
		conn.UserID, maxDevices,
	)
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected == 0 {
		// The sub-query returned count >= maxDevices; no row was inserted.
		return false, nil
	}
	return true, nil
}

// DisconnectConnection marks a connection as disconnected and records final byte counts.
// Returns ErrNotFound if the connection does not exist.
func DisconnectConnection(db *gorm.DB, id string, bytesUp, bytesDown int64) error {
	now := time.Now()
	result := db.Model(&model.Connection{}).
		Where("id = ? AND disconnected_at IS NULL", id).
		Updates(map[string]interface{}{
			"disconnected_at": now,
			"bytes_up":        bytesUp,
			"bytes_down":      bytesDown,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// CountActiveConnections returns the number of connections for a user that have no
// disconnected_at timestamp — i.e. connections that are still live.
func CountActiveConnections(db *gorm.DB, userID string) (int64, error) {
	var count int64
	result := db.Model(&model.Connection{}).
		Where("user_id = ? AND disconnected_at IS NULL", userID).
		Count(&count)
	return count, result.Error
}

// ListActiveConnectionsByUser returns all live connections for a given user.
func ListActiveConnectionsByUser(db *gorm.DB, userID string) ([]model.Connection, error) {
	var connections []model.Connection
	result := db.Where("user_id = ? AND disconnected_at IS NULL", userID).
		Order("connected_at DESC").
		Find(&connections)
	return connections, result.Error
}

// CleanupStaleConnections marks connections as disconnected when their last_heartbeat_at
// is older than staleDuration and they still have no disconnected_at. Returns the number
// of rows affected.
//
// The predicate is `disconnected_at IS NULL AND last_heartbeat_at < cutoff`. The COALESCE
// wrapper on last_heartbeat_at was intentionally dropped (PERF-05): an active
// (disconnected_at IS NULL) row always has a non-null last_heartbeat_at — migration 008
// backfilled it for then-active rows, and both CreateConnection and CreateConnectionAtomic
// set LastHeartbeatAt = &now on every insert — so the COALESCE was a no-op for live rows.
// Dropping it lets the planner range-scan the partial index idx_connections_heartbeat_active
// (migration 022) instead of sequential-scanning the full connections history.
func CleanupStaleConnections(db *gorm.DB, staleDuration time.Duration) (int64, error) {
	cutoff := time.Now().Add(-staleDuration)
	now := time.Now()

	result := db.Model(&model.Connection{}).
		Where("disconnected_at IS NULL AND last_heartbeat_at < ?", cutoff).
		Updates(map[string]interface{}{
			"disconnected_at": now,
		})
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

// CleanupStaleReservations marks connections with status='connecting' as disconnected
// when they are older than maxAge and have no disconnected_at.
// These are connections that were reserved but never transitioned to connected status.
// Returns the number of rows affected.
//
// This intentionally RETAINS the COALESCE(last_heartbeat_at, connected_at) wrapper. It
// filters the status='connecting' subset — a tiny, low-volume set of reservations — so the
// COALESCE is harmless here and is out of PERF-05's strict scope (which targets the
// high-volume CleanupStaleConnections sweep above).
func CleanupStaleReservations(db *gorm.DB, maxAge time.Duration) (int64, error) {
	cutoff := time.Now().Add(-maxAge)
	now := time.Now()
	result := db.Model(&model.Connection{}).
		Where("disconnected_at IS NULL AND status = 'connecting' AND COALESCE(last_heartbeat_at, connected_at) < ?", cutoff).
		Updates(map[string]interface{}{
			"disconnected_at": now,
		})
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

// UpdateHeartbeat refreshes the last_heartbeat_at timestamp for an active connection.
// Returns ErrNotFound if the connection does not exist or is already disconnected.
//
// Deprecated (PERF-02 / D-03): the heartbeat handler no longer calls this — beats
// are pipelined into Redis (cache.TouchHeartbeat) and flushed in one bulk UPDATE
// by the 10s scheduler ticker (cache.FlushHeartbeats). Retained as a single-row
// helper / for tests; the per-call PG write it embodies is intentionally gone
// from the hot path.
func UpdateHeartbeat(db *gorm.DB, id string) error {
	result := db.Model(&model.Connection{}).
		Where("id = ? AND disconnected_at IS NULL", id).
		Update("last_heartbeat_at", time.Now())
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// PruneOldConnections permanently DELETEs disconnected connection rows whose
// disconnected_at is older than cutoff, bounding unbounded history growth
// (PERF-08 / D-10c, audit §2.5). Returns the number of rows deleted.
//
// Safety: only rows with disconnected_at IS NOT NULL AND disconnected_at < cutoff
// are removed — ACTIVE connections (disconnected_at IS NULL) are NEVER touched
// (T-06-PRUNE). The scheduler calls this on a WEEKLY cadence with a 90-day
// cutoff; the predicate is served by idx_connections_connected_at (plan 02).
func PruneOldConnections(db *gorm.DB, cutoff time.Time) (int64, error) {
	result := db.Where("disconnected_at IS NOT NULL AND disconnected_at < ?", cutoff).
		Delete(&model.Connection{})
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

// FindConnectionByID looks up a connection by UUID.
// Returns ErrNotFound if no row exists.
func FindConnectionByID(db *gorm.DB, id string) (*model.Connection, error) {
	var conn model.Connection
	result := db.First(&conn, "id = ?", id)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, result.Error
	}
	return &conn, nil
}
