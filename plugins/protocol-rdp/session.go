package rdp

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"

	"github.com/goremote/goremote/internal/extlaunch"
	"github.com/goremote/goremote/sdk/protocol"
)

// Session is one live RDP session backed by an external client process.
//
// External-render sessions do not carry a stdin/stdout byte stream the way
// terminal sessions do; the only meaningful I/O over Start's stdout is a
// single status line plus any stderr output the native client emits, which
// is forwarded line-prefixed for diagnostic purposes.
type Session struct {
	binary string
	args   []string

	mu     sync.Mutex
	cancel context.CancelFunc
	proc   *extlaunch.Process

	closeOnce sync.Once
}

func newSession(binary string, args []string) *Session {
	return &Session{binary: binary, args: args}
}

// RenderMode reports the rendering mode negotiated at Open time. Native
// RDP clients own their own window, so we render in external mode.
func (s *Session) RenderMode() protocol.RenderMode { return protocol.RenderExternal }

// Start spawns the native RDP client and supervises it until exit or ctx
// cancellation. It writes a single "goremote: launched <binary> pid=<pid>"
// status line to stdout, then forwards the child's stdout verbatim and its
// stderr line-prefixed with "stderr: ".
//
// stdin is ignored: external launchers have no use for caller-supplied
// input. Any non-zero exit code from the native client is treated as a
// session end (not an error), matching the user expectation that closing
// the RDP window simply ends the session.
func (s *Session) Start(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	if stdout == nil {
		stdout = io.Discard
	}

	runCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	if s.cancel != nil {
		// Already started — refuse to start again.
		s.mu.Unlock()
		cancel()
		return errors.New("rdp: session already started")
	}
	s.cancel = cancel
	s.mu.Unlock()

	outR, outW := io.Pipe()
	errR, errW := io.Pipe()

	proc, err := extlaunch.Start(runCtx, extlaunch.Spec{
		Binary: s.binary,
		Args:   s.args,
		Stdout: outW,
		Stderr: errW,
	})
	if err != nil {
		_ = outW.Close()
		_ = errW.Close()
		_ = outR.Close()
		_ = errR.Close()
		cancel()
		return fmt.Errorf("rdp: start: %w", err)
	}

	s.mu.Lock()
	s.proc = proc
	s.mu.Unlock()

	// All writes to stdout — the status line, child stdout passthrough,
	// and prefixed stderr lines — are serialised through outMu so that
	// concurrent forwarders and the launching goroutine never race on the
	// user-supplied writer.
	var outMu sync.Mutex
	safeWrite := func(b []byte) {
		outMu.Lock()
		defer outMu.Unlock()
		_, _ = stdout.Write(b)
	}
	safeWrite([]byte(fmt.Sprintf("goremote: launched %s pid=%d\n", s.binary, proc.Pid())))

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := outR.Read(buf)
			if n > 0 {
				safeWrite(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(errR)
		// Allow long lines (some clients dump verbose status messages).
		sc.Buffer(make([]byte, 0, 4096), 1<<20)
		const maxStderrBytes = 10 << 20 // 10 MB total cap
		var total int
		for sc.Scan() {
			line := sc.Text()
			total += len(line) + 9 // len("stderr: ") + "\n"
			if total > maxStderrBytes {
				safeWrite([]byte("stderr: [output limit reached; truncated]\n"))
				return
			}
			safeWrite([]byte("stderr: " + line + "\n"))
		}
	}()

	waitErr := proc.Wait(runCtx)

	// Closing the writer ends of the pipes lets the forwarding goroutines
	// drain and exit.
	_ = outW.Close()
	_ = errW.Close()
	wg.Wait()
	_ = outR.Close()
	_ = errR.Close()

	// Any exit status is treated as a clean session end. Only "couldn't
	// even start the process" / context-internal errors are surfaced, and
	// even those are masked when the caller cancelled ctx.
	if waitErr == nil {
		return nil
	}
	if exitErr := (*exec.ExitError)(nil); errors.As(waitErr, &exitErr) {
		return nil
	}
	if errors.Is(waitErr, context.Canceled) || errors.Is(waitErr, context.DeadlineExceeded) {
		return nil
	}
	if ctx.Err() != nil {
		return nil
	}
	return waitErr
}

// Resize is unsupported: the native client manages its own window geometry.
func (s *Session) Resize(ctx context.Context, size protocol.Size) error {
	return protocol.ErrUnsupported
}

// SendInput is unsupported: input is delivered directly to the native
// client window by the OS.
func (s *Session) SendInput(ctx context.Context, data []byte) error {
	return protocol.ErrUnsupported
}

// Close terminates the session. Safe and idempotent across concurrent
// callers.
func (s *Session) Close() error {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		cancel := s.cancel
		proc := s.proc
		s.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		if proc != nil {
			proc.Kill()
		}
	})
	return nil
}
