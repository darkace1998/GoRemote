package persistence

import (
	"strings"
	"testing"

	"github.com/darkace1998/GoRemote/internal/domain"
)

func TestDecodeInventory_Errors(t *testing.T) {
	idA := domain.NewID()
	idB := domain.NewID()

	tests := []struct {
		name    string
		inv     inventoryFile
		wantErr string
	}{
		{
			name: "orphaned folder",
			inv: inventoryFile{
				Folders: []*domain.FolderNode{
					{ID: domain.NewID(), ParentID: idA, Name: "Orphan"},
				},
			},
			wantErr: "unresolvable folder parents: 1 folder(s) orphaned",
		},
		{
			name: "circular dependency",
			inv: inventoryFile{
				Folders: []*domain.FolderNode{
					{ID: idA, ParentID: idB, Name: "A"},
					{ID: idB, ParentID: idA, Name: "B"},
				},
			},
			wantErr: "unresolvable folder parents: 2 folder(s) orphaned",
		},
		{
			name: "duplicate folder ID",
			inv: inventoryFile{
				Folders: []*domain.FolderNode{
					{ID: idA, ParentID: domain.NilID, Name: "Root1"},
					{ID: idA, ParentID: domain.NilID, Name: "Root2"},
				},
			},
			wantErr: "persistence: add folder",
		},
		{
			name: "duplicate connection ID",
			inv: inventoryFile{
				Connections: []*domain.ConnectionNode{
					{ID: idA, ParentID: domain.NilID, Name: "Conn1"},
					{ID: idA, ParentID: domain.NilID, Name: "Conn2"},
				},
			},
			wantErr: "persistence: add connection",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := decodeInventory(tt.inv)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestDecodeInventory_TopologicalSuccess(t *testing.T) {
	rootID := domain.NewID()
	childID := domain.NewID()
	grandchildID := domain.NewID()

	inv := inventoryFile{
		Folders: []*domain.FolderNode{
			// Reverse order to ensure topological sorting works
			{ID: grandchildID, ParentID: childID, Name: "Grandchild"},
			{ID: childID, ParentID: rootID, Name: "Child"},
			{ID: rootID, ParentID: domain.NilID, Name: "Root"},
		},
	}

	tree, err := decodeInventory(inv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tree == nil {
		t.Fatal("expected tree, got nil")
	}

	// Verify they are added
	f, err := tree.Folder(grandchildID)
	if err != nil || f.Name != "Grandchild" {
		t.Errorf("expected to find Grandchild folder, got err=%v", err)
	}
}

func TestDecodeInventory_NilElements(t *testing.T) {
	rootID := domain.NewID()

	inv := inventoryFile{
		Folders: []*domain.FolderNode{
			nil,
			{ID: rootID, ParentID: domain.NilID, Name: "Root"},
			nil,
		},
		Connections: []*domain.ConnectionNode{
			nil,
			{ID: domain.NewID(), ParentID: rootID, Name: "Conn"},
			nil,
		},
	}

	tree, err := decodeInventory(inv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tree == nil {
		t.Fatal("expected tree, got nil")
	}
}

func TestEncodeInventory(t *testing.T) {
	t.Run("nil tree", func(t *testing.T) {
		inv := encodeInventory(nil)
		if len(inv.Folders) != 0 || len(inv.Connections) != 0 {
			t.Errorf("expected empty inventory for nil tree, got %d folders, %d connections", len(inv.Folders), len(inv.Connections))
		}
	})

	t.Run("empty tree", func(t *testing.T) {
		tree := domain.NewTree()
		inv := encodeInventory(tree)
		if len(inv.Folders) != 0 || len(inv.Connections) != 0 {
			t.Errorf("expected empty inventory for empty tree, got %d folders, %d connections", len(inv.Folders), len(inv.Connections))
		}
	})

	t.Run("populated tree", func(t *testing.T) {
		tree := domain.NewTree()
		rootID := domain.NewID()
		connID := domain.NewID()

		err := tree.AddFolder(&domain.FolderNode{ID: rootID, Name: "Root"})
		if err != nil {
			t.Fatalf("failed to add folder: %v", err)
		}

		err = tree.AddConnection(&domain.ConnectionNode{ID: connID, ParentID: rootID, Name: "Conn1"})
		if err != nil {
			t.Fatalf("failed to add connection: %v", err)
		}

		inv := encodeInventory(tree)

		if len(inv.Folders) != 1 {
			t.Errorf("expected 1 folder, got %d", len(inv.Folders))
		} else if inv.Folders[0].ID != rootID {
			t.Errorf("expected folder ID %v, got %v", rootID, inv.Folders[0].ID)
		}

		if len(inv.Connections) != 1 {
			t.Errorf("expected 1 connection, got %d", len(inv.Connections))
		} else if inv.Connections[0].ID != connID {
			t.Errorf("expected connection ID %v, got %v", connID, inv.Connections[0].ID)
		}
	})
}
