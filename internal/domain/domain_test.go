package domain

import (
	"errors"
	"testing"

	"github.com/goremote/goremote/sdk/credential"
	"github.com/goremote/goremote/sdk/protocol"
)

func mkFolder(name string, parent ID) *FolderNode {
	return &FolderNode{ID: NewID(), ParentID: parent, Name: name}
}

func mkConn(name string, parent ID) *ConnectionNode {
	return &ConnectionNode{ID: NewID(), ParentID: parent, Name: name}
}

func TestTreeAddAndFind(t *testing.T) {
	tr := NewTree()
	root := mkFolder("root", NilID)
	if err := tr.AddFolder(root); err != nil {
		t.Fatalf("AddFolder: %v", err)
	}
	// duplicate id
	if err := tr.AddFolder(root); !errors.Is(err, ErrDuplicateID) {
		t.Fatalf("expected ErrDuplicateID, got %v", err)
	}
	// folder under unknown parent
	bad := mkFolder("bad", NewID())
	if err := tr.AddFolder(bad); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	// connection under root
	c := mkConn("c1", root.ID)
	if err := tr.AddConnection(c); err != nil {
		t.Fatalf("AddConnection: %v", err)
	}
	got, err := tr.FindByID(c.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.NodeKind() != NodeKindConnection {
		t.Fatalf("want connection kind, got %v", got.NodeKind())
	}
	// connection under non-folder
	bc := &ConnectionNode{ID: NewID(), ParentID: c.ID, Name: "bad"}
	if err := tr.AddConnection(bc); !errors.Is(err, ErrParentNotFolder) {
		t.Fatalf("expected ErrParentNotFolder, got %v", err)
	}
}

func TestTreeMoveRejectsCycle(t *testing.T) {
	tr := NewTree()
	a := mkFolder("a", NilID)
	b := mkFolder("b", NilID)
	c := mkFolder("c", NilID)
	for _, f := range []*FolderNode{a, b, c} {
		if err := tr.AddFolder(f); err != nil {
			t.Fatalf("add: %v", err)
		}
	}
	if err := tr.Move(b.ID, a.ID); err != nil {
		t.Fatalf("move b->a: %v", err)
	}
	if err := tr.Move(c.ID, b.ID); err != nil {
		t.Fatalf("move c->b: %v", err)
	}
	// a under c would cycle: a -> c -> b -> a.
	if err := tr.Move(a.ID, c.ID); !errors.Is(err, ErrCycle) {
		t.Fatalf("expected ErrCycle, got %v", err)
	}
	// folder under itself
	if err := tr.Move(a.ID, a.ID); !errors.Is(err, ErrCycle) {
		t.Fatalf("expected ErrCycle moving into self, got %v", err)
	}
	// legal move
	if err := tr.Move(c.ID, NilID); err != nil {
		t.Fatalf("move c to root: %v", err)
	}
	if got, _ := tr.Folder(c.ID); got.ParentID != NilID {
		t.Fatalf("ParentID not updated: %v", got.ParentID)
	}
}

func TestTreeRemoveRecursive(t *testing.T) {
	tr := NewTree()
	root := mkFolder("root", NilID)
	_ = tr.AddFolder(root)
	sub := mkFolder("sub", root.ID)
	_ = tr.AddFolder(sub)
	c := mkConn("c", sub.ID)
	_ = tr.AddConnection(c)
	if err := tr.Remove(root.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := tr.FindByID(sub.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("sub should be gone")
	}
	if _, err := tr.FindByID(c.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("conn should be gone")
	}
}

func TestTreeWalkOrder(t *testing.T) {
	tr := NewTree()
	// Structure:
	// - aa (folder)
	//   - za (conn)
	// - bb (folder)
	// - cc (conn)  (root-level connection)
	aa := &FolderNode{ID: NewID(), Name: "aa"}
	bb := &FolderNode{ID: NewID(), Name: "bb"}
	cc := &ConnectionNode{ID: NewID(), Name: "cc"}
	za := &ConnectionNode{ID: NewID(), Name: "za", ParentID: aa.ID}
	_ = tr.AddFolder(aa)
	_ = tr.AddFolder(bb)
	_ = tr.AddConnection(cc)
	// add za after cc to confirm ordering is by name, not insertion:
	_ = tr.AddConnection(za)

	var names []string
	_ = tr.Walk(func(n Node) error {
		switch v := n.(type) {
		case *FolderNode:
			names = append(names, "F:"+v.Name)
		case *ConnectionNode:
			names = append(names, "C:"+v.Name)
		}
		return nil
	})
	want := []string{"F:aa", "C:za", "F:bb", "C:cc"}
	if len(names) != len(want) {
		t.Fatalf("walk len: got %v want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("walk[%d]: got %q want %q (full %v)", i, names[i], want[i], names)
		}
	}
}

func TestTreeWalkStopsOnError(t *testing.T) {
	tr := NewTree()
	_ = tr.AddFolder(mkFolder("x", NilID))
	sentinel := errors.New("stop")
	err := tr.Walk(func(Node) error { return sentinel })
	if !errors.Is(err, sentinel) {
		t.Fatalf("want sentinel, got %v", err)
	}
}

func TestInheritancePrecedence(t *testing.T) {
	// gp (host=gp.example, user=gpuser)
	//   parent (host=parent.example)
	//     node (empty host, user=explicit)
	gp := &FolderNode{ID: NewID(), Name: "gp", Defaults: FolderDefaults{Host: "gp.example", Username: "gpuser", Port: 22}}
	parent := &FolderNode{ID: NewID(), Name: "parent", ParentID: gp.ID, Defaults: FolderDefaults{Host: "parent.example"}}
	node := &ConnectionNode{ID: NewID(), ParentID: parent.ID, Name: "n", Username: "explicit"}
	ancestors := []*FolderNode{gp, parent} // root-first

	res := node.Inheritance.Resolve(node, ancestors)

	if res.Host != "parent.example" {
		t.Fatalf("host: want parent.example got %q", res.Host)
	}
	if got := res.Trace[FieldHost]; got.Source != ProvenanceFolder || got.FolderID != parent.ID {
		t.Fatalf("host provenance: %+v", got)
	}
	if res.Username != "explicit" {
		t.Fatalf("username: want explicit got %q", res.Username)
	}
	if got := res.Trace[FieldUsername]; got.Source != ProvenanceNode {
		t.Fatalf("username provenance: %+v", got)
	}
	// Port not set on node or parent but set on gp -> inherit from gp.
	if res.Port != 22 {
		t.Fatalf("port: want 22 got %d", res.Port)
	}
	if got := res.Trace[FieldPort]; got.Source != ProvenanceFolder || got.FolderID != gp.ID {
		t.Fatalf("port provenance: %+v", got)
	}
	// ProtocolID nowhere -> default.
	if got := res.Trace[FieldProtocolID]; got.Source != ProvenanceDefault {
		t.Fatalf("protocol provenance: %+v", got)
	}
}

func TestInheritanceForceInheritAndExplicit(t *testing.T) {
	gp := &FolderNode{ID: NewID(), Name: "gp", Defaults: FolderDefaults{Username: "gpuser"}}
	node := &ConnectionNode{ID: NewID(), Name: "n", Username: "localoverride"}
	// Force inherit -> node's "localoverride" must be ignored.
	node.Inheritance.SetInherit(FieldUsername)

	res := node.Inheritance.Resolve(node, []*FolderNode{gp})
	if res.Username != "gpuser" {
		t.Fatalf("force inherit: want gpuser got %q", res.Username)
	}
	if res.Trace[FieldUsername].Source != ProvenanceFolder {
		t.Fatalf("want folder provenance")
	}

	// Force explicit with a zero value -> empty wins.
	node2 := &ConnectionNode{ID: NewID(), Name: "n2", Username: ""}
	node2.Inheritance.SetExplicit(FieldUsername)
	res2 := node2.Inheritance.Resolve(node2, []*FolderNode{gp})
	if res2.Username != "" {
		t.Fatalf("force explicit: want empty got %q", res2.Username)
	}
	if res2.Trace[FieldUsername].Source != ProvenanceNode {
		t.Fatalf("want node provenance")
	}
}

func TestInheritanceCredentialRefAndAuthMethod(t *testing.T) {
	ref := credential.Reference{ProviderID: "p", EntryID: "e"}
	gp := &FolderNode{ID: NewID(), Name: "gp", Defaults: FolderDefaults{
		CredentialRef: ref,
		AuthMethod:    protocol.AuthPassword,
	}}
	node := &ConnectionNode{ID: NewID(), Name: "n"}
	res := node.Inheritance.Resolve(node, []*FolderNode{gp})
	if res.CredentialRef.ProviderID != "p" || res.CredentialRef.EntryID != "e" {
		t.Fatalf("ref not inherited: %+v", res.CredentialRef)
	}
	if res.AuthMethod != protocol.AuthPassword {
		t.Fatalf("authmethod: got %v", res.AuthMethod)
	}
}

func TestReverseFolders(t *testing.T) {
	a := &FolderNode{Name: "a"}
	b := &FolderNode{Name: "b"}
	c := &FolderNode{Name: "c"}
	got := ReverseFolders([]*FolderNode{a, b, c})
	if got[0] != c || got[1] != b || got[2] != a {
		t.Fatalf("reverse failed")
	}
}

func TestSearchPredicates(t *testing.T) {
	tr := NewTree()
	root := mkFolder("root", NilID)
	_ = tr.AddFolder(root)
	prod := &FolderNode{ID: NewID(), ParentID: root.ID, Name: "prod", Tags: []string{"prod"}}
	_ = tr.AddFolder(prod)
	sshA := &ConnectionNode{ID: NewID(), ParentID: prod.ID, Name: "webA", ProtocolID: "ssh", Tags: []string{"prod", "web"}}
	sshB := &ConnectionNode{ID: NewID(), ParentID: prod.ID, Name: "dbA", ProtocolID: "ssh", Tags: []string{"db"}}
	rdpC := &ConnectionNode{ID: NewID(), ParentID: prod.ID, Name: "winA", ProtocolID: "rdp", Tags: []string{"prod"}}
	for _, c := range []*ConnectionNode{sshA, sshB, rdpC} {
		if err := tr.AddConnection(c); err != nil {
			t.Fatal(err)
		}
	}

	// MatchAll returns every node (2 folders + 3 connections = 5)
	if got := tr.Search(MatchAll); len(got) != 5 {
		t.Fatalf("MatchAll: got %d", len(got))
	}
	// MatchNone returns nothing.
	if got := tr.Search(MatchNone); len(got) != 0 {
		t.Fatalf("MatchNone: got %d", len(got))
	}
	// MatchProtocol only matches connections.
	ssh := tr.Search(MatchProtocol("ssh"))
	if len(ssh) != 2 {
		t.Fatalf("MatchProtocol ssh: got %d", len(ssh))
	}
	// MatchName is case-insensitive.
	names := tr.Search(MatchName("WEB"))
	if len(names) != 1 || names[0].NodeID() != sshA.ID {
		t.Fatalf("MatchName WEB: %v", names)
	}
	// MatchTag
	dbMatches := tr.Search(MatchTag("db"))
	if len(dbMatches) != 1 || dbMatches[0].NodeID() != sshB.ID {
		t.Fatalf("MatchTag db: %v", dbMatches)
	}

	// And: ssh AND prod tag -> just sshA.
	andRes := tr.Search(And(MatchProtocol("ssh"), MatchTag("prod")))
	if len(andRes) != 1 || andRes[0].NodeID() != sshA.ID {
		t.Fatalf("And: %v", andRes)
	}
	// Empty And matches everything.
	if got := tr.Search(And()); len(got) != 5 {
		t.Fatalf("empty And: %d", len(got))
	}

	// Or: rdp OR tag=db -> rdpC + sshB.
	orRes := tr.Search(Or(MatchProtocol("rdp"), MatchTag("db")))
	if len(orRes) != 2 {
		t.Fatalf("Or: got %d", len(orRes))
	}
	// Empty Or matches nothing.
	if got := tr.Search(Or()); len(got) != 0 {
		t.Fatalf("empty Or: %d", len(got))
	}

	// Not(MatchProtocol(ssh)) among connections -> rdpC (folders also match because they're not ssh connections).
	notSSH := tr.Search(And(Not(MatchProtocol("ssh")), PredicateFunc(func(n Node) bool {
		_, ok := n.(*ConnectionNode)
		return ok
	})))
	if len(notSSH) != 1 || notSSH[0].NodeID() != rdpC.ID {
		t.Fatalf("Not: %v", notSSH)
	}
	// Not(nil) matches everything.
	if got := tr.Search(Not(nil)); len(got) != 5 {
		t.Fatalf("Not(nil): %d", len(got))
	}
}

func TestTreeAncestors(t *testing.T) {
	tr := NewTree()
	a := mkFolder("a", NilID)
	b := mkFolder("b", NilID)
	c := mkFolder("c", NilID)
	_ = tr.AddFolder(a)
	b.ParentID = a.ID
	_ = tr.AddFolder(b)
	c.ParentID = b.ID
	_ = tr.AddFolder(c)
	conn := mkConn("x", c.ID)
	_ = tr.AddConnection(conn)
	anc, err := tr.Ancestors(conn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(anc) != 3 || anc[0].ID != a.ID || anc[1].ID != b.ID || anc[2].ID != c.ID {
		t.Fatalf("ancestors order wrong: %v", anc)
	}
}

func TestTemplateApply(t *testing.T) {
	tpl := ConnectionTemplate{
		ProtocolID: "ssh",
		Port:       2222,
		Username:   "admin",
		AuthMethod: protocol.AuthPublicKey,
		Settings:   map[string]any{"keepalive": 30},
		Tags:       []string{"template"},
	}
	n := &ConnectionNode{ID: NewID(), Name: "new", Username: "alice"}
	tpl.Apply(n)
	if n.ProtocolID != "ssh" || n.Port != 2222 {
		t.Fatalf("template defaults not applied: %+v", n)
	}
	if n.Username != "alice" {
		t.Fatalf("template overwrote set field: %q", n.Username)
	}
	if n.Settings["keepalive"] != 30 {
		t.Fatalf("settings not copied")
	}
	// Mutating the template's map must not affect the node's (isolation).
	tpl.Settings["keepalive"] = 99
	if n.Settings["keepalive"] != 30 {
		t.Fatalf("template settings not isolated")
	}
}
