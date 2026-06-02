package logger

import (
	"regexp"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// HARD-10 (D-18 / SC#6) redaction contract. These regexes are the literal lock:
// any logged STRING field whose VALUE matches one of these must be replaced with
// the redacted marker before it reaches any sink — even via zap.String("token",
// x), because redaction happens at the resolved-field level (a wrapping
// zapcore.Core), not by trusting call sites to mask.
var (
	// JWT-shaped: three base64url segments separated by '.'.
	jwtShapedPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}$`)
	// Opaque base64url-32+ (the new opaque refresh tokens and start-tokens).
	base64url32Pattern = regexp.MustCompile(`^[A-Za-z0-9_-]{32,}$`)
)

// redactedMarker is the replacement string the redactor must emit.
const redactedMarker = "[REDACTED]"

// sample secrets exercising both contract regexes.
const (
	sampleJWT      = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ1c2VyLTEyMyJ9.c2lnbmF0dXJlX3NlZ21lbnRfYWJjZGVmZ2hpamtsbW5vcA"
	sampleOpaque43 = "abcdefghijklmnopqrstuvwxyz0123456789_-ABCDEF" // 43 base64url chars
)

// TestRedactingLogger_RedactsTokenValues pins HARD-10.
//
// SKIP (compiling) now: logger.NewRedactingLogger does not exist yet. When
// HARD-10 lands, replace this skip body with the wired assertion below: build a
// zaptest/observer in-memory core, wrap it with the redacting core via
// NewRedactingLogger, log a JWT-shaped value and a 43-char base64url value under
// zap.String, then assert every observed entry's serialised fields contain
// redactedMarker and NOT the original secret. Flips GREEN when HARD-10 lands.
func TestRedactingLogger_RedactsTokenValues(t *testing.T) {
	// Confirm the test data actually matches the contract regexes so this file
	// fails loudly if the samples drift — independent of the (absent) redactor.
	if !jwtShapedPattern.MatchString(sampleJWT) {
		t.Fatalf("sample JWT %q does not match the JWT-shaped contract regex", sampleJWT)
	}
	if !base64url32Pattern.MatchString(sampleOpaque43) {
		t.Fatalf("sample opaque token %q does not match the base64url-32 contract regex", sampleOpaque43)
	}

	t.Skip("GREEN when logger.NewRedactingLogger lands (HARD-10): wrap an observer core and assert [REDACTED]")

	// --- intended assertion (uncomment when NewRedactingLogger exists) ---
	//
	// core, logs := observer.New(zapcore.InfoLevel)
	// log := NewRedactingLogger(zap.New(core))
	// log.Info("auth", zap.String("token", sampleJWT), zap.String("x", sampleOpaque43))
	// for _, e := range logs.All() {
	// 	for _, f := range e.Context {
	// 		if f.Type == zapcore.StringType {
	// 			if f.String == sampleJWT || f.String == sampleOpaque43 {
	// 				t.Errorf("HARD-10: secret leaked unredacted in field %q", f.Key)
	// 			}
	// 		}
	// 	}
	// }
	_ = observer.New
	_ = zapcore.InfoLevel
	_ = zap.String
	_ = redactedMarker
}
