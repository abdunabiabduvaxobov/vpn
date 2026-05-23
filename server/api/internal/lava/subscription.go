package lava

import (
	"context"
	"fmt"
	"net/url"
)

// CancelSubscription calls DELETE /api/v1/subscriptions?contractId=X&email=Y.
// The user keeps Pro until the current period ends — local DB downgrade
// happens via the expiry cron (plan 03-09 / D-26). The handler that wraps
// this (plan 03-05) writes lava_contracts.cancelled_at = now() afterwards.
//
// Both query parameters are REQUIRED by lava (RESEARCH §1.4).
//
// WR-08: use url.Values to build the query string so any future change that
// introduces a literal `&` / `=` / `%` / `+` in the email or contract id
// can't break the request (string concat with escapeQuery is brittle —
// works for today's ASCII-safe values, double-encodes if a `%` ever shows up).
func (c *Client) CancelSubscription(ctx context.Context, contractID, email string) error {
	q := url.Values{}
	q.Set("contractId", contractID)
	q.Set("email", email)
	path := "/api/v1/subscriptions?" + q.Encode()
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
