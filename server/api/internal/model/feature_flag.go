package model

import "time"

// FeatureFlag is one row of the feature_flags table (migration 024).
//
// The table is the SOURCE OF TRUTH for the three operational toggles Phase 7
// controls — signups_off, payments_off, maintenance_mode (ADMIN-05/08). A
// short-TTL Redis cache (cache.GetFlag, ~10s) fronts it on the read path so
// per-request flag reads never hammer the DB; the DB row is what an admin
// PUT /admin/feature-flags/:key writes, and the cache is busted on that write.
//
// Key is the PK (a stable string like "maintenance_mode"); Value is a strict
// bool. UpdatedBy is the acting admin's UUID (nullable — the seed rows have no
// author) so the panel can show "last changed by".
type FeatureFlag struct {
	Key       string    `json:"key" gorm:"primaryKey;column:key"`
	Value     bool      `json:"value" gorm:"column:value;not null;default:false"`
	UpdatedAt time.Time `json:"updated_at" gorm:"column:updated_at;autoUpdateTime"`
	UpdatedBy *string   `json:"updated_by" gorm:"column:updated_by;type:uuid"`
}

// TableName pins the table to its snake_case form so GORM's default
// pluralisation ("feature_flags" happens to match, but pin it explicitly
// to stay robust if the struct is renamed).
func (FeatureFlag) TableName() string { return "feature_flags" }
