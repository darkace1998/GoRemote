package serial

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"

	goserial "go.bug.st/serial"

	"github.com/goremote/goremote/sdk/protocol"
)

// Session is a live Serial session. It is safe for the caller to invoke
// Start, Resize, SendInput, and Close concurrently.
type Session struct {
	port    goserial.Port
	eolMode string

	closeOnce sync.Once
	closeErr  error

	closedMu sync.Mutex
	closed   bool
}

func newSession(port goserial.Port, eolMode string) *Session {
	return &Session{port: port, eolMode: eolMode}
}

// RenderMode reports the rendering mode. Serial sessions always render
// through the host terminal.
func (s *Session) RenderMode() protocol.RenderMode { return protocol.RenderTerminal }

// Start runs the bidirectional byte pump between the host-supplied
// stdin / stdout pipes and the serial port. It blocks until either
// direction finishes (EOF or error) or ctx is cancelled, then closes the
// port and returns.
//
// Returns nil for clean shutdowns (cancellation, EOF, port closed by
// Close); any other I/O error is surfaced.
func (s *Session) Start(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	stop := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = s.Close()
		case <-stop:
		}
	}()
	defer close(stop)

	type result struct {
		who string
		err error
	}
	var pending int
	results := make(chan result, 2)

	if stdout != nil {
		pending++
		go func() {
			_, err := io.Copy(stdout, s.port)
			results <- result{who: "port->stdout", err: err}
		}()
	}
	if stdin != nil {
		pending++
		go func() {
			_, err := io.Copy(s.port, stdin)
			results <- result{who: "stdin->port", err: err}
		}()
	}

	if pending == 0 {
		<-ctx.Done()
		_ = s.Close()
		return nil
	}

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
		// Closing a serial port surfaces as a port-error or generic
		// "file already closed"; treat anything seen after our own
		// Close() as a clean shutdown.
		s.closedMu.Lock()
		closed := s.closed
		s.closedMu.Unlock()
		if closed {
			continue
		}
		if ctx.Err() != nil {
			continue
		}
		return r.err
	}
	return nil
}

// Resize is a no-op. Serial ports have no concept of a window size.
// Returning nil (rather than ErrNotSupported) keeps UI resize events
// silent for the user.
func (s *Session) Resize(ctx context.Context, size protocol.Size) error {
	return nil
}

// SendInput writes data to the port. Honors the configured EOL mode the
// same way the rawsocket plugin does.
func (s *Session) SendInput(ctx context.Context, data []byte) error {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	s.closedMu.Lock()
	closed := s.closed
	s.closedMu.Unlock()
	if closed {
		return errors.New("serial: send-input on closed session")
	}

	payload := data
	switch s.eolMode {
	case EOLModeLF:
		if !bytes.HasSuffix(data, []byte("\n")) {
			payload = append(append([]byte(nil), data...), '\n')
		}
	case EOLModeCR:
		if !bytes.HasSuffix(data, []byte("\r")) {
			if bytes.HasSuffix(data, []byte("\n")) {
				payload = append(append([]byte(nil), data[:len(data)-1]...), '\r')
			} else {
				payload = append(append([]byte(nil), data...), '\r')
			}
		}
	case EOLModeCRLF:
		if !bytes.HasSuffix(data, []byte("\r\n")) {
			if bytes.HasSuffix(data, []byte("\n")) {
				payload = append(append([]byte(nil), data[:len(data)-1]...), '\r', '\n')
			} else {
				payload = append(append([]byte(nil), data...), '\r', '\n')
			}
		}
	case EOLModeNone:
	}

	_, err := s.port.Write(payload)
	return err
}

// Close terminates the session. Idempotent under concurrent callers.
func (s *Session) Close() error {
	s.closeOnce.Do(func() {
		s.closedMu.Lock()
		s.closed = true
		s.closedMu.Unlock()
		s.closeErr = s.port.Close()
	})
	return s.closeErr
}
