package handler

import (
	"testing"
)

// TestAdminUpdateUser_RoleChange_WritesBeforeAfterDiff pins HARD-07 (D-15):
// when an admin changes a user's role, the audit row's details JSONB must carry
// the field-level diff, e.g. details.role = {"before":"user","after":"admin"}.
//
// Today AuditLog (audit.go:70-90) records method/path/params/query but NO body
// diff (S9-4), so a role escalation leaves no record of WHAT changed — only
// that the endpoint was hit. HARD-07 captures the before/after inside
// AdminUpdateUser (admin.go:128) and attaches the diff to model.AuditDetails.
//
// SKIP (compiling) now: the diff-capture path does not exist yet. When HARD-07
// lands, replace this skip with: seed a user with role="user" on a testcontainer
// Postgres, PATCH /admin/users/:id with {"role":"admin"} through the admin
// chain (AuditLog mounted), then query the latest audit_logs row and assert its
// details JSONB contains role.before=="user" and role.after=="admin". Flips
// GREEN when HARD-07 lands.
func TestAdminUpdateUser_RoleChange_WritesBeforeAfterDiff(t *testing.T) {
	t.Skip("GREEN when HARD-07 role-change before/after audit diff lands in AdminUpdateUser")
}
