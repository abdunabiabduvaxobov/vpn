package repository

import (
	"context"
	"errors"
	"time"

	"vpnapp/server/api/internal/model"

	"gorm.io/gorm"
)

// ListActiveServers returns all active, non-draining VPN servers ordered by load.
//
// The `AND is_draining = false` filter is the load-bearing half of ADMIN-04
// drain mode: a drained server (is_draining=true) must vanish from non-admin
// GET /servers so no new connections are handed out, while existing tunnels
// survive. The admin path (ListAllServers) is intentionally unfiltered so
// operators still see drained servers. This DB filter makes a cache miss
// correct even before BustServersCache runs (T-07-24).
func ListActiveServers(ctx context.Context, db *gorm.DB) ([]model.VPNServer, error) {
	var servers []model.VPNServer
	result := db.WithContext(ctx).
		Where("is_active = ? AND is_draining = ?", true, false).
		Order("current_load ASC").Find(&servers)
	return servers, result.Error
}

// CountFreshServers returns the number of active VPN servers whose last_seen_at
// is within `within` of now — i.e. servers currently emitting heartbeats.
//
// Used by the ADMIN-07 readyz tunnel check: tunnel health is a cheap DB read of
// last_seen_at freshness (no network call). > 0 means at least one tunnel is
// alive. The query is parameterized and ctx-threaded for the readyz timeout.
func CountFreshServers(ctx context.Context, db *gorm.DB, within time.Duration) (int64, error) {
	cutoff := time.Now().Add(-within)
	var n int64
	result := db.WithContext(ctx).
		Model(&model.VPNServer{}).
		Where("is_active = ? AND last_seen_at IS NOT NULL AND last_seen_at > ?", true, cutoff).
		Count(&n)
	return n, result.Error
}

// TouchServerHeartbeat records a tunnel heartbeat: it sets last_seen_at to now
// and updates the reported load for the server. Called by the shared-secret
// internal heartbeat endpoint (ADMIN-07). ctx-threaded so a slow write cannot
// hang the request beyond its deadline.
func TouchServerHeartbeat(ctx context.Context, db *gorm.DB, serverID string, loadPercent int) error {
	return db.WithContext(ctx).
		Model(&model.VPNServer{}).
		Where("id = ?", serverID).
		Updates(map[string]interface{}{
			"last_seen_at": time.Now(),
			"current_load": loadPercent,
		}).Error
}

// ServerHealthRow is the per-server health detail the ADMIN-08 deps-health
// endpoint returns: enough for an operator to see which tunnel is stale without
// exposing the full VPNServer row (keys, endpoints). Admin-only — this detail
// must NOT leak from the public /readyz (T-07-37).
type ServerHealthRow struct {
	ID          string     `json:"id"`
	Hostname    string     `json:"hostname"`
	IsActive    bool       `json:"is_active"`
	CurrentLoad int        `json:"current_load"`
	LastSeenAt  *time.Time `json:"last_seen_at"`
}

// ListServerHealth returns the per-server heartbeat detail for the admin
// deps-health page, ordered by hostname for a stable display. ctx-threaded so a
// slow read cannot hang the request beyond its deadline. It deliberately selects
// only the health-relevant columns — never the REALITY keys / endpoints on the
// full VPNServer row.
func ListServerHealth(ctx context.Context, db *gorm.DB) ([]ServerHealthRow, error) {
	var rows []ServerHealthRow
	result := db.WithContext(ctx).
		Model(&model.VPNServer{}).
		Select("id", "hostname", "is_active", "current_load", "last_seen_at").
		Order("hostname ASC").
		Find(&rows)
	return rows, result.Error
}

// FindServerByID looks up a VPN server by UUID.
func FindServerByID(ctx context.Context, db *gorm.DB, id string) (*model.VPNServer, error) {
	var server model.VPNServer
	result := db.WithContext(ctx).First(&server, "id = ?", id)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, result.Error
	}
	return &server, nil
}
