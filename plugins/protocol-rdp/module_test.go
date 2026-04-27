package rdp

import (
	"context"
	"reflect"
	"testing"

	"github.com/goremote/goremote/sdk/protocol"
)

var _ protocol.Module = (*Module)(nil)

func TestManifestValidate(t *testing.T) {
	if err := Manifest.Validate(); err != nil {
		t.Fatalf("Manifest.Validate() returned error: %v", err)
	}
	if Manifest.Version != "1.0.0" {
		t.Fatalf("Version = %q want 1.0.0", Manifest.Version)
	}
	if Manifest.Status != "ready" {
		t.Fatalf("Status = %q want ready", Manifest.Status)
	}
	if len(Manifest.Platforms) != 0 {
		t.Fatalf("Platforms = %v, expected nil/empty (works wherever a candidate binary exists)", Manifest.Platforms)
	}
	wantCap := false
	for _, c := range Manifest.Capabilities {
		if string(c) == "os.exec" {
			wantCap = true
		}
	}
	if !wantCap {
		t.Fatalf("Manifest must declare os.exec capability; got %v", Manifest.Capabilities)
	}
}

func TestCapabilities(t *testing.T) {
	caps := New().Capabilities()
	if len(caps.RenderModes) != 1 || caps.RenderModes[0] != protocol.RenderExternal {
		t.Fatalf("RenderModes = %v want [external]", caps.RenderModes)
	}
	if len(caps.AuthMethods) != 1 || caps.AuthMethods[0] != protocol.AuthNone {
		t.Fatalf("AuthMethods = %v want [none]", caps.AuthMethods)
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

func TestBuildArgvFor_Linux_AllOptional(t *testing.T) {
	cfg := &config{
		Host:     "host.example.com",
		Port:     3389,
		Username: "alice",
		Domain:   "EXAMPLE",
		Width:    1280,
		Height:   800,
		Gateway:  "gw.example.com:443",
	}
	got, err := buildArgvFor("linux", cfg)
	if err != nil {
		t.Fatalf("buildArgvFor: %v", err)
	}
	want := []string{
		"/v:host.example.com:3389",
		"/u:alice",
		"/d:EXAMPLE",
		"/w:1280",
		"/h:800",
		"/g:gw.example.com:443",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv = %#v\nwant   %#v", got, want)
	}
}

func TestBuildArgvFor_Linux_OptionalsSkippedWhenEmpty(t *testing.T) {
	cfg := &config{
		Host:   "host",
		Port:   3389,
		Width:  1024,
		Height: 768,
	}
	got, err := buildArgvFor("linux", cfg)
	if err != nil {
		t.Fatalf("buildArgvFor: %v", err)
	}
	want := []string{
		"/v:host:3389",
		"/w:1024",
		"/h:768",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv = %#v\nwant   %#v", got, want)
	}
}

func TestBuildArgvFor_FullscreenPrependsF(t *testing.T) {
	cfg := &config{Host: "h", Port: 1, Width: 1, Height: 1, Fullscreen: true}
	got, err := buildArgvFor("linux", cfg)
	if err != nil {
		t.Fatalf("buildArgvFor: %v", err)
	}
	if len(got) == 0 || got[0] != "/f" {
		t.Fatalf("expected first arg /f, got %#v", got)
	}

	gotW, err := buildArgvFor("windows", cfg)
	if err != nil {
		t.Fatalf("buildArgvFor windows: %v", err)
	}
	if len(gotW) == 0 || gotW[0] != "/f" {
		t.Fatalf("expected first arg /f on windows, got %#v", gotW)
	}
}

func TestBuildArgvFor_Windows_NoCredFlags(t *testing.T) {
	cfg := &config{
		Host: "h", Port: 3389,
		Username: "alice", Domain: "DOM", Gateway: "gw:443",
		Width: 1280, Height: 800,
	}
	got, err := buildArgvFor("windows", cfg)
	if err != nil {
		t.Fatalf("buildArgvFor: %v", err)
	}
	want := []string{
		"/v:h:3389",
		"/w:1280",
		"/h:800",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv = %#v\nwant   %#v (mstsc must not receive /u, /d, /g)", got, want)
	}
}

func TestBuildArgvFor_ExtraArgsAppended(t *testing.T) {
	cfg := &config{
		Host: "h", Port: 3389, Width: 1, Height: 1,
		ExtraArgs: []string{"+clipboard", "/cert:ignore"},
	}
	got, err := buildArgvFor("linux", cfg)
	if err != nil {
		t.Fatalf("buildArgvFor: %v", err)
	}
	if got[len(got)-2] != "+clipboard" || got[len(got)-1] != "/cert:ignore" {
		t.Fatalf("extra_args not appended verbatim at end: %#v", got)
	}
}

func TestCandidatesFor(t *testing.T) {
	if got := candidatesFor("linux"); !reflect.DeepEqual(got, []string{"xfreerdp3", "xfreerdp", "remmina"}) {
		t.Fatalf("linux candidates = %v", got)
	}
	if got := candidatesFor("darwin"); !reflect.DeepEqual(got, []string{"xfreerdp3", "xfreerdp", "remmina"}) {
		t.Fatalf("darwin candidates = %v", got)
	}
	if got := candidatesFor("windows"); !reflect.DeepEqual(got, []string{"mstsc.exe", "mstsc"}) {
		t.Fatalf("windows candidates = %v", got)
	}
}

func TestExtraArgsFromAnySlice(t *testing.T) {
	cfg, err := configFromRequest(protocol.OpenRequest{
		Settings: map[string]any{
			SettingHost:      "h",
			SettingExtraArgs: []any{"+clipboard", "/cert:ignore"},
		},
	})
	if err != nil {
		t.Fatalf("configFromRequest: %v", err)
	}
	if !reflect.DeepEqual(cfg.ExtraArgs, []string{"+clipboard", "/cert:ignore"}) {
		t.Fatalf("ExtraArgs = %v", cfg.ExtraArgs)
	}
}

func TestOpenDiscoveryFailureSurfacesError(t *testing.T) {
	mod := &Module{
		discover: func(override string, candidates []string) (string, error) {
			return "", errInjected
		},
	}
	_, err := mod.Open(context.Background(), protocol.OpenRequest{
		Host:     "h",
		Port:     3389,
		Settings: map[string]any{SettingHost: "h"},
	})
	if err == nil {
		t.Fatalf("expected discover error")
	}
}

var errInjected = injectedErr("injected")

type injectedErr string

func (e injectedErr) Error() string { return string(e) }
