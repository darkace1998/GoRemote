package mosh

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strconv"

	"github.com/goremote/goremote/internal/extlaunch"
	"github.com/goremote/goremote/sdk/plugin"
	"github.com/goremote/goremote/sdk/protocol"
)

// Setting keys exposed by the MOSH plugin.
const (
	SettingHost      = "host"
	SettingPort      = "port"
	SettingUsername  = "username"
	SettingMoshPort  = "mosh_port"
	SettingBinary    = "binary"
	SettingSSHArgs   = "ssh_args"
	SettingExtraArgs = "extra_args"
)

// Defaults applied when the corresponding setting is unset.
const (
	defaultPort = 22
)

func ptrInt(v int) *int { return &v }

// Module is the built-in MOSH protocol module.
//
// Module is safe for concurrent use; each [Module.Open] call yields an
// independent [Session] that owns its own subprocess.
type Module struct {
	// discover, when non-nil, replaces extlaunch.Discover. It exists for
	// tests so they can substitute a binary that's actually executable in
	// CI without depending on a real mosh client being installed.
	discover func(override string, candidates []string) (string, error)

	// argvFor, when non-nil, replaces buildArgv. It exists so tests can
	// substitute a trivial argv that prints a known marker line and exits
	// instead of attempting an actual mosh handshake.
	argvFor func(cfg *config) ([]string, error)
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
			Key: SettingPort, Label: "SSH Port", Type: protocol.SettingInt,
			Default:     defaultPort,
			Min:         ptrInt(1),
			Max:         ptrInt(65535),
			Description: "SSH port used for the initial mosh handshake.",
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
			Key: SettingBinary, Label: "Binary override", Type: protocol.SettingString,
			Description: "Explicit path or name of the mosh binary to launch (overrides auto-discovery).",
		},
		{
			Key: SettingSSHArgs, Label: "Extra SSH arguments", Type: protocol.SettingString,
			Description: "Extra SSH arguments passed via --ssh=\"ssh <args>\".",
		},
		{
			Key: SettingExtraArgs, Label: "Extra arguments", Type: protocol.SettingString,
			Description: "Additional command-line arguments appended verbatim to the mosh invocation.",
		},
	}
}

// Capabilities reports the runtime capabilities advertised by the MOSH
// module. The mosh client owns its own terminal window, so the host renders
// in external mode and cannot proxy resize / send-input events.
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
	Host      string
	Port      int
	Username  string
	MoshPort  int
	Binary    string
	SSHArgs   string
	ExtraArgs []string
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

	moshPort := view.intOr(SettingMoshPort, 0)
	if moshPort < 0 || moshPort > 65535 {
		return nil, fmt.Errorf("mosh: mosh_port must be in [0,65535], got %d", moshPort)
	}

	return &config{
		Host:      host,
		Port:      port,
		Username:  username,
		MoshPort:  moshPort,
		Binary:    view.stringOr(SettingBinary, ""),
		SSHArgs:   view.stringOr(SettingSSHArgs, ""),
		ExtraArgs: view.stringSlice(SettingExtraArgs),
	}, nil
}

// candidatesFor returns the ordered list of binary candidate names to try.
func candidatesFor(_ string) []string {
	return []string{"mosh"}
}

// buildArgv renders the argv for a mosh invocation.
//
// The resulting argv follows the form:
//
//	mosh [--port=N] [--ssh="ssh [-p PORT] [SSHArgs]"] [user@]host [ExtraArgs...]
func buildArgv(cfg *config) ([]string, error) {
	vars := extlaunch.Vars{
		"host":     cfg.Host,
		"username": cfg.Username,
	}

	var template []string

	if cfg.MoshPort != 0 {
		template = append(template, "--port="+strconv.Itoa(cfg.MoshPort))
	}

	if cfg.Port != defaultPort || cfg.SSHArgs != "" {
		sshCmd := "ssh"
		if cfg.Port != defaultPort {
			sshCmd += " -p " + strconv.Itoa(cfg.Port)
		}
		if cfg.SSHArgs != "" {
			sshCmd += " " + cfg.SSHArgs
		}
		template = append(template, "--ssh="+sshCmd)
	}

	if cfg.Username != "" {
		template = append(template, "{username}@{host}")
	} else {
		template = append(template, "{host}")
	}

	args, err := extlaunch.Build(template, vars)
	if err != nil {
		return nil, fmt.Errorf("mosh: build argv: %w", err)
	}
	if len(cfg.ExtraArgs) > 0 {
		args = append(args, cfg.ExtraArgs...)
	}
	return args, nil
}

// Open validates settings, discovers the mosh binary, and renders the argv.
// The returned [Session] holds the resolved binary path and arguments but
// does NOT spawn a subprocess until [Session.Start] is called.
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
		return nil, fmt.Errorf("mosh: discover client: %w", err)
	}

	argvFor := m.argvFor
	if argvFor == nil {
		argvFor = buildArgv
	}
	args, err := argvFor(cfg)
	if err != nil {
		return nil, err
	}

	return newSession(binary, args), nil
}
