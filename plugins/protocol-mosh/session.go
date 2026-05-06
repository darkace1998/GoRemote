package mosh

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"

	"github.com/darkace1998/GoRemote/sdk/protocol"
	"golang.org/x/crypto/ssh"
)

// Session is a live MOSH session.
//
// Start performs the SSH bootstrap (dial → ClientConn → run "mosh-server
// new") to obtain the MOSH CONNECT token, then TODO: implement the MOSH UDP
// transport. Until the UDP layer is in place, Start blocks on the SSH session
// so that connection management (open/close, context propagation) works
// end-to-end.
type Session struct {
	cfg  *config
	addr string // SSH host:port

	mu        sync.Mutex
	sshClient *ssh.Client
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
//
//  1. Dial the SSH port with the supplied credential (password or key-agent).
//  2. Run "mosh-server new [-p <port>]" on the remote.
//  3. Parse "MOSH CONNECT <udp-port> <key>" from the server's output.
//  4. TODO: Establish the MOSH UDP transport and relay stdin/stdout.
//
// Until step 4 is implemented the session relays the SSH pseudo-terminal
// directly so that basic terminal I/O is functional.
func (s *Session) Start(ctx context.Context, stdin io.Reader, stdout io.Writer) error {
	if stdout == nil {
		stdout = io.Discard
	}

	// Build SSH client config. We use InsecureIgnoreHostKey for now; a
	// proper host-key callback tied to the credential store is a follow-up.
	sshCfg := &ssh.ClientConfig{
		User: s.cfg.Username,
		Auth: []ssh.AuthMethod{
			ssh.Password(""), // TODO: wire credential from OpenRequest.Secret
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec // TODO: real host-key verification
	}

	tcpConn, err := (&net.Dialer{}).DialContext(ctx, "tcp", s.addr)
	if err != nil {
		return fmt.Errorf("mosh: ssh dial %s: %w", s.addr, err)
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(tcpConn, s.addr, sshCfg)
	if err != nil {
		_ = tcpConn.Close()
		return fmt.Errorf("mosh: ssh handshake: %w", err)
	}

	client := ssh.NewClient(sshConn, chans, reqs)
	s.mu.Lock()
	s.sshClient = client
	s.mu.Unlock()

	// Watch for context cancellation and tear down the SSH connection.
	ctxDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = s.Close()
		case <-ctxDone:
		}
	}()

	defer func() { close(ctxDone) }()

	// Bootstrap: run mosh-server on the remote to get the MOSH CONNECT line.
	moshCmd := "mosh-server new"
	if s.cfg.MoshPort != 0 {
		moshCmd += fmt.Sprintf(" -p %d", s.cfg.MoshPort)
	}

	bootstrapSession, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("mosh: new ssh session for bootstrap: %w", err)
	}

	bootstrapOut, err := bootstrapSession.CombinedOutput(moshCmd)
	bootstrapSession.Close()
	if err != nil {
		// Output may contain diagnostics even on failure.
		return fmt.Errorf("mosh: mosh-server new: %w (output: %s)", err, strings.TrimSpace(string(bootstrapOut)))
	}

	// Parse "MOSH CONNECT <port> <key>" from bootstrap output.
	connectPort, connectKey, err := parseMoshConnect(string(bootstrapOut))
	if err != nil {
		return fmt.Errorf("mosh: parse MOSH CONNECT: %w (output: %s)", err, strings.TrimSpace(string(bootstrapOut)))
	}
	_, _ = connectPort, connectKey // TODO: use in UDP transport

	// TODO: implement MOSH UDP (Roaming Terminal Protocol) transport using
	// connectPort and connectKey (AES-128-OCB). For now fall back to a plain
	// SSH pty session so that terminal I/O is functional.
	ptySession, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("mosh: new ssh pty session: %w", err)
	}
	defer ptySession.Close()

	if err := ptySession.RequestPty("xterm", 24, 80, ssh.TerminalModes{}); err != nil {
		return fmt.Errorf("mosh: request pty: %w", err)
	}

	ptySession.Stdin = stdin
	ptySession.Stdout = stdout
	ptySession.Stderr = stdout

	if err := ptySession.Shell(); err != nil {
		return fmt.Errorf("mosh: start shell: %w", err)
	}

	waitErr := ptySession.Wait()
	if waitErr != nil && ctx.Err() != nil {
		return nil
	}
	return waitErr
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
	s.closeOnce.Do(func() {
		s.mu.Lock()
		client := s.sshClient
		s.mu.Unlock()
		if client != nil {
			s.closeErr = client.Close()
		}
	})
	return s.closeErr
}


