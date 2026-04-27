package powershell

import (
	"testing"

	"github.com/goremote/goremote/sdk/plugin"
	"github.com/goremote/goremote/sdk/protocol"
)

// Compile-time check: Module satisfies the protocol.Module contract.
var _ protocol.Module = (*Module)(nil)

func TestModuleManifestValidates(t *testing.T) {
	m := New().Manifest()
	if err := m.Validate(); err != nil {
		t.Fatalf("Manifest.Validate: %v", err)
	}
	if m.ID != "io.goremote.protocol.powershell" {
		t.Fatalf("unexpected ID: %q", m.ID)
	}
	if m.Status != plugin.StatusReady {
		t.Fatalf("expected StatusReady, got %q", m.Status)
	}
	if m.Version != "1.0.0" {
		t.Fatalf("unexpected version: %q", m.Version)
	}
	if !m.HasCapability(plugin.CapTerminal) {
		t.Fatalf("manifest missing ui.terminal capability")
	}
	if !m.HasCapability(plugin.CapProcessSpawn) {
		t.Fatalf("manifest missing process.spawn capability")
	}
}

func TestModuleCapabilities(t *testing.T) {
	caps := New().Capabilities()
	if len(caps.RenderModes) != 1 || caps.RenderModes[0] != protocol.RenderTerminal {
		t.Fatalf("unexpected render modes: %+v", caps.RenderModes)
	}
	if len(caps.AuthMethods) != 1 || caps.AuthMethods[0] != protocol.AuthNone {
		t.Fatalf("unexpected auth methods: %+v", caps.AuthMethods)
	}
	if !caps.SupportsResize {
		t.Fatalf("expected SupportsResize")
	}
	if caps.SupportsReconnect {
		t.Fatalf("did not expect SupportsReconnect")
	}
}

func TestModuleSettingsSchema(t *testing.T) {
	defs := New().Settings()
	want := map[string]bool{
		SettingBinary: false,
		SettingCWD:    false,
		SettingArgs:   false,
		SettingEnv:    false,
		SettingCols:   false,
		SettingRows:   false,
	}
	for _, d := range defs {
		if _, ok := want[d.Key]; ok {
			want[d.Key] = true
		}
	}
	for k, seen := range want {
		if !seen {
			t.Errorf("settings schema missing key %q", k)
		}
	}
}

func TestResolveConfigDefaults(t *testing.T) {
	cfg, err := resolveConfig(protocol.OpenRequest{Settings: map[string]any{}})
	if err != nil {
		t.Fatalf("resolveConfig: %v", err)
	}
	if cfg.cols != defaultCols || cfg.rows != defaultRows {
		t.Fatalf("expected default size %dx%d, got %dx%d", defaultCols, defaultRows, cfg.cols, cfg.rows)
	}
}

func TestResolveConfigInitialSizeOverrides(t *testing.T) {
	cfg, err := resolveConfig(protocol.OpenRequest{
		Settings:    map[string]any{SettingCols: 200, SettingRows: 60},
		InitialSize: protocol.Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		t.Fatalf("resolveConfig: %v", err)
	}
	if cfg.cols != 80 || cfg.rows != 24 {
		t.Fatalf("InitialSize must override settings: got %dx%d", cfg.cols, cfg.rows)
	}
}

func TestDiscoverBinaryOverrideAbsolute(t *testing.T) {
	got, err := discoverBinary("/tmp/nope/fake-pwsh")
	if err != nil {
		t.Fatalf("discoverBinary: %v", err)
	}
	if got != "/tmp/nope/fake-pwsh" {
		t.Fatalf("expected verbatim path, got %q", got)
	}
}

func TestDiscoverBinaryOverrideMissing(t *testing.T) {
	if _, err := discoverBinary("definitely-not-on-path-xyz-pwsh-binary"); err == nil {
		t.Fatalf("expected error for missing binary")
	}
}
