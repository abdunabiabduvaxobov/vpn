package config_test

import (
	"testing"

	"vpnapp/server/api/internal/config"
)

// HOTFIX-08 regression tests: fail-fast aggregate env validator. The required
// set is intentionally the v2.1.0 runtime core only (D-03): JWT_SECRET,
// DATABASE_URL, REDIS_URL, TUNNEL_VLESS_UUID. LAVA_* keys are NOT in this set
// (Phase 3 owns that). The reserved Apple .p8 keys are optional-with-warn. The
// validator scans every var in one pass (D-04) so the operator sees ALL missing
// keys in one structured log line.

func TestRequireEnv_ReturnsAllMissingKeys(t *testing.T) {
	// Force every required var to empty via t.Setenv (automatically restored
	// after the test by Go's testing package). Phase 2 (AUTH-03) extended the
	// required set with six SSO keys (D-30); future phases will add more.
	// Assertion is "must-contain" rather than "exact set" so adding keys in
	// later phases doesn't regress this test. See TestRequireEnv_MissingSSOKeys_Reported
	// below for the phase-scoped exact assertion.
	t.Setenv("JWT_SECRET", "")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("REDIS_URL", "")
	t.Setenv("TUNNEL_VLESS_UUID", "")
	t.Setenv("APPLE_TEAM_ID", "")
	t.Setenv("APPLE_BUNDLE_ID", "")
	t.Setenv("APPLE_SERVICE_ID", "")
	t.Setenv("GOOGLE_CLIENT_ID_IOS", "")
	t.Setenv("GOOGLE_CLIENT_ID_ANDROID", "")
	t.Setenv("GOOGLE_CLIENT_ID_WEB", "")

	missing := config.RequireEnv()

	// Must-contain check: the four Phase 1 keys are always required (HOTFIX-08).
	wantKeys := []string{"JWT_SECRET", "DATABASE_URL", "REDIS_URL", "TUNNEL_VLESS_UUID"}
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
	// Sanity floor — the slice must at least contain the Phase 1 keys.
	if len(missing) < len(wantKeys) {
		t.Fatalf("expected at least %d missing keys, got %d: %v", len(wantKeys), len(missing), missing)
	}
}

