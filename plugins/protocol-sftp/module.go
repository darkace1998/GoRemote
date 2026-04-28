package sftp

import (
	"context"
	"errors"
	"fmt"

	pkgsftp "github.com/pkg/sftp"

	protossh "github.com/goremote/goremote/plugins/protocol-ssh"
	"github.com/goremote/goremote/sdk/plugin"
	"github.com/goremote/goremote/sdk/protocol"
)

// SFTP reuses every SSH connection setting (host, port, username,
// known_hosts, strict_host_key_checking, connect_timeout). The setting
// keys are re-exported here so the host's settings UI can build an
// editor without importing the SSH plugin directly.
const (
	SettingHost                  = protossh.SettingHost
	SettingPort                  = protossh.SettingPort
	SettingUsername              = protossh.SettingUsername
	SettingKnownHostsPath        = protossh.SettingKnownHostsPath
	SettingStrictHostKeyChecking = protossh.SettingStrictHostKeyChecking
	SettingConnectTimeoutSeconds = protossh.SettingConnectTimeoutSeconds
	SettingInitialPath           = "initial_path"
)

// Default port matches SSH (SFTP is a subsystem on the SSH connection).
const DefaultPort = protossh.DefaultPort

// Module is the built-in SFTP protocol module.
type Module struct{}

// New returns a ready-to-use [Module].
func New() *Module { return &Module{} }

// Manifest returns the static manifest for this plugin.
func (m *Module) Manifest() plugin.Manifest { return Manifest }

func ptrInt(v int) *int { return &v }

// Settings returns the protocol-specific setting schema.
func (m *Module) Settings() []protocol.SettingDef {
	return []protocol.SettingDef{
		{
			Key: SettingHost, Label: "Host", Type: protocol.SettingString,
			Required: true, Description: "Target SSH host name or IP.",
		},
		{
			Key: SettingPort, Label: "Port", Type: protocol.SettingInt,
			Default: DefaultPort, Min: ptrInt(1), Max: ptrInt(65535),
			Description: "TCP port of the remote SSH daemon.",
		},
		{
			Key: SettingUsername, Label: "Username", Type: protocol.SettingString,
			Required: true, Description: "Login user for the SSH/SFTP session.",
		},
		{
			Key: SettingKnownHostsPath, Label: "Known hosts file",
			Type:        protocol.SettingString,
			Description: "Path to the OpenSSH known_hosts file. Defaults to ~/.ssh/known_hosts.",
		},
		{
			Key: SettingStrictHostKeyChecking, Label: "Strict host key checking",
			Type:        protocol.SettingEnum,
			Default:     protossh.StrictAcceptNew,
			EnumValues:  []string{protossh.StrictAcceptNew, protossh.StrictStrict, protossh.StrictOff},
			Description: "accept-new: trust and remember unknown hosts. strict: refuse unknown hosts. off: skip verification.",
		},
		{
			Key: SettingConnectTimeoutSeconds, Label: "Connect timeout (seconds)",
			Type: protocol.SettingInt, Default: 15, Min: ptrInt(1), Max: ptrInt(600),
			Description: "Maximum time to wait for the SSH connect+handshake.",
		},
		{
			Key: SettingInitialPath, Label: "Initial remote path",
			Type:        protocol.SettingString,
			Description: "Optional remote directory to cd into immediately after connecting. Defaults to the user's home directory.",
		},
	}
}

// Capabilities reports the runtime capabilities advertised by the SFTP
// module.
func (m *Module) Capabilities() protocol.Capabilities {
	return protocol.Capabilities{
		RenderModes: []protocol.RenderMode{protocol.RenderTerminal},
		AuthMethods: []protocol.AuthMethod{
			protocol.AuthPassword,
			protocol.AuthPublicKey,
			protocol.AuthAgent,
			protocol.AuthInteractive,
		},
		SupportsResize:    false,
		SupportsClipboard: false,
		SupportsLogging:   true,
		SupportsReconnect: true,
	}
}

// Open dials SSH (delegating to the SSH plugin's exported Dial helper),
// negotiates the SFTP subsystem, and returns a session that runs the
// interactive shell inside the host terminal.
func (m *Module) Open(ctx context.Context, req protocol.OpenRequest) (protocol.Session, error) {
	if req.AuthMethod == "" {
		return nil, errors.New("sftp: auth method not specified")
	}

	client, cleanup, err := protossh.Dial(ctx, req)
	if err != nil {
		return nil, err
	}

	sc, err := pkgsftp.NewClient(client)
	if err != nil {
		_ = client.Close()
		cleanup()
		return nil, fmt.Errorf("sftp: subsystem: %w", err)
	}

	initialPath := ""
	if v, ok := req.Settings[SettingInitialPath]; ok {
		if s, ok := v.(string); ok {
			initialPath = s
		}
	}

	return newSession(sc, client, cleanup, initialPath), nil
}
