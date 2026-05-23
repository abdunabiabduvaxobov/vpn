package model

import "time"

// Plan is an admin-defined entitlement bundle per ADR §19.2.
//
// Exactly one row has is_system=TRUE — enforced by idx_plans_one_system
// (migration 019). When a paid plan expires, the scheduler (D-26) flips
// users.plan_id back to that row.
type Plan struct {
	ID             string    `json:"id"              gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	Code           string    `json:"code"            gorm:"type:varchar(40);uniqueIndex;not null"`
	Name           string    `json:"name"            gorm:"type:varchar(100);not null"`
	Description    string    `json:"description"     gorm:"type:text;default:''"`
	MaxDevices     int       `json:"max_devices"     gorm:"not null"`
	MaxServers     int       `json:"max_servers"     gorm:"not null"`
	SpeedLimitMbps int       `json:"speed_limit_mbps" gorm:"not null;default:0"`
	IsActive       bool      `json:"is_active"       gorm:"not null;default:true"`
	IsSystem       bool      `json:"is_system"       gorm:"not null;default:false"`
	SortOrder      int       `json:"sort_order"      gorm:"not null;default:0"`
	CreatedAt      time.Time `json:"created_at"      gorm:"autoCreateTime"`
	UpdatedAt      time.Time `json:"updated_at"      gorm:"autoUpdateTime"`
}

// PlanServer is the M:N join between plans and vpn_servers.
// Composite PK matches the migration's PRIMARY KEY (plan_id, server_id).
type PlanServer struct {
	PlanID   string `json:"plan_id"   gorm:"primaryKey;type:uuid"`
	ServerID string `json:"server_id" gorm:"primaryKey;type:uuid"`
}

// PlanOffer is a (plan, periodicity, currency) tuple bound to a lava_offer_id.
// Multiple offers per plan; multiple rows per (plan, periodicity, currency)
// allowed but only ONE with is_active=true (partial unique idx_plan_offers_unique_active).
type PlanOffer struct {
	ID          string    `json:"id"             gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	PlanID      string    `json:"plan_id"        gorm:"type:uuid;not null;index"`
	Periodicity string    `json:"periodicity"    gorm:"type:varchar(20);not null"`
	Currency    string    `json:"currency"       gorm:"type:varchar(3);not null"`
	Amount      float64   `json:"amount"         gorm:"type:numeric(10,2);not null"`
	LavaOfferID *string   `json:"lava_offer_id"  gorm:"column:lava_offer_id;type:varchar(64)"`
	IsActive    bool      `json:"is_active"      gorm:"not null;default:true"`
	CreatedAt   time.Time `json:"created_at"     gorm:"autoCreateTime"`
	UpdatedAt   time.Time `json:"updated_at"     gorm:"autoUpdateTime"`
}
