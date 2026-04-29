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

	credhost "github.com/darkace1998/GoRemote/host/credential"
	pluginhost "github.com/darkace1998/GoRemote/host/plugin"
	protohost "github.com/darkace1998/GoRemote/host/protocol"
	iapp "github.com/darkace1998/GoRemote/internal/app"
	"github.com/darkace1998/GoRemote/internal/eventbus"
	"github.com/darkace1998/GoRemote/internal/logging"
	"github.com/darkace1998/GoRemote/internal/persistence"
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

func TestChooseLogWriter_WindowsUsesStderrOnly(t *testing.T) {
	var stderr bytes.Buffer
	var stdout bytes.Buffer
	w := chooseLogWriter("windows", &stderr, &stdout)
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

func TestSelectLogLevel(t *testing.T) {
	if got := selectLogLevel("debug", "error"); got.Level() != slog.LevelDebug {
		t.Fatalf("env override failed: got %v want %v", got.Level(), slog.LevelDebug)
	}
	if got := selectLogLevel("", "trace"); got.Level() != slog.Level(-8) {
		t.Fatalf("settings level failed: got %v want %v", got.Level(), slog.Level(-8))
	}
	if got := selectLogLevel("", ""); got.Level() != slog.LevelInfo {
		t.Fatalf("default level failed: got %v want %v", got.Level(), slog.LevelInfo)
	}
}

func TestBindingsSetLogLevel(t *testing.T) {
	v := new(slog.LevelVar)
	v.Set(slog.LevelInfo)
	b := &Bindings{logLevel: v}

	cases := map[string]slog.Level{
		"trace": slog.Level(-8),
		"debug": slog.LevelDebug,
		"info":  slog.LevelInfo,
		"warn":  slog.LevelWarn,
		"error": slog.LevelError,
		"":      slog.LevelInfo,
		"junk":  slog.LevelInfo,
	}
	for in, want := range cases {
		b.SetLogLevel(in)
		if got := v.Level(); got != want {
			t.Errorf("SetLogLevel(%q) -> %v, want %v", in, got, want)
		}
	}

	// Nil LevelVar must not panic.
	(&Bindings{}).SetLogLevel("debug")
}
