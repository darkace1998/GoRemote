package ssh

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/goremote/goremote/sdk/protocol"

	"golang.org/x/crypto/ssh"
)

// reservedClosedPort returns a TCP address whose port was just released, so
// subsequent dials will get "connection refused". The returned cleanup frees
// any residual socket (it's a no-op today but kept for forward-compat).
func reservedClosedPort(t *testing.T) (*net.TCPAddr, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().(*net.TCPAddr)
	if err := ln.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return addr, func() {}
}

// testServer is an in-process SSH server fixture used by the session tests.
// It accepts password auth (password = "pw"), records the request types it
// observes, and invokes a user-supplied channel handler for each session
// channel.
type testServer struct {
	Addr          string
	close         func()
	signer        ssh.Signer
	mu            sync.Mutex
	seenReq       []string
	windowChanges int32
}

func (s *testServer) recordRequest(t string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seenReq = append(s.seenReq, t)
}

func (s *testServer) RequestTypes() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.seenReq))
	copy(out, s.seenReq)
	return out
}

// channelHandler runs for each "session" channel. It consumes the request
// channel until it closes; implementations should Reply to requests and may
// write to / close the channel.
type channelHandler func(t *testing.T, srv *testServer, ch ssh.Channel, reqs <-chan *ssh.Request)

func startTestServer(t *testing.T, h channelHandler) *testServer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pwd []byte) (*ssh.Permissions, error) {
			if string(pwd) == "pw" {
				return &ssh.Permissions{}, nil
			}
			return nil, fmt.Errorf("auth denied")
		},
	}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	srv := &testServer{
		Addr:   ln.Addr().String(),
		signer: signer,
	}
	closed := make(chan struct{})
	srv.close = func() {
		select {
		case <-closed:
		default:
			close(closed)
			_ = ln.Close()
		}
	}
	t.Cleanup(srv.close)

	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go func(nc net.Conn) {
				sconn, chans, reqs, err := ssh.NewServerConn(nc, cfg)
				if err != nil {
					return
				}
				defer sconn.Close()
				go ssh.DiscardRequests(reqs)
				for nch := range chans {
					if nch.ChannelType() != "session" {
						_ = nch.Reject(ssh.UnknownChannelType, "no")
						continue
					}
					ch, reqs, err := nch.Accept()
					if err != nil {
						return
					}
					go h(t, srv, ch, reqs)
				}
			}(nc)
		}
	}()
	return srv
}

// helloHandler replies to pty-req/shell and writes "hello\n" then closes.
func helloHandler(t *testing.T, srv *testServer, ch ssh.Channel, reqs <-chan *ssh.Request) {
	for req := range reqs {
		srv.recordRequest(req.Type)
		switch req.Type {
		case "pty-req":
			_ = req.Reply(true, nil)
		case "shell":
			_ = req.Reply(true, nil)
			go func() {
				_, _ = ch.Write([]byte("hello\n"))
				time.Sleep(20 * time.Millisecond)
				_ = ch.Close()
			}()
		case "window-change":
			atomic.AddInt32(&srv.windowChanges, 1)
			// window-change has no reply
		default:
			if req.WantReply {
				_ = req.Reply(false, nil)
			}
		}
	}
}

// keepOpenHandler replies to pty-req and shell but never closes the channel.
// It tracks window-change requests for Resize tests.
func keepOpenHandler(ready chan<- struct{}) channelHandler {
	var once sync.Once
	return func(t *testing.T, srv *testServer, ch ssh.Channel, reqs <-chan *ssh.Request) {
		for req := range reqs {
			srv.recordRequest(req.Type)
			switch req.Type {
			case "pty-req", "shell":
				_ = req.Reply(true, nil)
				if req.Type == "shell" {
					once.Do(func() { close(ready) })
				}
			case "window-change":
				atomic.AddInt32(&srv.windowChanges, 1)
			default:
				if req.WantReply {
					_ = req.Reply(false, nil)
				}
			}
		}
		_ = ch.Close()
	}
}

