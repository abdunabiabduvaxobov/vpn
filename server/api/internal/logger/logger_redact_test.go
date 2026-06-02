package logger

import (
	"encoding/json"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// newCaptureLogger builds a zap.Logger whose JSON output is captured into the
// returned *strings.Builder, then wraps it with the redacting core under test.
// This lets the tests assert on the exact serialised field values that would
// reach log aggregation.
func newCaptureLogger(buf *strings.Builder) *zap.Logger {
	encCfg := zap.NewProductionEncoderConfig()
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(encCfg),
		zapcore.AddSync(buf),
		zapcore.DebugLevel,
	)
	base := zap.New(core)
	return NewRedactingLogger(base)
}

// decodeLastLine parses the final JSON log line into a map for value assertions.
func decodeLastLine(t *testing.T, raw string) map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	last := lines[len(lines)-1]
	var m map[string]any
	if err := json.Unmarshal([]byte(last), &m); err != nil {
		t.Fatalf("failed to decode log line %q: %v", last, err)
	}
	return m
}

func TestRedactJWTShaped(t *testing.T) {
	var buf strings.Builder
	log := newCaptureLogger(&buf)

	jwt := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dummysignaturevaluedummysig"
	log.Info("auth", zap.String("note", jwt))

	m := decodeLastLine(t, buf.String())
	if got := m["note"]; got != "[REDACTED]" {
		t.Fatalf("JWT-shaped value not redacted: got %q want [REDACTED]", got)
	}
}

func TestRedactBase64URL32(t *testing.T) {
	var buf strings.Builder
	log := newCaptureLogger(&buf)

	// 43-char base64url string (e.g. a 32-byte opaque refresh token).
	secret := "Drmhze6EPcv0fN_81Bj-nABCDEFGHIJKLMNOPQRSTUv"
	log.Info("session", zap.String("x", secret))

	m := decodeLastLine(t, buf.String())
	if got := m["x"]; got != "[REDACTED]" {
		t.Fatalf("base64url-32 value not redacted: got %q want [REDACTED]", got)
	}
}

func TestRedactLeavesShortStringsAlone(t *testing.T) {
	var buf strings.Builder
	log := newCaptureLogger(&buf)

	log.Info("ok", zap.String("msg", "normal short text"))

	m := decodeLastLine(t, buf.String())
	if got := m["msg"]; got != "normal short text" {
		t.Fatalf("short string was altered: got %q", got)
	}
}

func TestRedactByKey(t *testing.T) {
	var buf strings.Builder
	log := newCaptureLogger(&buf)

	// A short value that does NOT match the shape regexes, but whose KEY is
	// sensitive, must still be redacted (belt-and-suspenders).
	log.Info("login", zap.String("token", "abc123"))

	m := decodeLastLine(t, buf.String())
	if got := m["token"]; got != "[REDACTED]" {
		t.Fatalf("sensitive key not redacted: got %q want [REDACTED]", got)
	}
}

func TestRedactNonStringFieldsUnchanged(t *testing.T) {
	var buf strings.Builder
	log := newCaptureLogger(&buf)

	log.Info("metrics", zap.Int("count", 42), zap.Bool("ok", true))

	m := decodeLastLine(t, buf.String())
	if got := m["count"]; got != float64(42) {
		t.Fatalf("int field altered: got %v", got)
	}
	if got := m["ok"]; got != true {
		t.Fatalf("bool field altered: got %v", got)
	}
}
