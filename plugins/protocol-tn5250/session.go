package tn5250

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/darkace1998/GoRemote/sdk/protocol"
)

// Session is a live TN5250 session backed by an in-process TCP connection.
//
// The session dials host:port on Start and relays data bidirectionally
// between the caller's stdin/stdout and the remote TCP stream. The 5250
// data stream is carried over this connection; no external binary is spawned.
type Session struct {
	addr string

	mu        sync.Mutex
	conn      net.Conn
	closeOnce sync.Once
	closeErr  error
}

// Compile-time assertion: *Session implements protocol.Session.
var _ protocol.Session = (*Session)(nil)

func newSession(addr string) *Session {
	return &Session{addr: addr}
}

// RenderMode reports the terminal rendering mode used by TN5250 sessions.
func (s *Session) RenderMode() protocol.RenderMode { return protocol.RenderTerminal }

// Start dials the remote TN5250 endpoint and runs the bidirectional I/O loop.
// It blocks until the remote closes, ctx is cancelled, or Close is called.
func (s *Session) Start(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	if stdout == nil {
		stdout = io.Discard
	}

	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", s.addr)
	if err != nil {
		return fmt.Errorf("tn5250: dial %s: %w", s.addr, err)
	}

	s.mu.Lock()
	s.conn = conn
	s.mu.Unlock()

	fromServer := make(chan error, 1)
	go func() {
		_, err := io.Copy(stdout, conn)
		fromServer <- err
	}()

	fromStdin := make(chan struct{}, 1)
	go func() {
		if stdin != nil {
			_, _ = io.Copy(conn, stdin)
		}
		// Signal EOF to the remote without dropping our receive path.
		if tc, ok := conn.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
		close(fromStdin)
	}()

	var result error
	select {
	case result = <-fromServer:
		_ = s.Close()
		<-fromStdin
	case <-ctx.Done():
		_ = s.Close()
		<-fromServer
		return nil
	}

	if result != nil && ctx.Err() != nil {
		return nil
	}
	return result
}

// Resize is not yet wired to a 5250 resize sequence.
func (s *Session) Resize(ctx context.Context, size protocol.Size) error {
	return protocol.ErrUnsupported
}

// SendInput writes data directly to the remote TCP stream.
func (s *Session) SendInput(ctx context.Context, data []byte) error {
	s.mu.Lock()
	conn := s.conn
	s.mu.Unlock()
	if conn == nil {
		return fmt.Errorf("tn5250: session not started")
	}
	_, err := conn.Write(data)
	return err
}

// Close terminates the TCP connection. Safe to call multiple times.
func (s *Session) Close() error {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		conn := s.conn
		s.mu.Unlock()
		if conn != nil {
			s.closeErr = conn.Close()
		}
	})
	return s.closeErr
}

