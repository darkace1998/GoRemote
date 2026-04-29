package tn5250

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/darkace1998/GoRemote/sdk/protocol"
)

var _ protocol.Module = (*Module)(nil)
var _ protocol.Session = (*Session)(nil)

func TestManifestValidate(t *testing.T) {
	if err := Manifest.Validate(); err != nil {
		t.Fatalf("Manifest.Validate() returned error: %v", err)
	}
}

func TestCapabilities(t *testing.T) {
	caps := New().Capabilities()
	if len(caps.RenderModes) != 1 || caps.RenderModes[0] != protocol.RenderExternal {
		t.Fatalf("RenderModes = %v, want [%s]", caps.RenderModes, protocol.RenderExternal)
	}
	if len(caps.AuthMethods) != 1 || caps.AuthMethods[0] != protocol.AuthNone {
		t.Fatalf("AuthMethods = %v, want [%s]", caps.AuthMethods, protocol.AuthNone)
	}
	if caps.SupportsResize {
		t.Errorf("SupportsResize must be false")
	}
	if caps.SupportsReconnect {
		t.Errorf("SupportsReconnect must be false")
	}
}

func TestBuildArgvUnix(t *testing.T) {
	tests := []struct {
		name string
		cfg  config
		want []string
	}{
		{
			name: "default port omitted",
			cfg:  config{Host: "as400.example.com", Port: 23},
			want: []string{"-env+TERM=IBM-3179-2", "as400.example.com"},
		},
		{
			name: "non-default port appended",
			cfg:  config{Host: "as400.example.com", Port: 9923},
			want: []string{"-env+TERM=IBM-3179-2", "as400.example.com:9923"},
		},
		{
			name: "ssl prefix on default port",
			cfg:  config{Host: "as400.example.com", Port: 23, SSL: true},
			want: []string{"-env+TERM=IBM-3179-2", "ssl:as400.example.com"},
		},
		{
			name: "ssl prefix with port",
			cfg:  config{Host: "as400.example.com", Port: 992, SSL: true},
			want: []string{"-env+TERM=IBM-3179-2", "ssl:as400.example.com:992"},
		},
		{
			name: "device name",
			cfg:  config{Host: "h", Port: 23, DeviceName: "QPADEV0001"},
			want: []string{"-env+TERM=IBM-3179-2", "-env+DEVNAME=QPADEV0001", "h"},
		},
		{
			name: "code page",
			cfg:  config{Host: "h", Port: 23, CodePage: "1141"},
			want: []string{"-env+TERM=IBM-3179-2", "-env+CODEPAGE=1141", "h"},
		},
		{
			name: "device name + code page + port + ssl",
			cfg:  config{Host: "h", Port: 992, SSL: true, DeviceName: "DEV1", CodePage: "37"},
			want: []string{"-env+TERM=IBM-3179-2", "-env+DEVNAME=DEV1", "-env+CODEPAGE=37", "ssl:h:992"},
		},
		{
			name: "extra args appended verbatim before host",
			cfg:  config{Host: "h", Port: 23, ExtraArgs: []string{"-y", "24", "-x", "80"}},
			want: []string{"-env+TERM=IBM-3179-2", "-y", "24", "-x", "80", "h"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := buildArgvFor("linux", &tc.cfg)
			if err != nil {
				t.Fatalf("buildArgvFor: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("argv mismatch\n got: %q\nwant: %q", got, tc.want)
			}
			// darwin must produce identical argv.
			gotDarwin, err := buildArgvFor("darwin", &tc.cfg)
			if err != nil {
				t.Fatalf("buildArgvFor darwin: %v", err)
			}
			if !reflect.DeepEqual(gotDarwin, tc.want) {
				t.Fatalf("darwin argv differs from linux\n got: %q\nwant: %q", gotDarwin, tc.want)
			}
		})
	}
}

func TestBuildArgvWindows(t *testing.T) {
	cfg := config{Host: "as400.example.com", Port: 23}
	got, err := buildArgvFor("windows", &cfg)
	if err != nil {
		t.Fatalf("buildArgvFor: %v", err)
	}
	want := []string{"as400.example.com"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}

	cfg = config{Host: "h", Port: 9923, SSL: true, ExtraArgs: []string{"--foo"}}
	got, err = buildArgvFor("windows", &cfg)
	if err != nil {
		t.Fatalf("buildArgvFor: %v", err)
	}
	want = []string{"--foo", "ssl:h:9923"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestConfigFromRequestValidation(t *testing.T) {
	cases := []struct {
		name    string
		req     protocol.OpenRequest
		wantErr string
	}{
		{
			name:    "missing host",
			req:     protocol.OpenRequest{Settings: map[string]any{}},
			wantErr: "host is required",
		},
		{
			name: "port zero in settings rejected",
			req: protocol.OpenRequest{
				Host:     "h",
				Settings: map[string]any{SettingPort: 0},
			},
			wantErr: "port must be in",
		},
		{
			name:    "port out of range high",
			req:     protocol.OpenRequest{Host: "h", Port: 70000},
			wantErr: "port must be in",
		},
		{
			name:    "port out of range negative via setting",
			req:     protocol.OpenRequest{Host: "h", Settings: map[string]any{SettingPort: -1}},
			wantErr: "port must be in",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := configFromRequest(tc.req)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want substring %q", err, tc.wantErr)
			}
		})
	}
}

func TestConfigFromRequestDefaults(t *testing.T) {
	cfg, err := configFromRequest(protocol.OpenRequest{
		Host:     "as400",
		Settings: map[string]any{},
	})
	if err != nil {
		t.Fatalf("configFromRequest: %v", err)
	}
	if cfg.Port != defaultPort {
		t.Errorf("Port = %d, want %d", cfg.Port, defaultPort)
	}
	if cfg.SSL {
		t.Errorf("SSL default should be false")
	}
	if cfg.CodePage != "" {
		t.Errorf("CodePage default should be empty, got %q", cfg.CodePage)
	}
}

func TestSettingsRequiredFlags(t *testing.T) {
	defs := New().Settings()
	var hostDef *protocol.SettingDef
	for i := range defs {
		if defs[i].Key == SettingHost {
			hostDef = &defs[i]
		}
	}
	if hostDef == nil {
		t.Fatalf("host setting missing")
	}
	if !hostDef.Required {
		t.Errorf("host setting must be required")
	}
}

// TestStartIntegration spawns a fake "client" via the discover hook so we
// can verify the Open -> Start -> exit pipeline end-to-end without needing
// a real tn5250 binary on the test host.
func TestStartIntegration(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("integration test uses /bin/sh; skipping on windows")
	}

	mod := New()
	mod.discover = func(override string, candidates []string) (string, error) {
		return "/bin/sh", nil
	}
	// Replace the argv template with one /bin/sh actually understands. We
	// also smuggle a marker through stdout so we can confirm execution.
	mod.argvFor = func(goos string, cfg *config) ([]string, error) {
		return []string{"-c", "echo tn5250-mock " + cfg.Host}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sess, err := mod.Open(ctx, protocol.OpenRequest{
		Host:     "as400.example.com",
		Settings: map[string]any{SettingHost: "as400.example.com"},
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer sess.Close()

	if sess.RenderMode() != protocol.RenderExternal {
		t.Errorf("RenderMode = %s, want %s", sess.RenderMode(), protocol.RenderExternal)
	}

	var out bytes.Buffer
	if err := sess.Start(ctx, nil, &out); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !strings.Contains(out.String(), "tn5250-mock as400.example.com") {
		t.Fatalf("stdout = %q, want marker", out.String())
	}
}

func TestSendInputAndResizeUnsupported(t *testing.T) {
	s := newSession("/bin/true", nil)
	if err := s.SendInput(context.Background(), []byte("x")); !errors.Is(err, protocol.ErrUnsupported) {
		t.Errorf("SendInput err = %v, want ErrUnsupported", err)
	}
	if err := s.Resize(context.Background(), protocol.Size{Cols: 80, Rows: 24}); !errors.Is(err, protocol.ErrUnsupported) {
		t.Errorf("Resize err = %v, want ErrUnsupported", err)
	}
}

func TestCloseIdempotent(t *testing.T) {
	s := newSession("/bin/true", nil)
	if err := s.Close(); err != nil {
		t.Fatalf("Close #1: %v", err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.Close()
		}()
	}
	wg.Wait()
}

func TestCloseCancelsRunningProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("integration test uses /bin/sh; skipping on windows")
	}
	mod := New()
	mod.discover = func(override string, candidates []string) (string, error) { return "/bin/sh", nil }
	// Long-running mock that we'll terminate via Close.
	mod.argvFor = func(goos string, cfg *config) ([]string, error) {
		return []string{"-c", "sleep 30"}, nil
	}

	sess, err := mod.Open(context.Background(), protocol.OpenRequest{Host: "h"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- sess.Start(context.Background(), nil, nil) }()

	// Give Start a moment to spawn.
	time.Sleep(100 * time.Millisecond)

	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("Start did not return after Close")
	}
}

func TestHostArg(t *testing.T) {
	cases := []struct {
		cfg  config
		want string
	}{
		{config{Host: "h", Port: 23}, "h"},
		{config{Host: "h", Port: 23, SSL: true}, "ssl:h"},
		{config{Host: "h", Port: 992}, "h:992"},
		{config{Host: "h", Port: 992, SSL: true}, "ssl:h:992"},
	}
	for _, tc := range cases {
		got := hostArg(&tc.cfg)
		if got != tc.want {
			t.Errorf("hostArg(%+v) = %q, want %q", tc.cfg, got, tc.want)
		}
	}
}
