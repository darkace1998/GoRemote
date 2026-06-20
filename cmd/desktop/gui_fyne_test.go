package main

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"

	iapp "github.com/darkace1998/GoRemote/internal/app"
	"github.com/darkace1998/GoRemote/internal/domain"
)

func TestTreeRowDoubleTappedOpensConnection(t *testing.T) {
	test.NewApp()
	defer test.NewApp()

	var opened string
	ct := newConnTree(nil, func(id string) {
		opened = id
	})
	node := &iapp.NodeView{ID: "conn-1", Kind: "connection", Name: "Prod SSH", Protocol: "ssh", Host: "prod.example.com"}
	ct.view = iapp.TreeView{Root: &iapp.NodeView{
		ID:   "",
		Kind: "folder",
		Children: []*iapp.NodeView{
			node,
		},
	}, NodeMap: map[string]*iapp.NodeView{"conn-1": node}}

	row := newTreeRow(ct)
	row.uid = "conn-1"
	row.DoubleTapped(&fyne.PointEvent{})

	if opened != "conn-1" {
		t.Fatalf("DoubleTapped opened %q, want conn-1", opened)
	}
	if selected := ct.selected(); selected != "conn-1" {
		t.Fatalf("DoubleTapped selected %q, want conn-1", selected)
	}
}

func TestTreeRowDoubleTappedTogglesFolderWithoutOpeningSession(t *testing.T) {
	test.NewApp()
	defer test.NewApp()

	var opened string
	ct := newConnTree(nil, func(id string) {
		opened = id
	})
	connNode := &iapp.NodeView{ID: "conn-1", Kind: "connection", Name: "Prod SSH"}
	folderNode := &iapp.NodeView{
		ID:       "folder-1",
		Kind:     "folder",
		Name:     "Production",
		Children: []*iapp.NodeView{connNode},
	}
	ct.view = iapp.TreeView{Root: &iapp.NodeView{
		ID:   "",
		Kind: "folder",
		Children: []*iapp.NodeView{
			folderNode,
		},
	}, NodeMap: map[string]*iapp.NodeView{"folder-1": folderNode, "conn-1": connNode}}

	row := newTreeRow(ct)
	row.uid = "folder-1"
	row.DoubleTapped(&fyne.PointEvent{})

	if opened != "" {
		t.Fatalf("DoubleTapped folder opened session %q, want no session", opened)
	}
	if !ct.tree.IsBranchOpen("folder-1") {
		t.Fatalf("DoubleTapped folder should open the branch")
	}
}

func TestConnectionDetailTextIncludesProtocolEndpointAndEnvironment(t *testing.T) {
	n := &iapp.NodeView{
		Kind:        "connection",
		Protocol:    "ssh",
		Host:        "prod.example.com",
		Port:        22,
		Environment: "prod",
	}

	got := connectionDetailText(n)
	want := "SSH | prod.example.com:22 | prod"
	if got != want {
		t.Fatalf("connectionDetailText() = %q, want %q", got, want)
	}
}

func TestSelectedConnectionReturnsOnlyConnectionNodes(t *testing.T) {
	ct := newConnTree(nil, nil)
	folderNode := &iapp.NodeView{ID: "folder-1", Kind: "folder", Name: "Production"}
	connNode := &iapp.NodeView{ID: "conn-1", Kind: "connection", Name: "Prod SSH"}
	ct.view = iapp.TreeView{Root: &iapp.NodeView{
		ID:   "",
		Kind: "folder",
		Children: []*iapp.NodeView{
			folderNode,
			connNode,
		},
	}, NodeMap: map[string]*iapp.NodeView{"folder-1": folderNode, "conn-1": connNode}}

	ct.selID = "folder-1"
	if got := ct.selectedConnection(); got != "" {
		t.Fatalf("selectedConnection() for folder = %q, want empty", got)
	}

	ct.selID = "conn-1"
	if got := ct.selectedConnection(); got != "conn-1" {
		t.Fatalf("selectedConnection() = %q, want conn-1", got)
	}
}

func TestUpdateItemClearsConnectionDetailsWhenNodeMissing(t *testing.T) {
	test.NewApp()
	defer test.NewApp()

	ct := newConnTree(nil, nil)
	connNode := &iapp.NodeView{ID: "conn-1", Kind: "connection", Name: "Prod SSH", Protocol: "ssh", Host: "prod.example.com", Favorite: true}
	ct.view = iapp.TreeView{Root: &iapp.NodeView{
		ID:   "",
		Kind: "folder",
		Children: []*iapp.NodeView{
			connNode,
		},
	}, NodeMap: map[string]*iapp.NodeView{"conn-1": connNode}}
	row := newTreeRow(ct)

	ct.updateItem("conn-1", row)
	if !row.detail.Visible() {
		t.Fatalf("connection detail should be visible after rendering a connection row")
	}
	if !row.star.Visible() {
		t.Fatalf("favorite star should be visible after rendering a favorite connection row")
	}

	ct.updateItem("missing", row)
	if row.detail.Visible() {
		t.Fatalf("missing node should hide stale connection detail")
	}
	if row.star.Visible() {
		t.Fatalf("missing node should hide stale favorite star")
	}
}

// TestFocusExistingSession_DetachedWindow verifies that focusExistingSession
// does not panic and returns true when the session's tabItem is nil (detached
// to a standalone window).
func TestFocusExistingSession_DetachedWindow(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()

	win := a.NewWindow("detached")
	tabs := container.NewDocTabs()

	hid := domain.NewID()
	st := &sessionTab{
		hid:     hid,
		connID:  "conn-detached",
		tabItem: nil, // detached — no tab
		window:  win,
		cv:      iapp.ConnectionView{Name: "detached-host"},
	}

	registry := &sessionRegistry{
		tabs:      tabs,
		items:     map[domain.ID]*sessionTab{hid: st},
		connItems: map[string]*sessionTab{"conn-detached": st},
		openConns: map[string]struct{}{"conn-detached": {}},
		groups:    map[*container.TabItem]*paneGroup{},
	}

	// Must not panic and must return true (session found).
	if !focusExistingSession(registry, "conn-detached") {
		t.Fatal("focusExistingSession returned false for known connection")
	}
}

// TestFocusExistingSession_NotFound verifies that focusExistingSession
// returns false when no session exists for the given connection ID.
func TestFocusExistingSession_NotFound(t *testing.T) {
	tabs := container.NewDocTabs()
	registry := &sessionRegistry{
		tabs:      tabs,
		items:     map[domain.ID]*sessionTab{},
		openConns: map[string]struct{}{},
		groups:    map[*container.TabItem]*paneGroup{},
	}

	if focusExistingSession(registry, "no-such-conn") {
		t.Fatal("focusExistingSession returned true for unknown connection")
	}
}
