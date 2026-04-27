package domain

import (
	"errors"
	"fmt"
	"sort"
)

// Errors returned by Tree operations.
var (
	// ErrNotFound is returned when a lookup or mutation targets an ID the
	// tree does not contain.
	ErrNotFound = errors.New("domain: node not found")
	// ErrCycle is returned by Move when the requested move would introduce
	// a cycle (moving a folder into itself or one of its descendants).
	ErrCycle = errors.New("domain: move would create a cycle")
	// ErrParentNotFolder is returned when a requested parent ID refers to
	// something that is not a folder.
	ErrParentNotFolder = errors.New("domain: parent is not a folder")
	// ErrDuplicateID is returned when an Add is attempted with an ID that
	// already exists in the tree.
	ErrDuplicateID = errors.New("domain: duplicate node id")
)

// Tree is the in-memory organisation of folders and connections.
//
// Tree is a value type with no concurrency primitives of its own; callers
// that share a tree across goroutines must guard it externally. Methods
// mutate the receiver in place.
type Tree struct {
	folders     map[ID]*FolderNode
	connections map[ID]*ConnectionNode
	// children indexes, parentID -> ordered child IDs.
	// NilID is a valid key and represents the tree root.
	folderChildren     map[ID][]ID
	connectionChildren map[ID][]ID
}

// NewTree returns an empty Tree ready for use.
func NewTree() *Tree {
	return &Tree{
		folders:            make(map[ID]*FolderNode),
		connections:        make(map[ID]*ConnectionNode),
		folderChildren:     make(map[ID][]ID),
		connectionChildren: make(map[ID][]ID),
	}
}

// AddFolder inserts f into the tree. f.ID must be set and unique. If
// f.ParentID is not NilID it must reference an existing folder.
func (t *Tree) AddFolder(f *FolderNode) error {
	if f == nil {
		return errors.New("domain: nil folder")
	}
	if f.ID == NilID {
		return errors.New("domain: folder id is required")
	}
	if _, ok := t.folders[f.ID]; ok {
		return fmt.Errorf("%w: folder %s", ErrDuplicateID, f.ID)
	}
	if _, ok := t.connections[f.ID]; ok {
		return fmt.Errorf("%w: id %s used by a connection", ErrDuplicateID, f.ID)
	}
	if f.ParentID != NilID {
		if _, ok := t.folders[f.ParentID]; !ok {
			return fmt.Errorf("%w: folder parent %s", ErrNotFound, f.ParentID)
		}
	}
	t.folders[f.ID] = f
	t.folderChildren[f.ParentID] = append(t.folderChildren[f.ParentID], f.ID)
	return nil
}

// AddConnection inserts c into the tree. c.ID must be set and unique. If
// c.ParentID is not NilID it must reference an existing folder.
func (t *Tree) AddConnection(c *ConnectionNode) error {
	if c == nil {
		return errors.New("domain: nil connection")
	}
	if c.ID == NilID {
		return errors.New("domain: connection id is required")
	}
	if _, ok := t.connections[c.ID]; ok {
		return fmt.Errorf("%w: connection %s", ErrDuplicateID, c.ID)
	}
	if _, ok := t.folders[c.ID]; ok {
		return fmt.Errorf("%w: id %s used by a folder", ErrDuplicateID, c.ID)
	}
	if c.ParentID != NilID {
		if _, ok := t.folders[c.ParentID]; !ok {
			return fmt.Errorf("%w: connection parent %s", ErrParentNotFolder, c.ParentID)
		}
	}
	t.connections[c.ID] = c
	t.connectionChildren[c.ParentID] = append(t.connectionChildren[c.ParentID], c.ID)
	return nil
}

// Move reparents the node identified by id under newParent (use NilID for
// the root). Connections may only live under folders. Move rejects cycles:
// a folder cannot become a descendant of itself.
func (t *Tree) Move(id, newParent ID) error {
	if id == NilID {
		return fmt.Errorf("%w: cannot move root", ErrNotFound)
	}
	if newParent != NilID {
		if _, ok := t.folders[newParent]; !ok {
			return fmt.Errorf("%w: parent %s", ErrParentNotFolder, newParent)
		}
	}

	if f, ok := t.folders[id]; ok {
		if newParent == id {
			return ErrCycle
		}
		if t.isDescendantFolder(newParent, id) {
			return ErrCycle
		}
		t.folderChildren[f.ParentID] = removeID(t.folderChildren[f.ParentID], id)
		f.ParentID = newParent
		t.folderChildren[newParent] = append(t.folderChildren[newParent], id)
		return nil
	}

	if c, ok := t.connections[id]; ok {
		t.connectionChildren[c.ParentID] = removeID(t.connectionChildren[c.ParentID], id)
		c.ParentID = newParent
		t.connectionChildren[newParent] = append(t.connectionChildren[newParent], id)
		return nil
	}

	return fmt.Errorf("%w: %s", ErrNotFound, id)
}

// Remove deletes the node identified by id. Removing a folder recursively
// removes everything under it.
func (t *Tree) Remove(id ID) error {
	if id == NilID {
		return fmt.Errorf("%w: cannot remove root", ErrNotFound)
	}
	if f, ok := t.folders[id]; ok {
		// Remove descendant connections and folders depth-first.
		for _, cid := range append([]ID(nil), t.connectionChildren[id]...) {
			delete(t.connections, cid)
		}
		delete(t.connectionChildren, id)
		for _, sub := range append([]ID(nil), t.folderChildren[id]...) {
			if err := t.Remove(sub); err != nil {
				return err
			}
		}
		delete(t.folderChildren, id)
		t.folderChildren[f.ParentID] = removeID(t.folderChildren[f.ParentID], id)
		delete(t.folders, id)
		return nil
	}
	if c, ok := t.connections[id]; ok {
		t.connectionChildren[c.ParentID] = removeID(t.connectionChildren[c.ParentID], id)
		delete(t.connections, id)
		return nil
	}
	return fmt.Errorf("%w: %s", ErrNotFound, id)
}

