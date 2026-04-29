package telnet

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"

	"github.com/darkace1998/GoRemote/sdk/protocol"
)

// Session is a live Telnet session. It is created by Module.Open and driven
// by its Start method. All methods are safe for concurrent use.
type Session struct {
	neg *Negotiator

	auth     protocol.AuthMethod
	username string
	password string

	closeOnce sync.Once
	closeErr  error
}

// Compile-time assertion: *Session implements protocol.Session.
var _ protocol.Session = (*Session)(nil)

// RenderMode always returns RenderTerminal for Telnet sessions.
func (s *Session) RenderMode() protocol.RenderMode { return protocol.RenderTerminal }

// Start runs the Telnet I/O loop until the remote closes, ctx is cancelled,
// or Close is called. When AuthPassword is configured, Start first performs
// a plaintext "login:" / "password:" expect handshake before handing the
// session over to the copy loops.
//
// INSECURE: the AuthPassword handshake transmits the credentials in the clear.
func (s *Session) Start(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	if stdout == nil {
		stdout = io.Discard
	}

	if s.auth == protocol.AuthPassword {
		if err := s.doPasswordLogin(ctx, stdout); err != nil {
			return err
		}
	}

	// stdin -> remote
	errCh := make(chan error, 2)
	go func() {
		if stdin == nil {
			errCh <- nil
			return
		}
		_, err := io.Copy(s.neg, stdin)
		errCh <- err
	}()
	// remote -> stdout
	go func() {
		_, err := io.Copy(stdout, s.neg)
		errCh <- err
	}()

	// Watch ctx cancellation in parallel so we can tear down the conn.
	ctxDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = s.Close()
		case <-ctxDone:
		}
	}()

	// Wait for first goroutine to finish, then close so the other unwinds.
	first := <-errCh
	_ = s.Close()
	second := <-errCh
	close(ctxDone)

	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	for _, e := range [2]error{first, second} {
		if e != nil && !isBenignCloseErr(e) {
			return e
		}
	}
	return nil
}

// Resize sends a NAWS subnegotiation announcing the new window size.
func (s *Session) Resize(ctx context.Context, size protocol.Size) error {
	if size.Cols <= 0 || size.Rows <= 0 {
		return fmt.Errorf("telnet: invalid resize %dx%d", size.Cols, size.Rows)
	}
	return s.neg.SendNAWS(size.Cols, size.Rows)
}

// SendInput writes data to the remote. 0xFF bytes are IAC-escaped for the
// caller; callers should pass unescaped application bytes.
func (s *Session) SendInput(ctx context.Context, data []byte) error {
	_, err := s.neg.Write(data)
	return err
}

// Close tears down the underlying connection. Safe to call repeatedly.
func (s *Session) Close() error {
	s.closeOnce.Do(func() {
		s.closeErr = s.neg.Close()
	})
	return s.closeErr
}

// doPasswordLogin scans the server preamble for "login:" and "password:"
// prompts (case-insensitive substring match on the tail of received data)
// and writes the configured username/password terminated with "\r\n". Bytes
// scanned during this phase are still forwarded to stdout so the user sees
// the banner.
func (s *Session) doPasswordLogin(ctx context.Context, stdout io.Writer) error {
	const maxBanner = 8192
	if err := expectPrompt(ctx, s.neg, stdout, "login:", maxBanner); err != nil {
		return fmt.Errorf("telnet: waiting for login prompt: %w", err)
	}
	if _, err := s.neg.Write([]byte(s.username + "\r\n")); err != nil {
		return fmt.Errorf("telnet: sending username: %w", err)
	}
	if err := expectPrompt(ctx, s.neg, stdout, "password:", maxBanner); err != nil {
		return fmt.Errorf("telnet: waiting for password prompt: %w", err)
	}
	if _, err := s.neg.Write([]byte(s.password + "\r\n")); err != nil {
		return fmt.Errorf("telnet: sending password: %w", err)
	}
	return nil
}

// expectPrompt reads from r one byte at a time, mirroring every byte to w,
// and returns nil when the lowercased accumulated tail contains needle.
// The accumulator is capped so memory use is bounded; overall scanning is
// capped at limit bytes total.
func expectPrompt(ctx context.Context, r io.Reader, w io.Writer, needle string, limit int) error {
	needle = strings.ToLower(needle)
	needleBytes := []byte(needle)
	// Keep the most recent window ~ 2*len(needle) bytes (at minimum 64).
	windowCap := 2 * len(needle)
	if windowCap < 64 {
		windowCap = 64
	}
	window := make([]byte, 0, windowCap+1)
	buf := make([]byte, 1)
	scanned := 0
	for scanned < limit {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := r.Read(buf)
		if n > 0 {
			scanned++
			if w != nil {
				if _, werr := w.Write(buf[:n]); werr != nil {
					return werr
				}
			}
			c := buf[0]
			// Lowercase ASCII in-place for matching.
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}
			if len(window) == windowCap {
				copy(window, window[1:])
				window = window[:windowCap-1]
			}
			window = append(window, c)
			if bytes.Contains(window, needleBytes) {
				return nil
			}
		}
		if err != nil {
			return err
		}
	}
	return fmt.Errorf("prompt %q not seen within %d bytes", needle, limit)
}

// isBenignCloseErr returns true for errors that indicate "the other side or
// we closed normally" and should not be surfaced from Start.
func isBenignCloseErr(err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, io.EOF) {
		return true
	}
	if errors.Is(err, net.ErrClosed) {
		return true
	}
	// io.Copy may wrap the underlying error in its own message; fall back
	// to substring match as a last resort for the common messages.
	s := err.Error()
	return strings.Contains(s, "use of closed network connection")
}
