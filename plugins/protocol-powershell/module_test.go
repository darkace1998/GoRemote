package powershell

import (
	"context"
	"errors"
	"testing"

	"github.com/darkace1998/GoRemote/sdk/plugin"
	"github.com/darkace1998/GoRemote/sdk/protocol"
)

var _ protocol.Module = (*Module)(nil)

func TestModuleManifestIsPlannedGoNativeRemoting(t *testing.T) {
	m := New().Manifest()
	if err := m.Validate(); err != nil {
		t.Fatalf("Manifest.Validate: %v", err)
	}
	if m.Status != plugin.StatusPlanned {
		t.Fatalf("Status = %q, want planned", m.Status)
	}
	if m.HasCapability(plugin.CapProcessSpawn) {
		t.Fatalf("PowerShell remoting protocol must not declare process spawning")
	}
	if !m.HasCapability(plugin.CapNetworkOutbound) {
		t.Fatalf("PowerShell remoting protocol must declare outbound network capability")
	}
	if !m.HasCapability(plugin.CapTerminal) {
		t.Fatalf("PowerShell remoting protocol must declare terminal capability")
	}
}

func TestModuleSettingsDoNotExposeLocalProcessControls(t *testing.T) {
	for _, def := range New().Settings() {
		switch def.Key {
		case "binary", "args", "cwd", "env":
			t.Fatalf("PowerShell remoting protocol must not expose local process setting %q", def.Key)
		}
	}
}

func TestOpenReturnsUnsupportedUntilRemotingEngineExists(t *testing.T) {
	_, err := New().Open(context.Background(), protocol.OpenRequest{
		Host:       "example.test",
		AuthMethod: protocol.AuthPassword,
		Secret:     protocol.CredentialMaterial{Password: "pw"},
	})
	if !errors.Is(err, protocol.ErrUnsupported) {
		t.Fatalf("Open error = %v, want ErrUnsupported", err)
	}
}