// Node is the common interface returned by FindByID: folders and connections
// both satisfy it.
type Node interface {
	NodeID() ID
	NodeParent() ID
	NodeKind() NodeKind
}

// NodeKind enumerates the node categories.
type NodeKind string

// Node kinds.
const (
	NodeKindFolder     NodeKind = "folder"
	NodeKindConnection NodeKind = "connection"
)

// NodeID returns the folder's ID.
func (f *FolderNode) NodeID() ID { return f.ID }

// NodeParent returns the folder's parent ID.
func (f *FolderNode) NodeParent() ID { return f.ParentID }

// NodeKind reports that the node is a folder.
func (f *FolderNode) NodeKind() NodeKind { return NodeKindFolder }

// NodeID returns the connection's ID.
func (c *ConnectionNode) NodeID() ID { return c.ID }

// NodeParent returns the connection's parent ID.
func (c *ConnectionNode) NodeParent() ID { return c.ParentID }

// NodeKind reports that the node is a connection.
func (c *ConnectionNode) NodeKind() NodeKind { return NodeKindConnection }

// FindByID returns the folder or connection with the given ID, or
// ErrNotFound.
func (t *Tree) FindByID(id ID) (Node, error) {
	if f, ok := t.folders[id]; ok {
		return f, nil
	}
	if c, ok := t.connections[id]; ok {
		return c, nil
	}
	return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
}

// Folder returns the folder with the given ID, or ErrNotFound.
func (t *Tree) Folder(id ID) (*FolderNode, error) {
	if f, ok := t.folders[id]; ok {
		return f, nil
	}
	return nil, fmt.Errorf("%w: folder %s", ErrNotFound, id)
}

// Connection returns the connection with the given ID, or ErrNotFound.
func (t *Tree) Connection(id ID) (*ConnectionNode, error) {
	if c, ok := t.connections[id]; ok {
		return c, nil
	}
	return nil, fmt.Errorf("%w: connection %s", ErrNotFound, id)
}

// Ancestors returns the folders above the node identified by id, ordered
// root-first (top-down). The slice is suitable to pass to
// InheritanceProfile.Resolve.
func (t *Tree) Ancestors(id ID) ([]*FolderNode, error) {
	n, err := t.FindByID(id)
	if err != nil {
		return nil, err
	}
	var rev []*FolderNode
	parent := n.NodeParent()
	for parent != NilID {
		f, ok := t.folders[parent]
		if !ok {
			return nil, fmt.Errorf("%w: ancestor %s", ErrNotFound, parent)
		}
		rev = append(rev, f)
		parent = f.ParentID
	}
	// rev is parent-first; reverse to root-first.
	return ReverseFolders(rev), nil
}

// Search returns every node for which pred returns true, in deterministic
// walk order (folders-before-connections, children sorted by name).
func (t *Tree) Search(pred Predicate) []Node {
	var out []Node
	_ = t.Walk(func(n Node) error {
		if pred == nil || pred.Match(n) {
			out = append(out, n)
		}
		return nil
	})
	return out
}

// Visitor receives every node during a Walk. Returning a non-nil error stops
// the walk and propagates the error out of Walk.
type Visitor func(Node) error

// Walk performs a deterministic pre-order depth-first traversal starting at
// the roots. At each level, folders are visited in name-sorted order,
// followed by connections in name-sorted order. Ties on name break by ID.
func (t *Tree) Walk(v Visitor) error {
	return t.walkFrom(NilID, v)
}

func (t *Tree) walkFrom(parent ID, v Visitor) error {
	folderIDs := append([]ID(nil), t.folderChildren[parent]...)
	sort.Slice(folderIDs, func(i, j int) bool {
		a, b := t.folders[folderIDs[i]], t.folders[folderIDs[j]]
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.ID.String() < b.ID.String()
	})
	for _, fid := range folderIDs {
		if err := v(t.folders[fid]); err != nil {
			return err
		}
		if err := t.walkFrom(fid, v); err != nil {
			return err
		}
	}
	connIDs := append([]ID(nil), t.connectionChildren[parent]...)
	sort.Slice(connIDs, func(i, j int) bool {
		a, b := t.connections[connIDs[i]], t.connections[connIDs[j]]
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.ID.String() < b.ID.String()
	})
	for _, cid := range connIDs {
		if err := v(t.connections[cid]); err != nil {
			return err
		}
	}
	return nil
}

// isDescendantFolder reports whether maybeChild is equal to or transitively
// under ancestor. Returns false if either ID is NilID.
func (t *Tree) isDescendantFolder(maybeChild, ancestor ID) bool {
	if maybeChild == NilID || ancestor == NilID {
		return false
	}
	cur := maybeChild
	for cur != NilID {
		if cur == ancestor {
			return true
		}
		f, ok := t.folders[cur]
		if !ok {
			return false
		}
		cur = f.ParentID
	}
	return false
}

func removeID(slice []ID, id ID) []ID {
	for i, x := range slice {
		if x == id {
			return append(slice[:i], slice[i+1:]...)
		}
	}
	return slice
}
