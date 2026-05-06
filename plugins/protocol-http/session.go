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
	verifyTLS   bool
	healthCheck bool
	probe       probeFunc
	fetch       fetchFunc
}

// probeFunc performs an HTTP health-check against rawURL. The returned line
// must be a single human-readable status string suitable for printing to the
// session's stdout (without a trailing newline).
type probeFunc func(ctx context.Context, rawURL string, verifyTLS bool) string
type fetchFunc func(ctx context.Context, rawURL string, verifyTLS bool, stdout io.Writer) error

// Session is a live HTTP/HTTPS session. It is safe for the caller to invoke
// Start, Resize, SendInput, and Close concurrently.
type Session struct {
	cfg sessionConfig

	closeOnce sync.Once
	closeCh   chan struct{}
	cancelMu  sync.Mutex
	cancel    context.CancelFunc
}

func newSession(cfg sessionConfig) *Session {
	return &Session{cfg: cfg, closeCh: make(chan struct{})}
}

// RenderMode reports the rendering mode negotiated at Open time.
func (s *Session) RenderMode() protocol.RenderMode { return protocol.RenderTerminal }

// Start fetches the URL with Go's in-process HTTP client. It optionally runs a
// single health-check probe first, then streams the response body to stdout.
//
// stdin is ignored: HTTP sessions accept no input.
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

	runCtx, cancel := context.WithCancel(ctx)
	s.cancelMu.Lock()
	s.cancel = cancel
	s.cancelMu.Unlock()
	defer cancel()

	go func() {
		select {
		case <-s.closeCh:
			cancel()
		case <-runCtx.Done():
		}
	}()

	if s.cfg.healthCheck {
		probe := s.cfg.probe
		if probe == nil {
			probe = defaultProbe
		}
		line := probe(runCtx, s.cfg.url, s.cfg.verifyTLS)
		if _, werr := io.WriteString(stdout, "goremote: "+line+"\n"); werr != nil {
			return werr
		}
	}

	fetch := s.cfg.fetch
	if fetch == nil {
		fetch = defaultFetch
	}
	if err := fetch(runCtx, s.cfg.url, s.cfg.verifyTLS, stdout); err != nil {
		if runCtx.Err() != nil {
			return nil
		}
		return err
	}
	return nil
}

// Resize is unsupported for HTTP sessions.
func (s *Session) Resize(ctx context.Context, size protocol.Size) error {
	return protocol.ErrUnsupported
}

// SendInput is unsupported for HTTP sessions.
func (s *Session) SendInput(ctx context.Context, data []byte) error {
	return protocol.ErrUnsupported
}

// Close terminates the logical session. It is idempotent and safe across
// concurrent callers; only the first invocation actually unblocks Start.
func (s *Session) Close() error {
	s.closeOnce.Do(func() {
		close(s.closeCh)
		s.cancelMu.Lock()
		cancel := s.cancel
		s.cancelMu.Unlock()
		if cancel != nil {
			cancel()
		}
	})
	return nil
}

func defaultFetch(ctx context.Context, rawURL string, verifyTLS bool, stdout io.Writer) error {
	tr := &http.Transport{
		// #nosec G402 -- verifyTLS=false is an explicit user opt-out; SECURITY.md documents the risk and the default remains true.
		TLSClientConfig: &tls.Config{InsecureSkipVerify: !verifyTLS},
	}
	client := &http.Client{Transport: tr}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("GET %s: %w", rawURL, err)
	}
	defer resp.Body.Close()
	if _, err := fmt.Fprintf(stdout, "goremote: GET %s, status %d\n", rawURL, resp.StatusCode); err != nil {
		return err
	}
	_, err = io.Copy(stdout, resp.Body)
	return err
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
