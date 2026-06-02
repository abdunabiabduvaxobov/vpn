package handler

import (
	"testing"
)

// debugErrorLimitPerMinute is the HARD-13 (D-21) target: /debug/error gets its
// own 5/min/IP bucket. Today the endpoint (main.go:328) rides only the global
// 30 req/min/IP limiter, so a client can amortise abuse of this unauthenticated
// log-writing endpoint over the broad public budget.
const debugErrorLimitPerMinute = 5

// TestDebugError_SixthCallPerMinute_Returns429 pins HARD-13: the 6th request
// from one IP within a minute must return 429.
//
// SKIP (compiling) now: the dedicated limiter does not exist yet. HARD-13 adds
// a middleware.DebugErrorLimit(redisClient, logger) (mirroring LinkAttemptLimit
// with key "debug:"+IP, window 60s, limit 5) and mounts it on the route:
//
//	api.Post("/debug/error", debugErrorLimit, handler)
//
// When that lands, replace this skip with: build the limiter against a
// miniredis, mount it + a 204 handler on a Fiber app, fire 6 rapid requests
// from RemoteAddr "10.0.0.9:1", and assert the 6th returns
// fiber.StatusTooManyRequests. Flips GREEN when HARD-13 lands.
func TestDebugError_SixthCallPerMinute_Returns429(t *testing.T) {
	t.Skip("GREEN when HARD-13 middleware.DebugErrorLimit (5/min/IP) lands and is mounted on /debug/error")

	_ = debugErrorLimitPerMinute
}
