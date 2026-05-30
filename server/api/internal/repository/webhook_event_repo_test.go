package repository_test

import (
	"context"
	"testing"
	"time"

	"vpnapp/server/api/internal/model"
	"vpnapp/server/api/internal/repository"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupWebhookRepoDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// Pin the connection pool to 1 so in-memory state survives across
	// implicit GORM connection grabs (matches plan_repo_test.go).
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)

	// SQLite does NOT enforce the COALESCE expression UNIQUE index from
	// migration 020 — for unit tests we simulate it by adding a simpler
	// UNIQUE on (event_type, contract_id, payload). That's enough to
	// validate OnConflict{DoNothing} behaviour at the GORM layer; the
	// Postgres-level COALESCE expression is tested by migrations_test.go
	// (plan 03-01 T06).
	stmts := []string{
		`CREATE TABLE lava_webhook_events (
			id TEXT PRIMARY KEY,
			event_type TEXT NOT NULL,
			contract_id TEXT,
			invoice_id TEXT,
			payload TEXT NOT NULL,
			received_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			processed_at TIMESTAMP,
			error TEXT,
			UNIQUE (event_type, contract_id, payload)
		)`,
		`CREATE TABLE lava_contracts (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			contract_id TEXT NOT NULL UNIQUE,
			parent_contract_id TEXT,
			offer_id TEXT NOT NULL,
			plan TEXT NOT NULL,
			periodicity TEXT NOT NULL,
			currency TEXT NOT NULL,
			is_active INTEGER NOT NULL DEFAULT 1,
			started_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			expires_at TIMESTAMP,
			cancelled_at TIMESTAMP,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
	}
	for _, s := range stmts {
		if err := db.Exec(s).Error; err != nil {
			t.Fatalf("DDL: %v", err)
		}
	}
	return db
}

// TestInsertWebhookEventIfNew_Idempotent is the PAY-04 named test (03-VALIDATION.md).
// First insert returns isNew=true; second insert with same natural key returns isNew=false.
func TestInsertWebhookEventIfNew_Idempotent(t *testing.T) {
	db := setupWebhookRepoDB(t)
	payload := `{"timestamp":"2026-05-23T10:00:00Z","contractId":"contract-X"}`
	contractID := "contract-X"
	first := &model.LavaWebhookEvent{
		ID: uuid.NewString(), EventType: "payment.success",
		ContractID: &contractID, Payload: datatypes.JSON(payload),
	}
	isNew, err := repository.InsertWebhookEventIfNew(context.Background(), db, first)
	if err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if !isNew {
		t.Errorf("first insert must return isNew=true")
	}

	// Re-insert the SAME natural key.
	dup := &model.LavaWebhookEvent{
		ID: uuid.NewString(), EventType: "payment.success",
		ContractID: &contractID, Payload: datatypes.JSON(payload),
	}
	isNew2, err := repository.InsertWebhookEventIfNew(context.Background(), db, dup)
	if err != nil {
		t.Fatalf("dup insert: %v", err)
	}
	if isNew2 {
		t.Errorf("PAY-04: duplicate must return isNew=false (RowsAffected=0)")
	}

	// Confirm exactly one row in the table.
	var n int64
	_ = db.Model(&model.LavaWebhookEvent{}).Count(&n).Error
	if n != 1 {
		t.Errorf("PAY-04: expected exactly 1 row, got %d", n)
	}
}

func TestMarkWebhookProcessed_SetsProcessedAtOrError(t *testing.T) {
	db := setupWebhookRepoDB(t)
	ev := &model.LavaWebhookEvent{
		ID: uuid.NewString(), EventType: "payment.failed",
		Payload: datatypes.JSON(`{}`),
	}
	if err := db.Create(ev).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := repository.MarkWebhookProcessed(context.Background(), db, ev.ID, nil); err != nil {
		t.Fatalf("MarkWebhookProcessed: %v", err)
	}
	var reloaded model.LavaWebhookEvent
	if err := db.First(&reloaded, "id = ?", ev.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.ProcessedAt == nil {
		t.Errorf("expected processed_at set, got nil")
	}
	// Now mark with an error message.
	errStr := "downstream DB outage"
	if err := repository.MarkWebhookProcessed(context.Background(), db, ev.ID, &errStr); err != nil {
		t.Fatalf("MarkWebhookProcessed error: %v", err)
	}
	if err := db.First(&reloaded, "id = ?", ev.ID).Error; err != nil {
		t.Fatalf("reload2: %v", err)
	}
	if reloaded.Error == nil || *reloaded.Error != "downstream DB outage" {
		t.Errorf("expected error set, got %v", reloaded.Error)
	}
}

func TestFindLavaContractByContractID_FoundAndNotFound(t *testing.T) {
	db := setupWebhookRepoDB(t)
	c := &model.LavaContract{
		ID: uuid.NewString(), UserID: uuid.NewString(), ContractID: "contract-A",
		OfferID: "off-1", Plan: "pro", Periodicity: "MONTHLY", Currency: "USD",
		IsActive: true,
	}
	if err := db.Create(c).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	got, err := repository.FindLavaContractByContractID(context.Background(), db, "contract-A")
	if err != nil || got.ID != c.ID {
		t.Errorf("expected to find contract-A: %+v err=%v", got, err)
	}
	if _, err := repository.FindLavaContractByContractID(context.Background(), db, "missing"); err != repository.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestUpsertLavaContract_InsertThenUpdate(t *testing.T) {
	db := setupWebhookRepoDB(t)
	uid := uuid.NewString()
	c := &model.LavaContract{
		ID: uuid.NewString(), UserID: uid, ContractID: "contract-U",
		OfferID: "off-1", Plan: "pro", Periodicity: "MONTHLY", Currency: "USD",
		IsActive: true,
	}
	if err := repository.UpsertLavaContract(context.Background(), db, c); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	// Update with new expires_at + cancelled_at + parent_contract_id.
	// We assert the DoUpdates AssignmentColumns clause writes these fields
	// even when a contract_id collision triggers the UPDATE branch.
	exp := time.Now().Add(60 * 24 * time.Hour)
	cancelled := time.Now()
	parent := "ctr-parent"
	c2 := &model.LavaContract{
		ID: uuid.NewString(), UserID: uid, ContractID: "contract-U",
		ParentContractID: &parent,
		OfferID:          "off-1", Plan: "pro", Periodicity: "MONTHLY", Currency: "USD",
		IsActive: true, ExpiresAt: &exp, CancelledAt: &cancelled,
	}
	if err := repository.UpsertLavaContract(context.Background(), db, c2); err != nil {
		t.Fatalf("update upsert: %v", err)
	}
	// Confirm there's still exactly one row with contract_id="contract-U" (upsert, not insert).
	var n int64
	_ = db.Model(&model.LavaContract{}).Where("contract_id = ?", "contract-U").Count(&n).Error
	if n != 1 {
		t.Errorf("expected 1 row after upsert, got %d", n)
	}
	// Confirm expires_at + cancelled_at + parent_contract_id were persisted by DoUpdates.
	var reloaded model.LavaContract
	if err := db.Where("contract_id = ?", "contract-U").First(&reloaded).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.ExpiresAt == nil {
		t.Errorf("expected expires_at set after upsert update")
	}
	if reloaded.CancelledAt == nil {
		t.Errorf("expected cancelled_at set after upsert update")
	}
	if reloaded.ParentContractID == nil || *reloaded.ParentContractID != parent {
		t.Errorf("expected parent_contract_id=%q, got %v", parent, reloaded.ParentContractID)
	}
	// Confirm write-once fields (user_id, offer_id, plan, periodicity, currency, started_at)
	// were NOT touched — DoUpdates AssignmentColumns excludes them.
	if reloaded.UserID != uid {
		t.Errorf("user_id must not be rewritten by upsert; got %q want %q", reloaded.UserID, uid)
	}
}
