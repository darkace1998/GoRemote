// Package plugin implements the generic plugin host: manifest validation,
// lifecycle management, capability enforcement, and a typed event feed.
//
// This host is transport-agnostic. It is used directly for built-in plugins;
// out-of-process plugins connect through the IPCTransport boundary declared in
// ipc.go.
package plugin

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/darkace1998/GoRemote/internal/eventbus"
	sdkplugin "github.com/darkace1998/GoRemote/sdk/plugin"
)

// EventKind classifies plugin-host events published on the event bus.
type EventKind string

const (
	EventLoaded      EventKind = "loaded"
	EventUnloaded    EventKind = "unloaded"
	EventCrashed     EventKind = "crashed"
	EventQuarantined EventKind = "quarantined"
)

// Event is published on the host's event bus whenever a plugin lifecycle
// transition or failure occurs.
type Event struct {
	Kind     EventKind
	PluginID string
	Err      error
	At       time.Time
}

// Loaded is a handle to a registered plugin, kept by the host.
type Loaded struct {
	Manifest sdkplugin.Manifest
	Module   any
	LoadedAt time.Time
	Trust    sdkplugin.Trust
}

// ApprovalHook is consulted for untrusted/community plugins before they are
// activated. Returning nil authorizes the plugin; returning an error aborts
// registration.
type ApprovalHook func(ctx context.Context, m sdkplugin.Manifest, trust sdkplugin.Trust) error

// Option configures a Host.
type Option func(*Host)

// WithApprovalHook installs a user-confirmation hook for untrusted/community
// plugins. If not set, those trust levels are rejected.
func WithApprovalHook(h ApprovalHook) Option {
	return func(host *Host) { host.approval = h }
}

// WithInitTimeout overrides the default Lifecycle.Init timeout.
func WithInitTimeout(d time.Duration) Option {
	return func(host *Host) {
		if d > 0 {
			host.initTimeout = d
		}
	}
}

// WithShutdownTimeout overrides the default Lifecycle.Shutdown timeout.
func WithShutdownTimeout(d time.Duration) Option {
	return func(host *Host) {
		if d > 0 {
			host.shutdownTimeout = d
		}
	}
}

// WithGOOS overrides the detected GOOS (used for testing platform gates).
func WithGOOS(goos string) Option {
	return func(host *Host) { host.goos = goos }
}

// Host is the generic plugin host.
type Host struct {
	mu      sync.RWMutex
	plugins map[string]*Loaded
	events  *eventbus.Bus[Event]

	approval        ApprovalHook
	initTimeout     time.Duration
	shutdownTimeout time.Duration
	goos            string
}

// Errors returned by Host.
var (
	ErrAlreadyRegistered    = errors.New("plugin already registered")
	ErrNotRegistered        = errors.New("plugin not registered")
	ErrAPIVersionMismatch   = errors.New("plugin api version incompatible with host")
	ErrPlatformUnsupported  = errors.New("plugin does not support this platform")
	ErrApprovalRequired     = errors.New("plugin requires user approval but no hook is configured")
	ErrCapabilityNotGranted = errors.New("capability not declared by plugin manifest")
)

