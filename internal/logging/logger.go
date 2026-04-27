// Package logging provides a structured logger built on top of log/slog with
// automatic redaction of sensitive attribute keys and string values.
//
// It is intended to be the single entry point for logging across goremote so
// that secrets (credentials, tokens, PEM blocks, etc.) never reach log sinks.
package logging

import (
	"io"
	"log/slog"
	"os"
	"regexp"
)

// Redacted is the string substituted in place of any attribute value that
// matches a sensitive key or pattern.
const Redacted = "[REDACTED]"

// DefaultSensitiveKeys lists attribute keys whose values are always redacted,
// regardless of type. Matching is case-insensitive.
var DefaultSensitiveKeys = []string{
	"password",
	"passwd",
	"secret",
	"token",
	"api_key",
	"apikey",
	"authorization",
	"private_key",
	"passphrase",
	"otp",
}

// DefaultSensitivePatterns lists regular expressions applied to string
// attribute values. A match causes the value to be replaced with Redacted.
var DefaultSensitivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`-----BEGIN [^-]+-----[\s\S]*?-----END [^-]+-----`),
	regexp.MustCompile(`Bearer\s+[A-Za-z0-9._\-]+`),
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
}

// Options configures [New].
//
// All fields are optional; the zero value produces a JSON logger written to
// stderr at slog.LevelInfo with the default sensitive-key and sensitive-pattern
// sets enabled.
type Options struct {
	// Level is the minimum level emitted by the logger. Defaults to
	// slog.LevelInfo when nil.
	Level slog.Leveler

	// Writer is the sink for the default JSON handler. Ignored if Handler is
	// set. Defaults to os.Stderr.
	Writer io.Writer

	// Handler, when non-nil, is wrapped directly and used as the inner
	// handler. This allows callers to plug in alternate formats (text,
	// tint, etc.) while still getting redaction.
	Handler slog.Handler

	// SensitiveKeys overrides [DefaultSensitiveKeys]. Keys are matched
	// case-insensitively. Use an explicit empty non-nil slice to disable
	// key-based redaction.
	SensitiveKeys []string

	// SensitivePatterns overrides [DefaultSensitivePatterns]. Use an explicit
	// empty non-nil slice to disable pattern-based redaction.
	SensitivePatterns []*regexp.Regexp

	// AddSource mirrors slog.HandlerOptions.AddSource for the default handler.
	AddSource bool
}

// New returns a *slog.Logger whose handler redacts sensitive attributes
// according to opts.
func New(opts Options) *slog.Logger {
	level := opts.Level
	if level == nil {
		level = slog.LevelInfo
	}

	inner := opts.Handler
	if inner == nil {
		w := opts.Writer
		if w == nil {
			w = os.Stderr
		}
		inner = slog.NewJSONHandler(w, &slog.HandlerOptions{
			Level:     level,
			AddSource: opts.AddSource,
		})
	}

	keys := opts.SensitiveKeys
	if keys == nil {
		keys = DefaultSensitiveKeys
	}
	patterns := opts.SensitivePatterns
	if patterns == nil {
		patterns = DefaultSensitivePatterns
	}

	h := &redactingHandler{
		inner:    inner,
		level:    level,
		keys:     buildKeySet(keys),
		patterns: patterns,
	}
	return slog.New(h)
}

// WithComponent returns a child logger that tags every record with
// component=name. It is a small convenience wrapper over logger.With.
func WithComponent(logger *slog.Logger, name string) *slog.Logger {
	if logger == nil {
		logger = slog.Default()
	}
	return logger.With(slog.String("component", name))
}
