package logging

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

type memoryHandler struct {
	records []slog.Record
	attrs   []slog.Attr
}

func (h *memoryHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *memoryHandler) Handle(ctx context.Context, r slog.Record) error {
	h.records = append(h.records, r)
	return nil
}
func (h *memoryHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	h.attrs = append(h.attrs, attrs...)
	return h
}
func (h *memoryHandler) WithGroup(name string) slog.Handler { return h }

func TestRedactingHandlerWithGroup(t *testing.T) {
	mem := &memoryHandler{}
	h := &redactingHandler{
		inner:   mem,
		pending: []slog.Attr{slog.String("k1", "v1")},
	}
	h2 := h.WithGroup("g1")
	if len(mem.attrs) != 1 || mem.attrs[0].Key != "k1" || mem.attrs[0].Value.String() != "v1" {
		t.Errorf("expected pending attrs to be flushed to inner handler, got %v", mem.attrs)
	}

	rh2, ok := h2.(*redactingHandler)
	if !ok {
		t.Fatalf("expected *redactingHandler, got %T", h2)
	}
	if len(rh2.pending) != 0 {
		t.Errorf("expected pending attrs to be empty after WithGroup, got %v", rh2.pending)
	}
}

func TestRedactingHandlerIsSensitiveKeyEmpty(t *testing.T) {
	h := &redactingHandler{}
	if h.isSensitiveKey("password") {
		t.Errorf("expected isSensitiveKey to return false when keys map is empty")
	}
}

func TestRedactingHandlerWithGroupEmptyPending(t *testing.T) {
	mem := &memoryHandler{}
	h := &redactingHandler{
		inner: mem,
		pending: nil,
	}
	h.WithGroup("g1")
	if len(mem.attrs) != 0 {
		t.Errorf("expected no attrs flushed to inner handler, got %v", mem.attrs)
	}
}

// LogValuer implementation for testing resolve
type testLogValuer struct {
	v slog.Value
}
func (t testLogValuer) LogValue() slog.Value {
	return t.v
}

func TestRedactAttrLogValuer(t *testing.T) {
	h := &redactingHandler{
		keys: map[string]struct{}{"secret": {}},
	}

	// Test LogValuer that returns a Group
	groupValuer := testLogValuer{
		v: slog.GroupValue(slog.String("secret", "val1"), slog.String("public", "val2")),
	}
	attr := slog.Attr{Key: "test", Value: slog.AnyValue(groupValuer)}

	redactedAttr := h.redactAttr(attr)
	if redactedAttr.Value.Kind() != slog.KindGroup {
		t.Fatalf("expected KindGroup, got %v", redactedAttr.Value.Kind())
	}

	groupAttrs := redactedAttr.Value.Group()
	if len(groupAttrs) != 2 {
		t.Fatalf("expected 2 attrs in group, got %d", len(groupAttrs))
	}

	if groupAttrs[0].Key != "secret" || groupAttrs[0].Value.String() != Redacted {
		t.Errorf("expected redacted secret, got %v", groupAttrs[0])
	}

	if groupAttrs[1].Key != "public" || groupAttrs[1].Value.String() != "val2" {
		t.Errorf("expected unredacted public, got %v", groupAttrs[1])
	}
}

func TestRedactingHandlerLevelFiltering(t *testing.T) {
	mem := &memoryHandler{}
	h := &redactingHandler{
		inner: mem,
		level: slog.LevelInfo,
	}

	if h.Enabled(context.Background(), slog.LevelDebug) {
		t.Errorf("expected debug to be disabled")
	}

	if !h.Enabled(context.Background(), slog.LevelInfo) {
		t.Errorf("expected info to be enabled")
	}
}

func TestRedactingHandlerHandle(t *testing.T) {
	mem := &memoryHandler{}
	h := &redactingHandler{
		inner: mem,
		keys: map[string]struct{}{"secret": {}},
		pending: []slog.Attr{
			slog.String("component", "test"),
		},
	}

	r := slog.NewRecord(time.Now(), slog.LevelInfo, "msg", 0)
	r.AddAttrs(slog.String("public", "value"), slog.String("secret", "hidden"))

	err := h.Handle(context.Background(), r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(mem.records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(mem.records))
	}

	rec := mem.records[0]
	var attrs []slog.Attr
	rec.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a)
		return true
	})

	if len(attrs) != 3 {
		t.Fatalf("expected 3 attrs, got %d", len(attrs))
	}

	// pending ones are prepended
	if attrs[0].Key != "component" || attrs[0].Value.String() != "test" {
		t.Errorf("expected component attr, got %v", attrs[0])
	}
	if attrs[1].Key != "public" || attrs[1].Value.String() != "value" {
		t.Errorf("expected public attr, got %v", attrs[1])
	}
	if attrs[2].Key != "secret" || attrs[2].Value.String() != Redacted {
		t.Errorf("expected redacted secret, got %v", attrs[2])
	}
}


