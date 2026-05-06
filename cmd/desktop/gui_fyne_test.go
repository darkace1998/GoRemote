package main

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	iapp "github.com/darkace1998/GoRemote/internal/app"
)

func TestTreeRowDoubleTappedOpensConnection(t *testing.T) {
	test.NewApp()
	defer test.NewApp()

	var opened string
	ct := newConnTree(nil, func(id string) {
		opened = id
	})
	ct.view = iapp.TreeView{Root: &iapp.NodeView{
		ID:   "",
		Kind: "folder",
		Children: []*iapp.NodeView{
			{ID: "conn-1", Kind: "connection", Name: "Prod SSH", Protocol: "ssh", Host: "prod.example.com"},
		},
	}}

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
	ct.view = iapp.TreeView{Root: &iapp.NodeView{
		ID:   "",
		Kind: "folder",
		Children: []*iapp.NodeView{
			{
				ID:   "folder-1",
				Kind: "folder",
				Name: "Production",
				Children: []*iapp.NodeView{
					{ID: "conn-1", Kind: "connection", Name: "Prod SSH"},
				},
			},
		},
	}}

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
	ct.view = iapp.TreeView{Root: &iapp.NodeView{
		ID:   "",
		Kind: "folder",
		Children: []*iapp.NodeView{
			{ID: "folder-1", Kind: "folder", Name: "Production"},
			{ID: "conn-1", Kind: "connection", Name: "Prod SSH"},
		},
	}}

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
	ct.view = iapp.TreeView{Root: &iapp.NodeView{
		ID:   "",
		Kind: "folder",
		Children: []*iapp.NodeView{
			{ID: "conn-1", Kind: "connection", Name: "Prod SSH", Protocol: "ssh", Host: "prod.example.com", Favorite: true},
		},
	}}
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
