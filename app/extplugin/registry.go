// Package extplugin manages the on-disk registry of out-of-process plugins:
// discovery, persisted enable/quarantine state, and the user-visible trust
// policy (trusted keys + permissive/strict).
//
// This package is the data layer behind the Settings → Plugins UI. It does
// NOT spawn external plugin processes — that is a separate launcher
// component. Discovery is purely filesystem-based: a plugin is a directory
// under <root>/<id>/ containing a manifest.json. State (enabled, quarantined)
// is persisted in <root>/state.json. Trusted keys are persisted in
// <root>/trusted_keys.json.
package extplugin

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"crypto/ed25519"

	sdkplugin "github.com/darkace1998/GoRemote/sdk/plugin"
)

// EntryStatus is the user-visible health/disposition of a discovered plugin.
type EntryStatus string

const (
	// StatusEnabled means the plugin is approved by the user and would be
	// launched at boot.
	StatusEnabled EntryStatus = "enabled"
	// StatusDisabled means the plugin is known but the user has turned it
	// off.
	StatusDisabled EntryStatus = "disabled"
	// StatusQuarantined means the plugin is suspended pending review;
	// stronger than disabled because it survives auto-promotion / sync.
	StatusQuarantined EntryStatus = "quarantined"
	// StatusBroken means the manifest is invalid or the plugin failed
	// signature verification under the current policy.
	StatusBroken EntryStatus = "broken"
)

// Entry is a single discovered plugin.
type Entry struct {
	ID           string
	Manifest     sdkplugin.Manifest
	ManifestPath string
	Status       EntryStatus
	TrustLevel   sdkplugin.Trust
	// Error explains StatusBroken; empty otherwise.
	Error string
}

// state is the on-disk persistence document. Trusted keys live in a
// separate file so they can be moved across machines independently of
// per-plugin enable state.
type state struct {
	Plugins map[string]EntryStatus `json:"plugins,omitempty"`
}

type trustDoc struct {
	Policy sdkplugin.Policy  `json:"policy"`
	Keys   map[string]string `json:"keys,omitempty"` // label → base64 pubkey
}

// Registry coordinates discovery, persisted state, and trust-key management
// for out-of-process plugins. The zero value is unusable; construct via Open.
type Registry struct {
	root string

	mu      sync.RWMutex
	state   state
	trust   trustDoc
	entries map[string]*Entry
}

const (
	stateFile  = "state.json"
	trustFile  = "trusted_keys.json"
	pluginsDir = "" // entries live directly under root
)

// Open creates the plugin root if needed, loads state.json and
// trusted_keys.json, and performs a discovery scan. It returns a usable
// Registry.
func Open(root string) (*Registry, error) {
	if root == "" {
		return nil, errors.New("extplugin: empty root")
	}
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, fmt.Errorf("extplugin: mkdir %q: %w", root, err)
	}
	r := &Registry{
		root:    root,
		state:   state{Plugins: map[string]EntryStatus{}},
		trust:   trustDoc{Policy: sdkplugin.PolicyPermissive, Keys: map[string]string{}},
		entries: map[string]*Entry{},
	}
	if err := r.loadState(); err != nil {
		return nil, err
	}
	if err := r.loadTrust(); err != nil {
		return nil, err
	}
	if err := r.Refresh(); err != nil {
		return nil, err
	}
	return r, nil
}

// Root returns the on-disk plugin root directory.
func (r *Registry) Root() string { return r.root }

// Policy returns the current verifier policy.
func (r *Registry) Policy() sdkplugin.Policy {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.trust.Policy
}

// SetPolicy persists a new policy and re-evaluates discovered manifests.
func (r *Registry) SetPolicy(p sdkplugin.Policy) error {
	switch p {
	case sdkplugin.PolicyPermissive, sdkplugin.PolicyStrict:
	default:
		return fmt.Errorf("extplugin: invalid policy %q", p)
	}
	r.mu.Lock()
	r.trust.Policy = p
	if err := r.saveTrustLocked(); err != nil {
		r.mu.Unlock()
		return err
	}
	r.mu.Unlock()
	return r.Refresh()
}

// TrustedKey is a label → base64 public key pair.
type TrustedKey struct {
	Label  string
	PubKey string // base64-encoded ed25519 public key (32 bytes raw)
}

