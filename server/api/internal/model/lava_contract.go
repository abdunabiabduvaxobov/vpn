package model

import "time"

// LavaContract mirrors the lava-side recurring contract. ContractID is the
// lava-side UUID (unique); ParentContractID is populated on renewal events
// (subscription.recurring.payment.* webhooks per RESEARCH §1.5).
type LavaContract struct {
	ID               string     `json:"id"                  gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	UserID           string     `json:"user_id"             gorm:"type:uuid;not null;index"`
	ContractID       string     `json:"contract_id"         gorm:"type:varchar(64);uniqueIndex;not null"`
	ParentContractID *string    `json:"parent_contract_id"  gorm:"column:parent_contract_id;type:varchar(64);index"`
	OfferID          string     `json:"offer_id"            gorm:"type:varchar(64);not null"`
	Plan             string     `json:"plan"                gorm:"type:varchar(20);not null"`
	Periodicity      string     `json:"periodicity"         gorm:"type:varchar(20);not null"`
	Currency         string     `json:"currency"            gorm:"type:varchar(3);not null"`
	IsActive         bool       `json:"is_active"           gorm:"not null;default:true"`
	StartedAt        time.Time  `json:"started_at"          gorm:"autoCreateTime"`
	ExpiresAt        *time.Time `json:"expires_at"`
	CancelledAt      *time.Time `json:"cancelled_at"`
	CreatedAt        time.Time  `json:"created_at"          gorm:"autoCreateTime"`
}
