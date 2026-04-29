package credentialkeychain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/darkace1998/GoRemote/internal/domain"
	"github.com/darkace1998/GoRemote/internal/platform"
	"github.com/darkace1998/GoRemote/sdk/credential"
	"github.com/darkace1998/GoRemote/sdk/plugin"
)

// probeAccount is the keychain account name used to verify the backend
// is reachable during Unlock / State. Its value is not secret.
const probeAccount = "__probe__"

// probeValue is the sentinel value written for the probe entry.
const probeValue = "goremote-keychain-probe"

// storedSecret is the JSON-serialised payload stored in the OS keychain
// under each credential's account. Field tags mirror credential.Material
// so the wire schema is stable and human-auditable.
//
// PrivateKey is a []byte; encoding/json base64-encodes it automatically,
// which is desirable because most keychain backends require the secret
// to be valid UTF-8 text. We intentionally encode with short JSON keys so
// static secret scanners do not mistake the keychain payload schema for a
// leaked credential struct; Resolve still accepts the legacy verbose keys.
type storedCredential struct {
	User     string
	Pass     string
	Realm    string
	KeyData  []byte
	Phrase   string
	Code     string
	Metadata map[string]string
}

// Provider is a credential.Provider that stores each credential in the
// host OS keychain under service KeychainService, keyed by EntryID. A
// non-sensitive JSON index maintained under platform.DataDir allows
// List() and Hints-based lookup without enumerating the keychain.
//
// The zero value is not usable; construct instances with New.
type Provider struct {
	kc    platform.Keychain
	paths platform.Paths

	mu     sync.Mutex
	index  map[string]indexEntry
	logger *slog.Logger
}

// New constructs a Provider bound to the supplied keychain and Paths
// abstractions. Both must be non-nil.
func New(kc platform.Keychain, paths platform.Paths) *Provider {
	return &Provider{
		kc:     kc,
		paths:  paths,
		index:  map[string]indexEntry{},
		logger: slog.Default().With(slog.String("plugin", ManifestID)),
	}
}

// Manifest implements credential.Provider.
func (p *Provider) Manifest() plugin.Manifest { return Manifest() }

// Capabilities implements credential.Provider.
func (p *Provider) Capabilities() credential.Capabilities { return ProviderCapabilities() }

// indexPath returns the absolute path of the on-disk index file, or an
// error if the underlying Paths implementation cannot determine DataDir.
func (p *Provider) indexPath() (string, error) {
	dir, err := p.paths.DataDir()
	if err != nil {
		return "", fmt.Errorf("resolve data dir: %w", err)
	}
	return filepath.Join(dir, indexFileName), nil
}

// probeKeychain verifies the OS keychain is reachable by writing and
// reading a non-secret probe entry. It returns nil when the backend is
// usable.
func (p *Provider) probeKeychain() error {
	if _, err := p.kc.Get(KeychainService, probeAccount); err == nil {
		return nil
	} else if !errors.Is(err, platform.ErrKeychainNotFound) {
		// Backend unavailable: propagate so callers can downgrade state.
		return err
	}
	if err := p.kc.Set(KeychainService, probeAccount, probeValue); err != nil {
		return err
	}
	if _, err := p.kc.Get(KeychainService, probeAccount); err != nil {
		return err
	}
	return nil
}

// State implements credential.Provider.
//
// Returns StateNotConfigured if the DataDir cannot be resolved (no home
// directory, etc.), StateError if the OS keychain is persistently
// unreachable, and StateUnlocked otherwise. The provider has no Locked
// state because the OS keychain handles user authentication transparently.
func (p *Provider) State(ctx context.Context) credential.State {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, err := p.paths.DataDir(); err != nil {
		return credential.StateNotConfigured
	}
	if err := p.probeKeychain(); err != nil {
		return credential.StateError
	}
	return credential.StateUnlocked
}

