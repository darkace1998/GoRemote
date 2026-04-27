package http

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/goremote/goremote/sdk/protocol"
)

// compile-time interface check.
var _ protocol.Module = (*Module)(nil)

// safeBuf is a thread-safe bytes.Buffer wrapper used by tests that poll for
// session output while a goroutine writes to it.
type safeBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// fakeLauncher is a launcher implementation used by the tests. It records
// the path/args it was asked to run and can be configured to fail.
type fakeLauncher struct {
	mu          sync.Mutex
	path        string
	prefixArgs  []string
	resolveErr  error
	runErr      error
	resolved    int
	gotPath     string
	gotArgs     []string
	overrideArg string
}

func (f *fakeLauncher) resolve(_ context.Context, override string) (string, []string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resolved++
	f.overrideArg = override
	if f.resolveErr != nil {
		return "", nil, f.resolveErr
	}
	if override != "" {
		return override, nil, nil
	}
	path := f.path
	if path == "" {
		path = "/usr/bin/fake-open"
	}
	return path, append([]string(nil), f.prefixArgs...), nil
}

func (f *fakeLauncher) run(_ context.Context, path string, args []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gotPath = path
	f.gotArgs = append([]string(nil), args...)
	return f.runErr
}

func TestManifestValidate(t *testing.T) {
	if err := Manifest.Validate(); err != nil {
		t.Fatalf("Manifest.Validate() returned error: %v", err)
	}
	if Manifest.ID != "io.goremote.protocol.http" {
		t.Fatalf("unexpected manifest ID: %q", Manifest.ID)
	}
	if Manifest.Version != "1.0.0" {
		t.Fatalf("unexpected version: %q", Manifest.Version)
	}
}

func TestCapabilities(t *testing.T) {
	caps := New().Capabilities()
	if len(caps.RenderModes) != 1 || caps.RenderModes[0] != protocol.RenderExternal {
		t.Fatalf("RenderModes = %v, want [external]", caps.RenderModes)
	}
	if len(caps.AuthMethods) != 1 || caps.AuthMethods[0] != protocol.AuthNone {
		t.Fatalf("AuthMethods = %v, want [none]", caps.AuthMethods)
	}
	if caps.SupportsResize {
		t.Errorf("SupportsResize must be false")
	}
	if caps.SupportsReconnect {
		t.Errorf("SupportsReconnect must be false")
	}
}

func openSession(t *testing.T, settings map[string]any, l launcher) protocol.Session {
	t.Helper()
	mod := New().WithLauncher(l)
	sess, err := mod.Open(context.Background(), protocol.OpenRequest{
		AuthMethod: protocol.AuthNone,
		Settings:   settings,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return sess
}

func TestOpenRequiresURL(t *testing.T) {
	mod := New()
	_, err := mod.Open(context.Background(), protocol.OpenRequest{Settings: map[string]any{}})
	if err == nil || !strings.Contains(err.Error(), "url is required") {
		t.Fatalf("expected 'url is required' error, got %v", err)
	}
}

func TestOpenRejectsInvalidScheme(t *testing.T) {
	cases := []string{
		"ftp://example.com",
		"file:///etc/passwd",
		"javascript:alert(1)",
		"://no-scheme",
		"",
	}
	for _, raw := range cases {
		raw := raw
		t.Run(raw, func(t *testing.T) {
			mod := New()
			_, err := mod.Open(context.Background(), protocol.OpenRequest{
				Settings: map[string]any{SettingURL: raw},
			})
			if err == nil {
				t.Fatalf("expected error for url %q", raw)
			}
		})
	}
}

func TestOpenAcceptsHTTPAndHTTPS(t *testing.T) {
	for _, raw := range []string{"http://example.com/", "https://example.com:8443/path?x=1"} {
		raw := raw
		t.Run(raw, func(t *testing.T) {
			fl := &fakeLauncher{}
			sess := openSession(t, map[string]any{SettingURL: raw}, fl)
			defer sess.Close()
			if sess.RenderMode() != protocol.RenderExternal {
				t.Fatalf("RenderMode = %q, want external", sess.RenderMode())
			}
		})
	}
}

func TestStartLaunchesAndAnnouncesURL(t *testing.T) {
	fl := &fakeLauncher{path: "/usr/bin/fake-open"}
	sess := openSession(t, map[string]any{SettingURL: "https://example.com/x"}, fl)

	var stdout safeBuf
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- sess.Start(ctx, nil, &stdout) }()

	// Give Start a moment to announce.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(stdout.String(), "goremote: opened") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if got := stdout.String(); !strings.Contains(got, "goremote: opened https://example.com/x in default browser") {
		t.Fatalf("stdout = %q", got)
	}

	fl.mu.Lock()
	gotPath := fl.gotPath
	gotArgs := append([]string(nil), fl.gotArgs...)
	fl.mu.Unlock()
	if gotPath != "/usr/bin/fake-open" {
		t.Fatalf("path = %q want /usr/bin/fake-open", gotPath)
	}
	if len(gotArgs) != 1 || gotArgs[0] != "https://example.com/x" {
		t.Fatalf("args = %v, want [https://example.com/x]", gotArgs)
	}

	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start returned %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("Start did not return after Close")
	}
}

