// Package ipc implements the out-of-process plugin transport for goremote:
// length-prefixed JSON over Unix domain sockets. On POSIX the socket is
// chmod 0600; on Windows, Go's net package uses the OS ACL model directly
// (Windows 10 RS1+ / build 17063 or later).
//
// The transport is intentionally narrow. Only two services are wired here —
// PluginHandshake and Echo — both defined in proto/plugin/v1. Higher-level
// protocol/credential RPCs will layer on top of the same connection in
// follow-up work.
package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	pluginv1 "github.com/goremote/goremote/proto/plugin/v1"
)

// ErrSocketInUse is returned by ListenUnix if another process appears to be
// listening on the requested socket path already.
var ErrSocketInUse = errors.New("ipc: socket path already in use")

// Listener wraps a net.Listener with the platform-specific cleanup needed
// for Unix domain sockets.
type Listener struct {
	net.Listener
	path    string
	cleanup func() error
}

// Path returns the socket path the listener is bound to.
func (l *Listener) Path() string { return l.path }

// Close stops accepting connections and removes the on-disk socket file
// (Unix) or releases the named-pipe handle (Windows).
func (l *Listener) Close() error {
	err := l.Listener.Close()
	if l.cleanup != nil {
		if cerr := l.cleanup(); cerr != nil && err == nil {
			err = cerr
		}
	}
	return err
}

// ListenUnix opens a host-side listener on the given socket path.
//
// The function refuses to clobber an existing socket that another process
// is actively listening on (returns ErrSocketInUse), but will reclaim a
// stale socket file left behind by a crashed process. The created socket
// is set to mode 0600 so only the owning user can connect.
func ListenUnix(ctx context.Context, socketPath string) (*Listener, error) {
	if socketPath == "" {
		return nil, errors.New("ipc: empty socket path")
	}
	ln, cleanup, err := socketListen(ctx, socketPath)
	if err != nil {
		return nil, err
	}
	return &Listener{Listener: ln, path: socketPath, cleanup: cleanup}, nil
}

// PluginHandshakeServer handles Hello RPCs.
type PluginHandshakeServer interface {
	Hello(ctx context.Context, req *pluginv1.HelloRequest) (*pluginv1.HelloResponse, error)
}

// EchoServer handles Ping RPCs.
type EchoServer interface {
	Ping(ctx context.Context, req *pluginv1.PingRequest) (*pluginv1.PingResponse, error)
}

// Server hosts the plugin IPC service over a JSON-framed net.Listener.
// Plugins (or tests) construct it with their PluginHandshakeServer and
// EchoServer implementations and call Serve.
type Server struct {
	listener  *Listener
	handshake PluginHandshakeServer
	echo      EchoServer
	stopOnce  sync.Once
	mu        sync.Mutex
	conns     map[net.Conn]struct{}
}

// NewServer builds a Server bound to ln and serving the supplied
// implementations. The Server owns the listener and will close it on Serve
// return.
func NewServer(ln *Listener, handshake PluginHandshakeServer, echo EchoServer) *Server {
	return &Server{
		listener:  ln,
		handshake: handshake,
		echo:      echo,
		conns:     make(map[net.Conn]struct{}),
	}
}

// Serve runs the server until ctx is cancelled. On cancel the listener is
// closed; active connections are given 3 seconds to drain before being
// force-closed.
func (s *Server) Serve(ctx context.Context) error {
	errc := make(chan error, 1)
	go func() {
		for {
			conn, err := s.listener.Accept()
			if err != nil {
				errc <- err
				return
			}
			s.mu.Lock()
			s.conns[conn] = struct{}{}
			s.mu.Unlock()
			go s.handleConn(conn)
		}
	}()

	select {
	case err := <-errc:
		_ = s.listener.Close()
		if errors.Is(err, net.ErrClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}

	_ = s.listener.Close()
	<-errc // wait for accept loop to exit

	// Give active connections up to 3s to drain.
	drainDeadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(drainDeadline) {
		s.mu.Lock()
		n := len(s.conns)
		s.mu.Unlock()
		if n == 0 {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Force-close remaining connections.
	s.mu.Lock()
	for c := range s.conns {
		_ = c.Close()
	}
	s.mu.Unlock()
	return nil
}

// Stop forcibly stops the server. Mainly useful in tests.
func (s *Server) Stop() {
	s.stopOnce.Do(func() {
		_ = s.listener.Close()
		s.mu.Lock()
		for c := range s.conns {
			_ = c.Close()
		}
		s.mu.Unlock()
	})
}

func (s *Server) handleConn(conn net.Conn) {
	defer func() {
		_ = conn.Close()
		s.mu.Lock()
		delete(s.conns, conn)
		s.mu.Unlock()
	}()
	for {
		frame, err := pluginv1.ReadFrame(conn)
		if err != nil {
			return
		}
		resp := s.dispatch(frame)
		if err := pluginv1.WriteFrame(conn, resp); err != nil {
			return
		}
	}
}

func (s *Server) dispatch(req pluginv1.Frame) pluginv1.Frame {
	ctx := context.Background()
	switch req.Method {
	case "Hello":
		if s.handshake == nil {
			return pluginv1.Frame{ID: req.ID, Error: "Hello: no handler registered"}
		}
		var r pluginv1.HelloRequest
		if err := json.Unmarshal(req.Payload, &r); err != nil {
			return pluginv1.Frame{ID: req.ID, Error: fmt.Sprintf("Hello: decode: %v", err)}
		}
		resp, err := s.handshake.Hello(ctx, &r)
		if err != nil {
			return pluginv1.Frame{ID: req.ID, Error: fmt.Sprintf("Hello: %v", err)}
		}
		data, err := json.Marshal(resp)
		if err != nil {
			return pluginv1.Frame{ID: req.ID, Error: fmt.Sprintf("Hello: encode: %v", err)}
		}
		return pluginv1.Frame{ID: req.ID, Payload: data}
	case "Ping":
		if s.echo == nil {
			return pluginv1.Frame{ID: req.ID, Error: "Ping: no handler registered"}
		}
		var r pluginv1.PingRequest
		if err := json.Unmarshal(req.Payload, &r); err != nil {
			return pluginv1.Frame{ID: req.ID, Error: fmt.Sprintf("Ping: decode: %v", err)}
		}
		resp, err := s.echo.Ping(ctx, &r)
		if err != nil {
			return pluginv1.Frame{ID: req.ID, Error: fmt.Sprintf("Ping: %v", err)}
		}
		data, err := json.Marshal(resp)
		if err != nil {
			return pluginv1.Frame{ID: req.ID, Error: fmt.Sprintf("Ping: encode: %v", err)}
		}
		return pluginv1.Frame{ID: req.ID, Payload: data}
	default:
		return pluginv1.Frame{ID: req.ID, Error: "unknown method: " + req.Method}
	}
}
