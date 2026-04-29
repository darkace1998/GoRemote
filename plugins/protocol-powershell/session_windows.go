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
	"sync/atomic"
	"syscall"

	"github.com/ActiveState/termtest/conpty"

	"github.com/darkace1998/GoRemote/sdk/protocol"
)

// Session is a live PowerShell session backed by the Windows ConPTY API
// (CreatePseudoConsole). Compared to the previous stdin/stdout-pipe
// implementation, ConPTY gives us:
//
//   - Full ANSI/VT colour and cursor control out of the box
//     (PowerShell auto-enables VT when it sees a real console).
//   - Working terminal resize via Resize → ResizePseudoConsole.
//   - Identical SendInput / Start / Close contract to the Unix PTY path.
//
// All locking, idempotent-Close, and ctx-cancel semantics mirror the
// Unix implementation so callers get the same behaviour everywhere.
type Session struct {
	pty *conpty.ConPty

	// pid / handle returned by ConPty.Spawn. We keep handle around so
	// Close can terminate the child without relying on os/exec.
	pid    int
	handle syscall.Handle

	cols, rows int

	mu        sync.Mutex
	closed    bool
	closeOnce sync.Once
	closeErr  error

	// started is set after the first Start so subsequent Close calls
	// know the wait goroutine is running.
	started atomic.Bool
}

// openSession resolves the binary, allocates a ConPty of the requested
// size, and spawns the child process. Mirrors the Unix openSession
// contract so module.go can stay platform-agnostic.
func openSession(_ context.Context, cfg openConfig) (*Session, error) {
	bin, err := discoverBinary(cfg.binary)
	if err != nil {
		return nil, fmt.Errorf("powershell: %w", err)
	}

	pty, err := conpty.New(int16(cfg.cols), int16(cfg.rows))
	if err != nil {
		return nil, fmt.Errorf("powershell: conpty: %w", err)
	}

	args := append([]string{bin, "-NoLogo", "-NoProfile", "-Interactive"}, cfg.args...)

	// Build a ProcAttr the way os/exec.Start would, so cwd / env land
	// where the user expects. ConPty.Spawn pulls stdin/stdout/stderr
	// off the PTY itself, so the Files slice is left nil here.
	attr := &syscall.ProcAttr{}
	if cfg.cwd != "" {
		attr.Dir = cfg.cwd
	}
	if len(cfg.env) > 0 {
		envSlice := os.Environ()
		for k, v := range cfg.env {
			envSlice = append(envSlice, k+"="+v)
		}
		attr.Env = envSlice
	}

	pid, handle, err := pty.Spawn(bin, args, attr)
	if err != nil {
		_ = pty.Close()
		return nil, fmt.Errorf("powershell: spawn %s: %w", bin, err)
	}

	return &Session{
		pty:    pty,
		pid:    pid,
		handle: syscall.Handle(handle),
		cols:   cfg.cols,
		rows:   cfg.rows,
	}, nil
}

// RenderMode reports the rendering mode. PowerShell sessions always
// render through the host terminal.
func (s *Session) RenderMode() protocol.RenderMode { return protocol.RenderTerminal }

// Start runs the bidirectional byte pump between the host-supplied
// stdin / stdout writers and the ConPTY pipes. It blocks until the
// child exits or ctx is cancelled, then tears the session down and
// returns.
func (s *Session) Start(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	s.started.Store(true)

	if stdout != nil {
		go func() {
			// OutPipe is the read-end of the PTY; io.Copy returns when
			// the PTY is closed (child exit or Close).
			_, _ = io.Copy(stdout, s.pty.OutPipe())
		}()
	}
	if stdin != nil {
		go func() {
			_, _ = io.Copy(s.pty.InPipe(), stdin)
		}()
	}

	waitErr := make(chan error, 1)
	go func() { waitErr <- s.waitForChild() }()

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

// waitForChild blocks until the child process exits and returns an
// error if WaitForSingleObject reports anything other than success.
// We use the syscall API directly because os/exec wasn't involved in
// spawning the child.
func (s *Session) waitForChild() error {
	if s.handle == 0 {
		return nil
	}
	event, err := syscall.WaitForSingleObject(s.handle, syscall.INFINITE)
	if err != nil {
		return fmt.Errorf("powershell: wait: %w", err)
	}
	if event != syscall.WAIT_OBJECT_0 {
		return fmt.Errorf("powershell: wait returned 0x%x", event)
	}
	var code uint32
	if err := syscall.GetExitCodeProcess(s.handle, &code); err != nil {
		return fmt.Errorf("powershell: exit code: %w", err)
	}
	if code != 0 {
		return &exec.ExitError{ProcessState: &os.ProcessState{}}
	}
	return nil
}

// Resize requests a window-size change on the underlying ConPTY.
// Matches the Unix implementation: zero or negative dimensions are an
// error, valid sizes propagate to ResizePseudoConsole.
func (s *Session) Resize(_ context.Context, size protocol.Size) error {
	if size.Cols <= 0 || size.Rows <= 0 {
		return fmt.Errorf("powershell: resize: cols/rows must be positive (got cols=%d rows=%d)", size.Cols, size.Rows)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("powershell: resize on closed session")
	}
	if err := s.pty.Resize(uint16(size.Cols), uint16(size.Rows)); err != nil {
		return fmt.Errorf("powershell: resize: %w", err)
	}
	s.cols = size.Cols
	s.rows = size.Rows
	return nil
}

// SendInput writes data verbatim into the PTY's input pipe. PowerShell
// receives it identically to keystrokes typed at a real console.
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
	_, err := s.pty.Write(data)
	return err
}

// Close terminates the session. Idempotent and safe under concurrent
// callers — only the first invocation kills the child and tears down
// the ConPTY; subsequent callers receive the same error.
func (s *Session) Close() error {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()

		s.killProcess()
		if s.pty != nil {
			if err := s.pty.Close(); err != nil {
				s.closeErr = err
			}
		}
		if s.handle != 0 {
			_ = syscall.CloseHandle(s.handle)
			s.handle = 0
		}
	})
	return s.closeErr
}

// killProcess sends TerminateProcess to the child if it's still alive.
// Safe to call multiple times and after the process has exited.
func (s *Session) killProcess() {
	if s.handle == 0 {
		return
	}
	_ = syscall.TerminateProcess(s.handle, 1)
}
