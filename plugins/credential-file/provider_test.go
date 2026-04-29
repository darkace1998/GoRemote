package credentialfile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/darkace1998/GoRemote/sdk/credential"
)

func tempPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "vault.crf")
}

func TestUnlockPutResolveRoundTrip(t *testing.T) {
	ctx := context.Background()
	p := New(tempPath(t))
	if err := p.Unlock(ctx, "pw"); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	mat := credential.Material{
		Reference: credential.Reference{Hints: map[string]string{"host": "h1"}},
		Username:  "alice",
		Password:  "s3cr3t",
		Domain:    "corp",
	}
	ref, err := p.Put(ctx, mat)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if ref.EntryID == "" {
		t.Fatal("expected generated EntryID")
	}
	got, err := p.Resolve(ctx, ref)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.Username != "alice" || got.Password != "s3cr3t" || got.Domain != "corp" {
		t.Fatalf("material mismatch: %+v", got)
	}
}

func TestLockThenResolveErrLocked(t *testing.T) {
	ctx := context.Background()
	p := New(tempPath(t))
	if err := p.Unlock(ctx, "pw"); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	ref, err := p.Put(ctx, credential.Material{Username: "u"})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := p.Lock(ctx); err != nil {
		t.Fatalf("lock: %v", err)
	}
	if _, err := p.Resolve(ctx, ref); !errors.Is(err, credential.ErrLocked) {
		t.Fatalf("expected ErrLocked, got %v", err)
	}
	if _, err := p.List(ctx); !errors.Is(err, credential.ErrLocked) {
		t.Fatalf("list: expected ErrLocked, got %v", err)
	}
	if _, err := p.Put(ctx, credential.Material{}); !errors.Is(err, credential.ErrLocked) {
		t.Fatalf("put: expected ErrLocked, got %v", err)
	}
	if err := p.Delete(ctx, ref); !errors.Is(err, credential.ErrLocked) {
		t.Fatalf("delete: expected ErrLocked, got %v", err)
	}
}

func TestWrongPassphrase(t *testing.T) {
	ctx := context.Background()
	path := tempPath(t)
	p := New(path)
	if err := p.Unlock(ctx, "right"); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	if _, err := p.Put(ctx, credential.Material{Username: "u"}); err != nil {
		t.Fatalf("put: %v", err)
	}

	p2 := New(path)
	err := p2.Unlock(ctx, "wrong")
	if !errors.Is(err, credential.ErrInvalidPassphrase) {
		t.Fatalf("expected ErrInvalidPassphrase, got %v", err)
	}
}

func TestPersistenceRoundTrip(t *testing.T) {
	ctx := context.Background()
	path := tempPath(t)

	p := New(path)
	if err := p.Unlock(ctx, "pw"); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	orig := credential.Material{
		Reference:  credential.Reference{Hints: map[string]string{"host": "h", "tag": "db"}},
		Username:   "bob",
		Password:   "hunter2",
		Domain:     "ACME",
		PrivateKey: []byte("-----BEGIN KEY-----\nabc\n-----END KEY-----\n"),
		Passphrase: "keypass",
		OTP:        "123456",
	}
	ref, err := p.Put(ctx, orig)
	if err != nil {
		t.Fatalf("put: %v", err)
	}

	// Reopen
	p2 := New(path)
	if st := p2.State(ctx); st != credential.StateLocked {
		t.Fatalf("expected StateLocked after reopen, got %v", st)
	}
	if err := p2.Unlock(ctx, "pw"); err != nil {
		t.Fatalf("reunlock: %v", err)
	}
	got, err := p2.Resolve(ctx, credential.Reference{EntryID: ref.EntryID})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.Username != orig.Username || got.Password != orig.Password ||
		got.Domain != orig.Domain || got.Passphrase != orig.Passphrase ||
		got.OTP != orig.OTP {
		t.Fatalf("material mismatch: %+v", got)
	}
	if !reflect.DeepEqual(got.PrivateKey, orig.PrivateKey) {
		t.Fatalf("private key mismatch")
	}
}

