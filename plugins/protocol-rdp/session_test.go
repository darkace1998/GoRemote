package rdp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/goremote/goremote/sdk/protocol"
)

func helperSession(args ...string) *Session {
	argv := append([]string{"-test.run=TestHelperProcess", "--"}, args...)
	return newSession(os.Args[0], argv)
}

func TestHelperProcess(t *testing.T) {
	sep := -1
	for i, arg := range os.Args {
		if arg == "--" {
			sep = i
			break
		}
	}
	if sep < 0 || sep+1 >= len(os.Args) {
		return
	}

	switch os.Args[sep+1] {
	case "stdout":
		if sep+2 < len(os.Args) {
			fmt.Fprintln(os.Stdout, os.Args[sep+2])
		}
	case "stderr":
		if sep+2 < len(os.Args) {
			fmt.Fprintln(os.Stderr, os.Args[sep+2])
		}
	case "exit":
		code := 0
		if sep+2 < len(os.Args) {
			code, _ = strconv.Atoi(os.Args[sep+2])
		}
		os.Exit(code)
	case "sleep":
		time.Sleep(30 * time.Second)
	}
	os.Exit(0)
}

func TestStart_LaunchesAndForwardsLine(t *testing.T) {
	sess := helperSession("stdout", "launched")

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
	if !strings.Contains(got, "goremote: launched "+os.Args[0]+" pid=") {
		t.Fatalf("status line missing in output: %q", got)
	}
	if !strings.Contains(got, "launched\n") {
		t.Fatalf("child stdout 'launched' line missing in output: %q", got)
	}
}

func TestStart_ForwardsStderrLinePrefixed(t *testing.T) {
	sess := helperSession("stderr", "oops")
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
	sess := helperSession("exit", "7")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sess.Start(ctx, nil, io.Discard); err != nil {
		t.Fatalf("non-zero exit must surface as nil; got %v", err)
	}
}

func TestStart_ContextCancellationStops(t *testing.T) {
	sess := helperSession("sleep")
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
	sess := helperSession("sleep")
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
	sess := helperSession("stdout", "unused")
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
			return os.Args[0], nil
		},
		argvFor: func(goos string, cfg *config) ([]string, error) {
			return []string{"-test.run=TestHelperProcess", "--", "stdout", "launched"}, nil
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
	if !strings.Contains(out.String(), "goremote: launched "+os.Args[0]+" pid=") {
		t.Fatalf("missing status line in %q", out.String())
	}
	if !strings.Contains(out.String(), "launched\n") {
		t.Fatalf("missing child output in %q", out.String())
	}
}
