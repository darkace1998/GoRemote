package rlogin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os/user"
	"strconv"
	"time"

	"github.com/darkace1998/GoRemote/sdk/plugin"
	"github.com/darkace1998/GoRemote/sdk/protocol"
)

// Module is the built-in rlogin protocol module.
type Module struct{}

// New returns a ready-to-use rlogin module.
func New() *Module { return &Module{} }

// Compile-time assertion: *Module implements protocol.Module.
var _ protocol.Module = (*Module)(nil)

// Manifest returns the static plugin manifest.
func (m *Module) Manifest() plugin.Manifest { return Manifest }

// Settings returns the user-facing settings schema for rlogin connections.
func (m *Module) Settings() []protocol.SettingDef {
	minPort, maxPort := 1, 65535
	minTimeout, maxTimeout := 1, 600
	minSpeed, maxSpeed := 50, 4000000
	defClient := ""
	if u, err := user.Current(); err == nil {
		defClient = u.Username
	}
	return []protocol.SettingDef{
		{Key: "host", Label: "Host", Type: protocol.SettingString, Required: true, Description: "Target host or address."},
		{Key: "port", Label: "Port", Type: protocol.SettingInt, Default: 513, Min: &minPort, Max: &maxPort, Description: "TCP port (default 513)."},
		{Key: "username", Label: "Server Username", Type: protocol.SettingString, Required: true, Description: "Remote user to log in as (server_user field in the RFC 1282 handshake)."},
		{Key: "client_username", Label: "Client Username", Type: protocol.SettingString, Default: defClient, Description: "Local user name advertised to the server (client_user field). Defaults to the current OS user."},
		{Key: "terminal_type", Label: "Terminal Type", Type: protocol.SettingString, Default: "xterm-256color", Description: "Terminal type sent in the <terminal_type>/<speed> handshake field."},
		{Key: "terminal_speed", Label: "Terminal Speed", Type: protocol.SettingInt, Default: 38400, Min: &minSpeed, Max: &maxSpeed, Description: "Terminal speed (baud) sent in the <terminal_type>/<speed> handshake field."},
		{Key: "connect_timeout_seconds", Label: "Connect Timeout (s)", Type: protocol.SettingInt, Default: 15, Min: &minTimeout, Max: &maxTimeout, Description: "Dial timeout in seconds."},
		{Key: "encoding", Label: "Encoding", Type: protocol.SettingEnum, Default: "utf-8", EnumValues: []string{"utf-8", "iso-8859-1"}, Description: "Advisory encoding label. No byte-level conversion is performed."},
	}
}

// Capabilities reports the protocol-level capabilities this module supports.
func (m *Module) Capabilities() protocol.Capabilities {
	return protocol.Capabilities{
		RenderModes:     []protocol.RenderMode{protocol.RenderTerminal},
		AuthMethods:     []protocol.AuthMethod{protocol.AuthNone},
		SupportsResize:  true,
		SupportsLogging: true,
	}
}

// settings is the parsed, validated subset of a request's settings map.
type settings struct {
	host           string
	port           int
	serverUser     string
	clientUser     string
	termType       string
	termSpeed      int
	connectTimeout time.Duration
	encoding       string
}

func resolveSettings(req protocol.OpenRequest) (settings, error) {
	s := settings{
		port:           513,
		termType:       "xterm-256color",
		termSpeed:      38400,
		connectTimeout: 15 * time.Second,
		encoding:       "utf-8",
	}
	if u, err := user.Current(); err == nil {
		s.clientUser = u.Username
	}
	m := req.Settings
	if v, ok := stringSetting(m, "host"); ok {
		s.host = v
	}
	if v, ok := intSetting(m, "port"); ok {
		s.port = v
	}
	if v, ok := stringSetting(m, "username"); ok {
		s.serverUser = v
	}
	if v, ok := stringSetting(m, "client_username"); ok {
		s.clientUser = v
	}
	if v, ok := stringSetting(m, "terminal_type"); ok && v != "" {
		s.termType = v
	}
	if v, ok := intSetting(m, "terminal_speed"); ok && v > 0 {
		s.termSpeed = v
	}
	if v, ok := intSetting(m, "connect_timeout_seconds"); ok && v > 0 {
		s.connectTimeout = time.Duration(v) * time.Second
	}
	if v, ok := stringSetting(m, "encoding"); ok && v != "" {
		switch v {
		case "utf-8", "iso-8859-1":
			s.encoding = v
		default:
			return s, fmt.Errorf("rlogin: unsupported encoding %q", v)
		}
	}
	return s, nil
}

// buildHandshake returns the RFC 1282 initial handshake bytes:
//
//	0x00, <client_user> 0x00, <server_user> 0x00, <term>/<speed> 0x00
//
// It rejects embedded NUL bytes in any user-supplied field.
func buildHandshake(clientUser, serverUser, termType string, termSpeed int) ([]byte, error) {
	for name, v := range map[string]string{
		"client_username": clientUser,
		"username":        serverUser,
		"terminal_type":   termType,
	} {
		if bytes.IndexByte([]byte(v), 0) >= 0 {
			return nil, fmt.Errorf("rlogin: %s contains NUL byte", name)
		}
	}
	if serverUser == "" {
		return nil, errors.New("rlogin: server username is required")
	}
	if termType == "" {
		return nil, errors.New("rlogin: terminal_type is required")
	}
	if termSpeed <= 0 {
		return nil, fmt.Errorf("rlogin: invalid terminal_speed %d", termSpeed)
	}
	var buf bytes.Buffer
	buf.WriteByte(0x00)
	buf.WriteString(clientUser)
	buf.WriteByte(0x00)
	buf.WriteString(serverUser)
	buf.WriteByte(0x00)
	buf.WriteString(termType)
	buf.WriteByte('/')
	buf.WriteString(strconv.Itoa(termSpeed))
	buf.WriteByte(0x00)
	return buf.Bytes(), nil
}

// Open dials the target, performs the RFC 1282 handshake, and returns a
// Session ready to have Start invoked on it.
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
		return nil, errors.New("rlogin: host is required")
	}
	port := s.port
	if req.Port != 0 {
		port = req.Port
	}
	if port <= 0 || port > 65535 {
		return nil, fmt.Errorf("rlogin: invalid port %d", port)
	}

	serverUser := s.serverUser
	if req.Username != "" {
		serverUser = req.Username
	}
	if serverUser == "" && req.Secret.Username != "" {
		serverUser = req.Secret.Username
	}
	if serverUser == "" {
		return nil, errors.New("rlogin: server username is required")
	}

	handshake, err := buildHandshake(s.clientUser, serverUser, s.termType, s.termSpeed)
	if err != nil {
		return nil, err
	}

	dialer := net.Dialer{Timeout: s.connectTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return nil, fmt.Errorf("rlogin: dial %s:%d: %w", host, port, err)
	}

	// Honor ctx and connect timeout across the whole handshake, not just dial.
	deadline := time.Now().Add(s.connectTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)

	if _, err := conn.Write(handshake); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("rlogin: writing handshake: %w", err)
	}

	ack := make([]byte, 1)
	if _, err := readFull(conn, ack); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("rlogin: reading ACK: %w", err)
	}
	if ack[0] != 0x00 {
		_ = conn.Close()
		return nil, fmt.Errorf("rlogin: server returned non-zero ACK byte 0x%02x", ack[0])
	}

	// Clear deadline for the long-running session.
	_ = conn.SetDeadline(time.Time{})

	return newSession(conn), nil
}

// readFull reads exactly len(buf) bytes or returns an error.
func readFull(c net.Conn, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := c.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
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
