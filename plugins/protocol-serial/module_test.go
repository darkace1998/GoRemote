package serial

import (
	"context"
	"testing"

	"github.com/darkace1998/GoRemote/sdk/protocol"
)

func TestModuleManifest(t *testing.T) {
	m := New()
	man := m.Manifest()
	if man.ID == "" || man.Kind == "" {
		t.Fatalf("manifest missing fields: %+v", man)
	}
	if err := man.Validate(); err != nil {
		t.Fatalf("manifest invalid: %v", err)
	}
}

func TestModuleSettingsCoverAllKeys(t *testing.T) {
	m := New()
	defs := m.Settings()
	want := []string{
		SettingDevice, SettingBaud, SettingDataBits, SettingParity,
		SettingStopBits, SettingEOLMode, SettingEncoding,
	}
	have := make(map[string]bool, len(defs))
	for _, d := range defs {
		have[d.Key] = true
	}
	for _, k := range want {
		if !have[k] {
			t.Fatalf("settings missing key %q", k)
		}
	}
}

func TestModuleCapabilities(t *testing.T) {
	caps := New().Capabilities()
	if len(caps.RenderModes) == 0 || caps.RenderModes[0] != protocol.RenderTerminal {
		t.Fatalf("expected RenderTerminal, got %+v", caps.RenderModes)
	}
}

func TestOpenRequiresDevice(t *testing.T) {
	_, err := New().Open(context.Background(), protocol.OpenRequest{
		AuthMethod: protocol.AuthNone,
		Settings:   map[string]any{},
	})
	if err == nil {
		t.Fatal("expected error for missing device")
	}
}

func TestOpenRejectsBadDataBits(t *testing.T) {
	_, err := New().Open(context.Background(), protocol.OpenRequest{
		AuthMethod: protocol.AuthNone,
		Settings: map[string]any{
			SettingDevice:   "/dev/null",
			SettingDataBits: 4,
		},
	})
	if err == nil {
		t.Fatal("expected error for invalid data_bits")
	}
}

func TestOpenRejectsBadEOL(t *testing.T) {
	_, err := New().Open(context.Background(), protocol.OpenRequest{
		AuthMethod: protocol.AuthNone,
		Settings: map[string]any{
			SettingDevice:  "/dev/null",
			SettingEOLMode: "garbage",
		},
	})
	if err == nil {
		t.Fatal("expected error for invalid eol_mode")
	}
}
