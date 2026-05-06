package tn5250

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"

	"github.com/darkace1998/GoRemote/sdk/plugin"
	"github.com/darkace1998/GoRemote/sdk/protocol"
)

// Setting keys exposed by the TN5250 plugin.
const (
	SettingHost       = "host"
	SettingPort       = "port"
	SettingDeviceName = "device_name"
	SettingCodePage   = "code_page"
)

// defaultPort is the standard TN5250 / Telnet port.
const defaultPort = 23

func ptrInt(v int) *int { return &v }

// Module is the built-in TN5250 protocol module.
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
			Description: "Target IBM i / AS400 host name or IP address.",
		},
		{
			Key: SettingPort, Label: "Port", Type: protocol.SettingInt,
			Default:     defaultPort,
			Min:         ptrInt(1),
			Max:         ptrInt(65535),
			Description: "TCP port the TN5250 service is listening on.",
		},
		{
			Key: SettingDeviceName, Label: "Device name", Type: protocol.SettingString,
			Description: "Optional 5250 device name sent during terminal negotiation.",
		},
		{
			Key: SettingCodePage, Label: "Code page", Type: protocol.SettingString,
			Default:     "",
			Description: "Optional EBCDIC code page. Common values: 37 (US/Canada), 1141 (Germany/Austria), 1140 (international).",
		},
	}
}

// Capabilities reports the runtime capabilities advertised by the TN5250 module.
func (m *Module) Capabilities() protocol.Capabilities {
	return protocol.Capabilities{
		RenderModes:       []protocol.RenderMode{protocol.RenderTerminal},
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
	}, nil
}

// hostArg renders the address used to connect to the remote. It returns
// "host" when port == defaultPort and "host:port" otherwise.
func hostArg(cfg *config) string {
	if cfg.Port == defaultPort {
		return cfg.Host
	}
	return cfg.Host + ":" + strconv.Itoa(cfg.Port)
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
