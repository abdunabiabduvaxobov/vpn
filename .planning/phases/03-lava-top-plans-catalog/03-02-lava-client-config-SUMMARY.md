---
phase: 3
plan: 02
subsystem: payment-provider
tags: [lava, payment, http-client, security, config]
requires:
  - HOTFIX-08 aggregate env validator (Phase 1) — RequireEnv pattern extended
  - Go 1.25 stdlib (crypto/subtle, net/http, net/url, encoding/json)
provides:
  - lava.Client (CreateInvoice, GetInvoice, ListProducts, CancelSubscription)
  - lava.VerifyAPIKey (constant-time webhook secret compare with rotation window)
  - lava.NewForTest (httptest affordance — consumed by plan 03-05 + 03-06)
  - lava.* DTOs (CreateInvoiceRequest, InvoiceResponse, InvoiceDetailResponse,
    ProductsResponse, WebhookEvent + nested types — pinned to OpenAPI 1.17.0)
  - config.LavaActiveAPIKey (resolved at startup from LAVA_ENV)
  - 4 new strict-required env vars (LAVA_WEBHOOK_SECRET,
    LAVA_WEBHOOK_ALLOWED_CIDRS, LAVA_SUCCESS_URL, LAVA_FAIL_URL)
  - Compound env validation: LAVA_API_KEY when LAVA_ENV=production,
    LAVA_API_KEY_SANDBOX when LAVA_ENV=sandbox; unknown values rejected
affects:
  - server/api/internal/config/config.go (Config struct + Load + RequireEnv)
  - server/api/internal/lava/ (new package, 6 source files + 5 test files)
tech-stack:
  added: []
  patterns:
    - "Pure HTTP client package: no Fiber, no GORM, no globals (BaseURL aside)"
    - "Hardcoded BaseURL const — SSRF mitigation per D-15"
    - "5s http.Client.Timeout + CheckRedirect=ErrUseLastResponse (open-redirect defence)"
    - "url.PathEscape on path segments + url.QueryEscape on query values"
    - "crypto/subtle.ConstantTimeCompare for webhook secret verification"
    - "Pagination drain in ListProducts (followed nextPage until empty)"
    - "Per-package test affordance (NewForTest) — keeps SSRF audit grep tight"
key-files:
  created:
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
  modified:
    - server/api/internal/config/config.go
decisions:
  - "BaseURL is a const string literal (never an env var) — D-15 / PAY-16"
  - "LAVA_ENV defaults to production (safer-by-default — RESEARCH §13.3)"
  - "LAVA_WEBHOOK_ALLOWED_CIDRS strict-required — no default CIDR list"
  - "NewForTest lives in client.go (with package) instead of inline in handler tests —
     plan 03-05 + 03-06 consume the helper; keeps SSRF grep in plan 03-11 narrow"
  - "Active API key resolved once at startup into LavaActiveAPIKey; downstream
     callers never inspect LAVA_API_KEY vs LAVA_API_KEY_SANDBOX directly"
metrics:
  duration_seconds: 365
  duration_human: "6m 5s"
  completed_at: "2026-05-23T11:07:05Z"
  tasks_completed: 3
  tests_added: 10
  tests_passing: 10
---

# Phase 3 Plan 02: lava-client-config Summary

Built the pure `internal/lava/` HTTP client package (no Fiber/GORM) implementing 4 outbound endpoints + 1 inbound webhook X-Api-Key verifier, with hardcoded `BaseURL` + 5s timeout + redirect-refusal, and extended `config.go` with 9 LAVA_* fields plus the LAVA_ENV compound validator.

## What Was Built

### Task 03-02-T01 — config.go LAVA_* extension (commit 6297a47)

Added 9 new fields to the `Config` struct in a "Phase 3 lava.top payment provider" section after the SSO Google fields:

- `LavaEnv` (default "production")
- `LavaAPIKey` / `LavaAPIKeySandbox`
- `LavaWebhookSecret` / `LavaWebhookSecretPrevious`
- `LavaWebhookAllowedCIDRs`
- `LavaSuccessURL` / `LavaFailURL`
- `LavaActiveAPIKey` (computed by `Load()` from `LavaEnv`)

`Load()` reads all 8 env vars and resolves `LavaActiveAPIKey` via a switch on `LavaEnv` (sandbox → `LavaAPIKeySandbox`; default → `LavaAPIKey`).

`RequireEnv()` rewritten:
- 4 unconditional new keys appended: `LAVA_WEBHOOK_SECRET`, `LAVA_WEBHOOK_ALLOWED_CIDRS`, `LAVA_SUCCESS_URL`, `LAVA_FAIL_URL`
- Compound compound check: `LAVA_ENV=production` requires `LAVA_API_KEY`; `LAVA_ENV=sandbox` requires `LAVA_API_KEY_SANDBOX`; anything else fails fast with a descriptive error string
- Default for unset `LAVA_ENV` is "production" — operators don't accidentally use sandbox keys

### Task 03-02-T02 — lava package (commit 7eb1db5)

Six new source files under `server/api/internal/lava/`:

| File | Provides |
|------|----------|
| `client.go` | `*Client`, `New(apiKey)`, `NewForTest(apiKey, baseURL)`, package-private `newWithBaseURL`, `do/decodeJSON/encodeJSON` helpers, `const BaseURL = "https://gate.lava.top"` |
| `dto.go` | All request/response DTOs pinned to lava OpenAPI 1.17.0 — including `WebhookEvent` (consumed by plan 03-06) |
| `invoice.go` | `CreateInvoice` (POST /api/v3/invoice), `GetInvoice` (GET /api/v2/invoices/{id} with `url.PathEscape`) |
| `products.go` | `ListProducts` — drains `nextPage` cursor server-side, filters POST entries |
| `subscription.go` | `CancelSubscription` (DELETE /api/v1/subscriptions?contractId=...&email=...) |
| `webhook.go` | `VerifyAPIKey(received, current, previous)` — constant-time compare with rotation fallback |

