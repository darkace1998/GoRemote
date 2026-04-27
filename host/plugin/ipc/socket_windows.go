//go:build windows

package ipc

import (
	"context"
	"fmt"
	"net"
	"os"
	"time"
)

// socketListen creates a Unix-domain socket listener at socketPath on Windows.
//
// Go's net package supports Unix domain sockets on Windows 10 build 17063
// (RS1) and later, which covers every supported Windows release as of Go 1.21.
// Windows ACLs govern socket access; the Unix chmod(0o600) is not applicable.
//
// The cleanup callback removes the on-disk file on Close so a subsequent
// run does not see the stale socket.
func socketListen(_ context.Context, socketPath string) (net.Listener, func() error, error) {
	if err := removeIfStale(socketPath); err != nil {
		return nil, nil, err
	}
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, nil, fmt.Errorf("ipc: listen %q: %w", socketPath, err)
	}
	cleanup := func() error {
		err := os.Remove(socketPath)
		if err != nil && os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return ln, cleanup, nil
}

func socketDial(ctx context.Context, socketPath string) (net.Conn, error) {
	d := net.Dialer{Timeout: 5 * time.Second}
	return d.DialContext(ctx, "unix", socketPath)
}

// removeIfStale tries to remove socketPath if it exists but no listener is
// bound to it. If something is actively accepting on it, ErrSocketInUse is
// returned.
func removeIfStale(socketPath string) error {
	if _, err := os.Stat(socketPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	c, err := net.DialTimeout("unix", socketPath, 100*time.Millisecond)
	if err == nil {
		_ = c.Close()
		return ErrSocketInUse
	}
	return os.Remove(socketPath)
}
