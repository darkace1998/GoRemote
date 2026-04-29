package powershell

import (
	"context"
	"fmt"

	"github.com/darkace1998/GoRemote/sdk/plugin"
	"github.com/darkace1998/GoRemote/sdk/protocol"
)

// Setting keys exposed by the PowerShell plugin.
const (
	SettingBinary = "binary"
	SettingCWD    = "cwd"
	SettingArgs   = "args"
	SettingEnv    = "env"
	SettingCols   = "cols"
	SettingRows   = "rows"
)

// Default initial PTY geometry when the caller does not supply one via
// OpenRequest.InitialSize or the cols/rows settings.
const (
	defaultCols = 120
	defaultRows = 40
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

// Settings returns the protocol-specific setting schema. Hosts use this to
// build property editors and to validate connection definitions before
// invoking [Module.Open].
func (m *Module) Settings() []protocol.SettingDef {
	return []protocol.SettingDef{
		{
			Key: SettingBinary, Label: "PowerShell binary", Type: protocol.SettingString,
			Description: "Explicit path or name of the PowerShell host to launch (overrides auto-discovery).",
		},
		{
			Key: SettingCWD, Label: "Working directory", Type: protocol.SettingString,
			Description: "Initial working directory for the PowerShell process. Defaults to the host process cwd.",
		},
		{
			Key: SettingArgs, Label: "Extra arguments", Type: protocol.SettingString,
			Description: "Additional command-line arguments appended after \"-NoLogo -NoProfile -Interactive\".",
		},
		{
			Key: SettingEnv, Label: "Environment variables", Type: protocol.SettingString,
			Description: "Extra environment variables (key=value) merged onto the inherited environment.",
		},
		{
			Key: SettingCols, Label: "Initial columns", Type: protocol.SettingInt,
			Default:     defaultCols,
			Description: "Initial PTY width in columns.",
		},
		{
			Key: SettingRows, Label: "Initial rows", Type: protocol.SettingInt,
			Default:     defaultRows,
			Description: "Initial PTY height in rows.",
		},
	}
}

// Capabilities reports the runtime capabilities advertised by the PowerShell
// module. Authentication is delegated to the OS (the spawned process
// inherits the caller's identity), so AuthNone is the only auth method.
func (m *Module) Capabilities() protocol.Capabilities {
	return protocol.Capabilities{
		RenderModes:       []protocol.RenderMode{protocol.RenderTerminal},
		AuthMethods:       []protocol.AuthMethod{protocol.AuthNone},
		SupportsResize:    true,
		SupportsClipboard: false,
		SupportsLogging:   true,
		SupportsReconnect: false,
	}
}

// settingsView is a typed view over the untyped settings map shared by the
// rawsocket plugin pattern. Kept package-local because the type assertions
// here understand only the keys this plugin advertises.
type settingsView struct {
	m map[string]any
}

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

func (s settingsView) stringMap(key string) map[string]string {
	v, ok := s.m[key]
	if !ok {
		return nil
	}
	switch x := v.(type) {
	case map[string]string:
		out := make(map[string]string, len(x))
		for k, v := range x {
			out[k] = v
		}
		return out
	case map[string]any:
		out := make(map[string]string, len(x))
		for k, v := range x {
			if str, ok := v.(string); ok {
				out[k] = str
			}
		}
		return out
	}
	return nil
}

// openConfig is the resolved, plugin-internal view of an OpenRequest.
type openConfig struct {
	binary string
	cwd    string
	args   []string
	env    map[string]string
	cols   int
	rows   int
}

// resolveConfig validates the OpenRequest and produces an openConfig with
// defaults applied. It does not perform any side effects (no process spawn,
// no PATH lookup).
func resolveConfig(req protocol.OpenRequest) (openConfig, error) {
	view := settingsView{m: req.Settings}

	cfg := openConfig{
		binary: view.stringOr(SettingBinary, ""),
		cwd:    view.stringOr(SettingCWD, ""),
		args:   view.stringSlice(SettingArgs),
		env:    view.stringMap(SettingEnv),
		cols:   view.intOr(SettingCols, defaultCols),
		rows:   view.intOr(SettingRows, defaultRows),
	}

	if req.InitialSize.Cols > 0 {
		cfg.cols = req.InitialSize.Cols
	}
	if req.InitialSize.Rows > 0 {
		cfg.rows = req.InitialSize.Rows
	}
	if cfg.cols <= 0 {
		cfg.cols = defaultCols
	}
	if cfg.rows <= 0 {
		cfg.rows = defaultRows
	}
	if cfg.cols > 0xFFFF || cfg.rows > 0xFFFF {
		return cfg, fmt.Errorf("powershell: cols/rows out of range (cols=%d rows=%d)", cfg.cols, cfg.rows)
	}
	return cfg, nil
}

// Open spawns a PowerShell host inside a pseudo-terminal. The returned
// [Session] owns the child process and PTY until Close is called.
func (m *Module) Open(ctx context.Context, req protocol.OpenRequest) (protocol.Session, error) {
	cfg, err := resolveConfig(req)
	if err != nil {
		return nil, err
	}
	return openSession(ctx, cfg)
}
