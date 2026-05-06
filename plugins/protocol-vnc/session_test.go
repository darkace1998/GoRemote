package vnc

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/darkace1998/GoRemote/sdk/plugin"
	"github.com/darkace1998/GoRemote/sdk/protocol"
)

type failOnRead struct{ t *testing.T }

func (r failOnRead) Read(p []byte) (int, error) {
	r.t.Helper()
	r.t.Fatalf("Start must not read stdin for experimental VNC sessions")
	return 0, io.EOF
}

// Compile-time check that *Module satisfies protocol.Module.
var _ protocol.Module = (*Module)(nil)

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

func startFixedReplyServer(t *testing.T, reply []byte) (addr string, closeServer func()) {
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
			go func() {
				_, _ = conn.Write(reply)
				_ = conn.Close()
			}()
		}
	}()
	return ln.Addr().String(), func() { _ = ln.Close() }
}

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

func TestStart_ReceivesDataFromServer(t *testing.T) {
	want := []byte("rfb-server-greeting")
	addr, closeServer := startFixedReplyServer(t, want)
	defer closeServer()

	sess := newSession(addr)
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

func TestStart_ReturnsWhenServerClosesWithOpenStdin(t *testing.T) {
	want := []byte("rfb-server-goodbye")
	addr, closeServer := startFixedReplyServer(t, want)
	defer closeServer()

	sess := newSession(addr)
	var out bytes.Buffer

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- sess.Start(ctx, failOnRead{t}, &out) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Start hung after remote closed while stdin remained open")
	}
	if !bytes.Equal(out.Bytes(), want) {
		t.Fatalf("output = %q, want %q", out.Bytes(), want)
	}
}

func TestStart_SendsDataToServer(t *testing.T) {
	addr, closeServer := startEchoServer(t)
	defer closeServer()

	sess := newSession(addr)
	var out bytes.Buffer

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- sess.Start(ctx, nil, &out) }()

	msg := []byte("vnc-hello")
	waitForConn(t, sess)
	if err := sess.SendInput(ctx, msg); err != nil {
		t.Fatalf("SendInput: %v", err)
	}
	waitForOutput(t, &out, msg)
	_ = sess.Close()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return")
	}

	waitForOutput(t, &out, msg)
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

func TestStart_DialFailure(t *testing.T) {
	sess := newSession("127.0.0.1:1")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := sess.Start(ctx, nil, io.Discard); err == nil {
		t.Fatal("expected dial error")
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
	if err := sess.Start(context.Background(), nil, io.Discard); err != nil {
		t.Fatalf("Start after Close: %v", err)
	}
}

func waitForConn(t *testing.T, sess *Session) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		sess.mu.Lock()
		conn := sess.conn
		sess.mu.Unlock()
		if conn != nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("session did not establish connection")
}

func waitForOutput(t *testing.T, out *bytes.Buffer, want []byte) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if bytes.Equal(out.Bytes(), want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("echoed = %q, want %q", out.Bytes(), want)
}
