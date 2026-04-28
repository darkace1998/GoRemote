// Package domain contains the pure domain model of goremote: connections,
// folders, inheritance, protocol descriptors, sessions, workspace layout, and
// the in-memory tree that organises them.
//
// The package is intentionally free of I/O, network, UI, and persistence
// concerns. It depends only on the standard library and the public sdk/*
// packages.
package domain

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/goremote/goremote/sdk/credential"
	"github.com/goremote/goremote/sdk/protocol"
)

// ID is a randomly generated 128-bit identifier stored as UUID v4 format.
type ID [16]byte

// NewID generates a new random UUID v4.
func NewID() ID {
	var id ID
	if _, err := rand.Read(id[:]); err != nil {
		panic(fmt.Sprintf("domain.NewID: crypto/rand: %v", err))
	}
	id[6] = (id[6] & 0x0f) | 0x40 // version 4
	id[8] = (id[8] & 0x3f) | 0x80 // variant 10
	return id
}

// NilID is the zero value, used to indicate "no parent" on root nodes.
var NilID ID

// ParseID parses a standard hyphenated UUID string (8-4-4-4-12 hex).
func ParseID(s string) (ID, error) {
	clean := strings.ReplaceAll(s, "-", "")
	if len(clean) != 32 {
		return NilID, fmt.Errorf("domain: invalid id %q", s)
	}
	b, err := hex.DecodeString(clean)
	if err != nil {
		return NilID, fmt.Errorf("domain: invalid id %q: %w", s, err)
	}
	var id ID
	copy(id[:], b)
	return id, nil
}

// String returns the ID as a lowercase hyphenated UUID string.
func (id ID) String() string {
	b := id[:]
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// MarshalJSON encodes the ID as a JSON string.
func (id ID) MarshalJSON() ([]byte, error) {
	return json.Marshal(id.String())
}

// UnmarshalJSON decodes a JSON string into an ID.
func (id *ID) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	parsed, err := ParseID(s)
	if err != nil {
		return err
	}
	*id = parsed
	return nil
}

// NewIDString returns a new random ID as a lowercase hyphenated UUID string.
func NewIDString() string { return NewID().String() }

// ConnectionNode describes a single remote target: a host reachable via a
// specific protocol, together with presentation metadata and inheritance
// overrides.
//
// ConnectionNode values carry *references* to credentials, never raw secrets.
type ConnectionNode struct {
	// ID uniquely identifies the node.
	ID ID
	// ParentID is the ID of the containing FolderNode, or NilID if at root.
	ParentID ID
	// Name is the human label shown in the tree.
	Name string
	// ProtocolID matches a ProtocolDescriptor.ID (e.g. "io.goremote.protocol.ssh").
	ProtocolID string
	// Host is the resolved hostname or address.
	Host string
	// Port is the target port. 0 means "use protocol default".
	Port int
	// Username used by protocols that need one.
	Username string
	// AuthMethod selects the auth strategy.
	AuthMethod protocol.AuthMethod
	// CredentialRef references a credential stored in a credential provider.
	CredentialRef credential.Reference
	// Settings is a bag of per-protocol settings keyed by SettingDef.Key.
	Settings map[string]any
	// Tags is an unordered set of user-assigned labels.
	Tags []string
	// Color is an optional presentation hint ("#rrggbb" or empty).
	Color string
	// Icon is an optional icon identifier.
	Icon string
	// Environment is an informational label ("prod", "staging", ...).
	Environment string
	// Favorite is true when the user has starred this connection so it
	// appears in the Favorites virtual folder. It is purely a
	// presentation hint; protocol behaviour is unaffected.
	Favorite bool
	// Description is free-form user notes.
	Description string
	// Inheritance declares per-field override/inherit policy for this node.
	Inheritance InheritanceProfile
	// CreatedAt and UpdatedAt are set by the persistence layer.
	CreatedAt time.Time
	UpdatedAt time.Time
}

// FolderNode groups ConnectionNodes (and other folders) in the tree and
// provides default values that descendants can inherit.
type FolderNode struct {
	// ID uniquely identifies the folder.
	ID ID
	// ParentID is the containing folder's ID, or NilID for roots.
	ParentID ID
	// Name is the human label.
	Name string
	// Description is free-form user notes.
	Description string
	// Defaults provides values descendants may inherit. Keys correspond to
	// ConnectionNode fields; see Field* constants in inheritance.go.
	Defaults FolderDefaults
	// Tags applied to the folder itself.
	Tags []string
	// Color for the folder row.
	Color string
	// Icon for the folder row.
	Icon string
	// CreatedAt and UpdatedAt are set by the persistence layer.
	CreatedAt time.Time
	UpdatedAt time.Time
}

