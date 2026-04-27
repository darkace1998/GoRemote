package ssh

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/goremote/goremote/sdk/plugin"
	"github.com/goremote/goremote/sdk/protocol"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

// Setting keys exposed by the SSH plugin.
const (
	SettingHost                  = "host"
	SettingPort                  = "port"
	SettingUsername              = "username"
	SettingKnownHostsPath        = "known_hosts_path"
	SettingStrictHostKeyChecking = "strict_host_key_checking"
	SettingKeepaliveSeconds      = "keepalive_seconds"
	SettingEncoding              = "encoding"
	SettingConnectTimeoutSeconds = "connect_timeout_seconds"
	SettingX11Forwarding         = "x11_forwarding"
	SettingAgentForwarding       = "agent_forwarding"
	SettingPTYTerm               = "pty_term"
)

// Strict host-key checking values.
const (
	StrictOff       = "off"
	StrictAcceptNew = "accept-new"
	StrictStrict    = "strict"
)

// Default port used when [protocol.OpenRequest.Port] is zero.
const DefaultPort = 22

// Module is the built-in SSH protocol module.
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
			Required: true, Description: "Target host name or IP address.",
		},
		{
			Key: SettingPort, Label: "Port", Type: protocol.SettingInt,
			Default: DefaultPort, Min: ptrInt(1), Max: ptrInt(65535),
			Description: "TCP port of the remote SSH daemon.",
		},
		{
			Key: SettingUsername, Label: "Username", Type: protocol.SettingString,
			Required: true, Description: "Login user for the SSH session.",
		},
		{
			Key: SettingKnownHostsPath, Label: "Known hosts file",
			Type:        protocol.SettingString,
			Description: "Path to the OpenSSH known_hosts file. Defaults to ~/.ssh/known_hosts.",
		},
		{
			Key: SettingStrictHostKeyChecking, Label: "Strict host key checking",
			Type:        protocol.SettingEnum,
			Default:     StrictAcceptNew,
			EnumValues:  []string{StrictAcceptNew, StrictStrict, StrictOff},
			Description: "accept-new: trust and remember unknown hosts. strict: refuse unknown hosts. off: skip verification.",
		},
		{
			Key: SettingKeepaliveSeconds, Label: "Keepalive interval (seconds)",
			Type:        protocol.SettingInt,
			Default:     30,
			Min:         ptrInt(0),
			Max:         ptrInt(3600),
			Description: "Interval between SSH keepalive requests. 0 disables keepalive.",
		},
		{
			Key: SettingEncoding, Label: "Character encoding",
			Type:        protocol.SettingEnum,
			Default:     "utf-8",
			EnumValues:  []string{"utf-8", "iso-8859-1"},
			Description: "Terminal character encoding for the rendered session.",
		},
		{
			Key: SettingConnectTimeoutSeconds, Label: "Connect timeout (seconds)",
			Type: protocol.SettingInt, Default: 15, Min: ptrInt(1), Max: ptrInt(600),
			Description: "Maximum time to wait for TCP + SSH handshake.",
		},
		{
			Key: SettingX11Forwarding, Label: "X11 forwarding",
			Type: protocol.SettingBool, Default: false,
			Description: "Request X11 forwarding for the session.",
		},
		{
			Key: SettingAgentForwarding, Label: "Agent forwarding",
			Type: protocol.SettingBool, Default: false,
			Description: "Forward the local SSH agent to the remote session.",
		},
		{
			Key: SettingPTYTerm, Label: "TERM value",
			Type: protocol.SettingString, Default: "xterm-256color",
			Description: "TERM environment variable advertised to the remote PTY.",
		},
	}
}

