package powershell

import (
	"context"

	"github.com/darkace1998/GoRemote/sdk/plugin"
	"github.com/darkace1998/GoRemote/sdk/protocol"
)

// Setting keys exposed by the PowerShell plugin.
const (
	SettingHost     = "host"
	SettingPort     = "port"
	SettingUsername = "username"
)

// Module is the built-in PowerShell protocol module.
//
// The zero value is a ready-to-use Module. It is safe for concurrent use;
// [Module.Open] creates an independent [Session] per call.
type Module struct{}

// New returns a ready-to-use [Module].
func New() *Module { return &Module{} }

// Manifest returns the static manifest for this plugin.
func (m *Module) Manifest() plugin.Manifest { return Manifest }

// Settings returns the planned remoting setting schema.
func (m *Module) Settings() []protocol.SettingDef {
	return []protocol.SettingDef{
		{
			Key: SettingHost, Label: "Host", Type: protocol.SettingString,
			Required:    true,
			Description: "Target host for the future PowerShell remoting transport.",
		},
		{
			Key: SettingPort, Label: "Port", Type: protocol.SettingInt,
			Default:     5986,
			Description: "PowerShell remoting endpoint port.",
		},
		{
			Key: SettingUsername, Label: "Username", Type: protocol.SettingString,
			Description: "Login user for the future PowerShell remoting transport.",
		},
	}
}

// Capabilities reports the planned runtime capabilities advertised by the
// PowerShell remoting module.
func (m *Module) Capabilities() protocol.Capabilities {
	return protocol.Capabilities{
		RenderModes:       []protocol.RenderMode{protocol.RenderTerminal},
		AuthMethods:       []protocol.AuthMethod{protocol.AuthPassword},
		SupportsResize:    false,
		SupportsClipboard: false,
		SupportsLogging:   true,
		SupportsReconnect: false,
	}
}

// Open is intentionally unsupported until a Go-native PowerShell remoting
// transport is implemented. It must not spawn a local PowerShell process.
func (m *Module) Open(ctx context.Context, req protocol.OpenRequest) (protocol.Session, error) {
	return nil, protocol.ErrUnsupported
}