// New constructs a Host. The events bus may be nil, in which case events are
// discarded; callers that care about lifecycle must pass a bus.
func New(events *eventbus.Bus[Event], opts ...Option) *Host {
	h := &Host{
		plugins:         make(map[string]*Loaded),
		events:          events,
		initTimeout:     10 * time.Second,
		shutdownTimeout: 10 * time.Second,
		goos:            runtime.GOOS,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Events returns the host's event bus (may be nil).
func (h *Host) Events() *eventbus.Bus[Event] { return h.events }

func (h *Host) publish(ctx context.Context, ev Event) {
	if h.events == nil {
		return
	}
	if ev.At.IsZero() {
		ev.At = time.Now()
	}
	h.events.Publish(ctx, ev)
}

// Publish emits an event on the host bus. Exposed so specialized hosts
// (protocol, credential) can publish plugin-level events.
func (h *Host) Publish(ctx context.Context, ev Event) { h.publish(ctx, ev) }

// Register validates the manifest and, if acceptable, adds the plugin to the
// host. If the module implements sdkplugin.Lifecycle, Init is invoked with a
// timeout; a failing Init aborts registration and publishes EventCrashed.
func (h *Host) Register(ctx context.Context, m sdkplugin.Manifest, module any, trust sdkplugin.Trust) error {
	if err := m.Validate(); err != nil {
		return fmt.Errorf("invalid manifest: %w", err)
	}
	if err := checkAPIMajor(m.APIVersion, sdkplugin.CurrentAPIVersion); err != nil {
		return err
	}
	if err := checkPlatform(h.goos, m.Platforms); err != nil {
		return err
	}

	switch trust {
	case sdkplugin.TrustCommunity, sdkplugin.TrustUntrusted:
		if h.approval == nil {
			return fmt.Errorf("%w: plugin %q trust=%s", ErrApprovalRequired, m.ID, trust)
		}
		if err := h.approval(ctx, m, trust); err != nil {
			return fmt.Errorf("approval rejected for plugin %q: %w", m.ID, err)
		}
	}

	h.mu.Lock()
	if _, dup := h.plugins[m.ID]; dup {
		h.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrAlreadyRegistered, m.ID)
	}
	// Reserve the ID to avoid duplicate Init races; store after Init succeeds.
	h.mu.Unlock()

	m.Trust = trust

	if lc, ok := module.(sdkplugin.Lifecycle); ok {
		ictx, cancel := context.WithTimeout(ctx, h.initTimeout)
		err := safeCall(func() error { return lc.Init(ictx) })
		cancel()
		if err != nil {
			h.publish(ctx, Event{Kind: EventCrashed, PluginID: m.ID, Err: err, At: time.Now()})
			return fmt.Errorf("plugin %q init failed: %w", m.ID, err)
		}
	}

	h.mu.Lock()
	if _, dup := h.plugins[m.ID]; dup {
		h.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrAlreadyRegistered, m.ID)
	}
	h.plugins[m.ID] = &Loaded{
		Manifest: m,
		Module:   module,
		LoadedAt: time.Now(),
		Trust:    trust,
	}
	h.mu.Unlock()

	h.publish(ctx, Event{Kind: EventLoaded, PluginID: m.ID, At: time.Now()})
	return nil
}

// Unregister removes a plugin, calling Shutdown if supported.
func (h *Host) Unregister(ctx context.Context, id string) error {
	h.mu.Lock()
	loaded, ok := h.plugins[id]
	if !ok {
		h.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrNotRegistered, id)
	}
	delete(h.plugins, id)
	h.mu.Unlock()

	if lc, ok := loaded.Module.(sdkplugin.Lifecycle); ok {
		sctx, cancel := context.WithTimeout(ctx, h.shutdownTimeout)
		err := safeCall(func() error { return lc.Shutdown(sctx) })
		cancel()
		if err != nil {
			h.publish(ctx, Event{Kind: EventCrashed, PluginID: id, Err: err, At: time.Now()})
			// Shutdown errors do not re-register the plugin.
			return fmt.Errorf("plugin %q shutdown failed: %w", id, err)
		}
	}

	h.publish(ctx, Event{Kind: EventUnloaded, PluginID: id, At: time.Now()})
	return nil
}

// Get returns a loaded plugin handle by ID.
func (h *Host) Get(id string) (*Loaded, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	l, ok := h.plugins[id]
	return l, ok
}

// List returns a snapshot of all loaded plugins.
func (h *Host) List() []*Loaded {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]*Loaded, 0, len(h.plugins))
	for _, l := range h.plugins {
		out = append(out, l)
	}
	return out
}

// EnforceCapability returns nil iff the named plugin declares the capability
// in its manifest.
func (h *Host) EnforceCapability(id string, cap sdkplugin.Capability) error {
	l, ok := h.Get(id)
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotRegistered, id)
	}
	if !l.Manifest.HasCapability(cap) {
		return fmt.Errorf("%w: plugin=%s capability=%s", ErrCapabilityNotGranted, id, cap)
	}
	return nil
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

// checkAPIMajor parses the leading numeric component of semver strings and
// returns an error if the plugin's major does not match the host's.
func checkAPIMajor(pluginVer, hostVer string) error {
	pm, err := majorOf(pluginVer)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrAPIVersionMismatch, err)
	}
	hm, err := majorOf(hostVer)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrAPIVersionMismatch, err)
	}
	if pm != hm {
		return fmt.Errorf("%w: plugin=%s host=%s", ErrAPIVersionMismatch, pluginVer, hostVer)
	}
	return nil
}

func majorOf(v string) (int, error) {
	s := strings.TrimPrefix(v, "v")
	if i := strings.IndexByte(s, '.'); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return 0, fmt.Errorf("empty version")
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid version %q", v)
	}
	return n, nil
}

func checkPlatform(goos string, supported []string) error {
	if len(supported) == 0 {
		return nil
	}
	for _, p := range supported {
		if p == goos {
			return nil
		}
	}
	return fmt.Errorf("%w: goos=%s supported=%v", ErrPlatformUnsupported, goos, supported)
}