func TestStartUsesCustomBrowser(t *testing.T) {
	fl := &fakeLauncher{}
	sess := openSession(t, map[string]any{
		SettingURL:     "https://example.com/",
		SettingBrowser: "/opt/firefox/firefox",
	}, fl)

	var stdout safeBuf
	done := make(chan error, 1)
	go func() { done <- sess.Start(context.Background(), nil, &stdout) }()

	// Wait for announce.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(stdout.String(), "goremote: opened") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	fl.mu.Lock()
	if fl.overrideArg != "/opt/firefox/firefox" {
		t.Errorf("override = %q, want /opt/firefox/firefox", fl.overrideArg)
	}
	if fl.gotPath != "/opt/firefox/firefox" {
		t.Errorf("path = %q, want /opt/firefox/firefox", fl.gotPath)
	}
	fl.mu.Unlock()

	if got := stdout.String(); !strings.Contains(got, "in /opt/firefox/firefox") {
		t.Errorf("stdout = %q, expected mention of custom browser", got)
	}

	_ = sess.Close()
	<-done
}

func TestStartLauncherFailurePropagates(t *testing.T) {
	wantErr := errors.New("boom-resolve")
	fl := &fakeLauncher{resolveErr: wantErr}
	sess := openSession(t, map[string]any{SettingURL: "https://example.com/"}, fl)

	err := sess.Start(context.Background(), nil, nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Start err = %v, want %v", err, wantErr)
	}

	// Run-stage failure.
	wantRun := errors.New("boom-run")
	fl2 := &fakeLauncher{runErr: wantRun}
	sess2 := openSession(t, map[string]any{SettingURL: "https://example.com/"}, fl2)
	err = sess2.Start(context.Background(), nil, nil)
	if err == nil || !strings.Contains(err.Error(), "boom-run") {
		t.Fatalf("Start err = %v, want to contain boom-run", err)
	}
}

func TestHealthCheckSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	fl := &fakeLauncher{}
	sess := openSession(t, map[string]any{
		SettingURL:         srv.URL,
		SettingHealthCheck: true,
	}, fl)

	var stdout safeBuf
	done := make(chan error, 1)
	go func() { done <- sess.Start(context.Background(), nil, &stdout) }()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(stdout.String(), "status 200") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	out := stdout.String()
	if !strings.Contains(out, "goremote: opened "+srv.URL) {
		t.Errorf("stdout missing announce: %q", out)
	}
	if !strings.Contains(out, "status 200") {
		t.Errorf("stdout missing status 200: %q", out)
	}
	if !strings.Contains(out, "HEAD ") {
		t.Errorf("stdout missing HEAD method line: %q", out)
	}

	_ = sess.Close()
	<-done
}

func TestHealthCheckFallsBackToGET(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	fl := &fakeLauncher{}
	sess := openSession(t, map[string]any{
		SettingURL:         srv.URL,
		SettingHealthCheck: true,
	}, fl)

	var stdout safeBuf
	done := make(chan error, 1)
	go func() { done <- sess.Start(context.Background(), nil, &stdout) }()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(stdout.String(), "status 200") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	out := stdout.String()
	if !strings.Contains(out, "GET ") {
		t.Errorf("expected GET fallback in output: %q", out)
	}
	if !strings.Contains(out, "status 200") {
		t.Errorf("expected status 200 in output: %q", out)
	}

	_ = sess.Close()
	<-done
}

