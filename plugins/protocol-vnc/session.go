package vnc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/goremote/goremote/internal/extlaunch"
	"github.com/goremote/goremote/sdk/protocol"
)

// candidatesFor returns the per-platform list of viewer binary names tried
// in order when no explicit override is supplied.
func candidatesFor(goos string) []string {
	switch goos {
	case "linux":
		return []string{"vncviewer", "tigervnc", "remmina", "xtigervncviewer"}
	case "darwin":
		return []string{"vncviewer", "open"}
	case "windows":
		return []string{"tvnviewer.exe", "tvnviewer", "vncviewer"}
	default:
		return []string{"vncviewer"}
	}
}

// discoverer is the discovery seam used by tests to inject a fake viewer
// path without needing the real binary on PATH.
type discoverer func(override string, candidates []string) (string, error)

var defaultDiscoverer discoverer = extlaunch.Discover

// buildArgv renders the viewer's command line for cfg. binaryBase is the
// resolved binary base name (e.g. "vncviewer", "open") which selects the
// argv shape. pwfile, when non-empty, is the path of a materialised VNC
// password file passed as -PasswordFile=...
func buildArgv(cfg openConfig, binaryBase string, pwfile string) []string {
	endpoint := fmt.Sprintf("%s::%d", cfg.host, cfg.port)

	var argv []string
	switch binaryBase {
	case "open":
		// macOS' /usr/bin/open hands the URL to the registered VNC handler
		// (Screen Sharing.app on stock macOS).
		argv = []string{fmt.Sprintf("vnc://%s:%d", cfg.host, cfg.port)}
		// open(1) doesn't expose -ViewOnly / -FullScreen / -PasswordFile,
		// so we deliberately skip those flags here.
	default:
		// vncviewer / tigervnc / xtigervncviewer / tvnviewer all accept
		// the host::port endpoint form.
		argv = []string{endpoint}
		if cfg.viewOnly {
			argv = append(argv, "-ViewOnly")
		}
		if cfg.fullscreen {
			argv = append(argv, "-FullScreen")
		}
		if cfg.passwordVia == PasswordViaPasswordFile && pwfile != "" {
			argv = append(argv, "-PasswordFile="+pwfile)
		}
	}
	if len(cfg.extraArgs) > 0 {
		argv = append(argv, cfg.extraArgs...)
	}
	return argv
}

// binaryBase returns the lower-cased basename of path stripped of any
// trailing ".exe" suffix, so callers can switch on a stable name across
// platforms.
func binaryBase(path string) string {
	base := path
	for i := len(base) - 1; i >= 0; i-- {
		if base[i] == '/' || base[i] == '\\' {
			base = base[i+1:]
			break
		}
	}
	if l := len(base); l >= 4 {
		suf := base[l-4:]
		if suf == ".exe" || suf == ".EXE" {
			base = base[:l-4]
		}
	}
	return base
}

// Session is a live external-viewer VNC session.
type Session struct {
	cfg    openConfig
	binary string
	argv   []string
	pwfile string // empty if not materialised; deleted on Close

	closeOnce sync.Once
	closeErr  error

	mu      sync.Mutex
	cancel  context.CancelFunc
	process *extlaunch.Process
}

// openSession discovers the viewer, builds argv, and (when applicable)
// materialises the password file.
func openSession(ctx context.Context, cfg openConfig, disc discoverer) (*Session, error) {
	bin, err := disc(cfg.binary, candidatesFor(cfg.goos))
	if err != nil {
		return nil, fmt.Errorf("vnc: %w", err)
	}

	pwfile := ""
	if cfg.passwordVia == PasswordViaPasswordFile && cfg.password != "" {
		pwfile, err = writePasswordFile(cfg.password)
		if err != nil {
			return nil, err
		}
	}

	argv := buildArgv(cfg, binaryBase(bin), pwfile)
	return &Session{cfg: cfg, binary: bin, argv: argv, pwfile: pwfile}, nil
}

// writePasswordFile writes password to a 0600 temp file and returns its
// path. The file is intentionally not removed here; the caller is
// responsible for deleting it once the viewer has read it.
//
// os.CreateTemp creates the file with mode 0600 (O_CREATE|O_EXCL, 0600)
// so no subsequent chmod is needed.
func writePasswordFile(password string) (string, error) {
	f, err := os.CreateTemp("", "goremote-vnc-pw-*")
	if err != nil {
		return "", fmt.Errorf("vnc: create password file: %w", err)
	}
	path := f.Name()
	if _, err := f.WriteString(password); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("vnc: write password file: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("vnc: close password file: %w", err)
	}
	return path, nil
}

// RenderMode reports the rendering mode negotiated at Open time. VNC
// sessions always render externally (in the user's native viewer).
func (s *Session) RenderMode() protocol.RenderMode { return protocol.RenderExternal }

// Start spawns the viewer and blocks until it exits or ctx is cancelled.
// stdin is ignored; stdout/stderr from the viewer are forwarded to stdout
// (when non-nil) so the host can surface viewer messages.
//
// When password_via=stdin and a password is set, it is written followed by
// a newline to the viewer's stdin before the supervisor begins waiting.
//
// Regardless of how Start exits, any temp password file created at Open
// time is removed.
func (s *Session) Start(ctx context.Context, _ io.Reader, stdout io.Writer) (retErr error) {
	defer s.removePasswordFile()

	// Local context lets Close() interrupt the supervisor.
	runCtx, cancel := context.WithCancel(ctx)

	s.mu.Lock()
	s.cancel = cancel
	s.mu.Unlock()

	spec := extlaunch.Spec{
		Binary: s.binary,
		Args:   s.argv,
		Stdout: stdout,
		Stderr: stdout,
	}
	if s.cfg.passwordVia == PasswordViaStdin && s.cfg.password != "" {
		spec.Stdin = strings.NewReader(s.cfg.password + "\n")
	}

	proc, err := extlaunch.Start(runCtx, spec)
	if err != nil {
		return err
	}

	s.mu.Lock()
	s.process = proc
	s.mu.Unlock()

	err = proc.Wait(runCtx)
	if err != nil && (errors.Is(runCtx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.Canceled)) {
		return nil
	}
	return err
}

// Resize is unsupported for external-viewer sessions.
func (s *Session) Resize(ctx context.Context, size protocol.Size) error {
	return protocol.ErrUnsupported
}

// SendInput is unsupported for external-viewer sessions.
func (s *Session) SendInput(ctx context.Context, data []byte) error {
	return protocol.ErrUnsupported
}

// Close terminates the running viewer, removes the temp password file (if
// any), and is safe to call multiple times concurrently.
func (s *Session) Close() error {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		cancel := s.cancel
		s.mu.Unlock()

		// Cancelling the supervisor context triggers extlaunch's
		// SIGTERM + grace + SIGKILL fast-path inside the running
		// Start goroutine.
		if cancel != nil {
			cancel()
		}
		s.removePasswordFile()
	})
	return s.closeErr
}

// removePasswordFile deletes the temp password file (if any). Idempotent.
func (s *Session) removePasswordFile() {
	s.mu.Lock()
	path := s.pwfile
	s.pwfile = ""
	s.mu.Unlock()
	if path == "" {
		return
	}
	_ = os.Remove(path)
}
