package repository

import (
	"errors"
	"time"

	"vpnapp/server/api/internal/model"

	"gorm.io/gorm"
)

// CreateInvoice inserts an invoice row. Caller populates all required fields
// (UserID, LavaInvoiceID, OfferID, PlanID, PlanOfferID, Plan, Periodicity,
// Currency, Amount, Status="pending", PaymentURL).
func CreateInvoice(db *gorm.DB, inv *model.Invoice) error {
	if inv.Status == "" {
		inv.Status = "pending"
	}
	return db.Create(inv).Error
}

// FindInvoiceByID returns the invoice with the given primary-key UUID.
func FindInvoiceByID(db *gorm.DB, id string) (*model.Invoice, error) {
	var inv model.Invoice
	result := db.Where("id = ?", id).First(&inv)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, result.Error
	}
	return &inv, nil
}

// FindInvoiceByLavaID is the webhook handler's reverse-lookup: given the
// lava-side invoice/contract id from the payload, find the local row to update.
func FindInvoiceByLavaID(db *gorm.DB, lavaInvoiceID string) (*model.Invoice, error) {
	var inv model.Invoice
	result := db.Where("lava_invoice_id = ?", lavaInvoiceID).First(&inv)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, result.Error
	}
	return &inv, nil
}

// FindActivePendingInvoice implements the ADR §9.2 60-second idempotency
// window for /checkout double-tap protection. Returns the most recent
// invoice for (user_id, lava-side offer_id) where:
//   - status = "pending"
//   - created_at > now() - within
//
// `within` is the caller-provided window (typically 60 seconds). Returns
// ErrNotFound when no eligible invoice exists.
func FindActivePendingInvoice(db *gorm.DB, userID, lavaOfferID string, within time.Duration) (*model.Invoice, error) {
	cutoff := time.Now().Add(-within)
	var inv model.Invoice
	result := db.
		Where("user_id = ? AND offer_id = ? AND status = ? AND created_at > ?", userID, lavaOfferID, "pending", cutoff).
		Order("created_at DESC").
		First(&inv)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, result.Error
	}
	return &inv, nil
}

// UpdateInvoiceStatus sets invoices.status. Webhook handler maps lava status
// to local enum (`pending` | `paid` | `failed` | `cancelled`).
func UpdateInvoiceStatus(db *gorm.DB, id, status string) error {
	result := db.Model(&model.Invoice{}).Where("id = ?", id).Update("status", status)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
