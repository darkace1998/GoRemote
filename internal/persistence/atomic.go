// Package persistence implements the goremote on-disk store: versioned
// native-format inventory, templates, workspace layout, and meta files;
// a schema migrator; an integrity validator; and a zip-based backup /
// restore facility.
//
// The package is UI- and plugin-agnostic: it depends only on the standard
// library, internal/domain, and sdk/credential. It does not persist raw
// secrets — connections carry credential.Reference values, and resolved
// Material is the responsibility of the credential host and its providers.
package persistence

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// WriteAtomic writes data to path atomically: it writes into a sibling
// temp file, fsyncs the file, renames it over the destination, and then
// fsyncs the containing directory so the rename is durable. Any temp
// file left behind by a failed write is cleaned up before returning.
func WriteAtomic(path string, data []byte) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("persistence: mkdir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".tmp-"+filepath.Base(path)+"-*")
	if err != nil {
		return fmt.Errorf("persistence: create temp: %w", err)
	}
	tmpPath := tmp.Name()
	// Best-effort cleanup of the temp file if anything below fails.
	defer func() {
		if err != nil {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, werr := tmp.Write(data); werr != nil {
		_ = tmp.Close()
		return fmt.Errorf("persistence: write temp: %w", werr)
	}
	if serr := tmp.Sync(); serr != nil {
		_ = tmp.Close()
		return fmt.Errorf("persistence: fsync temp: %w", serr)
	}
	if cerr := tmp.Close(); cerr != nil {
		return fmt.Errorf("persistence: close temp: %w", cerr)
	}
	if rerr := os.Rename(tmpPath, path); rerr != nil {
		return fmt.Errorf("persistence: rename: %w", rerr)
	}
	if derr := fsyncDir(dir); derr != nil {
		return fmt.Errorf("persistence: fsync dir: %w", derr)
	}
	return nil
}

// WriteAtomicJSON marshals v to indented JSON and writes it atomically
// to path via WriteAtomic.
func WriteAtomicJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("persistence: marshal %s: %w", filepath.Base(path), err)
	}
	return WriteAtomic(path, data)
}

// fsyncDir opens a directory and calls Sync on it, ensuring directory
// metadata is durable on disk. On platforms where directory fsync is not
// supported, the error is swallowed.
func fsyncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		// Some filesystems / platforms (e.g. certain Windows configurations)
		// do not support directory fsync. Treat not-supported and permission
		// errors on directory fsync as non-fatal.
		if errors.Is(err, os.ErrInvalid) || errors.Is(err, os.ErrPermission) {
			return nil
		}
	}
	return nil
}

// readFileIfExists reads path and returns its contents, or (nil, false, nil)
// if the file does not exist. Any other error is returned.
func readFileIfExists(path string) ([]byte, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}
