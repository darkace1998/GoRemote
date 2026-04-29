package ssh

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/darkace1998/GoRemote/sdk/plugin"
	"github.com/darkace1998/GoRemote/sdk/protocol"
)

func TestManifestValidates(t *testing.T) {
	m := Manifest
	if err := m.Validate(); err != nil {
		t.Fatalf("manifest failed validation: %v", err)
	}
	if m.Kind != plugin.KindProtocol {
		t.Fatalf("kind = %q, want %q", m.Kind, plugin.KindProtocol)
	}
	for _, want := range []plugin.Capability{plugin.CapNetworkOutbound, plugin.CapTerminal, plugin.CapKeychainRead} {
		if !m.HasCapability(want) {
			t.Errorf("missing capability %q", want)
		}
	}
}

func TestModuleCapabilities(t *testing.T) {
	caps := New().Capabilities()
	if len(caps.RenderModes) != 1 || caps.RenderModes[0] != protocol.RenderTerminal {
		t.Errorf("unexpected render modes: %+v", caps.RenderModes)
	}
	if !caps.SupportsResize || !caps.SupportsLogging || !caps.SupportsReconnect {
		t.Errorf("capability flags wrong: %+v", caps)
	}
	if caps.SupportsClipboard {
		t.Errorf("clipboard should be false")
	}
	want := map[protocol.AuthMethod]bool{
		protocol.AuthPassword: true, protocol.AuthPublicKey: true,
		protocol.AuthAgent: true, protocol.AuthInteractive: true,
	}
	got := map[protocol.AuthMethod]bool{}
	for _, a := range caps.AuthMethods {
		got[a] = true
	}
	for k := range want {
		if !got[k] {
			t.Errorf("missing auth method %q", k)
		}
	}
}

func TestSettingsDefaults(t *testing.T) {
	settings := New().Settings()
	by := map[string]protocol.SettingDef{}
	for _, s := range settings {
		by[s.Key] = s
	}
	cases := []struct {
		key      string
		typ      protocol.SettingType
		required bool
		def      any
	}{
		{SettingHost, protocol.SettingString, true, nil},
		{SettingPort, protocol.SettingInt, false, 22},
		{SettingUsername, protocol.SettingString, true, nil},
		{SettingStrictHostKeyChecking, protocol.SettingEnum, false, StrictAcceptNew},
		{SettingKeepaliveSeconds, protocol.SettingInt, false, 30},
		{SettingEncoding, protocol.SettingEnum, false, "utf-8"},
		{SettingConnectTimeoutSeconds, protocol.SettingInt, false, 15},
		{SettingX11Forwarding, protocol.SettingBool, false, false},
		{SettingAgentForwarding, protocol.SettingBool, false, false},
		{SettingPTYTerm, protocol.SettingString, false, "xterm-256color"},
		{SettingKnownHostsPath, protocol.SettingString, false, nil},
	}
	for _, tc := range cases {
		s, ok := by[tc.key]
		if !ok {
			t.Errorf("missing setting %q", tc.key)
			continue
		}
		if s.Type != tc.typ {
			t.Errorf("%s: type=%q want %q", tc.key, s.Type, tc.typ)
		}
		if s.Required != tc.required {
			t.Errorf("%s: required=%v want %v", tc.key, s.Required, tc.required)
		}
		if tc.def != nil && s.Default != tc.def {
			t.Errorf("%s: default=%v want %v", tc.key, s.Default, tc.def)
		}
	}
	// Bounded settings
	if by[SettingPort].Min == nil || *by[SettingPort].Min != 1 ||
		by[SettingPort].Max == nil || *by[SettingPort].Max != 65535 {
		t.Errorf("port bounds wrong: %+v", by[SettingPort])
	}
	if by[SettingKeepaliveSeconds].Min == nil || *by[SettingKeepaliveSeconds].Min != 0 {
		t.Errorf("keepalive min wrong")
	}
}

func TestOpenRejectsMissingHost(t *testing.T) {
	_, err := New().Open(context.Background(), protocol.OpenRequest{
		Username:   "root",
		AuthMethod: protocol.AuthPassword,
	})
	if err == nil {
		t.Fatal("want error for missing host")
	}
}

func TestOpenRejectsMissingUser(t *testing.T) {
	_, err := New().Open(context.Background(), protocol.OpenRequest{
		Host:       "127.0.0.1",
		AuthMethod: protocol.AuthPassword,
	})
	if err == nil {
		t.Fatal("want error for missing user")
	}
}

func TestOpenInvalidHostReturnsError(t *testing.T) {
	// 127.0.0.1 on an ephemeral port we immediately release: connection refused.
	addr, closer := reservedClosedPort(t)
	defer closer()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	_, err := New().Open(ctx, protocol.OpenRequest{
		Host:       "127.0.0.1",
		Port:       addr.Port,
		Username:   "u",
		AuthMethod: protocol.AuthPassword,
		Secret:     protocol.CredentialMaterial{Password: "pw"},
		Settings: map[string]any{
			SettingStrictHostKeyChecking: StrictOff,
			SettingConnectTimeoutSeconds: 2,
			SettingKeepaliveSeconds:      0,
		},
	})
	if err == nil {
		t.Fatal("expected error connecting to refused port")
	}
	if elapsed := time.Since(start); elapsed > 4*time.Second {
		t.Fatalf("connection attempt took too long: %v", elapsed)
	}
}

func TestOpenUnsupportedAuthMethod(t *testing.T) {
	_, err := New().Open(context.Background(), protocol.OpenRequest{
		Host:       "127.0.0.1",
		Port:       22,
		Username:   "u",
		AuthMethod: protocol.AuthMethod("bogus"),
		Settings: map[string]any{
			SettingStrictHostKeyChecking: StrictOff,
			SettingConnectTimeoutSeconds: 1,
		},
	})
	if err == nil {
		t.Fatal("expected auth method error")
	}
}

func TestResolveKnownHostsPathReturnsAbsolutePath(t *testing.T) {
	got, err := resolveKnownHostsPath(filepath.Join(".", "known_hosts.test"))
	if err != nil {
		t.Fatalf("resolveKnownHostsPath: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("expected absolute path, got %q", got)
	}
}

func TestResolveKnownHostsPathRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "known_hosts")
	if err := os.WriteFile(target, []byte("host key"), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	link := filepath.Join(dir, "known_hosts.link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if _, err := resolveKnownHostsPath(link); err == nil {
		t.Fatal("expected symlink path to be rejected")
	}
}

func TestResolveAgentSocketPathRequiresSocket(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "not-a-socket")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("write regular file: %v", err)
	}
	if _, err := resolveAgentSocketPath(path); err == nil {
		t.Fatal("expected regular file to be rejected")
	}
}

func TestResolveAgentSocketPathAcceptsUnixSocket(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Skipf("unix sockets unavailable: %v", err)
	}
	defer func() { _ = ln.Close() }()
	got, err := resolveAgentSocketPath(path)
	if err != nil {
		t.Fatalf("resolveAgentSocketPath: %v", err)
	}
	if got != path {
		t.Fatalf("got %q want %q", got, path)
	}
}
