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
		{
			name: "partial success but some orphaned",
			inv: inventoryFile{
				Folders: []*domain.FolderNode{
					{ID: domain.NewID(), ParentID: domain.NilID, Name: "Valid Root"},
					{ID: idA, ParentID: idB, Name: "Orphan A"},
					{ID: idB, ParentID: idA, Name: "Orphan B"},
					{ID: domain.NewID(), ParentID: domain.NewID(), Name: "Orphan C"},
				},
			},
			wantErr: "unresolvable folder parents: 3 folder(s) orphaned",
		},
		{
			name: "folder parented to a connection",
			inv: inventoryFile{
				Folders: []*domain.FolderNode{
					{ID: domain.NewID(), ParentID: idA, Name: "Bad Folder"},
				},
				Connections: []*domain.ConnectionNode{
					{ID: idA, ParentID: domain.NilID, Name: "Root Conn"},
				},
			},
			wantErr: "unresolvable folder parents: 1 folder(s) orphaned",
		},
		{
			name: "folder with nil id",
			inv: inventoryFile{
				Folders: []*domain.FolderNode{
					{ID: domain.NilID, ParentID: domain.NilID, Name: "No ID"},
				},
			},
			wantErr: "persistence: add folder",
		},
		{
			name: "connection with nil id",
			inv: inventoryFile{
				Connections: []*domain.ConnectionNode{
					{ID: domain.NilID, ParentID: domain.NilID, Name: "No ID"},
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
