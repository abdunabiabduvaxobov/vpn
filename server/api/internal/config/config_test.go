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
