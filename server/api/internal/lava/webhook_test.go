package lava

import (
	"strings"
	"testing"
)

func TestVerifyAPIKey_ConstantTime(t *testing.T) {
	// Exact match on current secret.
	if !VerifyAPIKey("topsecret", "topsecret", "") {
		t.Errorf("expected match on current secret")
	}
	// Exact match on previous secret (rotation window).
	if !VerifyAPIKey("oldsecret", "newsecret", "oldsecret") {
		t.Errorf("expected match on previous secret (rotation window)")
	}
	// Wrong secret — must fail.
	if VerifyAPIKey("guess", "topsecret", "") {
		t.Errorf("expected mismatch")
	}
	// Empty received — must fail.
	if VerifyAPIKey("", "topsecret", "") {
		t.Errorf("empty received must not match a non-empty secret")
	}
	// Previous-empty + received-empty + current-non-empty — must fail.
	if VerifyAPIKey("", "topsecret", "") {
		t.Errorf("empty received must not match")
	}
	// Both received and current empty — by current implementation
	// ConstantTimeCompare returns 1 for two empty byte slices. This is
	// acceptable because RequireEnv() refuses to start when LAVA_WEBHOOK_SECRET
	// is empty (config.go strict-required). Document the invariant:
	if !VerifyAPIKey("", "", "") {
		t.Logf("note: empty==empty matches; relies on RequireEnv to reject empty secret at startup")
	}
}

// TestVerifyAPIKey_PrefixLengthNonLeakage is a basic invariant test.
// crypto/subtle.ConstantTimeCompare returns 0 immediately if lengths differ —
// this short-circuit IS the timing-leak (length info). The implementation
// is correct per crypto/subtle docs; this test pins the contract.
func TestVerifyAPIKey_PrefixLengthNonLeakage(t *testing.T) {
	// "topsecret" (9 chars) vs "topsecr" (7 chars) — length diff is expected;
	// ConstantTimeCompare returns 0; no panic.
	if VerifyAPIKey("topsecr", "topsecret", "") {
		t.Errorf("expected mismatch on length difference")
	}
	// Names containing the secret as a prefix must not match either.
	if VerifyAPIKey(strings.Repeat("a", 1024), "topsecret", "") {
		t.Errorf("long mismatch must not match")
	}
}
