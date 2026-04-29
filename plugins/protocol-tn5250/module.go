package tn5250

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

// Setting keys exposed by the TN5250 plugin.
const (
	SettingHost       = "host"
	SettingPort       = "port"
	SettingDeviceName = "device_name"
	SettingCodePage   = "code_page"
	SettingSSL        = "ssl"
	SettingBinary     = "binary"
	SettingExtraArgs  = "extra_args"
)

// defaultPort is the standard TN5250 / Telnet port. Connections on this port
// use the bare hostname; non-default ports get appended as host:port.
const defaultPort = 23

func ptrInt(v int) *int { return &v }

// Module is the built-in TN5250 protocol module.
//
// Module is safe for concurrent use; each [Module.Open] call yields an
// independent [Session] that owns its own subprocess.
type Module struct {
	// discover, when non-nil, replaces extlaunch.Discover. It exists for
	// tests so they can substitute a binary that's actually executable in
	// CI (e.g. /bin/sh) without depending on a real TN5250 client being
	// installed.
	discover func(override string, candidates []string) (string, error)

	// argvFor, when non-nil, replaces buildArgvFor. Reserved for tests.
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
			Description: "Target IBM i / AS400 host name or IP address.",
		},
		{
			Key: SettingPort, Label: "Port", Type: protocol.SettingInt,
			Default:     defaultPort,
			Min:         ptrInt(1),
			Max:         ptrInt(65535),
			Description: "TCP port the TN5250 service is listening on. The default port (23) is omitted from the host argument.",
		},
		{
			Key: SettingDeviceName, Label: "Device name", Type: protocol.SettingString,
			Description: "Optional 5250 device name; passed to the client as -env+DEVNAME=<value>.",
		},
		{
			Key: SettingCodePage, Label: "Code page", Type: protocol.SettingString,
			Default:     "",
			Description: "Optional EBCDIC code page; passed to the client as -env+CODEPAGE=<value>. Common values: 37 (US/Canada), 1141 (Germany/Austria), 1140 (international).",
		},
		{
			Key: SettingSSL, Label: "Use SSL/TLS", Type: protocol.SettingBool,
			Default:     false,
			Description: "When true, prepend \"ssl:\" to the host argument so the native client establishes a TLS-wrapped TN5250 session.",
		},
		{
			Key: SettingBinary, Label: "Binary override", Type: protocol.SettingString,
			Description: "Explicit path or name of the TN5250 client binary to launch (overrides auto-discovery). On Windows users typically install tn5250j separately.",
		},
		{
			Key: SettingExtraArgs, Label: "Extra arguments", Type: protocol.SettingString,
			Description: "Additional command-line arguments appended verbatim to the native client invocation.",
		},
	}
}

// Capabilities reports the runtime capabilities advertised by the TN5250
// module. The native client owns its own window, so the host renders in
// external mode and cannot proxy resize / send-input events. 5250 sign-on
// happens inside the native terminal, so AuthNone is the only auth method
// the plugin needs to advertise.
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

