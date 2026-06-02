// Package logger provides a production zap.Logger whose string fields are
// scrubbed of token-shaped secrets before they are written to any sink.
//
// Motivation (HARD-10 / SECURITY-AUDIT S4-4): the codebase has many
// zap.String(...) call sites and ad-hoc client error reports that can carry
// JWTs, opaque refresh tokens, device secrets, or API keys into shared log
// aggregation. Rather than auditing and fixing every call site individually,
// we wrap the production logger's zapcore.Core so that ANY string field whose
// value looks like a secret — or whose KEY is a known-sensitive name — is
// replaced with "[REDACTED]" before serialisation.
//
// Tradeoff (D-18): the base64url-32 shape match is deliberately broad. It will
// occasionally redact a long non-secret identifier (for example a 32+ char
// base64url request ID) as a false positive. This is an intentional
// fail-safe-toward-redaction choice: a redacted ID is a minor diagnostic
// inconvenience, whereas a leaked token is a security incident.
package logger

import (
	"regexp"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	// jwtShaped matches a three-segment JWT where every segment is at least
	// 10 base64url chars (header.payload.signature). Real JWTs are far longer
	// but 10 keeps the match cheap while avoiding false hits on dotted words.
	jwtShaped = regexp.MustCompile(`^[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}$`)

	// base64url32 matches a single base64url token of 32+ chars — covers
	// 32-byte opaque refresh tokens (43 chars unpadded), API keys, and HMAC
	// secrets. Broad by design (see package doc, D-18).
	base64url32 = regexp.MustCompile(`^[A-Za-z0-9_-]{32,}$`)

	// sensitiveKeys are field names that must be redacted regardless of the
	// value's shape (belt-and-suspenders for short-but-secret values).
	sensitiveKeys = map[string]struct{}{
		"token":         {},
		"refresh_token": {},
		"access_token":  {},
		"secret":        {},
		"authorization": {},
	}
)

const redactedPlaceholder = "[REDACTED]"

// redactCore wraps an underlying zapcore.Core and scrubs token-shaped string
// fields (and known-sensitive keys) before delegating the write.
type redactCore struct {
	zapcore.Core
}

// With returns a clone whose accumulated context fields are also redacted so
// that logger.With(zap.String("token", ...)) cannot smuggle a secret past the
// scrubber via structured context.
func (rc *redactCore) With(fields []zapcore.Field) zapcore.Core {
	return &redactCore{Core: rc.Core.With(redactFields(fields))}
}

// Check delegates to the embedded core but registers THIS core as the writer,
// so redaction runs for entries that pass the level filter.
func (rc *redactCore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if rc.Enabled(ent.Level) {
		return ce.AddCore(ent, rc)
	}
	return ce
}

// Write scrubs the fields then delegates to the embedded core.
func (rc *redactCore) Write(ent zapcore.Entry, fields []zapcore.Field) error {
	return rc.Core.Write(ent, redactFields(fields))
}

// redactFields returns a copy of fields with token-shaped string values (and
// values under sensitive keys) replaced by the redaction placeholder. The
// input slice is not mutated.
func redactFields(fields []zapcore.Field) []zapcore.Field {
	out := make([]zapcore.Field, len(fields))
	copy(out, fields)
	for i := range out {
		f := out[i]
		if f.Type != zapcore.StringType {
			continue
		}
		if shouldRedact(f.Key, f.String) {
			out[i].String = redactedPlaceholder
		}
	}
	return out
}

// shouldRedact reports whether a string field's value must be replaced, either
// because its key is known-sensitive or its value matches a secret shape.
func shouldRedact(key, value string) bool {
	if _, ok := sensitiveKeys[key]; ok {
		return true
	}
	return jwtShaped.MatchString(value) || base64url32.MatchString(value)
}

// NewRedactingLogger wraps an existing *zap.Logger so that every entry it (and
// loggers derived from it via With) writes is passed through the redacting
// core. Callers receive a drop-in replacement: all existing call sites keep
// working unchanged, but token-shaped strings can no longer leak to log sinks.
func NewRedactingLogger(base *zap.Logger) *zap.Logger {
	return base.WithOptions(zap.WrapCore(func(c zapcore.Core) zapcore.Core {
		return &redactCore{Core: c}
	}))
}

// Production builds a standard zap.NewProduction logger already wrapped with
// the redacting core. It is a convenience for callers (e.g. main) that want a
// safe logger in one call.
func Production() (*zap.Logger, error) {
	base, err := zap.NewProduction()
	if err != nil {
		return nil, err
	}
	return NewRedactingLogger(base), nil
}
