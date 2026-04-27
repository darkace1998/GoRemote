// Package workspace defines the persisted UI workspace state (open tabs,
// active tab, layout grouping) and a small file-backed Store.
//
// The workspace document is intentionally a thin, stable surface. The UI
// hydrates it on launch and saves it (debounced) whenever the user opens,
// closes, reorders, pins, or otherwise rearranges tabs. Persistence is
// best-effort: a missing or corrupt file falls back to Default() so the
// UI can always boot.
package workspace

import (
	"errors"
	"fmt"
	"time"
)

// CurrentVersion is the schema version written by this build. Older
// documents are still loadable so long as their version is >= 1; newer
// versions are accepted at decode time but should be migrated by the
// caller.
const CurrentVersion = 1

// Workspace is the full persisted UI workspace document.
type Workspace struct {
	Version   int        `json:"version"`
	OpenTabs  []TabState `json:"openTabs"`
	ActiveTab string     `json:"activeTab,omitempty"`
	UpdatedAt time.Time  `json:"updatedAt"`
}

// TabState is a single open tab. ID is a UI-assigned identifier (uuid)
// that is stable across reload; ConnectionID references a saved
// connection so the UI can re-resolve title / protocol on hydrate.
type TabState struct {
	ID           string    `json:"id"`
	ConnectionID string    `json:"connectionId"`
	Title        string    `json:"title"`
	PaneGroup    string    `json:"paneGroup,omitempty"`
	Pinned       bool      `json:"pinned,omitempty"`
	LastUsedAt   time.Time `json:"lastUsedAt"`
}

// Default returns an empty workspace at the current schema version.
func Default() Workspace {
	return Workspace{
		Version:  CurrentVersion,
		OpenTabs: []TabState{},
	}
}

// Validate checks structural invariants:
//   - Version is >= 1.
//   - Every tab has a non-empty ID.
//   - Tab IDs are unique.
//   - ActiveTab, if set, refers to an existing tab.
//
// Multiple violations are joined into a single error so callers can show
// them all at once.
func (w *Workspace) Validate() error {
	var errs []error
	if w.Version < 1 {
		errs = append(errs, fmt.Errorf("version %d invalid: want >= 1", w.Version))
	}
	seen := make(map[string]struct{}, len(w.OpenTabs))
	for i, t := range w.OpenTabs {
		if t.ID == "" {
			errs = append(errs, fmt.Errorf("openTabs[%d]: id is empty", i))
			continue
		}
		if _, dup := seen[t.ID]; dup {
			errs = append(errs, fmt.Errorf("openTabs[%d]: duplicate id %q", i, t.ID))
			continue
		}
		seen[t.ID] = struct{}{}
	}
	if w.ActiveTab != "" {
		if _, ok := seen[w.ActiveTab]; !ok {
			errs = append(errs, fmt.Errorf("activeTab %q not present in openTabs", w.ActiveTab))
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}
