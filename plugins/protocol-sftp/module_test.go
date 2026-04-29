package sftp

import (
	"context"
	"testing"

	"github.com/darkace1998/GoRemote/sdk/protocol"
)

func TestModuleManifest(t *testing.T) {
	man := New().Manifest()
	if err := man.Validate(); err != nil {
		t.Fatalf("manifest invalid: %v", err)
	}
}

func TestModuleSettingsContainSSHKeys(t *testing.T) {
	defs := New().Settings()
	have := map[string]bool{}
	for _, d := range defs {
		have[d.Key] = true
	}
	want := []string{
		SettingHost, SettingPort, SettingUsername,
		SettingKnownHostsPath, SettingStrictHostKeyChecking,
		SettingConnectTimeoutSeconds, SettingInitialPath,
	}
	for _, k := range want {
		if !have[k] {
			t.Fatalf("settings missing key %q", k)
		}
	}
}

func TestModuleCapabilitiesAdvertiseTerminalRender(t *testing.T) {
	caps := New().Capabilities()
	if len(caps.RenderModes) == 0 || caps.RenderModes[0] != protocol.RenderTerminal {
		t.Fatalf("expected RenderTerminal, got %+v", caps.RenderModes)
	}
	if len(caps.AuthMethods) == 0 {
		t.Fatal("expected at least one auth method")
	}
}

func TestOpenRequiresAuthMethod(t *testing.T) {
	_, err := New().Open(context.Background(), protocol.OpenRequest{
		Host:     "127.0.0.1",
		Port:     22,
		Username: "x",
		Settings: map[string]any{},
	})
	if err == nil {
		t.Fatal("expected error when auth method is unspecified")
	}
}

// Tokenize tests cover the line parser used by the REPL.
func TestTokenize(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"ls", []string{"ls"}},
		{"  ls   /tmp ", []string{"ls", "/tmp"}},
		{`get "a b" "c d"`, []string{"get", "a b", "c d"}},
		{`put 'with space' /remote`, []string{"put", "with space", "/remote"}},
		{`mv a\ b c`, []string{"mv", "a b", "c"}},
		{"", nil},
	}
	for _, tc := range cases {
		got := tokenize(tc.in)
		if len(got) != len(tc.want) {
			t.Fatalf("tokenize(%q) = %v, want %v", tc.in, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("tokenize(%q)[%d] = %q, want %q", tc.in, i, got[i], tc.want[i])
			}
		}
	}
}
