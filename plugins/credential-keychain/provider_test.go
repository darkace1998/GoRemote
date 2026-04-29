package credentialkeychain

import (
	"context"
	"errors"
	"testing"

	keyring "github.com/zalando/go-keyring"

	"github.com/darkace1998/GoRemote/internal/platform"
	"github.com/darkace1998/GoRemote/sdk/credential"
)

// fakePaths is a minimal platform.Paths implementation whose DataDir is
// a test-supplied absolute path. It is sufficient for provider tests
// because the keychain provider only consults DataDir.
type fakePaths struct{ dataDir string }

func (f fakePaths) ConfigDir() (string, error) { return f.dataDir, nil }
func (f fakePaths) DataDir() (string, error)   { return f.dataDir, nil }
func (f fakePaths) CacheDir() (string, error)  { return f.dataDir, nil }
func (f fakePaths) LogDir() (string, error)    { return f.dataDir, nil }

// mockKeychain wraps go-keyring (after MockInit) in the platform.Keychain
// interface, translating keyring errors into the platform sentinels so
// the provider's errors.Is checks behave as they would in production.
type mockKeychain struct{}

func (mockKeychain) Get(service, account string) (string, error) {
	v, err := keyring.Get(service, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", platform.ErrKeychainNotFound
	}
	return v, err
}
func (mockKeychain) Set(service, account, secret string) error {
	return keyring.Set(service, account, secret)
}
func (mockKeychain) Delete(service, account string) error {
	err := keyring.Delete(service, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return platform.ErrKeychainNotFound
	}
	return err
}

// newProviderForTest resets the global keyring mock and returns a
// Provider rooted at a fresh temp dir. Tests sharing the same go-keyring
// process-global state must each call this to start from a clean slate.
func newProviderForTest(t *testing.T) (*Provider, fakePaths) {
	t.Helper()
	keyring.MockInit()
	fp := fakePaths{dataDir: t.TempDir()}
	return New(mockKeychain{}, fp), fp
}

func TestPutResolveRoundTrip(t *testing.T) {
	ctx := context.Background()
	p, _ := newProviderForTest(t)
	if err := p.Unlock(ctx, ""); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	ref, err := p.Put(ctx, credential.Material{
		Reference:  credential.Reference{Hints: map[string]string{"host": "example"}},
		Username:   "alice",
		Password:   "s3cret",
		Domain:     "corp",
		PrivateKey: []byte{1, 2, 3, 4},
		Passphrase: "keypass",
		OTP:        "123456",
		Extra:      map[string]string{"note": "primary"},
	})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if ref.EntryID == "" {
		t.Fatalf("expected generated EntryID")
	}
	mat, err := p.Resolve(ctx, ref)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if mat.Username != "alice" || mat.Password != "s3cret" || mat.Domain != "corp" ||
		mat.Passphrase != "keypass" || mat.OTP != "123456" {
		t.Fatalf("unexpected material: %+v", mat)
	}
	if string(mat.PrivateKey) != "\x01\x02\x03\x04" {
		t.Fatalf("private key mismatch: %v", mat.PrivateKey)
	}
	if mat.Extra["note"] != "primary" {
		t.Fatalf("extra mismatch: %v", mat.Extra)
	}
	if mat.Reference.Hints["host"] != "example" {
		t.Fatalf("hints mismatch: %v", mat.Reference.Hints)
	}
}

func TestResolveByHints(t *testing.T) {
	ctx := context.Background()
	p, _ := newProviderForTest(t)
	if err := p.Unlock(ctx, ""); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	if _, err := p.Put(ctx, credential.Material{
		Reference: credential.Reference{Hints: map[string]string{"host": "a", "user": "root"}},
		Password:  "pa",
	}); err != nil {
		t.Fatalf("put a: %v", err)
	}
	wanted, err := p.Put(ctx, credential.Material{
		Reference: credential.Reference{Hints: map[string]string{"host": "b", "user": "root"}},
		Password:  "pb",
	})
	if err != nil {
		t.Fatalf("put b: %v", err)
	}
	mat, err := p.Resolve(ctx, credential.Reference{Hints: map[string]string{"host": "b"}})
	if err != nil {
		t.Fatalf("resolve by hints: %v", err)
	}
	if mat.Password != "pb" {
		t.Fatalf("resolved wrong entry: %q", mat.Password)
	}
	if mat.Reference.EntryID != wanted.EntryID {
		t.Fatalf("resolved EntryID mismatch: got %q want %q", mat.Reference.EntryID, wanted.EntryID)
	}
}

