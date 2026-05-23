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