// Capabilities reports the runtime capabilities advertised by the SSH module.
func (m *Module) Capabilities() protocol.Capabilities {
	return protocol.Capabilities{
		RenderModes: []protocol.RenderMode{protocol.RenderTerminal},
		AuthMethods: []protocol.AuthMethod{
			protocol.AuthPassword,
			protocol.AuthPublicKey,
			protocol.AuthAgent,
			protocol.AuthInteractive,
		},
		SupportsResize:    true,
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

func (s settingsView) boolOr(key string, def bool) bool {
	if v, ok := s.m[key]; ok {
		if x, ok := v.(bool); ok {
			return x
		}
	}
	return def
}

// Open dials the target and negotiates an interactive SSH session. It honors
// ctx for both the dial and the SSH handshake; if ctx is cancelled midway,
// any in-flight connection is torn down before the function returns.
func (m *Module) Open(ctx context.Context, req protocol.OpenRequest) (protocol.Session, error) {
	view := settingsView{m: req.Settings}

	host := req.Host
	if host == "" {
		host = view.stringOr(SettingHost, "")
	}
	if host == "" {
		return nil, errors.New("ssh: host is required")
	}
	port := req.Port
	if port == 0 {
		port = view.intOr(SettingPort, DefaultPort)
	}
	username := req.Username
	if username == "" {
		username = view.stringOr(SettingUsername, "")
	}
	if username == "" {
		return nil, errors.New("ssh: username is required")
	}

	connectTimeout := time.Duration(view.intOr(SettingConnectTimeoutSeconds, 15)) * time.Second
	keepalive := time.Duration(view.intOr(SettingKeepaliveSeconds, 30)) * time.Second
	strict := view.stringOr(SettingStrictHostKeyChecking, StrictAcceptNew)
	knownHostsPath := view.stringOr(SettingKnownHostsPath, "")
	ptyTerm := view.stringOr(SettingPTYTerm, "xterm-256color")
	x11 := view.boolOr(SettingX11Forwarding, false)
	agentFwd := view.boolOr(SettingAgentForwarding, false)

	logger := slog.With(
		slog.String("protocol", "ssh"),
		slog.String("host", host),
		slog.Int("port", port),
		slog.String("user", username),
	)

	authMethods, agentCloser, err := buildAuthMethods(req.AuthMethod, req.Secret)
	if err != nil {
		return nil, fmt.Errorf("ssh: build auth: %w", err)
	}

	hostKeyCB, hkCloser, err := buildHostKeyCallback(strict, knownHostsPath)
	if err != nil {
		if agentCloser != nil {
			_ = agentCloser.Close()
		}
		return nil, fmt.Errorf("ssh: host-key callback: %w", err)
	}

	cfg := &ssh.ClientConfig{
		User:            username,
		Auth:            authMethods,
		HostKeyCallback: hostKeyCB,
		Timeout:         connectTimeout,
	}

	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	logger.Info("ssh dialing")

	client, err := dialContext(ctx, addr, connectTimeout, cfg)
	if err != nil {
		if agentCloser != nil {
			_ = agentCloser.Close()
		}
		if hkCloser != nil {
			_ = hkCloser.Close()
		}
		return nil, fmt.Errorf("ssh: dial %s: %w", addr, err)
	}

	// From here, failures must close the client too.
	cleanup := func() {
		_ = client.Close()
		if agentCloser != nil {
			_ = agentCloser.Close()
		}
		if hkCloser != nil {
			_ = hkCloser.Close()
		}
	}

	// If ctx is already cancelled, bail out.
	if err := ctx.Err(); err != nil {
		cleanup()
		return nil, err
	}

	sshSession, err := client.NewSession()
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("ssh: new session: %w", err)
	}

	if agentFwd {
		if err := agent.ForwardToRemote(client, os.Getenv("SSH_AUTH_SOCK")); err == nil {
			_ = agent.RequestAgentForwarding(sshSession)
		} else {
			logger.Warn("agent forwarding unavailable", slog.String("err", err.Error()))
		}
	}

	stdinPipe, err := sshSession.StdinPipe()
	if err != nil {
		_ = sshSession.Close()
		cleanup()
		return nil, fmt.Errorf("ssh: stdin pipe: %w", err)
	}
	stdoutPipe, err := sshSession.StdoutPipe()
	if err != nil {
		_ = sshSession.Close()
		cleanup()
		return nil, fmt.Errorf("ssh: stdout pipe: %w", err)
	}
	stderrPipe, err := sshSession.StderrPipe()
	if err != nil {
		_ = sshSession.Close()
		cleanup()
		return nil, fmt.Errorf("ssh: stderr pipe: %w", err)
	}

	cols, rows := req.InitialSize.Cols, req.InitialSize.Rows
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := sshSession.RequestPty(ptyTerm, rows, cols, modes); err != nil {
		_ = sshSession.Close()
		cleanup()
		return nil, fmt.Errorf("ssh: request pty: %w", err)
	}

	if x11 {
		if err := requestX11(sshSession); err != nil {
			logger.Warn("x11 forwarding unavailable", slog.String("err", err.Error()))
		}
	}

	if err := sshSession.Shell(); err != nil {
		_ = sshSession.Close()
		cleanup()
		return nil, fmt.Errorf("ssh: shell: %w", err)
	}

	s := &Session{
		client:      client,
		session:     sshSession,
		stdin:       stdinPipe,
		stdout:      stdoutPipe,
		stderr:      stderrPipe,
		keepalive:   keepalive,
		logger:      logger,
		agentCloser: agentCloser,
		hkCloser:    hkCloser,
		stopCh:      make(chan struct{}),
	}
	s.startKeepalive()
	logger.Info("ssh session established")
	return s, nil
}

// requestX11 sends an x11-req for the session. We advertise a synthetic
// auth cookie so remote X clients get something to validate against.
func requestX11(sess *ssh.Session) error {
	payload := struct {
		SingleConnection bool
		AuthProto        string
		AuthCookie       string
		ScreenNumber     uint32
	}{false, "MIT-MAGIC-COOKIE-1", "00000000000000000000000000000000", 0}
	ok, err := sess.SendRequest("x11-req", true, ssh.Marshal(&payload))
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("x11-req rejected")
	}
	return nil
}

