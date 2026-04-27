// Package fakecred provides an in-process credential.Provider implementation
// used by goremote's end-to-end integration tests. Resolve always returns a
// canned Material; List/Put/Delete operate on an in-memory map.
package fakecred

import (
	"context"
	"errors"
	"sync"

	"github.com/goremote/goremote/sdk/credential"
	"github.com/goremote/goremote/sdk/plugin"
)

// ManifestID is the static manifest ID published by the fake provider.
const ManifestID = "io.goremote.test.fake-cred"

// CannedUsername / CannedPassword are returned by every Resolve call.
const (
	CannedUsername = "alice"
	CannedPassword = "secret"
)

// Recorder collects every observable interaction with the fake provider.
type Recorder struct {
	mu       sync.Mutex
	resolves []credential.Reference
	puts     []credential.Material
	deletes  []credential.Reference
}

// Resolves returns a snapshot of every Reference that Resolve was called with.
func (r *Recorder) Resolves() []credential.Reference {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]credential.Reference, len(r.resolves))
	copy(out, r.resolves)
	return out
}

// Puts returns a snapshot of every Material passed to Put.
func (r *Recorder) Puts() []credential.Material {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]credential.Material, len(r.puts))
	copy(out, r.puts)
	return out
}

// Deletes returns a snapshot of every Reference passed to Delete.
func (r *Recorder) Deletes() []credential.Reference {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]credential.Reference, len(r.deletes))
	copy(out, r.deletes)
	return out
}

// Provider is the fake credential.Provider + credential.Writer implementation.
type Provider struct {
	rec *Recorder

	mu      sync.Mutex
	entries map[string]credential.Material
}

// New returns a fresh Provider with its own Recorder.
func New() *Provider {
	return &Provider{
		rec:     &Recorder{},
		entries: make(map[string]credential.Material),
	}
}

// Recorder returns the Recorder that backs this provider.
func (p *Provider) Recorder() *Recorder { return p.rec }

// Manifest implements credential.Provider.
func (p *Provider) Manifest() plugin.Manifest {
	return plugin.Manifest{
		ID:          ManifestID,
		Name:        "Fake Credentials (test)",
		Description: "In-process credential provider used by integration tests.",
		Kind:        plugin.KindCredential,
		Version:     "0.0.1",
		APIVersion:  credential.CurrentAPIVersion,
		Status:      plugin.StatusExperimental,
		Publisher:   "goremote-tests",
	}
}

// Capabilities implements credential.Provider.
func (p *Provider) Capabilities() credential.Capabilities {
	return credential.Capabilities{
		Lookup:         true,
		Write:          true,
		SupportedKinds: []credential.Kind{credential.KindPassword},
	}
}

// State implements credential.Provider; the fake is always Unlocked.
func (p *Provider) State(ctx context.Context) credential.State {
	return credential.StateUnlocked
}

// Unlock is a no-op.
func (p *Provider) Unlock(ctx context.Context, passphrase string) error { return nil }

// Lock is a no-op.
func (p *Provider) Lock(ctx context.Context) error { return nil }

// Resolve records the call and returns a canned Material. If a Put has
// previously stored a matching entry, the stored Material is returned
// instead so write/read round-trips can be verified.
func (p *Provider) Resolve(ctx context.Context, ref credential.Reference) (*credential.Material, error) {
	p.rec.mu.Lock()
	p.rec.resolves = append(p.rec.resolves, ref)
	p.rec.mu.Unlock()

	p.mu.Lock()
	stored, ok := p.entries[ref.EntryID]
	p.mu.Unlock()
	if ok {
		m := stored
		m.Reference = ref
		return &m, nil
	}
	return &credential.Material{
		Reference: ref,
		Username:  CannedUsername,
		Password:  CannedPassword,
	}, nil
}

// List returns every stored Reference plus a stable canonical entry so the
// host always sees at least one resolvable credential.
func (p *Provider) List(ctx context.Context) ([]credential.Reference, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]credential.Reference, 0, len(p.entries)+1)
	out = append(out, credential.Reference{ProviderID: ManifestID, EntryID: "canned"})
	for k := range p.entries {
		out = append(out, credential.Reference{ProviderID: ManifestID, EntryID: k})
	}
	return out, nil
}

// Put stores a Material in-memory under mat.Reference.EntryID.
func (p *Provider) Put(ctx context.Context, mat credential.Material) (credential.Reference, error) {
	if mat.Reference.EntryID == "" {
		return credential.Reference{}, errors.New("fakecred: EntryID is required")
	}
	ref := credential.Reference{ProviderID: ManifestID, EntryID: mat.Reference.EntryID}
	mat.Reference = ref
	p.mu.Lock()
	p.entries[ref.EntryID] = mat
	p.mu.Unlock()
	p.rec.mu.Lock()
	p.rec.puts = append(p.rec.puts, mat)
	p.rec.mu.Unlock()
	return ref, nil
}

// Delete removes a Material from the in-memory store.
func (p *Provider) Delete(ctx context.Context, ref credential.Reference) error {
	p.mu.Lock()
	delete(p.entries, ref.EntryID)
	p.mu.Unlock()
	p.rec.mu.Lock()
	p.rec.deletes = append(p.rec.deletes, ref)
	p.rec.mu.Unlock()
	return nil
}