func TestHealthCheckErrorDoesNotAbort(t *testing.T) {
	// Use the injectable probe to deterministically simulate a network error.
	fl := &fakeLauncher{}
	mod := New().WithLauncher(fl)
	_, err := mod.Open(context.Background(), protocol.OpenRequest{
		Settings: map[string]any{
			SettingURL:         "https://example.invalid/",
			SettingHealthCheck: true,
		},
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Build a session with an injected probe.
	sess := newSession(sessionConfig{
		url:         "https://example.invalid/",
		verifyTLS:   true,
		healthCheck: true,
		launcher:    fl,
		probe: func(_ context.Context, raw string, _ bool) string {
			return fmt.Sprintf("HEAD %s, error: simulated dns failure", raw)
		},
	})

	var stdout safeBuf
	done := make(chan error, 1)
	go func() { done <- sess.Start(context.Background(), nil, &stdout) }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(stdout.String(), "simulated dns failure") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(stdout.String(), "simulated dns failure") {
		t.Fatalf("expected probe error in output, got %q", stdout.String())
	}

	_ = sess.Close()
	if err := <-done; err != nil {
		t.Fatalf("Start returned %v", err)
	}
}

func TestVerifyTLSDefaultsTrueIsPlumbed(t *testing.T) {
	var (
		mu       sync.Mutex
		captured *bool
	)
	probe := func(_ context.Context, _ string, verifyTLS bool) string {
		mu.Lock()
		v := verifyTLS
		captured = &v
		mu.Unlock()
		return "HEAD https://x, status 200"
	}
	read := func() *bool {
		mu.Lock()
		defer mu.Unlock()
		if captured == nil {
			return nil
		}
		v := *captured
		return &v
	}
	reset := func() {
		mu.Lock()
		captured = nil
		mu.Unlock()
	}

	sess := newSession(sessionConfig{
		url:         "https://example.com/",
		healthCheck: true,
		verifyTLS:   true,
		launcher:    &fakeLauncher{},
		probe:       probe,
	})
	var stdout safeBuf
	done := make(chan error, 1)
	go func() { done <- sess.Start(context.Background(), nil, &stdout) }()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if read() != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := read(); got == nil || *got != true {
		t.Fatalf("verifyTLS not plumbed correctly: %v", got)
	}
	_ = sess.Close()
	<-done

	// And again with verifyTLS=false.
	reset()
	sess2 := newSession(sessionConfig{
		url:         "https://example.com/",
		healthCheck: true,
		verifyTLS:   false,
		launcher:    &fakeLauncher{},
		probe:       probe,
	})
	var stdout2 safeBuf
	done2 := make(chan error, 1)
	go func() { done2 <- sess2.Start(context.Background(), nil, &stdout2) }()
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if read() != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := read(); got == nil || *got != false {
		t.Fatalf("verifyTLS=false not plumbed correctly: %v", got)
	}
	_ = sess2.Close()
	<-done2
}

func TestSendInputAndResizeUnsupported(t *testing.T) {
	fl := &fakeLauncher{}
	sess := openSession(t, map[string]any{SettingURL: "https://example.com/"}, fl)
	defer sess.Close()

	if err := sess.SendInput(context.Background(), []byte("x")); !errors.Is(err, protocol.ErrUnsupported) {
		t.Errorf("SendInput err = %v, want ErrUnsupported", err)
	}
	if err := sess.Resize(context.Background(), protocol.Size{Cols: 80, Rows: 24}); !errors.Is(err, protocol.ErrUnsupported) {
		t.Errorf("Resize err = %v, want ErrUnsupported", err)
	}
}

func TestCloseIdempotent(t *testing.T) {
	fl := &fakeLauncher{}
	sess := openSession(t, map[string]any{SettingURL: "https://example.com/"}, fl)

	if err := sess.Close(); err != nil {
		t.Fatalf("Close #1: %v", err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = sess.Close()
		}()
	}
	wg.Wait()
}

func TestCloseBeforeStartCausesStartToReturnNil(t *testing.T) {
	fl := &fakeLauncher{}
	sess := openSession(t, map[string]any{SettingURL: "https://example.com/"}, fl)
	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- sess.Start(context.Background(), nil, nil) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start after Close returned %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("Start did not return promptly after pre-Close")
	}

	fl.mu.Lock()
	defer fl.mu.Unlock()
	if fl.resolved != 0 {
		t.Errorf("launcher.resolve invoked %d times; expected 0 because Close preceded Start", fl.resolved)
	}
}

func TestStartReturnsOnContextCancel(t *testing.T) {
	fl := &fakeLauncher{}
	sess := openSession(t, map[string]any{SettingURL: "https://example.com/"}, fl)
	defer sess.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- sess.Start(ctx, nil, nil) }()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start err = %v, want nil on cancel", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("Start did not return after cancel")
	}
}

func TestSettingsSchema(t *testing.T) {
	defs := New().Settings()
	required := map[string]bool{}
	keys := map[string]bool{}
	for _, d := range defs {
		keys[d.Key] = true
		if d.Required {
			required[d.Key] = true
		}
	}
	for _, k := range []string{SettingURL, SettingBrowser, SettingVerifyTLS, SettingHealthCheck} {
		if !keys[k] {
			t.Errorf("missing setting %q", k)
		}
	}
	if !required[SettingURL] {
		t.Errorf("url must be required")
	}
	if required[SettingBrowser] || required[SettingVerifyTLS] || required[SettingHealthCheck] {
		t.Errorf("only url should be required")
	}
}
