// Package apple verifies Sign in with Apple identity tokens.
//
// Pure-library package per D-12: no DB access, no Fiber types, no globals.
// The handler in internal/handler/auth.go composes this with repository
// lookups; this package only validates a JWT against Apple's JWKs +
// caller-provided issuer + audience whitelist.
//
// IMPORTANT (RESEARCH.md A1): Apple's `email_verified` and `is_private_email`
// claims are STRING-typed ("true"/"false"), not bool. This package handles
// the string typing explicitly. If Apple ever migrates to native booleans,
// the string comparison fails safely (decodes to false) — a real Apple token
// with email_verified=true would then look unverified, which blocks auto-link
// (the safe-failure mode). Re-run the dev capture spike from
// .planning/phases/02-auth-sso-backend/02-VALIDATION.md "Manual-Only" before
// changing this decoding logic.
package apple

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
)

// defaultAppleJWKsURL is the well-known JWKs endpoint published by Apple.
// HTTPS-only — D-CD threat-model row T-2-JWKsMITM forbids InsecureSkipVerify.
// Production code uses this URL; tests inject an unreachable URL to exercise
// the cold-start non-blocking guarantee (TestVerify_JWKsColdStart).
const defaultAppleJWKsURL = "https://appleid.apple.com/auth/keys"

// clockSkewLeeway absorbs minor clock drift between our server and Apple's
// issuer. Per RESEARCH.md §Race & Failure Modes #5 — 30s is the standard
// leeway used in production OIDC verifiers.
const clockSkewLeeway = 30 * time.Second

// AppleIdentity is the verified-identity output of Verify.
type AppleIdentity struct {
	Sub            string
	Email          string
	EmailVerified  bool
	IsPrivateRelay bool
}

// Options configures the audience + issuer whitelist for a Verifier.
// Constructed ONCE at server startup per D-34 — never per-request.
type Options struct {
	AllowedAudiences []string
	AllowedIssuer    string
	// JWKsURL overrides the default Apple JWKs endpoint. Leave empty for
	// production; tests pass a URL such as http://127.0.0.1:1/auth/keys to
	// exercise the cold-start non-blocking guarantee.
	JWKsURL string
}

// JWKSource is the minimal interface the verifier needs from a JWKs cache.
// Production: keyfunc.Keyfunc (returned by keyfunc.NewDefaultCtx) satisfies
// this via its `Keyfunc(token *jwt.Token) (interface{}, error)` method.
// Tests: a stub returning a static RSA public key.
type JWKSource interface {
	Keyfunc(token *jwt.Token) (interface{}, error)
}

// Verifier validates Apple identityTokens against a JWKs source.
type Verifier struct {
	kf   JWKSource
	opts Options
}

// New constructs a Verifier wired to Apple's live JWKs endpoint.
// Per RESEARCH.md, keyfunc.NewDefaultCtx is **non-blocking** on the initial
// fetch — it launches a refresh goroutine that fills the cache lazily, so
// New() returns promptly even if Apple's JWKs endpoint is unreachable at
// boot time (TestVerify_JWKsColdStart locks this guarantee). Use
// context.Background() to tie the goroutine to process lifetime; a future
// shutdown context can replace it via the JWKsURL override without changing
// this signature.
func New(opts Options) (*Verifier, error) {
	if opts.AllowedIssuer == "" {
		return nil, errors.New("apple: AllowedIssuer is required")
	}
	if len(opts.AllowedAudiences) == 0 {
		return nil, errors.New("apple: AllowedAudiences is required")
	}
	url := opts.JWKsURL
	if url == "" {
		url = defaultAppleJWKsURL
	}
	kf, err := keyfunc.NewDefaultCtx(context.Background(), []string{url})
	if err != nil {
		return nil, fmt.Errorf("apple: keyfunc init: %w", err)
	}
	return &Verifier{kf: kf, opts: opts}, nil
}

// Verify validates the identityToken's signature against Apple's JWKs,
// checks iss/aud/exp, and returns the extracted AppleIdentity.
//
// Errors are intentionally generic ("apple: audience mismatch" etc.) so
// the handler can map them to 401 without leaking parser internals to the
// client (HOTFIX-04 contract).
func (v *Verifier) Verify(_ context.Context, identityToken string) (AppleIdentity, error) {
	claims := jwt.MapClaims{}
	_, err := jwt.ParseWithClaims(
		identityToken,
		claims,
		v.kf.Keyfunc,
		jwt.WithIssuer(v.opts.AllowedIssuer),
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithLeeway(clockSkewLeeway),
	)
	if err != nil {
		return AppleIdentity{}, err
	}

	// Apple's `aud` is a single string per spec; jwt.WithAudience would also
	// work, but the explicit whitelist check produces a clearer error message.
	aud, _ := claims["aud"].(string)
	if !contains(v.opts.AllowedAudiences, aud) {
		return AppleIdentity{}, errors.New("apple: audience mismatch")
	}

	sub, _ := claims["sub"].(string)
	email, _ := claims["email"].(string)

	// RESEARCH.md A1: Apple sends these as STRING "true"/"false".
	emailVerifiedRaw, _ := claims["email_verified"].(string)
	isPrivateRaw, _ := claims["is_private_email"].(string)

	return AppleIdentity{
		Sub:            sub,
		Email:          email,
		EmailVerified:  emailVerifiedRaw == "true",
		IsPrivateRelay: isPrivateRaw == "true",
	}, nil
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
