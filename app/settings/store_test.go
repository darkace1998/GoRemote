package settings

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type capturingLogger struct {
	mu      sync.Mutex
	entries []string
}

func (l *capturingLogger) Error(msg string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	parts := []string{msg}
	for _, a := range args {
		parts = append(parts, formatArg(a))
	}
	l.entries = append(l.entries, strings.Join(parts, " "))
}

func (l *capturingLogger) joined() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.entries, "\n")
}

func formatArg(a any) string {
	switch v := a.(type) {
	case string:
		return v
	case error:
		return v.Error()
	default:
		return ""
	}
}

func TestFileStore_RoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	store := NewFileStore(path)

	got, err := store.Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != Default() {
		t.Errorf("missing-file Get = %+v, want Default", got)
	}

	in := Default()
	in.Theme = ThemeDark
	in.FontFamily = "Cascadia"
	in.FontSizePx = 16
	in.AutoReconnect = true
	in.ReconnectMaxN = 5
	in.ReconnectDelayMs = 1000
	in.LogLevel = LogLevelDebug

	saved, err := store.Update(context.Background(), in)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if saved.UpdatedAt.IsZero() {
		t.Errorf("UpdatedAt not set")
	}
	in.UpdatedAt = saved.UpdatedAt
	if saved != in {
		t.Errorf("Update mismatch: got %+v, want %+v", saved, in)
	}

	// File should exist with mode 0600.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("file mode = %v, want 0600", mode)
	}

	got2, err := store.Get(context.Background())
	if err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	if got2 != saved {
		t.Errorf("re-read mismatch: got %+v, want %+v", got2, saved)
	}
}

func TestFileStore_MissingFileReturnsDefaults(t *testing.T) {
	t.Parallel()
	store := NewFileStore(filepath.Join(t.TempDir(), "nope", "settings.json"))
	got, err := store.Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != Default() {
		t.Errorf("Get = %+v, want Default()", got)
	}
}

func TestFileStore_CorruptFileReturnsDefaultsAndLogs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("seed corrupt: %v", err)
	}
	logger := &capturingLogger{}
	store := NewFileStoreWithLogger(path, logger)

	got, err := store.Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != Default() {
		t.Errorf("corrupt Get = %+v, want Default()", got)
	}
	if !strings.Contains(logger.joined(), "corrupt") {
		t.Errorf("expected corruption log, got: %q", logger.joined())
	}
}

func TestFileStore_UpdateValidates(t *testing.T) {
	t.Parallel()
	store := NewFileStore(filepath.Join(t.TempDir(), "settings.json"))
	bad := Default()
	bad.Theme = "neon"
	if _, err := store.Update(context.Background(), bad); err == nil {
		t.Fatal("Update with invalid theme = nil, want error")
	}
	bad2 := Default()
	bad2.FontSizePx = 999
	if _, err := store.Update(context.Background(), bad2); err == nil {
		t.Fatal("Update with invalid fontSizePx = nil, want error")
	}
}

func TestFileStore_AtomicNoPartialFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	store := NewFileStore(path)

	// Write a few times.
	for i := 0; i < 3; i++ {
		s := Default()
		s.FontSizePx = 10 + i
		if _, err := store.Update(context.Background(), s); err != nil {
			t.Fatalf("Update %d: %v", i, err)
		}
	}

	// The directory should contain only the final settings file (no
	// dangling .tmp-* siblings).
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() == "settings.json" {
			continue
		}
		t.Errorf("unexpected leftover entry: %s", e.Name())
	}
}

func TestFileStore_ContextCancelled(t *testing.T) {
	t.Parallel()
	store := NewFileStore(filepath.Join(t.TempDir(), "settings.json"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Get(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Get cancelled = %v, want context.Canceled", err)
	}
	if _, err := store.Update(ctx, Default()); !errors.Is(err, context.Canceled) {
		t.Errorf("Update cancelled = %v, want context.Canceled", err)
	}
}

func TestFileStore_UpdatedAtMonotonic(t *testing.T) {
	t.Parallel()
	store := NewFileStore(filepath.Join(t.TempDir(), "settings.json"))
	s, err := store.Update(context.Background(), Default())
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if time.Since(s.UpdatedAt) > time.Minute || time.Since(s.UpdatedAt) < -time.Minute {
		t.Errorf("UpdatedAt %v not near now", s.UpdatedAt)
	}
}

func TestFileStore_PartialFileGetsDefaultsMerged(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	// Persist a file containing only theme + telemetry; everything else
	// should fall back to defaults on read.
	partial := map[string]any{
		"theme":            ThemeDark,
		"telemetryEnabled": true,
	}
	data, _ := json.Marshal(partial)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got, err := NewFileStore(path).Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Theme != ThemeDark {
		t.Errorf("Theme = %q, want %q", got.Theme, ThemeDark)
	}
	if !got.TelemetryEnabled {
		t.Errorf("TelemetryEnabled = false, want true")
	}
	if got.FontSizePx != Default().FontSizePx {
		t.Errorf("FontSizePx = %d, want default %d", got.FontSizePx, Default().FontSizePx)
	}
	if got.ReconnectMaxN != Default().ReconnectMaxN {
		t.Errorf("ReconnectMaxN = %d, want default %d", got.ReconnectMaxN, Default().ReconnectMaxN)
	}
	if got.LogLevel != Default().LogLevel {
		t.Errorf("LogLevel = %q, want default %q", got.LogLevel, Default().LogLevel)
	}
}
