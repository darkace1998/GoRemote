// Package fakeprotocol provides an in-process protocol.Module implementation
// used by goremote's end-to-end integration tests. It records every method
// invocation into a thread-safe Recorder so tests can assert wiring without
// relying on real network sockets.
package fakeprotocol

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"

	"github.com/darkace1998/GoRemote/sdk/plugin"
	"github.com/darkace1998/GoRemote/sdk/protocol"
)

// ManifestID is the static manifest ID published by the fake protocol.
const ManifestID = "io.goremote.test.fake-protocol"

// Recorder collects every observable interaction with the fake protocol.
// All methods are safe for concurrent use.
type Recorder struct {
	mu      sync.Mutex
	opens   []protocol.OpenRequest
	inputs  [][]byte
	resizes []protocol.Size
	closes  int
	starts  int
}

// Opens returns a snapshot of every OpenRequest seen by the fake.
func (r *Recorder) Opens() []protocol.OpenRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]protocol.OpenRequest, len(r.opens))
	copy(out, r.opens)
	return out
}

// Inputs returns a snapshot of every SendInput payload (deep-copied).
func (r *Recorder) Inputs() [][]byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([][]byte, len(r.inputs))
	for i, b := range r.inputs {
		c := make([]byte, len(b))
		copy(c, b)
		out[i] = c
	}
	return out
}

// LastResize returns the most recent (cols,rows) recorded by Resize and
// whether any resize has been observed.
func (r *Recorder) LastResize() (protocol.Size, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.resizes) == 0 {
		return protocol.Size{}, false
	}
	return r.resizes[len(r.resizes)-1], true
}

// Closes returns the number of times Close has been observed across every
// fake session produced by this Recorder.
func (r *Recorder) Closes() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closes
}

// Starts returns the number of Start invocations observed.
func (r *Recorder) Starts() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.starts
}

func (r *Recorder) addOpen(req protocol.OpenRequest) {
	r.mu.Lock()
	r.opens = append(r.opens, req)
	r.mu.Unlock()
}

func (r *Recorder) addInput(b []byte) {
	c := make([]byte, len(b))
	copy(c, b)
	r.mu.Lock()
	r.inputs = append(r.inputs, c)
	r.mu.Unlock()
}

func (r *Recorder) addResize(s protocol.Size) {
	r.mu.Lock()
	r.resizes = append(r.resizes, s)
	r.mu.Unlock()
}

func (r *Recorder) incClose() {
	r.mu.Lock()
	r.closes++
	r.mu.Unlock()
}

func (r *Recorder) incStart() {
	r.mu.Lock()
	r.starts++
	r.mu.Unlock()
}

// Option configures a Module.
type Option func(*Module)

// WithOpenError makes Module.Open return the supplied error every time it is
// called. Useful for testing protocol error propagation.
func WithOpenError(err error) Option {
	return func(m *Module) { m.openErr = err }
}

// Module is the fake protocol.Module implementation.
type Module struct {
	rec     *Recorder
	openErr error
}

// New returns a fresh Module with its own Recorder.
func New(opts ...Option) *Module {
	m := &Module{rec: &Recorder{}}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// Recorder returns the Recorder that backs this module.
func (m *Module) Recorder() *Recorder { return m.rec }

// Manifest implements protocol.Module.
func (m *Module) Manifest() plugin.Manifest {
	return plugin.Manifest{
		ID:          ManifestID,
		Name:        "Fake Protocol (test)",
		Description: "In-process protocol module used by integration tests.",
		Kind:        plugin.KindProtocol,
		Version:     "0.0.1",
		APIVersion:  protocol.CurrentAPIVersion,
		Capabilities: []plugin.Capability{
			plugin.CapTerminal,
		},
		Status:    plugin.StatusExperimental,
		Publisher: "goremote-tests",
	}
}

// Settings implements protocol.Module. The fake exposes no settings.
func (m *Module) Settings() []protocol.SettingDef { return nil }

// Capabilities implements protocol.Module.
func (m *Module) Capabilities() protocol.Capabilities {
	return protocol.Capabilities{
		RenderModes:    []protocol.RenderMode{protocol.RenderTerminal},
		AuthMethods:    []protocol.AuthMethod{protocol.AuthPassword},
		SupportsResize: true,
	}
}

// Open implements protocol.Module.
func (m *Module) Open(ctx context.Context, req protocol.OpenRequest) (protocol.Session, error) {
	m.rec.addOpen(req)
	if m.openErr != nil {
		return nil, m.openErr
	}
	return &fakeSession{
		rec:  m.rec,
		out:  make(chan []byte, 32),
		done: make(chan struct{}),
	}, nil
}

// fakeSession implements protocol.Session.
type fakeSession struct {
	rec       *Recorder
	out       chan []byte
	done      chan struct{}
	closeOnce sync.Once
	closed    atomic.Bool
}

func (s *fakeSession) RenderMode() protocol.RenderMode { return protocol.RenderTerminal }

// Start blocks until the session is closed (Close, ctx cancellation, or
// stdout failure), copying SendInput-produced echoes onto stdout.
func (s *fakeSession) Start(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	s.rec.incStart()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.done:
			return nil
		case b := <-s.out:
			if stdout == nil {
				continue
			}
			if _, err := stdout.Write(b); err != nil {
				return err
			}
		}
	}
}

// SendInput records the input and produces an `> <input>\n` echo on stdout.
func (s *fakeSession) SendInput(ctx context.Context, data []byte) error {
	if s.closed.Load() {
		return errors.New("fakeprotocol: session closed")
	}
	s.rec.addInput(data)
	echo, err := buildEchoLine(data)
	if err != nil {
		return err
	}
	select {
	case s.out <- echo:
	case <-s.done:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

func buildEchoLine(data []byte) ([]byte, error) {
	const maxInt = int(^uint(0) >> 1)
	if len(data) > maxInt-3 {
		return nil, errors.New("fakeprotocol: input too large")
	}
	echo := make([]byte, 0, len(data)+3)
	echo = append(echo, '>', ' ')
	echo = append(echo, data...)
	echo = append(echo, '\n')
	return echo, nil
}

// Resize records the new dimensions.
func (s *fakeSession) Resize(ctx context.Context, size protocol.Size) error {
	if s.closed.Load() {
		return errors.New("fakeprotocol: session closed")
	}
	s.rec.addResize(size)
	return nil
}

// Close is idempotent: only the first call records and unblocks Start.
func (s *fakeSession) Close() error {
	s.closeOnce.Do(func() {
		s.closed.Store(true)
		s.rec.incClose()
		close(s.done)
	})
	return nil
}