// TrustedKeys returns a snapshot of currently trusted keys.
func (r *Registry) TrustedKeys() []TrustedKey {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]TrustedKey, 0, len(r.trust.Keys))
	for label, key := range r.trust.Keys {
		out = append(out, TrustedKey{Label: label, PubKey: key})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out
}

// AddTrustedKey registers a base64-encoded ed25519 public key under label.
// Returns an error if the key cannot be decoded into a 32-byte ed25519 key.
func (r *Registry) AddTrustedKey(label, pubKeyB64 string) error {
	if label == "" {
		return errors.New("extplugin: empty label")
	}
	raw, err := base64.StdEncoding.DecodeString(pubKeyB64)
	if err != nil {
		return fmt.Errorf("extplugin: decode key %q: %w", label, err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return fmt.Errorf("extplugin: key %q wrong size: got %d want %d",
			label, len(raw), ed25519.PublicKeySize)
	}
	r.mu.Lock()
	r.trust.Keys[label] = pubKeyB64
	if err := r.saveTrustLocked(); err != nil {
		r.mu.Unlock()
		return err
	}
	r.mu.Unlock()
	return r.Refresh()
}

// RemoveTrustedKey deletes the trusted key with the given label. Removing a
// missing label is a no-op.
func (r *Registry) RemoveTrustedKey(label string) error {
	r.mu.Lock()
	delete(r.trust.Keys, label)
	if err := r.saveTrustLocked(); err != nil {
		r.mu.Unlock()
		return err
	}
	r.mu.Unlock()
	return r.Refresh()
}

// Verifier returns a sdk/plugin.Verifier configured against the current
// trust store and policy. Callers use this to verify a plugin's signed
// manifest before launching its host process.
func (r *Registry) Verifier() *sdkplugin.Verifier {
	r.mu.RLock()
	defer r.mu.RUnlock()
	store := &sdkplugin.TrustStore{}
	for label, key := range r.trust.Keys {
		raw, err := base64.StdEncoding.DecodeString(key)
		if err != nil || len(raw) != ed25519.PublicKeySize {
			continue
		}
		store.Add(label, ed25519.PublicKey(raw))
	}
	return sdkplugin.NewVerifier(store, r.trust.Policy)
}

// Entries returns a snapshot of discovered plugins, sorted by ID.
func (r *Registry) Entries() []Entry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Entry, 0, len(r.entries))
	for _, e := range r.entries {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Get returns the entry with the given ID.
func (r *Registry) Get(id string) (Entry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[id]
	if !ok {
		return Entry{}, false
	}
	return *e, true
}

// SetStatus updates the persisted status for a plugin. It is legal to set
// the status of a plugin that is not currently discovered (e.g. to
// pre-quarantine before installation).
func (r *Registry) SetStatus(id string, s EntryStatus) error {
	switch s {
	case StatusEnabled, StatusDisabled, StatusQuarantined:
	default:
		return fmt.Errorf("extplugin: invalid status %q", s)
	}
	r.mu.Lock()
	if r.state.Plugins == nil {
		r.state.Plugins = map[string]EntryStatus{}
	}
	r.state.Plugins[id] = s
	if err := r.saveStateLocked(); err != nil {
		r.mu.Unlock()
		return err
	}
	if e, ok := r.entries[id]; ok && e.Status != StatusBroken {
		e.Status = s
	}
	r.mu.Unlock()
	return nil
}

// Forget removes the on-disk plugin directory (if present) and forgets
// any persisted state. Useful for "Uninstall".
func (r *Registry) Forget(id string) error {
	if id == "" {
		return errors.New("extplugin: empty id")
	}
	r.mu.Lock()
	delete(r.state.Plugins, id)
	delete(r.entries, id)
	if err := r.saveStateLocked(); err != nil {
		r.mu.Unlock()
		return err
	}
	r.mu.Unlock()
	dir, err := r.pluginDirPath(id)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("extplugin: remove %q: %w", dir, err)
	}
	return nil
}

// Refresh re-scans the plugin root, validating manifests and re-applying
// signature verification against the current policy.
func (r *Registry) Refresh() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.refreshLocked()
}

