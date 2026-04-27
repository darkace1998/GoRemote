package plugin

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/goremote/goremote/internal/eventbus"
	sdkplugin "github.com/goremote/goremote/sdk/plugin"
)

type stubModule struct {
	initCalls     atomic.Int32
	shutdownCalls atomic.Int32
	initErr       error
	initPanic     bool
	blockInit     time.Duration
}

func (s *stubModule) Init(ctx context.Context) error {
	s.initCalls.Add(1)
	if s.initPanic {
		panic("boom")
	}
	if s.blockInit > 0 {
		select {
		case <-time.After(s.blockInit):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return s.initErr
}
func (s *stubModule) Shutdown(ctx context.Context) error {
	s.shutdownCalls.Add(1)
	return nil
}

func validManifest(id string) sdkplugin.Manifest {
	return sdkplugin.Manifest{
		ID:         id,
		Name:       id,
		Kind:       sdkplugin.KindProtocol,
		Version:    "1.0.0",
		APIVersion: sdkplugin.CurrentAPIVersion,
	}
}

func drainEvents(ctx context.Context, ch <-chan Event) func() []Event {
	var mu sync.Mutex
	out := []Event{}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-ch:
				if !ok {
					return
				}
				mu.Lock()
				out = append(out, ev)
				mu.Unlock()
			}
		}
	}()
	return func() []Event {
		mu.Lock()
		defer mu.Unlock()
		return append([]Event(nil), out...)
	}
}

func TestRegisterValid(t *testing.T) {
	bus := eventbus.New[Event]()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := bus.Subscribe(ctx, 8)
	events := drainEvents(ctx, ch)

	h := New(bus)
	mod := &stubModule{}
	if err := h.Register(ctx, validManifest("x.y"), mod, sdkplugin.TrustCore); err != nil {
		t.Fatalf("register: %v", err)
	}
	if mod.initCalls.Load() != 1 {
		t.Fatalf("init not called")
	}
	if _, ok := h.Get("x.y"); !ok {
		t.Fatalf("Get returned false")
	}
	if len(h.List()) != 1 {
		t.Fatalf("list=%d want 1", len(h.List()))
	}

	time.Sleep(20 * time.Millisecond)
	evs := events()
	if len(evs) == 0 || evs[0].Kind != EventLoaded {
		t.Fatalf("expected loaded event, got %+v", evs)
	}
}

func TestRegisterDuplicateRejected(t *testing.T) {
	h := New(nil)
	ctx := context.Background()
	if err := h.Register(ctx, validManifest("dup"), &stubModule{}, sdkplugin.TrustCore); err != nil {
		t.Fatal(err)
	}
	err := h.Register(ctx, validManifest("dup"), &stubModule{}, sdkplugin.TrustCore)
	if !errors.Is(err, ErrAlreadyRegistered) {
		t.Fatalf("expected ErrAlreadyRegistered, got %v", err)
	}
}

func TestRegisterAPIMajorMismatch(t *testing.T) {
	h := New(nil)
	m := validManifest("bad")
	m.APIVersion = "2.0.0"
	err := h.Register(context.Background(), m, &stubModule{}, sdkplugin.TrustCore)
	if !errors.Is(err, ErrAPIVersionMismatch) {
		t.Fatalf("expected ErrAPIVersionMismatch, got %v", err)
	}
}

func TestRegisterPlatformMismatch(t *testing.T) {
	h := New(nil, WithGOOS("plan9"))
	m := validManifest("plat")
	m.Platforms = []string{"darwin", "linux", "windows"}
	err := h.Register(context.Background(), m, &stubModule{}, sdkplugin.TrustCore)
	if !errors.Is(err, ErrPlatformUnsupported) {
		t.Fatalf("expected ErrPlatformUnsupported, got %v", err)
	}

	// empty platforms list is universal
	m2 := validManifest("universal")
	m2.Platforms = nil
	if err := h.Register(context.Background(), m2, &stubModule{}, sdkplugin.TrustCore); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	_ = runtime.GOOS
}

