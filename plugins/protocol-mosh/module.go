package mosh

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"

	protossh "github.com/darkace1998/GoRemote/plugins/protocol-ssh"
	"github.com/darkace1998/GoRemote/sdk/plugin"
	"github.com/darkace1998/GoRemote/sdk/protocol"
)

// Setting keys exposed by the MOSH plugin.
const (
	SettingHost                  = "host"
	SettingPort                  = "port"
	SettingUsername              = "username"
	SettingMoshPort              = "mosh_port"
	SettingKnownHostsPath        = protossh.SettingKnownHostsPath
	SettingStrictHostKeyChecking = protossh.SettingStrictHostKeyChecking
)

// Defaults applied when the corresponding setting is unset.
const (
	defaultPort = 22
)

func ptrInt(v int) *int { return &v }

// Module is the built-in MOSH protocol module.
//
// Module is safe for concurrent use; each [Module.Open] call yields an
// independent [Session] that performs an SSH bootstrap to start mosh-server.
type Module struct{}

// New returns a ready-to-use [Module].
func New() *Module { return &Module{} }

// Manifest returns the static manifest for this plugin.
func (m *Module) Manifest() plugin.Manifest { return Manifest }

// Settings returns the protocol-specific setting schema.
func (m *Module) Settings() []protocol.SettingDef {
	return []protocol.SettingDef{
		{
			Key: SettingHost, Label: "Host", Type: protocol.SettingString,
			Required:    true,
			Description: "Target host name or IP address.",
		},
		{
			Key: SettingPort, Label: "SSH Port", Type: protocol.SettingInt,
			Default:     defaultPort,
			Min:         ptrInt(1),
			Max:         ptrInt(65535),
			Description: "SSH port used for the initial mosh-server bootstrap.",
		},
		{
			Key: SettingUsername, Label: "Username", Type: protocol.SettingString,
			Description: "SSH user name (optional).",
		},
		{
			Key: SettingMoshPort, Label: "Mosh UDP port", Type: protocol.SettingInt,
			Default:     0,
			Min:         ptrInt(0),
			Max:         ptrInt(65535),
			Description: "UDP port range start for the mosh server. 0 lets mosh pick automatically.",
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
	}
}

// Capabilities reports the runtime capabilities advertised by the MOSH module.
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
		SupportsReconnect: false,
	}
}

// config is the validated form of an OpenRequest.
type config struct {
	Host                  string
	Port                  int
	Username              string
	MoshPort              int
	KnownHostsPath        string
	StrictHostKeyChecking string
	AuthMethod            protocol.AuthMethod
	Secret                protocol.CredentialMaterial
}

// settingsView is a minimal typed view over the untyped settings map.
type settingsView struct{ m map[string]any }

func (s settingsView) stringOr(key, def string) string {
	if v, ok := s.m[key]; ok {
		if x, ok := v.(string); ok && x != "" {
			return x
		}
	}
	return def
}

func (s settingsView) intOr(key string, def int) int {
	if v, ok := s.m[key]; ok {
		switch x := v.(type) {
		case int:
			return x
		case int64:
			return int(x)
		case float64:
			return int(x)
		}
	}
	return def
}

// configFromRequest validates and assembles a config from the OpenRequest.
func configFromRequest(req protocol.OpenRequest) (*config, error) {
	view := settingsView{m: req.Settings}

	host := req.Host
	if host == "" {
		host = view.stringOr(SettingHost, "")
	}
	if host == "" {
		return nil, errors.New("mosh: host is required")
	}

	port := req.Port
	if port == 0 {
		port = view.intOr(SettingPort, defaultPort)
	}
	if port < 1 || port > 65535 {
		return nil, fmt.Errorf("mosh: port must be in [1,65535], got %d", port)
	}

	username := req.Username
	if username == "" {
		username = view.stringOr(SettingUsername, "")
	}
	if username == "" {
		username = req.Secret.Username
	}
	if username == "" {
		return nil, errors.New("mosh: username is required")
	}

	authMethod := req.AuthMethod
	if authMethod == "" {
		authMethod = protocol.AuthPassword
	}

	moshPort := view.intOr(SettingMoshPort, 0)
	if moshPort < 0 || moshPort > 65535 {
		return nil, fmt.Errorf("mosh: mosh_port must be in [0,65535], got %d", moshPort)
	}

	return &config{
		Host:                  host,
		Port:                  port,
		Username:              username,
		MoshPort:              moshPort,
		KnownHostsPath:        view.stringOr(SettingKnownHostsPath, ""),
		StrictHostKeyChecking: view.stringOr(SettingStrictHostKeyChecking, protossh.StrictAcceptNew),
		AuthMethod:            authMethod,
		Secret:                req.Secret,
	}, nil
}

// Open validates settings and returns a Session ready to bootstrap mosh via SSH.
// The SSH connection is not established until [Session.Start] is called.
func (m *Module) Open(ctx context.Context, req protocol.OpenRequest) (protocol.Session, error) {
	cfg, err := configFromRequest(req)
	if err != nil {
		return nil, err
	}
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	return newSession(cfg, addr), nil
}