// FolderDefaults is the bundle of values a folder advertises to descendants
// when an inheritance rule selects it as the source for a field.
type FolderDefaults struct {
	ProtocolID    string
	Host          string
	Port          int
	Username      string
	AuthMethod    protocol.AuthMethod
	CredentialRef credential.Reference
	Settings      map[string]any
	Tags          []string
	Color         string
	Icon          string
	Environment   string
	Description   string
}

// ConnectionTemplate is a named bundle of default values applied when a new
// ConnectionNode is created. Templates are flat (no inheritance) by design:
// they initialize a node's fields, after which inheritance takes over.
type ConnectionTemplate struct {
	// ID uniquely identifies the template.
	ID ID
	// Name is the human label shown in the "New connection from template" UI.
	Name string
	// Description is free-form user notes.
	Description string
	// ProtocolID of the template.
	ProtocolID string
	// Port is applied to new nodes (0 means use protocol default).
	Port int
	// Username seeded on new nodes.
	Username string
	// AuthMethod seeded on new nodes.
	AuthMethod protocol.AuthMethod
	// Settings seeded on new nodes; copied by value.
	Settings map[string]any
	// Tags seeded on new nodes.
	Tags []string
	// Color seeded on new nodes.
	Color string
	// Icon seeded on new nodes.
	Icon string
	// Environment seeded on new nodes.
	Environment string
}

// Apply copies the template's defaults onto the given connection. Fields
// already set on node are preserved; template provides defaults only for
// zero-valued fields.
func (t ConnectionTemplate) Apply(node *ConnectionNode) {
	if node == nil {
		return
	}
	if node.ProtocolID == "" {
		node.ProtocolID = t.ProtocolID
	}
	if node.Port == 0 {
		node.Port = t.Port
	}
	if node.Username == "" {
		node.Username = t.Username
	}
	if node.AuthMethod == "" {
		node.AuthMethod = t.AuthMethod
	}
	if node.Settings == nil && len(t.Settings) > 0 {
		node.Settings = make(map[string]any, len(t.Settings))
		for k, v := range t.Settings {
			node.Settings[k] = v
		}
	}
	if len(node.Tags) == 0 && len(t.Tags) > 0 {
		node.Tags = append(node.Tags, t.Tags...)
	}
	if node.Color == "" {
		node.Color = t.Color
	}
	if node.Icon == "" {
		node.Icon = t.Icon
	}
	if node.Environment == "" {
		node.Environment = t.Environment
	}
}

// ProtocolDescriptor is the UI-facing description of a protocol, derived from
// a protocol.Module manifest + capabilities + settings schema.
type ProtocolDescriptor struct {
	// ID matches protocol.Module.Manifest().ID.
	ID string
	// Name is the human label.
	Name string
	// RenderMode is the primary render mode the UI should prepare for.
	RenderMode protocol.RenderMode
	// Capabilities describes runtime capabilities.
	Capabilities protocol.Capabilities
	// DefaultPort is used when a ConnectionNode.Port is 0.
	DefaultPort int
}

// SessionStatus enumerates lifecycle states of a live session.
type SessionStatus string

// Session lifecycle values.
const (
	SessionStatusConnecting   SessionStatus = "connecting"
	SessionStatusConnected    SessionStatus = "connected"
	SessionStatusDisconnected SessionStatus = "disconnected"
	SessionStatusError        SessionStatus = "error"
)

// SessionDescriptor is the runtime descriptor for an open session. It is a
// value type; the live Session object lives in the session host.
type SessionDescriptor struct {
	// ID identifies the session instance.
	ID ID
	// ConnectionID is the ConnectionNode this session was opened for.
	ConnectionID ID
	// ProtocolID of the underlying protocol module.
	ProtocolID string
	// OpenedAt is the wall-clock time the session entered Connecting.
	OpenedAt time.Time
	// Status is the current lifecycle status.
	Status SessionStatus
	// ErrorMessage is set when Status == SessionStatusError.
	ErrorMessage string
}

// WindowBounds describes a window's screen position and size, in device-
// independent pixels.
type WindowBounds struct {
	X      int
	Y      int
	Width  int
	Height int
}

// OpenTab describes one tab in the WorkspaceLayout's tab bar.
type OpenTab struct {
	// ConnectionID references a ConnectionNode.
	ConnectionID ID
	// Label is the tab label, which may differ from the connection's name.
	Label string
	// Color is an optional presentation hint.
	Color string
}

// WorkspaceLayout is the persisted workspace state: window geometry, dock
// layout, open tabs, and focus.
type WorkspaceLayout struct {
	// Bounds is the main window's position and size.
	Bounds WindowBounds
	// DockLayoutJSON is an opaque blob produced by the UI dock manager.
	DockLayoutJSON string
	// OpenTabs is the ordered list of tabs in the tab bar.
	OpenTabs []OpenTab
	// FocusedTab points at an entry in OpenTabs; NilID means no focus.
	FocusedTab ID
}
