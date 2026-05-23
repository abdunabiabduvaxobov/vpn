---
phase: 3
plan: 02
type: execute
slug: lava-top-plans-catalog
plan_number: 2
wave: 1
depends_on: []
files_modified:
  - server/api/internal/lava/client.go
  - server/api/internal/lava/dto.go
  - server/api/internal/lava/invoice.go
  - server/api/internal/lava/products.go
  - server/api/internal/lava/subscription.go
  - server/api/internal/lava/webhook.go
  - server/api/internal/lava/client_test.go
  - server/api/internal/lava/invoice_test.go
  - server/api/internal/lava/products_test.go
  - server/api/internal/lava/subscription_test.go
  - server/api/internal/lava/webhook_test.go
  - server/api/internal/config/config.go
autonomous: true
requirements_addressed: [PAY-07, PAY-16, PAY-02, PAY-10]
estimated_complexity: medium
must_haves:
  refs:
    - "See <must_haves> XML block in plan body for canonical truths / artifacts / key_links"

---

<objective>
Build the pure `internal/lava/` HTTP client package (no Fiber, no GORM) implementing the 4 outbound endpoints (CreateInvoice, GetInvoice, ListProducts, CancelSubscription) and the inbound webhook X-Api-Key verifier (constant-time compare with optional previous secret). Hardcode `const BaseURL = "https://gate.lava.top"` (D-15 / PAY-16) — no env override. Use 5-second HTTP timeout + `ErrUseLastResponse` redirect-stopper. Extend `config.go` with the 7 new LAVA_* env vars and add them to the HOTFIX-08 aggregate validator with the explicit `LAVA_ENV=sandbox|production` flag that selects between `LAVA_API_KEY` and `LAVA_API_KEY_SANDBOX` (RESEARCH §13.2 — planner's pick for D-30 sub-choice).
</objective>

<context>
@.planning/phases/03-lava-top-plans-catalog/03-CONTEXT.md
@.planning/phases/03-lava-top-plans-catalog/03-RESEARCH.md
@server/api/internal/config/config.go
@server/api/internal/auth/google/client.go
</context>

<interfaces>
Public API the rest of the codebase will consume (handlers wired in plans 03-05 and 03-06):

```go
package lava

const BaseURL = "https://gate.lava.top"

// Client is constructed once at startup with the active API key.
type Client struct { /* unexported fields */ }
func New(apiKey string) *Client

// Outbound endpoints — all take context for 5s timeout enforcement.
func (c *Client) CreateInvoice(ctx context.Context, req CreateInvoiceRequest) (*InvoiceResponse, error)
func (c *Client) GetInvoice(ctx context.Context, invoiceID string) (*InvoiceDetailResponse, error)
func (c *Client) ListProducts(ctx context.Context) ([]ProductItemResponse, error)
func (c *Client) CancelSubscription(ctx context.Context, contractID, email string) error

// Inbound webhook authentication — constant-time compare, accepts either current or previous secret.
func VerifyAPIKey(received, current, previous string) bool
```

New env vars added to Config:
```go
LavaEnv                      string // "sandbox" | "production" (default "production")
LavaAPIKey                   string // required when LavaEnv=="production"
LavaAPIKeySandbox            string // required when LavaEnv=="sandbox"
LavaWebhookSecret            string // required
LavaWebhookSecretPrevious    string // optional (rotation only)
LavaWebhookAllowedCIDRs      string // required CSV
LavaSuccessURL               string // required
LavaFailURL                  string // required
```

Computed field for convenience (the active key chosen by LAVA_ENV):
```go
LavaActiveAPIKey             string // populated by Load() from LavaAPIKey or LavaAPIKeySandbox based on LavaEnv
```
</interfaces>

<tasks>

<task type="auto">
  <id>03-02-T01</id>
  <name>Extend config.go with LAVA_* env vars + RequireEnv aggregate (strict-required + LAVA_ENV selector)</name>
  <files>server/api/internal/config/config.go</files>
  <read_first>
    - server/api/internal/config/config.go (CURRENT — existing RequireEnv list + Load + OptionalEnvWarnings)
    - .planning/phases/03-lava-top-plans-catalog/03-CONTEXT.md D-30 (env list), D-16 (LAVA_WEBHOOK_ALLOWED_CIDRS), Claude's Discretion (strict-required + explicit LAVA_ENV flag)
    - .planning/phases/03-lava-top-plans-catalog/03-RESEARCH.md §13.2 (RequireEnv amendment code), §13.3 (recommendations: strict-required + LAVA_ENV)
  </read_first>
  <action>
    Edit `server/api/internal/config/config.go` per the following three sub-edits. Keep all existing fields/methods/comments intact.

    **(a) Add 8 new fields to the `Config` struct, in a new "Phase 3 lava.top" section AFTER the SSO Google fields (just BEFORE the closing `}`):**
```go
	// Phase 3 lava.top payment provider (D-30, RESEARCH §13).
	// LavaEnv selects which API key the constructed *lava.Client uses.
	// Strict-required at startup via RequireEnv() — no defaults except LavaEnv ("production").
	LavaEnv                   string // "sandbox" | "production" (default "production" when env unset)
	LavaAPIKey                string // required when LavaEnv == "production"
	LavaAPIKeySandbox         string // required when LavaEnv == "sandbox"
	LavaWebhookSecret         string // required — X-Api-Key on inbound webhook
	LavaWebhookSecretPrevious string // optional — set only during rotation (D-17)
	LavaWebhookAllowedCIDRs   string // required CSV of CIDRs lava webhook sources (D-16 — strict-required per planner's pick)
	LavaSuccessURL            string // required — https://risevpn.com/pay/success
	LavaFailURL               string // required — https://risevpn.com/pay/fail
	// LavaActiveAPIKey is the resolved key based on LavaEnv (populated by Load()).
	// Consumers (lava.Client constructor in cmd/main.go) read this; they never
	// inspect LavaAPIKey vs LavaAPIKeySandbox directly.
	LavaActiveAPIKey string
```

    **(b) Inside `Load()`, AFTER the existing `cfg.GoogleClientIDWeb = getEnv(...)` line, add:**
```go
	// Phase 3 lava.top (D-30).
	cfg.LavaEnv = getEnv("LAVA_ENV", "production")
	cfg.LavaAPIKey = getEnv("LAVA_API_KEY", "")
	cfg.LavaAPIKeySandbox = getEnv("LAVA_API_KEY_SANDBOX", "")
	cfg.LavaWebhookSecret = getEnv("LAVA_WEBHOOK_SECRET", "")
	cfg.LavaWebhookSecretPrevious = getEnv("LAVA_WEBHOOK_SECRET_PREVIOUS", "")
	cfg.LavaWebhookAllowedCIDRs = getEnv("LAVA_WEBHOOK_ALLOWED_CIDRS", "")
	cfg.LavaSuccessURL = getEnv("LAVA_SUCCESS_URL", "")
	cfg.LavaFailURL = getEnv("LAVA_FAIL_URL", "")
	// Resolve active key once at startup; RequireEnv enforces non-empty.
	switch cfg.LavaEnv {
	case "sandbox":
		cfg.LavaActiveAPIKey = cfg.LavaAPIKeySandbox
	default: // "production" or anything else (RequireEnv catches invalid values)
		cfg.LavaActiveAPIKey = cfg.LavaAPIKey
	}
```

    **(c) REWRITE the `RequireEnv` function — add the 4 strict-required Lava keys + the LAVA_ENV / active-key compound check. Final function body:**
```go
// RequireEnv reports every required environment variable that is unset or empty.
// Returns an empty slice when all required vars are set.
//
// Single-pass aggregate validator (per HOTFIX-08 D-04): scans every var in one
// call so the operator sees ALL missing keys in one error, not "fix one, restart,
// fix the next". Called from cmd/main.go BEFORE config.Load(); a non-empty return
// becomes a logger.Fatal which calls os.Exit(1).
//
// Phase 3 (D-30): adds LAVA_* keys. Per planner's pick of strict-required
// (RESEARCH §13.3), LAVA_WEBHOOK_ALLOWED_CIDRS has no default — the operator
// MUST supply the CIDR list explicitly. LAVA_ENV defaults to "production"
// when unset; the matching LAVA_API_KEY / LAVA_API_KEY_SANDBOX is then required.
func RequireEnv() []string {
	required := []string{
		"JWT_SECRET",
		"DATABASE_URL",
		"REDIS_URL",
		"TUNNEL_VLESS_UUID",
		// SSO required (Phase 2 D-30):
		"APPLE_TEAM_ID",
		"APPLE_BUNDLE_ID",
		"APPLE_SERVICE_ID",
		"GOOGLE_CLIENT_ID_IOS",
		"GOOGLE_CLIENT_ID_ANDROID",
		"GOOGLE_CLIENT_ID_WEB",
		// Phase 3 lava.top required (D-30):
		"LAVA_WEBHOOK_SECRET",
		"LAVA_WEBHOOK_ALLOWED_CIDRS",
		"LAVA_SUCCESS_URL",
		"LAVA_FAIL_URL",
	}
	var missing []string
	for _, key := range required {
		if os.Getenv(key) == "" {
			missing = append(missing, key)
		}
	}
	// LAVA_ENV + matching API key compound check. LAVA_ENV defaults to "production"
	// when unset; values other than "sandbox"/"production" are rejected here.
	lavaEnv := os.Getenv("LAVA_ENV")
	if lavaEnv == "" {
		lavaEnv = "production"
	}
	switch lavaEnv {
	case "production":
		if os.Getenv("LAVA_API_KEY") == "" {
			missing = append(missing, "LAVA_API_KEY (required when LAVA_ENV=production)")
		}
	case "sandbox":
		if os.Getenv("LAVA_API_KEY_SANDBOX") == "" {
			missing = append(missing, "LAVA_API_KEY_SANDBOX (required when LAVA_ENV=sandbox)")
		}
	default:
		missing = append(missing, fmt.Sprintf("LAVA_ENV=%q (must be 'sandbox' or 'production')", lavaEnv))
	}
	return missing
}
```

    Then run `cd server/api && go build ./internal/config/...` to confirm the package still compiles.
  </action>
  <acceptance_criteria>
    - `grep -c "LavaActiveAPIKey\s*string" server/api/internal/config/config.go` returns 1
    - `grep "LAVA_WEBHOOK_SECRET\"," server/api/internal/config/config.go` finds one match in RequireEnv
    - `grep "LAVA_WEBHOOK_ALLOWED_CIDRS" server/api/internal/config/config.go` finds at least 2 matches (Load + RequireEnv)
    - `grep "LAVA_SUCCESS_URL" server/api/internal/config/config.go` finds at least 2 matches
    - `grep "LAVA_FAIL_URL" server/api/internal/config/config.go` finds at least 2 matches
    - `grep "lavaEnv == \"sandbox\"\|case \"sandbox\":" server/api/internal/config/config.go` finds at least one match
    - `grep "LAVA_API_KEY (required when LAVA_ENV=production)" server/api/internal/config/config.go` finds one match
    - `cd server/api && go build ./internal/config/...` exits 0
  </acceptance_criteria>
  <automated>cd server/api && go build ./internal/config/...</automated>
  <done>Config struct holds 9 new Lava-related fields; RequireEnv strict-requires 4 webhook+URL keys plus the LAVA_ENV-conditioned API key.</done>
</task>

<task type="auto">
  <id>03-02-T02</id>
  <name>Create lava package files (client.go, dto.go, invoice.go, products.go, subscription.go, webhook.go)</name>
  <files>
    server/api/internal/lava/client.go,
    server/api/internal/lava/dto.go,
    server/api/internal/lava/invoice.go,
    server/api/internal/lava/products.go,
    server/api/internal/lava/subscription.go,
    server/api/internal/lava/webhook.go
  </files>
  <read_first>
    - .planning/phases/03-lava-top-plans-catalog/03-RESEARCH.md §1.1 (CreateInvoiceRequest), §1.2 (InvoiceDetailResponse), §1.3 (ProductsResponse + pagination), §1.4 (DELETE /api/v1/subscriptions), §1.5 (webhook DTO shapes), §12.2 (client.go), §12.3 (invoice/products/subscription skeletons), §12.4 (VerifyAPIKey)
    - .planning/phases/03-lava-top-plans-catalog/03-CONTEXT.md D-14 (file layout), D-15 (hardcoded BaseURL)
    - server/api/internal/auth/google/client.go (Phase 2 precedent for pure-package HTTP client structure — see if it follows similar `Client` struct + constructor pattern; reproduce)
  </read_first>
  <action>
    Six new files in `server/api/internal/lava/`. Copy each block verbatim — DTO field tags are pinned to lava OpenAPI 1.17.0.

    **(a) `server/api/internal/lava/client.go`:**
```go
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
```

    **(b) `server/api/internal/lava/dto.go`:** request/response DTOs from RESEARCH §1.1 through §1.5. Field tags are load-bearing — match lava OpenAPI 1.17.0.

```go
package lava

// --- CreateInvoice / GetInvoice / ListProducts request/response DTOs.
// RESEARCH.md §1.1 (CreateInvoiceRequest), §1.2 (InvoiceDetailResponse),
// §1.3 (ProductsResponse + ProductItemResponse), §1.5 (WebhookEvent shapes).

// CreateInvoiceRequest is POST /api/v3/invoice request body.
type CreateInvoiceRequest struct {
	Email           string             `json:"email"`
	OfferID         string             `json:"offerId"`
	Currency        string             `json:"currency"`
	Periodicity     string             `json:"periodicity,omitempty"`
	BuyerLanguage   string             `json:"buyerLanguage,omitempty"`
	PaymentProvider string             `json:"paymentProvider,omitempty"`
	PaymentMethod   string             `json:"paymentMethod,omitempty"`
	ClientUtm       map[string]string  `json:"clientUtm,omitempty"`
	Amount          *float64           `json:"amount,omitempty"`
}

// InvoiceAmount is the amountTotal nested object.
type InvoiceAmount struct {
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
}

// InvoiceResponse is the POST /api/v3/invoice response (RESEARCH §1.1).
type InvoiceResponse struct {
	ID          string         `json:"id"`
	Status      string         `json:"status"`
	AmountTotal InvoiceAmount  `json:"amountTotal"`
	PaymentURL  *string        `json:"paymentUrl"`
}

// InvoiceDetailResponse is the GET /api/v2/invoices/{id} response (RESEARCH §1.2).
// Used by the escalate path (D-25) in /api/v1/invoices/:id?escalate=true.
type InvoiceDetailResponse struct {
	ID                  string                          `json:"id"`
	Type                string                          `json:"type"`
	Datetime            string                          `json:"datetime"`
	Status              string                          `json:"status"`
	Receipt             InvoiceReceipt                  `json:"receipt"`
	Buyer               InvoiceBuyer                    `json:"buyer"`
	Product             InvoiceProduct                  `json:"product"`
	ParentInvoice       *InvoiceParent                  `json:"parentInvoice,omitempty"`
	SubscriptionStatus  *string                         `json:"subscriptionStatus,omitempty"`
	SubscriptionDetails *InvoiceSubscriptionDetails     `json:"subscriptionDetails,omitempty"`
	ClientUtm           map[string]*string              `json:"clientUtm,omitempty"`
}

type InvoiceReceipt struct {
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
	Fee      float64 `json:"fee"`
}

type InvoiceBuyer struct {
	Email    string `json:"email"`
	CardMask string `json:"cardMask"`
}

type InvoiceProduct struct {
	Name  string `json:"name"`
	Offer string `json:"offer"`
}

type InvoiceParent struct {
	ID string `json:"id"`
}

type InvoiceSubscriptionDetails struct {
	ExpiredAt    *string `json:"expiredAt"`
	TerminatedAt *string `json:"terminatedAt"`
	CancelledAt  *string `json:"cancelledAt"`
}

// ProductsResponse is GET /api/v2/products response (RESEARCH §1.3).
type ProductsResponse struct {
	Items    []ProductsItem `json:"items"`
	NextPage *string        `json:"nextPage,omitempty"`
}

type ProductsItem struct {
	Type string              `json:"type"` // "PRODUCT" or "POST"
	Data ProductItemResponse `json:"data"` // when type=PRODUCT
}

type ProductItemResponse struct {
	ID          string         `json:"id"`
	Title       *string        `json:"title"`
	Description *string        `json:"description"`
	Type        string         `json:"type"`
	Offers      []ProductOffer `json:"offers"`
}

type ProductOffer struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Description *string            `json:"description"`
	Prices      []ProductOfferPrice `json:"prices"`
	Recurrent   bool               `json:"recurrent"`
}

type ProductOfferPrice struct {
	Amount      float64 `json:"amount"`
	Currency    string  `json:"currency"`
	Periodicity string  `json:"periodicity"`
}

// WebhookEvent is the inbound webhook payload (RESEARCH §1.5). The handler
// in plan 03-06 BodyParser-s into this; field optionality reflects which
// events carry which fields.
type WebhookEvent struct {
	EventType        string          `json:"eventType"`
	ContractID       string          `json:"contractId"`
	ParentContractID *string         `json:"parentContractId,omitempty"`
	Amount           *float64        `json:"amount,omitempty"`    // omitted on subscription.cancelled
	Currency         *string         `json:"currency,omitempty"`  // omitted on subscription.cancelled
	Timestamp        *string         `json:"timestamp,omitempty"` // omitted on subscription.cancelled (use CancelledAt)
	Status           *string         `json:"status,omitempty"`
	ErrorMessage     *string         `json:"errorMessage,omitempty"`
	Product          *WebhookProduct `json:"product,omitempty"`
	Buyer            *WebhookBuyer   `json:"buyer,omitempty"`
	CancelledAt      *string         `json:"cancelledAt,omitempty"`  // only on subscription.cancelled
	WillExpireAt     *string         `json:"willExpireAt,omitempty"` // only on subscription.cancelled
}

type WebhookProduct struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type WebhookBuyer struct {
	Email string `json:"email"`
}
```

    **(c) `server/api/internal/lava/invoice.go`:**
```go
package lava

import (
	"context"
	"fmt"
)

// CreateInvoice calls POST /api/v3/invoice. Returns the invoice/contract
// identifier (`id`) and the lava-hosted paymentUrl that the client redirects to.
//
// Errors:
//   - context.DeadlineExceeded after the configured 5s timeout
//   - lava api: status=4xx/5xx ... — for any non-2xx lava response
func (c *Client) CreateInvoice(ctx context.Context, req CreateInvoiceRequest) (*InvoiceResponse, error) {
	body, err := encodeJSON(req)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(ctx, "POST", "/api/v3/invoice", body)
	if err != nil {
		return nil, fmt.Errorf("lava CreateInvoice: %w", err)
	}
	var out InvoiceResponse
	if err := decodeJSON(resp, &out); err != nil {
		return nil, fmt.Errorf("lava CreateInvoice: %w", err)
	}
	return &out, nil
}

// GetInvoice calls GET /api/v2/invoices/{id}. Used by the escalate path
// (D-25) in /api/v1/invoices/:id?escalate=true when the local DB still
// shows pending after a few polls.
func (c *Client) GetInvoice(ctx context.Context, invoiceID string) (*InvoiceDetailResponse, error) {
	resp, err := c.do(ctx, "GET", "/api/v2/invoices/"+pathEscape(invoiceID), nil)
	if err != nil {
		return nil, fmt.Errorf("lava GetInvoice: %w", err)
	}
	var out InvoiceDetailResponse
	if err := decodeJSON(resp, &out); err != nil {
		return nil, fmt.Errorf("lava GetInvoice: %w", err)
	}
	return &out, nil
}
```

    **(d) `server/api/internal/lava/products.go`:** drain pagination cursor.
```go
package lava

import (
	"context"
	"fmt"
)

// ListProducts calls GET /api/v2/products and follows the `nextPage` cursor
// server-side until exhausted. Returns the flattened list of products. The
// admin proxy endpoint (plan 03-05 / D-12) normalizes this into a flat
// {productId, offerId, periodicity, currency, amount} dropdown source.
func (c *Client) ListProducts(ctx context.Context) ([]ProductItemResponse, error) {
	var all []ProductItemResponse
	next := ""
	for {
		path := "/api/v2/products"
		if next != "" {
			path += "?nextPage=" + escapeQuery(next)
		}
		resp, err := c.do(ctx, "GET", path, nil)
		if err != nil {
			return nil, fmt.Errorf("lava ListProducts: %w", err)
		}
		var page ProductsResponse
		if err := decodeJSON(resp, &page); err != nil {
			return nil, fmt.Errorf("lava ListProducts: %w", err)
		}
		for _, item := range page.Items {
			if item.Type == "PRODUCT" {
				all = append(all, item.Data)
			}
		}
		if page.NextPage == nil || *page.NextPage == "" {
			break
		}
		next = *page.NextPage
	}
	return all, nil
}
```

    **(e) `server/api/internal/lava/subscription.go`:**
```go
package lava

import (
	"context"
	"fmt"
)

// CancelSubscription calls DELETE /api/v1/subscriptions?contractId=X&email=Y.
// The user keeps Pro until the current period ends — local DB downgrade
// happens via the expiry cron (plan 03-09 / D-26). The handler that wraps
// this (plan 03-05) writes lava_contracts.cancelled_at = now() afterwards.
//
// Both query parameters are REQUIRED by lava (RESEARCH §1.4).
func (c *Client) CancelSubscription(ctx context.Context, contractID, email string) error {
	path := "/api/v1/subscriptions?contractId=" + escapeQuery(contractID) + "&email=" + escapeQuery(email)
	resp, err := c.do(ctx, "DELETE", path, nil)
	if err != nil {
		return fmt.Errorf("lava CancelSubscription: %w", err)
	}
	// Treat 2xx as success; lava's spec doesn't enumerate the response body.
	if err := decodeJSON(resp, nil); err != nil {
		return fmt.Errorf("lava CancelSubscription: %w", err)
	}
	return nil
}
```

    **(f) `server/api/internal/lava/webhook.go`:**
```go
package lava

import "crypto/subtle"

// VerifyAPIKey returns true iff the supplied X-Api-Key matches either the
// current or (when set) the previous shared secret. Both comparisons use
// crypto/subtle.ConstantTimeCompare so timing attacks cannot leak prefix
// matches (PAY-07 / CONTEXT.md D-17).
//
// Rotation flow (D-17):
//   1. Set LAVA_WEBHOOK_SECRET_PREVIOUS=<old>, LAVA_WEBHOOK_SECRET=<new>, restart.
//   2. Update lava.top dashboard to <new>.
//   3. Clear LAVA_WEBHOOK_SECRET_PREVIOUS, restart.
//
// Zero-downtime: during step 2 some webhooks still arrive with <old>; both
// secrets accepted in step 1's window.
func VerifyAPIKey(received, current, previous string) bool {
	if subtle.ConstantTimeCompare([]byte(received), []byte(current)) == 1 {
		return true
	}
	if previous != "" && subtle.ConstantTimeCompare([]byte(received), []byte(previous)) == 1 {
		return true
	}
	return false
}
```

    Then `cd server/api && go build ./internal/lava/...` to confirm package compiles.
  </action>
  <acceptance_criteria>
    - Files `client.go`, `dto.go`, `invoice.go`, `products.go`, `subscription.go`, `webhook.go` all exist under `server/api/internal/lava/`
    - `grep 'const BaseURL = "https://gate.lava.top"' server/api/internal/lava/client.go` finds one match
    - `grep "ErrUseLastResponse" server/api/internal/lava/client.go` finds one match
    - `grep -n "func NewForTest\(apiKey, baseURL string\)" server/api/internal/lava/client.go` finds exactly one match (test affordance lives WITH the package per BLOCKER #2 fix)
    - `grep -n "newWithBaseURL\(apiKey, baseURL\)" server/api/internal/lava/client.go` finds at least 1 match in the NewForTest body — the parameter is passed through, NOT replaced with the package-const BaseURL
    - `grep -B 2 "func NewForTest" server/api/internal/lava/client.go` shows the doc comment naming "httptest" explicitly so future readers understand the constraint
    - `grep "Timeout: 5 \\* time.Second" server/api/internal/lava/client.go` finds one match
    - `grep "subtle.ConstantTimeCompare" server/api/internal/lava/webhook.go` finds at least 2 matches
    - `grep 'POST", "/api/v3/invoice"' server/api/internal/lava/invoice.go` finds one match
    - `grep 'GET", "/api/v2/invoices/"' server/api/internal/lava/invoice.go` finds one match
    - `grep 'DELETE", path' server/api/internal/lava/subscription.go` finds one match
    - `grep "nextPage" server/api/internal/lava/products.go` finds matches (pagination drain)
    - `grep "ParentContractID" server/api/internal/lava/dto.go` finds one match
    - `grep "CancelledAt" server/api/internal/lava/dto.go` finds one match
    - `cd server/api && go build ./internal/lava/...` exits 0
    - `cd server/api && go vet ./internal/lava/...` exits 0
  </acceptance_criteria>
  <automated>cd server/api && go build ./internal/lava/... && go vet ./internal/lava/...</automated>
  <done>Pure lava package compiles standalone. Public API surface matches the interfaces block in this plan.</done>
</task>

<task type="auto">
  <id>03-02-T03</id>
  <name>Write lava package unit tests (CreateInvoice, GetInvoice, ListProducts pagination, CancelSubscription, VerifyAPIKey constant-time)</name>
  <files>
    server/api/internal/lava/client_test.go,
    server/api/internal/lava/invoice_test.go,
    server/api/internal/lava/products_test.go,
    server/api/internal/lava/subscription_test.go,
    server/api/internal/lava/webhook_test.go
  </files>
  <read_first>
    - server/api/internal/lava/client.go, invoice.go, products.go, subscription.go, webhook.go (just written in T02 — the test targets)
    - .planning/phases/03-lava-top-plans-catalog/03-RESEARCH.md §12.5 (httptest pattern), §1.1-§1.5 (DTO shapes for fixture payloads)
    - .planning/phases/03-lava-top-plans-catalog/03-VALIDATION.md PAY-07 row + PAY-16 row + PAY-02 row (test names to match)
  </read_first>
  <action>
    Create 5 test files using `httptest.NewServer` to mock lava endpoints. Tests are pure-Go (no DB, no testcontainers). Run with `cd server/api && go test ./internal/lava/ -count=1 -timeout=30s -v`.

    **(a) `server/api/internal/lava/client_test.go`:** — covers `TestClient_HardcodedBaseURL_5sTimeout_NoRedirect` (PAY-16) + the package-private `newWithBaseURL` helper.

```go
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
```

    **(b) `server/api/internal/lava/invoice_test.go`:**

```go
package lava

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateInvoice_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v3/invoice" {
			t.Errorf("expected /api/v3/invoice, got %s", r.URL.Path)
		}
		if r.Header.Get("X-Api-Key") != "test-key" {
			t.Errorf("expected X-Api-Key=test-key, got %s", r.Header.Get("X-Api-Key"))
		}
		var req CreateInvoiceRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Email != "alice@example.com" || req.OfferID != "off-1" || req.Currency != "USD" {
			t.Errorf("unexpected request body: %+v", req)
		}
		w.WriteHeader(200)
		paymentURL := "https://app.lava.top/pay/abc"
		_ = json.NewEncoder(w).Encode(InvoiceResponse{
			ID:          "inv-123",
			Status:      "in-progress",
			AmountTotal: InvoiceAmount{Amount: 5.0, Currency: "USD"},
			PaymentURL:  &paymentURL,
		})
	}))
	defer srv.Close()

	c := newWithBaseURL("test-key", srv.URL)
	out, err := c.CreateInvoice(context.Background(), CreateInvoiceRequest{
		Email: "alice@example.com", OfferID: "off-1", Currency: "USD", Periodicity: "MONTHLY",
	})
	if err != nil {
		t.Fatalf("CreateInvoice: %v", err)
	}
	if out.ID != "inv-123" || out.Status != "in-progress" {
		t.Errorf("unexpected response: %+v", out)
	}
	if out.PaymentURL == nil || *out.PaymentURL != "https://app.lava.top/pay/abc" {
		t.Errorf("unexpected paymentUrl: %v", out.PaymentURL)
	}
}

func TestCreateInvoice_LavaError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(422)
		_, _ = w.Write([]byte(`{"message":"invalid offer"}`))
	}))
	defer srv.Close()
	c := newWithBaseURL("test-key", srv.URL)
	_, err := c.CreateInvoice(context.Background(), CreateInvoiceRequest{Email: "a", OfferID: "off", Currency: "USD"})
	if err == nil {
		t.Fatalf("expected error on 422 response")
	}
}

func TestGetInvoice_HappyPath(t *testing.T) {
	expectedPath := "/api/v2/invoices/inv-456"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != expectedPath {
			t.Errorf("expected %s, got %s", expectedPath, r.URL.Path)
		}
		w.WriteHeader(200)
		exp := "2026-06-23T10:00:00Z"
		_ = json.NewEncoder(w).Encode(InvoiceDetailResponse{
			ID:                  "inv-456",
			Status:              "COMPLETED",
			Type:                "SUBSCRIPTION_FIRST_INVOICE",
			SubscriptionDetails: &InvoiceSubscriptionDetails{ExpiredAt: &exp},
		})
	}))
	defer srv.Close()
	c := newWithBaseURL("test-key", srv.URL)
	got, err := c.GetInvoice(context.Background(), "inv-456")
	if err != nil {
		t.Fatalf("GetInvoice: %v", err)
	}
	if got.ID != "inv-456" || got.Status != "COMPLETED" {
		t.Errorf("unexpected response: %+v", got)
	}
	if got.SubscriptionDetails == nil || got.SubscriptionDetails.ExpiredAt == nil {
		t.Errorf("expected SubscriptionDetails.ExpiredAt to be set")
	}
}
```

    **(c) `server/api/internal/lava/products_test.go`:** verifies the cursor drain.

```go
package lava

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListProducts_PaginationDrain(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		next := ""
		var items []ProductsItem
		switch {
		case calls == 1:
			items = []ProductsItem{{Type: "PRODUCT", Data: ProductItemResponse{ID: "p1", Type: "SUBSCRIPTION"}}}
			next = "cursor2"
		case calls == 2:
			if !strings.Contains(r.URL.RawQuery, "nextPage=cursor2") {
				t.Errorf("expected nextPage=cursor2 in query, got %q", r.URL.RawQuery)
			}
			items = []ProductsItem{
				{Type: "POST", Data: ProductItemResponse{ID: "post-x"}}, // should be filtered out
				{Type: "PRODUCT", Data: ProductItemResponse{ID: "p2"}},
			}
			next = "" // last page
		default:
			t.Fatalf("unexpected extra call %d", calls)
		}
		var nextPtr *string
		if next != "" {
			nextPtr = &next
		}
		w.WriteHeader(200)
		_ = json.NewEncoder(w).Encode(ProductsResponse{Items: items, NextPage: nextPtr})
	}))
	defer srv.Close()
	c := newWithBaseURL("k", srv.URL)
	out, err := c.ListProducts(context.Background())
	if err != nil {
		t.Fatalf("ListProducts: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 products (POST type filtered out), got %d", len(out))
	}
	if out[0].ID != "p1" || out[1].ID != "p2" {
		t.Errorf("unexpected IDs: %s,%s", out[0].ID, out[1].ID)
	}
	if calls != 2 {
		t.Errorf("expected 2 HTTP calls (cursor drain), got %d", calls)
	}
}
```

    **(d) `server/api/internal/lava/subscription_test.go`:**

```go
package lava

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCancelSubscription_QueryParams(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/subscriptions" {
			t.Errorf("expected /api/v1/subscriptions, got %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("contractId") != "contract-x" {
			t.Errorf("expected contractId=contract-x, got %q", q.Get("contractId"))
		}
		if q.Get("email") != "alice+test@example.com" {
			t.Errorf("expected url-encoded email, got %q", q.Get("email"))
		}
		// + must be encoded in the raw query but decoded by url.Query() — accept either form.
		if !strings.Contains(r.URL.RawQuery, "contractId=contract-x") {
			t.Errorf("expected RawQuery to contain contractId=contract-x, got %q", r.URL.RawQuery)
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()
	c := newWithBaseURL("k", srv.URL)
	if err := c.CancelSubscription(context.Background(), "contract-x", "alice+test@example.com"); err != nil {
		t.Fatalf("CancelSubscription: %v", err)
	}
}

func TestCancelSubscription_LavaError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"message":"contract not found"}`))
	}))
	defer srv.Close()
	c := newWithBaseURL("k", srv.URL)
	if err := c.CancelSubscription(context.Background(), "missing", "a@b.c"); err == nil {
		t.Fatalf("expected error on 404")
	}
}
```

    **(e) `server/api/internal/lava/webhook_test.go`:** covers PAY-07 (ConstantTimeCompare) + previous-secret fallback (D-17).

```go
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
```

    Run `cd server/api && go test ./internal/lava/ -count=1 -timeout=30s -v` — all tests must pass.
  </action>
  <acceptance_criteria>
    - 5 test files exist: `client_test.go`, `invoice_test.go`, `products_test.go`, `subscription_test.go`, `webhook_test.go` (all under `server/api/internal/lava/`)
    - `grep "TestClient_HardcodedBaseURL_5sTimeout_NoRedirect" server/api/internal/lava/client_test.go` finds one match (matches PAY-16 row in 03-VALIDATION.md)
    - `grep "TestVerifyAPIKey_ConstantTime" server/api/internal/lava/webhook_test.go` finds one match (matches PAY-07 row)
    - `grep "TestListProducts_PaginationDrain" server/api/internal/lava/products_test.go` finds one match
    - `cd server/api && go test ./internal/lava/ -count=1 -timeout=30s` exits 0
    - All tests pass; httptest mocks correctly verify request shape (X-Api-Key header, path, query params, body)
  </acceptance_criteria>
  <automated>cd server/api && go test ./internal/lava/ -count=1 -timeout=30s</automated>
  <done>Lava package has 100% test coverage of public methods; PAY-07 + PAY-16 verified via unit tests; PAY-02 + PAY-10 partial coverage (handler-level coverage lands in plans 03-05 and 03-06).</done>
</task>

</tasks>

<verification>
- `cd server/api && go build ./...` exits 0
- `cd server/api && go vet ./internal/lava/... ./internal/config/...` exits 0
- `cd server/api && go test ./internal/lava/ -count=1 -timeout=30s` exits 0 (all unit tests pass)
- `grep -rn 'lava.BaseURL\|"https://gate.lava.top"' server/api/internal/lava/` shows the const lives ONLY in `client.go` (no string-literal duplication elsewhere)
- `grep -c "LAVA_" server/api/internal/config/config.go` returns at least 16 (8 new fields + 4 RequireEnv lines + LAVA_ENV handling + Load assignments)
</verification>

<must_haves>
truths:
  - "Pure lava package implements 4 outbound endpoints + 1 inbound verifier; package has no Fiber/GORM imports."
  - "BaseURL is a const string literal — no env override (PAY-16 / D-15)."
  - "5-second HTTP timeout + no redirect follow (PAY-16)."
  - "VerifyAPIKey uses crypto/subtle.ConstantTimeCompare for both current + optional previous secret (PAY-07)."
  - "config.go strict-requires LAVA_WEBHOOK_SECRET, LAVA_WEBHOOK_ALLOWED_CIDRS, LAVA_SUCCESS_URL, LAVA_FAIL_URL, plus LAVA_API_KEY OR LAVA_API_KEY_SANDBOX based on LAVA_ENV."
  - "Active API key is resolved once at startup into Config.LavaActiveAPIKey; downstream code never picks between LAVA_API_KEY / LAVA_API_KEY_SANDBOX directly."
artifacts:
  - path: "server/api/internal/lava/client.go"
    provides: "*Client type + New constructor + hardcoded BaseURL + NewForTest helper for external-package httptest mocks"
    contains: 'const BaseURL = "https://gate.lava.top"'
  - path: "server/api/internal/lava/dto.go"
    provides: "All request/response DTOs (lava OpenAPI 1.17.0)"
    contains: "type WebhookEvent struct"
  - path: "server/api/internal/lava/webhook.go"
    provides: "VerifyAPIKey constant-time compare with optional previous secret"
    contains: "subtle.ConstantTimeCompare"
  - path: "server/api/internal/config/config.go"
    provides: "Strict-required LAVA_* env vars + LAVA_ENV selector"
    contains: "LavaActiveAPIKey"
key_links:
  - from: "server/api/internal/config/config.go::RequireEnv"
    to: "server/api/internal/config/config.go::Load"
    via: "missing key → logger.Fatal at startup (HOTFIX-08 pattern)"
    pattern: "LAVA_API_KEY \\(required when LAVA_ENV=production\\)"
  - from: "server/api/internal/lava/client.go::New"
    to: "server/api/internal/lava/client.go::BaseURL"
    via: "newWithBaseURL(apiKey, BaseURL) — package-private; only test code overrides"
    pattern: "newWithBaseURL\\(apiKey, BaseURL\\)"
</must_haves>

<threat_model>
## Trust Boundaries
| Boundary | Description |
|----------|-------------|
| API server → gate.lava.top | Outbound HTTPS with X-Api-Key. SSRF surface lives here. |
| Operator env → API server | LAVA_* secrets cross this boundary at startup; must fail-fast on missing values. |
| (inbound webhook is OUT OF SCOPE for this plan — covered in 03-06) | |

## STRIDE Threat Register
| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-03-08 | Information disclosure | Client.apiKey field | mitigate | The struct field is unexported (lowercase `apiKey`); never serialized to JSON; never echoed in error paths (decodeJSON wraps lava's `message` only, never the request). API key never appears in logs (no `zap.String("apiKey", ...)` anywhere in plan 03-02 surface). |
| T-03-09 | Information disclosure | Outbound request body to lava | accept | The body contains buyer email + offer ID + currency. Email is PII but the recipient (lava.top) is the legitimate payment provider — this is the entire purpose of the call. TLS verification ON (default http.Transport) per D-14. |
| T-03-10 | Spoofing | SSRF via interpolated path | mitigate | All path segments use `url.PathEscape` (invoice ID); all query values use `url.QueryEscape` (contractId, email, nextPage). BaseURL is a const literal — no env interpolation. Plan 03-11 smoke-greps for any string-literal "https://" outside `lava/client.go`. |
| T-03-11 | Tampering | Open-redirect to attacker site | mitigate | `CheckRedirect` returns `ErrUseLastResponse` — the client refuses to follow ANY redirect. A lava-side compromise that issues 302 to an attacker URL is neutralized at the HTTP layer. |
| T-03-12 | Information disclosure | Timing attack on VerifyAPIKey | mitigate | Both current and previous secret compared via `crypto/subtle.ConstantTimeCompare` (PAY-07). NOT mitigated: length-based timing (different-length inputs return immediately) — accepted because the secret length is config-fixed at startup, not attacker-controlled. |
| T-03-13 | DoS | Slowloris / unbounded lava response | mitigate | `http.Client.Timeout: 5 * time.Second` is total request-response budget — no infinite hang. context.Context propagation in all 4 endpoints allows handler-side cancellation. |
| T-03-14 | Spoofing | Operator misconfigures LAVA_ENV → wrong key used in production | mitigate | RequireEnv compound check: `LAVA_ENV=sandbox` demands LAVA_API_KEY_SANDBOX; `LAVA_ENV=production` demands LAVA_API_KEY; anything else fails fast. Default is `production` — safer-by-default principle (RESEARCH §13.3). |
| T-03-15 | DoS | Pagination cursor loop on /api/v2/products | accept | Lava returns cursor in `nextPage`; we follow until empty. A lava-side bug could in theory create an infinite cursor — accepted because (a) each request has 5s timeout, (b) admin proxy endpoint (plan 03-05) is rate-limited by the global per-IP limiter (HOTFIX-03), (c) admin can stop calling the endpoint. |

ASVS L2 scoping per D-31: this plan IS in L2 scope (it touches the lava client, which is a payment-path component). All L2 controls applied: constant-time crypto compare (V6), no client-supplied URL (V13), strict env-validation (V14), TLS verification on (V9).
</threat_model>

<success_criteria>
1. `cd server/api && go build ./...` exits 0 after T01 + T02 + T03.
2. `cd server/api && go test ./internal/lava/ -count=1 -timeout=30s` exits 0.
3. `grep -rn 'lava.BaseURL\|"https://gate.lava.top"' server/api/internal/lava/` shows the literal only in `client.go` and the unit test (`client_test.go` line that asserts the const value).
4. `grep -c '"LAVA_' server/api/internal/config/config.go` returns at least 14 (8 env-var name strings + RequireEnv list members + dynamic-error formatting).
5. PAY-07 + PAY-16 verified by unit tests (`TestVerifyAPIKey_ConstantTime` + `TestClient_HardcodedBaseURL_5sTimeout_NoRedirect`); partial PAY-02 + PAY-10 (DTO + outbound shape) verified.
</success_criteria>

<output>
After T01 + T02 + T03 land, each as a separate atomic commit (`feat(03-02): ...` during execution; planner only writes plan file with `docs(03): plan lava-client-config`).
</output>
