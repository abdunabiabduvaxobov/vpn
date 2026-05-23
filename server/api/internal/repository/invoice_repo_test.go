package repository_test

import (
	"testing"
	"time"

	"vpnapp/server/api/internal/model"
	"vpnapp/server/api/internal/repository"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupInvoiceRepoDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	// Pin pool to one connection so any future tx-wrapped helpers see prior
	// committed writes (matches plan_repo_test.go pattern + auth_test.go race
	// pattern). Cheap insurance.
	if sqlDB, dbErr := db.DB(); dbErr == nil {
		sqlDB.SetMaxOpenConns(1)
		sqlDB.SetMaxIdleConns(1)
	}
	// `id` has a DEFAULT so SQLite auto-fills when the GORM tag
	// `default:gen_random_uuid()` makes GORM omit the column from INSERT.
	if err := db.Exec(`CREATE TABLE invoices (
		id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
		user_id TEXT NOT NULL,
		lava_invoice_id TEXT NOT NULL UNIQUE,
		offer_id TEXT NOT NULL,
		plan_id TEXT,
		plan_offer_id TEXT,
		plan TEXT NOT NULL,
		periodicity TEXT NOT NULL,
		currency TEXT NOT NULL,
		amount REAL NOT NULL,
		status TEXT NOT NULL,
		payment_url TEXT,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`).Error; err != nil {
		t.Fatalf("create invoices: %v", err)
	}
	return db
}

func TestCreateInvoice_DefaultsStatusToPending(t *testing.T) {
	db := setupInvoiceRepoDB(t)
	inv := &model.Invoice{
		ID:            uuid.NewString(),
		UserID:        uuid.NewString(),
		LavaInvoiceID: "lava-1",
		OfferID:       "off-1",
		Plan:          "pro",
		Periodicity:   "MONTHLY",
		Currency:      "USD",
		Amount:        5.0,
		// Status left empty intentionally.
	}
	if err := repository.CreateInvoice(db, inv); err != nil {
		t.Fatalf("CreateInvoice: %v", err)
	}
	if inv.Status != "pending" {
		t.Errorf("expected default status=pending, got %q", inv.Status)
	}
}

func TestFindInvoiceByID_FoundAndNotFound(t *testing.T) {
	db := setupInvoiceRepoDB(t)
	inv := &model.Invoice{ID: uuid.NewString(), UserID: uuid.NewString(), LavaInvoiceID: "lava-x", OfferID: "off", Plan: "pro", Periodicity: "MONTHLY", Currency: "USD", Amount: 5, Status: "pending"}
	_ = repository.CreateInvoice(db, inv)
	got, err := repository.FindInvoiceByID(db, inv.ID)
	if err != nil || got.LavaInvoiceID != "lava-x" {
		t.Errorf("unexpected: %+v err=%v", got, err)
	}
	if _, err := repository.FindInvoiceByID(db, "missing"); err != repository.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestFindInvoiceByLavaID_HappyPath(t *testing.T) {
	db := setupInvoiceRepoDB(t)
	inv := &model.Invoice{ID: uuid.NewString(), UserID: uuid.NewString(), LavaInvoiceID: "lava-reverse", OfferID: "off", Plan: "pro", Periodicity: "MONTHLY", Currency: "USD", Amount: 5, Status: "pending"}
	_ = repository.CreateInvoice(db, inv)
	got, err := repository.FindInvoiceByLavaID(db, "lava-reverse")
	if err != nil || got.ID != inv.ID {
		t.Errorf("unexpected: %+v err=%v", got, err)
	}
}

func TestFindActivePendingInvoice_WithinAndOutsideWindow(t *testing.T) {
	db := setupInvoiceRepoDB(t)
	uid := uuid.NewString()
	// Recent pending — inside the 60s window.
	recent := &model.Invoice{ID: uuid.NewString(), UserID: uid, LavaInvoiceID: "rec-1", OfferID: "off-a", Plan: "pro", Periodicity: "MONTHLY", Currency: "USD", Amount: 5, Status: "pending"}
	if err := repository.CreateInvoice(db, recent); err != nil {
		t.Fatalf("seed recent: %v", err)
	}
	// Outside-window pending: manually backdate by raw UPDATE.
	old := &model.Invoice{ID: uuid.NewString(), UserID: uid, LavaInvoiceID: "old-1", OfferID: "off-b", Plan: "pro", Periodicity: "MONTHLY", Currency: "USD", Amount: 5, Status: "pending"}
	if err := repository.CreateInvoice(db, old); err != nil {
		t.Fatalf("seed old: %v", err)
	}
	if err := db.Exec("UPDATE invoices SET created_at = ? WHERE id = ?", time.Now().Add(-5*time.Minute), old.ID).Error; err != nil {
		t.Fatalf("backdate: %v", err)
	}

	got, err := repository.FindActivePendingInvoice(db, uid, "off-a", 60*time.Second)
	if err != nil || got.ID != recent.ID {
		t.Errorf("expected to find recent, got %+v err=%v", got, err)
	}

	// Outside the window — must return ErrNotFound.
	if _, err := repository.FindActivePendingInvoice(db, uid, "off-b", 60*time.Second); err != repository.ErrNotFound {
		t.Errorf("expected ErrNotFound for backdated row, got %v", err)
	}
}

func TestUpdateInvoiceStatus_HappyAndMissing(t *testing.T) {
	db := setupInvoiceRepoDB(t)
	inv := &model.Invoice{ID: uuid.NewString(), UserID: uuid.NewString(), LavaInvoiceID: "lava-u", OfferID: "off", Plan: "pro", Periodicity: "MONTHLY", Currency: "USD", Amount: 5, Status: "pending"}
	_ = repository.CreateInvoice(db, inv)
	if err := repository.UpdateInvoiceStatus(db, inv.ID, "paid"); err != nil {
		t.Fatalf("UpdateInvoiceStatus: %v", err)
	}
	got, _ := repository.FindInvoiceByID(db, inv.ID)
	if got.Status != "paid" {
		t.Errorf("expected status=paid, got %q", got.Status)
	}
	if err := repository.UpdateInvoiceStatus(db, "missing", "paid"); err != repository.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}
