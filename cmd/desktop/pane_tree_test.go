package main

import (
	"reflect"
	"testing"

	appworkspace "github.com/goremote/goremote/app/workspace"
)

// paneGroup tree operations are pure data manipulation — they do not
// touch any Fyne widgets unless you call buildLeaf / setActive on a
// node that already has a titleBtn. These tests therefore run in a
// headless environment without a fyne.App.

func mkLeaf(connID string) *sessionTab {
	return &sessionTab{connID: connID}
}

func leafConns(g *paneGroup) []string {
	out := make([]string, 0)
	for _, lf := range g.leaves() {
		out = append(out, lf.session.connID)
	}
	return out
}

func TestPaneGroupSplitLeafBuildsBalancedTree(t *testing.T) {
	a := mkLeaf("A")
	root := &paneNode{session: a}
	g := &paneGroup{root: root, active: root}

	// First split: A -> [A | B]
	b := mkLeaf("B")
	bLeaf := g.splitLeaf(g.active, b, "h")
	if bLeaf == nil || bLeaf.session != b {
		t.Fatalf("splitLeaf #1 returned %v", bLeaf)
	}
	if g.active != bLeaf {
		t.Fatalf("active should be the new leaf, got %v", g.active)
	}
	if got, want := leafConns(g), []string{"A", "B"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after split #1 leaves = %v, want %v", got, want)
	}

	// Second split on the active leaf (B): A -> [A | [B / C]]
	c := mkLeaf("C")
	cLeaf := g.splitLeaf(g.active, c, "v")
	if cLeaf == nil || cLeaf.session != c {
		t.Fatalf("splitLeaf #2 returned %v", cLeaf)
	}
	if got, want := leafConns(g), []string{"A", "B", "C"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after split #2 leaves = %v, want %v", got, want)
	}
	// The B/C branch should be vertical; the outer should still be horizontal.
	if g.root.orientation != "h" {
		t.Errorf("root orientation = %q, want h", g.root.orientation)
	}
	if g.root.b.orientation != "v" {
		t.Errorf("inner branch orientation = %q, want v", g.root.b.orientation)
	}

	// Third split on A: [[A / D] | [B / C]]
	d := mkLeaf("D")
	g.active = g.root.a // switch focus to A's leaf
	dLeaf := g.splitLeaf(g.active, d, "v")
	if dLeaf == nil {
		t.Fatalf("splitLeaf #3 returned nil")
	}
	if got, want := leafConns(g), []string{"A", "D", "B", "C"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after split #3 leaves = %v, want %v", got, want)
	}

	// Parent links should be intact.
	for _, lf := range g.leaves() {
		if lf.parent == nil {
			t.Errorf("leaf %s has nil parent", lf.session.connID)
		}
	}
}

func TestPaneGroupSplitLeafDefaultsBadOrientation(t *testing.T) {
	a := mkLeaf("A")
	g := &paneGroup{root: &paneNode{session: a}, active: nil}
	g.active = g.root
	b := mkLeaf("B")
	g.splitLeaf(g.active, b, "diagonal")
	if g.root.orientation != "h" {
		t.Errorf("expected fallback to horizontal, got %q", g.root.orientation)
	}
}

func TestPaneGroupRemoveLeafCollapsesSibling(t *testing.T) {
	// Build [A | [B / C]] then remove B; result should be [A | C].
	a := mkLeaf("A")
	g := &paneGroup{root: &paneNode{session: a}}
	g.active = g.root
	b := mkLeaf("B")
	g.splitLeaf(g.active, b, "h")
	c := mkLeaf("C")
	g.splitLeaf(g.active, c, "v")

	ok, empty := g.removeLeaf(b)
	if !ok || empty {
		t.Fatalf("removeLeaf(b) ok=%v empty=%v, want ok=true empty=false", ok, empty)
	}
	if got, want := leafConns(g), []string{"A", "C"}; !reflect.DeepEqual(got, want) {
		t.Errorf("after remove leaves = %v, want %v", got, want)
	}
	// Root should remain horizontal with C promoted.
	if g.root.orientation != "h" {
		t.Errorf("root orientation = %q, want h", g.root.orientation)
	}
	if g.root.b == nil || !g.root.b.isLeaf() || g.root.b.session != c {
		t.Errorf("right child should be C leaf, got %#v", g.root.b)
	}
	// C's parent must point at the new root.
	if g.root.b.parent != g.root {
		t.Errorf("collapsed leaf parent not rewired: parent=%p root=%p", g.root.b.parent, g.root)
	}
}

