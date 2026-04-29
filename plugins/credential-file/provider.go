package credentialfile

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/darkace1998/GoRemote/internal/domain"
	"github.com/darkace1998/GoRemote/sdk/credential"
	"github.com/darkace1998/GoRemote/sdk/plugin"
)

// Provider is a credential.Provider backed by a single encrypted file.
//
// The zero value is not usable; construct instances with New.
type Provider struct {
	path string

	mu     sync.Mutex
	vault  *vault
	key    []byte
	salt   []byte
	locked bool
	// unlocked tracks whether Unlock has succeeded since process start.
	// State() uses this to distinguish "file exists, waiting for unlock"
	// from "file exists and is currently unlocked".
	unlocked bool

	logger *slog.Logger
}

// New constructs a Provider bound to the given file path. The file need not
// exist yet; it is created lazily on the first successful save after Unlock.
func New(path string) *Provider {
	return &Provider{
		path:   path,
		locked: true,
		logger: slog.Default().With(slog.String("plugin", ManifestID)),
	}
}

// Manifest implements credential.Provider.
func (p *Provider) Manifest() plugin.Manifest { return Manifest() }

// Capabilities implements credential.Provider.
func (p *Provider) Capabilities() credential.Capabilities { return ProviderCapabilities() }

// State implements credential.Provider.
func (p *Provider) State(ctx context.Context) credential.State {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, err := os.Stat(p.path); err != nil {
		if os.IsNotExist(err) {
			if p.unlocked && p.vault != nil {
				// File has been prepared in memory but not yet saved; we
				// still report unlocked because Put will persist it.
				return credential.StateUnlocked
			}
			return credential.StateNotConfigured
		}
		return credential.StateError
	}
	if p.unlocked {
		return credential.StateUnlocked
	}
	return credential.StateLocked
}

// Unlock implements credential.Provider.
//
// If the backing file does not yet exist, the passphrase is accepted as the
// new master key and retained in memory until the first save creates the
// file on disk.
func (p *Provider) Unlock(ctx context.Context, passphrase string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// #nosec G304 -- p.path is the configured vault file path for this provider.
	data, err := os.ReadFile(p.path)
	if errors.Is(err, os.ErrNotExist) {
		// Bootstrap: remember the passphrase-derived key for the first save.
		salt, key, derr := newSaltAndKey(passphrase)
		if derr != nil {
			return derr
		}
		p.vault = &vault{Version: 1}
		p.zeroKeyLocked()
		p.key = key
		p.salt = salt
		p.locked = false
		p.unlocked = true
		return nil
	}
	if err != nil {
		return fmt.Errorf("read credential file: %w", err)
	}
	v, key, salt, derr := decodeFile(data, passphrase)
	if derr != nil {
		// Treat AES-GCM authentication failure as an invalid passphrase.
		// Other errors (short/corrupt/magic) propagate verbatim so callers
		// can distinguish file corruption from a bad passphrase.
		if errors.Is(derr, ErrBadMagic) || errors.Is(derr, ErrUnsupportedVersion) || errors.Is(derr, ErrShortFile) {
			return derr
		}
		// cipher.AEAD.Open returns an opaque error ("cipher: message
		// authentication failed") on either bad key or tampered ciphertext.
		// We map the former to ErrInvalidPassphrase; the integrity test in
		// provider_test.go confirms tampering is also surfaced as an error.
		p.logger.Debug("unlock failed", slog.String("err", derr.Error()))
		return credential.ErrInvalidPassphrase
	}
	p.zeroKeyLocked()
	p.vault = v
	p.key = key
	p.salt = salt
	p.locked = false
	p.unlocked = true
	return nil
}

// Lock implements credential.Provider. It zeros the cached key and vault so
// resolved material cannot be produced until Unlock is called again.
func (p *Provider) Lock(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.zeroKeyLocked()
	p.vault = nil
	p.salt = nil
	p.locked = true
	p.unlocked = false
	return nil
}

// Resolve implements credential.Provider.
func (p *Provider) Resolve(ctx context.Context, ref credential.Reference) (*credential.Material, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.locked || p.vault == nil {
		return nil, credential.ErrLocked
	}
	e := p.findEntryLocked(ref)
	if e == nil {
		return nil, credential.ErrNotFound
	}
	outRef := credential.Reference{
		ProviderID: ManifestID,
		EntryID:    e.ID,
		Hints:      copyStringMap(e.Hints),
	}
	mat := &credential.Material{
		Reference:  outRef,
		Username:   e.Username,
		Password:   e.Password,
		Domain:     e.Domain,
		Passphrase: e.Passphrase,
		OTP:        e.OTP,
	}
	if len(e.PrivateKey) > 0 {
		mat.PrivateKey = append([]byte(nil), e.PrivateKey...)
	}
	return mat, nil
}

// List implements credential.Provider. It returns a Reference per entry with
// hints copied; no secret fields are included.
func (p *Provider) List(ctx context.Context) ([]credential.Reference, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.locked || p.vault == nil {
		return nil, credential.ErrLocked
	}
	refs := make([]credential.Reference, 0, len(p.vault.Entries))
	for i := range p.vault.Entries {
		e := &p.vault.Entries[i]
		refs = append(refs, credential.Reference{
			ProviderID: ManifestID,
			EntryID:    e.ID,
			Hints:      copyStringMap(e.Hints),
		})
	}
	return refs, nil
}

