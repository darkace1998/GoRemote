package persistence

import (
	"fmt"

	"github.com/goremote/goremote/internal/domain"
)

// Severity classifies the impact of an integrity issue.
type Severity string

// Issue severities.
const (
	// SeverityWarn marks a non-fatal inconsistency. Callers may proceed.
	SeverityWarn Severity = "warn"
	// SeverityError marks a data-integrity violation. Callers should refuse
	// to proceed with the snapshot as-is.
	SeverityError Severity = "error"
)

// Issue codes returned by Validate.
const (
	CodeMissingFolderParent     = "missing_folder_parent"
	CodeMissingConnectionParent = "missing_connection_parent"
	CodeDuplicateID             = "duplicate_id"
	CodeFolderCycle             = "folder_cycle"
	CodeOrphanWorkspaceTab      = "orphan_workspace_tab"
	CodeOrphanFocusedTab        = "orphan_focused_tab"
)

// ValidationIssue describes a single finding produced by Validate. The
// RelatedIDs slice lists the IDs the issue refers to so UI layers can
// highlight them.
type ValidationIssue struct {
	// Severity is Warn or Error.
	Severity Severity
	// Code is a short stable identifier (see Code* constants).
	Code string
	// Message is a human-readable description of the issue.
	Message string
	// RelatedIDs lists nodes the issue refers to, if any.
	RelatedIDs []domain.ID
}

// Validate inspects snap and returns every integrity issue it finds. An
// empty return slice means the snapshot is fully consistent. Callers should
// treat any SeverityError entry as a blocker.
func Validate(snap *Snapshot) []ValidationIssue {
	if snap == nil {
		return nil
	}
	folders, connections := flattenSnapshotTree(snap.Tree)
	return validateFlat(folders, connections, snap)
}

// folderCycle walks the ancestor chain of f and returns the list of folder
// IDs participating in the cycle, or nil if none. A cycle is detected when
// the walker revisits an ID already seen in the current walk.
func folderCycle(f *domain.FolderNode, index map[domain.ID]*domain.FolderNode) []domain.ID {
	seen := map[domain.ID]int{f.ID: 0}
	order := []domain.ID{f.ID}
	cur := f.ParentID
	for cur != domain.NilID {
		if idx, ok := seen[cur]; ok {
			return append([]domain.ID(nil), order[idx:]...)
		}
		parent, ok := index[cur]
		if !ok {
			return nil
		}
		seen[cur] = len(order)
		order = append(order, cur)
		cur = parent.ParentID
	}
	return nil
}

// cycleKey returns a stable canonical key for a cycle's ID list so repeated
// reports of the same cycle can be deduplicated.
func cycleKey(ids []domain.ID) string {
	sorted := append([]domain.ID(nil), ids...)
	sortIDs(sorted)
	var b []byte
	for _, id := range sorted {
		b = append(b, id[:]...)
	}
	return string(b)
}

func sortIDs(ids []domain.ID) {
	// insertion sort — these lists are tiny.
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0 && idLess(ids[j], ids[j-1]); j-- {
			ids[j], ids[j-1] = ids[j-1], ids[j]
		}
	}
}

func idLess(a, b domain.ID) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// flattenSnapshotTree returns the folders and connections in snap.Tree.
// A nil Tree yields two empty slices.
func flattenSnapshotTree(t *domain.Tree) ([]*domain.FolderNode, []*domain.ConnectionNode) {
	if t == nil {
		return nil, nil
	}
	var fs []*domain.FolderNode
	var cs []*domain.ConnectionNode
	_ = t.Walk(func(n domain.Node) error {
		switch v := n.(type) {
		case *domain.FolderNode:
			fs = append(fs, v)
		case *domain.ConnectionNode:
			cs = append(cs, v)
		}
		return nil
	})
	return fs, cs
}

// ValidateRawSnapshot is a helper for tests / tooling that need to validate
// a snapshot assembled without going through domain.Tree (e.g. to reproduce
// a known-bad on-disk state with cycles). It materializes a throwaway Tree-
// equivalent view and runs the same checks. It is not safe to use with a
// snapshot whose folders reference each other in a cycle, because domain.Tree
// refuses to construct one; it is intended for the RawSnapshot shape below.
func ValidateRawSnapshot(r RawSnapshot) []ValidationIssue {
	// Convert the raw shape into the pointer slices the rest of the
	// validator uses. We sidestep domain.Tree entirely.
	folders := make([]*domain.FolderNode, 0, len(r.Folders))
	for i := range r.Folders {
		f := r.Folders[i]
		folders = append(folders, &f)
	}
	connections := make([]*domain.ConnectionNode, 0, len(r.Connections))
	for i := range r.Connections {
		c := r.Connections[i]
		connections = append(connections, &c)
	}

	fakeSnap := &Snapshot{Workspace: r.Workspace}
	issues := validateFlat(folders, connections, fakeSnap)
	return issues
}

