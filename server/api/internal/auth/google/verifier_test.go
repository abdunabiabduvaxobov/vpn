package google

import (
	"context"
	"errors"
	"strings"
	"testing"

	"google.golang.org/api/idtoken"
)

const (
	audIOS     = "ios.googleusercontent.com"
	audAndroid = "android.googleusercontent.com"
	audWeb     = "web.googleusercontent.com"
)

type validateResult struct {
	payload *idtoken.Payload
	err     error
}

// fakeValidator implements idtokenValidator for tests.
type fakeValidator struct {
	byAudience map[string]validateResult
	fallback   validateResult
}

func (f *fakeValidator) Validate(_ context.Context, _ string, audience string) (*idtoken.Payload, error) {
	if r, ok := f.byAudience[audience]; ok {
		return r.payload, r.err
	}
	return f.fallback.payload, f.fallback.err
}

func newTestVerifier(v idtokenValidator) *Verifier {
	return &Verifier{
		validator:        v,
		AllowedAudiences: []string{audIOS, audAndroid, audWeb},
	}
}

func okPayload(sub, email string, verified bool, hd string) *idtoken.Payload {
	return &idtoken.Payload{
		Subject: sub,
		Claims: map[string]interface{}{
			"email":          email,
			"email_verified": verified,
			"hd":             hd,
		},
	}
}

func TestVerify_HappyPath(t *testing.T) {
	fv := &fakeValidator{
		byAudience: map[string]validateResult{
			audIOS: {payload: okPayload("googleSub123", "u@example.com", true, "")},
		},
		fallback: validateResult{err: errors.New("not this aud")},
	}
	v := newTestVerifier(fv)
	id, err := v.Verify(context.Background(), "fake-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id.Sub != "googleSub123" || id.Email != "u@example.com" || !id.EmailVerified {
		t.Errorf("unexpected identity: %+v", id)
	}
}

func TestVerify_HappyPath_ThirdAudience(t *testing.T) {
	fv := &fakeValidator{
		byAudience: map[string]validateResult{
			audIOS:     {err: errors.New("aud mismatch")},
			audAndroid: {err: errors.New("aud mismatch")},
			audWeb:     {payload: okPayload("googleSub789", "x@example.com", true, "")},
		},
	}
	v := newTestVerifier(fv)
	id, err := v.Verify(context.Background(), "fake-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id.Sub != "googleSub789" {
		t.Errorf("Sub: want googleSub789, got %q", id.Sub)
	}
}

func TestVerify_AllAudiencesFail(t *testing.T) {
	fv := &fakeValidator{
		fallback: validateResult{err: errors.New("aud mismatch")},
	}
	v := newTestVerifier(fv)
	_, err := v.Verify(context.Background(), "fake-token")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestVerify_EmailNotVerified(t *testing.T) {
	fv := &fakeValidator{
		byAudience: map[string]validateResult{
			audIOS: {payload: okPayload("googleSubXYZ", "u@example.com", false, "")},
		},
	}
	v := newTestVerifier(fv)
	_, err := v.Verify(context.Background(), "fake-token")
	if err == nil {
		t.Fatal("expected error for email_verified=false, got nil")
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "email") || !strings.Contains(msg, "verif") {
		t.Errorf("error: want substring 'email' and 'verif', got %q", err.Error())
	}
}

func TestVerify_HostedDomainExtracted(t *testing.T) {
	fv := &fakeValidator{
		byAudience: map[string]validateResult{
			audIOS: {payload: okPayload("googleSubHD", "boss@risevpn.com", true, "risevpn.com")},
		},
	}
	v := newTestVerifier(fv)
	id, err := v.Verify(context.Background(), "fake-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id.HostedDomain != "risevpn.com" {
		t.Errorf("HostedDomain: want risevpn.com, got %q", id.HostedDomain)
	}
}