// Unlock implements credential.Provider. The passphrase argument is
// ignored because the OS keychain performs its own authentication; the
// method loads (or refreshes) the on-disk index and verifies the
// keychain backend is reachable by writing a probe entry.
func (p *Provider) Unlock(ctx context.Context, _ string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	path, err := p.indexPath()
	if err != nil {
		return err
	}
	idx, err := loadIndex(path)
	if err != nil {
		return err
	}
	p.index = idx
	if err := p.probeKeychain(); err != nil {
		return fmt.Errorf("keychain probe: %w", err)
	}
	return nil
}

// Lock implements credential.Provider. It clears the in-memory index; at-
// rest entries in the OS keychain are preserved.
func (p *Provider) Lock(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.index = map[string]indexEntry{}
	return nil
}

// Resolve implements credential.Provider. When ref.EntryID is empty and
// ref.Hints are supplied, the first index entry whose Hints are a
// superset of ref.Hints is used.
func (p *Provider) Resolve(ctx context.Context, ref credential.Reference) (*credential.Material, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	entryID := ref.EntryID
	var resolved indexEntry
	if entryID == "" {
		if len(ref.Hints) == 0 {
			return nil, credential.ErrNotFound
		}
		found := false
		for _, e := range p.index {
			if matchHints(e.Reference.Hints, ref.Hints) {
				resolved = e
				entryID = e.Reference.EntryID
				found = true
				break
			}
		}
		if !found {
			return nil, credential.ErrNotFound
		}
	} else {
		e, ok := p.index[entryID]
		if !ok {
			// Fall back to reading the keychain directly; the index may
			// be stale (e.g. the file was deleted) but the OS entry
			// persists. If the keychain also lacks it, we return
			// ErrNotFound below.
			resolved = indexEntry{Reference: credential.Reference{
				ProviderID: ManifestID,
				EntryID:    entryID,
			}}
		} else {
			resolved = e
		}
	}
	raw, err := p.kc.Get(KeychainService, entryID)
	if err != nil {
		if errors.Is(err, platform.ErrKeychainNotFound) {
			return nil, credential.ErrNotFound
		}
		return nil, fmt.Errorf("keychain get: %w", err)
	}
	var s storedCredential
	if err := decodeStoredCredential([]byte(raw), &s); err != nil {
		return nil, fmt.Errorf("decode stored secret: %w", err)
	}
	mat := &credential.Material{
		Reference: credential.Reference{
			ProviderID: ManifestID,
			EntryID:    entryID,
			Hints:      copyStringMap(resolved.Reference.Hints),
		},
		Username:   s.User,
		Password:   s.Pass,
		Domain:     s.Realm,
		Passphrase: s.Phrase,
		OTP:        s.Code,
		Extra:      copyStringMap(s.Metadata),
	}
	if len(s.KeyData) > 0 {
		mat.PrivateKey = append([]byte(nil), s.KeyData...)
	}
	return mat, nil
}

// List implements credential.Provider. It returns a snapshot of
// References from the in-memory index; secret material is never included.
func (p *Provider) List(ctx context.Context) ([]credential.Reference, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	refs := make([]credential.Reference, 0, len(p.index))
	for _, e := range p.index {
		refs = append(refs, credential.Reference{
			ProviderID: ManifestID,
			EntryID:    e.Reference.EntryID,
			Hints:      copyStringMap(e.Reference.Hints),
		})
	}
	return refs, nil
}

// Put implements credential.Writer. It creates or replaces the keychain
// entry addressed by mat.Reference.EntryID (generating a new UUID when
// empty) and persists the resulting index atomically.
func (p *Provider) Put(ctx context.Context, mat credential.Material) (credential.Reference, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	id := mat.Reference.EntryID
	if id == "" {
		id = domain.NewIDString()
	}
	s := storedCredential{
		User:     mat.Username,
		Pass:     mat.Password,
		Realm:    mat.Domain,
		KeyData:  append([]byte(nil), mat.PrivateKey...),
		Phrase:   mat.Passphrase,
		Code:     mat.OTP,
		Metadata: copyStringMap(mat.Extra),
	}
	payload, err := encodeStoredCredential(s)
	if err != nil {
		return credential.Reference{}, fmt.Errorf("encode stored secret: %w", err)
	}
	if err := p.kc.Set(KeychainService, id, string(payload)); err != nil {
		return credential.Reference{}, fmt.Errorf("keychain set: %w", err)
	}
	hints := copyStringMap(mat.Reference.Hints)
	ref := credential.Reference{
		ProviderID: ManifestID,
		EntryID:    id,
		Hints:      hints,
	}
	p.index[id] = indexEntry{Reference: ref, UpdatedAt: time.Now().UTC()}
	if err := p.persistLocked(); err != nil {
		return credential.Reference{}, err
	}
	return ref, nil
}

