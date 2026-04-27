package extlaunch

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestDiscoverOverrideAbsolute(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only test")
	}
	got, err := Discover("/bin/sh", nil)
	if err != nil {
		t.Fatalf("Discover override: %v", err)
	}
	if got != "/bin/sh" {
		t.Fatalf("expected /bin/sh, got %q", got)
	}
}

func TestDiscoverCandidates(t *testing.T) {
	got, err := Discover("", []string{"definitely-not-installed-xyz", "sh"})
	if runtime.GOOS == "windows" {
		// On windows neither is likely to resolve; tolerate either outcome.
		_ = got
		_ = err
		return
	}
	if err != nil {
		t.Fatalf("Discover sh: %v", err)
	}
	if filepath.Base(got) != "sh" {
		t.Fatalf("unexpected binary %q", got)
	}
}

func TestDiscoverNotFound(t *testing.T) {
	_, err := Discover("", []string{"definitely-not-installed-xyz"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestDiscoverOverrideMissing(t *testing.T) {
	_, err := Discover("definitely-not-installed-xyz", nil)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestBuildSubstitution(t *testing.T) {
	got, err := Build([]string{"--host", "{host}", "--port", "{port}"}, Vars{"host": "example.com", "port": "22"})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	want := []string{"--host", "example.com", "--port", "22"}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: %v vs %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("at %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuildEscapedBraces(t *testing.T) {
	got, err := Build([]string{"hi{{there}}"}, Vars{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got[0] != "hi{there}" {
		t.Fatalf("got %q", got[0])
	}
}

func TestBuildUnknownPlaceholder(t *testing.T) {
	_, err := Build([]string{"{nope}"}, Vars{})
	if !errors.Is(err, ErrPlaceholder) {
		t.Fatalf("want ErrPlaceholder, got %v", err)
	}
}

func TestBuildUnterminated(t *testing.T) {
	_, err := Build([]string{"{nope"}, Vars{})
	if !errors.Is(err, ErrPlaceholder) {
		t.Fatalf("want ErrPlaceholder, got %v", err)
	}
}

func TestBuildOptionalDropped(t *testing.T) {
	got, err := Build([]string{"{empty}"}, Vars{"empty": ""})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty argv, got %v", got)
	}
}

func TestStartCapturesOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only test")
	}
	var stdout, stderr bytes.Buffer
	p, err := Start(context.Background(), Spec{
		Binary: "/bin/sh",
		Args:   []string{"-c", "printf hi; printf err 1>&2"},
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := p.Wait(context.Background()); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if stdout.String() != "hi" {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if stderr.String() != "err" {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestStartContextCancelTerminates(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	p, err := Start(ctx, Spec{
		Binary: "/bin/sh",
		Args:   []string{"-c", "sleep 60"},
		Grace:  100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	cancel()
	done := make(chan error, 1)
	go func() { done <- p.Wait(context.Background()) }()
	select {
	case <-done:
		// good
	case <-time.After(2 * time.Second):
		t.Fatalf("process did not exit after cancel + grace")
	}
}

func TestStartMissingBinary(t *testing.T) {
	_, err := Start(context.Background(), Spec{Binary: "/no/such/thing-xyz"})
	if err == nil {
		t.Fatalf("want error, got nil")
	}
}

func TestKillIdempotent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only test")
	}
	p, err := Start(context.Background(), Spec{Binary: "/bin/sh", Args: []string{"-c", "sleep 60"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	p.Kill()
	p.Kill()
	_ = p.Wait(context.Background())
	if p.Pid() == 0 {
		t.Fatalf("expected non-zero pid before kill")
	}
}

func TestEnvIsolated(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only test")
	}
	var out bytes.Buffer
	p, err := Start(context.Background(), Spec{
		Binary: "/bin/sh",
		Args:   []string{"-c", "echo $GOREMOTE_TEST_VAR"},
		Env:    []string{"GOREMOTE_TEST_VAR=hello", "PATH=" + os.Getenv("PATH")},
		Stdout: &out,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := p.Wait(context.Background()); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if got := bytes.TrimSpace(out.Bytes()); string(got) != "hello" {
		t.Fatalf("env not propagated, got %q", got)
	}
}

func TestStartStdinWired(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-only test")
	}
	var out bytes.Buffer
	p, err := Start(context.Background(), Spec{
		Binary: "/bin/sh",
		Args:   []string{"-c", "cat"},
		Stdin:  strings.NewReader("hello-stdin\n"),
		Stdout: &out,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := p.Wait(context.Background()); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "hello-stdin" {
		t.Fatalf("stdin not consumed, stdout = %q", out.String())
	}
}
