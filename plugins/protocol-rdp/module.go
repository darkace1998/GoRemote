package rdp

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strconv"

	"github.com/darkace1998/GoRemote/internal/extlaunch"
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
	SettingBinary     = "binary"
	SettingExtraArgs  = "extra_args"
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
// independent [Session] that owns its own subprocess.
type Module struct {
	// discover, when non-nil, replaces extlaunch.Discover. It exists for
	// tests so they can substitute a binary that's actually executable in
	// CI (e.g. /bin/sh) without depending on a real RDP client being
	// installed.
	discover func(override string, candidates []string) (string, error)

	// argvFor, when non-nil, replaces buildArgvFor. It exists so tests can
	// substitute a trivial argv that prints a known marker line and exits
	// instead of attempting an actual RDP handshake.
	argvFor func(goos string, cfg *config) ([]string, error)
}

// New returns a ready-to-use [Module] that uses the real binary discovery
// and argv templates.
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
			Key: SettingUsername, Label: "Username", Type: protocol.SettingString,
			Description: "User name passed to the native client. This is *not* an authentication credential; the user still authenticates inside the native client window.",
		},
		{
			Key: SettingDomain, Label: "Domain", Type: protocol.SettingString,
			Description: "Optional Active Directory / NT domain name passed to the native client.",
		},
		{
			Key: SettingWidth, Label: "Width", Type: protocol.SettingInt,
			Default:     defaultWidth,
			Min:         ptrInt(1),
			Description: "Initial RDP session width in pixels.",
		},
		{
			Key: SettingHeight, Label: "Height", Type: protocol.SettingInt,
			Default:     defaultHeight,
			Min:         ptrInt(1),
			Description: "Initial RDP session height in pixels.",
		},
		{
			Key: SettingFullscreen, Label: "Fullscreen", Type: protocol.SettingBool,
			Default:     false,
			Description: "Launch the native client in fullscreen mode.",
		},
		{
			Key: SettingGateway, Label: "RD Gateway", Type: protocol.SettingString,
			Description: "Optional Remote Desktop Gateway in host[:port] form.",
		},
		{
			Key: SettingBinary, Label: "Binary override", Type: protocol.SettingString,
			Description: "Explicit path or name of the RDP client binary to launch (overrides auto-discovery).",
		},
		{
			Key: SettingExtraArgs, Label: "Extra arguments", Type: protocol.SettingString,
			Description: "Additional command-line arguments appended verbatim to the native client invocation.",
		},
	}
}

// Capabilities reports the runtime capabilities advertised by the RDP
// module. The native client owns its own window, so the host renders in
// external mode and cannot proxy resize / send-input events.
func (m *Module) Capabilities() protocol.Capabilities {
	return protocol.Capabilities{
		RenderModes:       []protocol.RenderMode{protocol.RenderExternal},
		AuthMethods:       []protocol.AuthMethod{protocol.AuthNone},
		SupportsResize:    false,
		SupportsClipboard: false,
		SupportsLogging:   true,
		SupportsReconnect: false,
	}
}

// config is the validated form of an OpenRequest. Kept package-local; the
// public surface is the OpenRequest -> Session pipeline.
type config struct {
	Host       string
	Port       int
	Username   string
	Domain     string
	Width      int
	Height     int
	Fullscreen bool
	Gateway    string
	Binary     string
	ExtraArgs  []string
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

func (s settingsView) stringSlice(key string) []string {
	v, ok := s.m[key]
	if !ok {
		return nil
	}
	switch x := v.(type) {
	case []string:
		out := make([]string, 0, len(x))
		for _, e := range x {
			if e != "" {
				out = append(out, e)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			if s, ok := e.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// configFromRequest validates and assembles a config from the OpenRequest.
// req.Host / req.Port take precedence over the settings map entries when
// non-empty/non-zero, mirroring the rawsocket plugin's convention.
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
		Binary:     view.stringOr(SettingBinary, ""),
		ExtraArgs:  view.stringSlice(SettingExtraArgs),
	}, nil
}

// candidatesFor returns the ordered list of binary candidate names to try
// on the given GOOS.
func candidatesFor(goos string) []string {
	if goos == "windows" {
		return []string{"mstsc.exe", "mstsc"}
	}
	return []string{"xfreerdp3", "xfreerdp", "remmina"}
}

// buildArgvFor renders the argv for the given platform. The xfreerdp form is
// used for any non-Windows platform; remmina accepts a different syntax in
// general but the xfreerdp-flavoured argv is the common case and remains the
// most useful default — users who need remmina-native invocation can supply
// their own template via extra_args + binary override.
//
// On Windows the mstsc client cannot accept inline credentials on the
// command line — only /v, /w, /h, and /f are honoured; username, domain,
// and gateway flags are intentionally omitted from the template.
func buildArgvFor(goos string, cfg *config) ([]string, error) {
	vars := extlaunch.Vars{
		"host":     cfg.Host,
		"port":     strconv.Itoa(cfg.Port),
		"username": cfg.Username,
		"domain":   cfg.Domain,
		"width":    strconv.Itoa(cfg.Width),
		"height":   strconv.Itoa(cfg.Height),
		"gateway":  cfg.Gateway,
	}

	var template []string
	if goos == "windows" {
		// mstsc: /v: target, /w: width, /h: height, optional /f for fullscreen.
		// mstsc cannot accept inline credentials on the command line — only
		// /v, /w, /h, and /f are honoured; username, domain, and gateway
		// flags are intentionally omitted from the template.
		if cfg.Fullscreen {
			template = append(template, "/f")
		}
		template = append(template,
			"/v:{host}:{port}",
			"/w:{width}",
			"/h:{height}",
		)
	} else {
		// xfreerdp argv. Optional arguments whose substituted value is
		// empty are omitted entirely so the client does not see flags like
		// "/u:" with no value (which xfreerdp interprets as an explicit
		// empty username rather than "no username supplied").
		if cfg.Fullscreen {
			template = append(template, "/f")
		}
		template = append(template, "/v:{host}:{port}")
		if cfg.Username != "" {
			template = append(template, "/u:{username}")
		}
		if cfg.Domain != "" {
			template = append(template, "/d:{domain}")
		}
		template = append(template,
			"/w:{width}",
			"/h:{height}",
		)
		if cfg.Gateway != "" {
			template = append(template, "/g:{gateway}")
		}
	}

	args, err := extlaunch.Build(template, vars)
	if err != nil {
		return nil, fmt.Errorf("rdp: build argv: %w", err)
	}
	if len(cfg.ExtraArgs) > 0 {
		args = append(args, cfg.ExtraArgs...)
	}
	return args, nil
}

// Open validates settings, discovers a suitable RDP client binary, and
// renders the argv. The returned [Session] holds the resolved binary path
// and arguments but does NOT spawn a subprocess until [Session.Start] is
// called.
func (m *Module) Open(ctx context.Context, req protocol.OpenRequest) (protocol.Session, error) {
	cfg, err := configFromRequest(req)
	if err != nil {
		return nil, err
	}

	goos := runtime.GOOS

	discover := m.discover
	if discover == nil {
		discover = extlaunch.Discover
	}
	binary, err := discover(cfg.Binary, candidatesFor(goos))
	if err != nil {
		return nil, fmt.Errorf("rdp: discover client: %w", err)
	}

	argvFor := m.argvFor
	if argvFor == nil {
		argvFor = buildArgvFor
	}
	args, err := argvFor(goos, cfg)
	if err != nil {
		return nil, err
	}

	return newSession(binary, args), nil
}