func TestLifecycleInitError(t *testing.T) {
	bus := eventbus.New[Event]()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := bus.Subscribe(ctx, 8)
	events := drainEvents(ctx, ch)

	h := New(bus)
	mod := &stubModule{initErr: errors.New("init failure")}
	err := h.Register(ctx, validManifest("failing"), mod, sdkplugin.TrustCore)
	if err == nil {
		t.Fatalf("expected error")
	}
	if _, ok := h.Get("failing"); ok {
		t.Fatalf("plugin should not be registered")
	}

	time.Sleep(20 * time.Millisecond)
	found := false
	for _, ev := range events() {
		if ev.Kind == EventCrashed && ev.PluginID == "failing" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected EventCrashed")
	}
}

func TestLifecycleInitPanicRecovered(t *testing.T) {
	h := New(nil)
	mod := &stubModule{initPanic: true}
	err := h.Register(context.Background(), validManifest("panicky"), mod, sdkplugin.TrustCore)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestUnregisterCallsShutdown(t *testing.T) {
	h := New(nil)
	ctx := context.Background()
	mod := &stubModule{}
	if err := h.Register(ctx, validManifest("shut"), mod, sdkplugin.TrustCore); err != nil {
		t.Fatal(err)
	}
	if err := h.Unregister(ctx, "shut"); err != nil {
		t.Fatal(err)
	}
	if mod.shutdownCalls.Load() != 1 {
		t.Fatalf("shutdown not called")
	}
	if _, ok := h.Get("shut"); ok {
		t.Fatalf("still present")
	}
	if err := h.Unregister(ctx, "shut"); !errors.Is(err, ErrNotRegistered) {
		t.Fatalf("expected ErrNotRegistered, got %v", err)
	}
}

func TestEnforceCapability(t *testing.T) {
	h := New(nil)
	m := validManifest("caps")
	m.Capabilities = []sdkplugin.Capability{sdkplugin.CapNetworkOutbound}
	if err := h.Register(context.Background(), m, &stubModule{}, sdkplugin.TrustCore); err != nil {
		t.Fatal(err)
	}
	if err := h.EnforceCapability("caps", sdkplugin.CapNetworkOutbound); err != nil {
		t.Fatalf("declared capability denied: %v", err)
	}
	if err := h.EnforceCapability("caps", sdkplugin.CapClipboardWrite); !errors.Is(err, ErrCapabilityNotGranted) {
		t.Fatalf("expected ErrCapabilityNotGranted, got %v", err)
	}
	if err := h.EnforceCapability("missing", sdkplugin.CapNetworkOutbound); !errors.Is(err, ErrNotRegistered) {
		t.Fatalf("expected ErrNotRegistered, got %v", err)
	}
}

func TestApprovalHookRequiredForUntrusted(t *testing.T) {
	h := New(nil)
	err := h.Register(context.Background(), validManifest("u"), &stubModule{}, sdkplugin.TrustUntrusted)
	if !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("expected ErrApprovalRequired, got %v", err)
	}

	// With hook that rejects
	h2 := New(nil, WithApprovalHook(func(ctx context.Context, m sdkplugin.Manifest, tr sdkplugin.Trust) error {
		return errors.New("nope")
	}))
	if err := h2.Register(context.Background(), validManifest("u2"), &stubModule{}, sdkplugin.TrustCommunity); err == nil {
		t.Fatalf("expected rejection")
	}

	// With hook that approves
	called := 0
	h3 := New(nil, WithApprovalHook(func(ctx context.Context, m sdkplugin.Manifest, tr sdkplugin.Trust) error {
		called++
		return nil
	}))
	if err := h3.Register(context.Background(), validManifest("u3"), &stubModule{}, sdkplugin.TrustCommunity); err != nil {
		t.Fatal(err)
	}
	if called != 1 {
		t.Fatalf("hook not called")
	}
}

func TestConcurrentRegisterUnregister(t *testing.T) {
	h := New(nil)
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := "p." + itoa(i)
			m := validManifest(id)
			if err := h.Register(ctx, m, &stubModule{}, sdkplugin.TrustCore); err != nil {
				t.Errorf("register %s: %v", id, err)
				return
			}
			if err := h.Unregister(ctx, id); err != nil {
				t.Errorf("unregister %s: %v", id, err)
			}
		}(i)
	}
	wg.Wait()
	if len(h.List()) != 0 {
		t.Fatalf("expected empty, got %d", len(h.List()))
	}
}

// itoa avoids strconv import solely in tests.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