func TestPaneGroupRemoveLeafEmptyTab(t *testing.T) {
	a := mkLeaf("A")
	g := &paneGroup{root: &paneNode{session: a}}
	g.active = g.root
	ok, empty := g.removeLeaf(a)
	if !ok || !empty {
		t.Fatalf("removeLeaf(a) ok=%v empty=%v, want ok=true empty=true", ok, empty)
	}
	if g.root != nil || g.active != nil {
		t.Errorf("expected nil root + active after empty, got root=%v active=%v", g.root, g.active)
	}
}

func TestPaneGroupRemoveLeafNotFound(t *testing.T) {
	a := mkLeaf("A")
	g := &paneGroup{root: &paneNode{session: a}}
	g.active = g.root
	other := mkLeaf("X")
	ok, empty := g.removeLeaf(other)
	if ok || empty {
		t.Errorf("removeLeaf(other) ok=%v empty=%v, want false/false", ok, empty)
	}
}

func TestPaneGroupRemoveActiveReassigns(t *testing.T) {
	// [A | B], active=B, remove B -> active should land on A.
	a := mkLeaf("A")
	g := &paneGroup{root: &paneNode{session: a}}
	g.active = g.root
	b := mkLeaf("B")
	g.splitLeaf(g.active, b, "h") // active moves to B
	if g.active.session != b {
		t.Fatalf("post-split active = %v, want B", g.active)
	}
	g.removeLeaf(b)
	if g.active == nil || g.active.session != a {
		t.Errorf("after removing active, expected active=A, got %v", g.active)
	}
}

func TestPaneGroupLeafForFindsByPointer(t *testing.T) {
	a, b := mkLeaf("A"), mkLeaf("B")
	g := &paneGroup{root: &paneNode{session: a}}
	g.active = g.root
	g.splitLeaf(g.active, b, "h")

	lf := g.leafFor(a)
	if lf == nil || lf.session != a {
		t.Errorf("leafFor(a) = %v, want leaf for A", lf)
	}
	if g.leafFor(mkLeaf("Z")) != nil {
		t.Error("leafFor on unknown session should return nil")
	}
}

func TestSnapshotPaneTreeMatchesShape(t *testing.T) {
	// [A | [B / C]]
	a := mkLeaf("A")
	g := &paneGroup{root: &paneNode{session: a}}
	g.active = g.root
	g.splitLeaf(g.active, mkLeaf("B"), "h")
	g.splitLeaf(g.active, mkLeaf("C"), "v")

	got := snapshotPaneTree(g.root)
	want := &appworkspace.PaneNode{
		Orientation: "h",
		A:           &appworkspace.PaneNode{ConnectionID: "A"},
		B: &appworkspace.PaneNode{
			Orientation: "v",
			A:           &appworkspace.PaneNode{ConnectionID: "B"},
			B:           &appworkspace.PaneNode{ConnectionID: "C"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("snapshot mismatch:\n got=%#v\nwant=%#v", got, want)
	}
}

func TestSnapshotPaneTreeNil(t *testing.T) {
	if snapshotPaneTree(nil) != nil {
		t.Error("snapshotPaneTree(nil) should return nil")
	}
}

func TestPaneGroupMemberSessionsTraversalOrder(t *testing.T) {
	a, b, c := mkLeaf("A"), mkLeaf("B"), mkLeaf("C")
	g := &paneGroup{root: &paneNode{session: a}}
	g.active = g.root
	g.splitLeaf(g.active, b, "h") // [A | B]
	g.active = g.root.a            // back to A
	g.splitLeaf(g.active, c, "v") // [[A / C] | B]
	got := []string{}
	for _, st := range g.memberSessions() {
		got = append(got, st.connID)
	}
	if want := []string{"A", "C", "B"}; !reflect.DeepEqual(got, want) {
		t.Errorf("memberSessions order = %v, want %v", got, want)
	}
}
