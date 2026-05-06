package rdp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"

	"github.com/darkace1998/GoRemote/sdk/plugin"
	"github.com/darkace1998/GoRemote/sdk/protocol"
)

// Setting keys exposed by the RDP plugin.
const (
	SettingHost       = "host"
	SettingPort       = "port"
	SettingUsername   = "username"
	SettingDomain     = "domain"
	SettingWidth      = "width"
	SettingHeight     = "height"
	SettingFullscreen = "fullscreen"
	SettingGateway    = "gateway"
)

// Defaults applied when the corresponding setting is unset.
const (
	defaultPort = 3389
)

func ptrInt(v int) *int { return &v }

// Module is the built-in RDP protocol module.
//
// Module is safe for concurrent use; each [Module.Open] call yields an
// independent [Session] backed by a Go TCP connection.
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
			Key: SettingPort, Label: "Port", Type: protocol.SettingInt,
			Default:     defaultPort,
			Min:         ptrInt(1),
			Max:         ptrInt(65535),
			Description: "TCP port the RDP service is listening on.",
		},
	}
}

// Capabilities reports the runtime capabilities advertised by the RDP module.
func (m *Module) Capabilities() protocol.Capabilities {
	return protocol.Capabilities{
		RenderModes:       []protocol.RenderMode{protocol.RenderGraphical},
		AuthMethods:       []protocol.AuthMethod{protocol.AuthNone},
		SupportsResize:    false,
		SupportsClipboard: false,
		SupportsLogging:   true,
		SupportsReconnect: false,
	}
}

// config is the validated form of an OpenRequest.
type config struct {
	Host string
	Port int
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
// req.Host / req.Port take precedence over the settings map entries when
// non-empty/non-zero.
func configFromRequest(req protocol.OpenRequest) (*config, error) {
	view := settingsView{m: req.Settings}

	host := req.Host
	if host == "" {
		host = view.stringOr(SettingHost, "")
	}
	if host == "" {
		return nil, errors.New("rdp: host is required")
	}

	port := req.Port
	if port == 0 {
		port = view.intOr(SettingPort, defaultPort)
	}
	if port < 1 || port > 65535 {
		return nil, fmt.Errorf("rdp: port must be in [1,65535], got %d", port)
	}

	return &config{
		Host: host,
		Port: port,
	}, nil
}

// Open validates settings and returns a Session ready to dial the remote.
// The TCP connection is not established until [Session.Start] is called.
func (m *Module) Open(ctx context.Context, req protocol.OpenRequest) (protocol.Session, error) {
	cfg, err := configFromRequest(req)
	if err != nil {
		return nil, err
	}
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	return newSession(addr), nil
}