Security invariants enforced:

- `http.Client.Timeout = 5 * time.Second`
- `CheckRedirect = func(...) error { return http.ErrUseLastResponse }` (no redirect-following)
- `apiKey` field unexported, never serialized
- All path segments use `url.PathEscape`; all query values use `url.QueryEscape`
- TLS verification on (default `http.Transport`, no `InsecureSkipVerify`)

### Task 03-02-T03 — unit tests (commit 29fa994)

Five test files with httptest mocks. All 10 tests pass.

| Test | Coverage |
|------|----------|
| `TestClient_HardcodedBaseURL_5sTimeout_NoRedirect` | PAY-16 invariants (BaseURL literal, 5s timeout, refuse redirects, default TLS transport) |
| `TestNewWithBaseURL_OverridesForTests` | httptest helper round-trip via package-private constructor |
| `TestCreateInvoice_HappyPath` | POST /api/v3/invoice with X-Api-Key header + body shape + paymentUrl |
| `TestCreateInvoice_LavaError` | 422 lava error surfaces wrapped Go error |
| `TestGetInvoice_HappyPath` | GET /api/v2/invoices/{id} path escape + SubscriptionDetails parsing |
| `TestListProducts_PaginationDrain` | 2 pages followed; POST type filtered out; cursor query encoded |
| `TestCancelSubscription_QueryParams` | DELETE method + both query params + encoding of `+` in email |
| `TestCancelSubscription_LavaError` | 404 surfaces error |
| `TestVerifyAPIKey_ConstantTime` | PAY-07: current-secret match, previous-secret rotation match, wrong-secret reject, empty-received reject |
| `TestVerifyAPIKey_PrefixLengthNonLeakage` | Length-difference returns false without panic; long-prefix attacks rejected |

## Verification Results

```
cd server/api && go build ./...                                # exit 0
cd server/api && go vet ./internal/lava/... ./internal/config/...  # exit 0
cd server/api && go test ./internal/lava/ -count=1 -timeout=30s    # ok 0.588s, 10/10 PASS
```

Plan-level greps:
- `grep -rn '"https://gate.lava.top"' server/api/internal/lava/` → const in `client.go` line 24 + 3 references in `client_test.go` (the assertion of that const). No string-literal duplication elsewhere — passes success criterion #3.
- `grep -c "LAVA_" server/api/internal/config/config.go` → 24 (well above the required floor of 14).

## Deviations from Plan

None — plan executed exactly as written. All three sub-edits in T01, all six files in T02, and all five test files in T03 were copied verbatim from the plan; acceptance criteria and `<verification>` greps all pass; tests pass first run with no debugging required.

## Threat Model Compliance

All STRIDE mitigations in the plan's `<threat_model>` are now in code:

| Threat ID | Mitigation in code |
|-----------|--------------------|
| T-03-08 (Info disclosure: apiKey) | `Client.apiKey` unexported; no JSON tags; never echoed in `decodeJSON` |
| T-03-10 (SSRF) | `url.PathEscape` in `invoice.go`/`subscription.go`; `url.QueryEscape` everywhere; BaseURL const |
| T-03-11 (Open redirect) | `CheckRedirect` returns `ErrUseLastResponse` |
| T-03-12 (Timing attack) | `subtle.ConstantTimeCompare` for both current + previous secret |
| T-03-13 (DoS) | `Timeout: 5 * time.Second` + `context.Context` propagation |
| T-03-14 (LAVA_ENV misconfiguration) | RequireEnv compound check rejects unknown values, demands matching API key |

No new surfaces introduced beyond plan scope.

## Downstream Consumers

- **Plan 03-05** will consume `lava.NewForTest`, `lava.Client.CreateInvoice`, `lava.Client.GetInvoice`, `lava.Client.CancelSubscription`, `lava.Client.ListProducts`, and `config.LavaActiveAPIKey`.
- **Plan 03-06** will consume `lava.VerifyAPIKey`, `lava.WebhookEvent`, and `config.LavaWebhookSecret` / `LavaWebhookSecretPrevious` / `LavaWebhookAllowedCIDRs`.
- **Plan 03-11** SSRF audit grep will assert `BaseURL` literal lives only in `lava/client.go` + `lava/client_test.go`.

## Commits

| Task | Commit | Description |
|------|--------|-------------|
| T01 | `6297a47` | feat(03-02): extend config with LAVA_* env vars + LAVA_ENV selector |
| T02 | `7eb1db5` | feat(03-02): add pure lava.top HTTP client package |
| T03 | `29fa994` | test(03-02): add lava package unit tests (httptest mocks) |

## Self-Check: PASSED

Files exist:
- FOUND: server/api/internal/lava/client.go
- FOUND: server/api/internal/lava/dto.go
- FOUND: server/api/internal/lava/invoice.go
- FOUND: server/api/internal/lava/products.go
- FOUND: server/api/internal/lava/subscription.go
- FOUND: server/api/internal/lava/webhook.go
- FOUND: server/api/internal/lava/client_test.go
- FOUND: server/api/internal/lava/invoice_test.go
- FOUND: server/api/internal/lava/products_test.go
- FOUND: server/api/internal/lava/subscription_test.go
- FOUND: server/api/internal/lava/webhook_test.go
- FOUND: server/api/internal/config/config.go (modified)

Commits exist:
- FOUND: 6297a47
- FOUND: 7eb1db5
- FOUND: 29fa994
