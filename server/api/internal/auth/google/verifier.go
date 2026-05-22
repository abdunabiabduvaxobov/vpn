// Package google verifies Sign in with Google ID tokens.
//
// Pure-library package per D-13: no DB access, no Fiber types, no globals.
// The handler in internal/handler/auth.go composes this with repository
// lookups; this package only validates a JWT against Google's JWKs +
// caller-provided audience whitelist + email_verified gate.
//
// Unlike Apple, Google's email_verified claim is a native bool (RESEARCH.md
// §Google Verifier). Per D-17 we reject any token whose email_verified=false
// — this prevents unverified addresses from entering the auto-link search
// space in handler-level logic (T-2-EmailSpoof).
package google

import (
	"context"
	"errors"
	"fmt"

	"google.golang.org/api/idtoken"
)

// GoogleIdentity is the verified-identity output of Verify.
type GoogleIdentity struct {
	Sub           string
	Email         string
	EmailVerified bool
	HostedDomain  string
}

// idtokenValidator is the minimal interface the verifier needs from the
// google.golang.org/api/idtoken library. Lets tests inject a fake
// (RESEARCH.md §Testing Strategy option (b)).
type idtokenValidator interface {
	Validate(ctx context.Context, idToken, audience string) (*idtoken.Payload, error)
}

// defaultValidator wraps the real idtoken.Validate function so it satisfies
// the idtokenValidator interface. Production code uses this; tests substitute
// a fake.
type defaultValidator struct{}

func (defaultValidator) Validate(ctx context.Context, idToken, audience string) (*idtoken.Payload, error) {
	return idtoken.Validate(ctx, idToken, audience)
}

// Verifier validates Google ID tokens by iterating allowed audiences.
type Verifier struct {
	validator        idtokenValidator
	AllowedAudiences []string
}

// New constructs the production Verifier wired to idtoken.Validate.
// Per D-34, audience whitelist is constructed ONCE at startup from
// cfg.GoogleClientIDIOS, cfg.GoogleClientIDAndroid, cfg.GoogleClientIDWeb.
func New(audiences []string) *Verifier {
	return &Verifier{
		validator:        defaultValidator{},
		AllowedAudiences: audiences,
	}
}

// Verify iterates through AllowedAudiences calling validator.Validate; accepts
// the first success. Rejects on email_verified=false per D-17. If all audiences
// fail, returns the last error.
func (v *Verifier) Verify(ctx context.Context, idToken string) (GoogleIdentity, error) {
	if len(v.AllowedAudiences) == 0 {
		return GoogleIdentity{}, errors.New("google: no allowed audiences configured")
	}
	var lastErr error
	for _, aud := range v.AllowedAudiences {
		payload, err := v.validator.Validate(ctx, idToken, aud)
		if err != nil {
			lastErr = err
			continue
		}
		email, _ := payload.Claims["email"].(string)
		verified, _ := payload.Claims["email_verified"].(bool)
		hd, _ := payload.Claims["hd"].(string)
		if !verified {
			// D-17: reject any Google identity with unverified email.
			return GoogleIdentity{}, errors.New("google: email not verified")
		}
		return GoogleIdentity{
			Sub:           payload.Subject,
			Email:         email,
			EmailVerified: true,
			HostedDomain:  hd,
		}, nil
	}
	if lastErr == nil {
		return GoogleIdentity{}, errors.New("google: token validation failed (no audience matched)")
	}
	return GoogleIdentity{}, fmt.Errorf("google: %w", lastErr)
}
