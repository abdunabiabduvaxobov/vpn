package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

// TestHealth_NoGoVersion pins HARD-17 (D-24): GET /health must NOT expose the
// Go runtime version. Leaking `go_version` hands an attacker the exact toolchain
// to match against known stdlib/runtime advisories.
//
// RED now: Health() currently returns "go_version": runtime.Version() at
// health.go:28. This test fails until that line is deleted. Flips GREEN when
// HARD-17 lands.
func TestHealth_NoGoVersion(t *testing.T) {
	app := fiber.New()
	app.Get("/health", Health())

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: want 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("decode /health body: %v (body=%s)", err, body)
	}

	if _, present := m["go_version"]; present {
		t.Errorf("HARD-17: /health leaks go_version key (got %v); it must be removed", m["go_version"])
	}

	// The other keys must survive the change.
	for _, k := range []string{"status", "uptime", "timestamp"} {
		if _, ok := m[k]; !ok {
			t.Errorf("HARD-17: /health dropped expected key %q (only go_version should go)", k)
		}
	}
}
