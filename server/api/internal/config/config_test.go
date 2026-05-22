package config_test

import (
	"testing"

	"vpnapp/server/api/internal/config"
)

// HOTFIX-08 regression tests: fail-fast aggregate env validator. The required
// set is intentionally the v2.1.0 runtime core only (D-03): JWT_SECRET,
// DATABASE_URL, REDIS_URL, TUNNEL_VLESS_UUID. LAVA_* keys are NOT in this set
// (Phase 3 owns that). Stripe vars are optional-with-warn (Stripe leaves in
// Phase 8). The validator scans every var in one pass (D-04) so the operator
// sees ALL missing keys in one structured log line.

func TestRequireEnv_ReturnsAllMissingKeys(t *testing.T) {
	// Force every required var to empty via t.Setenv (automatically restored
	// after the test by Go's testing package).
	t.Setenv("JWT_SECRET", "")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("REDIS_URL", "")
	t.Setenv("TUNNEL_VLESS_UUID", "")

	missing := config.RequireEnv()

	wantKeys := []string{"JWT_SECRET", "DATABASE_URL", "REDIS_URL", "TUNNEL_VLESS_UUID"}
	if len(missing) != len(wantKeys) {
		t.Fatalf("expected %d missing keys, got %d: %v", len(wantKeys), len(missing), missing)
	}
	for _, want := range wantKeys {
		found := false
		for _, got := range missing {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %q in missing, got %v", want, missing)
		}
	}
}

func TestRequireEnv_ReturnsEmptyWhenAllSet(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-not-empty")
	t.Setenv("DATABASE_URL", "postgres://localhost:5432/test?sslmode=disable")
	t.Setenv("REDIS_URL", "redis://localhost:6379")
	t.Setenv("TUNNEL_VLESS_UUID", "00000000-0000-4000-8000-000000000000")

	missing := config.RequireEnv()

	if len(missing) != 0 {
		t.Fatalf("expected 0 missing keys when all required vars set, got %d: %v", len(missing), missing)
	}
}

// TestLoad_RecordsParseWarnings asserts that an invalid tunable env var
// (set but unparseable) shows up in cfg.EnvParseWarnings rather than
// being silently swallowed. Regression test for REVIEW.md WR-05.
func TestLoad_RecordsParseWarnings(t *testing.T) {
	// Required vars must be set so Load() reaches the helper calls
	// without returning the "JWT_SECRET is required" early-out.
	t.Setenv("JWT_SECRET", "test-secret-not-empty")
	t.Setenv("DATABASE_URL", "postgres://localhost:5432/test?sslmode=disable")
	t.Setenv("REDIS_URL", "redis://localhost:6379")
	t.Setenv("TUNNEL_VLESS_UUID", "00000000-0000-4000-8000-000000000000")

	// Invalid duration: "3min" is a common operator typo for "3m".
	t.Setenv("STALE_CONNECTION_AFTER", "3min")
	// Invalid int64: trailing letter.
	t.Setenv("TELEGRAM_ADMIN_CHAT_ID", "12345x")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}

	if len(cfg.EnvParseWarnings) < 2 {
		t.Fatalf("expected >=2 parse warnings (STALE_CONNECTION_AFTER + TELEGRAM_ADMIN_CHAT_ID), got %d: %v",
			len(cfg.EnvParseWarnings), cfg.EnvParseWarnings)
	}

	var sawDuration, sawInt64 bool
	for _, w := range cfg.EnvParseWarnings {
		if contains(w, "STALE_CONNECTION_AFTER") {
			sawDuration = true
		}
		if contains(w, "TELEGRAM_ADMIN_CHAT_ID") {
			sawInt64 = true
		}
	}
	if !sawDuration {
		t.Errorf("expected STALE_CONNECTION_AFTER in EnvParseWarnings, got %v", cfg.EnvParseWarnings)
	}
	if !sawInt64 {
		t.Errorf("expected TELEGRAM_ADMIN_CHAT_ID in EnvParseWarnings, got %v", cfg.EnvParseWarnings)
	}
}

// TestLoad_NoParseWarningsForValidOrUnset asserts that valid (or unset)
// tunables produce no warnings. Companion to TestLoad_RecordsParseWarnings.
func TestLoad_NoParseWarningsForValidOrUnset(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-not-empty")
	t.Setenv("DATABASE_URL", "postgres://localhost:5432/test?sslmode=disable")
	t.Setenv("REDIS_URL", "redis://localhost:6379")
	t.Setenv("TUNNEL_VLESS_UUID", "00000000-0000-4000-8000-000000000000")

	// Valid duration.
	t.Setenv("STALE_CONNECTION_AFTER", "30s")
	// Unset (must NOT warn — operator chose the default).
	t.Setenv("STALE_DEVICE_AFTER", "")
	// Valid int64.
	t.Setenv("TELEGRAM_ADMIN_CHAT_ID", "401485415")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}

	if len(cfg.EnvParseWarnings) != 0 {
		t.Errorf("expected 0 parse warnings for valid/unset tunables, got %d: %v",
			len(cfg.EnvParseWarnings), cfg.EnvParseWarnings)
	}
}

// contains is a tiny strings.Contains-free helper to keep the test file
// import list minimal — there's no functional reason, just stylistic.
func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestOptionalEnvWarnings_FlagsPlaceholders(t *testing.T) {
	// Clear unrelated optional vars so the assertion is precise. The
	// placeholder for STRIPE_PRICE_PREMIUM must be flagged even though it
	// is non-empty — that's the whole point of the "placeholder" path.
	t.Setenv("STRIPE_KEY", "sk_test_realvalue")
	t.Setenv("STRIPE_WEBHOOK_SECRET", "whsec_realvalue")
	t.Setenv("STRIPE_PRICE_PREMIUM", "price_PLACEHOLDER_PREMIUM")
	t.Setenv("STRIPE_PRICE_ULTIMATE", "price_realvalue")

	warned := config.OptionalEnvWarnings()

	foundPremium := false
	for _, key := range warned {
		if key == "STRIPE_PRICE_PREMIUM" {
			foundPremium = true
			break
		}
	}
	if !foundPremium {
		t.Errorf("expected STRIPE_PRICE_PREMIUM in warned (placeholder value should be flagged), got %v", warned)
	}

	// Vars set to real values must NOT appear.
	for _, key := range warned {
		switch key {
		case "STRIPE_KEY", "STRIPE_WEBHOOK_SECRET", "STRIPE_PRICE_ULTIMATE":
			t.Errorf("unexpected %q in warned (real value should not flag): %v", key, warned)
		}
	}
}
