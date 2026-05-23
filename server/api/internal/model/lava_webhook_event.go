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
}
