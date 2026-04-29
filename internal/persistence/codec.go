package persistence

import (
	"encoding/json"
	"fmt"

	"github.com/darkace1998/GoRemote/internal/domain"
)

// File names persisted under Store.Dir().
const (
	FileInventory = "inventory.json"
	FileTemplates = "templates.json"
	FileWorkspace = "workspace.json"
	FileMeta      = "meta.json"
)

// inventoryFile is the JSON-on-disk shape for the inventory. The domain.Tree
// exposes only behavioural API, so we serialize its contents as two flat
// arrays and reconstruct the Tree on load.
type inventoryFile struct {
	Folders     []*domain.FolderNode     `json:"folders"`
	Connections []*domain.ConnectionNode `json:"connections"`
}

// templatesFile is the on-disk shape for the templates list.
type templatesFile struct {
	Templates []domain.ConnectionTemplate `json:"templates"`
}

// workspaceFile is the on-disk shape for the workspace layout.
type workspaceFile struct {
	Workspace domain.WorkspaceLayout `json:"workspace"`
}

// encodeInventory serializes a Tree to the on-disk shape. Walk yields
// parents before their children, so rebuilding on load can add nodes in
// the order stored.
func encodeInventory(t *domain.Tree) inventoryFile {
	var inv inventoryFile
	if t == nil {
		return inv
	}
	_ = t.Walk(func(n domain.Node) error {
		switch v := n.(type) {
		case *domain.FolderNode:
			inv.Folders = append(inv.Folders, v)
		case *domain.ConnectionNode:
			inv.Connections = append(inv.Connections, v)
		}
		return nil
	})
	return inv
}

// decodeInventory rebuilds a domain.Tree from an inventoryFile. Folders are
// inserted before connections, topologically sorted so each folder's parent
// exists before the child is added.
func decodeInventory(inv inventoryFile) (*domain.Tree, error) {
	t := domain.NewTree()

	// Topologically order folders: parents first. Repeatedly add folders
	// whose parent is NilID or already present. Any folder whose parent is
	// missing after no progress is made is reported as an error.
	remaining := append([]*domain.FolderNode(nil), inv.Folders...)
	added := make(map[domain.ID]bool, len(remaining))
	for len(remaining) > 0 {
		progress := false
		next := remaining[:0]
		for _, f := range remaining {
			if f == nil {
				continue
			}
			if f.ParentID == domain.NilID || added[f.ParentID] {
				if err := t.AddFolder(f); err != nil {
					return nil, fmt.Errorf("persistence: add folder %s: %w", f.ID, err)
				}
				added[f.ID] = true
				progress = true
				continue
			}
			next = append(next, f)
		}
		if !progress {
			return nil, fmt.Errorf("persistence: unresolvable folder parents: %d folder(s) orphaned", len(next))
		}
		remaining = next
	}

	for _, c := range inv.Connections {
		if c == nil {
			continue
		}
		if err := t.AddConnection(c); err != nil {
			return nil, fmt.Errorf("persistence: add connection %s: %w", c.ID, err)
		}
	}
	return t, nil
}

// decodeJSON unmarshals data into v, returning a wrapped error on failure.
// An empty or missing input yields the zero value without error.
func decodeJSON(name string, data []byte, v any) error {
	if len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("persistence: decode %s: %w", name, err)
	}
	return nil
}
