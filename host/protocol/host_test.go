package protocol

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	pluginhost "github.com/darkace1998/GoRemote/host/plugin"
	"github.com/darkace1998/GoRemote/internal/eventbus"
	sdkplugin "github.com/darkace1998/GoRemote/sdk/plugin"
	"github.com/darkace1998/GoRemote/sdk/protocol"
)

type stubSession struct {
	mode       protocol.RenderMode
	startOut   string
	startPanic bool
	startSlow  time.Duration
}

func (s *stubSession) RenderMode() protocol.RenderMode { return s.mode }
func (s *stubSession) Start(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	if s.startPanic {
		panic("start boom")
	}
	if s.startSlow > 0 {
		select {
		case <-time.After(s.startSlow):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if stdout != nil && s.startOut != "" {
		if _, err := io.WriteString(stdout, s.startOut); err != nil {
			return err
		}
	}
	return nil
}
func (s *stubSession) Resize(ctx context.Context, sz protocol.Size) error { return nil }
func (s *stubSession) SendInput(ctx context.Context, d []byte) error      { return nil }
func (s *stubSession) Close() error                                       { return nil }

type stubModule struct {
	id        string
	kind      sdkplugin.Kind
	openPanic bool
	openErr   error
	session   protocol.Session
}

func (m *stubModule) Manifest() sdkplugin.Manifest {
	k := m.kind
	if k == "" {
		k = sdkplugin.KindProtocol
	}
	return sdkplugin.Manifest{
		ID:         m.id,
		Name:       m.id,
		Kind:       k,
		Version:    "1.0.0",
		APIVersion: sdkplugin.CurrentAPIVersion,
	}
}
func (m *stubModule) Settings() []protocol.SettingDef     { return nil }
func (m *stubModule) Capabilities() protocol.Capabilities { return protocol.Capabilities{} }
func (m *stubModule) Open(ctx context.Context, req protocol.OpenRequest) (protocol.Session, error) {
	if m.openPanic {
		panic("open boom")
	}
	if m.openErr != nil {
		return nil, m.openErr
	}
	return m.session, nil
}

func newHost(t *testing.T) (*Host, *eventbus.Bus[pluginhost.Event], context.CancelFunc) {
	t.Helper()
	bus := eventbus.New[pluginhost.Event]()
	ph := pluginhost.New(bus)
	h := New(ph)
	_, cancel := context.WithCancel(context.Background())
	return h, bus, cancel
}

func collectEvents(ctx context.Context, bus *eventbus.Bus[pluginhost.Event]) func() []pluginhost.Event {
	ch := bus.Subscribe(ctx, 16)
	var mu sync.Mutex
	out := []pluginhost.Event{}
	go func() {
		for ev := range ch {
			mu.Lock()
			out = append(out, ev)
			mu.Unlock()
		}
	}()
	return func() []pluginhost.Event {
		mu.Lock()
		defer mu.Unlock()
		return append([]pluginhost.Event(nil), out...)
	}
}

func TestRegisterAndOpen(t *testing.T) {
	h, _, cancel := newHost(t)
	defer cancel()
	ctx := context.Background()
	mod := &stubModule{id: "proto.ok", session: &stubSession{mode: protocol.RenderTerminal, startOut: "ok"}}
	if err := h.Register(ctx, mod, sdkplugin.TrustCore); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, ok := h.Module("proto.ok"); !ok {
		t.Fatalf("module missing")
	}
	if len(h.List()) != 1 {
		t.Fatalf("list=%d", len(h.List()))
	}

	sess, err := h.Open(ctx, "proto.ok", protocol.OpenRequest{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer sess.Close()

	var buf bytes.Buffer
	if err := sess.Start(ctx, nil, &buf); err != nil {
		t.Fatalf("start: %v", err)
	}
	if buf.String() != "ok" {
		t.Fatalf("got %q want ok", buf.String())
	}
}

func TestRegisterWrongKind(t *testing.T) {
	h, _, cancel := newHost(t)
	defer cancel()
	mod := &stubModule{id: "bad", kind: sdkplugin.KindCredential}
	err := h.Register(context.Background(), mod, sdkplugin.TrustCore)
	if !errors.Is(err, ErrWrongKind) {
		t.Fatalf("got %v", err)
	}
}

func TestOpenUnknown(t *testing.T) {
	h, _, cancel := newHost(t)
	defer cancel()
	_, err := h.Open(context.Background(), "missing", protocol.OpenRequest{})
	if !errors.Is(err, ErrProtocolNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestOpenPanicCaught(t *testing.T) {
	bus := eventbus.New[pluginhost.Event]()
	ph := pluginhost.New(bus)
	h := New(ph)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := collectEvents(ctx, bus)

	mod := &stubModule{id: "proto.panic", openPanic: true}
	if err := h.Register(ctx, mod, sdkplugin.TrustCore); err != nil {
		t.Fatal(err)
	}
	_, err := h.Open(ctx, "proto.panic", protocol.OpenRequest{})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "panic") {
		t.Fatalf("err should mention panic: %v", err)
	}

	time.Sleep(30 * time.Millisecond)
	foundCrash := false
	for _, ev := range events() {
		if ev.Kind == pluginhost.EventCrashed && ev.PluginID == "proto.panic" {
			foundCrash = true
		}
	}
	if !foundCrash {
		t.Fatalf("expected EventCrashed, got %+v", events())
	}
}

func TestStartPanicCaught(t *testing.T) {
	bus := eventbus.New[pluginhost.Event]()
	ph := pluginhost.New(bus)
	h := New(ph)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := collectEvents(ctx, bus)

	mod := &stubModule{id: "proto.startpanic", session: &stubSession{startPanic: true}}
	if err := h.Register(ctx, mod, sdkplugin.TrustCore); err != nil {
		t.Fatal(err)
	}
	sess, err := h.Open(ctx, "proto.startpanic", protocol.OpenRequest{})
	if err != nil {
		t.Fatal(err)
	}
	err = sess.Start(ctx, nil, nil)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !errors.Is(err, ErrSessionCrashed) {
		t.Fatalf("expected ErrSessionCrashed, got %v", err)
	}

	time.Sleep(30 * time.Millisecond)
	foundCrash := false
	for _, ev := range events() {
		if ev.Kind == pluginhost.EventCrashed && ev.PluginID == "proto.startpanic" {
			foundCrash = true
		}
	}
	if !foundCrash {
		t.Fatalf("expected EventCrashed, got %+v", events())
	}
}
