package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"

	"github.com/goremote/goremote/internal/domain"
	mremoteng "github.com/goremote/goremote/internal/import/mremoteng"
	"github.com/goremote/goremote/sdk/credential"
	"github.com/goremote/goremote/sdk/protocol"
)

// FolderOpts is the input payload for CreateFolder.
type FolderOpts struct {
	Description string
	Tags        []string
	Icon        string
	Color       string
	Defaults    domain.FolderDefaults
}

// FolderPatch describes a partial update to a folder. nil pointer fields are
// left untouched.
type FolderPatch struct {
	Name        *string
	Description *string
	Tags        *[]string
	Icon        *string
	Color       *string
	Defaults    *domain.FolderDefaults
}

// ConnectionOpts is the input payload for CreateConnection.
type ConnectionOpts struct {
	Name          string
	ProtocolID    string
	Host          string
	Port          int
	Username      string
	AuthMethod    protocol.AuthMethod
	CredentialRef credential.Reference
	Description   string
	Tags          []string
	Settings      map[string]any
	Icon          string
	Color         string
	Environment   string
	Favorite      bool
	Inheritance   domain.InheritanceProfile
}

// ConnectionPatch describes a partial update to a connection. nil pointer
// fields are left untouched.
type ConnectionPatch struct {
	Name          *string
	ProtocolID    *string
	Host          *string
	Port          *int
	Username      *string
	AuthMethod    *protocol.AuthMethod
	CredentialRef *credential.Reference
	Description   *string
	Tags          *[]string
	Settings      *map[string]any
	Icon          *string
	Color         *string
	Environment   *string
	Favorite      *bool
	Inheritance   *domain.InheritanceProfile
}

// SearchQuery parameterises a tree search. Empty fields are ignored.
type SearchQuery struct {
	Name     string
	Tag      string
	Protocol string
}

// errAttr is a small helper for attaching an error to a slog record.
func errAttr(err error) slog.Attr {
	if err == nil {
		return slog.String("err", "")
	}
	return slog.String("err", err.Error())
}

// CreateFolder creates a folder under parent and returns its ID.
func (a *App) CreateFolder(ctx context.Context, parent domain.ID, name string, opts FolderOpts) (domain.ID, error) {
	if err := ctx.Err(); err != nil {
		return domain.NilID, err
	}
	if name == "" {
		return domain.NilID, errors.New("app: folder name is required")
	}
	f := &domain.FolderNode{
		ID:          domain.NewID(),
		ParentID:    parent,
		Name:        name,
		Description: opts.Description,
		Tags:        append([]string(nil), opts.Tags...),
		Icon:        opts.Icon,
		Color:       opts.Color,
		Defaults:    opts.Defaults,
		CreatedAt:   a.now(),
		UpdatedAt:   a.now(),
	}
	a.treeMu.Lock()
	if err := a.tree.AddFolder(f); err != nil {
		a.treeMu.Unlock()
		return domain.NilID, err
	}
	a.treeMu.Unlock()
	a.markDirty()
	a.publish(Event{
		Kind: EventNodeCreated, NodeID: f.ID, ParentID: f.ParentID,
		NodeKind: string(domain.NodeKindFolder), Name: f.Name,
	})
	return f.ID, nil
}

// CreateConnection creates a connection under parent.
func (a *App) CreateConnection(ctx context.Context, parent domain.ID, opts ConnectionOpts) (domain.ID, error) {
	if err := ctx.Err(); err != nil {
		return domain.NilID, err
	}
	if opts.Name == "" {
		return domain.NilID, errors.New("app: connection name is required")
	}
	c := &domain.ConnectionNode{
		ID:            domain.NewID(),
		ParentID:      parent,
		Name:          opts.Name,
		ProtocolID:    opts.ProtocolID,
		Host:          opts.Host,
		Port:          opts.Port,
		Username:      opts.Username,
		AuthMethod:    opts.AuthMethod,
		CredentialRef: opts.CredentialRef,
		Description:   opts.Description,
		Tags:          append([]string(nil), opts.Tags...),
		Settings:      cloneSettings(opts.Settings),
		Icon:          opts.Icon,
		Color:         opts.Color,
		Environment:   opts.Environment,
		Favorite:      opts.Favorite,
		Inheritance:   opts.Inheritance,
		CreatedAt:     a.now(),
		UpdatedAt:     a.now(),
	}
	a.treeMu.Lock()
	if err := a.tree.AddConnection(c); err != nil {
		a.treeMu.Unlock()
		return domain.NilID, err
	}
	a.treeMu.Unlock()
	a.markDirty()
	a.publish(Event{
		Kind: EventNodeCreated, NodeID: c.ID, ParentID: c.ParentID,
		NodeKind: string(domain.NodeKindConnection), Name: c.Name,
	})
	return c.ID, nil
}

