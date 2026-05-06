package rdp

import (
	"context"
	"strings"
	"testing"

	"github.com/darkace1998/GoRemote/sdk/protocol"
)

var _ protocol.Module = (*Module)(nil)

func TestManifestValidate(t *testing.T) {
	if err := Manifest.Validate(); err != nil {
		t.Fatalf("Manifest.Validate() returned error: %v", err)
	}
	if Manifest.Version != "2.0.0" {
		t.Fatalf("Version = %q want 2.0.0", Manifest.Version)
	}
	if Manifest.Status != "ready" {
		t.Fatalf("Status = %q want ready", Manifest.Status)
	}
	if !Manifest.HasCapability("network.outbound") {
		t.Fatalf("Manifest must declare network.outbound capability; got %v", Manifest.Capabilities)
	}
}

func TestCapabilities(t *testing.T) {
	caps := New().Capabilities()
	if len(caps.RenderModes) != 1 || caps.RenderModes[0] != protocol.RenderGraphical {
		t.Fatalf("RenderModes = %v want [graphical]", caps.RenderModes)
	}
	if caps.SupportsResize {
		t.Fatalf("SupportsResize must be false")
	}
	if caps.SupportsReconnect {
		t.Fatalf("SupportsReconnect must be false")
	}
}

func TestSettingsSchemaPortRange(t *testing.T) {
	defs := New().Settings()
	var port *protocol.SettingDef
	for i := range defs {
		if defs[i].Key == SettingPort {
			port = &defs[i]
		}
	}
	if port == nil {
		t.Fatalf("port setting missing")
	}
	if port.Min == nil || *port.Min != 1 {
		t.Fatalf("port.Min = %v want 1", port.Min)
	}
	if port.Max == nil || *port.Max != 65535 {
		t.Fatalf("port.Max = %v want 65535", port.Max)
	}
	if got := port.Default; got != 3389 {
		t.Fatalf("port.Default = %v want 3389", got)
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

func TestConfigFromRequest_PortOutOfRange(t *testing.T) {
	for _, p := range []int{-1, 0, 70000} {
		_, err := configFromRequest(protocol.OpenRequest{
			Settings: map[string]any{
				SettingHost: "h",
				SettingPort: p,
			},
		})
		if err == nil {
			t.Fatalf("expected error for out-of-range port %d", p)
		}
	}
	// Default applies when the setting is omitted entirely.
	cfg, err := configFromRequest(protocol.OpenRequest{
		Settings: map[string]any{SettingHost: "h"},
	})
	if err != nil {
		t.Fatalf("default port should not error: %v", err)
	}
	if cfg.Port != 3389 {
		t.Fatalf("default port = %d want 3389", cfg.Port)
	}
}

func TestConfigFromRequest_HostPrecedence(t *testing.T) {
	cfg, err := configFromRequest(protocol.OpenRequest{
		Host: "override.example.com",
		Port: 13389,
		Settings: map[string]any{
			SettingHost: "settings.example.com",
			SettingPort: 3389,
		},
	})
	if err != nil {
		t.Fatalf("configFromRequest: %v", err)
	}
	if cfg.Host != "override.example.com" {
		t.Fatalf("Host = %q, want override.example.com", cfg.Host)
	}
	if cfg.Port != 13389 {
		t.Fatalf("Port = %d, want 13389", cfg.Port)
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
	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestSettingsSchema_HostRequired(t *testing.T) {
	defs := New().Settings()
	for _, d := range defs {
		if d.Key == SettingHost {
			if !d.Required {
				t.Fatalf("host setting must be required")
			}
			return
		}
	}
	t.Fatal("host setting not found")
}

func TestConfigFromRequest_Fullscreen(t *testing.T) {
	cfg, err := configFromRequest(protocol.OpenRequest{
		Settings: map[string]any{
			SettingHost:       "h",
			SettingFullscreen: true,
		},
	})
	if err != nil {
		t.Fatalf("configFromRequest: %v", err)
	}
	if !cfg.Fullscreen {
		t.Fatal("Fullscreen should be true")
	}
}

func TestConfigFromRequest_Gateway(t *testing.T) {
	cfg, err := configFromRequest(protocol.OpenRequest{
		Settings: map[string]any{
			SettingHost:    "h",
			SettingGateway: "gw.example.com:443",
		},
	})
	if err != nil {
		t.Fatalf("configFromRequest: %v", err)
	}
	if !strings.Contains(cfg.Gateway, "gw.example.com") {
		t.Fatalf("Gateway = %q", cfg.Gateway)
	}
}