// RawSnapshot is a bag of folders/connections/workspace used to exercise the
// validator with states the domain.Tree would refuse to build (e.g. cycles,
// duplicate IDs, orphan parents). Production code should use Snapshot.
type RawSnapshot struct {
	Folders     []domain.FolderNode
	Connections []domain.ConnectionNode
	Workspace   domain.WorkspaceLayout
}

// validateFlat is the shared core of Validate and ValidateRawSnapshot.
func validateFlat(folders []*domain.FolderNode, connections []*domain.ConnectionNode, snap *Snapshot) []ValidationIssue {
	var issues []ValidationIssue

	seen := make(map[domain.ID]string, len(folders)+len(connections))
	for _, f := range folders {
		if prev, ok := seen[f.ID]; ok {
			issues = append(issues, ValidationIssue{
				Severity:   SeverityError,
				Code:       CodeDuplicateID,
				Message:    fmt.Sprintf("duplicate id %s (folder conflicts with %s)", f.ID, prev),
				RelatedIDs: []domain.ID{f.ID},
			})
			continue
		}
		seen[f.ID] = "folder"
	}
	for _, c := range connections {
		if prev, ok := seen[c.ID]; ok {
			issues = append(issues, ValidationIssue{
				Severity:   SeverityError,
				Code:       CodeDuplicateID,
				Message:    fmt.Sprintf("duplicate id %s (connection conflicts with %s)", c.ID, prev),
				RelatedIDs: []domain.ID{c.ID},
			})
			continue
		}
		seen[c.ID] = "connection"
	}

	folderIndex := make(map[domain.ID]*domain.FolderNode, len(folders))
	for _, f := range folders {
		folderIndex[f.ID] = f
	}

	for _, f := range folders {
		if f.ParentID == domain.NilID {
			continue
		}
		if _, ok := folderIndex[f.ParentID]; !ok {
			issues = append(issues, ValidationIssue{
				Severity:   SeverityError,
				Code:       CodeMissingFolderParent,
				Message:    fmt.Sprintf("folder %s references missing parent %s", f.ID, f.ParentID),
				RelatedIDs: []domain.ID{f.ID, f.ParentID},
			})
		}
	}

	connIndex := make(map[domain.ID]*domain.ConnectionNode, len(connections))
	for _, c := range connections {
		connIndex[c.ID] = c
		if c.ParentID == domain.NilID {
			continue
		}
		if _, ok := folderIndex[c.ParentID]; !ok {
			issues = append(issues, ValidationIssue{
				Severity:   SeverityError,
				Code:       CodeMissingConnectionParent,
				Message:    fmt.Sprintf("connection %s references missing parent %s", c.ID, c.ParentID),
				RelatedIDs: []domain.ID{c.ID, c.ParentID},
			})
		}
	}

	reportedCycles := make(map[string]bool)
	for _, f := range folders {
		cycle := folderCycle(f, folderIndex)
		if cycle == nil {
			continue
		}
		key := cycleKey(cycle)
		if reportedCycles[key] {
			continue
		}
		reportedCycles[key] = true
		issues = append(issues, ValidationIssue{
			Severity:   SeverityError,
			Code:       CodeFolderCycle,
			Message:    fmt.Sprintf("folder %s participates in a cycle", f.ID),
			RelatedIDs: cycle,
		})
	}

	if snap != nil {
		for _, tab := range snap.Workspace.OpenTabs {
			if tab.ConnectionID == domain.NilID {
				continue
			}
			if _, ok := connIndex[tab.ConnectionID]; !ok {
				issues = append(issues, ValidationIssue{
					Severity:   SeverityWarn,
					Code:       CodeOrphanWorkspaceTab,
					Message:    fmt.Sprintf("workspace tab references missing connection %s", tab.ConnectionID),
					RelatedIDs: []domain.ID{tab.ConnectionID},
				})
			}
		}
		if snap.Workspace.FocusedTab != domain.NilID {
			if _, ok := connIndex[snap.Workspace.FocusedTab]; !ok {
				issues = append(issues, ValidationIssue{
					Severity:   SeverityWarn,
					Code:       CodeOrphanFocusedTab,
					Message:    fmt.Sprintf("workspace focus references missing connection %s", snap.Workspace.FocusedTab),
					RelatedIDs: []domain.ID{snap.Workspace.FocusedTab},
				})
			}
		}
	}

	return issues
}
