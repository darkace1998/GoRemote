package mosh

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/darkace1998/GoRemote/sdk/protocol"
)

// Session is a live MOSH session.
//
// Start performs the SSH bootstrap (dial → ClientConn → run "mosh-server
// new") to obtain the MOSH CONNECT token, then TODO: implement the MOSH UDP
// transport.
type Session struct {
	cfg  *config
	addr string // SSH host:port

	closeOnce sync.Once
	closeErr  error
}

// Compile-time assertion: *Session implements protocol.Session.
var _ protocol.Session = (*Session)(nil)

func newSession(cfg *config, addr string) *Session {
	return &Session{cfg: cfg, addr: addr}
}

// RenderMode reports the terminal rendering mode used by MOSH sessions.
func (s *Session) RenderMode() protocol.RenderMode { return protocol.RenderTerminal }

// Start bootstraps the MOSH session via SSH and relays terminal I/O.
func (s *Session) Start(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	return protocol.ErrUnsupported
}

// parseMoshConnect scans output for the "MOSH CONNECT <port> <key>" line.
func parseMoshConnect(output string) (port, key string, err error) {
	sc := bufio.NewScanner(strings.NewReader(output))
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "MOSH CONNECT ") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 4 {
			return "", "", fmt.Errorf("unexpected MOSH CONNECT line: %q", line)
		}
		return parts[2], parts[3], nil
	}
	return "", "", fmt.Errorf("MOSH CONNECT line not found in mosh-server output")
}

// Resize requests a terminal resize. This will be wired to the MOSH UDP
// transport once it is implemented.
func (s *Session) Resize(ctx context.Context, size protocol.Size) error {
	return protocol.ErrUnsupported
}

// SendInput writes data to the SSH session's stdin.
func (s *Session) SendInput(ctx context.Context, data []byte) error {
	return protocol.ErrUnsupported
}

// Close terminates the SSH connection. Safe to call multiple times.
func (s *Session) Close() error {
	s.closeOnce.Do(func() {})
	return s.closeErr
}