// buildAuthMethods converts a caller-provided [protocol.AuthMethod] + credential
// material into an x/crypto/ssh auth method list. It may return a closer for
// any side resources (e.g. the SSH agent socket connection) that the caller
// owns for the lifetime of the session.
func buildAuthMethods(method protocol.AuthMethod, secret protocol.CredentialMaterial) ([]ssh.AuthMethod, sessionCloser, error) {
	switch method {
	case protocol.AuthPassword:
		return []ssh.AuthMethod{ssh.Password(secret.Password)}, nil, nil

	case protocol.AuthPublicKey:
		if len(secret.PrivateKey) == 0 {
			return nil, nil, errors.New("publickey: private key is empty")
		}
		var (
			signer ssh.Signer
			err    error
		)
		if secret.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(secret.PrivateKey, []byte(secret.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey(secret.PrivateKey)
		}
		if err != nil {
			return nil, nil, fmt.Errorf("publickey: parse: %w", err)
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil, nil

	case protocol.AuthAgent:
		sock := os.Getenv("SSH_AUTH_SOCK")
		if sock == "" {
			return nil, nil, errors.New("agent: SSH_AUTH_SOCK is not set")
		}
		conn, err := net.Dial("unix", sock)
		if err != nil {
			return nil, nil, fmt.Errorf("agent: dial: %w", err)
		}
		ag := agent.NewClient(conn)
		return []ssh.AuthMethod{ssh.PublicKeysCallback(ag.Signers)}, conn, nil

	case protocol.AuthInteractive:
		password := secret.Password
		answer := func(user, instruction string, questions []string, echos []bool) ([]string, error) {
			out := make([]string, len(questions))
			for i := range questions {
				out[i] = password
			}
			return out, nil
		}
		return []ssh.AuthMethod{ssh.KeyboardInteractive(answer)}, nil, nil

	case "":
		return nil, nil, errors.New("ssh: auth method not specified")

	default:
		return nil, nil, fmt.Errorf("ssh: unsupported auth method %q", method)
	}
}

// sessionCloser is an optional resource attached to the session.
type sessionCloser interface {
	Close() error
}

// buildHostKeyCallback returns an ssh.HostKeyCallback honoring the supplied
// strict-checking policy. For "accept-new", unknown host keys are appended to
// the known_hosts file under an advisory file lock; subsequent connections
// use the strict path.
func buildHostKeyCallback(strict, knownHostsPath string) (ssh.HostKeyCallback, sessionCloser, error) {
	if strict == StrictOff {
		return ssh.InsecureIgnoreHostKey(), nil, nil //nolint:gosec // explicitly requested by user
	}

	path := knownHostsPath
	if path == "" {
		p, err := defaultKnownHostsPath()
		if err != nil {
			return nil, nil, err
		}
		path = p
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, nil, fmt.Errorf("known_hosts: mkdir: %w", err)
	}
	// Ensure the file exists so knownhosts.New doesn't fail on first-run.
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		f, cErr := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
		if cErr != nil {
			return nil, nil, fmt.Errorf("known_hosts: create: %w", cErr)
		}
		_ = f.Close()
	} else if err != nil {
		return nil, nil, fmt.Errorf("known_hosts: stat: %w", err)
	}

	base, err := knownhosts.New(path)
	if err != nil {
		return nil, nil, fmt.Errorf("known_hosts: load: %w", err)
	}

	if strict == StrictStrict {
		return base, nil, nil
	}

	// accept-new: on unknown-host KeyError (Want empty), append and accept.
	var mu sync.Mutex
	cb := func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := base(hostname, remote, key)
		if err == nil {
			return nil
		}
		var ke *knownhosts.KeyError
		if errors.As(err, &ke) && len(ke.Want) == 0 {
			mu.Lock()
			defer mu.Unlock()
			return appendKnownHost(path, hostname, remote, key)
		}
		return err
	}
	return cb, nil, nil
}

