package app

import (
	"time"

	"github.com/darkace1998/GoRemote/internal/domain"
	"github.com/darkace1998/GoRemote/internal/eventbus"
)

// EventKind enumerates the kinds of lifecycle events the app publishes on its
// event bus. High-volume session output is intentionally NOT routed through
// this bus; use SessionManager.SubscribeOutput for raw byte streams.
type EventKind string

// Event kinds.
const (
	EventNodeCreated        EventKind = "node_created"
	EventNodeUpdated        EventKind = "node_updated"
	EventNodeMoved          EventKind = "node_moved"
	EventNodeDeleted        EventKind = "node_deleted"
	EventSessionOpened      EventKind = "session_opened"
	EventSessionClosed      EventKind = "session_closed"
	EventCredentialUnlocked EventKind = "credential_unlocked"
	EventError              EventKind = "error"
)

// Event is the envelope published on the app event bus. Only one of the
// payload fields is populated per event kind:
//
//   - NodeCreated/NodeUpdated/NodeDeleted: NodeID, ParentID, NodeKind, Name.
//   - NodeMoved: NodeID, ParentID (new parent), OldParentID, NodeKind.
//   - SessionOpened/SessionClosed: SessionID, NodeID (connection id), Err.
//   - CredentialUnlocked: ProviderID.
//   - Error: Where, Err.
//
// Note: Per-session raw output bytes are high-volume and are delivered via
// SessionManager.SubscribeOutput, not through this bus. The bus is for
// lifecycle events only.
type Event struct {
	Kind        EventKind
	At          time.Time
	NodeID      domain.ID
	ParentID    domain.ID
	OldParentID domain.ID
	NodeKind    string
	Name        string
	SessionID   SessionHandle
	ProviderID  string
	Where       string
	Err         error
}

// Events returns the app-wide event bus.
func (a *App) Events() *eventbus.Bus[Event] { return a.events }

func (a *App) publish(e Event) {
	if e.At.IsZero() {
		e.At = a.now()
	}
	a.events.Publish(a.rootCtx, e)
}