// UpdateConnection applies a patch to the connection with the given id.
func (a *App) UpdateConnection(ctx context.Context, id domain.ID, patch ConnectionPatch) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a.treeMu.Lock()
	c, err := a.tree.Connection(id)
	if err != nil {
		a.treeMu.Unlock()
		return err
	}
	if patch.Name != nil {
		c.Name = *patch.Name
	}
	if patch.ProtocolID != nil {
		c.ProtocolID = *patch.ProtocolID
	}
	if patch.Host != nil {
		c.Host = *patch.Host
	}
	if patch.Port != nil {
		c.Port = *patch.Port
	}
	if patch.Username != nil {
		c.Username = *patch.Username
	}
	if patch.AuthMethod != nil {
		c.AuthMethod = *patch.AuthMethod
	}
	if patch.CredentialRef != nil {
		c.CredentialRef = *patch.CredentialRef
	}
	if patch.Description != nil {
		c.Description = *patch.Description
	}
	if patch.Tags != nil {
		c.Tags = append([]string(nil), (*patch.Tags)...)
	}
	if patch.Settings != nil {
		c.Settings = cloneSettings(*patch.Settings)
	}
	if patch.Icon != nil {
		c.Icon = *patch.Icon
	}
	if patch.Color != nil {
		c.Color = *patch.Color
	}
	if patch.Environment != nil {
		c.Environment = *patch.Environment
	}
	if patch.Favorite != nil {
		c.Favorite = *patch.Favorite
	}
	if patch.Inheritance != nil {
		c.Inheritance = *patch.Inheritance
	}
	c.UpdatedAt = a.now()
	name, parent := c.Name, c.ParentID
	a.treeMu.Unlock()
	a.markDirty()
	a.publish(Event{
		Kind: EventNodeUpdated, NodeID: id, ParentID: parent,
		NodeKind: string(domain.NodeKindConnection), Name: name,
	})
	return nil
}

// ToggleFavorite flips the Favorite flag on the connection with the
// given id. Returns the new state. The command is its own publish
// event so the UI can refresh the favorites virtual folder without
// re-walking the entire tree.
func (a *App) ToggleFavorite(ctx context.Context, id domain.ID) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	a.treeMu.Lock()
	c, err := a.tree.Connection(id)
	if err != nil {
		a.treeMu.Unlock()
		return false, err
	}
	c.Favorite = !c.Favorite
	c.UpdatedAt = a.now()
	state := c.Favorite
	name, parent := c.Name, c.ParentID
	a.treeMu.Unlock()
	a.markDirty()
	a.publish(Event{
		Kind: EventNodeUpdated, NodeID: id, ParentID: parent,
		NodeKind: string(domain.NodeKindConnection), Name: name,
	})
	return state, nil
}