// Put implements credential.Writer.
func (p *Provider) Put(ctx context.Context, mat credential.Material) (credential.Reference, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.locked || p.vault == nil {
		return credential.Reference{}, credential.ErrLocked
	}
	id := mat.Reference.EntryID
	if id == "" {
		id = domain.NewIDString()
	}
	newEntry := entry{
		ID:         id,
		Username:   mat.Username,
		Password:   mat.Password,
		Domain:     mat.Domain,
		PrivateKey: append([]byte(nil), mat.PrivateKey...),
		Passphrase: mat.Passphrase,
		OTP:        mat.OTP,
		Hints:      copyStringMap(mat.Reference.Hints),
		UpdatedAt:  time.Now().UTC(),
	}
	replaced := false
	for i := range p.vault.Entries {
		if p.vault.Entries[i].ID == id {
			p.vault.Entries[i] = newEntry
			replaced = true
			break
		}
	}
	if !replaced {
		p.vault.Entries = append(p.vault.Entries, newEntry)
	}
	if err := p.saveLocked(); err != nil {
		return credential.Reference{}, err
	}
	return credential.Reference{
		ProviderID: ManifestID,
		EntryID:    id,
		Hints:      copyStringMap(newEntry.Hints),
	}, nil
}

// Delete implements credential.Writer.
func (p *Provider) Delete(ctx context.Context, ref credential.Reference) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.locked || p.vault == nil {
		return credential.ErrLocked
	}
	for i := range p.vault.Entries {
		if p.vault.Entries[i].ID == ref.EntryID {
			p.vault.Entries = append(p.vault.Entries[:i], p.vault.Entries[i+1:]...)
			return p.saveLocked()
		}
	}
	return credential.ErrNotFound
}

// findEntryLocked returns a pointer to the matching entry or nil. Caller
// must hold p.mu.
func (p *Provider) findEntryLocked(ref credential.Reference) *entry {
	if ref.EntryID != "" {
		for i := range p.vault.Entries {
			if p.vault.Entries[i].ID == ref.EntryID {
				return &p.vault.Entries[i]
			}
		}
		return nil
	}
	if len(ref.Hints) == 0 {
		return nil
	}
	for i := range p.vault.Entries {
		e := &p.vault.Entries[i]
		if matchHints(e.Hints, ref.Hints) {
			return e
		}
	}
	return nil
}

// matchHints returns true if every key in want exists in got with the same
// value. Extra keys in got are ignored.
func matchHints(got, want map[string]string) bool {
	for k, v := range want {
		if gv, ok := got[k]; !ok || gv != v {
			return false
		}
	}
	return true
}

// saveLocked serialises and atomically replaces the on-disk file. Caller
// must hold p.mu and have p.vault, p.key, p.salt populated.
func (p *Provider) saveLocked() error {
	payload, err := encodeFileWithKey(p.vault, p.key, p.salt)
	if err != nil {
		return err
	}
	return atomicWrite(p.path, payload, 0o600)
}

// atomicWrite writes data to path via path+".tmp" + fsync + rename, then
// syncs the parent directory so the rename is durable.
func atomicWrite(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir parent: %w", err)
	}
	tmp := path + ".tmp"
	// #nosec G304 -- tmp is derived from the provider-controlled destination path.
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("open tmp: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		return cleanupAtomicWriteFailure(fmt.Errorf("write tmp: %w", err), f, tmp)
	}
	if err := f.Sync(); err != nil {
		return cleanupAtomicWriteFailure(fmt.Errorf("sync tmp: %w", err), f, tmp)
	}
	if err := f.Close(); err != nil {
		return cleanupAtomicWriteFailure(fmt.Errorf("close tmp: %w", err), nil, tmp)
	}
	if err := os.Rename(tmp, path); err != nil {
		return cleanupAtomicWriteFailure(fmt.Errorf("rename: %w", err), nil, tmp)
	}
	// Sync the directory so the rename is durable across crashes.
	if err := syncDir(dir); err != nil {
		return err
	}
	return nil
}

func cleanupAtomicWriteFailure(base error, f *os.File, tmp string) error {
	if f != nil {
		if err := f.Close(); err != nil {
			base = errors.Join(base, fmt.Errorf("close tmp: %w", err))
		}
	}
	if err := removeAtomicWriteTemp(tmp); err != nil {
		base = errors.Join(base, fmt.Errorf("remove tmp: %w", err))
	}
	return base
}

func removeAtomicWriteTemp(path string) error {
	err := os.Remove(path)
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func syncDir(dir string) error {
	// #nosec G304 -- dir is derived from the provider-controlled destination path.
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open parent dir: %w", err)
	}
	var joined error
	if err := d.Sync(); err != nil {
		if !errors.Is(err, os.ErrInvalid) && !errors.Is(err, os.ErrPermission) {
			joined = errors.Join(joined, fmt.Errorf("sync parent dir: %w", err))
		}
	}
	if err := d.Close(); err != nil {
		joined = errors.Join(joined, fmt.Errorf("close parent dir: %w", err))
	}
	return joined
}

// zeroKeyLocked wipes the in-memory AES key. Caller must hold p.mu.
func (p *Provider) zeroKeyLocked() {
	if p.key != nil {
		zero(p.key)
		p.key = nil
	}
}

// newSaltAndKey generates a fresh random salt and derives an Argon2id key
// for an initial (empty) vault.
func newSaltAndKey(passphrase string) (salt, key []byte, err error) {
	salt = make([]byte, saltLen)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, nil, fmt.Errorf("random salt: %w", err)
	}
	key = deriveKey(passphrase, salt)
	return salt, key, nil
}

// Compile-time interface checks.
var (
	_ credential.Provider = (*Provider)(nil)
	_ credential.Writer   = (*Provider)(nil)
)

func copyStringMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
