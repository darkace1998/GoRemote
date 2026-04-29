// Package protocol implements the protocol-plugin host. It wraps host/plugin
// to provide a unified registry for built-in and (eventually) external
// protocol modules, and protects the application core from protocol panics
// by wrapping sessions in a panic-recovering shim.
package protocol

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	pluginhost "github.com/darkace1998/GoRemote/host/plugin"
	sdkplugin "github.com/darkace1998/GoRemote/sdk/plugin"
	"github.com/darkace1998/GoRemote/sdk/protocol"
)

// Errors returned by the protocol host.
var (
	ErrWrongKind        = errors.New("module manifest kind is not protocol")
	ErrProtocolNotFound = errors.New("protocol not found")
	ErrSessionCrashed   = errors.New("protocol session crashed")
)

// Host is the protocol-module host.
type Host struct {
	ph      *pluginhost.Host
	mu      sync.RWMutex
	modules map[string]protocol.Module
}

// New constructs a protocol Host backed by the given plugin host.
func New(ph *pluginhost.Host) *Host {
	return &Host{
		ph:      ph,
		modules: make(map[string]protocol.Module),
	}
}

// PluginHost returns the underlying generic plugin host.
func (h *Host) PluginHost() *pluginhost.Host { return h.ph }

// Register validates that m advertises a protocol manifest and delegates to
// the plugin host for lifecycle management.
func (h *Host) Register(ctx context.Context, m protocol.Module, trust sdkplugin.Trust) error {
	if m == nil {
		return errors.New("module is nil")
	}
	man := m.Manifest()
	if man.Kind != sdkplugin.KindProtocol {
		return fmt.Errorf("%w: plugin %q kind=%s", ErrWrongKind, man.ID, man.Kind)
	}
	if err := h.ph.Register(ctx, man, m, trust); err != nil {
		return err
	}
	h.mu.Lock()
	h.modules[man.ID] = m
	h.mu.Unlock()
	return nil
}

// Unregister removes the protocol module.
func (h *Host) Unregister(ctx context.Context, id string) error {
	h.mu.Lock()
	delete(h.modules, id)
	h.mu.Unlock()
	return h.ph.Unregister(ctx, id)
}

// Module returns the protocol module registered under id.
func (h *Host) Module(id string) (protocol.Module, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	m, ok := h.modules[id]
	return m, ok
}

// List returns all registered protocol modules.
func (h *Host) List() []protocol.Module {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]protocol.Module, 0, len(h.modules))
	for _, m := range h.modules {
		out = append(out, m)
	}
	return out
}

// Open looks up the named protocol module and asks it to open a session. The
// returned Session is wrapped so that panics in Start/Resize/SendInput/Close
// are caught and surfaced as errors, and the plugin is reported as crashed on
// the host event bus.
func (h *Host) Open(ctx context.Context, protocolID string, req protocol.OpenRequest) (protocol.Session, error) {
	h.mu.RLock()
	m, ok := h.modules[protocolID]
	h.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrProtocolNotFound, protocolID)
	}

	var sess protocol.Session
	err := safeCall(func() error {
		s, err := m.Open(ctx, req)
		if err != nil {
			return err
		}
		sess = s
		return nil
	})
	if err != nil {
		h.reportCrash(ctx, protocolID, err)
		return nil, fmt.Errorf("protocol %q open failed: %w", protocolID, err)
	}
	if sess == nil {
		return nil, fmt.Errorf("protocol %q returned nil session", protocolID)
	}
	return &safeSession{inner: sess, host: h, pluginID: protocolID}, nil
}

func (h *Host) reportCrash(ctx context.Context, pluginID string, err error) {
	if h.ph == nil {
		return
	}
	h.ph.Publish(ctx, pluginhost.Event{
		Kind:     pluginhost.EventCrashed,
		PluginID: pluginID,
		Err:      err,
		At:       time.Now(),
	})
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

// safeSession wraps a protocol.Session and recovers from panics raised by
// misbehaving plugins.
type safeSession struct {
	inner    protocol.Session
	host     *Host
	pluginID string
	closed   atomic.Bool
}

func (s *safeSession) RenderMode() protocol.RenderMode {
	var m protocol.RenderMode
	_ = safeCall(func() error {
		m = s.inner.RenderMode()
		return nil
	})
	return m
}

func (s *safeSession) Start(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	err := safeCall(func() error { return s.inner.Start(ctx, stdin, stdout) })
	if err != nil && isPanicErr(err) {
		s.host.reportCrash(ctx, s.pluginID, err)
		return fmt.Errorf("%w: %w", ErrSessionCrashed, err)
	}
	return err
}

func (s *safeSession) Resize(ctx context.Context, size protocol.Size) error {
	err := safeCall(func() error { return s.inner.Resize(ctx, size) })
	if err != nil && isPanicErr(err) {
		s.host.reportCrash(ctx, s.pluginID, err)
		return fmt.Errorf("%w: %w", ErrSessionCrashed, err)
	}
	return err
}

func (s *safeSession) SendInput(ctx context.Context, data []byte) error {
	err := safeCall(func() error { return s.inner.SendInput(ctx, data) })
	if err != nil && isPanicErr(err) {
		s.host.reportCrash(ctx, s.pluginID, err)
		return fmt.Errorf("%w: %w", ErrSessionCrashed, err)
	}
	return err
}

func (s *safeSession) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	err := safeCall(func() error { return s.inner.Close() })
	if err != nil && isPanicErr(err) {
		s.host.reportCrash(context.Background(), s.pluginID, err)
		return fmt.Errorf("%w: %w", ErrSessionCrashed, err)
	}
	return err
}

// isPanicErr reports whether err was produced by safeCall's recover path.
// We identify it by its prefix.
func isPanicErr(err error) bool {
	if err == nil {
		return false
	}
	const prefix = "panic: "
	s := err.Error()
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
