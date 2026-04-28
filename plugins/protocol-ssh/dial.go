package ssh

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/goremote/goremote/sdk/protocol"
)

// Dial performs the full SSH connection setup — auth method construction,
// host-key callback, ctx-aware dial, and handshake — and returns a
// connected [*ssh.Client] plus a cleanup func that the caller MUST invoke
// to release auth-related resources (agent socket, known-hosts file
// handles) once the client is closed.
//
// This is exported so peer protocol plugins (e.g. SFTP) can stand on the
// same battle-tested SSH connection code without duplicating it.
//
// The req fields used are: Host, Port, Username, AuthMethod, Secret. The
// settings map is consulted for the SSH-specific keys (KnownHostsPath,
// StrictHostKeyChecking, ConnectTimeoutSeconds). All have sensible
// defaults that match an interactive SSH terminal session.
func Dial(ctx context.Context, req protocol.OpenRequest) (*ssh.Client, func(), error) {
	view := settingsView{m: req.Settings}

	host := req.Host
	if host == "" {
		host = view.stringOr(SettingHost, "")
	}
	if host == "" {
		return nil, nil, errors.New("ssh: host is required")
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
		return nil, nil, errors.New("ssh: username is required")
	}

	connectTimeout := time.Duration(view.intOr(SettingConnectTimeoutSeconds, 15)) * time.Second
	strict := view.stringOr(SettingStrictHostKeyChecking, StrictAcceptNew)
	knownHostsPath := view.stringOr(SettingKnownHostsPath, "")

	authMethods, agentCloser, err := buildAuthMethods(req.AuthMethod, req.Secret)
	if err != nil {
		return nil, nil, fmt.Errorf("ssh: build auth: %w", err)
	}

	hostKeyCB, hkCloser, err := buildHostKeyCallback(strict, knownHostsPath)
	if err != nil {
		if agentCloser != nil {
			_ = agentCloser.Close()
		}
		return nil, nil, fmt.Errorf("ssh: host-key callback: %w", err)
	}

	cfg := &ssh.ClientConfig{
		User:            username,
		Auth:            authMethods,
		HostKeyCallback: hostKeyCB,
		Timeout:         connectTimeout,
	}

	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	client, err := dialContext(ctx, addr, connectTimeout, cfg)
	if err != nil {
		if agentCloser != nil {
			_ = agentCloser.Close()
		}
		if hkCloser != nil {
			_ = hkCloser.Close()
		}
		return nil, nil, fmt.Errorf("ssh: dial %s: %w", addr, err)
	}

	cleanup := func() {
		if agentCloser != nil {
			_ = agentCloser.Close()
		}
		if hkCloser != nil {
			_ = hkCloser.Close()
		}
	}
	return client, cleanup, nil
}
