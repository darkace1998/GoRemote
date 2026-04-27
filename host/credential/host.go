// Package credential implements the credential-provider host. It wraps
// host/plugin and adds reliability features specific to credential access:
// per-call timeouts, panic isolation, rolling-failure quarantine, and
// manual reinstatement.
package credential

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	pluginhost "github.com/goremote/goremote/host/plugin"
	"github.com/goremote/goremote/sdk/credential"
	sdkplugin "github.com/goremote/goremote/sdk/plugin"
)

// Errors returned by the credential host.
var (
	ErrWrongKind        = errors.New("provider manifest kind is not credential")
	ErrProviderNotFound = errors.New("credential provider not found")
	ErrQuarantined      = errors.New("credential provider is quarantined")
)

// Defaults governing failure-quarantine behavior.
const (
	DefaultFailureWindow    = time.Minute
	DefaultFailureThreshold = 3
	DefaultQuarantineFor    = 5 * time.Minute
	DefaultCallTimeout      = 10 * time.Second
)

// Clock is the time source used by the host. Injected via WithClock for
// deterministic tests.
type Clock func() time.Time

// Option configures a Host.
type Option func(*Host)

// WithClock overrides the time source (used for tests).
func WithClock(c Clock) Option {
	return func(h *Host) {
		if c != nil {
			h.now = c
		}
	}
}

// WithFailureThreshold overrides the number of failures within the rolling
// window that triggers quarantine.
func WithFailureThreshold(n int) Option {
	return func(h *Host) {
		if n > 0 {
			h.failureThreshold = n
		}
	}
}

// WithFailureWindow overrides the rolling failure window.
func WithFailureWindow(d time.Duration) Option {
	return func(h *Host) {
		if d > 0 {
			h.failureWindow = d
		}
	}
}

// WithQuarantineDuration overrides how long a provider is quarantined.
func WithQuarantineDuration(d time.Duration) Option {
	return func(h *Host) {
		if d > 0 {
			h.quarantineFor = d
		}
	}
}

// WithAuditLogger directs structured credential-access audit events
// (resolve / unlock / lock attempts and outcomes) to the supplied logger.
// A nil logger silences audit output. If never set, audit goes to a
// discard sink so library use in tests stays quiet.
func WithAuditLogger(l *slog.Logger) Option {
	return func(h *Host) {
		if l == nil {
			h.audit = slog.New(slog.NewTextHandler(io.Discard, nil))
			return
		}
		h.audit = l
	}
}

// Host is the credential-provider host.
type Host struct {
	ph *pluginhost.Host

	mu          sync.Mutex
	providers   map[string]credential.Provider
	failures    map[string]int
	firstFailAt map[string]time.Time
	quarantined map[string]time.Time // value = quarantine-until

	now              Clock
	failureThreshold int
	failureWindow    time.Duration
	quarantineFor    time.Duration

	audit *slog.Logger
}

// New constructs a credential Host backed by the given plugin host.
func New(ph *pluginhost.Host, opts ...Option) *Host {
	h := &Host{
		ph:               ph,
		providers:        make(map[string]credential.Provider),
		failures:         make(map[string]int),
		firstFailAt:      make(map[string]time.Time),
		quarantined:      make(map[string]time.Time),
		now:              time.Now,
		failureThreshold: DefaultFailureThreshold,
		failureWindow:    DefaultFailureWindow,
		quarantineFor:    DefaultQuarantineFor,
		audit:            slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// PluginHost returns the underlying generic plugin host.
func (h *Host) PluginHost() *pluginhost.Host { return h.ph }

// Register validates that p advertises a credential manifest and delegates
// lifecycle management to the plugin host.
func (h *Host) Register(ctx context.Context, p credential.Provider, trust sdkplugin.Trust) error {
	if p == nil {
		return errors.New("provider is nil")
	}
	man := p.Manifest()
	if man.Kind != sdkplugin.KindCredential {
		return fmt.Errorf("%w: provider %q kind=%s", ErrWrongKind, man.ID, man.Kind)
	}
	if err := h.ph.Register(ctx, man, p, trust); err != nil {
		return err
	}
	h.mu.Lock()
	h.providers[man.ID] = p
	h.mu.Unlock()
	return nil
}

// Unregister removes a provider and clears any tracked failure state.
func (h *Host) Unregister(ctx context.Context, id string) error {
	h.mu.Lock()
	delete(h.providers, id)
	delete(h.failures, id)
	delete(h.firstFailAt, id)
	delete(h.quarantined, id)
	h.mu.Unlock()
	return h.ph.Unregister(ctx, id)
}

// Provider returns the credential provider registered under id.
func (h *Host) Provider(id string) (credential.Provider, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	p, ok := h.providers[id]
	return p, ok
}

// List returns all registered credential providers.
func (h *Host) List() []credential.Provider {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]credential.Provider, 0, len(h.providers))
	for _, p := range h.providers {
		out = append(out, p)
	}
	return out
}

