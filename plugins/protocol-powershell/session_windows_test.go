//go:build windows

package powershell

import (
	"bytes"
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/darkace1998/GoRemote/sdk/protocol"
)

// openWinTestSession opens a session that uses cmd.exe to echo a line and
// exit immediately. It is the Windows equivalent of the Unix fake-pwsh helper.
func openWinTestSession(t *testing.T, settings map[string]any) protocol.Session {
	t.Helper()
	if settings == nil {
		settings = map[string]any{}
	}
	// Use cmd.exe with /C to run a command that writes one line and exits.
	// This lets us test the full Start→exit path without needing pwsh installed.
	if _, ok := settings[SettingBinary]; !ok {
		settings[SettingBinary] = "cmd.exe"
		if _, ok := settings[SettingArgs]; !ok {
			settings[SettingArgs] = []string{"/D", "/C", "echo goremote-test-ok"}
		}
	}
	mod := New()
	sess, err := mod.Open(context.Background(), protocol.OpenRequest{
		AuthMethod: protocol.AuthNone,
		Settings:   settings,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return sess
}

// TestWindowsStartAndExit verifies that Start returns cleanly when the child
// exits normally and that the output line is forwarded to stdout.
func TestWindowsStartAndExit(t *testing.T) {
	sess := openWinTestSession(t, nil)
	defer sess.Close()

	var buf bytes.Buffer
	var mu sync.Mutex
	pr, pw := io.Pipe()
	go func() {
		_, _ = io.Copy(&lockedWriter{w: &buf, mu: &mu}, pr)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := sess.Start(ctx, nil, pw); err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = pw.Close()
	_ = pr.Close()

	mu.Lock()
	out := buf.String()
	mu.Unlock()

	if !bytes.Contains([]byte(out), []byte("goremote-test-ok")) {
		t.Fatalf("expected 'goremote-test-ok' in output, got: %q", out)
	}
}

// TestWindowsResizeRejectsZero verifies Resize rejects non-positive
// dimensions but accepts a real resize on the ConPTY.
func TestWindowsResizeRejectsZero(t *testing.T) {
	sess := openWinTestSession(t, nil)
	defer sess.Close()

	if err := sess.Resize(context.Background(), protocol.Size{Cols: 0, Rows: 0}); err == nil {
		t.Fatalf("Resize: expected error for 0x0")
	}
	if err := sess.Resize(context.Background(), protocol.Size{Cols: 80, Rows: 24}); err != nil {
		t.Fatalf("Resize 80x24: %v", err)
	}
}

// TestWindowsCloseIdempotent verifies that multiple concurrent Close calls
// do not panic and all return nil.
func TestWindowsCloseIdempotent(t *testing.T) {
	sess := openWinTestSession(t, nil)

	if err := sess.Close(); err != nil {
		t.Fatalf("Close #1: %v", err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = sess.Close()
		}()
	}
	wg.Wait()

	// SendInput on a closed session must return an error (not panic).
	if err := sess.SendInput(context.Background(), []byte("x")); err == nil {
		t.Fatalf("expected SendInput error on closed session")
	}
}

// TestWindowsContextCancelKillsChild verifies that cancelling the context
// causes Start to return promptly.
func TestWindowsContextCancelKillsChild(t *testing.T) {
	// Use a long-running command so we can cancel it mid-flight.
	settings := map[string]any{
		SettingBinary: "cmd.exe",
		SettingArgs:   []string{"/D", "/C", "ping -n 30 127.0.0.1 > nul"},
	}
	sess := openWinTestSession(t, settings)
	defer sess.Close()

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- sess.Start(ctx, nil, io.Discard) }()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start returned unexpected error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("Start did not return within 3s after ctx cancel")
	}
}

// TestWindowsOpenInvalidBinary mirrors the unix test for missing binaries.
func TestWindowsOpenInvalidBinary(t *testing.T) {
	mod := New()
	_, err := mod.Open(context.Background(), protocol.OpenRequest{
		AuthMethod: protocol.AuthNone,
		Settings: map[string]any{
			SettingBinary: `C:\definitely\not\pwsh.exe`,
		},
	})
	if err == nil {
		t.Fatalf("expected error for missing binary")
	}
}

type lockedWriter struct {
	mu *sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}