func TestDeleteThenResolve(t *testing.T) {
	ctx := context.Background()
	p, _ := newProviderForTest(t)
	if err := p.Unlock(ctx, ""); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	ref, err := p.Put(ctx, credential.Material{Password: "x"})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := p.Delete(ctx, ref); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := p.Resolve(ctx, ref); !errors.Is(err, credential.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestListReturnsAllReferences(t *testing.T) {
	ctx := context.Background()
	p, _ := newProviderForTest(t)
	if err := p.Unlock(ctx, ""); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	want := map[string]bool{}
	for i := 0; i < 3; i++ {
		ref, err := p.Put(ctx, credential.Material{Password: "p"})
		if err != nil {
			t.Fatalf("put %d: %v", i, err)
		}
		want[ref.EntryID] = true
	}
	refs, err := p.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(refs) != 3 {
		t.Fatalf("expected 3 refs, got %d", len(refs))
	}
	for _, r := range refs {
		if !want[r.EntryID] {
			t.Fatalf("unexpected EntryID %q in list", r.EntryID)
		}
		if r.ProviderID != ManifestID {
			t.Fatalf("unexpected ProviderID %q", r.ProviderID)
		}
	}
}

func TestIndexSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	keyring.MockInit()
	kc := mockKeychain{}
	fp := fakePaths{dataDir: t.TempDir()}

	p1 := New(kc, fp)
	if err := p1.Unlock(ctx, ""); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	ref, err := p1.Put(ctx, credential.Material{
		Reference: credential.Reference{Hints: map[string]string{"host": "h"}},
		Username:  "bob",
		Password:  "p",
	})
	if err != nil {
		t.Fatalf("put: %v", err)
	}

	// Simulate a process restart: new Provider, same keychain + paths.
	p2 := New(kc, fp)
	if err := p2.Unlock(ctx, ""); err != nil {
		t.Fatalf("unlock 2: %v", err)
	}
	refs, err := p2.List(ctx)
	if err != nil {
		t.Fatalf("list 2: %v", err)
	}
	if len(refs) != 1 || refs[0].EntryID != ref.EntryID {
		t.Fatalf("index did not survive restart: %+v", refs)
	}
	if refs[0].Hints["host"] != "h" {
		t.Fatalf("hints lost across restart: %+v", refs[0].Hints)
	}
	mat, err := p2.Resolve(ctx, ref)
	if err != nil {
		t.Fatalf("resolve 2: %v", err)
	}
	if mat.Username != "bob" || mat.Password != "p" {
		t.Fatalf("resolved material mismatch: %+v", mat)
	}
}

func TestStateUnlockedAfterInit(t *testing.T) {
	ctx := context.Background()
	p, _ := newProviderForTest(t)
	if err := p.Unlock(ctx, ""); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	if st := p.State(ctx); st != credential.StateUnlocked {
		t.Fatalf("expected StateUnlocked, got %v", st)
	}
}

func TestManifestAndCapabilities(t *testing.T) {
	p, _ := newProviderForTest(t)
	m := p.Manifest()
	if m.ID != ManifestID {
		t.Fatalf("manifest id: %q", m.ID)
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("manifest invalid: %v", err)
	}
	caps := p.Capabilities()
	if !caps.Write || !caps.Lookup || caps.Unlock {
		t.Fatalf("unexpected caps: %+v", caps)
	}
	if len(caps.SupportedKinds) != 4 {
		t.Fatalf("expected 4 supported kinds, got %v", caps.SupportedKinds)
	}
}