// config is the validated form of an OpenRequest.
type config struct {
	Host       string
	Port       int
	DeviceName string
	CodePage   string
	SSL        bool
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
// req.Host / req.Port take precedence over settings-map entries when
// non-empty / non-zero, mirroring the rawsocket and rdp plugins' convention.
func configFromRequest(req protocol.OpenRequest) (*config, error) {
	view := settingsView{m: req.Settings}

	host := req.Host
	if host == "" {
		host = view.stringOr(SettingHost, "")
	}
	if host == "" {
		return nil, errors.New("tn5250: host is required")
	}

	port := req.Port
	if port == 0 {
		port = view.intOr(SettingPort, defaultPort)
	}
	if port < 1 || port > 65535 {
		return nil, fmt.Errorf("tn5250: port must be in [1,65535], got %d", port)
	}

	return &config{
		Host:       host,
		Port:       port,
		DeviceName: view.stringOr(SettingDeviceName, ""),
		CodePage:   view.stringOr(SettingCodePage, ""),
		SSL:        view.boolOr(SettingSSL, false),
		Binary:     view.stringOr(SettingBinary, ""),
		ExtraArgs:  view.stringSlice(SettingExtraArgs),
	}, nil
}

// candidatesFor returns the ordered list of binary candidate names to try
// on the given GOOS.
//
// On Windows the canonical client is tn5250j (a Java app launched via a
// .bat shim); some distributions also ship a native tn5250.exe build.
// Windows users who don't have either on PATH must install tn5250j
// separately and either add it to PATH or supply an explicit binary
// override via the "binary" setting.
func candidatesFor(goos string) []string {
	if goos == "windows" {
		return []string{"tn5250j.exe", "tn5250j", "tn5250.exe"}
	}
	return []string{"tn5250", "xt5250"}
}

// hostArg renders the trailing positional argument that points the native
// client at the target. It is "[ssl:]host" when port == defaultPort and
// "[ssl:]host:port" otherwise.
func hostArg(cfg *config) string {
	prefix := ""
	if cfg.SSL {
		prefix = "ssl:"
	}
	if cfg.Port == defaultPort {
		return prefix + cfg.Host
	}
	return prefix + cfg.Host + ":" + strconv.Itoa(cfg.Port)
}

// buildArgvFor renders the argv for the given platform.
//
// Unix (linux/darwin/etc, tn5250 / xt5250):
//
//	tn5250 -env+TERM=IBM-3179-2 \
//	       [-env+DEVNAME=<dev>] \
//	       [-env+CODEPAGE=<cp>] \
//	       [extra_args...] \
//	       [ssl:]host[:port]
//
// Windows (tn5250j / tn5250.exe):
//
//	tn5250j [extra_args...] [ssl:]host[:port]
//
// The 5250 sign-on flow happens inside the native client, so no
// credential material appears on the command line.
func buildArgvFor(goos string, cfg *config) ([]string, error) {
	var args []string

	if goos != "windows" {
		args = append(args, "-env+TERM=IBM-3179-2")
		if cfg.DeviceName != "" {
			args = append(args, "-env+DEVNAME="+cfg.DeviceName)
		}
		if cfg.CodePage != "" {
			args = append(args, "-env+CODEPAGE="+cfg.CodePage)
		}
	} else {
		// tn5250j accepts -DEVNAME=... and -CODEPAGE=... style switches in
		// some builds, but the safest cross-build invocation is just the
		// host argument plus user-supplied extra_args. Device/code-page
		// users on Windows can plumb them through extra_args until a
		// dedicated Windows arg template lands.
		_ = cfg
	}

	if len(cfg.ExtraArgs) > 0 {
		args = append(args, cfg.ExtraArgs...)
	}

	args = append(args, hostArg(cfg))
	return args, nil
}

// resolveArgvAndBinary is shared by Open and the integration test helper.
func (m *Module) resolveArgvAndBinary(cfg *config, goos string) (binary string, args []string, err error) {
	discover := m.discover
	if discover == nil {
		discover = extlaunch.Discover
	}
	binary, err = discover(cfg.Binary, candidatesFor(goos))
	if err != nil {
		return "", nil, fmt.Errorf("tn5250: discover client: %w", err)
	}

	argvFor := m.argvFor
	if argvFor == nil {
		argvFor = buildArgvFor
	}
	args, err = argvFor(goos, cfg)
	if err != nil {
		return "", nil, err
	}
	return binary, args, nil
}

// Open validates settings, discovers a suitable TN5250 client binary, and
// renders the argv. The returned [Session] holds the resolved binary path
// and arguments but does NOT spawn a subprocess until [Session.Start] is
// called.
func (m *Module) Open(ctx context.Context, req protocol.OpenRequest) (protocol.Session, error) {
	cfg, err := configFromRequest(req)
	if err != nil {
		return nil, err
	}

	binary, args, err := m.resolveArgvAndBinary(cfg, runtime.GOOS)
	if err != nil {
		return nil, err
	}
	return newSession(binary, args), nil
}
