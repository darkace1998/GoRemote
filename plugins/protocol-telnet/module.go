package telnet

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/darkace1998/GoRemote/sdk/plugin"
	"github.com/darkace1998/GoRemote/sdk/protocol"
)

// Module is the built-in Telnet protocol module.
type Module struct{}

// New returns a ready-to-use Telnet module.
func New() *Module { return &Module{} }

// Compile-time assertion: Module implements protocol.Module.
var _ protocol.Module = (*Module)(nil)

// Manifest returns the static plugin manifest.
func (m *Module) Manifest() plugin.Manifest { return Manifest }

// Settings returns the user-facing settings schema for Telnet connections.
func (m *Module) Settings() []protocol.SettingDef {
	minPort, maxPort := 1, 65535
	minTimeout, maxTimeout := 1, 600
	minKeep, maxKeep := 0, 86400
	return []protocol.SettingDef{
		{Key: "host", Label: "Host", Type: protocol.SettingString, Required: true, Description: "Target host or address."},
		{Key: "port", Label: "Port", Type: protocol.SettingInt, Default: 23, Min: &minPort, Max: &maxPort, Description: "TCP port (default 23)."},
		{Key: "username", Label: "Username", Type: protocol.SettingString, Description: "Username sent when AuthPassword is used. Telnet has no in-band username prompt; we match the server's 'login:' prompt heuristically."},
		{Key: "terminal_type", Label: "Terminal Type", Type: protocol.SettingString, Default: "xterm-256color", Description: "TTYPE advertised via RFC 1091 subnegotiation."},
		{Key: "encoding", Label: "Encoding", Type: protocol.SettingEnum, Default: "utf-8", EnumValues: []string{"utf-8", "iso-8859-1"}, Description: "Advisory encoding label for the session. No byte-level conversion is performed; the terminal renderer is expected to honor it."},
		{Key: "connect_timeout_seconds", Label: "Connect Timeout (s)", Type: protocol.SettingInt, Default: 15, Min: &minTimeout, Max: &maxTimeout, Description: "Dial timeout in seconds."},
		{Key: "keepalive_seconds", Label: "TCP Keepalive (s)", Type: protocol.SettingInt, Default: 0, Min: &minKeep, Max: &maxKeep, Description: "TCP keepalive interval in seconds; 0 disables keepalives."},
	}
}

// Capabilities reports the protocol-level capabilities this module supports.
func (m *Module) Capabilities() protocol.Capabilities {
	return protocol.Capabilities{
		RenderModes: []protocol.RenderMode{protocol.RenderTerminal},
		AuthMethods: []protocol.AuthMethod{
			protocol.AuthNone,
			// AuthPassword is INSECURE: credentials are sent in cleartext as
			// literal keystrokes after matching the server's "login:" and
			// "password:" prompts. Use SSH for anything sensitive.
			protocol.AuthPassword,
		},
		SupportsResize:  true,
		SupportsLogging: true,
	}
}

// Open dials the target and returns a Session that has not yet started its
// I/O loop. The caller is responsible for invoking Session.Start.
func (m *Module) Open(ctx context.Context, req protocol.OpenRequest) (protocol.Session, error) {
	s, err := resolveSettings(req)
	if err != nil {
		return nil, err
	}

	host := s.host
	if req.Host != "" {
		host = req.Host
	}
	if host == "" {
		return nil, errors.New("telnet: host is required")
	}
	port := s.port
	if req.Port != 0 {
		port = req.Port
	}
	if port <= 0 || port > 65535 {
		return nil, fmt.Errorf("telnet: invalid port %d", port)
	}

	dialer := net.Dialer{Timeout: s.connectTimeout}
	if s.keepalive > 0 {
		dialer.KeepAlive = s.keepalive
	}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return nil, fmt.Errorf("telnet: dial %s:%d: %w", host, port, err)
	}

	cols, rows := req.InitialSize.Cols, req.InitialSize.Rows
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}

	auth := req.AuthMethod
	if auth == "" {
		auth = protocol.AuthNone
	}
	sess := &Session{
		neg:      NewNegotiator(conn, s.termType, cols, rows),
		auth:     auth,
		username: firstNonEmpty(req.Username, req.Secret.Username, s.username),
		password: req.Secret.Password,
	}
	return sess, nil
}

// settings is the parsed, validated subset of a request's settings map.
type settings struct {
	host           string
	port           int
	username       string
	termType       string
	encoding       string
	connectTimeout time.Duration
	keepalive      time.Duration
}

func resolveSettings(req protocol.OpenRequest) (settings, error) {
	s := settings{
		port:           23,
		termType:       "xterm-256color",
		encoding:       "utf-8",
		connectTimeout: 15 * time.Second,
	}
	m := req.Settings
	if v, ok := stringSetting(m, "host"); ok {
		s.host = v
	}
	if v, ok := intSetting(m, "port"); ok {
		s.port = v
	}
	if v, ok := stringSetting(m, "username"); ok {
		s.username = v
	}
	if v, ok := stringSetting(m, "terminal_type"); ok && v != "" {
		s.termType = v
	}
	if v, ok := stringSetting(m, "encoding"); ok && v != "" {
		switch v {
		case "utf-8", "iso-8859-1":
			s.encoding = v
		default:
			return s, fmt.Errorf("telnet: unsupported encoding %q", v)
		}
	}
	if v, ok := intSetting(m, "connect_timeout_seconds"); ok && v > 0 {
		s.connectTimeout = time.Duration(v) * time.Second
	}
	if v, ok := intSetting(m, "keepalive_seconds"); ok && v > 0 {
		s.keepalive = time.Duration(v) * time.Second
	}
	return s, nil
}

func stringSetting(m map[string]any, key string) (string, bool) {
	if m == nil {
		return "", false
	}
	v, ok := m[key]
	if !ok {
		return "", false
	}
	switch x := v.(type) {
	case string:
		return x, true
	case fmt.Stringer:
		return x.String(), true
	}
	return "", false
}

func intSetting(m map[string]any, key string) (int, bool) {
	if m == nil {
		return 0, false
	}
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	switch x := v.(type) {
	case int:
		return x, true
	case int32:
		return int(x), true
	case int64:
		return int(x), true
	case float64:
		return int(x), true
	case string:
		n, err := strconv.Atoi(x)
		if err != nil {
			return 0, false
		}
		return n, true
	}
	return 0, false
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
