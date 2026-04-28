package credential

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pluginhost "github.com/goremote/goremote/host/plugin"
	"github.com/goremote/goremote/internal/eventbus"
	"github.com/goremote/goremote/sdk/credential"
	sdkplugin "github.com/goremote/goremote/sdk/plugin"
)

type stubProvider struct {
	id           string
	kind         sdkplugin.Kind
	state        credential.State
	resolveErr   error
	resolvePanic bool
	resolveSlow  time.Duration
	mat          *credential.Material

	unlockCalls atomic.Int32
	lockCalls   atomic.Int32
	stateCalls  atomic.Int32
}

func (p *stubProvider) Manifest() sdkplugin.Manifest {
	k := p.kind
	if k == "" {
		k = sdkplugin.KindCredential
	}
	return sdkplugin.Manifest{
		ID:         p.id,
		Name:       p.id,
		Kind:       k,
		Version:    "1.0.0",
		APIVersion: sdkplugin.CurrentAPIVersion,
	}
}
func (p *stubProvider) Capabilities() credential.Capabilities { return credential.Capabilities{} }
func (p *stubProvider) State(ctx context.Context) credential.State {
	p.stateCalls.Add(1)
	if p.state == "" {
		return credential.StateUnlocked
	}
	return p.state
}
func (p *stubProvider) Unlock(ctx context.Context, passphrase string) error {
	p.unlockCalls.Add(1)
	return nil
}
func (p *stubProvider) Lock(ctx context.Context) error { p.lockCalls.Add(1); return nil }
func (p *stubProvider) Resolve(ctx context.Context, ref credential.Reference) (*credential.Material, error) {
	if p.resolvePanic {
		panic("resolve boom")
	}
	if p.resolveSlow > 0 {
		select {
		case <-time.After(p.resolveSlow):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if p.resolveErr != nil {
		return nil, p.resolveErr
	}
	if p.mat != nil {
		return p.mat, nil
	}
	return &credential.Material{Reference: ref, Username: "u", Password: "p"}, nil
}
func (p *stubProvider) List(ctx context.Context) ([]credential.Reference, error) {
	return nil, nil
}

func newHost(t *testing.T, opts ...Option) (*Host, *eventbus.Bus[pluginhost.Event]) {
	t.Helper()
	bus := eventbus.New[pluginhost.Event]()
	ph := pluginhost.New(bus)
	return New(ph, opts...), bus
}

func TestRegisterValid(t *testing.T) {
	h, _ := newHost(t)
	p := &stubProvider{id: "cred.ok"}
	if err := h.Register(context.Background(), p, sdkplugin.TrustCore); err != nil {
		t.Fatalf("register: %v", err)
	}
	if len(h.List()) != 1 {
		t.Fatalf("list=%d", len(h.List()))
	}
}

func TestRegisterWrongKind(t *testing.T) {
	h, _ := newHost(t)
	p := &stubProvider{id: "bad", kind: sdkplugin.KindProtocol}
	err := h.Register(context.Background(), p, sdkplugin.TrustCore)
	if !errors.Is(err, ErrWrongKind) {
		t.Fatalf("got %v", err)
	}
}

func TestResolveSuccess(t *testing.T) {
	h, _ := newHost(t)
	p := &stubProvider{id: "cred.ok"}
	if err := h.Register(context.Background(), p, sdkplugin.TrustCore); err != nil {
		t.Fatal(err)
	}
	mat, err := h.Resolve(context.Background(), credential.Reference{ProviderID: "cred.ok", EntryID: "e"}, 0)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if mat == nil || mat.Username != "u" {
		t.Fatalf("bad mat: %+v", mat)
	}
	// No failure tracking
	h.mu.Lock()
	if h.failures["cred.ok"] != 0 {
		t.Fatalf("failures recorded")
	}
	h.mu.Unlock()
}

func TestResolveUnknownProvider(t *testing.T) {
	h, _ := newHost(t)
	_, err := h.Resolve(context.Background(), credential.Reference{ProviderID: "missing"}, 0)
	if !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestQuarantineAfterFailures(t *testing.T) {
	var clockMu sync.Mutex
	nowVal := time.Unix(1_700_000_000, 0)
	clock := func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		return nowVal
	}
	advance := func(d time.Duration) {
		clockMu.Lock()
		nowVal = nowVal.Add(d)
		clockMu.Unlock()
	}

	bus := eventbus.New[pluginhost.Event]()
	ph := pluginhost.New(bus)
	h := New(ph, WithClock(clock))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := bus.Subscribe(ctx, 16)
	var emu sync.Mutex
	events := []pluginhost.Event{}
	go func() {
		for ev := range ch {
			emu.Lock()
			events = append(events, ev)
			emu.Unlock()
		}
	}()

	p := &stubProvider{id: "cred.fail", resolveErr: errors.New("boom")}
	if err := h.Register(ctx, p, sdkplugin.TrustCore); err != nil {
		t.Fatal(err)
	}

	ref := credential.Reference{ProviderID: "cred.fail"}
	for i := 0; i < 3; i++ {
		_, err := h.Resolve(ctx, ref, 0)
		if err == nil {
			t.Fatalf("expected failure on attempt %d", i)
		}
	}
	if !h.Quarantined("cred.fail") {
		t.Fatalf("expected quarantine")
	}
	_, err := h.Resolve(ctx, ref, 0)
	if !errors.Is(err, ErrQuarantined) {
		t.Fatalf("expected ErrQuarantined got %v", err)
	}

	time.Sleep(30 * time.Millisecond)
	emu.Lock()
	foundQ := false
	for _, ev := range events {
		if ev.Kind == pluginhost.EventQuarantined && ev.PluginID == "cred.fail" {
			foundQ = true
		}
	}
	emu.Unlock()
	if !foundQ {
		t.Fatalf("expected EventQuarantined")
	}

	// Fix the provider, Reinstate, try again
	p.resolveErr = nil
	h.Reinstate("cred.fail")
	mat, err := h.Resolve(ctx, ref, 0)
	if err != nil {
		t.Fatalf("after reinstate: %v", err)
	}
	if mat == nil {
		t.Fatalf("nil mat")
	}

	// Advance clock beyond quarantine; re-trigger quarantine then advance
	p.resolveErr = errors.New("boom2")
	for i := 0; i < 3; i++ {
		_, _ = h.Resolve(ctx, ref, 0)
	}
	if !h.Quarantined("cred.fail") {
		t.Fatalf("expected quarantine again")
	}
	advance(DefaultQuarantineFor + time.Second)
	// Quarantined() check clears expired; but Resolve itself should now succeed if provider ok.
	p.resolveErr = nil
	if h.Quarantined("cred.fail") {
		t.Fatalf("quarantine should have expired")
	}
	if _, err := h.Resolve(ctx, ref, 0); err != nil {
		t.Fatalf("post-expiry resolve: %v", err)
	}
}

func TestResolvePanicIsFailure(t *testing.T) {
	h, _ := newHost(t, WithFailureThreshold(2))
	p := &stubProvider{id: "cred.panic", resolvePanic: true}
	if err := h.Register(context.Background(), p, sdkplugin.TrustCore); err != nil {
		t.Fatal(err)
	}
	ref := credential.Reference{ProviderID: "cred.panic"}
	_, err := h.Resolve(context.Background(), ref, 0)
	if err == nil {
		t.Fatalf("expected error")
	}
	_, err = h.Resolve(context.Background(), ref, 0)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !h.Quarantined("cred.panic") {
		t.Fatalf("expected quarantine from panics")
	}
}

func TestResolveTimeout(t *testing.T) {
	h, _ := newHost(t)
	p := &stubProvider{id: "cred.slow", resolveSlow: 200 * time.Millisecond}
	if err := h.Register(context.Background(), p, sdkplugin.TrustCore); err != nil {
		t.Fatal(err)
	}
	_, err := h.Resolve(context.Background(), credential.Reference{ProviderID: "cred.slow"}, 20*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}
}

func TestUnlockLockStatePassthrough(t *testing.T) {
	h, _ := newHost(t)
	p := &stubProvider{id: "cred.u", state: credential.StateLocked}
	if err := h.Register(context.Background(), p, sdkplugin.TrustCore); err != nil {
		t.Fatal(err)
	}
	if err := h.Unlock(context.Background(), "cred.u", "pw", 0); err != nil {
		t.Fatal(err)
	}
	if err := h.Lock(context.Background(), "cred.u", 0); err != nil {
		t.Fatal(err)
	}
	if h.State(context.Background(), "cred.u") != credential.StateLocked {
		t.Fatalf("state mismatch")
	}
	if p.unlockCalls.Load() != 1 || p.lockCalls.Load() != 1 || p.stateCalls.Load() != 1 {
		t.Fatalf("passthrough not invoked: %d %d %d", p.unlockCalls.Load(), p.lockCalls.Load(), p.stateCalls.Load())
	}

	// Unknown provider
	if err := h.Unlock(context.Background(), "nope", "", 0); !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("got %v", err)
	}
	if err := h.Lock(context.Background(), "nope", 0); !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("got %v", err)
	}
	if h.State(context.Background(), "nope") != credential.StateNotConfigured {
		t.Fatalf("expected StateNotConfigured")
	}
}

func TestAuditLogger(t *testing.T) {
	var buf strings.Builder
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	h, _ := newHost(t, WithAuditLogger(logger))
	p := &stubProvider{id: "cred.audit"}
	if err := h.Register(context.Background(), p, sdkplugin.TrustCore); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Resolve(context.Background(), credential.Reference{ProviderID: "cred.audit", EntryID: "k"}, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Resolve(context.Background(), credential.Reference{ProviderID: "missing"}, 0); err == nil {
		t.Fatal("expected error")
	}
	if err := h.Unlock(context.Background(), "cred.audit", "pw", 0); err != nil {
		t.Fatal(err)
	}
	if err := h.Lock(context.Background(), "cred.audit", 0); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"credential.resolve attempt",
		"credential.resolve ok",
		"credential.resolve denied",
		"credential.unlock ok",
		"credential.lock ok",
		"component=credential.audit",
		"provider=cred.audit",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("audit log missing %q in:\n%s", want, out)
		}
	}
}
