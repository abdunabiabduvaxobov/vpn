package model

import (
	"time"

	"gorm.io/datatypes"
)

// LavaWebhookEvent is the idempotency + forensics log for inbound
// lava.top webhooks. The natural key (event_type, contract_id,
// COALESCE(payload->>timestamp, payload->>cancelledAt)) is enforced
// by idx_lava_webhook_events_natural_key (migration 020).
type LavaWebhookEvent struct {
	ID          string         `json:"id"           gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	EventType   string         `json:"event_type"   gorm:"type:varchar(64);not null"`
	ContractID  *string        `json:"contract_id"  gorm:"type:varchar(64)"`
	InvoiceID   *string        `json:"invoice_id"   gorm:"type:varchar(64)"`
	Payload     datatypes.JSON `json:"payload"      gorm:"type:jsonb;not null"`
	ReceivedAt  time.Time      `json:"received_at"  gorm:"autoCreateTime"`
	ProcessedAt *time.Time     `json:"processed_at"`
	Error       *string        `json:"error"        gorm:"type:text"`
	// Status is the webhook log lifecycle column added in migration 024
	// (ADMIN-06). Values: PENDING (received, not yet dispatched), DELIVERED
	// (processed without error), FAILED (processing errored), REPLAYED (an
	// admin re-applied a previously-DELIVERED/FAILED event via the replay
	// endpoint). It is forensic, not part of the lava idempotency authority
	// (that is the natural-key UNIQUE index from migration 020).
	Status string `json:"status" gorm:"column:status;type:varchar(16);not null;default:PENDING"`
	// RetriedCount counts admin-initiated replays of this event (migration 024,
	// ADMIN-06). Bumped by MarkWebhookReplayed on every replay; live deliveries
	// never touch it.
	RetriedCount int `json:"retried_count" gorm:"column:retried_count;not null;default:0"`
}
