package handler

// Durable SC#1 regression fence (HARD-01). Phase 8 plan 08-05 removed the last
// residual Stripe code and the stripe-go dependency. A one-shot execution-time
// grep would only prove the tree was clean at execution time; this test re-runs
// the grep at EVERY `go test`, so a future PR that re-adds a stripe reference
// (a stripe_id fixture column, a stripe-go import, a stale comment) fails the
// suite — and CI (ubuntu-latest, which always ships grep) — instead of silently
// reopening SC#1.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// allowlistedStripeFiles are the ONLY .go files permitted to contain the word
// "stripe". Keep this list minimal and explicit so a future reader can see the
// fence is intentional and not a loophole. A new entry here means a new
// legitimate reason a stripe mention is allowed — review carefully.
var allowlistedStripeFiles = []string{
	// This fence file itself necessarily contains the literal search term
	// "stripe" in its allowlist, message strings, and the grep argument.
	"stripe_removal_test.go",
	// migrations_test.go (server/api/migrations) asserts that the legacy
	// subscriptions.stripe_id column was DROPPED by migration 020 (D-01/D-11).
	// That absence assertion legitimately names the dropped column, so it is
	// the one intentional remaining "stripe" mention and is the SC#1 evidence
	// the column is gone — not a regression. Do NOT remove this assertion.
	"migrations_test.go",
}

// findServerRoot walks up from the test's working directory until it finds a
// directory named "server", returning its absolute path. This resolves to the
// repo-level server/ dir (which holds the api, tunnel, and infra Go trees), so
// the fence covers a stripe reference reintroduced in EITHER Go module, not
// just this one. Returns ("", false) if no such ancestor exists.
func findServerRoot() (string, bool) {
	dir, err := os.Getwd()
	if err != nil {
		return "", false
	}
	for {
		if filepath.Base(dir) == "server" {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached the filesystem root without finding "server".
			return "", false
		}
		dir = parent
	}
}

// TestNoStripeReferences is the durable SC#1 fence. It greps every .go file
// under the repo server/ root (case-insensitive) for "stripe" and fails the
// suite if any non-allowlisted match exists.
func TestNoStripeReferences(t *testing.T) {
	// CI (ubuntu-latest) always has grep, so the gate is live in CI. On a
	// runner without grep we skip rather than fail — a missing tool must not
	// masquerade as a stripe regression.
	if _, err := exec.LookPath("grep"); err != nil {
		t.Skip("grep not found on PATH; the SC#1 fence is live in CI (ubuntu-latest always has grep)")
	}

	serverRoot, ok := findServerRoot()
	if !ok {
		// Do NOT silently pass — an unresolved root would make the fence a
		// no-op, which is exactly the regression this test guards against.
		t.Fatalf("could not resolve the repo server/ root by walking up from the test working dir; fence cannot run")
	}

	// -r recursive, -n line numbers, -i case-insensitive, -I skip binary files,
	// --include scopes to Go source. grep exits 1 (and err is non-nil) when
	// there is no match — that is the PASS case, so the error is intentionally
	// ignored and we inspect the captured output instead.
	cmd := exec.Command("grep", "-rniI", "--include=*.go", "stripe", serverRoot)
	out, _ := cmd.CombinedOutput()

	var offending []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		// grep output is "path:line:content"; the path is everything before
		// the first colon. Allowlist by basename so the check is independent
		// of the absolute path the test happens to run from.
		path := line
		if idx := strings.IndexByte(line, ':'); idx >= 0 {
			path = line[:idx]
		}
		base := filepath.Base(path)
		allowed := false
		for _, a := range allowlistedStripeFiles {
			if base == a {
				allowed = true
				break
			}
		}
		if allowed {
			continue
		}
		offending = append(offending, line)
	}

	if len(offending) > 0 {
		t.Fatalf("SC#1 regression: %d stripe reference(s) must be zero:\n%s",
			len(offending), strings.Join(offending, "\n"))
	}
}
