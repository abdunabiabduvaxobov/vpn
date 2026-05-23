package lava

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestClient_HardcodedBaseURL_5sTimeout_NoRedirect verifies PAY-16 invariants:
//   - BaseURL is exactly the literal "https://gate.lava.top"
//   - HTTP client timeout is 5 seconds
//   - Redirects are NOT followed (CheckRedirect returns ErrUseLastResponse)
//   - No InsecureSkipVerify
func TestClient_HardcodedBaseURL_5sTimeout_NoRedirect(t *testing.T) {
	if BaseURL != "https://gate.lava.top" {
		t.Fatalf("PAY-16: BaseURL must be exactly %q, got %q", "https://gate.lava.top", BaseURL)
	}
	c := New("test-key")
	if c.http.Timeout != 5*time.Second {
		t.Errorf("PAY-16: timeout must be 5s, got %v", c.http.Timeout)
	}
	if c.http.CheckRedirect == nil {
		t.Errorf("PAY-16: CheckRedirect must be set (refuse redirects)")
	} else {
		// Synthesize a redirect to verify the function returns ErrUseLastResponse.
		req, _ := http.NewRequest("GET", "https://example.com/", nil)
		err := c.http.CheckRedirect(req, nil)
		if err != http.ErrUseLastResponse {
			t.Errorf("PAY-16: CheckRedirect must return ErrUseLastResponse, got %v", err)
		}
	}
	// Default transport — InsecureSkipVerify must NOT be enabled.
	// The default transport is *http.Transport with nil TLSConfig => verification on.
	if c.http.Transport != nil {
		// If a custom transport is set in the future, this test will need updating;
		// for now we expect nil (use default).
		t.Errorf("PAY-16: expected nil Transport (default => TLS verification on), got %T", c.http.Transport)
	}
}

// TestNewWithBaseURL_OverridesForTests verifies the test helper points at a custom URL.
func TestNewWithBaseURL_OverridesForTests(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := newWithBaseURL("k", srv.URL)
	if c.baseURL != srv.URL {
		t.Fatalf("expected baseURL=%s, got %s", srv.URL, c.baseURL)
	}
	// Ensure the request actually hits the test server.
	resp, err := c.do(context.Background(), "GET", "/anything", nil)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}
