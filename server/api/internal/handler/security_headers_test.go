package handler

import (
	"testing"
)

// requiredAdminSecurityHeaders is the HARD-08 (D-16) contract: every admin API
// response must carry these hardening headers.
var requiredAdminSecurityHeaders = []string{
	"Strict-Transport-Security",
	"X-Content-Type-Options", // must be "nosniff"
	"Content-Security-Policy",
}

// TestAdminRoutes_CarrySecurityHeaders pins HARD-08: responses from the admin
// route group must carry HSTS, X-Content-Type-Options: nosniff, and a CSP.
//
// Today no security-headers middleware exists anywhere (a grep for HSTS/nosniff/
// CSP returns nothing); the admin group (main.go:399) mounts only
// auth/AdminRequired/AuditLog. HARD-08 adds Fiber's built-in helmet as the first
// middleware on the admin group.
//
// SKIP (compiling) now: the headers middleware is not mounted yet, and the admin
// group is assembled in cmd/main.go (not an importable handler), so there is no
// in-package surface to exercise it. When HARD-08 lands, replace this skip with:
// build a Fiber app, mount helmet.New(...) + a 200 handler under /admin, issue a
// request, and assert each header in requiredAdminSecurityHeaders is present
// (and X-Content-Type-Options == "nosniff"). Flips GREEN when HARD-08 lands.
func TestAdminRoutes_CarrySecurityHeaders(t *testing.T) {
	t.Skip("GREEN when HARD-08 admin security-headers (helmet: HSTS + nosniff + CSP) middleware lands")

	_ = requiredAdminSecurityHeaders
}
