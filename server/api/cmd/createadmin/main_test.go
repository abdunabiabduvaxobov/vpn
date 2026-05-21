package main

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"

	"vpnapp/server/api/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// HOTFIX-06 regression tests. The createadmin CLI no longer accepts a
// `-password` argv flag (S2-1: argv leaks via ps / journald / docker
// inspect). Passwords are read from stdin instead. Additionally, the
// seeded admin row defaults to subscription_tier='free' (was the
// hard-coded "ultimate" bug — ROADMAP §Phase 1 success criterion #8).

// TestCreateAdmin_RejectsPasswordFlag asserts that the legacy
// `-password=...` argv flag has been removed. Invoking the CLI with
// `-password=anything` must exit non-zero and stderr must contain
// `flag provided but not defined: -password` (Go's stdlib `flag`
// package wording).
//
// Implementation: spawns the CLI via `go run ./cmd/createadmin` in a
// subprocess so the assertion is end-to-end (the package's own main()
// is what flag.Parse executes against).
func TestCreateAdmin_RejectsPasswordFlag(t *testing.T) {
	// Resolve the createadmin package directory relative to this test.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	cmd := exec.Command("go", "run", ".", "-email=x@x.com", "-password=hunter2")
	cmd.Dir = wd
	// Force a TTY-less stdin so the CLI doesn't block waiting for a
	// password prompt. The argv rejection happens at flag.Parse() before
	// any password read, so stdin shape is irrelevant — but we close
	// stdin defensively in case error paths diverge.
	cmd.Stdin = strings.NewReader("")

	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected non-zero exit when -password is passed, got success.\noutput:\n%s", out)
	}

	combined := string(out)
	if !strings.Contains(combined, "flag provided but not defined: -password") {
		t.Errorf("expected stderr to contain `flag provided but not defined: -password`; got:\n%s", combined)
	}
}

// TestCreateAdmin_SeedsFreeTier asserts that the seeded admin row has
// `subscription_tier='free'`. Phase-1 success criterion #8 part B —
// the previous hard-coded `"ultimate"` literal in main.go was a bug.
//
// Exercises the extracted createAdminUser helper against an in-memory
// sqlite DB (matching the pattern from
// internal/repository/subscription_repo_test.go).
func TestCreateAdmin_SeedsFreeTier(t *testing.T) {
	db := openSeedTestDB(t)

	emailHash := fmt.Sprintf("%x", sha256.Sum256([]byte("admin@example.com")))
	// bcrypt-shaped placeholder: cost/salt/digest fields are not exercised
	// by this test (we only assert the inserted row), so a fixed string is
	// enough.
	passwordHash := []byte("$2a$10$abcdefghijklmnopqrstuv1234567890.placeholder.hash.bytes")

	user, err := createAdminUser(db, emailHash, passwordHash)
	if err != nil {
		t.Fatalf("createAdminUser returned unexpected error: %v", err)
	}

	if user.SubscriptionTier != "free" {
		t.Errorf("expected SubscriptionTier='free', got %q (Phase-1 success criterion #8 part B)", user.SubscriptionTier)
	}
	if user.Role != "admin" {
		t.Errorf("expected Role='admin', got %q", user.Role)
	}

	// Re-read from the DB to confirm what was actually persisted (not
	// just what the in-memory struct holds).
	var fromDB model.User
	if err := db.Where("id = ?", user.ID).First(&fromDB).Error; err != nil {
		t.Fatalf("re-reading admin user from db: %v", err)
	}
	if fromDB.SubscriptionTier != "free" {
		t.Errorf("expected persisted subscription_tier='free', got %q", fromDB.SubscriptionTier)
	}
	if fromDB.Role != "admin" {
		t.Errorf("expected persisted role='admin', got %q", fromDB.Role)
	}
}

// TestCreateAdmin_AcceptsPipedStdin asserts that when stdin is not a
// TTY (CI / automation / `echo password | ./createadmin`), readPassword
// falls back to a plain bufio line read instead of calling
// term.ReadPassword (which would fail with "inappropriate ioctl for
// device" on a pipe).
//
// We exercise readPassword() directly with a piped *os.File so the
// test is deterministic and fast (no go-build subprocess required).
func TestCreateAdmin_AcceptsPipedStdin(t *testing.T) {
	const want = "hunter2x"

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })

	// Write the password (with trailing newline) and close so EOF
	// terminates the bufio read.
	go func() {
		defer w.Close()
		_, _ = io.WriteString(w, want+"\n")
	}()

	// Discard the "warning: stdin is not a TTY" message that
	// readPassword prints to its prompt sink.
	promptR, promptW, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe (prompt): %v", err)
	}
	t.Cleanup(func() {
		_ = promptR.Close()
		_ = promptW.Close()
	})
	go func() { _, _ = io.Copy(io.Discard, promptR) }()

	got, err := readPassword(r, promptW)
	if err != nil {
		t.Fatalf("readPassword returned unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("readPassword on piped stdin: got %q, want %q", got, want)
	}
}

// openSeedTestDB opens an in-memory sqlite DB with the users table
// schema needed for createAdminUser. Mirrors the pattern in
// internal/repository/subscription_repo_test.go (openTestDB) so the
// test reads naturally against the rest of the codebase.
func openSeedTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	stmt := `CREATE TABLE IF NOT EXISTS users (
		id                      TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
		email_hash              TEXT NOT NULL UNIQUE,
		password_hash           TEXT NOT NULL,
		full_name               TEXT NOT NULL DEFAULT '',
		subscription_tier       TEXT NOT NULL DEFAULT 'free',
		subscription_expires_at DATETIME,
		role                    TEXT NOT NULL DEFAULT 'user',
		telegram_user_id        INTEGER UNIQUE,
		telegram_linked_at      DATETIME,
		telegram_username       TEXT,
		telegram_first_name     TEXT,
		created_at              DATETIME,
		updated_at              DATETIME
	)`
	if err := db.Exec(stmt).Error; err != nil {
		t.Fatalf("failed to create users table: %v", err)
	}
	return db
}
