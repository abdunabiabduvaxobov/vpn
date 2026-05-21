package handler_test

// HOTFIX-04 regression tests. Verify the global Fiber ErrorHandler:
//   - scrubs 5xx response bodies to {"error":"internal server error","request_id":"<uuid>"} (D-05)
//   - leaves 4xx bodies verbose (D-06)
//   - echoes incoming X-Request-ID verbatim
//   - generates a UUIDv4 when no X-Request-ID is sent
//
// These tests drive a tiny Fiber app built per-test via newErrorHandlerApp().
// They DO NOT touch the real DB or main.go wiring — the assertions are on
// the handler closure in isolation, with the requestid middleware wired in
// the same order cmd/main.go uses (requestid BEFORE recover).

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"vpnapp/server/api/internal/handler"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/requestid"
	"github.com/gofiber/fiber/v2/utils"
	"go.uber.org/zap"
)

// newErrorHandlerApp builds a Fiber app with the production ErrorHandler and
// requestid middleware wired identically to cmd/main.go. Two routes:
//   - GET /500-pq returns a fake `pq: connection refused` to exercise 5xx scrub.
//   - GET /400-validate returns a fiber 400 to exercise the 4xx pass-through.
func newErrorHandlerApp() *fiber.App {
	app := fiber.New(fiber.Config{
		ErrorHandler: handler.ErrorHandler(zap.NewNop()),
	})
	app.Use(requestid.New(requestid.Config{
		Header:    fiber.HeaderXRequestID,
		Generator: utils.UUIDv4,
	}))
	app.Get("/500-pq", func(c *fiber.Ctx) error {
		return fmt.Errorf("pq: connection refused")
	})
	app.Get("/400-validate", func(c *fiber.Ctx) error {
		return fiber.NewError(fiber.StatusBadRequest, "email required")
	})
	return app
}

// readJSON reads the response body fully and unmarshals it into a generic map.
func readJSON(t *testing.T, body io.ReadCloser) (map[string]any, string) {
	t.Helper()
	raw, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	defer body.Close()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode body %q: %v", string(raw), err)
	}
	return m, string(raw)
}

// TestErrorHandler_Scrubs5xxBody asserts the response body for an induced 500
// is exactly {"error":"internal server error","request_id":"<uuid>"} — no GORM
// driver name, no library prefix, no internal err.Error() text reaches the
// client. This closes CR CRIT-04 / S9-1.
func TestErrorHandler_Scrubs5xxBody(t *testing.T) {
	app := newErrorHandlerApp()

	req := httptest.NewRequest(fiber.MethodGet, "/500-pq", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}

	if resp.StatusCode != fiber.StatusInternalServerError {
		t.Fatalf("status: got %d want %d", resp.StatusCode, fiber.StatusInternalServerError)
	}

	body, raw := readJSON(t, resp.Body)

	// Body must NOT contain any of the known leak markers.
	for _, marker := range []string{"pq:", "bcrypt:", "gorm:"} {
		if strings.Contains(raw, marker) {
			t.Fatalf("body leaks %q: %s", marker, raw)
		}
	}

	// Body must have EXACTLY two keys.
	if len(body) != 2 {
		t.Fatalf("body has %d keys, want 2: %v", len(body), body)
	}
	if got, _ := body["error"].(string); got != "internal server error" {
		t.Fatalf("error: got %q want %q", got, "internal server error")
	}
	if rid, _ := body["request_id"].(string); rid == "" {
		t.Fatalf("request_id missing or empty: %v", body)
	}
}

// TestErrorHandler_EchoesIncomingRequestID asserts that an incoming
// X-Request-ID is echoed verbatim in both the response header and the body's
// request_id field. This lets clients trace a single request end-to-end.
func TestErrorHandler_EchoesIncomingRequestID(t *testing.T) {
	app := newErrorHandlerApp()

	req := httptest.NewRequest(fiber.MethodGet, "/500-pq", nil)
	req.Header.Set(fiber.HeaderXRequestID, "smoke-1")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}

	if got := resp.Header.Get(fiber.HeaderXRequestID); got != "smoke-1" {
		t.Fatalf("response header X-Request-ID: got %q want %q", got, "smoke-1")
	}

	body, _ := readJSON(t, resp.Body)
	if rid, _ := body["request_id"].(string); rid != "smoke-1" {
		t.Fatalf("body request_id: got %q want %q", rid, "smoke-1")
	}
}

// uuidv4Re matches the RFC 4122 v4 UUID shape (version nibble 4, variant nibble 8-b).
var uuidv4Re = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// TestErrorHandler_GeneratesUUIDv4WhenMissing asserts that when no incoming
// X-Request-ID is supplied, the middleware generates a fresh UUIDv4 (not the
// monotonic utils.UUID default — that would leak the request count).
func TestErrorHandler_GeneratesUUIDv4WhenMissing(t *testing.T) {
	app := newErrorHandlerApp()

	req := httptest.NewRequest(fiber.MethodGet, "/500-pq", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}

	headerRID := resp.Header.Get(fiber.HeaderXRequestID)
	if !uuidv4Re.MatchString(headerRID) {
		t.Fatalf("header request id %q does not match UUIDv4 regex", headerRID)
	}

	body, _ := readJSON(t, resp.Body)
	bodyRID, _ := body["request_id"].(string)
	if !uuidv4Re.MatchString(bodyRID) {
		t.Fatalf("body request_id %q does not match UUIDv4 regex", bodyRID)
	}
	if headerRID != bodyRID {
		t.Fatalf("header (%q) and body (%q) request ids differ", headerRID, bodyRID)
	}
}

// TestErrorHandler_PassesThrough4xx asserts that 4xx responses keep their
// verbose error text — they're client-UX surface, not a leak surface (D-06).
func TestErrorHandler_PassesThrough4xx(t *testing.T) {
	app := newErrorHandlerApp()

	req := httptest.NewRequest(fiber.MethodGet, "/400-validate", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}

	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status: got %d want %d", resp.StatusCode, fiber.StatusBadRequest)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	defer resp.Body.Close()

	if !strings.Contains(string(raw), `"error":"email required"`) {
		t.Fatalf("4xx body lost verbose error message: %s", raw)
	}
}
