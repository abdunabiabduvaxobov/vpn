package model

import "time"

// Invoice is one row per /checkout call. Status lifecycle:
// pending -> paid | failed | cancelled (set by webhook handler).
type Invoice struct {
	ID            string    `json:"id"              gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`
	UserID        string    `json:"user_id"         gorm:"type:uuid;not null;index"`
	LavaInvoiceID string    `json:"lava_invoice_id" gorm:"type:varchar(64);uniqueIndex;not null"`
	OfferID       string    `json:"offer_id"        gorm:"type:varchar(64);not null"` // lava-side offer UUID
	PlanID        *string   `json:"plan_id"         gorm:"type:uuid;index"`           // ADR §19.6
	PlanOfferID   *string   `json:"plan_offer_id"   gorm:"type:uuid;index"`           // ADR §19.6
	Plan          string    `json:"plan"            gorm:"type:varchar(20);not null"`
	Periodicity   string    `json:"periodicity"     gorm:"type:varchar(20);not null"`
	Currency      string    `json:"currency"        gorm:"type:varchar(3);not null"`
	Amount        float64   `json:"amount"          gorm:"type:numeric(10,2);not null"`
	Status        string    `json:"status"          gorm:"type:varchar(20);not null"`
	PaymentURL    string    `json:"payment_url"     gorm:"type:text"`
	CreatedAt     time.Time `json:"created_at"      gorm:"autoCreateTime"`
	UpdatedAt     time.Time `json:"updated_at"      gorm:"autoUpdateTime"`
}
