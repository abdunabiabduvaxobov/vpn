package apple

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	testIssuer = "https://appleid.apple.com"
	testBundle = "com.flawlssr.risevpn"
	testSvcID  = "services.risevpn.web"
)

// setupTestKeypair generates a fresh RSA-2048 keypair for signing test tokens.
// Each test gets its own keypair so cross-test pollution is impossible.
func setupTestKeypair(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	return priv
}

// stubJWKS implements JWKSource by always returning a single public key.
// Production code substitutes keyfunc.Keyfunc; tests substitute this.
type stubJWKS struct {
	pub *rsa.PublicKey
}

func (s *stubJWKS) Keyfunc(_ *jwt.Token) (interface{}, error) {
	return s.pub, nil
}

func signTestToken(t *testing.T, priv *rsa.PrivateKey, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	s, err := tok.SignedString(priv)
	if err != nil {
		t.Fatalf("SignedString: %v", err)
	}
	return s
}

func newTestVerifier(t *testing.T, priv *rsa.PrivateKey) *Verifier {
	t.Helper()
	return &Verifier{
		kf: &stubJWKS{pub: &priv.PublicKey},
		opts: Options{
			AllowedAudiences: []string{testBundle, testSvcID},
			AllowedIssuer:    testIssuer,
		},
	}
}

func TestVerify_HappyPath(t *testing.T) {
	priv := setupTestKeypair(t)
	v := newTestVerifier(t, priv)
	tok := signTestToken(t, priv, jwt.MapClaims{
		"iss":              testIssuer,
		"aud":              testBundle,
		"sub":              "appleSub123",
		"email":            "u@example.com",
		"email_verified":   "true", // Apple sends string!
		"is_private_email": "false",
		"exp":              time.Now().Add(5 * time.Minute).Unix(),
		"iat":              time.Now().Unix(),
	})

	id, err := v.Verify(context.Background(), tok)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id.Sub != "appleSub123" {
		t.Errorf("Sub: want appleSub123, got %q", id.Sub)
	}
	if id.Email != "u@example.com" {
		t.Errorf("Email: want u@example.com, got %q", id.Email)
	}
	if !id.EmailVerified {
		t.Errorf("EmailVerified: want true")
	}
	if id.IsPrivateRelay {
		t.Errorf("IsPrivateRelay: want false")
	}
}

func TestVerify_AudienceMismatch(t *testing.T) {
	priv := setupTestKeypair(t)
	v := newTestVerifier(t, priv)
	tok := signTestToken(t, priv, jwt.MapClaims{
		"iss": testIssuer,
		"aud": "evil.app", // not in whitelist
		"sub": "appleSub123",
		"exp": time.Now().Add(5 * time.Minute).Unix(),
	})

	_, err := v.Verify(context.Background(), tok)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "audience") {
		t.Errorf("error: want substring 'audience', got %q", err.Error())
	}
}

func TestVerify_ExpiredToken(t *testing.T) {
	priv := setupTestKeypair(t)
	v := newTestVerifier(t, priv)
	tok := signTestToken(t, priv, jwt.MapClaims{
		"iss": testIssuer,
		"aud": testBundle,
		"sub": "appleSub123",
		"exp": time.Now().Add(-10 * time.Minute).Unix(), // expired 10min ago
		"iat": time.Now().Add(-15 * time.Minute).Unix(),
	})

	_, err := v.Verify(context.Background(), tok)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, jwt.ErrTokenExpired) {
		t.Errorf("error: want jwt.ErrTokenExpired chain, got %v", err)
	}
}

func TestVerify_SignatureMismatch(t *testing.T) {
	signingKey := setupTestKeypair(t)
	verifierKey := setupTestKeypair(t) // verifier knows a DIFFERENT key

	v := &Verifier{
		kf: &stubJWKS{pub: &verifierKey.PublicKey},
		opts: Options{
			AllowedAudiences: []string{testBundle},
			AllowedIssuer:    testIssuer,
		},
	}
	tok := signTestToken(t, signingKey, jwt.MapClaims{
		"iss": testIssuer,
		"aud": testBundle,
		"sub": "appleSub123",
		"exp": time.Now().Add(5 * time.Minute).Unix(),
	})

	_, err := v.Verify(context.Background(), tok)
	if err == nil {
		t.Fatal("expected signature mismatch error, got nil")
	}
}

func TestVerify_IssuerMismatch(t *testing.T) {
	priv := setupTestKeypair(t)
	v := newTestVerifier(t, priv)
	tok := signTestToken(t, priv, jwt.MapClaims{
		"iss": "https://attacker.example.com",
		"aud": testBundle,
		"sub": "appleSub123",
		"exp": time.Now().Add(5 * time.Minute).Unix(),
	})

	_, err := v.Verify(context.Background(), tok)
	if err == nil {
		t.Fatal("expected issuer mismatch error, got nil")
	}
}

func TestVerify_AppleEmailVerifiedStringType(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  bool
	}{
		{"string true", "true", true},
		{"string false", "false", false},
		{"empty string", "", false},
		{"garbage", "yes", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			priv := setupTestKeypair(t)
			v := newTestVerifier(t, priv)
			tok := signTestToken(t, priv, jwt.MapClaims{
				"iss":            testIssuer,
				"aud":            testBundle,
				"sub":            "appleSub123",
				"email":          "u@example.com",
				"email_verified": tc.value,
				"exp":            time.Now().Add(5 * time.Minute).Unix(),
			})
			id, err := v.Verify(context.Background(), tok)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if id.EmailVerified != tc.want {
				t.Errorf("EmailVerified: want %v, got %v", tc.want, id.EmailVerified)
			}
		})
	}
}

func TestVerify_PrivateRelayFlag(t *testing.T) {
	priv := setupTestKeypair(t)
	v := newTestVerifier(t, priv)
	tok := signTestToken(t, priv, jwt.MapClaims{
		"iss":              testIssuer,
		"aud":              testBundle,
		"sub":              "appleSub456",
		"email":            "abc@privaterelay.appleid.com",
		"email_verified":   "true",
		"is_private_email": "true",
		"exp":              time.Now().Add(5 * time.Minute).Unix(),
	})
	id, err := v.Verify(context.Background(), tok)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !id.IsPrivateRelay {
		t.Errorf("IsPrivateRelay: want true for is_private_email=\"true\"")
	}
}

// TestVerify_JWKsColdStart proves that apple.New() does not block on the initial
// JWKs fetch. We point JWKsURL at 127.0.0.1:1 (guaranteed unreachable / connection
// refused) — if New() blocked on the network round-trip we'd time out; instead it
// must return a non-nil *Verifier promptly. This covers VALIDATION.md
// `Operational | JWKsColdStart` row and locks the RESEARCH.md §Apple Verifier
// non-blocking guarantee in a regression test.
func TestVerify_JWKsColdStart(t *testing.T) {
	done := make(chan struct{})
	var v *Verifier
	var err error
	go func() {
		v, err = New(Options{
			AllowedIssuer:    testIssuer,
			AllowedAudiences: []string{testBundle, testSvcID},
			JWKsURL:          "http://127.0.0.1:1/auth/keys", // unreachable
		})
		close(done)
	}()
	select {
	case <-done:
		// proceed to assertions
	case <-time.After(2 * time.Second):
		t.Fatal("apple.New() blocked on cold-start JWKs fetch; must be non-blocking")
	}
	if err != nil {
		t.Fatalf("apple.New must not fail on cold start, got: %v", err)
	}
	if v == nil {
		t.Fatal("apple.New must return a non-nil *Verifier even when JWKs URL unreachable")
	}
}
