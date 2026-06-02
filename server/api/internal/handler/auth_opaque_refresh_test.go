package handler

import (
	"regexp"
	"strings"
	"testing"
)

// opaqueRefreshPattern is the HARD-03 contract: the refresh token must be an
// opaque 43-char base64url string (32 random bytes, raw-url-encoded, no padding)
// — NOT a JWT. 43 chars is the exact length of base64url(32 bytes) without "=".
var opaqueRefreshPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

// TestGenerateTokens_RefreshIsOpaque pins HARD-03 (D-?): generateTokens must
// mint an opaque refresh token, not a signed JWT. A JWT refresh token is
// self-describing, replayable until expiry, and carries claims the server must
// trust; an opaque random token is a pure lookup key the server controls.
//
// RED now: generateTokens currently signs a JWT (auth.go:621-632), so the
// refresh field contains two `.` separators and far exceeds 43 chars. This test
// fails until HARD-03 swaps it for a 43-char base64url opaque token.
func TestGenerateTokens_RefreshIsOpaque(t *testing.T) {
	const secret = "test-jwt-secret-32-bytes-minimum!"

	resp, err := generateTokens("user-123", "free", "user", "Test User", "plan-abc", secret)
	if err != nil {
		t.Fatalf("generateTokens: %v", err)
	}

	refresh := resp.RefreshToken

	if strings.Contains(refresh, ".") {
		t.Errorf("HARD-03: refresh token contains '.' — it is still a JWT (%q); want opaque base64url", refresh)
	}

	if !opaqueRefreshPattern.MatchString(refresh) {
		t.Errorf("HARD-03: refresh token %q (len=%d) does not match ^[A-Za-z0-9_-]{43}$ — want 43-char opaque base64url",
			refresh, len(refresh))
	}
}