func TestRedactingHandlerWithAttrsDedup(t *testing.T) {
	mem := &memoryHandler{}
	h := &redactingHandler{
		inner: mem,
		keys: map[string]struct{}{"secret": {}},
		pending: []slog.Attr{
			slog.String("component", "test"),
			slog.String("other", "val"),
		},
	}

	h2 := h.WithAttrs([]slog.Attr{
		slog.String("component", "updated"),
		slog.String("new", "val2"),
		slog.String("secret", "hidden"),
	})

	rh2 := h2.(*redactingHandler)
	if len(rh2.pending) != 4 {
		t.Fatalf("expected 4 pending attrs, got %d", len(rh2.pending))
	}

	if rh2.pending[0].Key != "component" || rh2.pending[0].Value.String() != "updated" {
		t.Errorf("expected component replaced, got %v", rh2.pending[0])
	}
	if rh2.pending[1].Key != "other" || rh2.pending[1].Value.String() != "val" {
		t.Errorf("expected other preserved, got %v", rh2.pending[1])
	}
	if rh2.pending[2].Key != "new" || rh2.pending[2].Value.String() != "val2" {
		t.Errorf("expected new appended, got %v", rh2.pending[2])
	}
	if rh2.pending[3].Key != "secret" || rh2.pending[3].Value.String() != Redacted {
		t.Errorf("expected secret redacted, got %v", rh2.pending[3])
	}
}

func TestRedactingHandlerWithAttrsDedupNotReplaced(t *testing.T) {
	mem := &memoryHandler{}
	h := &redactingHandler{
		inner: mem,
		pending: []slog.Attr{
			slog.String("other", "val"),
		},
	}

	h2 := h.WithAttrs([]slog.Attr{
		slog.String("component", "added"), // in dedupKeys but not in pending
	})

	rh2 := h2.(*redactingHandler)
	if len(rh2.pending) != 2 {
		t.Fatalf("expected 2 pending attrs, got %d", len(rh2.pending))
	}
	if rh2.pending[1].Key != "component" || rh2.pending[1].Value.String() != "added" {
		t.Errorf("expected component appended, got %v", rh2.pending[1])
	}
}

// A test log valuer that returns a string.
type stringLogValuer struct {
	v string
}
func (s stringLogValuer) LogValue() slog.Value {
	return slog.StringValue(s.v)
}

func TestRedactAttrLogValuerString(t *testing.T) {
	h := &redactingHandler{
		keys: map[string]struct{}{"secret": {}},
		patterns: DefaultSensitivePatterns,
	}

	// Valuer that returns a string, but the key is sensitive
	sensitiveStr := "some secret"
	attr := slog.Attr{Key: "secret", Value: slog.AnyValue(stringLogValuer{v: sensitiveStr})}

	redactedAttr := h.redactAttr(attr)
	if redactedAttr.Value.Kind() != slog.KindString {
		t.Fatalf("expected KindString, got %v", redactedAttr.Value.Kind())
	}

	if redactedAttr.Value.String() != Redacted {
		t.Errorf("expected redacted valuer string, got %v", redactedAttr.Value.String())
	}
}

func TestRedactAttrNilHandlerEnabled(t *testing.T) {
	mem := &memoryHandler{}
	h := &redactingHandler{
		inner: mem,
		// nil level means it uses inner's Enabled, which is true
	}

	if !h.Enabled(context.Background(), slog.LevelDebug) {
		t.Errorf("expected debug to be enabled with nil leveler")
	}
}

func TestRedactStringFalse(t *testing.T) {
	h := &redactingHandler{
		patterns: DefaultSensitivePatterns,
	}

	str, redacted := h.redactString("safe string")
	if redacted {
		t.Errorf("expected false, got true")
	}
	if str != "safe string" {
		t.Errorf("expected 'safe string', got %q", str)
	}
}
