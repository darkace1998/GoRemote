package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Store is the persistence interface for settings. Implementations must be
// safe for concurrent use.
type Store interface {
	// Get returns the current settings. If the underlying file is missing
	// or corrupt, Get returns Default() with a nil error (corruption is
	// logged but never fatal so the UI can always boot).
	Get(ctx context.Context) (Settings, error)
	// Update validates the supplied settings, stamps UpdatedAt, persists
	// them, and returns the persisted value.
	Update(ctx context.Context, s Settings) (Settings, error)
}

// Logger is the minimal logging surface the file store needs. *slog.Logger
// satisfies it.
type Logger interface {
	Error(msg string, args ...any)
}

type discardLogger struct{}

func (discardLogger) Error(string, ...any) {}

// fileStore persists settings as a single JSON file using an atomic
// temp-file + rename + fsync sequence.
type fileStore struct {
	path   string
	logger Logger
	now    func() time.Time

	mu sync.Mutex
}

// NewFileStore returns a Store that persists to path. The parent directory
// is created lazily on first write.
func NewFileStore(path string) Store {
	return &fileStore{
		path:   path,
		logger: discardLogger{},
		now:    time.Now,
	}
}

// NewFileStoreWithLogger is like NewFileStore but routes corruption /
// unexpected I/O errors to the supplied logger.
func NewFileStoreWithLogger(path string, logger Logger) Store {
	if logger == nil {
		logger = discardLogger{}
	}
	return &fileStore{
		path:   path,
		logger: logger,
		now:    time.Now,
	}
}

// DefaultPath returns the on-disk location of the settings file:
// $UserConfigDir/goremote/settings.json.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("settings: user config dir: %w", err)
	}
	return filepath.Join(dir, "goremote", "settings.json"), nil
}

// Get returns the persisted settings, or Default() if the file is missing
// or unreadable. JSON decode errors are logged but not returned so callers
// can always render a UI.
func (s *fileStore) Get(ctx context.Context) (Settings, error) {
	if err := ctx.Err(); err != nil {
		return Default(), err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Default(), nil
		}
		s.logger.Error("settings: read", "path", s.path, "err", err.Error())
		return Default(), nil
	}
	var out Settings
	if err := json.Unmarshal(data, &out); err != nil {
		s.logger.Error("settings: corrupt file, returning defaults",
			"path", s.path, "err", err.Error())
		return Default(), nil
	}
	// Ensure unset/zero fields fall back to defaults so older files keep
	// working when new fields are added.
	merged := mergeWithDefaults(out)
	return merged, nil
}

// Update validates, stamps UpdatedAt, and atomically writes the file with
// mode 0600.
func (s *fileStore) Update(ctx context.Context, in Settings) (Settings, error) {
	if err := ctx.Err(); err != nil {
		return Settings{}, err
	}
	if err := in.Validate(); err != nil {
		return Settings{}, err
	}
	in.UpdatedAt = s.now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.MarshalIndent(in, "", "  ")
	if err != nil {
		return Settings{}, fmt.Errorf("settings: marshal: %w", err)
	}
	if err := writeAtomic(s.path, data, 0o600); err != nil {
		return Settings{}, err
	}
	return in, nil
}

// writeAtomic writes data to path atomically: write to a sibling temp file
// in the same directory, fsync, then rename. The directory is fsynced
// after the rename so the new entry is durable. On failure the temp file
// is cleaned up and no partial file is left at the destination path.
func writeAtomic(path string, data []byte, mode os.FileMode) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("settings: mkdir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".tmp-"+filepath.Base(path)+"-*")
	if err != nil {
		return fmt.Errorf("settings: create temp: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := os.Chmod(tmpPath, mode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("settings: chmod temp: %w", err)
	}
	if _, werr := tmp.Write(data); werr != nil {
		_ = tmp.Close()
		return fmt.Errorf("settings: write temp: %w", werr)
	}
	if serr := tmp.Sync(); serr != nil {
		_ = tmp.Close()
		return fmt.Errorf("settings: fsync temp: %w", serr)
	}
	if cerr := tmp.Close(); cerr != nil {
		return fmt.Errorf("settings: close temp: %w", cerr)
	}
	if rerr := os.Rename(tmpPath, path); rerr != nil {
		return fmt.Errorf("settings: rename: %w", rerr)
	}
	if d, derr := os.Open(dir); derr == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// mergeWithDefaults fills in any zero-valued required fields with their
// defaults so an old or partial file still produces a valid Settings.
func mergeWithDefaults(in Settings) Settings {
	d := Default()
	if in.Theme == "" {
		in.Theme = d.Theme
	}
	if in.FontSizePx == 0 {
		in.FontSizePx = d.FontSizePx
	}
	if in.ReconnectMaxN == 0 && in.ReconnectDelayMs == 0 && !in.AutoReconnect {
		// keep the defaults pair coherent on a brand-new file
		in.ReconnectMaxN = d.ReconnectMaxN
		in.ReconnectDelayMs = d.ReconnectDelayMs
	}
	if in.LogLevel == "" {
		in.LogLevel = d.LogLevel
	}
	return in
}

// slogAdapter adapts *slog.Logger to the local Logger interface so callers
// don't need to import slog just for this package.
type slogAdapter struct{ l *slog.Logger }

// NewSlogLogger wraps a *slog.Logger to satisfy Logger.
func NewSlogLogger(l *slog.Logger) Logger {
	if l == nil {
		return discardLogger{}
	}
	return slogAdapter{l: l}
}

func (a slogAdapter) Error(msg string, args ...any) { a.l.Error(msg, args...) }
