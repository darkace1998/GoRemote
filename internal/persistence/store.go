package persistence

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/darkace1998/GoRemote/internal/domain"
)

// BackupsDirName is the sub-directory (under Store.Dir()) where zip backups
// are written. It is excluded from backup archives and restore overwrite.
const BackupsDirName = "backups"

// Meta is the persisted schema / build metadata stored in meta.json.
type Meta struct {
	// Version is the on-disk schema version. See CurrentVersion.
	Version int `json:"version"`
	// CreatedAt is the first-write wall-clock time of this store.
	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt is the last-write wall-clock time of this store.
	UpdatedAt time.Time `json:"updated_at"`
	// AppVersion is the goremote build identifier that last wrote the store.
	AppVersion string `json:"app_version,omitempty"`
}

// Snapshot is the bundled, in-memory view of the persisted state: the tree
// of folders and connections, the templates list, the workspace layout, and
// the meta record.
//
// Snapshot is what callers pass to Save and what Load returns. It contains
// no raw secrets; credentials are referenced by credential.Reference only.
type Snapshot struct {
	Tree      *domain.Tree
	Templates []domain.ConnectionTemplate
	Workspace domain.WorkspaceLayout
	Meta      Meta
}

// Store is a filesystem-backed persistence layer. It is safe for concurrent
// use: Load and Save are serialized through an internal RWMutex so readers
// never observe partial writes.
type Store struct {
	dir string
	mu  sync.RWMutex
	// now is indirected for tests that need deterministic timestamps; tests
	// in this package don't override it, but it's available for callers.
	now func() time.Time
}

// New constructs a Store rooted at dir. The directory is created lazily on
// the first Save; Load on a fresh directory returns an empty Snapshot with
// Meta.Version == CurrentVersion.
func New(dir string) *Store {
	return &Store{dir: dir, now: time.Now}
}

// Dir returns the root directory this Store manages.
func (s *Store) Dir() string { return s.dir }

// Load reads every persisted file under Dir(), runs any required schema
// migrations, validates nothing on its own (use Validate for that), and
// returns the assembled Snapshot.
//
// On a fresh / empty directory Load returns a zero-valued Snapshot with a
// non-nil empty Tree and Meta.Version == CurrentVersion.
func (s *Store) Load(ctx context.Context) (*Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	files, err := s.readAll()
	if err != nil {
		return nil, err
	}

	// Parse meta first to know whether migrations are needed.
	var meta Meta
	if raw, ok := files[FileMeta]; ok {
		if err := decodeJSON(FileMeta, raw, &meta); err != nil {
			return nil, err
		}
	}
	if meta.Version == 0 && len(files) == 0 {
		// Fresh store; present a coherent current-version meta.
		meta.Version = CurrentVersion
	}

	if meta.Version != CurrentVersion {
		mig := DefaultMigrator()
		migrated, merr := mig.Run(&meta, files)
		if merr != nil {
			return nil, merr
		}
		files = migrated
	}

	var inv inventoryFile
	if err := decodeJSON(FileInventory, files[FileInventory], &inv); err != nil {
		return nil, err
	}
	tree, err := decodeInventory(inv)
	if err != nil {
		return nil, err
	}

	var tpl templatesFile
	if err := decodeJSON(FileTemplates, files[FileTemplates], &tpl); err != nil {
		return nil, err
	}

	var ws workspaceFile
	if err := decodeJSON(FileWorkspace, files[FileWorkspace], &ws); err != nil {
		return nil, err
	}

	return &Snapshot{
		Tree:      tree,
		Templates: tpl.Templates,
		Workspace: ws.Workspace,
		Meta:      meta,
	}, nil
}

// Save writes every persisted file under Dir() atomically. Meta.UpdatedAt is
// set to the current time and Meta.CreatedAt is populated on the first write.
// Meta.Version is forced to CurrentVersion.
func (s *Store) Save(ctx context.Context, snap *Snapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if snap == nil {
		return fmt.Errorf("persistence: nil snapshot")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("persistence: mkdir %s: %w", s.dir, err)
	}

	meta := snap.Meta
	meta.Version = CurrentVersion
	now := s.now()
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = now
	}
	meta.UpdatedAt = now

	inv := encodeInventory(snap.Tree)
	tpl := templatesFile{Templates: snap.Templates}
	ws := workspaceFile{Workspace: snap.Workspace}

	if err := WriteAtomicJSON(filepath.Join(s.dir, FileInventory), inv); err != nil {
		return err
	}
	if err := WriteAtomicJSON(filepath.Join(s.dir, FileTemplates), tpl); err != nil {
		return err
	}
	if err := WriteAtomicJSON(filepath.Join(s.dir, FileWorkspace), ws); err != nil {
		return err
	}
	if err := WriteAtomicJSON(filepath.Join(s.dir, FileMeta), meta); err != nil {
		return err
	}

	// Reflect the persisted meta into the caller's snapshot so round-trip
	// callers see the final values.
	snap.Meta = meta
	return nil
}

// readAll enumerates the known top-level persisted files and returns their
// contents keyed by file name. Missing files are simply absent from the
// returned map; any other IO error is propagated.
func (s *Store) readAll() (map[string][]byte, error) {
	out := make(map[string][]byte, 4)
	for _, name := range []string{FileMeta, FileInventory, FileTemplates, FileWorkspace} {
		data, ok, err := readFileIfExists(filepath.Join(s.dir, name))
		if err != nil {
			return nil, fmt.Errorf("persistence: read %s: %w", name, err)
		}
		if ok {
			out[name] = data
		}
	}
	return out, nil
}

// MarshalIndent is a re-export so callers can produce the same indented
// JSON shape the Store uses on disk without reaching for encoding/json
// directly. It is mainly useful for tests and tooling.
func MarshalIndent(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}
