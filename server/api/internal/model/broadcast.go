package model

import "time"

// BroadcastMessage is one row of the broadcast_messages table (migration 024).
//
// Broadcasts are operator-authored banners (ADMIN-05) shown to clients —
// landing + mobile read the active set via the PUBLIC GET /api/v1/broadcasts
// endpoint (handler.ListBroadcastsPublic), which exposes ONLY {title, body,
// severity} (T-07-29: no target_tier/updated_by/internal columns leak).
//
// Severity is constrained to {info, warning, critical} at the handler layer
// (T-07-30). StartsAt/EndsAt are nullable bounds defining the active window
// (NULL bound = open-ended); Locale/TargetTier are optional targeting hints
// kept admin-side only for now.
type BroadcastMessage struct {
	ID         string     `json:"id" gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	Title      string     `json:"title" gorm:"column:title;not null"`
	Body       string     `json:"body" gorm:"column:body;not null"`
	Severity   string     `json:"severity" gorm:"column:severity;not null;default:info"`
	Locale     *string    `json:"locale" gorm:"column:locale"`
	TargetTier *string    `json:"target_tier" gorm:"column:target_tier"`
	StartsAt   *time.Time `json:"starts_at" gorm:"column:starts_at"`
	EndsAt     *time.Time `json:"ends_at" gorm:"column:ends_at"`
	CreatedAt  time.Time  `json:"created_at" gorm:"column:created_at;autoCreateTime"`
}

// TableName pins the table to its snake_case form so GORM's default
// pluralisation does not leak into SQL.
func (BroadcastMessage) TableName() string { return "broadcast_messages" }
