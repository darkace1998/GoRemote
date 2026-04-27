//go:build windows

package powershell

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"

	"github.com/goremote/goremote/sdk/protocol"
)

// Session is a live PowerShell session backed by stdin/stdout pipes.
//
// On Windows, creack/pty does not support the Windows Console API, so we use
// plain os/exec pipe I/O instead of a PTY. This means terminal control
// sequences (colour, bold, etc.) are limited to whatever PowerShell emits
// via ConEscapeSequence, and window-size changes are silently ignored.
// All other session semantics — including Send Input, Close idempotency,
// and context-based cancellation — are identical to the Unix implementation.
type Session struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser

	mu        sync.Mutex
	closed    bool
	closeOnce sync.Once
	closeErr  error
}

// openSession resolves the binary, builds the command, and wires up the stdin
// pipe. The child process is NOT started here; Start does that so that the
// caller-supplied stdout/stderr writers are available at launch time.
func openSession(_ context.Context, cfg openConfig) (*Session, error) {
	bin, err := discoverBinary(cfg.binary)
	if err != nil {
		return nil, fmt.Errorf("powershell: %w", err)
	}

	args := append([]string{"-NoLogo", "-NoProfile", "-NonInteractive"}, cfg.args...)
	cmd := exec.Command(bin, args...)
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

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("powershell: stdin pipe: %w", err)
	}

	return &Session{cmd: cmd, stdin: stdinPipe}, nil
}

// RenderMode reports the rendering mode. PowerShell sessions always use the
// host terminal renderer.
func (s *Session) RenderMode() protocol.RenderMode { return protocol.RenderTerminal }

// Start wires stdout/stderr to the writer, launches the child process, pumps
// stdin from the reader, and blocks until the child exits or ctx is cancelled.
//
// On ctx cancellation the child process is killed so Start returns promptly.
func (s *Session) Start(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	if stdout != nil {
		// Merge stdout and stderr so the caller sees all output in one stream.
		s.cmd.Stdout = stdout
		s.cmd.Stderr = stdout
	}

	if err := s.cmd.Start(); err != nil {
		return fmt.Errorf("powershell: start %s: %w", s.cmd.Path, err)
	}

	if stdin != nil {
		go func() {
			_, _ = io.Copy(s.stdin, stdin)
			_ = s.stdin.Close()
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
		s.killProcess()
		<-waitErr
		_ = s.Close()
		return nil
	}
}

// Resize is a no-op on Windows: there is no underlying PTY to deliver
// SIGWINCH to. Callers should not depend on resize on Windows.
func (s *Session) Resize(_ context.Context, _ protocol.Size) error { return nil }

// SendInput writes data to the child's stdin pipe.
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
	_, err := s.stdin.Write(data)
	return err
}

// Close terminates the session. It is safe and idempotent across concurrent
// callers: only the first invocation kills the child and closes the pipe.
func (s *Session) Close() error {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()

		_ = s.stdin.Close()
		s.killProcess()
	})
	return s.closeErr
}

// killProcess sends Kill to the child if it is still alive.
func (s *Session) killProcess() {
	if s.cmd == nil || s.cmd.Process == nil {
		return
	}
	_ = s.cmd.Process.Kill()
}
