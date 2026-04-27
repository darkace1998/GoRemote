package rdp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/goremote/goremote/sdk/protocol"
)

// fakeShDiscover returns the absolute path /bin/sh, regardless of inputs.
// Tests use it together with a session whose argv is set to run /bin/sh
// against a known one-liner so we can exercise Start without needing an
// actual RDP client installed.
func TestStart_LaunchesAndForwardsLine(t *testing.T) {
	// Spawn /bin/sh -c "echo launched" via a Session built directly from
	// newSession, mirroring what Module.Open would produce.
	sess := newSession("/bin/sh", []string{"-c", "echo launched"})

	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- sess.Start(ctx, nil, &out) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("Start did not return")
	}

	got := out.String()
	if !strings.Contains(got, "goremote: launched /bin/sh pid=") {
		t.Fatalf("status line missing in output: %q", got)
	}
	if !strings.Contains(got, "launched\n") {
		t.Fatalf("child stdout 'launched' line missing in output: %q", got)
	}
}

func TestStart_ForwardsStderrLinePrefixed(t *testing.T) {
	sess := newSession("/bin/sh", []string{"-c", "echo oops 1>&2"})
	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sess.Start(ctx, nil, &out); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !strings.Contains(out.String(), "stderr: oops") {
		t.Fatalf("expected line-prefixed stderr forwarding, got %q", out.String())
	}
}

func TestStart_NonZeroExitTreatedAsClean(t *testing.T) {
	sess := newSession("/bin/sh", []string{"-c", "exit 7"})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sess.Start(ctx, nil, io.Discard); err != nil {
		t.Fatalf("non-zero exit must surface as nil; got %v", err)
	}
}

func TestStart_ContextCancellationStops(t *testing.T) {
	// Use `exec` so /bin/sh is replaced by sleep; otherwise SIGTERM kills
	// sh while leaving sleep as an orphan holding the stdout pipe open,
	// which makes exec.Cmd.Wait block on output draining.
	sess := newSession("/bin/sh", []string{"-c", "exec sleep 30"})
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- sess.Start(ctx, nil, io.Discard) }()

	time.Sleep(80 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("Start returned unexpected error after cancel: %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatalf("Start did not return after ctx cancel")
	}
}

func TestCloseIdempotent(t *testing.T) {
	sess := newSession("/bin/sh", []string{"-c", "exec sleep 30"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- sess.Start(ctx, nil, io.Discard) }()

	time.Sleep(50 * time.Millisecond)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := sess.Close(); err != nil {
				t.Errorf("Close: %v", err)
			}
		}()
	}
	wg.Wait()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("Start did not return after Close")
	}

	// And one more Close just to confirm.
	if err := sess.Close(); err != nil {
		t.Fatalf("Close after stop: %v", err)
	}
}

func TestSendInputAndResizeUnsupported(t *testing.T) {
	sess := newSession("/bin/sh", []string{"-c", "true"})
	if err := sess.SendInput(context.Background(), []byte("x")); !errors.Is(err, protocol.ErrUnsupported) {
		t.Fatalf("SendInput err = %v, want ErrUnsupported", err)
	}
	if err := sess.Resize(context.Background(), protocol.Size{Cols: 80, Rows: 24}); !errors.Is(err, protocol.ErrUnsupported) {
		t.Fatalf("Resize err = %v, want ErrUnsupported", err)
	}
}

// TestOpen_WithInjectedDiscover verifies that the public Open path,
// configured with a test-only discover hook, produces a Session whose Start
// can run a known no-op binary and emit the launched marker line.
func TestOpen_WithInjectedDiscover(t *testing.T) {
	mod := &Module{
		discover: func(override string, candidates []string) (string, error) {
			return "/bin/sh", nil
		},
		argvFor: func(goos string, cfg *config) ([]string, error) {
			return []string{"-c", "echo launched"}, nil
		},
	}
	sess, err := mod.Open(context.Background(), protocol.OpenRequest{
		Host: "h",
		Port: 3389,
		Settings: map[string]any{
			SettingHost: "h",
		},
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if sess.RenderMode() != protocol.RenderExternal {
		t.Fatalf("RenderMode = %v want external", sess.RenderMode())
	}
	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sess.Start(ctx, nil, &out); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !strings.Contains(out.String(), "goremote: launched /bin/sh pid=") {
		t.Fatalf("missing status line in %q", out.String())
	}
	if !strings.Contains(out.String(), "launched\n") {
		t.Fatalf("missing child output in %q", out.String())
	}
}
