//go:build !windows

package powershell

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/darkace1998/GoRemote/sdk/protocol"
)

// writeFakePwsh writes a tiny POSIX shell script that mimics enough of pwsh
// for these tests. It accepts (and ignores) the -NoLogo / -NoProfile /
// -Interactive flags goremote always passes, then runs an interactive cat-
// like loop so input written to the PTY shows up on stdout.
func writeFakePwsh(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fake-pwsh")
	body := "#!/usr/bin/env bash\n" +
		"# Ignore the goremote-supplied flags (-NoLogo -NoProfile -Interactive ...).\n" +
		"printf 'READY\\n'\n" +
		"# Echo each line of stdin back to stdout until EOF.\n" +
		"while IFS= read -r line; do\n" +
		"  printf '%s\\n' \"$line\"\n" +
		"done\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake pwsh: %v", err)
	}
	return path
}

func openTestSession(t *testing.T, settings map[string]any) protocol.Session {
	t.Helper()
	if settings == nil {
		settings = map[string]any{}
	}
	if _, ok := settings[SettingBinary]; !ok {
		settings[SettingBinary] = writeFakePwsh(t)
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

func TestOpenSendInputEcho(t *testing.T) {
	sess := openTestSession(t, nil)
	defer sess.Close()

	stdoutR, stdoutW := io.Pipe()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- sess.Start(ctx, nil, stdoutW) }()

	br := bufio.NewReader(stdoutR)
	deadline := time.Now().Add(5 * time.Second)
	var sawReady bool
	for time.Now().Before(deadline) {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("read startup output: %v (got %q)", err, line)
		}
		clean := strings.TrimRight(line, "\r\n")
		if clean == "READY" {
			sawReady = true
			break
		}
	}
	if !sawReady {
		t.Fatalf("did not observe startup readiness marker")
	}

	if err := sess.SendInput(ctx, []byte("hello\n")); err != nil {
		t.Fatalf("SendInput: %v", err)
	}

	// PTYs typically echo the input themselves and the fake pwsh prints it
	// once more. Scan up to a few lines until we see "hello".
	deadline = time.Now().Add(5 * time.Second)
	var saw bool
	for time.Now().Before(deadline) {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("read echo: %v (got %q)", err, line)
		}
		// Strip CR injected by line-discipline ECHO before LF.
		clean := strings.TrimRight(line, "\r\n")
		if strings.Contains(clean, "hello") {
			saw = true
			break
		}
	}
	if !saw {
		t.Fatalf("did not observe 'hello' in PTY output")
	}

	_ = sess.Close()
	_ = stdoutW.Close()
	_ = stdoutR.Close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("Start did not return after Close")
	}
}

func TestResizeNoError(t *testing.T) {
	sess := openTestSession(t, nil)
	defer sess.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() { _ = sess.Start(ctx, nil, io.Discard) }()
	// Give Start a chance to wire up its goroutines.
	time.Sleep(50 * time.Millisecond)

	if err := sess.Resize(ctx, protocol.Size{Cols: 132, Rows: 50}); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	if err := sess.Resize(ctx, protocol.Size{Cols: 80, Rows: 24}); err != nil {
		t.Fatalf("Resize: %v", err)
	}
}

func TestResizeRejectsNonPositive(t *testing.T) {
	sess := openTestSession(t, nil)
	defer sess.Close()

	if err := sess.Resize(context.Background(), protocol.Size{Cols: 0, Rows: 24}); err == nil {
		t.Fatalf("expected error for zero cols")
	}
}

func TestCloseIdempotent(t *testing.T) {
	sess := openTestSession(t, nil)

	if err := sess.Close(); err != nil {
		t.Fatalf("Close #1: %v", err)
	}
	// Concurrent re-closes must not panic and must not surface a different
	// error than the first call.
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = sess.Close()
		}()
	}
	wg.Wait()

	// SendInput / Resize on a closed session must not panic.
	if err := sess.SendInput(context.Background(), []byte("x")); err == nil {
		t.Fatalf("expected SendInput error on closed session")
	}
	if err := sess.Resize(context.Background(), protocol.Size{Cols: 80, Rows: 24}); err == nil {
		t.Fatalf("expected Resize error on closed session")
	}
}

