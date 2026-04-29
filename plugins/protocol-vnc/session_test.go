package vnc

import (
	"context"
	"errors"
	"os"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/darkace1998/GoRemote/sdk/protocol"
)

// Compile-time check that *Module satisfies protocol.Module.
var _ protocol.Module = (*Module)(nil)

func TestManifestValidate(t *testing.T) {
	if err := Manifest.Validate(); err != nil {
		t.Fatalf("Manifest.Validate(): %v", err)
	}
	if Manifest.ID != "io.goremote.protocol.vnc" {
		t.Fatalf("manifest ID = %q", Manifest.ID)
	}
	if Manifest.Version != "1.0.0" {
		t.Fatalf("manifest version = %q", Manifest.Version)
	}
	if Manifest.Status != "ready" {
		t.Fatalf("manifest status = %q", Manifest.Status)
	}
	if !Manifest.HasCapability("os.exec") {
		t.Fatalf("manifest must declare os.exec capability")
	}
}

func TestCapabilities(t *testing.T) {
	caps := New().Capabilities()
	if len(caps.RenderModes) != 1 || caps.RenderModes[0] != protocol.RenderExternal {
		t.Fatalf("render modes = %v", caps.RenderModes)
	}
	if len(caps.AuthMethods) != 1 || caps.AuthMethods[0] != protocol.AuthNone {
		t.Fatalf("auth methods = %v", caps.AuthMethods)
	}
	if caps.SupportsResize || caps.SupportsReconnect {
		t.Fatalf("unexpected positive capability flags: %+v", caps)
	}
}

