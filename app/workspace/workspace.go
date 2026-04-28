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
	Version     int           `json:"version"`
	OpenTabs    []TabState    `json:"openTabs"`
	ActiveTab   string        `json:"activeTab,omitempty"`
	PaneLayouts []PaneLayout  `json:"paneLayouts,omitempty"`
	Recents     []RecentEntry `json:"recents,omitempty"`
	UpdatedAt   time.Time     `json:"updatedAt"`
}

// RecentEntry records a single connection's most recent open. The list
// is bounded (see TouchRecent) and ordered most-recent-first so the UI
// can render it directly without a sort. OpenCount is informational —
// we do not currently use it for ranking but it's cheap to track.
type RecentEntry struct {
	ConnectionID string    `json:"connectionId"`
	OpenedAt     time.Time `json:"openedAt"`
	OpenCount    int       `json:"openCount,omitempty"`
}

// MaxRecents bounds the size of the Recents list. Twenty is the same
// cap most editors use for "recent files" and easily fits in a pop-up
// menu without scrolling.
const MaxRecents = 20

// TouchRecent records a fresh open for the given connection ID. The
// matching entry (if any) is moved to the front and its counter
// incremented; otherwise a new entry is prepended. The list is
// truncated to MaxRecents.
func (w *Workspace) TouchRecent(connID string, at time.Time) {
	if connID == "" {
		return
	}
	if at.IsZero() {
		at = time.Now()
	}
	out := make([]RecentEntry, 0, len(w.Recents)+1)
	count := 1
	for _, e := range w.Recents {
		if e.ConnectionID == connID {
			count = e.OpenCount + 1
			continue
		}
		out = append(out, e)
	}
	out = append([]RecentEntry{{ConnectionID: connID, OpenedAt: at, OpenCount: count}}, out...)
	if len(out) > MaxRecents {
		out = out[:MaxRecents]
	}
	w.Recents = out
}

// PaneLayout describes the split structure of a single tab that hosts
// more than one session pane. GroupID is the stable identifier shared
// by all TabStates that belong to this group (TabState.PaneGroup).
// Root is the pane tree: leaves carry a ConnectionID that the UI uses
// to match the tab's restored sessions back into the correct slot.
type PaneLayout struct {
	GroupID string    `json:"groupId"`
	Root    *PaneNode `json:"root"`
}

// PaneNode is one node in a PaneLayout's tree. A node is either a leaf
// (ConnectionID set, A/B nil) or a branch (Orientation set to "h" or
// "v" and both A/B non-nil).
type PaneNode struct {
	ConnectionID string    `json:"connectionId,omitempty"`
	Orientation  string    `json:"orientation,omitempty"`
	A            *PaneNode `json:"a,omitempty"`
	B            *PaneNode `json:"b,omitempty"`
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
