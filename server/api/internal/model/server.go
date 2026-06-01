package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// VPNServer represents a VPN tunnel server in the database.
type VPNServer struct {
	ID               string    `json:"id" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	Hostname         string    `json:"hostname" gorm:"uniqueIndex;not null"`
	IPAddress        string    `json:"ip_address" gorm:"not null"`
	Region           string    `json:"region" gorm:"not null;index"`
	City             string    `json:"city" gorm:"not null"`
	Country          string    `json:"country" gorm:"not null"`
	CountryCode      string    `json:"country_code" gorm:"type:char(2);not null"`
	Protocol         string    `json:"protocol" gorm:"not null;default:vless-reality"`
	Capacity         int       `json:"-" gorm:"not null;default:500"`
	CurrentLoad      int       `json:"load_percent" gorm:"default:0"`
	IsActive         bool      `json:"is_active" gorm:"default:true;index"`
	// IsDraining (migration 024) marks a server in drain mode: existing
	// connections survive but ListActiveServers excludes it, so non-admin
	// GET /servers stops handing it out for new connections (ADMIN-04).
	IsDraining bool `json:"is_draining" gorm:"column:is_draining;not null;default:false"`
	// LastSeenAt (migration 024) is the last tunnel-heartbeat timestamp,
	// written by TouchServerHeartbeat. Nil until the server first reports.
	LastSeenAt       *time.Time `json:"last_seen_at" gorm:"column:last_seen_at"`
	RealityPublicKey string     `json:"-" gorm:"column:reality_public_key"`
	RealityShortID   string    `json:"-" gorm:"column:reality_short_id"`
	// WebSocket CDN transport fields (migration 005).
	// WSEnabled is true only when the server has Cloudflare CDN + Nginx configured.
	WSEnabled bool   `json:"-" gorm:"column:ws_enabled;default:false"`
	WSHost    string `json:"-" gorm:"column:ws_host"`
	WSPath    string `json:"-" gorm:"column:ws_path;default:/ws"`
	// AmneziaWG fields (migration 004).
	// AWGPublicKey is nil when the server does not support AmneziaWG.
	AWGPublicKey *string       `json:"-" gorm:"column:awg_public_key"`
	AWGEndpoint  *string       `json:"-" gorm:"column:awg_endpoint"`
	AWGParams    *AWGParams    `json:"-" gorm:"column:awg_params;type:jsonb"`
	CreatedAt    time.Time     `json:"created_at" gorm:"autoCreateTime"`
}

// AWGParams holds the AmneziaWG obfuscation parameters stored in the
// awg_params JSONB column.  These are forwarded verbatim to the client
// so it can configure its amneziawg-go device.
type AWGParams struct {
	Jc   int `json:"jc"`
	Jmin int `json:"jmin"`
	Jmax int `json:"jmax"`
	S1   int `json:"s1"`
	S2   int `json:"s2"`
	H1   int `json:"h1"`
	H2   int `json:"h2"`
	H3   int `json:"h3"`
	H4   int `json:"h4"`
}

// Value implements driver.Valuer so GORM can store AWGParams as a JSONB value.
func (p AWGParams) Value() (driver.Value, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("marshaling AWGParams: %w", err)
	}
	return string(b), nil
}

// Scan implements sql.Scanner so GORM can read AWGParams from a JSONB column.
func (p *AWGParams) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return fmt.Errorf("AWGParams.Scan: unsupported type %T", value)
	}
	return json.Unmarshal(bytes, p)
}

// Connection tracks an active VPN connection (for concurrent device limiting).
// JSON tags are snake_case across all fields so admin-panel TypeScript types
// stay in sync with the wire shape. The mobile client only reads Length from
// GET /connections and .id from POST /connections, so snake-casing the rest
// does not break backwards compatibility there.
type Connection struct {
	ID              string     `json:"id" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	UserID          string     `json:"user_id" gorm:"not null;index"`
	ServerID        string     `json:"server_id" gorm:"not null;index"`
	ConnectedAt     time.Time  `json:"connected_at" gorm:"autoCreateTime"`
	DisconnectedAt  *time.Time `json:"disconnected_at" gorm:"index"`
	BytesUp         int64      `json:"bytes_up" gorm:"default:0"`
	BytesDown       int64      `json:"bytes_down" gorm:"default:0"`
	Status          string     `json:"status" gorm:"type:varchar(20);not null;default:connected"`
	LastHeartbeatAt *time.Time `json:"last_heartbeat_at"`
}