func TestSettingsSchema(t *testing.T) {
	got := map[string]protocol.SettingDef{}
	for _, s := range New().Settings() {
		got[s.Key] = s
	}
	for _, want := range []string{SettingHost, SettingPort, SettingPasswordVia, SettingViewOnly, SettingFullscreen, SettingBinary, SettingExtraArgs} {
		if _, ok := got[want]; !ok {
			t.Errorf("missing setting %q", want)
		}
	}
	if !got[SettingHost].Required {
		t.Errorf("host must be required")
	}
	if got[SettingPort].Default != 5900 {
		t.Errorf("port default = %v, want 5900", got[SettingPort].Default)
	}
	if pv := got[SettingPasswordVia]; pv.Default != PasswordViaNone {
		t.Errorf("password_via default = %v", pv.Default)
	} else if !reflect.DeepEqual(pv.EnumValues, []string{PasswordViaNone, PasswordViaStdin, PasswordViaPasswordFile}) {
		t.Errorf("password_via enum = %v", pv.EnumValues)
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

func TestResolveConfigInvalidPasswordVia(t *testing.T) {
	_, err := resolveConfig(protocol.OpenRequest{Settings: map[string]any{
		SettingHost: "h", SettingPasswordVia: "bogus",
	}})
	if err == nil {
		t.Fatalf("expected invalid password_via error")
	}
}

func TestBuildArgvVncviewer(t *testing.T) {
	cfg := openConfig{host: "10.0.0.5", port: 5901}
	got := buildArgv(cfg, "vncviewer", "")
	if !reflect.DeepEqual(got, []string{"10.0.0.5::5901"}) {
		t.Fatalf("argv = %v", got)
	}
}

func TestBuildArgvFlagsAndPwfile(t *testing.T) {
	cfg := openConfig{
		host: "h", port: 5900,
		viewOnly: true, fullscreen: true,
		passwordVia: PasswordViaPasswordFile,
		extraArgs:   []string{"-Quality=9"},
	}
	got := buildArgv(cfg, "tigervnc", "/tmp/pw")
	want := []string{"h::5900", "-ViewOnly", "-FullScreen", "-PasswordFile=/tmp/pw", "-Quality=9"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv = %v want %v", got, want)
	}
}

func TestBuildArgvOpenURL(t *testing.T) {
	cfg := openConfig{host: "host.example", port: 5902, viewOnly: true, fullscreen: true}
	got := buildArgv(cfg, "open", "")
	want := []string{"vnc://host.example:5902"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv = %v want %v", got, want)
	}
}

func TestCandidatesPerPlatform(t *testing.T) {
	if got := candidatesFor("linux"); !reflect.DeepEqual(got, []string{"vncviewer", "tigervnc", "remmina", "xtigervncviewer"}) {
		t.Fatalf("linux: %v", got)
	}
	if got := candidatesFor("darwin"); !reflect.DeepEqual(got, []string{"vncviewer", "open"}) {
		t.Fatalf("darwin: %v", got)
	}
	if got := candidatesFor("windows"); !reflect.DeepEqual(got, []string{"tvnviewer.exe", "tvnviewer", "vncviewer"}) {
		t.Fatalf("windows: %v", got)
	}
}

func TestBinaryBase(t *testing.T) {
	cases := map[string]string{
		"/usr/bin/vncviewer":     "vncviewer",
		`C:\tools\tvnviewer.EXE`: "tvnviewer",
		"vncviewer":              "vncviewer",
	}
	for in, want := range cases {
		if got := binaryBase(in); got != want {
			t.Errorf("binaryBase(%q) = %q want %q", in, got, want)
		}
	}
}

func TestWritePasswordFileMode(t *testing.T) {
	path, err := writePasswordFile("hunter2")
	if err != nil {
		t.Fatalf("writePasswordFile: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if runtime.GOOS != "windows" {
		if mode := info.Mode().Perm(); mode != 0o600 {
			t.Fatalf("password file mode = %o want 0600", mode)
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(b) != "hunter2" {
		t.Fatalf("password file contents = %q", b)
	}
}

func TestOpenSessionPasswordFileLifecycle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only test (uses /bin/sh)")
	}
	disc := func(override string, candidates []string) (string, error) {
		return "/bin/sh", nil
	}
	cfg := openConfig{
		host: "h", port: 5900, goos: runtime.GOOS,
		passwordVia: PasswordViaPasswordFile,
		password:    "secret",
		// Replace the rendered argv with a no-op shell command via
		// extraArgs, since the real binary is /bin/sh below.
	}
	sess, err := openSession(context.Background(), cfg, disc)
	if err != nil {
		t.Fatalf("openSession: %v", err)
	}
	if sess.pwfile == "" {
		t.Fatalf("expected pwfile to be materialised")
	}
	if _, err := os.Stat(sess.pwfile); err != nil {
		t.Fatalf("pwfile missing: %v", err)
	}
	pwfile := sess.pwfile

	// Replace argv so /bin/sh just exits cleanly.
	sess.argv = []string{"-c", "exit 0"}

	if err := sess.Start(context.Background(), nil, nil); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := os.Stat(pwfile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pwfile %q still present after Start: %v", pwfile, err)
	}
	// Close should still be a no-op.
	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("Close (second): %v", err)
	}
}

func TestStartStdinDeliversPassword(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only test (uses /bin/sh)")
	}
	dir := t.TempDir()
	captured := dir + "/stdin.txt"

	disc := func(override string, candidates []string) (string, error) {
		return "/bin/sh", nil
	}
	cfg := openConfig{
		host: "h", port: 5900, goos: runtime.GOOS,
		passwordVia: PasswordViaStdin,
		password:    "topsekret",
	}
	sess, err := openSession(context.Background(), cfg, disc)
	if err != nil {
		t.Fatalf("openSession: %v", err)
	}
	// Override argv: read all of stdin into the captured file, then echo
	// "launched" to stdout for the caller to verify the spawn happened.
	sess.argv = []string{"-c", "cat > " + captured + "; echo launched"}

	var out strings.Builder
	if err := sess.Start(context.Background(), nil, &out); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "launched" {
		t.Fatalf("stdout = %q", out.String())
	}
	got, err := os.ReadFile(captured)
	if err != nil {
		t.Fatalf("read captured stdin: %v", err)
	}
	if string(got) != "topsekret\n" {
		t.Fatalf("stdin captured = %q want %q", got, "topsekret\n")
	}
}

func TestSendInputAndResizeUnsupported(t *testing.T) {
	sess := &Session{}
	if err := sess.SendInput(context.Background(), []byte("x")); !errors.Is(err, protocol.ErrUnsupported) {
		t.Fatalf("SendInput err = %v", err)
	}
	if err := sess.Resize(context.Background(), protocol.Size{}); !errors.Is(err, protocol.ErrUnsupported) {
		t.Fatalf("Resize err = %v", err)
	}
}

func TestCloseIdempotent(t *testing.T) {
	sess := &Session{}
	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("Close (second): %v", err)
	}
}

func TestCloseInterruptsRunningViewer(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only test")
	}
	disc := func(string, []string) (string, error) { return "/bin/sh", nil }
	cfg := openConfig{host: "h", port: 5900, goos: runtime.GOOS}
	sess, err := openSession(context.Background(), cfg, disc)
	if err != nil {
		t.Fatalf("openSession: %v", err)
	}
	sess.argv = []string{"-c", "sleep 30"}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = sess.Start(context.Background(), nil, nil)
	}()

	// Wait briefly for the child to be spawned, then Close.
	time.Sleep(150 * time.Millisecond)
	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	doneCh := make(chan struct{})
	go func() { wg.Wait(); close(doneCh) }()
	select {
	case <-doneCh:
	case <-time.After(5 * time.Second):
		t.Fatalf("Start did not return after Close")
	}
}

func TestOpenSessionDiscoveryFailureSurfaces(t *testing.T) {
	disc := func(string, []string) (string, error) { return "", errors.New("nope") }
	cfg := openConfig{host: "h", port: 5900}
	if _, err := openSession(context.Background(), cfg, disc); err == nil {
		t.Fatalf("expected discovery error")
	}
}
