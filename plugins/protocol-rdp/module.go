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
	defaultPort   = 3389
	defaultWidth  = 1280
	defaultHeight = 800
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
		{
			Key:         SettingUsername,
			Label:       "Username",
			Type:        protocol.SettingString,
			Description: "User name for RDP authentication.",
		},
		{
			Key:         SettingDomain,
			Label:       "Domain",
			Type:        protocol.SettingString,
			Description: "Optional Active Directory / NT domain name.",
		},
		{
			Key:         SettingWidth,
			Label:       "Width",
			Type:        protocol.SettingInt,
			Default:     defaultWidth,
			Min:         ptrInt(1),
			Description: "Initial RDP session width in pixels.",
		},
		{
			Key:         SettingHeight,
			Label:       "Height",
			Type:        protocol.SettingInt,
			Default:     defaultHeight,
			Min:         ptrInt(1),
			Description: "Initial RDP session height in pixels.",
		},
		{
			Key:         SettingFullscreen,
			Label:       "Fullscreen",
			Type:        protocol.SettingBool,
			Default:     false,
			Description: "Request a fullscreen RDP session.",
		},
		{
			Key:         SettingGateway,
			Label:       "RD Gateway",
			Type:        protocol.SettingString,
			Description: "Optional Remote Desktop Gateway in host[:port] form.",
		},
	}
}

// Capabilities reports the runtime capabilities advertised by the RDP module.
func (m *Module) Capabilities() protocol.Capabilities {
	return protocol.Capabilities{
		RenderModes:       []protocol.RenderMode{protocol.RenderGraphical},
		AuthMethods:       []protocol.AuthMethod{protocol.AuthPassword, protocol.AuthNone},
		SupportsResize:    false,
		SupportsClipboard: false,
		SupportsLogging:   true,
		SupportsReconnect: false,
	}
}

// config is the validated form of an OpenRequest.
type config struct {
	Host       string
	Port       int
	Username   string
	Domain     string
	Width      int
	Height     int
	Fullscreen bool
	Gateway    string
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

func (s settingsView) boolOr(key string, def bool) bool {
	if v, ok := s.m[key]; ok {
		if b, ok := v.(bool); ok {
			return b
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

	username := req.Username
	if username == "" {
		username = view.stringOr(SettingUsername, "")
	}

	width := view.intOr(SettingWidth, defaultWidth)
	if width < 1 {
		return nil, fmt.Errorf("rdp: width must be >= 1, got %d", width)
	}
	height := view.intOr(SettingHeight, defaultHeight)
	if height < 1 {
		return nil, fmt.Errorf("rdp: height must be >= 1, got %d", height)
	}

	return &config{
		Host:       host,
		Port:       port,
		Username:   username,
		Domain:     view.stringOr(SettingDomain, ""),
		Width:      width,
		Height:     height,
		Fullscreen: view.boolOr(SettingFullscreen, false),
		Gateway:    view.stringOr(SettingGateway, ""),
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