func TestRequireEnv_ReturnsEmptyWhenAllSet(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-not-empty")
	t.Setenv("DATABASE_URL", "postgres://localhost:5432/test?sslmode=disable")
	t.Setenv("REDIS_URL", "redis://localhost:6379")
	t.Setenv("TUNNEL_VLESS_UUID", "00000000-0000-4000-8000-000000000000")
	// Phase 2 (AUTH-03) — every required SSO key (D-30) must also be set
	// for RequireEnv() to return an empty slice.
	t.Setenv("APPLE_TEAM_ID", "team-test")
	t.Setenv("APPLE_BUNDLE_ID", "com.flawlssr.risevpn")
	t.Setenv("APPLE_SERVICE_ID", "services.risevpn.web")
	t.Setenv("GOOGLE_CLIENT_ID_IOS", "ios-client.apps.googleusercontent.com")
	t.Setenv("GOOGLE_CLIENT_ID_ANDROID", "android-client.apps.googleusercontent.com")
	t.Setenv("GOOGLE_CLIENT_ID_WEB", "web-client.apps.googleusercontent.com")
	// Phase 3 (PAY-16) — LAVA_* required keys must also be set.
	t.Setenv("LAVA_ENV", "production")
	t.Setenv("LAVA_API_KEY", "lava-live-key")
	t.Setenv("LAVA_WEBHOOK_SECRET", "whsec-test")
	t.Setenv("LAVA_WEBHOOK_ALLOWED_CIDRS", "0.0.0.0/0")
	t.Setenv("LAVA_SUCCESS_URL", "https://risevpn.com/checkout/success")
	t.Setenv("LAVA_FAIL_URL", "https://risevpn.com/checkout/fail")
	// Phase 7 (ADMIN-07 / T-07-08) — tunnel-heartbeat shared secret is required.
	t.Setenv("INTERNAL_HEARTBEAT_SECRET", "internal-hb-secret")

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

// TestRequireEnv_MissingSSOKeys_Reported asserts the Phase 2 AUTH-03 wiring:
// the six required SSO env keys (D-30) flow through the same HOTFIX-08
// aggregate validator that already enforces JWT_SECRET, DATABASE_URL,
// REDIS_URL, TUNNEL_VLESS_UUID. With the Phase 1 keys set and the six new
// SSO keys empty, RequireEnv() must report exactly the six SSO keys —
// nothing more, nothing less. This is the boot-time safety net that
// prevents a deploy with APPLE_BUNDLE_ID="" from accepting any Apple
// token (threat T-2-EnvBoot — empty audience whitelist would otherwise
// match attacker-minted tokens). See VALIDATION.md "Operational" row.
func TestRequireEnv_MissingSSOKeys_Reported(t *testing.T) {
	// Phase 1 required keys all set so the missing list is purely the
	// SSO subset.
	t.Setenv("JWT_SECRET", "x")
	t.Setenv("DATABASE_URL", "x")
	t.Setenv("REDIS_URL", "x")
	t.Setenv("TUNNEL_VLESS_UUID", "x")
	// All six SSO required keys explicitly empty.
	t.Setenv("APPLE_TEAM_ID", "")
	t.Setenv("APPLE_BUNDLE_ID", "")
	t.Setenv("APPLE_SERVICE_ID", "")
	t.Setenv("GOOGLE_CLIENT_ID_IOS", "")
	t.Setenv("GOOGLE_CLIENT_ID_ANDROID", "")
	t.Setenv("GOOGLE_CLIENT_ID_WEB", "")
	// Phase 3 (PAY-16) — LAVA_* keys set so they don't appear in missing list.
	t.Setenv("LAVA_ENV", "production")
	t.Setenv("LAVA_API_KEY", "x")
	t.Setenv("LAVA_WEBHOOK_SECRET", "x")
	t.Setenv("LAVA_WEBHOOK_ALLOWED_CIDRS", "0.0.0.0/0")
	t.Setenv("LAVA_SUCCESS_URL", "https://x")
	t.Setenv("LAVA_FAIL_URL", "https://x")
	// Phase 7 — set so the missing list stays the pure SSO subset.
	t.Setenv("INTERNAL_HEARTBEAT_SECRET", "x")

	missing := config.RequireEnv()

	want := map[string]bool{
		"APPLE_TEAM_ID":            true,
		"APPLE_BUNDLE_ID":          true,
		"APPLE_SERVICE_ID":         true,
		"GOOGLE_CLIENT_ID_IOS":     true,
		"GOOGLE_CLIENT_ID_ANDROID": true,
		"GOOGLE_CLIENT_ID_WEB":     true,
	}
	if len(missing) != len(want) {
		t.Fatalf("expected %d missing keys, got %d: %v", len(want), len(missing), missing)
	}
	for _, k := range missing {
		if !want[k] {
			t.Errorf("unexpected key in missing: %q", k)
		}
	}
}

func TestOptionalEnvWarnings_FlagsUnsetOptionalKeys(t *testing.T) {
	// As of Phase 8 the optional set is the two reserved Apple .p8 keys
	// (D-30). An UNSET optional key must be flagged so a deploy that forgot
	// to configure the future authorizationCode exchange is visible; a key
	// set to a real value must NOT be flagged.
	t.Setenv("APPLE_KEY_ID", "")                   // unset -> must be flagged
	t.Setenv("APPLE_PRIVATE_KEY_P8", "real-p8-pem") // real value -> must NOT be flagged

	warned := config.OptionalEnvWarnings()

	foundKeyID := false
	for _, key := range warned {
		if key == "APPLE_KEY_ID" {
			foundKeyID = true
			break
		}
	}
	if !foundKeyID {
		t.Errorf("expected APPLE_KEY_ID in warned (unset optional key should be flagged), got %v", warned)
	}

	// Keys set to real values must NOT appear.
	for _, key := range warned {
		if key == "APPLE_PRIVATE_KEY_P8" {
			t.Errorf("unexpected %q in warned (real value should not flag): %v", key, warned)
		}
	}
}
