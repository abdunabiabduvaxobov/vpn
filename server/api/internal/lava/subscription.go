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
