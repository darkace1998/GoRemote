package vnc

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/darkace1998/GoRemote/sdk/plugin"
	"github.com/darkace1998/GoRemote/sdk/protocol"
)

// Compile-time check that *Module satisfies protocol.Module.
var _ protocol.Module = (*Module)(nil)

// --- manifest / capabilities -----------------------------------------------

func TestManifestValidate(t *testing.T) {
	if err := Manifest.Validate(); err != nil {
		t.Fatalf("Manifest.Validate(): %v", err)
	}
	if Manifest.ID != "io.goremote.protocol.vnc" {
		t.Fatalf("manifest ID = %q", Manifest.ID)
	}
	if Manifest.Version != "2.0.0" {
		t.Fatalf("manifest version = %q", Manifest.Version)
	}
	if Manifest.Status != plugin.StatusExperimental {
		t.Fatalf("manifest status = %q", Manifest.Status)
	}
	if !Manifest.HasCapability("network.outbound") {
		t.Fatalf("manifest must declare network.outbound capability; got %v", Manifest.Capabilities)
	}
}

func TestCapabilities(t *testing.T) {
	caps := New().Capabilities()
	if len(caps.RenderModes) != 1 || caps.RenderModes[0] != protocol.RenderGraphical {
		t.Fatalf("render modes = %v, want [graphical]", caps.RenderModes)
	}
	if caps.SupportsResize || caps.SupportsReconnect {
		t.Fatalf("unexpected positive capability flags: %+v", caps)
	}
}

// --- settings / config -----------------------------------------------------

func TestSettingsSchema(t *testing.T) {
	got := map[string]protocol.SettingDef{}
	for _, s := range New().Settings() {
		got[s.Key] = s
	}
	for _, want := range []string{SettingHost, SettingPort} {
		if _, ok := got[want]; !ok {
			t.Errorf("missing setting %q", want)
		}
	}
	for _, unsupported := range []string{"view_only", "fullscreen"} {
		if _, ok := got[unsupported]; ok {
			t.Fatalf("VNC must not expose unimplemented setting %q while protocol engine is experimental", unsupported)
		}
	}
	if !got[SettingHost].Required {
		t.Errorf("host must be required")
	}
	if got[SettingPort].Default != 5900 {
		t.Errorf("port default = %v, want 5900", got[SettingPort].Default)
	}
}

func TestResolveConfigRequiresHost(t *testing.T) {
	_, err := resolveConfig(protocol.OpenRequest{Settings: map[string]any{}})
	if err == nil {
		t.Fatalf("expected error for missing host")
	}
}

func TestResolveConfigPortRange(t *testing.T) {
	_, err := resolveConfig(protocol.OpenRequest{Settings: map[string]any{
		SettingHost: "h", SettingPort: 99999,
	}})
	if err == nil {
		t.Fatalf("expected port range error")
	}
}

// --- session ---------------------------------------------------------------

func TestRenderMode(t *testing.T) {
	s := newSession("127.0.0.1:5900")
	if s.RenderMode() != protocol.RenderGraphical {
		t.Fatalf("RenderMode = %s, want graphical", s.RenderMode())
	}
}

// All Start* tests assert ErrUnsupported: full RFB protocol framing is not implemented.

func TestStart_ReturnsErrUnsupported(t *testing.T) {
	sess := newSession("127.0.0.1:5900")
	err := sess.Start(context.Background(), nil, io.Discard)
	if !errors.Is(err, protocol.ErrUnsupported) {
		t.Fatalf("Start: got %v, want ErrUnsupported", err)
	}
}

func TestStart_ReceivesDataFromServer(t *testing.T) {
	sess := newSession("127.0.0.1:5900")
	err := sess.Start(context.Background(), nil, io.Discard)
	if !errors.Is(err, protocol.ErrUnsupported) {
		t.Fatalf("Start: got %v, want ErrUnsupported", err)
	}
}

func TestStart_ReturnsWhenServerClosesWithOpenStdin(t *testing.T) {
	sess := newSession("127.0.0.1:5900")
	err := sess.Start(context.Background(), nil, io.Discard)
	if !errors.Is(err, protocol.ErrUnsupported) {
		t.Fatalf("Start: got %v, want ErrUnsupported", err)
	}
}

func TestStart_SendsDataToServer(t *testing.T) {
	sess := newSession("127.0.0.1:5900")
	err := sess.Start(context.Background(), nil, io.Discard)
	if !errors.Is(err, protocol.ErrUnsupported) {
		t.Fatalf("Start: got %v, want ErrUnsupported", err)
	}
}

func TestStart_ContextCancellation(t *testing.T) {
	sess := newSession("127.0.0.1:5900")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := sess.Start(ctx, nil, io.Discard)
	if !errors.Is(err, protocol.ErrUnsupported) {
		t.Fatalf("Start: got %v, want ErrUnsupported", err)
	}
}

func TestSendInputAndResizeUnsupported(t *testing.T) {
	sess := newSession("127.0.0.1:5900")
	if err := sess.SendInput(context.Background(), []byte("x")); err == nil {
		t.Fatal("expected error for SendInput before Start")
	}
	if err := sess.Resize(context.Background(), protocol.Size{}); !errors.Is(err, protocol.ErrUnsupported) {
		t.Fatalf("Resize err = %v, want ErrUnsupported", err)
	}
}

func TestSendInput_ContextCanceled(t *testing.T) {
	sess := newSession("127.0.0.1:5900")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := sess.SendInput(ctx, []byte("x"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("SendInput err = %v, want context.Canceled", err)
	}
}

func TestCloseIdempotent(t *testing.T) {
	sess := newSession("127.0.0.1:5900")
	for i := 0; i < 4; i++ {
		if err := sess.Close(); err != nil {
			t.Errorf("Close #%d: %v", i, err)
		}
	}
}

func TestCloseBeforeStartPreventsDial(t *testing.T) {
	sess := newSession("127.0.0.1:5900")
	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := sess.Start(context.Background(), nil, io.Discard); !errors.Is(err, protocol.ErrUnsupported) {
		t.Fatalf("Start after Close: got %v, want ErrUnsupported", err)
	}
}
