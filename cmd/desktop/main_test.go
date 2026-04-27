package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	credhost "github.com/goremote/goremote/host/credential"
	pluginhost "github.com/goremote/goremote/host/plugin"
	protohost "github.com/goremote/goremote/host/protocol"
	iapp "github.com/goremote/goremote/internal/app"
	"github.com/goremote/goremote/internal/eventbus"
	"github.com/goremote/goremote/internal/logging"
	"github.com/goremote/goremote/internal/persistence"
)

func TestNewAppWithRecovery_RecoversFromCorruptSnapshot(t *testing.T) {
	dir := t.TempDir()
	// Force app.New to fail during snapshot load.
	if err := os.WriteFile(filepath.Join(dir, persistence.FileMeta), []byte("{"), 0o600); err != nil {
		t.Fatalf("WriteFile corrupt meta: %v", err)
	}

	logger := logging.New(logging.Options{Writer: io.Discard})
	a, err := newAppWithRecovery(iapp.Config{Dir: dir, Logger: logger}, logger)
	if err != nil {
		t.Fatalf("newAppWithRecovery: %v", err)
	}
	defer func() { _ = a.Shutdown(context.Background()) }()

	// Recovery should quarantine the old state directory.
	pattern := filepath.Join(filepath.Dir(dir), filepath.Base(dir)+".corrupt-*")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(matches) == 0 {
		t.Fatalf("expected quarantine directory matching %q", pattern)
	}
	if _, err := os.Stat(filepath.Join(matches[0], persistence.FileMeta)); err != nil {
		t.Fatalf("quarantined meta.json not found: %v", err)
	}
}

func TestNewAppWithRecovery_DoesNotMaskNonLoadErrors(t *testing.T) {
	logger := logging.New(logging.Options{Writer: io.Discard})
	_, err := newAppWithRecovery(iapp.Config{Dir: "", Logger: logger}, logger)
	if err == nil {
		t.Fatalf("expected error for empty dir")
	}
	if errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected unrelated filesystem error: %v", err)
	}
}

func TestRegisterBuiltins_SkipsUnsupportedPlatformModules(t *testing.T) {
	dir := t.TempDir()
	logger := logging.New(logging.Options{Writer: io.Discard})
	ph := pluginhost.New(eventbus.New[pluginhost.Event](), pluginhost.WithGOOS("windows"))

	a, err := iapp.New(iapp.Config{
		Dir:            dir,
		Logger:         logger,
		PluginHost:     ph,
		ProtocolHost:   protohost.New(ph),
		CredentialHost: credhost.New(ph),
	})
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	defer func() { _ = a.Shutdown(context.Background()) }()

	if err := registerBuiltins(context.Background(), a, dir); err != nil {
		t.Fatalf("registerBuiltins: %v", err)
	}
	if _, ok := a.ProtocolHost().Module("io.goremote.protocol.mosh"); ok {
		t.Fatalf("mosh should be skipped on windows host")
	}
	if _, ok := a.ProtocolHost().Module("io.goremote.protocol.ssh"); !ok {
		t.Fatalf("ssh should be registered")
	}
}

func TestChooseLogWriter_WindowsMirrorsToStdoutAndStderr(t *testing.T) {
	var stderr bytes.Buffer
	var stdout bytes.Buffer
	w := chooseLogWriter("windows", &stderr, &stdout)
	if _, err := w.Write([]byte("logline")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("stderr = %q, want empty", got)
	}
	if got := stdout.String(); got != "logline" {
		t.Fatalf("stdout = %q, want %q", got, "logline")
	}
}

func TestChooseLogWriter_NonWindowsUsesStderrOnly(t *testing.T) {
	var stderr bytes.Buffer
	var stdout bytes.Buffer
	w := chooseLogWriter("linux", &stderr, &stdout)
	if _, err := w.Write([]byte("logline")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := stderr.String(); got != "logline" {
		t.Fatalf("stderr = %q, want %q", got, "logline")
	}
	if got := stdout.String(); got != "" {
		t.Fatalf("stdout = %q, want empty", got)
	}
}

func TestResolveLogLevel(t *testing.T) {
	cases := []struct {
		in   string
		want slog.Level
	}{
		{"trace", slog.Level(-8)},
		{"debug", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
		{"", slog.LevelInfo},
		{"nonsense", slog.LevelInfo},
	}
	for _, tc := range cases {
		got := resolveLogLevel(tc.in)
		if got.Level() != tc.want {
			t.Fatalf("resolveLogLevel(%q) = %v, want %v", tc.in, got.Level(), tc.want)
		}
	}
}
