package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vpnapp/server/api/internal/repository"

	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// dryRunDB returns a GORM session in DryRun mode so we can inspect the SQL a
// repository call WOULD generate. SQLite supplies the dialect only — no
// statement is executed, so the ILIKE operator (Postgres-only) never errors.
func dryRunDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DryRun: true,
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("open dry-run db: %v", err)
	}
	return db
}

// TestListUsers_SearchUsesPrefixNoLeadingWildcard pins the HARD-06 (D-14) query
// shape: a valid search must be an index-usable PREFIX match (`search%`), never
// a leading-wildcard contains match (`%search%`). A leading `%` defeats every
// btree index on full_name/email_hash and forces a sequential scan — the S2-3
// admin-search DoS amplifier.
//
// RED now: ListUsers builds `like := "%search%"` (admin_repo.go:28), so the
// bound LIKE argument still begins with `%`. Flips GREEN when HARD-06 rewrites
// the predicate to a prefix match on indexed columns only.
func TestListUsers_SearchUsesPrefixNoLeadingWildcard(t *testing.T) {
	db := dryRunDB(t)

	// In DryRun, ListUsers builds the statement (binding the LIKE pattern into
	// Statement.Vars) without executing it. The repository's internal Count call
	// returns no error under DryRun, so the Where bindings are captured.
	_, _, _ = repository.ListUsers(t.Context(), db, 1, 20, "abcdef")

	foundStringVar := false
	for _, v := range db.Statement.Vars {
		s, ok := v.(string)
		if !ok {
			continue
		}
		foundStringVar = true
		if strings.HasPrefix(s, "%") {
			t.Errorf("HARD-06: search bound a leading-wildcard pattern %q — want index-usable prefix match (no leading %%)", s)
		}
	}
	if !foundStringVar {
		t.Fatalf("HARD-06: no bound search pattern captured — DryRun did not record the LIKE arg (Vars=%v); adjust capture", db.Statement.Vars)
	}
}

// adminListApp mounts AdminListUsers directly behind a shim that supplies the
// admin locals AdminRequired would normally set, so we can exercise the
// handler's own validation without the full admin middleware chain.
func adminListApp(db *gorm.DB) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user_id", "admin-1")
		c.Locals("role", "admin")
		return c.Next()
	})
	app.Get("/admin/users", AdminListUsers(zap.NewNop(), db))
	return app
}

// TestAdminListUsers_ShortSearchRejected pins HARD-06 (D-14): a search shorter
// than 3 chars must be rejected with 400. Sub-3-char searches match nearly
// every row, re-creating the sequential-scan DoS the prefix rewrite is meant to
// prevent.
//
// RED now: AdminListUsers (admin.go:40) reads `search` and passes it straight
// to ListUsers with no length guard, so a 2-char search returns 200. Flips
// GREEN when HARD-06 adds the len<3 -> 400 guard.
func TestAdminListUsers_ShortSearchRejected(t *testing.T) {
	app := adminListApp(dryRunDB(t))

	req := httptest.NewRequest(http.MethodGet, "/admin/users?search=ab", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("HARD-06: search=ab (len<3) returned %d, want 400", resp.StatusCode)
	}
}
