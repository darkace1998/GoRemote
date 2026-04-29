package credentialkeychain

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/darkace1998/GoRemote/sdk/credential"
)

// indexFileName is the filename used for the on-disk index inside the
// provider's DataDir.
const indexFileName = "keychain-index.json"

// indexEntry records a single credential's non-sensitive metadata so the
// provider can enumerate its entries without calling the OS keychain.
type indexEntry struct {
	// Reference is the canonical Reference for this entry; the EntryID
	// is used as the keychain "account".
	Reference credential.Reference `json:"reference"`
	// UpdatedAt is the wall-clock time the entry was last written.
	UpdatedAt time.Time `json:"updated_at"`
}

// indexFile is the on-disk envelope stored as keychain-index.json.
type indexFile struct {
	// Version identifies the on-disk schema; currently only 1 is defined.
	Version int                   `json:"version"`
	Entries map[string]indexEntry `json:"entries"`
}

// loadIndex reads and decodes the index file at path. A missing file is
// not an error; it returns an empty map.
func loadIndex(path string) (map[string]indexEntry, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]indexEntry{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read index: %w", err)
	}
	var f indexFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("decode index: %w", err)
	}
	if f.Entries == nil {
		f.Entries = map[string]indexEntry{}
	}
	return f.Entries, nil
}

// saveIndex writes the given map to path atomically (tmp + fsync + rename
// + directory fsync). The parent directory is created with 0o700 if
// missing; the file is written with mode 0o600.
func saveIndex(path string, entries map[string]indexEntry) error {
	payload, err := json.MarshalIndent(indexFile{Version: 1, Entries: entries}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode index: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir parent: %w", err)
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open tmp: %w", err)
	}
	if _, err := f.Write(payload); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("sync tmp: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}
