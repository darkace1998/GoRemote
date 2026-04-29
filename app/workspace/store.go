package workspace

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

// Store is the persistence interface for the workspace document.
// Implementations must be safe for concurrent use.
type Store interface {
	// Load returns the persisted workspace or Default() if the file is
	// missing or corrupt. Corruption is logged but not returned, so the
	// UI can always boot.
	Load(ctx context.Context) (Workspace, error)
	// Save validates, stamps UpdatedAt, and atomically writes the
	// document. Invalid input is rejected.
	Save(ctx context.Context, w Workspace) error
}

// Logger is the minimal logging surface NewFileStore needs.
// *slog.Logger satisfies it via NewSlogLogger.
type Logger interface {
	Error(msg string, args ...any)
}

type discardLogger struct{}

func (discardLogger) Error(string, ...any) {}

// fileStore persists the workspace as a single JSON file using the same
// atomic temp-file + fsync + rename sequence as app/settings.
type fileStore struct {
	path   string
	logger Logger
	now    func() time.Time

	mu sync.Mutex
}

// NewFileStore returns a Store that persists to path. The parent directory
// is created lazily on first Save. A nil logger is acceptable.
func NewFileStore(path string, log Logger) Store {
	if log == nil {
		log = discardLogger{}
	}
	return &fileStore{
		path:   path,
		logger: log,
		now:    time.Now,
	}
}

// DefaultPath returns the on-disk location of the workspace document:
// $UserConfigDir/goremote/workspace.json.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("workspace: user config dir: %w", err)
	}
	return filepath.Join(dir, "goremote", "workspace.json"), nil
}

// Load returns the persisted workspace, or Default() on missing/corrupt.
// JSON decode errors are logged but never returned.
func (s *fileStore) Load(ctx context.Context) (Workspace, error) {
	if err := ctx.Err(); err != nil {
		return Default(), err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	// #nosec G304 -- s.path is the configured workspace file path for this store.
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Default(), nil
		}
		s.logger.Error("workspace: read", "path", s.path, "err", err.Error())
		return Default(), nil
	}
	var out Workspace
	if err := json.Unmarshal(data, &out); err != nil {
		s.logger.Error("workspace: corrupt file, returning defaults",
			"path", s.path, "err", err.Error())
		return Default(), nil
	}
	if out.Version < 1 {
		out.Version = CurrentVersion
	}
	if out.OpenTabs == nil {
		out.OpenTabs = []TabState{}
	}
	// Defensive: if the on-disk file became invalid (e.g. dup ids from a
	// crash mid-write or hand-edit), drop to defaults rather than handing
	// the UI a broken document.
	if err := out.Validate(); err != nil {
		s.logger.Error("workspace: invalid file, returning defaults",
			"path", s.path, "err", err.Error())
		return Default(), nil
	}
	return out, nil
}

// Save validates, stamps UpdatedAt, and atomically writes the document
// with mode 0600.
func (s *fileStore) Save(ctx context.Context, in Workspace) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if in.Version == 0 {
		in.Version = CurrentVersion
	}
	if in.OpenTabs == nil {
		in.OpenTabs = []TabState{}
	}
	if err := in.Validate(); err != nil {
		return err
	}
	in.UpdatedAt = s.now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.MarshalIndent(in, "", "  ")
	if err != nil {
		return fmt.Errorf("workspace: marshal: %w", err)
	}
	return writeAtomic(s.path, data, 0o600)
}

// writeAtomic mirrors app/settings.writeAtomic: write a sibling temp file,
// fsync it, rename it over the destination, then fsync the directory. On
// failure the temp file is removed and no partial file is left at path.
func writeAtomic(path string, data []byte, mode os.FileMode) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("workspace: mkdir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".tmp-"+filepath.Base(path)+"-*")
	if err != nil {
		return fmt.Errorf("workspace: create temp: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := os.Chmod(tmpPath, mode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("workspace: chmod temp: %w", err)
	}
	if _, werr := tmp.Write(data); werr != nil {
		_ = tmp.Close()
		return fmt.Errorf("workspace: write temp: %w", werr)
	}
	if serr := tmp.Sync(); serr != nil {
		_ = tmp.Close()
		return fmt.Errorf("workspace: fsync temp: %w", serr)
	}
	if cerr := tmp.Close(); cerr != nil {
		return fmt.Errorf("workspace: close temp: %w", cerr)
	}
	if rerr := os.Rename(tmpPath, path); rerr != nil {
		return fmt.Errorf("workspace: rename: %w", rerr)
	}
	// #nosec G304 -- dir is derived from the configured workspace file path.
	if d, derr := os.Open(dir); derr == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// slogAdapter adapts *slog.Logger to the local Logger interface.
type slogAdapter struct{ l *slog.Logger }

// NewSlogLogger wraps a *slog.Logger to satisfy Logger.
func NewSlogLogger(l *slog.Logger) Logger {
	if l == nil {
		return discardLogger{}
	}
	return slogAdapter{l: l}
}

func (a slogAdapter) Error(msg string, args ...any) { a.l.Error(msg, args...) }
