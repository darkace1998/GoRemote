package http

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/darkace1998/GoRemote/sdk/protocol"
)

// sessionConfig is the immutable parameter bundle a [Session] is built with.
type sessionConfig struct {
	url         string
	browser     string
	verifyTLS   bool
	healthCheck bool
	launcher    launcher
	probe       probeFunc
}

// probeFunc performs an HTTP health-check against rawURL. The returned line
// must be a single human-readable status string suitable for printing to the
// session's stdout (without a trailing newline).
type probeFunc func(ctx context.Context, rawURL string, verifyTLS bool) string

// Session is a live HTTP/HTTPS launcher session. It is safe for the caller
// to invoke Start, Resize, SendInput, and Close concurrently.
type Session struct {
	cfg sessionConfig

	closeOnce sync.Once
	closeCh   chan struct{}
}

func newSession(cfg sessionConfig) *Session {
	return &Session{cfg: cfg, closeCh: make(chan struct{})}
}

// RenderMode reports the rendering mode negotiated at Open time. HTTP
// sessions always render externally (in the user's browser).
func (s *Session) RenderMode() protocol.RenderMode { return protocol.RenderExternal }

// Start launches the URL in the configured browser, optionally runs a single
// health-check probe, and then blocks until either ctx is cancelled or
// [Session.Close] is invoked. Start returns nil for both clean shutdowns and
// a non-nil error only if the launch itself failed.
//
// stdin is ignored: HTTP sessions accept no input. stdout receives one line
// describing the launch and, if health_check was set, one additional line
// reporting the probe result. If stdout is nil, the lines are simply
// discarded.
func (s *Session) Start(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	if stdout == nil {
		stdout = io.Discard
	}

	// If Close was already called before Start, exit immediately.
	select {
	case <-s.closeCh:
		return nil
	default:
	}

	path, prefixArgs, err := s.cfg.launcher.resolve(ctx, s.cfg.browser)
	if err != nil {
		return err
	}
	args := append(append([]string(nil), prefixArgs...), s.cfg.url)
	if err := s.cfg.launcher.run(ctx, path, args); err != nil {
		return fmt.Errorf("http: failed to launch browser: %w", err)
	}

	announce := fmt.Sprintf("goremote: opened %s in default browser\n", s.cfg.url)
	if s.cfg.browser != "" {
		announce = fmt.Sprintf("goremote: opened %s in %s\n", s.cfg.url, s.cfg.browser)
	}
	if _, werr := io.WriteString(stdout, announce); werr != nil {
		return werr
	}

	if s.cfg.healthCheck {
		probe := s.cfg.probe
		if probe == nil {
			probe = defaultProbe
		}
		line := probe(ctx, s.cfg.url, s.cfg.verifyTLS)
		if _, werr := io.WriteString(stdout, "goremote: "+line+"\n"); werr != nil {
			return werr
		}
	}

	// Block until ctx is cancelled or Close is invoked. HTTP sessions have
	// no I/O loop of their own; their lifetime is purely host-driven.
	select {
	case <-ctx.Done():
	case <-s.closeCh:
	}
	return nil
}

// Resize is unsupported for external-launcher sessions.
func (s *Session) Resize(ctx context.Context, size protocol.Size) error {
	return protocol.ErrUnsupported
}

// SendInput is unsupported for external-launcher sessions.
func (s *Session) SendInput(ctx context.Context, data []byte) error {
	return protocol.ErrUnsupported
}

// Close terminates the logical session. It is idempotent and safe across
// concurrent callers; only the first invocation actually unblocks Start.
func (s *Session) Close() error {
	s.closeOnce.Do(func() { close(s.closeCh) })
	return nil
}

// defaultProbe performs a single HEAD request (falling back to GET if HEAD
// is rejected with 405 Method Not Allowed) honoring verifyTLS, and returns a
// single human-readable status line without trailing newline. Network /
// transport errors are reported as "HEAD <path>, error: ..." rather than
// returned to the caller, because health-check failures must not abort the
// launch.
func defaultProbe(ctx context.Context, rawURL string, verifyTLS bool) string {
	tr := &http.Transport{
		// #nosec G402 -- verifyTLS=false is an explicit user opt-out; SECURITY.md documents the risk and the default remains true.
		TLSClientConfig: &tls.Config{InsecureSkipVerify: !verifyTLS},
	}
	client := &http.Client{Transport: tr, Timeout: 10 * time.Second}

	probeOnce := func(method string) (int, error) {
		req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
		if err != nil {
			return 0, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return 0, err
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		return resp.StatusCode, nil
	}

	method := "HEAD"
	status, err := probeOnce(method)
	if err == nil && status == http.StatusMethodNotAllowed {
		method = "GET"
		status, err = probeOnce(method)
	}
	if err != nil {
		return fmt.Sprintf("%s %s, error: %v", method, rawURL, err)
	}
	return fmt.Sprintf("%s %s, status %d", method, rawURL, status)
}