// ListFavorites returns NodeView projections of every connection
// whose Favorite flag is set, sorted by name (case-insensitive).
func (a *App) ListFavorites(ctx context.Context) []*NodeView {
	if err := ctx.Err(); err != nil {
		return nil
	}
	a.treeMu.RLock()
	defer a.treeMu.RUnlock()
	out := []*NodeView{}
	_ = a.tree.Walk(func(n domain.Node) error {
		if c, ok := n.(*domain.ConnectionNode); ok && c.Favorite {
			out = append(out, connectionNodeView(c))
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// UpdateFolder applies a patch to the folder with the given id.
func (a *App) UpdateFolder(ctx context.Context, id domain.ID, patch FolderPatch) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a.treeMu.Lock()
	f, err := a.tree.Folder(id)
	if err != nil {
		a.treeMu.Unlock()
		return err
	}
	if patch.Name != nil {
		f.Name = *patch.Name
	}
	if patch.Description != nil {
		f.Description = *patch.Description
	}
	if patch.Tags != nil {
		f.Tags = append([]string(nil), (*patch.Tags)...)
	}
	if patch.Icon != nil {
		f.Icon = *patch.Icon
	}
	if patch.Color != nil {
		f.Color = *patch.Color
	}
	if patch.Defaults != nil {
		f.Defaults = *patch.Defaults
	}
	f.UpdatedAt = a.now()
	name, parent := f.Name, f.ParentID
	a.treeMu.Unlock()
	a.markDirty()
	a.publish(Event{
		Kind: EventNodeUpdated, NodeID: id, ParentID: parent,
		NodeKind: string(domain.NodeKindFolder), Name: name,
	})
	return nil
}

// MoveNode reparents the node identified by id under newParent.
func (a *App) MoveNode(ctx context.Context, id, newParent domain.ID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a.treeMu.Lock()
	n, err := a.tree.FindByID(id)
	if err != nil {
		a.treeMu.Unlock()
		return err
	}
	oldParent := n.NodeParent()
	kind := n.NodeKind()
	if err := a.tree.Move(id, newParent); err != nil {
		a.treeMu.Unlock()
		return err
	}
	a.treeMu.Unlock()
	a.markDirty()
	a.publish(Event{
		Kind: EventNodeMoved, NodeID: id, ParentID: newParent,
		OldParentID: oldParent, NodeKind: string(kind),
	})
	return nil
}

// DeleteNode removes a folder (recursively) or connection.
func (a *App) DeleteNode(ctx context.Context, id domain.ID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a.treeMu.Lock()
	n, err := a.tree.FindByID(id)
	if err != nil {
		a.treeMu.Unlock()
		return err
	}
	parent := n.NodeParent()
	kind := n.NodeKind()
	if err := a.tree.Remove(id); err != nil {
		a.treeMu.Unlock()
		return err
	}
	a.treeMu.Unlock()
	a.markDirty()
	a.publish(Event{
		Kind: EventNodeDeleted, NodeID: id, ParentID: parent,
		NodeKind: string(kind),
	})
	return nil
}

// ListTree returns a read-only projection of the entire tree.
func (a *App) ListTree(ctx context.Context) TreeView {
	a.treeMu.RLock()
	defer a.treeMu.RUnlock()

	root := &NodeView{Kind: "root"}
	byID := map[domain.ID]*NodeView{domain.NilID: root}
	_ = a.tree.Walk(func(n domain.Node) error {
		var nv *NodeView
		switch v := n.(type) {
		case *domain.FolderNode:
			nv = folderNodeView(v)
		case *domain.ConnectionNode:
			nv = connectionNodeView(v)
		default:
			return nil
		}
		parent := byID[n.NodeParent()]
		if parent == nil {
			parent = root
		}
		parent.Children = append(parent.Children, nv)
		byID[n.NodeID()] = nv
		return nil
	})
	return TreeView{Root: root}
}

// GetConnection returns a ConnectionView for the given connection id with
// inheritance resolved into the Effective* fields.
func (a *App) GetConnection(ctx context.Context, id domain.ID) (ConnectionView, error) {
	if err := ctx.Err(); err != nil {
		return ConnectionView{}, err
	}
	a.treeMu.RLock()
	defer a.treeMu.RUnlock()
	c, err := a.tree.Connection(id)
	if err != nil {
		return ConnectionView{}, err
	}
	ancestors, err := a.tree.Ancestors(id)
	if err != nil {
		return ConnectionView{}, err
	}
	resolved := c.Inheritance.Resolve(c, ancestors)

	v := ConnectionView{
		ID:          c.ID.String(),
		Name:        c.Name,
		Description: c.Description,
		Tags:        append([]string(nil), c.Tags...),
		Icon:        c.Icon,
		Color:       c.Color,
		Environment: c.Environment,
		Favorite:    c.Favorite,
		Protocol:    c.ProtocolID,
		Host:        c.Host,
		Port:        c.Port,
		Username:    c.Username,
		AuthMethod:  string(c.AuthMethod),
		Settings:    cloneSettings(c.Settings),

		EffectiveProtocol: resolved.ProtocolID,
		EffectiveHost:     resolved.Host,
		EffectivePort:     resolved.Port,
		EffectiveUsername: resolved.Username,
	}
	if c.ParentID != domain.NilID {
		v.ParentID = c.ParentID.String()
	}
	if c.CredentialRef.ProviderID != "" || c.CredentialRef.EntryID != "" || len(c.CredentialRef.Hints) > 0 {
		v.CredentialRef = CredentialRefView{
			ProviderID: c.CredentialRef.ProviderID,
			EntryID:    c.CredentialRef.EntryID,
		}
		if len(c.CredentialRef.Hints) > 0 {
			v.CredentialRef.Hints = make(map[string]string, len(c.CredentialRef.Hints))
			for k, val := range c.CredentialRef.Hints {
				v.CredentialRef.Hints[k] = val
			}
		}
	}
	return v, nil
}

// Search returns all nodes matching the query, as flat NodeViews (no
// Children populated).
func (a *App) Search(ctx context.Context, q SearchQuery) []NodeView {
	a.treeMu.RLock()
	defer a.treeMu.RUnlock()

	preds := make([]domain.Predicate, 0, 3)
	if q.Name != "" {
		preds = append(preds, domain.MatchName(q.Name))
	}
	if q.Tag != "" {
		preds = append(preds, domain.MatchTag(q.Tag))
	}
	if q.Protocol != "" {
		preds = append(preds, domain.MatchProtocol(q.Protocol))
	}
	pred := domain.MatchAll
	if len(preds) > 0 {
		pred = domain.And(preds...)
	}
	matches := a.tree.Search(pred)
	out := make([]NodeView, 0, len(matches))
	for _, n := range matches {
		switch v := n.(type) {
		case *domain.FolderNode:
			out = append(out, *folderNodeView(v))
		case *domain.ConnectionNode:
			out = append(out, *connectionNodeView(v))
		}
	}
	return out
}

// ImportMRemoteNG imports an mRemoteNG XML or CSV export. format must be
// "xml" or "csv". Imported folders and connections are appended under the
// tree root; existing content is preserved.
func (a *App) ImportMRemoteNG(ctx context.Context, format string, r io.Reader) (ImportResult, error) {
	if err := ctx.Err(); err != nil {
		return ImportResult{}, err
	}
	var (
		res *mremoteng.Result
		err error
	)
	switch format {
	case "xml":
		res, err = mremoteng.ImportXML(r)
	case "csv":
		res, err = mremoteng.ImportCSV(r)
	default:
		return ImportResult{}, fmt.Errorf("app: unsupported import format %q", format)
	}
	if err != nil {
		return ImportResult{}, err
	}

	// Merge the imported tree into ours. Walk in parent-first order; since
	// imported IDs are freshly generated and disjoint from ours, add is safe.
	out := ImportResult{
		Folders:  res.Stats.Folders,
		Imported: res.Stats.Connections,
	}
	a.treeMu.Lock()
	walkErr := res.Tree.Walk(func(n domain.Node) error {
		switch v := n.(type) {
		case *domain.FolderNode:
			return a.tree.AddFolder(v)
		case *domain.ConnectionNode:
			return a.tree.AddConnection(v)
		}
		return nil
	})
	a.treeMu.Unlock()
	if walkErr != nil {
		return out, fmt.Errorf("app: merge imported tree: %w", walkErr)
	}
	for _, w := range res.Warnings {
		out.Warnings = append(out.Warnings, ImportWarning{
			Severity: w.Severity, Code: w.Code, Message: w.Message,
			Path: w.Path, Field: w.Field,
		})
	}
	a.markDirty()
	return out, nil
}

// ExportSnapshot creates a backup zip of the on-disk state and returns the
// archive path. Pending in-memory mutations are flushed first.
func (a *App) ExportSnapshot(ctx context.Context) (BackupInfo, error) {
	if err := ctx.Err(); err != nil {
		return BackupInfo{}, err
	}
	if err := a.flushNow(ctx); err != nil {
		return BackupInfo{}, err
	}
	path, err := a.store.Backup(ctx)
	if err != nil {
		return BackupInfo{}, err
	}
	return BackupInfo{Path: path}, nil
}

// RestoreSnapshot extracts a backup archive over the current store, then
// reloads the in-memory state from disk. All active sessions must be closed
// before calling Restore.
func (a *App) RestoreSnapshot(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a.persistMu.Lock()
	defer a.persistMu.Unlock()

	// Flush first so the pre-restore safety backup (taken inside Store.Restore)
	// reflects the latest in-memory state. Hold persistMu through reload so the
	// background persister cannot overwrite the restored files with a stale
	// pre-restore snapshot.
	if err := a.flushNowLocked(ctx); err != nil {
		return err
	}
	if err := a.store.Restore(ctx, path); err != nil {
		return err
	}
	snap, err := a.store.Load(ctx)
	if err != nil {
		return err
	}
	if snap.Tree == nil {
		snap.Tree = domain.NewTree()
	}
	a.treeMu.Lock()
	a.tree = snap.Tree
	a.templates = snap.Templates
	a.workspace = snap.Workspace
	a.meta = snap.Meta
	a.treeMu.Unlock()
	a.dirty.Store(false)
	return nil
}

// cloneSettings returns a shallow copy of m, or nil when m is nil/empty.
func cloneSettings(m map[string]any) map[string]any {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
