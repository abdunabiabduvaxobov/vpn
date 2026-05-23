//go:build integration
// +build integration

// Package integration_test contains opt-in integration tests for the API.
// All files in this package carry the //go:build integration tag so they
// are excluded from the default `go test ./...` run. The operator runs
// them on demand against external services (lava.top sandbox, etc).
package integration_test

import (
	"context"
	"os"
	"testing"
	"time"

	"vpnapp/server/api/internal/lava"
)

// TestLavaSandbox_CreateInvoice exercises the real lava.top sandbox.
//
// Setup (manual, per 03-CONTEXT.md D-06):
//  1. Set LAVA_API_KEY_SANDBOX to the operator's sandbox key.
//  2. Set TEST_LAVA_OFFER_ID to a real sandbox offer UUID (configured ahead of
//     time via the lava.top dashboard for the "Test Pro Monthly $5" offer).
//  3. Set TEST_LAVA_EMAIL to an email reachable for the sandbox test (e.g. the
//     operator's mailbox).
//
// Run:
//
//	go test -tags=integration ./server/api/integration/... \
//	    -run TestLavaSandbox -count=1 -timeout=30s -v
//
// What it verifies:
//   - The lava client can authenticate against gate.lava.top with the sandbox key.
//   - CreateInvoice returns a valid invoice id + paymentUrl.
//   - Subsequent GetInvoice on the same id returns a parseable InvoiceDetailResponse.
//
// What it does NOT verify:
//   - Card payment flow (requires browser interaction with the returned paymentUrl).
//   - Webhook delivery (requires public-facing webhook endpoint via ngrok/cloudflared).
//
// The launch acceptance test (real card -> webhook -> tier flip in <=5s) per
// 03-VALIDATION.md Manual-Only Verifications row 1 is operator-driven; this
// test covers the API-side portions that CAN be automated.
func TestLavaSandbox_CreateInvoice(t *testing.T) {
	apiKey := os.Getenv("LAVA_API_KEY_SANDBOX")
	offerID := os.Getenv("TEST_LAVA_OFFER_ID")
	email := os.Getenv("TEST_LAVA_EMAIL")
	if apiKey == "" || offerID == "" || email == "" {
		t.Skip("integration test requires LAVA_API_KEY_SANDBOX, TEST_LAVA_OFFER_ID, TEST_LAVA_EMAIL")
	}

	client := lava.New(apiKey)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. CreateInvoice.
	inv, err := client.CreateInvoice(ctx, lava.CreateInvoiceRequest{
		Email:       email,
		OfferID:     offerID,
		Currency:    "USD",
		Periodicity: "MONTHLY",
	})
	if err != nil {
		t.Fatalf("CreateInvoice: %v", err)
	}
	if inv.ID == "" {
		t.Errorf("expected non-empty invoice id")
	}
	if inv.PaymentURL == nil || *inv.PaymentURL == "" {
		t.Errorf("expected paymentUrl set; got nil/empty")
	}
	t.Logf("sandbox invoice created: id=%s paymentUrl=%s", inv.ID, ptrStr(inv.PaymentURL))

	// 2. GetInvoice — escalate-path probe.
	detail, err := client.GetInvoice(ctx, inv.ID)
	if err != nil {
		t.Fatalf("GetInvoice: %v", err)
	}
	if detail.ID != inv.ID {
		t.Errorf("expected GetInvoice.ID=%s, got %s", inv.ID, detail.ID)
	}
	t.Logf("sandbox invoice status: %s (type=%s)", detail.Status, detail.Type)

	// 3. Hand off to the operator for the manual card-payment portion.
	t.Logf(`
================ MANUAL PORTION (per 03-VALIDATION.md row 1) ================
1. Open the payment URL above in a browser.
2. Use the sandbox test card from operator's lava.top account.
3. Complete the payment.
4. Observe the local API server log for "webhook: payment.success applied".
5. Curl GET /api/v1/subscription with the test user's JWT — confirm tier=pro within 5s.
6. Confirm subscription_expires_at populated from the webhook's period_end.
============================================================================`)
}

// ptrStr returns "" when the pointer is nil, else its dereferenced value.
// Tiny helper so t.Logf calls stay readable without `if p != nil` boilerplate.
func ptrStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
