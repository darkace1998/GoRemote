// Package protocol defines the contract every protocol plugin must satisfy,
// regardless of whether it is built-in or out-of-process.
//
// A protocol plugin describes:
//   - its manifest and settings schema (declarative metadata)
//   - which authentication methods it supports
//   - which session rendering modes it can drive (terminal, graphical, external)
//   - how to open, resize, send input to, and tear down a session
//
// The sdk/protocol package intentionally has no dependency on any concrete
// transport (terminal renderer, framebuffer, IPC). Hosts wire concrete
// terminal/framebuffer sinks at runtime via the Session interface.
package protocol

import (
	"context"
	"errors"
	"io"

	"github.com/goremote/goremote/sdk/plugin"
)

// RenderMode tells the host how the session should be presented.
type RenderMode string

const (
	RenderTerminal  RenderMode = "terminal"
	RenderGraphical RenderMode = "graphical"
	RenderExternal  RenderMode = "external"
)

// AuthMethod enumerates auth strategies a protocol may support.
type AuthMethod string

const (
	AuthNone        AuthMethod = "none"
	AuthPassword    AuthMethod = "password"
	AuthPublicKey   AuthMethod = "publickey"
	AuthAgent       AuthMethod = "agent"
	AuthGSSAPI      AuthMethod = "gssapi"
	AuthInteractive AuthMethod = "keyboard-interactive"
	AuthClientCert  AuthMethod = "client-cert"
)

// SettingType is a coarse type for protocol-specific settings.
type SettingType string

const (
	SettingString SettingType = "string"
	SettingInt    SettingType = "int"
	SettingBool   SettingType = "bool"
	SettingEnum   SettingType = "enum"
	SettingSecret SettingType = "secret"
)

// SettingDef declares a single configurable setting offered by a protocol.
// Hosts use this to build the property editor UI and to validate values
// before opening sessions.
type SettingDef struct {
	Key         string      `json:"key"`
	Label       string      `json:"label"`
	Type        SettingType `json:"type"`
	Default     any         `json:"default,omitempty"`
	Required    bool        `json:"required,omitempty"`
	Description string      `json:"description,omitempty"`
	// Enum values when Type == SettingEnum.
	EnumValues []string `json:"enum_values,omitempty"`
	// Min / Max for numeric settings.
	Min *int `json:"min,omitempty"`
	Max *int `json:"max,omitempty"`
}

// Capabilities describes the per-plugin protocol-level capabilities.
type Capabilities struct {
	RenderModes       []RenderMode `json:"render_modes"`
	AuthMethods       []AuthMethod `json:"auth_methods"`
	SupportsResize    bool         `json:"supports_resize"`
	SupportsClipboard bool         `json:"supports_clipboard"`
	SupportsLogging   bool         `json:"supports_logging"`
	SupportsReconnect bool         `json:"supports_reconnect"`
}

// Module is the entry point a protocol plugin exposes.
//
// Each Module instance represents a *protocol type* (e.g. SSH); a Module is
// asked to produce a Session for each individual connection attempt.
type Module interface {
	// Manifest returns the plugin's static manifest. Must be cheap and
	// safe to call before Init.
	Manifest() plugin.Manifest

	// Settings returns the protocol-specific settings schema.
	Settings() []SettingDef

	// Capabilities describes runtime capabilities of this module.
	Capabilities() Capabilities

	// Open creates a new session against the given target. The session has
	// not started its I/O loop yet; the caller is expected to attach a
	// renderer/sink and then call Session.Start.
	Open(ctx context.Context, req OpenRequest) (Session, error)
}

// OpenRequest is the parameter bundle for Module.Open.
type OpenRequest struct {
	// Host is the resolved target host or address.
	Host string
	// Port is the target port. 0 = use protocol default.
	Port int
	// Username for auth methods that need it.
	Username string
	// AuthMethod chosen by the user/host.
	AuthMethod AuthMethod
	// Secret is the resolved credential material; never logged.
	Secret CredentialMaterial
	// Settings is the merged protocol-specific settings (post-inheritance).
	Settings map[string]any
	// PreferredRender is the rendering mode the UI wants. Modules MAY
	// downgrade (e.g. graphical -> external) and report it back via Session.RenderMode.
	PreferredRender RenderMode
	// InitialSize is the initial terminal size for terminal sessions; ignored otherwise.
	InitialSize Size
}

// CredentialMaterial is the minimum subset of credential.Material the protocol
// needs at session-open time. We keep this duplicated here to avoid the
// sdk/protocol package importing sdk/credential (and thus the credential host).
//
// Hosts adapt sdk/credential.Material -> CredentialMaterial when calling Open.
type CredentialMaterial struct {
	Username string
	Password string
	// PrivateKey is PEM-encoded.
	PrivateKey []byte
	// Passphrase optionally unlocks PrivateKey.
	Passphrase string
	// Domain is used by NTLM/Kerberos style auth.
	Domain string
	// Extra holds protocol-specific extras (e.g. OTP code).
	Extra map[string]string
}

// Size describes a terminal grid size in cells.
type Size struct {
	Cols int
	Rows int
}

// Session is one live protocol session. Implementations must be safe for the
// caller to invoke Start, Resize, SendInput, and Close concurrently.
type Session interface {
	// RenderMode reports the actual rendering mode after Open negotiation.
	RenderMode() RenderMode

	// Start runs the session's I/O loop, copying remote->stdout and
	// stdin->remote as appropriate. It blocks until the session exits or ctx
	// is cancelled. Implementations must close stdout and stop reading from
	// stdin before returning.
	//
	// For graphical / external sessions, stdout/stdin may be nil; the host
	// supplies framebuffer or launcher channels via separate adapters.
	Start(ctx context.Context, stdin io.Reader, stdout io.Writer) error

	// Resize requests a window-size change; protocols that don't support it
	// must return ErrNotSupported.
	Resize(ctx context.Context, size Size) error

	// SendInput is an alternative to writing to the Start stdin pipe; useful
	// for paste operations, special-key events, or out-of-band signals.
	SendInput(ctx context.Context, data []byte) error

	// Close terminates the session. Safe to call multiple times.
	Close() error
}

// ErrNotSupported is returned by optional methods (Resize, etc) on protocols
// that don't implement them.
var ErrNotSupported = errors.New("operation not supported by this protocol")

// ErrUnsupported is returned by Session methods (SendInput, Resize, ...) that
// the protocol cannot meaningfully implement (e.g. SendInput on an external-
// launcher session). Hosts should treat this as a soft "no-op" rather than a
// fatal error.
var ErrUnsupported = errors.New("operation unsupported by this protocol")

// CurrentAPIVersion of the protocol SDK contract.
const CurrentAPIVersion = "1.0.0"
