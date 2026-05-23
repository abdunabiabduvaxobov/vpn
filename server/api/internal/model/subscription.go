package model

import "time"

// UnlimitedServers / UnlimitedDevices are sentinel values for Plan.MaxServers /
// Plan.MaxDevices. Handlers reading plans.* check for these before applying
// a slice or cap. Lives here for backward import compatibility — handlers
// like connection.go, devices.go, servers.go reference model.UnlimitedDevices /
// model.UnlimitedServers directly.
const UnlimitedServers = -1
const UnlimitedDevices = -1

// Subscription is the canonical "current entitlement" record. Phase 3 drops
// stripe_id (migration 020) and adds lava_contract_id. The legacy plan-limits
// map is DELETED — limits now live in the plans table (queried via plan_repo).
type Subscription struct {
	ID             string     `json:"id"                gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	UserID         string     `json:"user_id"           gorm:"not null;index"`
	Plan           string     `json:"plan"              gorm:"not null;default:free"`
	LavaContractID *string    `json:"-"                 gorm:"column:lava_contract_id;type:varchar(64);index"`
	IsActive       bool       `json:"is_active"         gorm:"default:true"`
	StartedAt      time.Time  `json:"started_at"        gorm:"autoCreateTime"`
	ExpiresAt      *time.Time `json:"expires_at"`
}
