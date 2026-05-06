package mosh

import (
	"context"
	"errors"
	"testing"

	"github.com/darkace1998/GoRemote/sdk/protocol"
)

var _ protocol.Module = (*Module)(nil)

func TestManifestValid(t *testing.T) {
	if err := Manifest.Validate(); err != nil {
		t.Fatalf("Manifest.Validate() returned error: %v", err)
	}
}

func TestCapabilities(t *testing.T) {
	caps := New().Capabilities()
	if len(caps.RenderModes) != 1 || caps.RenderModes[0] != protocol.RenderTerminal {
		t.Fatalf("RenderModes = %v, want [terminal]", caps.RenderModes)
	}
	if caps.SupportsResize || caps.SupportsReconnect {
		t.Fatalf("unexpected positive capability flags: %+v", caps)
	}
}

func TestConfigFromRequest_MissingHost(t *testing.T) {
	_, err := configFromRequest(protocol.OpenRequest{
		Settings: map[string]any{},
	})
	if err == nil {
		t.Fatalf("expected error when host is missing")
	}
}

func TestConfigFromRequest_BadPort(t *testing.T) {
	_, err := configFromRequest(protocol.OpenRequest{
		Settings: map[string]any{
			SettingHost: "h",
			SettingPort: 99999,
		},
	})
	if err == nil {
		t.Fatalf("expected error for out-of-range port 99999")
	}
}

func TestConfigFromRequest_Defaults(t *testing.T) {
	cfg, err := configFromRequest(protocol.OpenRequest{
		Host:     "example.com",
		Settings: map[string]any{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != defaultPort {
		t.Fatalf("Port = %d, want %d", cfg.Port, defaultPort)
	}
	if cfg.MoshPort != 0 {
		t.Fatalf("MoshPort default = %d, want 0", cfg.MoshPort)
	}
}

func TestOpen_ReturnsSessionWithoutDialing(t *testing.T) {
	mod := New()
	sess, err := mod.Open(context.Background(), protocol.OpenRequest{
		Host:     "example.com",
		Settings: map[string]any{SettingHost: "example.com"},
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if sess == nil {
		t.Fatal("expected non-nil session")
	}
	// Session should not have dialed yet — Close should be safe.
	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestParseMoshConnect(t *testing.T) {
	output := "some preamble\nMOSH CONNECT 60001 abc123key\nsome trailer\n"
	port, key, err := parseMoshConnect(output)
	if err != nil {
		t.Fatalf("parseMoshConnect: %v", err)
	}
	if port != "60001" {
		t.Fatalf("port = %q, want 60001", port)
	}
	if key != "abc123key" {
		t.Fatalf("key = %q, want abc123key", key)
	}
}

func TestParseMoshConnect_NotFound(t *testing.T) {
	_, _, err := parseMoshConnect("no connect line here\n")
	if err == nil {
		t.Fatal("expected error when MOSH CONNECT line is absent")
	}
}

func TestResizeUnsupported(t *testing.T) {
	cfg := &config{Host: "h", Port: 22}
	sess := newSession(cfg, "h:22")
	err := sess.Resize(context.Background(), protocol.Size{Cols: 80, Rows: 24})
	if !errors.Is(err, protocol.ErrUnsupported) {
		t.Fatalf("Resize err = %v, want ErrUnsupported", err)
	}
}

func TestCloseBeforeStart(t *testing.T) {
	cfg := &config{Host: "h", Port: 22}
	sess := newSession(cfg, "h:22")
	if err := sess.Close(); err != nil {
		t.Fatalf("Close before Start: %v", err)
	}
	// Idempotent
	if err := sess.Close(); err != nil {
		t.Fatalf("Close (second): %v", err)
	}
}


var _ protocol.Module = (*Module)(nil)