func (r *Registry) refreshLocked() error {
	r.entries = map[string]*Entry{}
	dir, err := os.ReadDir(r.root)
	if err != nil {
		return fmt.Errorf("extplugin: read %q: %w", r.root, err)
	}
	for _, d := range dir {
		if !d.IsDir() {
			continue
		}
		pluginDir, err := r.pluginDirPath(d.Name())
		if err != nil {
			continue
		}
		manifestPath, err := safeJoinWithinRoot(pluginDir, "manifest.json")
		if err != nil {
			continue
		}
		// #nosec G304 -- manifestPath is constrained to a discovered child directory under the registry root.
		f, err := os.Open(manifestPath)
		if err != nil {
			continue
		}
		entry := r.loadOneLocked(d.Name(), manifestPath, f)
		_ = f.Close()
		r.entries[entry.ID] = entry
	}
	return nil
}

func (r *Registry) loadOneLocked(dirName, manifestPath string, src io.Reader) *Entry {
	e := &Entry{ID: dirName, ManifestPath: manifestPath}
	var m sdkplugin.Manifest
	dec := json.NewDecoder(src)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		e.Status = StatusBroken
		e.Error = fmt.Sprintf("manifest decode: %v", err)
		return e
	}
	if err := m.Validate(); err != nil {
		e.Status = StatusBroken
		e.Error = fmt.Sprintf("manifest invalid: %v", err)
		return e
	}
	if m.ID != "" {
		e.ID = m.ID
	}
	e.Manifest = m
	v := r.verifierLocked()
	if err := v.Verify(&m); err != nil {
		e.Status = StatusBroken
		e.Error = err.Error()
		e.TrustLevel = ""
		return e
	}
	e.Manifest = m
	e.TrustLevel = m.Trust
	if persisted, ok := r.state.Plugins[e.ID]; ok {
		e.Status = persisted
	} else {
		e.Status = StatusDisabled
	}
	return e
}

// verifierLocked is verifier() but assumes the caller already holds r.mu.
func (r *Registry) verifierLocked() *sdkplugin.Verifier {
	store := &sdkplugin.TrustStore{}
	for label, key := range r.trust.Keys {
		raw, err := base64.StdEncoding.DecodeString(key)
		if err != nil || len(raw) != ed25519.PublicKeySize {
			continue
		}
		store.Add(label, ed25519.PublicKey(raw))
	}
	return sdkplugin.NewVerifier(store, r.trust.Policy)
}

func (r *Registry) loadState() error {
	path, err := safeJoinWithinRoot(r.root, stateFile)
	if err != nil {
		return err
	}
	// #nosec G304 -- state.json is a fixed file under the registry root.
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("extplugin: read state: %w", err)
	}
	var s state
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("extplugin: parse state: %w", err)
	}
	if s.Plugins == nil {
		s.Plugins = map[string]EntryStatus{}
	}
	r.state = s
	return nil
}

func (r *Registry) saveStateLocked() error {
	path, err := safeJoinWithinRoot(r.root, stateFile)
	if err != nil {
		return err
	}
	return writeJSONAtomic(path, r.state)
}

func (r *Registry) loadTrust() error {
	path, err := safeJoinWithinRoot(r.root, trustFile)
	if err != nil {
		return err
	}
	// #nosec G304 -- trusted_keys.json is a fixed file under the registry root.
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("extplugin: read trust: %w", err)
	}
	var t trustDoc
	if err := json.Unmarshal(data, &t); err != nil {
		return fmt.Errorf("extplugin: parse trust: %w", err)
	}
	if t.Keys == nil {
		t.Keys = map[string]string{}
	}
	if t.Policy == "" {
		t.Policy = sdkplugin.PolicyPermissive
	}
	r.trust = t
	return nil
}

func (r *Registry) saveTrustLocked() error {
	path, err := safeJoinWithinRoot(r.root, trustFile)
	if err != nil {
		return err
	}
	return writeJSONAtomic(path, r.trust)
}

func (r *Registry) pluginDirPath(id string) (string, error) {
	if id == "" {
		return "", errors.New("extplugin: empty id")
	}
	if filepath.Base(id) != id || strings.ContainsAny(id, `/\\`) {
		return "", fmt.Errorf("extplugin: invalid plugin id %q", id)
	}
	return safeJoinWithinRoot(r.root, id)
}

func safeJoinWithinRoot(root string, elems ...string) (string, error) {
	base, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	parts := append([]string{base}, elems...)
	dest := filepath.Join(parts...)
	dest, err = filepath.Abs(dest)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(base, dest)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("extplugin: path escapes root: %s", dest)
	}
	return dest, nil
}

// writeJSONAtomic writes v as pretty JSON to path via a temp file + rename.
func writeJSONAtomic(path string, v any) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}