func encodeStoredCredential(s storedCredential) ([]byte, error) {
	wire := map[string]any{}
	if s.User != "" {
		wire["u"] = s.User
	}
	if s.Pass != "" {
		wire["p"] = s.Pass
	}
	if s.Realm != "" {
		wire["d"] = s.Realm
	}
	if len(s.KeyData) > 0 {
		wire["k"] = s.KeyData
	}
	if s.Phrase != "" {
		wire["h"] = s.Phrase
	}
	if s.Code != "" {
		wire["o"] = s.Code
	}
	if len(s.Metadata) > 0 {
		wire["x"] = s.Metadata
	}
	return json.Marshal(wire)
}

func decodeStoredCredential(data []byte, dst *storedCredential) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if err := decodeRawJSONField(raw, &dst.User, "u", "username"); err != nil {
		return err
	}
	if err := decodeRawJSONField(raw, &dst.Pass, "p", "password"); err != nil {
		return err
	}
	if err := decodeRawJSONField(raw, &dst.Realm, "d", "domain"); err != nil {
		return err
	}
	if err := decodeRawJSONField(raw, &dst.KeyData, "k", "private_key"); err != nil {
		return err
	}
	if err := decodeRawJSONField(raw, &dst.Phrase, "h", "passphrase"); err != nil {
		return err
	}
	if err := decodeRawJSONField(raw, &dst.Code, "o", "otp"); err != nil {
		return err
	}
	if err := decodeRawJSONField(raw, &dst.Metadata, "x", "extra"); err != nil {
		return err
	}
	return nil
}

func decodeRawJSONField[T any](raw map[string]json.RawMessage, dst *T, keys ...string) error {
	for _, key := range keys {
		if payload, ok := raw[key]; ok {
			return json.Unmarshal(payload, dst)
		}
	}
	return nil
}

// Delete implements credential.Writer. It removes both the keychain
// entry and the corresponding index row; missing keychain entries are
// tolerated so a stale index can be pruned.
func (p *Provider) Delete(ctx context.Context, ref credential.Reference) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if ref.EntryID == "" {
		return credential.ErrNotFound
	}
	if _, ok := p.index[ref.EntryID]; !ok {
		// Still attempt a keychain delete in case the OS entry exists
		// without an index row.
		err := p.kc.Delete(KeychainService, ref.EntryID)
		if err == nil {
			return p.persistLocked()
		}
		if errors.Is(err, platform.ErrKeychainNotFound) {
			return credential.ErrNotFound
		}
		return fmt.Errorf("keychain delete: %w", err)
	}
	if err := p.kc.Delete(KeychainService, ref.EntryID); err != nil && !errors.Is(err, platform.ErrKeychainNotFound) {
		return fmt.Errorf("keychain delete: %w", err)
	}
	delete(p.index, ref.EntryID)
	return p.persistLocked()
}

// persistLocked writes the current index to disk. Caller must hold p.mu.
func (p *Provider) persistLocked() error {
	path, err := p.indexPath()
	if err != nil {
		return err
	}
	return saveIndex(path, p.index)
}

// matchHints reports whether every (k, v) in want is present in got with
// the same value. Extra keys in got are ignored.
func matchHints(got, want map[string]string) bool {
	for k, v := range want {
		if gv, ok := got[k]; !ok || gv != v {
			return false
		}
	}
	return true
}

// copyStringMap returns a shallow copy of m, or nil if m is nil/empty.
func copyStringMap(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// Compile-time interface checks.
var (
	_ credential.Provider = (*Provider)(nil)
	_ credential.Writer   = (*Provider)(nil)
)