// Reinstate clears any quarantine and failure counters for the given provider.
func (h *Host) Reinstate(id string) {
	h.mu.Lock()
	delete(h.quarantined, id)
	delete(h.failures, id)
	delete(h.firstFailAt, id)
	h.mu.Unlock()
}

// Quarantined reports whether the provider is currently quarantined.
func (h *Host) Quarantined(id string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.isQuarantinedLocked(id)
}

func (h *Host) isQuarantinedLocked(id string) bool {
	until, ok := h.quarantined[id]
	if !ok {
		return false
	}
	if !h.now().Before(until) {
		// Expired; clear and return false so Resolve can proceed.
		delete(h.quarantined, id)
		delete(h.failures, id)
		delete(h.firstFailAt, id)
		return false
	}
	return true
}

// Resolve fetches the credential material behind ref through the appropriate
// provider. It enforces the per-call timeout, converts panics into failures,
// and quarantines the provider after repeated failures within the rolling
// window.
func (h *Host) Resolve(ctx context.Context, ref credential.Reference, timeout time.Duration) (*credential.Material, error) {
	id := ref.ProviderID
	start := h.now()
	h.audit.LogAttrs(ctx, slog.LevelInfo, "credential.resolve attempt",
		slog.String("component", "credential.audit"),
		slog.String("provider", id),
		slog.String("entry", ref.EntryID))
	h.mu.Lock()
	p, ok := h.providers[id]
	if !ok {
		h.mu.Unlock()
		err := fmt.Errorf("%w: %s", ErrProviderNotFound, id)
		h.audit.LogAttrs(ctx, slog.LevelWarn, "credential.resolve denied",
			slog.String("component", "credential.audit"),
			slog.String("provider", id),
			slog.String("entry", ref.EntryID),
			slog.String("err", err.Error()))
		return nil, err
	}
	if h.isQuarantinedLocked(id) {
		until := h.quarantined[id]
		h.mu.Unlock()
		err := fmt.Errorf("%w: provider=%s until=%s", ErrQuarantined, id, until.Format(time.RFC3339))
		h.audit.LogAttrs(ctx, slog.LevelWarn, "credential.resolve quarantined",
			slog.String("component", "credential.audit"),
			slog.String("provider", id),
			slog.String("entry", ref.EntryID),
			slog.Time("until", until))
		return nil, err
	}
	h.mu.Unlock()

	if timeout <= 0 {
		timeout = DefaultCallTimeout
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var mat *credential.Material
	err := safeCall(func() error {
		m, err := p.Resolve(cctx, ref)
		if err != nil {
			return err
		}
		mat = m
		return nil
	})
	dur := h.now().Sub(start)
	if err != nil {
		h.recordFailure(ctx, id, err)
		h.audit.LogAttrs(ctx, slog.LevelWarn, "credential.resolve failed",
			slog.String("component", "credential.audit"),
			slog.String("provider", id),
			slog.String("entry", ref.EntryID),
			slog.Duration("dur", dur),
			slog.String("err", err.Error()))
		return nil, err
	}
	h.recordSuccess(id)
	h.audit.LogAttrs(ctx, slog.LevelInfo, "credential.resolve ok",
		slog.String("component", "credential.audit"),
		slog.String("provider", id),
		slog.String("entry", ref.EntryID),
		slog.Duration("dur", dur))
	return mat, nil
}

// Unlock forwards to the provider under id with a timeout.
func (h *Host) Unlock(ctx context.Context, providerID, passphrase string, timeout time.Duration) error {
	p, ok := h.Provider(providerID)
	if !ok {
		err := fmt.Errorf("%w: %s", ErrProviderNotFound, providerID)
		h.audit.LogAttrs(ctx, slog.LevelWarn, "credential.unlock denied",
			slog.String("component", "credential.audit"),
			slog.String("provider", providerID),
			slog.String("err", err.Error()))
		return err
	}
	if timeout <= 0 {
		timeout = DefaultCallTimeout
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	err := safeCall(func() error { return p.Unlock(cctx, passphrase) })
	level := slog.LevelInfo
	msg := "credential.unlock ok"
	attrs := []slog.Attr{
		slog.String("component", "credential.audit"),
		slog.String("provider", providerID),
	}
	if err != nil {
		level = slog.LevelWarn
		msg = "credential.unlock failed"
		attrs = append(attrs, slog.String("err", err.Error()))
	}
	h.audit.LogAttrs(ctx, level, msg, attrs...)
	return err
}

// Lock forwards to the provider under id with a timeout.
func (h *Host) Lock(ctx context.Context, providerID string, timeout time.Duration) error {
	p, ok := h.Provider(providerID)
	if !ok {
		err := fmt.Errorf("%w: %s", ErrProviderNotFound, providerID)
		h.audit.LogAttrs(ctx, slog.LevelWarn, "credential.lock denied",
			slog.String("component", "credential.audit"),
			slog.String("provider", providerID),
			slog.String("err", err.Error()))
		return err
	}
	if timeout <= 0 {
		timeout = DefaultCallTimeout
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	err := safeCall(func() error { return p.Lock(cctx) })
	level := slog.LevelInfo
	msg := "credential.lock ok"
	attrs := []slog.Attr{
		slog.String("component", "credential.audit"),
		slog.String("provider", providerID),
	}
	if err != nil {
		level = slog.LevelWarn
		msg = "credential.lock failed"
		attrs = append(attrs, slog.String("err", err.Error()))
	}
	h.audit.LogAttrs(ctx, level, msg, attrs...)
	return err
}

// State forwards to the provider under id. Returns StateNotConfigured when
// the provider is unknown.
func (h *Host) State(ctx context.Context, providerID string) credential.State {
	p, ok := h.Provider(providerID)
	if !ok {
		return credential.StateNotConfigured
	}
	var s credential.State = credential.StateError
	_ = safeCall(func() error {
		s = p.State(ctx)
		return nil
	})
	return s
}

func (h *Host) recordSuccess(id string) {
	h.mu.Lock()
	delete(h.failures, id)
	delete(h.firstFailAt, id)
	h.mu.Unlock()
}

func (h *Host) recordFailure(ctx context.Context, id string, cause error) {
	now := h.now()
	h.mu.Lock()
	defer h.mu.Unlock()

	first, has := h.firstFailAt[id]
	if !has || now.Sub(first) > h.failureWindow {
		h.failures[id] = 1
		h.firstFailAt[id] = now
	} else {
		h.failures[id]++
	}

	if h.failures[id] >= h.failureThreshold {
		until := now.Add(h.quarantineFor)
		h.quarantined[id] = until
		h.failures[id] = 0
		delete(h.firstFailAt, id)
		if h.ph != nil {
			h.ph.Publish(ctx, pluginhost.Event{
				Kind:     pluginhost.EventQuarantined,
				PluginID: id,
				Err:      cause,
				At:       now,
			})
		}
	}
}

// safeCall runs fn and converts panics into errors.
func safeCall(fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return fn()
}
