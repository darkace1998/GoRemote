package rawsocket

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"sync"

	"github.com/darkace1998/GoRemote/sdk/protocol"
)

// Session is a live Raw Socket session. It is safe for the caller to invoke
// Start, Resize, SendInput, and Close concurrently.
type Session struct {
	conn    net.Conn
	eolMode string

	closeOnce sync.Once
	closeErr  error
}

func newSession(conn net.Conn, eolMode string) *Session {
	return &Session{conn: conn, eolMode: eolMode}
}

// RenderMode reports the rendering mode negotiated at Open time. Raw sockets
// always render through the host terminal.
func (s *Session) RenderMode() protocol.RenderMode { return protocol.RenderTerminal }

// Start runs the bidirectional byte pump between the host-supplied stdin /
// stdout pipes and the underlying TCP connection. It blocks until either
// direction finishes (EOF or error) or ctx is cancelled, then closes the
// connection and returns.
//
// Start returns nil when the session ended via cancellation or a clean EOF
// from either side, and the first non-EOF / non-cancellation error otherwise.
func (s *Session) Start(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	// Ensure ctx cancellation unblocks in-flight I/O by closing the socket.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	stopCtx := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = s.conn.Close()
		case <-stopCtx:
		}
	}()
	defer close(stopCtx)

	type result struct {
		who string
		err error
	}
	// Only spin up copiers for the sides the caller actually wired up, so
	// SendInput-driven sessions (stdin == nil) don't self-terminate.
	var pending int
	results := make(chan result, 2)

	if stdout != nil {
		pending++
		go func() {
			_, err := io.Copy(stdout, s.conn)
			results <- result{who: "remote->stdout", err: err}
		}()
	}
	if stdin != nil {
		pending++
		go func() {
			_, err := io.Copy(s.conn, stdin)
			// Signal EOF to the remote so it can drain and close.
			if tcp, ok := s.conn.(*net.TCPConn); ok {
				_ = tcp.CloseWrite()
			}
			results <- result{who: "stdin->remote", err: err}
		}()
	}

	// If neither direction was wired up, just block on ctx.
	if pending == 0 {
		<-ctx.Done()
		_ = s.Close()
		return nil
	}

	// Wait for the first side to finish, then tear down and drain the other.
	first := <-results
	_ = s.Close()
	gathered := []result{first}
	for i := 1; i < pending; i++ {
		gathered = append(gathered, <-results)
	}

	for _, r := range gathered {
		if r.err == nil {
			continue
		}
		if errors.Is(r.err, io.EOF) || errors.Is(r.err, context.Canceled) || errors.Is(r.err, context.DeadlineExceeded) {
			continue
		}
		// A close initiated by ctx cancellation surfaces as "use of closed
		// network connection"; treat that as a clean shutdown.
		if errors.Is(r.err, net.ErrClosed) {
			continue
		}
		if ctx.Err() != nil {
			continue
		}
		return r.err
	}
	return nil
}

// Resize is a no-op. Raw TCP sockets have no concept of a window size. We
// intentionally return nil rather than [protocol.ErrNotSupported] so UI
// resize events do not surface errors to users.
func (s *Session) Resize(ctx context.Context, size protocol.Size) error {
	return nil
}

// SendInput writes data to the remote. If the configured EOL mode is not
// "none" and data does not already end with a newline ("\n" for lf, "\r\n"
// for crlf), the EOL is appended. SendInput never double-appends: data that
// already ends with the configured EOL (or plain "\n") is sent verbatim.
func (s *Session) SendInput(ctx context.Context, data []byte) error {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}

	payload := data
	switch s.eolMode {
	case EOLModeLF:
		if !bytes.HasSuffix(data, []byte("\n")) {
			payload = append(append([]byte(nil), data...), '\n')
		}
	case EOLModeCRLF:
		if !bytes.HasSuffix(data, []byte("\r\n")) {
			// If it ends with a bare LF, upgrade to CRLF; otherwise append CRLF.
			if bytes.HasSuffix(data, []byte("\n")) {
				payload = append(append([]byte(nil), data[:len(data)-1]...), '\r', '\n')
			} else {
				payload = append(append([]byte(nil), data...), '\r', '\n')
			}
		}
	case EOLModeNone:
		// Send verbatim.
	}

	_, err := s.conn.Write(payload)
	return err
}

// Close terminates the session. It is safe and idempotent across concurrent
// callers: only the first invocation closes the underlying connection; later
// calls return the same result.
func (s *Session) Close() error {
	s.closeOnce.Do(func() {
		s.closeErr = s.conn.Close()
	})
	return s.closeErr
}
