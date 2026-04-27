// Package extlaunch provides a shared helper for protocol plugins that
// implement their session by spawning a native external client
// (xfreerdp/mstsc for RDP, vncviewer/tigervnc for VNC, tn5250/tn5250j for
// TN5250, system browser for HTTP, etc.).
//
// The helper handles three things uniformly:
//
//  1. Binary discovery: walk a list of candidate executable names through
//     [exec.LookPath], honouring an optional explicit override.
//  2. Argument templating: simple `{name}` placeholder substitution against
//     a [Vars] map. Unknown placeholders cause [Build] to error rather than
//     silently emitting an empty string. Single-character placeholders
//     unrelated to substitution (e.g. `{1}`) pass through unchanged when the
//     name is not present in Vars; this lets authors embed literal braces by
//     escaping with `{{` / `}}`.
//  3. Process supervision: spawn the binary with the rendered args, capture
//     stdout/stderr to user-supplied writers, wait for exit while honouring
//     [context.Context] cancellation. On cancellation the process is sent
//     SIGTERM (Interrupt on Windows) and given a configurable grace period
//     before SIGKILL.
//
// The helper has no dependency on the rest of the goremote codebase apart
// from the standard library, so it is easy to unit-test in isolation.
package extlaunch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// ErrNotFound is returned by [Discover] when no candidate binary was found.
var ErrNotFound = errors.New("extlaunch: no candidate binary found on PATH")

// ErrPlaceholder is returned by [Build] when an arg references an unknown
// placeholder name.
var ErrPlaceholder = errors.New("extlaunch: unknown placeholder")

// DefaultGrace is the default delay between SIGTERM and SIGKILL when the
// supervising context is cancelled.
const DefaultGrace = 3 * time.Second

// Vars is a substitution map. Keys are placeholder names without braces;
// values are the literal replacement strings.
type Vars map[string]string

// Discover returns the absolute path to the first candidate found on PATH.
// override, when non-empty, is returned verbatim (after a LookPath check) so
// that callers can let users specify an explicit binary path.
func Discover(override string, candidates []string) (string, error) {
	if strings.TrimSpace(override) != "" {
		// Allow absolute paths to bypass LookPath; otherwise resolve
		// relative names against PATH.
		if abs, err := exec.LookPath(override); err == nil {
			return abs, nil
		}
		return "", fmt.Errorf("extlaunch: override %q not executable: %w", override, ErrNotFound)
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if p, err := exec.LookPath(c); err == nil {
			return p, nil
		}
	}
	return "", ErrNotFound
}

// Build renders an argv template by replacing `{name}` tokens against vars.
// Doubled braces `{{` and `}}` are emitted as literal `{` and `}`.
//
// Unknown placeholder names yield [ErrPlaceholder] so that templates can be
// validated at startup rather than producing surprising empty arguments at
// runtime.
func Build(template []string, vars Vars) ([]string, error) {
	out := make([]string, 0, len(template))
	for _, t := range template {
		rendered, err := renderOne(t, vars)
		if err != nil {
			return nil, err
		}
		// Drop fully empty arguments produced by an optional placeholder
		// expanding to an empty string. This keeps argv tidy.
		if rendered == "" && containsPlaceholder(t) {
			continue
		}
		out = append(out, rendered)
	}
	return out, nil
}

func renderOne(t string, vars Vars) (string, error) {
	var b strings.Builder
	for i := 0; i < len(t); i++ {
		c := t[i]
		switch c {
		case '{':
			if i+1 < len(t) && t[i+1] == '{' {
				b.WriteByte('{')
				i++
				continue
			}
			end := strings.IndexByte(t[i+1:], '}')
			if end < 0 {
				return "", fmt.Errorf("%w: unterminated placeholder in %q", ErrPlaceholder, t)
			}
			name := t[i+1 : i+1+end]
			val, ok := vars[name]
			if !ok {
				return "", fmt.Errorf("%w: %q in %q", ErrPlaceholder, name, t)
			}
			b.WriteString(val)
			i += 1 + end
		case '}':
			if i+1 < len(t) && t[i+1] == '}' {
				b.WriteByte('}')
				i++
				continue
			}
			return "", fmt.Errorf("%w: stray '}' in %q", ErrPlaceholder, t)
		default:
			b.WriteByte(c)
		}
	}
	return b.String(), nil
}