func TestResolveByHints(t *testing.T) {
	ctx := context.Background()
	p := New(tempPath(t))
	if err := p.Unlock(ctx, "pw"); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	if _, err := p.Put(ctx, credential.Material{
		Reference: credential.Reference{Hints: map[string]string{"host": "a"}},
		Username:  "ua",
	}); err != nil {
		t.Fatalf("put a: %v", err)
	}
	if _, err := p.Put(ctx, credential.Material{
		Reference: credential.Reference{Hints: map[string]string{"host": "b"}},
		Username:  "ub",
	}); err != nil {
		t.Fatalf("put b: %v", err)
	}
	got, err := p.Resolve(ctx, credential.Reference{Hints: map[string]string{"host": "b"}})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.Username != "ub" {
		t.Fatalf("expected ub, got %q", got.Username)
	}
}

func TestDeleteThenResolveNotFound(t *testing.T) {
	ctx := context.Background()
	p := New(tempPath(t))
	if err := p.Unlock(ctx, "pw"); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	ref, err := p.Put(ctx, credential.Material{Username: "u"})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := p.Delete(ctx, ref); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := p.Resolve(ctx, ref); !errors.Is(err, credential.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err := p.Delete(ctx, ref); !errors.Is(err, credential.ErrNotFound) {
		t.Fatalf("second delete: expected ErrNotFound, got %v", err)
	}
}

func TestCorruptedFileFailsUnlock(t *testing.T) {
	ctx := context.Background()
	path := tempPath(t)
	p := New(path)
	if err := p.Unlock(ctx, "pw"); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	if _, err := p.Put(ctx, credential.Material{Username: "u"}); err != nil {
		t.Fatalf("put: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	// Flip a byte deep in the ciphertext region (past the header).
	idx := len(data) - 5
	data[idx] ^= 0xFF
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	p2 := New(path)
	if err := p2.Unlock(ctx, "pw"); err == nil {
		t.Fatal("expected error from tampered file, got nil")
	}
}

func TestStateTransitions(t *testing.T) {
	ctx := context.Background()
	path := tempPath(t)
	p := New(path)
	if st := p.State(ctx); st != credential.StateNotConfigured {
		t.Fatalf("initial: expected NotConfigured, got %v", st)
	}
	if err := p.Unlock(ctx, "pw"); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	// After bootstrap unlock but before first save the file still doesn't
	// exist on disk; we accept StateUnlocked in this in-memory stage.
	if st := p.State(ctx); st != credential.StateUnlocked {
		t.Fatalf("after unlock (pre-save): expected Unlocked, got %v", st)
	}
	if _, err := p.Put(ctx, credential.Material{Username: "u"}); err != nil {
		t.Fatalf("put: %v", err)
	}
	if st := p.State(ctx); st != credential.StateUnlocked {
		t.Fatalf("after put: expected Unlocked, got %v", st)
	}

	// Fresh provider against the existing file should be Locked.
	p2 := New(path)
	if st := p2.State(ctx); st != credential.StateLocked {
		t.Fatalf("fresh reopen: expected Locked, got %v", st)
	}
	if err := p2.Unlock(ctx, "pw"); err != nil {
		t.Fatalf("reunlock: %v", err)
	}
	if st := p2.State(ctx); st != credential.StateUnlocked {
		t.Fatalf("after reunlock: expected Unlocked, got %v", st)
	}
	if err := p2.Lock(ctx); err != nil {
		t.Fatalf("lock: %v", err)
	}
	if st := p2.State(ctx); st != credential.StateLocked {
		t.Fatalf("after lock: expected Locked, got %v", st)
	}
}

func TestNoTempLeftoverAfterSave(t *testing.T) {
	ctx := context.Background()
	path := tempPath(t)
	p := New(path)
	if err := p.Unlock(ctx, "pw"); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	if _, err := p.Put(ctx, credential.Material{Username: "u"}); err != nil {
		t.Fatalf("put: %v", err)
	}
	if _, err := os.Stat(path + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no .tmp leftover, stat err=%v", err)
	}
}

func TestManifestAndCapabilities(t *testing.T) {
	p := New(tempPath(t))
	m := p.Manifest()
	if m.ID != ManifestID {
		t.Fatalf("manifest id: %q", m.ID)
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("manifest invalid: %v", err)
	}
	caps := p.Capabilities()
	if !caps.Write || !caps.Lookup || !caps.Unlock {
		t.Fatalf("unexpected caps: %+v", caps)
	}
	if len(caps.SupportedKinds) != 2 {
		t.Fatalf("expected 2 supported kinds, got %v", caps.SupportedKinds)
	}
}