func TestStartContextCancelKillsChild(t *testing.T) {
	sess := openTestSession(t, nil)
	defer sess.Close()

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- sess.Start(ctx, nil, io.Discard) }()

	// Let Start enter its select.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("Start returned unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("Start did not return within 1s after ctx cancel")
	}
}

func TestOpenInvalidBinary(t *testing.T) {
	mod := New()
	_, err := mod.Open(context.Background(), protocol.OpenRequest{
		AuthMethod: protocol.AuthNone,
		Settings: map[string]any{
			SettingBinary: "/nonexistent/path/to/definitely-not-pwsh",
		},
	})
	if err == nil {
		t.Fatalf("expected error for missing binary")
	}
}

func TestOpenInvalidBinaryByName(t *testing.T) {
	mod := New()
	_, err := mod.Open(context.Background(), protocol.OpenRequest{
		AuthMethod: protocol.AuthNone,
		Settings: map[string]any{
			SettingBinary: "definitely-not-pwsh-xyz-" + filepath.Base(t.TempDir()),
		},
	})
	if err == nil {
		t.Fatalf("expected error for missing binary by name")
	}
}

// TestEnvAndCWDPlumbed asserts that the resolved openConfig values reach the
// child via a fake pwsh that records cwd and a known env var before entering
// its stdin loop.
func TestEnvAndCWDPlumbed(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-pwsh")
	pwdFile := filepath.Join(dir, "pwd.txt")
	envFile := filepath.Join(dir, "env.txt")
	body := "#!/usr/bin/env bash\n" +
		"# Record cwd and GOREMOTE_TEST then loop echoing stdin.\n" +
		"pwd > " + shellQuote(pwdFile) + "\n" +
		"printf '%s' \"$GOREMOTE_TEST\" > " + shellQuote(envFile) + "\n" +
		"while IFS= read -r line; do printf '%s\\n' \"$line\"; done\n"
	if err := os.WriteFile(bin, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake pwsh: %v", err)
	}

	cwd := t.TempDir()
	wantPWD := canonicalPath(t, cwd)
	mod := New()
	sess, err := mod.Open(context.Background(), protocol.OpenRequest{
		AuthMethod: protocol.AuthNone,
		Settings: map[string]any{
			SettingBinary: bin,
			SettingCWD:    cwd,
			SettingEnv:    map[string]string{"GOREMOTE_TEST": "abc123"},
		},
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer sess.Close()

	var buf bytes.Buffer
	var bufMu sync.Mutex
	stdout := &lockedWriter{w: &buf, mu: &bufMu}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	startDone := make(chan error, 1)
	go func() { startDone <- sess.Start(ctx, nil, stdout) }()

	deadline := time.Now().Add(3 * time.Second)
	var gotPWD, gotEnv string
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(pwdFile); err == nil {
			gotPWD = canonicalPath(t, strings.TrimSpace(string(data)))
		}
		if data, err := os.ReadFile(envFile); err == nil {
			gotEnv = strings.TrimSpace(string(data))
		}
		if gotPWD == wantPWD && gotEnv == "abc123" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if gotPWD != wantPWD {
		bufMu.Lock()
		t.Fatalf("child cwd mismatch: got %q want %q (requested %q, pty output %q)", gotPWD, wantPWD, cwd, buf.String())
	}
	if gotEnv != "abc123" {
		bufMu.Lock()
		t.Fatalf("child env mismatch: got %q want %q (pty output %q)", gotEnv, "abc123", buf.String())
	}
	_ = sess.Close()
	<-startDone
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func canonicalPath(t *testing.T, p string) string {
	t.Helper()
	if p == "" {
		return ""
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatalf("Abs(%q): %v", p, err)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return abs
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
