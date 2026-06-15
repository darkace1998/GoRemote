package domain

import (
	"errors"
	"testing"

	"github.com/darkace1998/GoRemote/sdk/credential"
	"github.com/darkace1998/GoRemote/sdk/protocol"
)

func mkFolder(name string, parent ID) *FolderNode {
	return &FolderNode{ID: NewID(), ParentID: parent, Name: name}
}

func mkConn(name string, parent ID) *ConnectionNode {
	return &ConnectionNode{ID: NewID(), ParentID: parent, Name: name}
}

func TestTreeAddFolder(t *testing.T) {
	t.Run("nil folder", func(t *testing.T) {
		tr := NewTree()
		err := tr.AddFolder(nil)
		if err == nil || err.Error() != "domain: nil folder" {
			t.Fatalf("expected 'domain: nil folder', got %v", err)
		}
	})

	t.Run("empty ID", func(t *testing.T) {
		tr := NewTree()
		f := &FolderNode{ID: NilID, Name: "empty"}
		err := tr.AddFolder(f)
		if err == nil || err.Error() != "domain: folder id is required" {
			t.Fatalf("expected 'domain: folder id is required', got %v", err)
		}
	})

	t.Run("duplicate folder ID", func(t *testing.T) {
		tr := NewTree()
		f := mkFolder("f1", NilID)
		_ = tr.AddFolder(f)

		err := tr.AddFolder(f)
		if !errors.Is(err, ErrDuplicateID) {
			t.Fatalf("expected ErrDuplicateID, got %v", err)
		}
	})

	t.Run("duplicate connection ID", func(t *testing.T) {
		tr := NewTree()
		f := mkFolder("f1", NilID)
		_ = tr.AddFolder(f)
		c := mkConn("c1", f.ID)
		_ = tr.AddConnection(c)

		badF := &FolderNode{ID: c.ID, Name: "bad"}
		err := tr.AddFolder(badF)
		if !errors.Is(err, ErrDuplicateID) {
			t.Fatalf("expected ErrDuplicateID, got %v", err)
		}
	})

	t.Run("missing parent", func(t *testing.T) {
		tr := NewTree()
		f := mkFolder("f1", NewID())
		err := tr.AddFolder(f)
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("success root and child", func(t *testing.T) {
		tr := NewTree()
		root := mkFolder("root", NilID)
		if err := tr.AddFolder(root); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		child := mkFolder("child", root.ID)
		if err := tr.AddFolder(child); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		gotRoot, err := tr.Folder(root.ID)
		if err != nil || gotRoot != root {
			t.Fatalf("expected root folder %v, got %v, err %v", root, gotRoot, err)
		}

		gotChild, err := tr.Folder(child.ID)
		if err != nil || gotChild != child {
			t.Fatalf("expected child folder %v, got %v, err %v", child, gotChild, err)
		}

		if len(tr.folderChildren[root.ID]) != 1 || tr.folderChildren[root.ID][0] != child.ID {
			t.Fatalf("expected child %v in root children %v", child.ID, tr.folderChildren[root.ID])
		}
	})
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

func TestInheritanceResolveNilNode(t *testing.T) {
	var p InheritanceProfile
	res := p.Resolve(nil, nil)
	if res.Trace == nil {
		t.Fatal("expected Trace to be initialized")
	}
	if len(res.Trace) != 0 {
		t.Fatalf("expected empty Trace, got len %d", len(res.Trace))
	}
}

func TestInheritanceResolve(t *testing.T) {
	gp := &FolderNode{ID: NewID(), Name: "gp", Defaults: FolderDefaults{Host: "gp.example", Username: "gpuser", Port: 22}}
	parent := &FolderNode{ID: NewID(), Name: "parent", ParentID: gp.ID, Defaults: FolderDefaults{Host: "parent.example"}}
	node := &ConnectionNode{ID: NewID(), ParentID: parent.ID, Name: "n", Username: "explicit"}

	node.Inheritance.SetInherit(FieldPort)

	ancestors := []*FolderNode{gp, parent}

	res := node.Inheritance.Resolve(node, ancestors)

	if res.ID != node.ID {
		t.Fatalf("expected ID %v, got %v", node.ID, res.ID)
	}

	for _, f := range AllInheritableFields {
		if _, ok := res.Trace[f]; !ok {
			t.Errorf("missing Trace entry for field: %v", f)
		}
	}

	if prov := res.Trace[FieldHost]; prov.Source != ProvenanceFolder || prov.FolderID != parent.ID {
		t.Errorf("host provenance mismatch: %+v", prov)
	}

	if prov := res.Trace[FieldUsername]; prov.Source != ProvenanceNode {
		t.Errorf("username provenance mismatch: %+v", prov)
	}

	if prov := res.Trace[FieldPort]; prov.Source != ProvenanceFolder || prov.FolderID != gp.ID {
		t.Errorf("port provenance mismatch: %+v", prov)
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

func TestTreeMoveEdgeCases(t *testing.T) {
	tr := NewTree()
	f1 := mkFolder("f1", NilID)
	f2 := mkFolder("f2", NilID)
	c1 := mkConn("c1", f1.ID)

	for _, f := range []*FolderNode{f1, f2} {
		if err := tr.AddFolder(f); err != nil {
			t.Fatalf("add folder: %v", err)
		}
	}
	if err := tr.AddConnection(c1); err != nil {
		t.Fatalf("add connection: %v", err)
	}

	// Move root
	if err := tr.Move(NilID, f1.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound moving root, got %v", err)
	}

	// Move to non-existent folder
	nonExistentID := NewID()
	if err := tr.Move(f1.ID, nonExistentID); !errors.Is(err, ErrParentNotFolder) {
		t.Fatalf("expected ErrParentNotFolder moving to non-existent folder, got %v", err)
	}

	// Move to a connection (not a folder)
	if err := tr.Move(f1.ID, c1.ID); !errors.Is(err, ErrParentNotFolder) {
		t.Fatalf("expected ErrParentNotFolder moving to connection, got %v", err)
	}

	// Move connection (successful move)
	if err := tr.Move(c1.ID, f2.ID); err != nil {
		t.Fatalf("move connection failed: %v", err)
	}
	if got, _ := tr.Connection(c1.ID); got.ParentID != f2.ID {
		t.Fatalf("connection ParentID not updated: %v", got.ParentID)
	}

	// Move non-existent ID
	if err := tr.Move(nonExistentID, f1.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound moving non-existent ID, got %v", err)
	}
}

func TestInheritanceResolve_Comprehensive(t *testing.T) {
	gpID := NewID()
	pID := NewID()
	nID := NewID()

	gp := &FolderNode{
		ID:   gpID,
		Name: "Grandparent",
		Defaults: FolderDefaults{
			ProtocolID:  "ssh",
			Host:        "gp.example.com",
			Port:        22,
			Environment: "prod",
			Tags:        []string{"gp-tag"},
			Color:       "#gpcolor",
		},
	}

	parent := &FolderNode{
		ID:       pID,
		ParentID: gpID,
		Name:     "Parent",
		Defaults: FolderDefaults{
			Host:        "p.example.com",
			Username:    "puser",
			Icon:        "picon",
			Description: "pdesc",
		},
	}

	node := &ConnectionNode{
		ID:       nID,
		ParentID: pID,
		Name:     "Node",
		Host:     "n.example.com",
		Username: "nuser",
		Settings: map[string]any{"timeout": 30},
	}

	// Setup Inheritance Profile
	// 1. Force Explicit: Node's value should win even if empty
	node.Inheritance.SetExplicit(FieldPort) // Node port is 0, should stay 0 despite GP having 22

	// 2. Force Inherit: Node's value should be ignored
	node.Inheritance.SetInherit(FieldHost) // Node has "n.example.com", Parent has "p.example.com". Parent should win.

	// 3. Force Inherit where no ancestor has value: should trigger zeroNodeField
	node.Inheritance.SetInherit(FieldSettings) // Node has settings, ancestors do not. Should become nil.

	ancestors := []*FolderNode{gp, parent} // Top-down

	res := node.Inheritance.Resolve(node, ancestors)

	tests := []struct {
		name    string
		field   Field
		wantVal any
		wantSrc ProvenanceSource
		wantFld ID
	}{
		{
			name:    "Explicit override on zero value (Port)",
			field:   FieldPort,
			wantVal: 0,
			wantSrc: ProvenanceNode,
			wantFld: NilID,
		},
		{
			name:    "Inherit override with ancestor value (Host)",
			field:   FieldHost,
			wantVal: "p.example.com",
			wantSrc: ProvenanceFolder,
			wantFld: pID,
		},
		{
			name:    "Inherit override with no ancestor value (Settings)",
			field:   FieldSettings,
			wantVal: (map[string]any)(nil),
			wantSrc: ProvenanceDefault,
			wantFld: NilID,
		},
		{
			name:    "Node value wins when not overridden (Username)",
			field:   FieldUsername,
			wantVal: "nuser",
			wantSrc: ProvenanceNode,
			wantFld: NilID,
		},
		{
			name:    "Fallback to parent (Icon)",
			field:   FieldIcon,
			wantVal: "picon",
			wantSrc: ProvenanceFolder,
			wantFld: pID,
		},
		{
			name:    "Fallback to grandparent (Environment)",
			field:   FieldEnvironment,
			wantVal: "prod",
			wantSrc: ProvenanceFolder,
			wantFld: gpID,
		},
		{
			name:    "Default when no one has it (AuthMethod)",
			field:   FieldAuthMethod,
			wantVal: protocol.AuthMethod(""),
			wantSrc: ProvenanceDefault,
			wantFld: NilID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prov, ok := res.Trace[tt.field]
			if !ok {
				t.Fatalf("missing trace for field %v", tt.field)
			}

			if prov.Source != tt.wantSrc {
				t.Errorf("got source %v, want %v", prov.Source, tt.wantSrc)
			}
			if prov.FolderID != tt.wantFld {
				t.Errorf("got folder %v, want %v", prov.FolderID, tt.wantFld)
			}

			// Check actual value
			var gotVal any
			switch tt.field {
			case FieldPort:
				gotVal = res.Port
			case FieldHost:
				gotVal = res.Host
			case FieldSettings:
				gotVal = res.Settings
			case FieldUsername:
				gotVal = res.Username
			case FieldIcon:
				gotVal = res.Icon
			case FieldEnvironment:
				gotVal = res.Environment
			case FieldAuthMethod:
				gotVal = res.AuthMethod
			}

			// Simple comparison for non-maps/slices
			if tt.field == FieldSettings {
				gotM, gotOk := gotVal.(map[string]any)
				wantM, wantOk := tt.wantVal.(map[string]any)
				if gotOk && wantOk {
					if len(gotM) != len(wantM) {
						t.Errorf("got settings %v, want %v", gotVal, tt.wantVal)
					}
				} else if gotVal != nil || tt.wantVal != nil {
					t.Errorf("got settings %v, want %v", gotVal, tt.wantVal)
				}
			} else {
				if gotVal != tt.wantVal {
					t.Errorf("got val %v, want %v", gotVal, tt.wantVal)
				}
			}
		})
	}
}

func TestInheritanceResolve_ZeroNodeField_All(t *testing.T) {
	node := &ConnectionNode{
		ID:            NewID(),
		ProtocolID:    "ssh",
		Host:          "host",
		Port:          22,
		Username:      "user",
		AuthMethod:    protocol.AuthPassword,
		CredentialRef: credential.Reference{ProviderID: "p"},
		Settings:      map[string]any{"a": "b"},
		Tags:          []string{"tag"},
		Color:         "color",
		Icon:          "icon",
		Environment:   "env",
		Description:   "desc",
	}

	for _, f := range AllInheritableFields {
		node.Inheritance.SetInherit(f)
	}

	res := node.Inheritance.Resolve(node, nil)

	if res.ProtocolID != "" {
		t.Errorf("ProtocolID not zeroed")
	}
	if res.Host != "" {
		t.Errorf("Host not zeroed")
	}
	if res.Port != 0 {
		t.Errorf("Port not zeroed")
	}
	if res.Username != "" {
		t.Errorf("Username not zeroed")
	}
	if res.AuthMethod != "" {
		t.Errorf("AuthMethod not zeroed")
	}
	if !isRefZero(res.CredentialRef) {
		t.Errorf("CredentialRef not zeroed")
	}
	if res.Settings != nil {
		t.Errorf("Settings not zeroed")
	}
	if res.Tags != nil {
		t.Errorf("Tags not zeroed")
	}
	if res.Color != "" {
		t.Errorf("Color not zeroed")
	}
	if res.Icon != "" {
		t.Errorf("Icon not zeroed")
	}
	if res.Environment != "" {
		t.Errorf("Environment not zeroed")
	}
	if res.Description != "" {
		t.Errorf("Description not zeroed")
	}
}

func TestInheritanceResolve_CopyFolderField_All(t *testing.T) {
	parent := &FolderNode{
		ID: NewID(),
		Defaults: FolderDefaults{
			ProtocolID:    "ssh",
			Host:          "host",
			Port:          22,
			Username:      "user",
			AuthMethod:    protocol.AuthPassword,
			CredentialRef: credential.Reference{ProviderID: "p"},
			Settings:      map[string]any{"a": "b"},
			Tags:          []string{"tag"},
			Color:         "color",
			Icon:          "icon",
			Environment:   "env",
			Description:   "desc",
		},
	}
	node := &ConnectionNode{ID: NewID()}

	res := node.Inheritance.Resolve(node, []*FolderNode{parent})

	if res.ProtocolID != "ssh" {
		t.Errorf("ProtocolID not copied")
	}
	if res.Host != "host" {
		t.Errorf("Host not copied")
	}
	if res.Port != 22 {
		t.Errorf("Port not copied")
	}
	if res.Username != "user" {
		t.Errorf("Username not copied")
	}
	if res.AuthMethod != protocol.AuthPassword {
		t.Errorf("AuthMethod not copied")
	}
	if res.CredentialRef.ProviderID != "p" {
		t.Errorf("CredentialRef not copied")
	}
	if len(res.Settings) != 1 {
		t.Errorf("Settings not copied")
	}
	if len(res.Tags) != 1 {
		t.Errorf("Tags not copied")
	}
	if res.Color != "color" {
		t.Errorf("Color not copied")
	}
	if res.Icon != "icon" {
		t.Errorf("Icon not copied")
	}
	if res.Environment != "env" {
		t.Errorf("Environment not copied")
	}
	if res.Description != "desc" {
		t.Errorf("Description not copied")
	}
}

func TestInheritanceResolve_MiscPaths(t *testing.T) {
	// Cover ReverseFolders
	folders := []*FolderNode{
		{ID: NewID(), Name: "1"},
		{ID: NewID(), Name: "2"},
	}
	reversed := ReverseFolders(folders)
	if len(reversed) != 2 || reversed[0].Name != "2" || reversed[1].Name != "1" {
		t.Errorf("ReverseFolders failed")
	}

	// Cover cloneRef empty string handling
	ref := credential.Reference{ProviderID: "p", Hints: map[string]string{"h": "val"}}
	cRef := cloneRef(ref)
	if cRef.Hints["h"] != "val" {
		t.Errorf("cloneRef hints not copied")
	}

	// Cover copyNodeField remaining branch
	node := &ConnectionNode{ID: NewID()}
	node.Inheritance.SetExplicit(FieldTags)
	node.Inheritance.SetExplicit(FieldSettings)
	node.Inheritance.SetExplicit(FieldCredentialRef)

	res := node.Inheritance.Resolve(node, nil)
	if res.Tags != nil || res.Settings != nil {
		t.Errorf("expected nils for explicit zero node fields")
	}
}
