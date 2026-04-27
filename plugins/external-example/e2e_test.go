//go:build unix

package externalexample_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/goremote/goremote/host/plugin/ipc"
	pluginv1 "github.com/goremote/goremote/proto/plugin/v1"
)

func buildPluginBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "external-example")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/external-example")
	cmd.Dir = "."
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("go build: %v", err)
	}
	return bin
}

// waitForSocket polls until the socket file exists or the deadline elapses.
func waitForSocket(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("socket %s did not appear within %v", path, timeout)
}

func TestExternalExampleEndToEnd(t *testing.T) {
	bin := buildPluginBinary(t)

	dir := t.TempDir()
	sock := filepath.Join(dir, "sock")
	cmd := exec.Command(bin, "--socket", sock)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start plugin: %v", err)
	}

	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Signal(syscall.SIGKILL)
		}
		select {
		case <-exited:
		case <-time.After(3 * time.Second):
		}
	}()

	waitForSocket(t, sock, 5*time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := ipc.DialUnix(ctx, sock)
	if err != nil {
		t.Fatalf("DialUnix: %v", err)
	}
	defer c.Close()

	hello, err := c.Hello(ctx, &pluginv1.HelloRequest{HostVersion: "test", PluginID: "host"})
	if err != nil {
		t.Fatalf("Hello: %v", err)
	}
	if hello.PluginVersion != "0.1.0" {
		t.Errorf("PluginVersion = %q, want 0.1.0", hello.PluginVersion)
	}
	if hello.Status != "ready" {
		t.Errorf("Status = %q, want ready", hello.Status)
	}
	if len(hello.Capabilities) != 1 || hello.Capabilities[0] != "echo" {
		t.Errorf("Capabilities = %v, want [echo]", hello.Capabilities)
	}

	for i := 0; i < 5; i++ {
		payload := []byte(fmt.Sprintf("payload-%d-%s", i, string(make([]byte, i*7))))
		resp, err := c.Ping(ctx, &pluginv1.PingRequest{Payload: payload})
		if err != nil {
			t.Fatalf("Ping[%d]: %v", i, err)
		}
		if string(resp.Payload) != string(payload) {
			t.Errorf("Ping[%d] payload mismatch: got %q want %q", i, resp.Payload, payload)
		}
	}

	_ = c.Close()

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}
	select {
	case err := <-exited:
		if err != nil {
			// Some signals leave a non-nil exit error; treat clean
			// exit and signal-driven exit as success.
			var ee *exec.ExitError
			if !errors.As(err, &ee) {
				t.Fatalf("plugin exited with unexpected error: %v", err)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("plugin did not exit within 5s of SIGTERM")
	}
}
