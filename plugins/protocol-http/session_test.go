package http

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/darkace1998/GoRemote/sdk/plugin"
	"github.com/darkace1998/GoRemote/sdk/protocol"
)

var _ protocol.Module = (*Module)(nil)

func TestManifestIsGoNativeHTTPProtocol(t *testing.T) {
	if err := Manifest.Validate(); err != nil {
		t.Fatalf("Manifest.Validate() returned error: %v", err)
	}
	if Manifest.HasCapability(plugin.CapProcessSpawn) {
		t.Fatalf("HTTP protocol must not declare process spawning")
	}
	if Manifest.HasCapability(plugin.CapExternalLauncher) {
		t.Fatalf("HTTP protocol must not declare external launcher capability")
	}
	if !Manifest.HasCapability(plugin.CapNetworkOutbound) {
		t.Fatalf("HTTP protocol must declare outbound network capability")
	}
}

func TestCapabilitiesUseTerminalRenderer(t *testing.T) {
	caps := New().Capabilities()
	if len(caps.RenderModes) != 1 || caps.RenderModes[0] != protocol.RenderTerminal {
		t.Fatalf("RenderModes = %v, want [terminal]", caps.RenderModes)
	}
}

func TestSettingsDoNotExposeBrowserBinary(t *testing.T) {
	for _, def := range New().Settings() {
		if def.Key == "browser" {
			t.Fatalf("HTTP protocol must not expose browser binary setting")
		}
	}
}

func TestOpenRequiresURL(t *testing.T) {
	_, err := New().Open(context.Background(), protocol.OpenRequest{Settings: map[string]any{}})
	if err == nil || !strings.Contains(err.Error(), "url is required") {
		t.Fatalf("expected 'url is required' error, got %v", err)
	}
}

func TestOpenRejectsInvalidScheme(t *testing.T) {
	cases := []string{"ftp://example.com", "file:///etc/passwd", "javascript:alert(1)", "://no-scheme", ""}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			_, err := New().Open(context.Background(), protocol.OpenRequest{
				Settings: map[string]any{SettingURL: raw},
			})
			if err == nil {
				t.Fatalf("expected error for url %q", raw)
			}
		})
	}
}

func TestStartFetchesURLInProcess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		_, _ = w.Write([]byte("native-http-response"))
	}))
	defer srv.Close()

	sess := openHTTP(t, srv.URL, nil)

	var out bytes.Buffer
	if err := sess.Start(context.Background(), nil, &out); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "native-http-response") {
		t.Fatalf("stdout = %q, want fetched response body", got)
	}
}

func TestHealthCheckFallsBackToGET(t *testing.T) {
	var sawGet bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.Method == http.MethodGet {
			sawGet = true
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	sess := openHTTP(t, srv.URL, map[string]any{SettingHealthCheck: true})
	var out bytes.Buffer
	if err := sess.Start(context.Background(), nil, &out); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !sawGet {
		t.Fatalf("health check did not fall back to GET")
	}
	if !strings.Contains(out.String(), "status 200") {
		t.Fatalf("stdout missing health status: %q", out.String())
	}
}

func TestCloseCancelsInFlightFetch(t *testing.T) {
	requestStarted := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requestStarted)
		<-r.Context().Done()
	}))
	defer srv.Close()

	sess := openHTTP(t, srv.URL, nil)
	done := make(chan error, 1)
	go func() { done <- sess.Start(context.Background(), nil, &bytes.Buffer{}) }()

	select {
	case <-requestStarted:
	case <-time.After(2 * time.Second):
		t.Fatalf("request did not start")
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start returned %v after Close", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("Start did not return after Close")
	}
}

func openHTTP(t *testing.T, rawURL string, extra map[string]any) protocol.Session {
	t.Helper()
	settings := map[string]any{SettingURL: rawURL}
	for k, v := range extra {
		settings[k] = v
	}
	sess, err := New().Open(context.Background(), protocol.OpenRequest{
		AuthMethod: protocol.AuthNone,
		Settings:   settings,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return sess
}
