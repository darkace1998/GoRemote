package tn5250

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/darkace1998/GoRemote/sdk/protocol"
)

var _ protocol.Module = (*Module)(nil)
var _ protocol.Session = (*Session)(nil)

// --- helpers ---------------------------------------------------------------

func startEchoServer(t *testing.T) (addr string, closeServer func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { io.Copy(conn, conn); conn.Close() }() //nolint:errcheck
		}
	}()
	return ln.Addr().String(), func() { _ = ln.Close() }
}

// --- manifest / capabilities -----------------------------------------------

func TestManifestValidate(t *testing.T) {
	if err := Manifest.Validate(); err != nil {
		t.Fatalf("Manifest.Validate() returned error: %v", err)
	}
}

func TestCapabilities(t *testing.T) {
	caps := New().Capabilities()
	if len(caps.RenderModes) != 1 || caps.RenderModes[0] != protocol.RenderTerminal {
		t.Fatalf("RenderModes = %v, want [%s]", caps.RenderModes, protocol.RenderTerminal)
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

// --- config validation -----------------------------------------------------

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

// --- session ---------------------------------------------------------------

func TestRenderMode(t *testing.T) {
	s := newSession("127.0.0.1:23")
	if s.RenderMode() != protocol.RenderTerminal {
		t.Fatalf("RenderMode = %s, want terminal", s.RenderMode())
	}
}

func TestStart_ReceivesDataFromServer(t *testing.T) {
	want := []byte("5250-server-data")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				_, _ = conn.Write(want)
				_ = conn.Close()
			}()
		}
	}()
	defer ln.Close()

	sess := newSession(ln.Addr().String())
	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := sess.Start(ctx, nil, &out); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !bytes.Equal(out.Bytes(), want) {
		t.Fatalf("output = %q, want %q", out.Bytes(), want)
	}
}

func TestStart_SendsDataToServer(t *testing.T) {
	addr, closeServer := startEchoServer(t)
	defer closeServer()

	sess := newSession(addr)
	pr, pw := io.Pipe()
	var out bytes.Buffer

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- sess.Start(ctx, pr, &out) }()

	msg := []byte("5250-input")
	_, _ = pw.Write(msg)
	_ = pw.Close()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return")
	}
	if !bytes.Equal(out.Bytes(), msg) {
		t.Fatalf("echoed = %q, want %q", out.Bytes(), msg)
	}
}

func TestStart_ContextCancellation(t *testing.T) {
	addr, closeServer := startEchoServer(t)
	defer closeServer()

	sess := newSession(addr)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- sess.Start(ctx, nil, io.Discard) }()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after context cancel")
	}
}

func TestSendInputAndResizeUnsupported(t *testing.T) {
	s := newSession("127.0.0.1:23")
	if err := s.SendInput(context.Background(), []byte("x")); err == nil {
		t.Error("expected error for SendInput before Start")
	}
	if err := s.Resize(context.Background(), protocol.Size{Cols: 80, Rows: 24}); !errors.Is(err, protocol.ErrUnsupported) {
		t.Errorf("Resize err = %v, want ErrUnsupported", err)
	}
}

func TestCloseIdempotent(t *testing.T) {
	s := newSession("127.0.0.1:23")
	for i := 0; i < 8; i++ {
		if err := s.Close(); err != nil {
			t.Errorf("Close #%d: %v", i, err)
		}
	}
}


var _ protocol.Module = (*Module)(nil)
var _ protocol.Session = (*Session)(nil)
