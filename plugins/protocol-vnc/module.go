package vnc

import (
	"context"
	"fmt"
	"runtime"

	"github.com/goremote/goremote/sdk/plugin"
	"github.com/goremote/goremote/sdk/protocol"
)

// Setting keys exposed by the VNC plugin.
const (
	SettingHost        = "host"
	SettingPort        = "port"
	SettingPasswordVia = "password_via"
	SettingViewOnly    = "view_only"
	SettingFullscreen  = "fullscreen"
	SettingBinary      = "binary"
	SettingExtraArgs   = "extra_args"
)

// Allowed values for password_via.
const (
	PasswordViaNone         = "none"
	PasswordViaStdin        = "stdin"
	PasswordViaPasswordFile = "passwordfile"
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
			Key: SettingPasswordVia, Label: "Password delivery", Type: protocol.SettingEnum,
			Default:     PasswordViaNone,
			EnumValues:  []string{PasswordViaNone, PasswordViaStdin, PasswordViaPasswordFile},
			Description: "How to deliver the credential password to the viewer (when a credential is supplied).",
		},
		{
			Key: SettingViewOnly, Label: "View-only", Type: protocol.SettingBool,
			Default:     false,
			Description: "Open the session without sending input events to the server.",
		},
		{
			Key: SettingFullscreen, Label: "Fullscreen", Type: protocol.SettingBool,
			Default:     false,
			Description: "Launch the viewer in fullscreen mode.",
		},
		{
			Key: SettingBinary, Label: "Viewer binary", Type: protocol.SettingString,
			Description: "Explicit path or name of the VNC viewer binary (overrides auto-discovery).",
		},
		{
			Key: SettingExtraArgs, Label: "Extra arguments", Type: protocol.SettingString,
			Description: "Additional command-line arguments appended after the rendered argv.",
		},
	}
}

// Capabilities reports the runtime capabilities advertised by the VNC
// module. Because we drive an external viewer, the host has no way to
// programmatically resize the framebuffer or inject input.
func (m *Module) Capabilities() protocol.Capabilities {
	return protocol.Capabilities{
		RenderModes:       []protocol.RenderMode{protocol.RenderExternal},
		AuthMethods:       []protocol.AuthMethod{protocol.AuthNone},
		SupportsResize:    false,
		SupportsClipboard: false,
		SupportsLogging:   false,
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

func (s settingsView) stringSlice(key string) []string {
	v, ok := s.m[key]
	if !ok {
		return nil
	}
	switch x := v.(type) {
	case []string:
		return append([]string(nil), x...)
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			if str, ok := e.(string); ok {
				out = append(out, str)
			}
		}
		return out
	}
	return nil
}

// openConfig is the resolved, plugin-internal view of an OpenRequest.
type openConfig struct {
	host        string
	port        int
	passwordVia string
	viewOnly    bool
	fullscreen  bool
	binary      string
	extraArgs   []string

	// runtime
	goos     string
	password string // resolved from credential, never logged
}

// resolveConfig validates the OpenRequest and produces an openConfig with
// defaults applied. It does not perform any side effects (no PATH lookup,
// no file I/O).
func resolveConfig(req protocol.OpenRequest) (openConfig, error) {
	view := settingsView{m: req.Settings}

	cfg := openConfig{
		host:        view.stringOr(SettingHost, ""),
		port:        view.intOr(SettingPort, defaultPort),
		passwordVia: view.stringOr(SettingPasswordVia, PasswordViaNone),
		viewOnly:    view.boolOr(SettingViewOnly, false),
		fullscreen:  view.boolOr(SettingFullscreen, false),
		binary:      view.stringOr(SettingBinary, ""),
		extraArgs:   view.stringSlice(SettingExtraArgs),
		goos:        runtime.GOOS,
		password:    req.Secret.Password,
	}

	// OpenRequest.Host / Port take precedence when set; otherwise fall back
	// to the values declared via Settings.
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
	switch cfg.passwordVia {
	case PasswordViaNone, PasswordViaStdin, PasswordViaPasswordFile:
	default:
		return cfg, fmt.Errorf("vnc: invalid %s %q", SettingPasswordVia, cfg.passwordVia)
	}
	return cfg, nil
}

// Open prepares a new VNC session. Discovery and password-file
// materialisation happen here so that Open can fail fast before the host
// wires up a renderer.
func (m *Module) Open(ctx context.Context, req protocol.OpenRequest) (protocol.Session, error) {
	cfg, err := resolveConfig(req)
	if err != nil {
		return nil, err
	}
	return openSession(ctx, cfg, defaultDiscoverer)
}
