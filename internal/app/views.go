package app

import (
	"github.com/darkace1998/GoRemote/internal/domain"
)

// NodeView is a JSON-friendly projection of a folder or connection node. UI
// callers receive NodeView, never *domain.FolderNode/*ConnectionNode.
type NodeView struct {
	ID          string      `json:"id"`
	Kind        string      `json:"kind"`
	Name        string      `json:"name"`
	ParentID    string      `json:"parent_id,omitempty"`
	Description string      `json:"description,omitempty"`
	Tags        []string    `json:"tags,omitempty"`
	Icon        string      `json:"icon,omitempty"`
	Color       string      `json:"color,omitempty"`
	Environment string      `json:"environment,omitempty"`
	Favorite    bool        `json:"favorite,omitempty"`
	Protocol    string      `json:"protocol,omitempty"`
	Host        string      `json:"host,omitempty"`
	Port        int         `json:"port,omitempty"`
	Username    string      `json:"username,omitempty"`
	Children    []*NodeView `json:"children,omitempty"`
}

// TreeView is the full tree shape consumed by the UI. Root is a virtual node
// whose Children are the root-level folders and connections.
type TreeView struct {
	Root *NodeView `json:"root"`
}

// CredentialRefView mirrors sdk/credential.Reference for JSON-friendly output.
type CredentialRefView struct {
	ProviderID string            `json:"provider_id,omitempty"`
	EntryID    string            `json:"entry_id,omitempty"`
	Hints      map[string]string `json:"hints,omitempty"`
}

// ConnectionView is the hint-rich view of a ConnectionNode including the
// effective values after inheritance has been applied.
type ConnectionView struct {
	ID            string            `json:"id"`
	ParentID      string            `json:"parent_id,omitempty"`
	Name          string            `json:"name"`
	Description   string            `json:"description,omitempty"`
	Tags          []string          `json:"tags,omitempty"`
	Icon          string            `json:"icon,omitempty"`
	Color         string            `json:"color,omitempty"`
	Environment   string            `json:"environment,omitempty"`
	Favorite      bool              `json:"favorite,omitempty"`
	Protocol      string            `json:"protocol,omitempty"`
	Host          string            `json:"host,omitempty"`
	Port          int               `json:"port,omitempty"`
	Username      string            `json:"username,omitempty"`
	AuthMethod    string            `json:"auth_method,omitempty"`
	CredentialRef CredentialRefView `json:"credential_ref,omitempty"`
	Settings      map[string]any    `json:"settings,omitempty"`

	// Effective* fields reflect post-inheritance values. Useful for the UI
	// to show what an "Open" would actually do without duplicating logic.
	EffectiveProtocol string `json:"effective_protocol,omitempty"`
	EffectiveHost     string `json:"effective_host,omitempty"`
	EffectivePort     int    `json:"effective_port,omitempty"`
	EffectiveUsername string `json:"effective_username,omitempty"`
}

// SessionInfo is the lightweight descriptor returned by ListSessions.
type SessionInfo struct {
	ID           string `json:"id"`
	ConnectionID string `json:"connection_id"`
	Protocol     string `json:"protocol"`
	Host         string `json:"host,omitempty"`
	OpenedAt     string `json:"opened_at"`
}

// ImportWarning is the JSON-friendly projection of a single importer warning.
type ImportWarning struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Path     string `json:"path,omitempty"`
	Field    string `json:"field,omitempty"`
}

// ImportResult is the outcome of ImportMRemoteNG.
type ImportResult struct {
	Imported int             `json:"imported"`
	Folders  int             `json:"folders"`
	Warnings []ImportWarning `json:"warnings,omitempty"`
}

// BackupInfo is the outcome of ExportSnapshot.
type BackupInfo struct {
	Path string `json:"path"`
}

// buildNodeView renders a connection node to a NodeView (without children).
func connectionNodeView(c *domain.ConnectionNode) *NodeView {
	parent := ""
	if c.ParentID != domain.NilID {
		parent = c.ParentID.String()
	}
	return &NodeView{
		ID:          c.ID.String(),
		Kind:        string(domain.NodeKindConnection),
		Name:        c.Name,
		ParentID:    parent,
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
	}
}

// buildFolderView renders a folder node to a NodeView (without children).
func folderNodeView(f *domain.FolderNode) *NodeView {
	parent := ""
	if f.ParentID != domain.NilID {
		parent = f.ParentID.String()
	}
	return &NodeView{
		ID:          f.ID.String(),
		Kind:        string(domain.NodeKindFolder),
		Name:        f.Name,
		ParentID:    parent,
		Description: f.Description,
		Tags:        append([]string(nil), f.Tags...),
		Icon:        f.Icon,
		Color:       f.Color,
	}
}
