package persistence

import (
	"encoding/json"
	"fmt"
)

// CurrentVersion is the on-disk schema version this build writes.
//
// The initial release has no legacy formats, so the only registered
// migration is a no-op 0→1 identity transform that exercises the
// framework. New migrations must be appended to DefaultMigrator's
// Migrations slice in ascending (From, To) order.
const CurrentVersion = 1

// Migration is a single step in the schema history. From is the on-disk
// version the step consumes; To is the version it produces. Migrate
// receives a parsed view of every persisted file (keyed by file name) and
// must return the same keyspace (missing entries are treated as absent).
type Migration struct {
	From    int
	To      int
	Migrate func(raw map[string]any) (map[string]any, error)
}

// Migrator applies a chain of Migration steps to persisted bytes.
type Migrator struct {
	// Migrations is the ordered chain of steps. Each step's From must equal
	// the previous step's To.
	Migrations []Migration
}

// DefaultMigrator returns the Migrator the Store uses at Load time. It
// currently contains a single identity 0→1 step so the framework has a
// deterministic target for the initial release.
func DefaultMigrator() *Migrator {
	return &Migrator{
		Migrations: []Migration{
			{From: 0, To: 1, Migrate: identityMigration},
		},
	}
}

// identityMigration is a no-op that leaves raw unchanged. It exists so the
// migrator always has a 0→1 target even before real format changes.
func identityMigration(raw map[string]any) (map[string]any, error) {
	return raw, nil
}

// Run applies migrations needed to bring meta.Version up to CurrentVersion.
// It marshals and unmarshals through a combined raw map keyed by file name
// (with the ".json" suffix stripped), invokes each applicable migration in
// order, and returns a new files map on success. On any error the original
// files map is returned unchanged (rollback semantics) together with the
// error. meta.Version is updated in-place only on success.
func (m *Migrator) Run(meta *Meta, files map[string][]byte) (map[string][]byte, error) {
	if meta == nil {
		return files, fmt.Errorf("persistence: nil meta")
	}
	if meta.Version == CurrentVersion {
		return files, nil
	}
	if meta.Version > CurrentVersion {
		return files, fmt.Errorf("persistence: on-disk version %d is newer than supported %d", meta.Version, CurrentVersion)
	}

	// Parse every file into a combined raw map keyed by basename-without-json.
	raw, err := toRaw(files)
	if err != nil {
		return files, err
	}

	// Snapshot of original files/meta for rollback on any failure.
	originalFiles := cloneFiles(files)
	originalVersion := meta.Version

	current := meta.Version
	for _, step := range m.Migrations {
		if step.From < current {
			continue
		}
		if step.From != current {
			return originalFiles, fmt.Errorf("persistence: migration chain gap: have v%d, next step is v%d→v%d", current, step.From, step.To)
		}
		if step.Migrate == nil {
			return originalFiles, fmt.Errorf("persistence: migration v%d→v%d has no Migrate func", step.From, step.To)
		}
		out, merr := step.Migrate(raw)
		if merr != nil {
			meta.Version = originalVersion
			return originalFiles, fmt.Errorf("persistence: migration v%d→v%d: %w", step.From, step.To, merr)
		}
		raw = out
		current = step.To
		if current == CurrentVersion {
			break
		}
	}

	if current != CurrentVersion {
		return originalFiles, fmt.Errorf("persistence: no migration path from v%d to v%d", meta.Version, CurrentVersion)
	}

	newFiles, err := fromRaw(raw, files)
	if err != nil {
		return originalFiles, err
	}
	meta.Version = CurrentVersion
	return newFiles, nil
}

// toRaw parses every file in files as JSON into a single raw map keyed by
// the file name without its ".json" suffix. Files whose bodies are absent
// or not valid JSON objects/arrays are included as raw bytes under a
// "_raw" suffix so migrations can still rewrite them if needed.
func toRaw(files map[string][]byte) (map[string]any, error) {
	out := make(map[string]any, len(files))
	for name, data := range files {
		if len(data) == 0 {
			continue
		}
		var v any
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, fmt.Errorf("persistence: parse %s for migration: %w", name, err)
		}
		out[rawKey(name)] = v
	}
	return out, nil
}

// fromRaw re-serializes raw back into the file-bytes shape. Keys in raw that
// are not present in the template are emitted under "<key>.json". Keys that
// were present in the original template but absent in raw are dropped.
func fromRaw(raw map[string]any, template map[string][]byte) (map[string][]byte, error) {
	out := make(map[string][]byte, len(raw))
	// Preserve deterministic file names for known entries.
	for _, name := range []string{FileMeta, FileInventory, FileTemplates, FileWorkspace} {
		key := rawKey(name)
		if v, ok := raw[key]; ok {
			data, err := json.MarshalIndent(v, "", "  ")
			if err != nil {
				return nil, fmt.Errorf("persistence: re-encode %s: %w", name, err)
			}
			out[name] = data
			delete(raw, key)
		} else if _, had := template[name]; had {
			// File existed but the migration removed it → skip.
		}
	}
	// Any residual keys become <key>.json files.
	for key, v := range raw {
		data, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("persistence: re-encode %s: %w", key, err)
		}
		out[key+".json"] = data
	}
	return out, nil
}

// rawKey converts a file name like "inventory.json" to its raw-map key
// "inventory". Names without the suffix are returned unchanged.
func rawKey(name string) string {
	if len(name) > 5 && name[len(name)-5:] == ".json" {
		return name[:len(name)-5]
	}
	return name
}

func cloneFiles(in map[string][]byte) map[string][]byte {
	out := make(map[string][]byte, len(in))
	for k, v := range in {
		b := make([]byte, len(v))
		copy(b, v)
		out[k] = b
	}
	return out
}
