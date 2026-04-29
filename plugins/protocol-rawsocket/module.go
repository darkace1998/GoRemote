package rawsocket

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/darkace1998/GoRemote/sdk/plugin"
	"github.com/darkace1998/GoRemote/sdk/protocol"
)

// Setting keys exposed by the Raw Socket plugin.
const (
	SettingHost                  = "host"
	SettingPort                  = "port"
	SettingConnectTimeoutSeconds = "connect_timeout_seconds"
	SettingKeepaliveSeconds      = "keepalive_seconds"
	SettingEOLMode               = "eol_mode"
	SettingEncoding              = "encoding"
)

// EOL modes controlling what SendInput appends when callers send a line that
// does not already end in a newline.
const (
	EOLModeLF   = "lf"
	EOLModeCRLF = "crlf"
	EOLModeNone = "none"
)

// Module is the built-in Raw Socket protocol module.
//
// The zero value is a ready-to-use Module. It is safe for concurrent use;
// [Module.Open] creates an independent [Session] per call.
type Module struct{}

// New returns a ready-to-use [Module].
func New() *Module { return &Module{} }

// Manifest returns the static manifest for this plugin.
func (m *Module) Manifest() plugin.Manifest { return Manifest }

func ptrInt(v int) *int { return &v }

// Settings returns the protocol-specific setting schema. Hosts use this to
// build property editors and to validate connection definitions before
// invoking [Module.Open].
func (m *Module) Settings() []protocol.SettingDef {
	return []protocol.SettingDef{
		{
			Key: SettingHost, Label: "Host", Type: protocol.SettingString,
			Required:    true,
			Description: "Target host name or IP address.",
		},
		{
			Key: SettingPort, Label: "Port", Type: protocol.SettingInt,
			Required:    true,
			Min:         ptrInt(1),
			Max:         ptrInt(65535),
			Description: "TCP port to connect to. No default: raw sockets are service-agnostic.",
		},
		{
			Key: SettingConnectTimeoutSeconds, Label: "Connect timeout (seconds)",
			Type:        protocol.SettingInt,
			Default:     15,
			Min:         ptrInt(1),
			Max:         ptrInt(600),
			Description: "Maximum time to wait for the TCP connection to establish.",
		},
		{
			Key: SettingKeepaliveSeconds, Label: "TCP keepalive interval (seconds)",
			Type:        protocol.SettingInt,
			Default:     0,
			Min:         ptrInt(0),
			Max:         ptrInt(3600),
			Description: "If > 0, enables TCP keepalive with this interval; 0 disables keepalive.",
		},
		{
			Key: SettingEOLMode, Label: "End-of-line mode",
			Type:        protocol.SettingEnum,
			Default:     EOLModeLF,
			EnumValues:  []string{EOLModeLF, EOLModeCRLF, EOLModeNone},
			Description: "Line ending appended by SendInput when the supplied data does not already end in a newline. \"none\" sends bytes verbatim.",
		},
		{
			Key: SettingEncoding, Label: "Character encoding",
			Type:        protocol.SettingEnum,
			Default:     "utf-8",
			EnumValues:  []string{"utf-8", "iso-8859-1"},
			Description: "Advertised encoding for the rendered terminal view. The raw byte stream is transmitted unchanged.",
		},
	}
}

// Capabilities reports the runtime capabilities advertised by the Raw Socket
// module.
func (m *Module) Capabilities() protocol.Capabilities {
	return protocol.Capabilities{
		RenderModes:       []protocol.RenderMode{protocol.RenderTerminal},
		AuthMethods:       []protocol.AuthMethod{protocol.AuthNone},
		SupportsResize:    false,
		SupportsClipboard: false,
		SupportsLogging:   true,
		SupportsReconnect: true,
	}
}

// settingsView is a typed view over the untyped settings map.
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

// Open dials the target over TCP. It honors ctx and the configured connect
// timeout; whichever fires first aborts the dial. On success, the returned
// [Session] owns the underlying connection until [Session.Close] is called.
func (m *Module) Open(ctx context.Context, req protocol.OpenRequest) (protocol.Session, error) {
	view := settingsView{m: req.Settings}

	host := req.Host
	if host == "" {
		host = view.stringOr(SettingHost, "")
	}
	if host == "" {
		return nil, errors.New("rawsocket: host is required")
	}

	port := req.Port
	if port == 0 {
		port = view.intOr(SettingPort, 0)
	}
	if port < 1 || port > 65535 {
		return nil, fmt.Errorf("rawsocket: port must be in [1,65535], got %d", port)
	}

	connectTimeout := time.Duration(view.intOr(SettingConnectTimeoutSeconds, 15)) * time.Second
	keepalive := time.Duration(view.intOr(SettingKeepaliveSeconds, 0)) * time.Second

	eolMode := view.stringOr(SettingEOLMode, EOLModeLF)
	switch eolMode {
	case EOLModeLF, EOLModeCRLF, EOLModeNone:
	default:
		return nil, fmt.Errorf("rawsocket: invalid eol_mode %q", eolMode)
	}

	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	d := net.Dialer{Timeout: connectTimeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("rawsocket: dial %s: %w", addr, err)
	}

	if tcp, ok := conn.(*net.TCPConn); ok && keepalive > 0 {
		_ = tcp.SetKeepAlive(true)
		_ = tcp.SetKeepAlivePeriod(keepalive)
	}

	return newSession(conn, eolMode), nil
}