func openAgainst(t *testing.T, srv *testServer, extra map[string]any) *Session {
	t.Helper()
	host, portStr, err := net.SplitHostPort(srv.Addr)
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	var port int
	fmt.Sscanf(portStr, "%d", &port)

	settings := map[string]any{
		SettingStrictHostKeyChecking: StrictOff,
		SettingConnectTimeoutSeconds: 5,
		SettingKeepaliveSeconds:      0,
		SettingPTYTerm:               "xterm-256color",
	}
	for k, v := range extra {
		settings[k] = v
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sess, err := New().Open(ctx, protocol.OpenRequest{
		Host:        host,
		Port:        port,
		Username:    "tester",
		AuthMethod:  protocol.AuthPassword,
		Secret:      protocol.CredentialMaterial{Password: "pw"},
		Settings:    settings,
		InitialSize: protocol.Size{Cols: 80, Rows: 24},
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return sess.(*Session)
}

func TestSessionStartReceivesRemoteOutput(t *testing.T) {
	srv := startTestServer(t, helloHandler)
	sess := openAgainst(t, srv, nil)
	defer sess.Close()

	if sess.RenderMode() != protocol.RenderTerminal {
		t.Fatalf("render mode = %q, want terminal", sess.RenderMode())
	}

	var buf bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sess.Start(ctx, nil, &buf); err != nil {
		t.Fatalf("start: %v", err)
	}
	if !strings.Contains(buf.String(), "hello") {
		t.Fatalf("stdout = %q, want to contain 'hello'", buf.String())
	}
	// Server should have seen at least pty-req and shell.
	got := srv.RequestTypes()
	seen := map[string]bool{}
	for _, r := range got {
		seen[r] = true
	}
	for _, want := range []string{"pty-req", "shell"} {
		if !seen[want] {
			t.Errorf("request type %q not seen, got %v", want, got)
		}
	}
}

func TestSessionResizeSendsWindowChange(t *testing.T) {
	ready := make(chan struct{})
	srv := startTestServer(t, keepOpenHandler(ready))
	sess := openAgainst(t, srv, nil)
	defer sess.Close()

	// Run Start in a goroutine; it blocks until Close.
	done := make(chan error, 1)
	go func() {
		done <- sess.Start(context.Background(), nil, &bytes.Buffer{})
	}()

	select {
	case <-ready:
	case <-time.After(3 * time.Second):
		t.Fatal("server never saw shell request")
	}

	if err := sess.Resize(context.Background(), protocol.Size{Cols: 120, Rows: 40}); err != nil {
		t.Fatalf("resize: %v", err)
	}
	// Give the server a moment to observe the request.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&srv.windowChanges) >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := atomic.LoadInt32(&srv.windowChanges); got < 1 {
		t.Fatalf("window-change count = %d, want >= 1", got)
	}

	_ = sess.Close()
	<-done
}

func TestSessionResizeRejectsBadSize(t *testing.T) {
	ready := make(chan struct{})
	srv := startTestServer(t, keepOpenHandler(ready))
	sess := openAgainst(t, srv, nil)
	defer sess.Close()
	if err := sess.Resize(context.Background(), protocol.Size{Cols: 0, Rows: 0}); err == nil {
		t.Fatal("expected error for zero-sized resize")
	}
}

func TestSessionCloseIdempotent(t *testing.T) {
	ready := make(chan struct{})
	srv := startTestServer(t, keepOpenHandler(ready))
	sess := openAgainst(t, srv, nil)
	// Multiple Close calls must not panic or return unexpected errors.
	_ = sess.Close()
	_ = sess.Close()
	_ = sess.Close()
}

func TestSessionSendInputWritesToStdin(t *testing.T) {
	// Handler that echoes stdin to stdout line-by-line, then closes on "bye".
	echo := func(t *testing.T, srv *testServer, ch ssh.Channel, reqs <-chan *ssh.Request) {
		shellStarted := make(chan struct{})
		go func() {
			for req := range reqs {
				srv.recordRequest(req.Type)
				switch req.Type {
				case "pty-req":
					_ = req.Reply(true, nil)
				case "shell":
					_ = req.Reply(true, nil)
					close(shellStarted)
				default:
					if req.WantReply {
						_ = req.Reply(false, nil)
					}
				}
			}
		}()
		<-shellStarted
		buf := make([]byte, 256)
		for {
			n, err := ch.Read(buf)
			if n > 0 {
				_, _ = ch.Write(buf[:n])
				if bytes.Contains(buf[:n], []byte("bye")) {
					_ = ch.Close()
					return
				}
			}
			if err != nil {
				return
			}
		}
	}
	srv := startTestServer(t, echo)
	sess := openAgainst(t, srv, nil)

	var out bytes.Buffer
	var mu sync.Mutex
	safeWriter := writerFunc(func(p []byte) (int, error) {
		mu.Lock()
		defer mu.Unlock()
		return out.Write(p)
	})

	done := make(chan error, 1)
	go func() { done <- sess.Start(context.Background(), nil, safeWriter) }()

	if err := sess.SendInput(context.Background(), []byte("hi\n")); err != nil {
		t.Fatalf("send: %v", err)
	}
	// Wait briefly for echo.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		has := bytes.Contains(out.Bytes(), []byte("hi"))
		mu.Unlock()
		if has {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := sess.SendInput(context.Background(), []byte("bye\n")); err != nil {
		t.Fatalf("send bye: %v", err)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		_ = sess.Close()
		<-done
	}
	mu.Lock()
	defer mu.Unlock()
	if !bytes.Contains(out.Bytes(), []byte("hi")) {
		t.Fatalf("output = %q, want to contain 'hi'", out.String())
	}
}

type writerFunc func(p []byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

func TestSessionStartCtxCancel(t *testing.T) {
	ready := make(chan struct{})
	srv := startTestServer(t, keepOpenHandler(ready))
	sess := openAgainst(t, srv, nil)
	defer sess.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- sess.Start(ctx, nil, &bytes.Buffer{}) }()
	<-ready
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected ctx error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Start did not return after ctx cancel")
	}
}
