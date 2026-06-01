// createadmin is a one-shot CLI that inserts an admin user into the VPN API
// database. It is intended to be run once per environment to bootstrap the
// first admin account, after which further admins can be promoted via the
// /admin/users/:id endpoint.
//
// Usage:
//
//	DATABASE_URL=postgres://... ./createadmin -email=you@example.com
//
// The password is read from stdin (echo-off when stdin is a TTY; piped
// fallback otherwise). Passing the password on the command line is no
// longer supported — argv is a leak surface (ps, shell history, journald,
// container inspect). See SECURITY-AUDIT S2-1 / HOTFIX-06.
//
// The command is idempotent in the sense that it will refuse to overwrite an
// existing user with the same email.
package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"vpnapp/server/api/internal/model"
	"vpnapp/server/api/internal/repository"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"

	"gorm.io/gorm"
)

func main() {
	email := flag.String("email", "", "admin email address")
	dbURL := flag.String("db", os.Getenv("DATABASE_URL"), "database URL (defaults to $DATABASE_URL)")
	flag.Parse()

	if *email == "" {
		fmt.Fprintln(os.Stderr, "usage: createadmin -email=<email> -db=<dsn>  (password is read from stdin)")
		flag.Usage()
		os.Exit(2)
	}
	if *dbURL == "" {
		log.Fatal("database URL required: set -db flag or DATABASE_URL env var")
	}

	pwd, err := readPassword(os.Stdin, os.Stderr)
	if err != nil {
		log.Fatalf("reading password: %v", err)
	}
	if len(pwd) < 8 || len(pwd) > 72 {
		log.Fatal("password must be 8-72 characters")
	}

	db, err := repository.NewDB(*dbURL)
	if err != nil {
		log.Fatalf("connecting to database: %v", err)
	}

	// Refuse to overwrite an existing account with the same email.
	// Two checks: a friendly pre-flight lookup, and a unique-constraint
	// catch on insert in case two concurrent createadmin runs race.
	emailHash := fmt.Sprintf("%x", sha256.Sum256([]byte(*email)))
	if existing, err := repository.FindUserByEmailHash(context.Background(), db, emailHash); err == nil {
		log.Fatalf("user with email %s already exists (id=%s, role=%s)", *email, existing.ID, existing.Role)
	} else if !errors.Is(err, repository.ErrNotFound) {
		log.Fatalf("checking existing user: %v", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(pwd), bcrypt.DefaultCost)
	if err != nil {
		log.Fatalf("hashing password: %v", err)
	}

	user, err := createAdminUser(db, emailHash, hash)
	if err != nil {
		if errors.Is(err, repository.ErrDuplicate) {
			log.Fatalf("user with email %s already exists (concurrent createadmin?)", *email)
		}
		log.Fatalf("creating admin user: %v", err)
	}

	fmt.Printf("admin user created\n  id:    %s\n  email: %s\n  role:  %s\n", user.ID, *email, user.Role)
}

// readPassword reads an admin password from `in`. When `in` is a terminal,
// the password is read with echo-off via golang.org/x/term.ReadPassword
// (sudo-style prompt). When stdin is piped (CI / automation), it falls
// back to a plain line read via bufio with a stderr warning so operators
// know their password just traveled through the pipe in plaintext.
//
// Argv is intentionally NOT a transport for the password — see HOTFIX-06.
func readPassword(in *os.File, prompt *os.File) (string, error) {
	fd := int(in.Fd())
	if term.IsTerminal(fd) {
		fmt.Fprint(prompt, "Password (8-72 chars): ")
		pwdBytes, err := term.ReadPassword(fd)
		fmt.Fprintln(prompt) // newline because ReadPassword strips it
		if err != nil {
			return "", fmt.Errorf("reading password from terminal: %w", err)
		}
		return string(pwdBytes), nil
	}
	// Stdin is piped (CI, automation). Fall back to plain read; warn so
	// operators know their password just traveled through the pipe in
	// plaintext.
	fmt.Fprintln(prompt, "warning: stdin is not a TTY; reading password from pipe (no echo-off)")
	line, err := bufio.NewReader(in).ReadString('\n')
	// Only io.EOF is acceptable — that's the "operator piped a password
	// without a trailing newline" case. Any other error (closed pipe
	// mid-read, ErrUnexpectedEOF, context cancellation) might leave us
	// with a truncated buffer that bcrypt would happily hash; refuse it
	// so we don't silently accept a partial password as the real one.
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("reading password from stdin: %w", err)
	}
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		return "", fmt.Errorf("reading password from stdin: empty input")
	}
	return line, nil
}

// createAdminUser inserts a new admin user row with the canonical defaults
// (role='admin', subscription_tier='free'). Factored out of main() so it can
// be unit-tested against an in-memory sqlite DB.
//
// emailHash is the SHA-256 hex of the operator-supplied email (the API
// stores email as a hash only — zero-knowledge policy). passwordHash is
// the bcrypt output; the caller is responsible for the cost choice.
//
// Returns ErrDuplicate from the underlying repository if the email_hash
// already exists.
func createAdminUser(db *gorm.DB, emailHash string, passwordHash []byte) (*model.User, error) {
	// D-29: resolve the system plan_id BEFORE the INSERT. users.plan_id is
	// `uuid NOT NULL` (migration 019) with no DB default and model.User.PlanID is
	// a non-pointer string, so an unset value serializes to plan_id='' and
	// Postgres rejects the INSERT with SQLSTATE 22P02. Setting it here mirrors the
	// GuestLogin fix and keeps the app layer as the single source of plan_id.
	systemPlanID, err := repository.FindSystemPlanID(context.Background(), db)
	if err != nil {
		return nil, fmt.Errorf("resolving system plan id: %w", err)
	}
	if systemPlanID == "" {
		return nil, fmt.Errorf("resolving system plan id: no system plan configured")
	}

	hashStr := string(passwordHash)
	user := &model.User{
		EmailHash:        &emailHash,
		PasswordHash:     &hashStr,
		FullName:         "Admin",
		Role:             "admin",
		SubscriptionTier: "free",
		PlanID:           systemPlanID,
	}
	if err := repository.CreateUser(context.Background(), db, user); err != nil {
		return nil, err
	}
	return user, nil
}
