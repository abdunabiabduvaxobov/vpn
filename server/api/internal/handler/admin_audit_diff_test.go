package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"vpnapp/server/api/internal/middleware"
	"vpnapp/server/api/internal/model"

	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// openAuditTestDB opens an in-memory SQLite database with just the audit_log
// table. AuditDetails round-trips through its driver.Valuer/Scanner (stored as
// JSON text), so SQLite is sufficient to assert the merged details payload
// without standing up Postgres. The id default lets GORM omit id on INSERT and
// let the DB fill it.
func openAuditTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("open audit test db: %v", err)
	}
	stmt := `CREATE TABLE IF NOT EXISTS audit_log (
		id         TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
		admin_id   TEXT NOT NULL,
		action     TEXT NOT NULL,
		target_id  TEXT,
		details    TEXT,
		ip         TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`
	if err := db.Exec(stmt).Error; err != nil {
		t.Fatalf("create audit_log table: %v", err)
	}
	return db
}

// TestAdminUpdateUser_RoleChange_WritesBeforeAfterDiff pins HARD-07 (D-15 /
// audit S2-4,S9-4): when a privilege-relevant field changes, the audit row's
// details JSONB must carry the field-level before->after diff, e.g.
// details.role = {"before":"user","after":"admin"} — so a role escalation is
// attributable, not just "the endpoint was hit".
//
// AdminUpdateUser (admin.go:287) builds that diff against its `before` snapshot
// and stashes it under c.Locals("audit_details"); the AuditLog middleware
// (audit.go:89) merges it into the persisted details. This test exercises that
// persistence seam directly: a stub handler stashes the same diff shape the
// handler produces, AuditLog runs, and we read the row back from SQLite and
// assert the diff survived into details.role.before/after.
func TestAdminUpdateUser_RoleChange_WritesBeforeAfterDiff(t *testing.T) {
	db := openAuditTestDB(t)

	const adminID = "11111111-1111-1111-1111-111111111111"
	const targetID = "22222222-2222-2222-2222-222222222222"

	app := fiber.New()
	app.Use(middleware.AuditLog(db, zap.NewNop()))
	app.Patch("/api/v1/admin/users/:id", func(c *fiber.Ctx) error {
		// Stand in for AuthRequired+AdminRequired and the diff-capture in
		// AdminUpdateUser: set the acting admin and the role before->after diff
		// in the exact shape admin.go produces (map[string]map[string]any).
		c.Locals("user_id", adminID)
		c.Locals("audit_details", map[string]map[string]any{
			"role": {"before": "user", "after": "admin"},
		})
		return c.JSON(fiber.Map{"data": fiber.Map{"id": c.Params("id"), "updated": fiber.Map{"role": "admin"}}})
	})

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/users/"+targetID, nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("handler returned %d, want 200 (AuditLog only records 2xx mutations)", resp.StatusCode)
	}

	var entry model.AuditLogEntry
	if err := db.Order("created_at DESC").First(&entry).Error; err != nil {
		t.Fatalf("HARD-07: no audit row written for the role change: %v", err)
	}

	if entry.Action != "update_user" {
		t.Errorf("HARD-07: audit action = %q, want %q", entry.Action, "update_user")
	}
	if entry.AdminID != adminID {
		t.Errorf("HARD-07: audit admin_id = %q, want %q", entry.AdminID, adminID)
	}
	if entry.TargetID == nil || *entry.TargetID != targetID {
		t.Errorf("HARD-07: audit target_id = %v, want %q", entry.TargetID, targetID)
	}

	roleDiff, ok := entry.Details["role"].(map[string]any)
	if !ok {
		t.Fatalf("HARD-07: details.role missing or wrong type — got %T (%v); the before/after diff was not persisted", entry.Details["role"], entry.Details["role"])
	}
	if roleDiff["before"] != "user" {
		t.Errorf("HARD-07: details.role.before = %v, want \"user\"", roleDiff["before"])
	}
	if roleDiff["after"] != "admin" {
		t.Errorf("HARD-07: details.role.after = %v, want \"admin\"", roleDiff["after"])
	}
}