func containsPlaceholder(t string) bool {
	for i := 0; i < len(t)-1; i++ {
		if t[i] == '{' && t[i+1] != '{' {
			return true
		}
	}
	return false
}

// Spec describes a single supervised invocation.
type Spec struct {
	// Binary is the resolved binary path (use [Discover]).
	Binary string
	// Args are the rendered command-line arguments (use [Build]).
	Args []string
	// Env, when non-nil, fully replaces the child process environment.
	// When nil, the child inherits the parent environment.
	Env []string
	// Dir, when non-empty, sets the child's working directory.
	Dir string
	// Stdin, when non-nil, is wired to the child's standard input. The
	// supervisor does not close the reader; callers retain ownership.
	Stdin io.Reader
	// Stdout / Stderr receive the child's output. Either may be nil to
	// discard.
	Stdout io.Writer
	Stderr io.Writer
	// Grace is the delay between SIGTERM and SIGKILL when ctx is
	// cancelled. Zero means [DefaultGrace].
	Grace time.Duration
}

// Process is a running supervised process.
type Process struct {
	cmd     *exec.Cmd
	doneCh  chan struct{}
	exitErr error
	once    sync.Once
	grace   time.Duration
}

// Start spawns the process described by spec and supervises it under ctx.
// The returned Process is non-nil on success; callers should always call
// [Process.Wait] (or rely on context cancellation) to release resources.
func Start(ctx context.Context, spec Spec) (*Process, error) {
	if spec.Binary == "" {
		return nil, errors.New("extlaunch: Spec.Binary is empty")
	}
	cmd := exec.Command(spec.Binary, spec.Args...)
	if spec.Env != nil {
		cmd.Env = spec.Env
	}
	if spec.Dir != "" {
		cmd.Dir = spec.Dir
	}
	if spec.Stdin != nil {
		cmd.Stdin = spec.Stdin
	}
	if spec.Stdout != nil {
		cmd.Stdout = spec.Stdout
	}
	if spec.Stderr != nil {
		cmd.Stderr = spec.Stderr
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("extlaunch: start %s: %w", spec.Binary, err)
	}

	grace := spec.Grace
	if grace <= 0 {
		grace = DefaultGrace
	}

	p := &Process{cmd: cmd, doneCh: make(chan struct{}), grace: grace}

	go func() {
		p.exitErr = cmd.Wait()
		close(p.doneCh)
	}()

	go func() {
		select {
		case <-ctx.Done():
			p.terminate()
		case <-p.doneCh:
		}
	}()

	return p, nil
}

// Wait blocks until the process exits or ctx is done. It returns the
// underlying [*exec.Cmd] error (which is nil for a clean zero-exit).
func (p *Process) Wait(ctx context.Context) error {
	select {
	case <-p.doneCh:
		return p.exitErr
	case <-ctx.Done():
		p.terminate()
		<-p.doneCh
		return p.exitErr
	}
}

// Pid returns the OS process id, or 0 if the process has not started yet.
func (p *Process) Pid() int {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

// Kill terminates the process immediately. Safe to call multiple times.
func (p *Process) Kill() {
	p.once.Do(func() {
		if p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
		}
	})
}

// terminate sends an interrupt and, after the grace period, a kill if the
// process is still running. It is idempotent.
func (p *Process) terminate() {
	p.once.Do(func() {
		if p.cmd.Process == nil {
			return
		}
		_ = sendInterrupt(p.cmd)
		select {
		case <-p.doneCh:
			return
		case <-time.After(p.grace):
			_ = p.cmd.Process.Kill()
		}
	})
}

// sendInterrupt is platform-specific. On Unix it sends SIGTERM; on Windows
// it currently falls back to Kill since console-attached children are hard
// to interrupt portably without conpty wrangling.
func sendInterrupt(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	if runtime.GOOS == "windows" {
		return cmd.Process.Kill()
	}
	return cmd.Process.Signal(interruptSignal)
}
