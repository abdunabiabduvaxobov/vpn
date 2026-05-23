// Package lava is a pure HTTP client for gate.lava.top. No Fiber, no GORM,
// no globals beyond the BaseURL constant. The package is constructed once
// at startup in cmd/main.go and dependency-injected into payment handlers.
//
// SSRF mitigation per CONTEXT.md D-15 / PAY-16: BaseURL is a const string
// literal. The verification grep in plan 03-11 will fail if anything else
// appears. The HTTP client refuses redirects (defence against open-redirect
// abuse) and uses a hardcoded 5-second timeout (D-14).
package lava

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// BaseURL is the lava.top API root. Hardcoded per CONTEXT.md D-15.
// SANDBOX uses the SAME URL — only the API key differs (RESEARCH §11.1).
const BaseURL = "https://gate.lava.top"

// Client is an HTTP client for the lava.top public API.
type Client struct {
	apiKey  string
	http    *http.Client
	baseURL string // package-private; test-only override via newWithBaseURL
}

// New constructs a production Client bound to BaseURL.
//
//   - apiKey: X-Api-Key header value. Resolved by config.Load() based on
//     LAVA_ENV (LAVA_API_KEY or LAVA_API_KEY_SANDBOX).
//   - http.Client uses a 5-second timeout (CONTEXT.md D-14).
//   - CheckRedirect returns ErrUseLastResponse so the client does NOT follow
//     redirects — defence against open-redirect abuse on lava-side bugs.
func New(apiKey string) *Client {
	return newWithBaseURL(apiKey, BaseURL)
}

// newWithBaseURL is package-private — used by *_test.go to point at an
// httptest server. Production code paths go through New(), which always
// passes BaseURL. The SSRF audit grep in plan 03-11 ensures no caller
// outside the lava package calls newWithBaseURL directly.
func newWithBaseURL(apiKey, baseURL string) *Client {
	return &Client{
		apiKey:  apiKey,
		baseURL: baseURL,
		http: &http.Client{
			Timeout: 5 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// NewForTest constructs a Client pointing at an arbitrary base URL — for httptest-mocking ONLY.
// External-package tests (e.g. handler/payment_test.go in plan 03-05, handler/webhook_lava_test.go
// in plan 03-06) call this to point the lava client at httptest.NewServer().URL. Production code
// MUST use New(), which always passes the hardcoded BaseURL. The SSRF audit grep in plan 03-11
// confirms no production call site invokes NewForTest.
func NewForTest(apiKey, baseURL string) *Client {
	return newWithBaseURL(apiKey, baseURL)
}

// do is the shared request helper. Stamps X-Api-Key + Content-Type + Accept;
// returns the raw *http.Response or a wrapped error. Caller closes resp.Body.
func (c *Client) do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	fullURL := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, fullURL, body)
	if err != nil {
		return nil, fmt.Errorf("lava: build request: %w", err)
	}
	req.Header.Set("X-Api-Key", c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	return c.http.Do(req)
}

// decodeJSON decodes resp.Body into out. Closes the body. Returns ErrLavaAPI
// when the status code is >= 400 (includes lava's `message` if parseable).
func decodeJSON(resp *http.Response, out interface{}) error {
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		var errBody struct {
			Message string `json:"message"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&errBody)
		return fmt.Errorf("lava api: status=%d message=%q", resp.StatusCode, errBody.Message)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// encodeJSON marshals v and returns it as an io.Reader for use with do().
func encodeJSON(v interface{}) (io.Reader, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("lava: marshal request: %w", err)
	}
	return bytes.NewReader(b), nil
}

// escapeQuery is a tiny wrapper so call sites read cleanly.
func escapeQuery(v string) string { return url.QueryEscape(v) }

// pathEscape is the segment-safe escaper for {id} in URL paths.
func pathEscape(v string) string { return url.PathEscape(v) }
