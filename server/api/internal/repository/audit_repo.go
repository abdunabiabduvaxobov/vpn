package repository

import (
	"context"
	"fmt"

	"vpnapp/server/api/internal/model"

	"gorm.io/gorm"
)

// CreateAuditEntry inserts a single audit log row. Callers build the
// entry struct in the middleware; the repository keeps its signature
// flat so audit writes are a single DB round-trip on the happy path.
func CreateAuditEntry(ctx context.Context, db *gorm.DB, entry *model.AuditLogEntry) error {
	if db == nil {
		return errNilDB
	}
	if err := db.WithContext(ctx).Create(entry).Error; err != nil {
		return fmt.Errorf("creating audit entry: %w", err)
	}
	return nil
}

// ListAuditEntries returns the most recent audit rows, newest first.
// The limit is capped at 200 so a runaway query can't dump the whole
// table into a single response. The UI paginates with page+limit the
// same way the users list does.
func ListAuditEntries(ctx context.Context, db *gorm.DB, page, limit int) ([]model.AuditLogEntry, int64, error) {
	if db == nil {
		return nil, 0, errNilDB
	}
	// Thread the request ctx onto the connection once; the Count and the
	// Find below reuse the same context-bound session.
	db = db.WithContext(ctx)
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if page < 1 {
		page = 1
	}

	var total int64
	if err := db.Model(&model.AuditLogEntry{}).Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("counting audit entries: %w", err)
	}

	var entries []model.AuditLogEntry
	offset := (page - 1) * limit
	if err := db.
		Order("created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&entries).Error; err != nil {
		return nil, 0, fmt.Errorf("listing audit entries: %w", err)
	}
	return entries, total, nil
}

// ListAuditEntriesByTarget returns the most recent audit rows whose
// target_id matches the given user, newest first. Backs the admin panel's
// per-user audit history card (ADMIN-02 — "who touched this user?", served by
// idx_audit_log_target_id from migration 014). Paginated with page+limit the
// same way ListAuditEntries is; limit capped at 200, default 50.
func ListAuditEntriesByTarget(ctx context.Context, db *gorm.DB, targetID string, page, limit int) ([]model.AuditLogEntry, int64, error) {
	if db == nil {
		return nil, 0, errNilDB
	}
	db = db.WithContext(ctx)
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if page < 1 {
		page = 1
	}

	var total int64
	if err := db.Model(&model.AuditLogEntry{}).
		Where("target_id = ?", targetID).
		Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("counting audit entries for target %s: %w", targetID, err)
	}

	var entries []model.AuditLogEntry
	offset := (page - 1) * limit
	if err := db.
		Where("target_id = ?", targetID).
		Order("created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&entries).Error; err != nil {
		return nil, 0, fmt.Errorf("listing audit entries for target %s: %w", targetID, err)
	}
	return entries, total, nil
}

// ListConnectionsByUser returns the N most recent connections for a
// user, newest first. Used by the per-user connection history card on
// the admin panel's user detail page.
func ListConnectionsByUser(ctx context.Context, db *gorm.DB, userID string, limit int) ([]model.Connection, error) {
	if db == nil {
		return nil, errNilDB
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var conns []model.Connection
	if err := db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("connected_at DESC").
		Limit(limit).
		Find(&conns).Error; err != nil {
		return nil, fmt.Errorf("listing connections for %s: %w", userID, err)
	}
	return conns, nil
}