// appendKnownHost writes a line describing (hostname, key) to the known_hosts
// file, using an advisory lock to avoid interleaving writes across sessions.
func appendKnownHost(path, hostname string, remote net.Addr, key ssh.PublicKey) error {
	addrs := []string{knownhosts.Normalize(hostname)}
	if remote != nil {
		if n := knownhosts.Normalize(remote.String()); n != addrs[0] {
			addrs = append(addrs, n)
		}
	}
	line := knownhosts.Line(addrs, key)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("known_hosts: open: %w", err)
	}
	defer f.Close()
	if err := lockFile(f); err != nil {
		return fmt.Errorf("known_hosts: lock: %w", err)
	}
	defer unlockFile(f)
	if !strings.HasSuffix(line, "\n") {
		line += "\n"
	}
	if _, err := f.WriteString(line); err != nil {
		return fmt.Errorf("known_hosts: write: %w", err)
	}
	return nil
}

func defaultKnownHostsPath() (string, error) {
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return filepath.Join(h, ".ssh", "known_hosts"), nil
	}
	u, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("known_hosts: resolve home: %w", err)
	}
	return filepath.Join(u.HomeDir, ".ssh", "known_hosts"), nil
}

// dialContext performs a ctx-aware TCP dial followed by the SSH client
// handshake, enforcing the caller-supplied timeout for both phases.
func dialContext(ctx context.Context, addr string, timeout time.Duration, cfg *ssh.ClientConfig) (*ssh.Client, error) {
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	// Apply a handshake deadline so a half-open remote cannot hang forever.
	if timeout > 0 {
		_ = conn.SetDeadline(time.Now().Add(timeout))
	}
	// Wire ctx cancellation to the conn so mid-handshake cancels abort.
	handshakeDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-handshakeDone:
		}
	}()
	c, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	close(handshakeDone)
	if err != nil {
		_ = conn.Close()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, err
	}
	if timeout > 0 {
		_ = conn.SetDeadline(time.Time{})
	}
	return ssh.NewClient(c, chans, reqs), nil
}
