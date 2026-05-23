package lava

import "crypto/subtle"

// VerifyAPIKey returns true iff the supplied X-Api-Key matches either the
// current or (when set) the previous shared secret. Both comparisons use
// crypto/subtle.ConstantTimeCompare so timing attacks cannot leak prefix
// matches (PAY-07 / CONTEXT.md D-17).
//
// Rotation flow (D-17):
//  1. Set LAVA_WEBHOOK_SECRET_PREVIOUS=<old>, LAVA_WEBHOOK_SECRET=<new>, restart.
//  2. Update lava.top dashboard to <new>.
//  3. Clear LAVA_WEBHOOK_SECRET_PREVIOUS, restart.
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
