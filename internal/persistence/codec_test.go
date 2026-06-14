package persistence

import (
	"testing"
	"github.com/darkace1998/GoRemote/internal/domain"
)

func TestEncodeInventory_NilTree(t *testing.T) {
	inv := encodeInventory(nil)
	if len(inv.Folders) != 0 || len(inv.Connections) != 0 {
		t.Errorf("Expected empty inventory for nil tree, got %+v", inv)
	}
}

func TestEncodeInventory_EmptyTree(t *testing.T) {
	tree := domain.NewTree()
	inv := encodeInventory(tree)
	if len(inv.Folders) != 0 || len(inv.Connections) != 0 {
		t.Errorf("Expected empty inventory for empty tree, got %+v", inv)
	}
}

func TestEncodeInventory_PopulatedTree(t *testing.T) {
	tree := domain.NewTree()

	// Create folders
	f1 := &domain.FolderNode{ID: domain.NewID(), ParentID: domain.NilID, Name: "F1"}
	f2 := &domain.FolderNode{ID: domain.NewID(), ParentID: f1.ID, Name: "F2"}

	if err := tree.AddFolder(f1); err != nil {
		t.Fatalf("AddFolder: %v", err)
	}
	if err := tree.AddFolder(f2); err != nil {
		t.Fatalf("AddFolder: %v", err)
	}

	// Create connections
	c1 := &domain.ConnectionNode{ID: domain.NewID(), ParentID: f1.ID, Name: "C1"}
	c2 := &domain.ConnectionNode{ID: domain.NewID(), ParentID: f2.ID, Name: "C2"}

	if err := tree.AddConnection(c1); err != nil {
		t.Fatalf("AddConnection: %v", err)
	}
	if err := tree.AddConnection(c2); err != nil {
		t.Fatalf("AddConnection: %v", err)
	}

	inv := encodeInventory(tree)

	if len(inv.Folders) != 2 {
		t.Errorf("Expected 2 folders, got %d", len(inv.Folders))
	}
	if len(inv.Connections) != 2 {
		t.Errorf("Expected 2 connections, got %d", len(inv.Connections))
	}

	// The Tree.Walk method guarantees a certain order (parents before children).
	// We check if all items are present, but might need to sort or search.
	// Since there are only 2 items we can just verify them.

	foldersSet := make(map[domain.ID]bool)
	for _, f := range inv.Folders {
		foldersSet[f.ID] = true
	}
	if !foldersSet[f1.ID] || !foldersSet[f2.ID] {
		t.Errorf("Expected folders F1 and F2 to be encoded, got %v", inv.Folders)
	}

	connectionsSet := make(map[domain.ID]bool)
	for _, c := range inv.Connections {
		connectionsSet[c.ID] = true
	}
	if !connectionsSet[c1.ID] || !connectionsSet[c2.ID] {
		t.Errorf("Expected connections C1 and C2 to be encoded, got %v", inv.Connections)
	}

	// Test order constraints (parents before children)
	// F1 should be before F2
	f1Idx, f2Idx := -1, -1
	for i, f := range inv.Folders {
		if f.ID == f1.ID {
			f1Idx = i
		} else if f.ID == f2.ID {
			f2Idx = i
		}
	}
	if f1Idx > f2Idx {
		t.Errorf("Expected parent folder F1 to be before F2, got F1 at %d, F2 at %d", f1Idx, f2Idx)
	}
}
