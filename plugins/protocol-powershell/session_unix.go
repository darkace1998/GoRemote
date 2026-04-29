//go:build !windows

package powershell

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"github.com/creack/pty"

	"github.com/darkace1998/GoRemote/sdk/protocol"
)

// Session is a live PowerShell PTY session. It is safe for the caller to
// invoke Start, Resize, SendInput, and Close concurrently.
type Session struct {
	cmd  *exec.Cmd
	ptmx *os.File

	cols, rows int

	mu        sync.Mutex
	closed    bool
	closeOnce sync.Once
	closeErr  error
}

// openSession resolves the binary, builds the command, sizes the PTY, and
// spawns the child. It is the unix implementation of the openSession hook
// declared in module.go.
func openSession(ctx context.Context, cfg openConfig) (*Session, error) {
	bin, err := discoverBinary(cfg.binary)
	if err != nil {
		return nil, fmt.Errorf("powershell: %w", err)
	}

	args := append([]string{"-NoLogo", "-NoProfile", "-Interactive"}, cfg.args...)
	// #nosec G204 -- bin is resolved by discoverBinary and args are passed directly without a shell.
	cmd := exec.CommandContext(ctx, bin, args...)
	if cfg.cwd != "" {
		cmd.Dir = cfg.cwd
	}
	if len(cfg.env) > 0 {
		env := os.Environ()
		for k, v := range cfg.env {
			env = append(env, k+"="+v)
		}
		cmd.Env = env
	}

	// #nosec G115 -- resolveConfig bounds cols and rows to uint16 before openSession is called.
	ws := &pty.Winsize{Cols: uint16(cfg.cols), Rows: uint16(cfg.rows)}
	ptmx, err := pty.StartWithSize(cmd, ws)
	if err != nil {
		return nil, fmt.Errorf("powershell: pty start %s: %w", bin, err)
	}

	return &Session{
		cmd:  cmd,
		ptmx: ptmx,
		cols: cfg.cols,
		rows: cfg.rows,
	}, nil
}

// RenderMode reports the rendering mode negotiated at Open time. PowerShell
// always renders through the host terminal.
func (s *Session) RenderMode() protocol.RenderMode { return protocol.RenderTerminal }

// Start runs the bidirectional byte pump between the host-supplied stdin /
// stdout pipes and the PTY master. It blocks until the child exits or ctx
// is cancelled, then tears the session down and returns.
//
// On ctx cancellation the child process is killed with SIGKILL so Start
// returns promptly even if the child is ignoring termination signals.
func (s *Session) Start(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	if stdout != nil {
		go func() {
			// io.Copy returns when the PTY master is closed (which happens
			// when the child exits or Close is called).
			_, _ = io.Copy(stdout, s.ptmx)
		}()
	}
	if stdin != nil {
		go func() {
			_, _ = io.Copy(s.ptmx, stdin)
		}()
	}

	waitErr := make(chan error, 1)
	go func() { waitErr <- s.cmd.Wait() }()

	select {
	case err := <-waitErr:
		_ = s.Close()
		if err == nil {
			return nil
		}
		// A killed child surfaces via *exec.ExitError; map signals we
		// triggered ourselves (Close / ctx cancel) to a clean shutdown.
		if ctx.Err() != nil {
			return nil
		}
		s.mu.Lock()
		closed := s.closed
		s.mu.Unlock()
		if closed {
			return nil
		}
		return err
	case <-ctx.Done():
		// Best-effort kill; the wait goroutine will then complete.
		s.killProcess()
		<-waitErr
		_ = s.Close()
		return nil
	}
}

// Resize requests a window-size change on the PTY.
func (s *Session) Resize(ctx context.Context, size protocol.Size) error {
	if size.Cols <= 0 || size.Rows <= 0 {
		return fmt.Errorf("powershell: resize: cols/rows must be positive (got cols=%d rows=%d)", size.Cols, size.Rows)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("powershell: resize on closed session")
	}
	return pty.Setsize(s.ptmx, &pty.Winsize{Cols: uint16(size.Cols), Rows: uint16(size.Rows)})
}

// SendInput writes data verbatim to the PTY master. PowerShell receives it
// the same way it would receive keystrokes from a real terminal.
func (s *Session) SendInput(ctx context.Context, data []byte) error {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return errors.New("powershell: send-input on closed session")
	}
	_, err := s.ptmx.Write(data)
	return err
}

// Close terminates the session. It is safe and idempotent across concurrent
// callers: only the first invocation kills the child and closes the PTY;
// later calls return the same result.
func (s *Session) Close() error {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()

		s.killProcess()
		if s.ptmx != nil {
			if err := s.ptmx.Close(); err != nil {
				s.closeErr = err
			}
		}
	})
	return s.closeErr
}

// killProcess sends SIGKILL to the child if it is still alive. It is safe
// to call multiple times and after the process has exited.
func (s *Session) killProcess() {
	if s.cmd == nil || s.cmd.Process == nil {
		return
	}
	// Signal(0) is a portable "is it alive?" probe.
	if err := s.cmd.Process.Signal(syscall.Signal(0)); err != nil {
		return
	}
	_ = s.cmd.Process.Kill()
}
