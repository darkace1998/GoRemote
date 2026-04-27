package logging

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
)

// redactingHandler wraps another slog.Handler and rewrites attributes whose
// keys or string values look sensitive before delegating to the inner handler.
type redactingHandler struct {
	inner    slog.Handler
	level    slog.Leveler
	keys     map[string]struct{}
	patterns []*regexp.Regexp
}

func (h *redactingHandler) Enabled(ctx context.Context, lvl slog.Level) bool {
	if h.level != nil && lvl < h.level.Level() {
		return false
	}
	return h.inner.Enabled(ctx, lvl)
}

func (h *redactingHandler) Handle(ctx context.Context, r slog.Record) error {
	// Rebuild the record with redacted attributes. slog.Record exposes Attrs
	// via an iterator, so we collect them, redact, and emit a new record.
	out := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(h.redactAttr(a))
		return true
	})
	return h.inner.Handle(ctx, out)
}

func (h *redactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	redacted := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		redacted[i] = h.redactAttr(a)
	}
	return &redactingHandler{
		inner:    h.inner.WithAttrs(redacted),
		level:    h.level,
		keys:     h.keys,
		patterns: h.patterns,
	}
}

func (h *redactingHandler) WithGroup(name string) slog.Handler {
	return &redactingHandler{
		inner:    h.inner.WithGroup(name),
		level:    h.level,
		keys:     h.keys,
		patterns: h.patterns,
	}
}

// redactAttr returns a redacted copy of a, recursing into slog.Group values.
func (h *redactingHandler) redactAttr(a slog.Attr) slog.Attr {
	if a.Value.Kind() == slog.KindGroup {
		groupAttrs := a.Value.Group()
		redacted := make([]any, 0, len(groupAttrs))
		for _, ga := range groupAttrs {
			redacted = append(redacted, h.redactAttr(ga))
		}
		return slog.Group(a.Key, redacted...)
	}

	if h.isSensitiveKey(a.Key) {
		return slog.String(a.Key, Redacted)
	}

	// Resolve LogValuer and similar indirection once, then inspect the value.
	v := a.Value.Resolve()
	if v.Kind() == slog.KindGroup {
		// LogValue returned a group; recurse.
		return h.redactAttr(slog.Attr{Key: a.Key, Value: v})
	}
	if v.Kind() == slog.KindString {
		if redacted, changed := h.redactString(v.String()); changed {
			return slog.String(a.Key, redacted)
		}
	}
	return a
}

func (h *redactingHandler) isSensitiveKey(key string) bool {
	if len(h.keys) == 0 {
		return false
	}
	_, ok := h.keys[strings.ToLower(key)]
	return ok
}

// redactString applies every configured pattern. If any pattern matches, the
// value is fully replaced with Redacted (we do not reveal even the
// non-matching portions, since context around a secret is often sensitive).
func (h *redactingHandler) redactString(s string) (string, bool) {
	for _, p := range h.patterns {
		if p.MatchString(s) {
			return Redacted, true
		}
	}
	return s, false
}

func buildKeySet(keys []string) map[string]struct{} {
	m := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		m[strings.ToLower(k)] = struct{}{}
	}
	return m
}
