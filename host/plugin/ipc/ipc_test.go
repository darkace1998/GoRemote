//go:build unix

package ipc_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/darkace1998/GoRemote/host/plugin/ipc"
	pluginv1 "github.com/darkace1998/GoRemote/proto/plugin/v1"
)

// helloImpl is a minimal PluginHandshakeServer used by tests.
type helloImpl struct{}

func (helloImpl) Hello(_ context.Context, req *pluginv1.HelloRequest) (*pluginv1.HelloResponse, error) {
	return &pluginv1.HelloResponse{
		PluginVersion:  "test-1.2.3",
		Capabilities:   []string{"echo", "test"},
		Status:         "ready",
		ServerTimeUnix: time.Now().Unix(),
	}, nil
}

// echoImpl is a minimal EchoServer used by tests.
type echoImpl struct{}

func (echoImpl) Ping(_ context.Context, req *pluginv1.PingRequest) (*pluginv1.PingResponse, error) {
	cp := append([]byte(nil), req.Payload...)
	return &pluginv1.PingResponse{Payload: cp, ReceivedAtUnix: time.Now().Unix()}, nil
}

// startTestServer brings up an ipc.Server on a temp socket and returns the
// socket path along with a stop function that cancels the context, waits
// for Serve to return, and asserts no error.
func startTestServer(t *testing.T) (string, func()) {
	t.Helper()
	sock := testSocketPath(t)

	ctx, cancel := context.WithCancel(context.Background())
	ln, err := ipc.ListenUnix(ctx, sock)
	if err != nil {
		cancel()
		t.Fatalf("ListenUnix: %v", err)
	}
	srv := ipc.NewServer(ln, helloImpl{}, echoImpl{})
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx) }()

	stop := func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Serve returned error: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Errorf("Serve did not return after cancel")
		}
	}
	return sock, stop
}

func TestHelloAndPingRoundTrip(t *testing.T) {
	sock, stop := startTestServer(t)
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := ipc.DialUnix(ctx, sock)
	if err != nil {
		t.Fatalf("DialUnix: %v", err)
	}
	defer c.Close()

	hello, err := c.Hello(ctx, &pluginv1.HelloRequest{HostVersion: "0.0.1", PluginID: "test"})
	if err != nil {
		t.Fatalf("Hello: %v", err)
	}
	if hello.PluginVersion != "test-1.2.3" {
		t.Errorf("PluginVersion = %q, want test-1.2.3", hello.PluginVersion)
	}
	if hello.Status != "ready" {
		t.Errorf("Status = %q", hello.Status)
	}
	if len(hello.Capabilities) != 2 || hello.Capabilities[0] != "echo" {
		t.Errorf("Capabilities = %v", hello.Capabilities)
	}
	if hello.ServerTimeUnix == 0 {
		t.Errorf("ServerTimeUnix not populated")
	}

	payload := []byte("hello, plugin")
	pong, err := c.Ping(ctx, &pluginv1.PingRequest{Payload: payload})
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if string(pong.Payload) != string(payload) {
		t.Errorf("Ping payload mismatch: got %q want %q", pong.Payload, payload)
	}
}

func TestServerCleansUpSocketOnStop(t *testing.T) {
	sock, stop := startTestServer(t)
	if _, err := os.Stat(sock); err != nil {
		t.Fatalf("socket should exist while server running: %v", err)
	}
	stop()
	if _, err := os.Stat(sock); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("socket file still present after stop: %v", err)
	}
}

func TestDialNonexistentSocketFailsFast(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "nope.sock")

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := ipc.DialUnix(ctx, sock, ipc.WithDialTimeout(500*time.Millisecond))
	if err == nil {
		t.Fatalf("expected error dialing nonexistent socket")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("dial took too long to fail: %v", elapsed)
	}
}

func TestConcurrentPings(t *testing.T) {
	sock, stop := startTestServer(t)
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, err := ipc.DialUnix(ctx, sock)
	if err != nil {
		t.Fatalf("DialUnix: %v", err)
	}
	defer c.Close()

	const goroutines = 10
	const perG = 100

	var wg sync.WaitGroup
	errs := make(chan error, goroutines)
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perG; i++ {
				want := []byte(fmt.Sprintf("g%d-i%d", g, i))
				resp, err := c.Ping(ctx, &pluginv1.PingRequest{Payload: want})
				if err != nil {
					errs <- err
					return
				}
				if string(resp.Payload) != string(want) {
					errs <- fmt.Errorf("payload mismatch: got %q want %q", resp.Payload, want)
					return
				}
			}
		}(g)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent ping: %v", err)
	}
}

func TestListenUnixRefusesActiveSocket(t *testing.T) {
	sock, stop := startTestServer(t)
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := ipc.ListenUnix(ctx, sock); !errors.Is(err, ipc.ErrSocketInUse) {
		t.Fatalf("ListenUnix on active socket: got err=%v, want ErrSocketInUse", err)
	}
}

func TestListenUnixRefusesRegularFilePath(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "ipc.sock")
	if err := os.WriteFile(sock, []byte("not-a-socket"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := ipc.ListenUnix(ctx, sock); err == nil {
		t.Fatalf("ListenUnix should fail when path is a regular file")
	}

	data, err := os.ReadFile(sock)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "not-a-socket" {
		t.Fatalf("regular file at socket path was modified")
	}
}

func TestListenUnixRefusesSymlinkPath(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("target"), 0o600); err != nil {
		t.Fatalf("WriteFile target: %v", err)
	}
	sock := filepath.Join(dir, "ipc.sock")
	if err := os.Symlink(target, sock); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := ipc.ListenUnix(ctx, sock); err == nil {
		t.Fatalf("ListenUnix should fail when path is a symlink")
	}

	if _, err := os.Lstat(sock); err != nil {
		t.Fatalf("symlink path should remain untouched: %v", err)
	}
}
