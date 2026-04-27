// Package credential defines the contract every credential provider plugin
// must satisfy. Providers can be built-in (e.g. local encrypted file) or
// external (1Password, Bitwarden, vaults).
//
// Design rule: connection definitions store *references*, never raw secrets.
// At session-open time the host calls Provider.Resolve(ref) which returns
// Material. Material lifetimes are short-lived; callers should zero them out
// after use where possible.
package credential

import (
	"context"
	"errors"
	"time"

	"github.com/goremote/goremote/sdk/plugin"
)

// Reference identifies a credential within a specific provider.
//
// The combination (ProviderID, EntryID) must be globally unique. ProviderID
// matches the credential plugin's manifest ID; EntryID is opaque to the host.
type Reference struct {
	ProviderID string `json:"provider_id"`
	EntryID    string `json:"entry_id"`
	// Hints help providers disambiguate when EntryID alone is insufficient.
	// Common keys: "username", "host", "url", "tag".
	Hints map[string]string `json:"hints,omitempty"`
}

// Material is a resolved credential bundle. Callers must avoid logging or
// persisting any field except Reference / ExpiresAt.
type Material struct {
	Reference  Reference         `json:"reference"`
	Username   string            `json:"-"`
	Password   string            `json:"-"`
	Domain     string            `json:"-"`
	PrivateKey []byte            `json:"-"`
	Passphrase string            `json:"-"`
	OTP        string            `json:"-"`
	Extra      map[string]string `json:"-"`
	// Fields exposes provider-native additional fields not otherwise
	// modelled (e.g. arbitrary 1Password item fields). Like Extra, the
	// values are sensitive and are not serialised.
	Fields    map[string]string `json:"-"`
	ExpiresAt time.Time         `json:"expires_at,omitempty"`
}

// Zeroize clears sensitive fields. Best-effort (Go strings are immutable;
// byte slices can be wiped). Call this when you're done using the material.
func (m *Material) Zeroize() {
	if m == nil {
		return
	}
	m.Username = ""
	m.Password = ""
	m.Domain = ""
	for i := range m.PrivateKey {
		m.PrivateKey[i] = 0
	}
	m.PrivateKey = nil
	m.Passphrase = ""
	m.OTP = ""
	for k := range m.Extra {
		m.Extra[k] = ""
	}
	m.Extra = nil
	for k := range m.Fields {
		m.Fields[k] = ""
	}
	m.Fields = nil
}

// Capabilities a credential provider may expose.
type Capabilities struct {
	// Lookup means the provider supports finding credentials by Hints
	// without an explicit EntryID.
	Lookup bool `json:"lookup"`
	// Refresh means resolved Material may have ExpiresAt and benefits from
	// being refreshed on demand.
	Refresh bool `json:"refresh"`
	// Write means the provider supports adding/updating entries.
	Write bool `json:"write"`
	// Unlock means the provider has a locked state and needs an unlock step.
	Unlock bool `json:"unlock"`
	// SupportedKinds lists which Material fields the provider can populate.
	SupportedKinds []Kind `json:"supported_kinds"`
}

// Kind enumerates the categories of secrets a provider can return.
type Kind string

const (
	KindPassword   Kind = "password"
	KindPrivateKey Kind = "private_key"
	KindOTP        Kind = "otp"
	KindAPIKey     Kind = "api_key"
)

// State represents the provider's current operational state.
type State string

const (
	StateLocked        State = "locked"
	StateUnlocked      State = "unlocked"
	StateError         State = "error"
	StateNotConfigured State = "not_configured"
	// StateUnavailable indicates the provider's backend (binary, daemon,
	// network endpoint) cannot be reached at all, distinct from a
	// reachable backend that simply requires unlocking.
	StateUnavailable State = "unavailable"
)

// Provider is the interface every credential provider plugin must implement.
type Provider interface {
	// Manifest returns the plugin manifest.
	Manifest() plugin.Manifest

	// Capabilities advertises what the provider supports.
	Capabilities() Capabilities

	// State reports the current state. Used by the UI to surface
	// "unlock required" prompts.
	State(ctx context.Context) State

	// Unlock transitions the provider from Locked to Unlocked.
	// passphrase may be empty for providers that use OS-level auth.
	Unlock(ctx context.Context, passphrase string) error

	// Lock clears any cached unlocked state.
	Lock(ctx context.Context) error

	// Resolve returns Material for the given reference. If the ref's
	// EntryID is empty and Capabilities.Lookup is true, providers may use
	// Hints to find a matching entry. Implementations MUST return
	// ErrLocked if not unlocked.
	Resolve(ctx context.Context, ref Reference) (*Material, error)

	// List returns references the provider can resolve. May return a
	// truncated list with Capabilities reporting that.
	List(ctx context.Context) ([]Reference, error)
}

// Writer is an optional interface for providers that allow programmatic writes.
type Writer interface {
	// Put creates or updates a credential. Returns the canonical Reference.
	Put(ctx context.Context, mat Material) (Reference, error)
	// Delete removes a credential.
	Delete(ctx context.Context, ref Reference) error
}

// Errors used by providers and hosts.
var (
	ErrLocked            = errors.New("credential provider is locked")
	ErrNotFound          = errors.New("credential not found")
	ErrNotSupported      = errors.New("operation not supported by provider")
	ErrInvalidPassphrase = errors.New("invalid passphrase")
	// ErrReadOnly indicates the provider supports Lookup but does not
	// implement Writer-side mutations (Put / Delete).
	ErrReadOnly = errors.New("credential provider is read-only")
)

// CurrentAPIVersion of the credential SDK contract.
const CurrentAPIVersion = "1.0.0"
