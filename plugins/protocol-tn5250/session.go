package tn5250

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/darkace1998/GoRemote/internal/extlaunch"
	"github.com/darkace1998/GoRemote/sdk/protocol"
)

// Session is a single TN5250 launcher session. It owns one external client
// subprocess and supervises its lifetime under the caller-supplied context.
//
// Session is safe for concurrent invocation of Start, Resize, SendInput,
// and Close.
type Session struct {
	binary string
	args   []string

	mu     sync.Mutex
	cancel context.CancelFunc
	proc   *extlaunch.Process
	closed bool
}

func newSession(binary string, args []string) *Session {
	return &Session{
		binary: binary,
		args:   append([]string(nil), args...),
	}
}

// RenderMode reports the rendering mode negotiated at Open time. The native
// TN5250 client owns its own window/terminal, so the host always renders in
// external mode.
func (s *Session) RenderMode() protocol.RenderMode { return protocol.RenderExternal }

// Start spawns the external TN5250 client and blocks until it exits or ctx
// is cancelled. stdin/stdout from the host pipeline are intentionally not
// wired to the child: the native client takes over its own terminal /
// window. stderr is forwarded to stdout (when supplied) so users can see
// client diagnostics through the host log surface.
func (s *Session) Start(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("tn5250: session already closed")
	}
	if s.proc != nil {
		s.mu.Unlock()
		return fmt.Errorf("tn5250: session already started")
	}

	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	spec := extlaunch.Spec{
		Binary: s.binary,
		Args:   s.args,
		Stdout: stdout,
		Stderr: stdout,
	}

	proc, err := extlaunch.Start(runCtx, spec)
	if err != nil {
		cancel()
		s.cancel = nil
		s.mu.Unlock()
		return fmt.Errorf("tn5250: start: %w", err)
	}
	s.proc = proc
	s.mu.Unlock()

	return proc.Wait(runCtx)
}

// Resize is unsupported: the native client owns its window geometry.
func (s *Session) Resize(ctx context.Context, size protocol.Size) error {
	return protocol.ErrUnsupported
}

// SendInput is unsupported: the native client reads keyboard input from its
// own window directly.
func (s *Session) SendInput(ctx context.Context, data []byte) error {
	return protocol.ErrUnsupported
}

// Close terminates the session. Safe and idempotent across concurrent
// callers; only the first invocation cancels the supervising context, but
// every call returns nil.
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	return nil
}
