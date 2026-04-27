package ssh

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/goremote/goremote/sdk/protocol"

	"golang.org/x/crypto/ssh"
)

// Session is a live SSH shell session returned by [Module.Open].
//
// Session implements [protocol.Session] and is safe for concurrent use:
// Start, Resize, SendInput, and Close may be called from different
// goroutines.
type Session struct {
	client  *ssh.Client
	session *ssh.Session

	stdin  io.WriteCloser
	stdout io.Reader
	stderr io.Reader

	keepalive time.Duration
	logger    *slog.Logger

	agentCloser sessionCloser
	hkCloser    sessionCloser

	mu      sync.Mutex
	started bool

	closeOnce sync.Once
	closeErr  error
	stopCh    chan struct{}
}

// RenderMode reports that this session is rendered as a terminal.
func (s *Session) RenderMode() protocol.RenderMode { return protocol.RenderTerminal }

// Start runs the bidirectional I/O loop for the session. Remote stdout and
// stderr are copied to stdout; stdin (if non-nil) is copied to the remote
// shell. Start blocks until the remote shell exits, ctx is cancelled, or
// Close is invoked, whichever happens first. Start returns nil on a clean
// exit and the first non-EOF error otherwise.
func (s *Session) Start(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return errors.New("ssh: session already started")
	}
	s.started = true
	s.mu.Unlock()

	if stdout == nil {
		return errors.New("ssh: stdout writer is nil")
	}

	// Serialize writes from the two remote→local copy goroutines so that
	// callers may pass a non-thread-safe writer (e.g. *bytes.Buffer).
	sw := &syncWriter{w: stdout}

	copyErrs := make(chan error, 3)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, err := io.Copy(sw, s.stdout)
		copyErrs <- err
	}()
	go func() {
		defer wg.Done()
		_, err := io.Copy(sw, s.stderr)
		copyErrs <- err
	}()

	if stdin != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := io.Copy(s.stdin, stdin)
			// Half-close remote stdin so the peer sees EOF.
			_ = s.stdin.Close()
			copyErrs <- err
		}()
	}

	waitCh := make(chan error, 1)
	go func() { waitCh <- s.session.Wait() }()

	var runErr error
	select {
	case <-ctx.Done():
		runErr = ctx.Err()
	case err := <-waitCh:
		if err != nil && !isCleanExit(err) {
			runErr = err
		}
	case <-s.stopCh:
		// Close was called externally; treat as a clean shutdown.
	}

	_ = s.Close()
	wg.Wait()

	// Drain remaining copy errors; surface the first real one if Wait was clean.
	close(copyErrs)
	if runErr == nil {
		for err := range copyErrs {
			if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe) {
				runErr = err
				break
			}
		}
	}
	return runErr
}

// Resize forwards a window-size change to the remote PTY.
func (s *Session) Resize(ctx context.Context, size protocol.Size) error {
	if size.Cols <= 0 || size.Rows <= 0 {
		return errors.New("ssh: resize requires positive cols and rows")
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	return s.session.WindowChange(size.Rows, size.Cols)
}

// SendInput writes data to the remote shell's stdin. It is safe to call
// concurrently with Start.
func (s *Session) SendInput(ctx context.Context, data []byte) error {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	_, err := s.stdin.Write(data)
	return err
}

// Close terminates the session and releases all associated resources. It is
// idempotent and safe to call from any goroutine.
func (s *Session) Close() error {
	s.closeOnce.Do(func() {
		close(s.stopCh)
		if s.session != nil {
			_ = s.session.Close()
		}
		if s.client != nil {
			s.closeErr = s.client.Close()
		}
		if s.agentCloser != nil {
			_ = s.agentCloser.Close()
		}
		if s.hkCloser != nil {
			_ = s.hkCloser.Close()
		}
		if s.logger != nil {
			s.logger.Info("ssh session closed")
		}
	})
	return s.closeErr
}

// startKeepalive launches a background goroutine that periodically sends
// an SSH "keepalive@openssh.com" request. The goroutine exits when the
// session is closed.
func (s *Session) startKeepalive() {
	if s.keepalive <= 0 {
		return
	}
	ticker := time.NewTicker(s.keepalive)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-s.stopCh:
				return
			case <-ticker.C:
				_, _, err := s.client.SendRequest("keepalive@openssh.com", true, nil)
				if err != nil {
					if s.logger != nil {
						s.logger.Debug("keepalive failed", slog.String("err", err.Error()))
					}
					return
				}
			}
		}
	}()
}

// syncWriter serializes writes to an underlying writer. Used to multiplex
// remote stdout and stderr onto a single caller-supplied io.Writer.
type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

// isCleanExit reports whether an ssh.Session.Wait error should be treated as
// a successful termination for our purposes.
func isCleanExit(err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, io.EOF) {
		return true
	}
	var missing *ssh.ExitMissingError
	return errors.As(err, &missing)
}
