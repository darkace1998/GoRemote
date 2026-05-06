package vnc

import (
	"context"
	"fmt"
	"net"
	"strconv"

	"github.com/darkace1998/GoRemote/sdk/plugin"
	"github.com/darkace1998/GoRemote/sdk/protocol"
)

// Setting keys exposed by the VNC plugin.
const (
	SettingHost       = "host"
	SettingPort       = "port"
	SettingViewOnly   = "view_only"
	SettingFullscreen = "fullscreen"
)

// Default port for VNC (display 0 == 5900).
const defaultPort = 5900

// Module is the built-in VNC protocol module.
//
// The zero value is a ready-to-use Module. It is safe for concurrent use;
// [Module.Open] creates an independent [Session] per call.
type Module struct{}

// New returns a ready-to-use [Module].
func New() *Module { return &Module{} }

// Manifest returns the static manifest for this plugin.
func (m *Module) Manifest() plugin.Manifest { return Manifest }

// Settings returns the protocol-specific setting schema.
func (m *Module) Settings() []protocol.SettingDef {
	minPort := 1
	maxPort := 65535
	return []protocol.SettingDef{
		{
			Key: SettingHost, Label: "Host", Type: protocol.SettingString,
			Required:    true,
			Description: "Target host or address of the VNC server.",
		},
		{
			Key: SettingPort, Label: "Port", Type: protocol.SettingInt,
			Default:     defaultPort,
			Min:         &minPort,
			Max:         &maxPort,
			Description: "TCP port of the VNC server (5900 is display :0).",
		},
		{
			Key: SettingViewOnly, Label: "View-only", Type: protocol.SettingBool,
			Default:     false,
			Description: "Open the session without sending input events to the server.",
		},
		{
			Key: SettingFullscreen, Label: "Fullscreen", Type: protocol.SettingBool,
			Default:     false,
			Description: "Request a fullscreen display.",
		},
	}
}

// Capabilities reports the runtime capabilities advertised by the VNC module.
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

// settingsView is a typed view over the untyped settings map.
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

// openConfig is the resolved, plugin-internal view of an OpenRequest.
type openConfig struct {
	host       string
	port       int
	viewOnly   bool
	fullscreen bool
}

// resolveConfig validates the OpenRequest and produces an openConfig with
// defaults applied.
func resolveConfig(req protocol.OpenRequest) (openConfig, error) {
	view := settingsView{m: req.Settings}

	cfg := openConfig{
		host:       view.stringOr(SettingHost, ""),
		port:       view.intOr(SettingPort, defaultPort),
		viewOnly:   view.boolOr(SettingViewOnly, false),
		fullscreen: view.boolOr(SettingFullscreen, false),
	}

	if req.Host != "" {
		cfg.host = req.Host
	}
	if req.Port > 0 {
		cfg.port = req.Port
	}

	if cfg.host == "" {
		return cfg, fmt.Errorf("vnc: %s is required", SettingHost)
	}
	if cfg.port < 1 || cfg.port > 65535 {
		return cfg, fmt.Errorf("vnc: port out of range: %d", cfg.port)
	}
	return cfg, nil
}

// Open prepares a new VNC session. The TCP connection is not established
// until [Session.Start] is called.
func (m *Module) Open(ctx context.Context, req protocol.OpenRequest) (protocol.Session, error) {
	cfg, err := resolveConfig(req)
	if err != nil {
		return nil, err
	}
	addr := net.JoinHostPort(cfg.host, strconv.Itoa(cfg.port))
	return newSession(addr), nil
}

