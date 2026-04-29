package extplugin

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	sdkplugin "github.com/darkace1998/GoRemote/sdk/plugin"
)

func writeManifest(t *testing.T, dir string, m sdkplugin.Manifest) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func validManifest(id string) sdkplugin.Manifest {
	return sdkplugin.Manifest{
		ID:         id,
		Name:       id,
		Kind:       sdkplugin.KindProtocol,
		Version:    "0.0.1",
		APIVersion: sdkplugin.CurrentAPIVersion,
	}
}

func TestOpenScansExistingPlugins(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, filepath.Join(root, "io.example.alpha"), validManifest("io.example.alpha"))
	writeManifest(t, filepath.Join(root, "io.example.beta"), validManifest("io.example.beta"))

	r, err := Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	got := r.Entries()
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2: %+v", len(got), got)
	}
	if got[0].ID != "io.example.alpha" || got[1].ID != "io.example.beta" {
		t.Fatalf("entries unsorted: %+v", got)
	}
	for _, e := range got {
		if e.Status != StatusDisabled {
			t.Errorf("plugin %q default status = %q, want %q", e.ID, e.Status, StatusDisabled)
		}
	}
}

func TestSetStatusPersistsAcrossReopen(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, filepath.Join(root, "io.example.alpha"), validManifest("io.example.alpha"))

	r, err := Open(root)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := r.SetStatus("io.example.alpha", StatusEnabled); err != nil {
		t.Fatal(err)
	}

	r2, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	e, ok := r2.Get("io.example.alpha")
	if !ok || e.Status != StatusEnabled {
		t.Fatalf("after reopen got %+v ok=%v", e, ok)
	}
}

func TestForgetRemovesDir(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, "io.example.alpha")
	writeManifest(t, pluginDir, validManifest("io.example.alpha"))

	r, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Forget("io.example.alpha"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(pluginDir); !os.IsNotExist(err) {
		t.Fatalf("expected plugin dir gone, stat err = %v", err)
	}
	if _, ok := r.Get("io.example.alpha"); ok {
		t.Fatal("entry still present after Forget")
	}
}

func TestBrokenManifestMarkedBroken(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "io.example.bad")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	e, ok := r.Get("io.example.bad")
	if !ok {
		t.Fatal("expected broken entry to be discovered")
	}
	if e.Status != StatusBroken || e.Error == "" {
		t.Fatalf("got %+v", e)
	}
}

func TestStrictPolicyRejectsUnsigned(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, filepath.Join(root, "io.example.alpha"), validManifest("io.example.alpha"))

	r, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.SetPolicy(sdkplugin.PolicyStrict); err != nil {
		t.Fatal(err)
	}
	e, _ := r.Get("io.example.alpha")
	if e.Status != StatusBroken {
		t.Fatalf("strict + unsigned should be broken, got %+v", e)
	}
}

func TestStrictPolicyAcceptsSigned(t *testing.T) {
	root := t.TempDir()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	m := validManifest("io.example.signed")
	if err := sdkplugin.Sign(&m, priv); err != nil {
		t.Fatal(err)
	}
	writeManifest(t, filepath.Join(root, "io.example.signed"), m)

	r, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.AddTrustedKey("publisher", base64.StdEncoding.EncodeToString(pub)); err != nil {
		t.Fatal(err)
	}
	if err := r.SetPolicy(sdkplugin.PolicyStrict); err != nil {
		t.Fatal(err)
	}
	e, _ := r.Get("io.example.signed")
	if e.Status == StatusBroken {
		t.Fatalf("signed + trusted key should pass strict policy, got %+v", e)
	}
	if e.TrustLevel != sdkplugin.TrustVerified {
		t.Errorf("TrustLevel = %q, want %q", e.TrustLevel, sdkplugin.TrustVerified)
	}
}

func TestTrustedKeyValidation(t *testing.T) {
	root := t.TempDir()
	r, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.AddTrustedKey("", "AA=="); err == nil {
		t.Error("expected error for empty label")
	}
	if err := r.AddTrustedKey("bad", "@@@@not base64"); err == nil {
		t.Error("expected error for non-base64 key")
	}
	if err := r.AddTrustedKey("short", base64.StdEncoding.EncodeToString([]byte{1, 2, 3})); err == nil {
		t.Error("expected error for short key")
	}
}

func TestRemoveTrustedKey(t *testing.T) {
	root := t.TempDir()
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	r, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.AddTrustedKey("p1", base64.StdEncoding.EncodeToString(pub)); err != nil {
		t.Fatal(err)
	}
	if len(r.TrustedKeys()) != 1 {
		t.Fatal("expected 1 key")
	}
	if err := r.RemoveTrustedKey("p1"); err != nil {
		t.Fatal(err)
	}
	if len(r.TrustedKeys()) != 0 {
		t.Fatalf("got %d keys, want 0", len(r.TrustedKeys()))
	}
}

func TestSetStatusRejectsInvalid(t *testing.T) {
	r, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := r.SetStatus("any", "garbage"); err == nil {
		t.Error("expected error for invalid status")
	}
}
