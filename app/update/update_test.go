package update

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestIsNewer(t *testing.T) {
	t.Parallel()
	cases := []struct {
		a, b string
		want bool
	}{
		{"1.13.0", "1.12.0", true},
		{"1.12.0", "1.12.0", false},
		{"1.12.0", "1.13.0", false},
		{"v1.2.3", "1.2.2", true},
		{"2.0.0", "1.99.99", true},
		{"1.12.1", "1.12.0", true},
		{"1.12.0-rc1", "1.12.0", false},
		{"", "1.0.0", false},
	}
	for _, c := range cases {
		if got := IsNewer(c.a, c.b); got != c.want {
			t.Errorf("IsNewer(%q,%q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestVerifySignatureGood(t *testing.T) {
	t.Parallel()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tgt := &ManifestTarget{
		OS:     runtime.GOOS,
		Arch:   runtime.GOARCH,
		URL:    "https://example.com/x",
		Sha256: "abc",
	}
	payload := canonicalPayload("1.2.3", tgt.OS, tgt.Arch, tgt.Sha256, tgt.URL)
	tgt.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, payload))
	if err := tgt.VerifySignature("1.2.3", base64.StdEncoding.EncodeToString(pub)); err != nil {
		t.Fatalf("VerifySignature: %v", err)
	}
}

func TestVerifySignatureTampered(t *testing.T) {
	t.Parallel()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	tgt := &ManifestTarget{OS: runtime.GOOS, Arch: runtime.GOARCH, URL: "https://example.com/x", Sha256: "abc"}
	tgt.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, canonicalPayload("1.2.3", tgt.OS, tgt.Arch, tgt.Sha256, tgt.URL)))
	// Mutate URL after signing.
	tgt.URL = "https://evil.example/y"
	if err := tgt.VerifySignature("1.2.3", base64.StdEncoding.EncodeToString(pub)); err == nil {
		t.Fatalf("VerifySignature(tampered) = nil, want error")
	}
}

func TestFetchManifestAndDownload(t *testing.T) {
	t.Parallel()
	body := []byte("fake binary payload")
	digest := sha256.Sum256(body)
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)

	mux := http.NewServeMux()
	mux.HandleFunc("/manifest.json", func(w http.ResponseWriter, r *http.Request) {
		assetURL := "https://" + r.Host + "/asset.bin"
		shaHex := hex.EncodeToString(digest[:])
		payload := canonicalPayload("9.9.9", runtime.GOOS, runtime.GOARCH, shaHex, assetURL)
		sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, payload))
		m := Manifest{Version: "9.9.9", Targets: []ManifestTarget{{
			OS: runtime.GOOS, Arch: runtime.GOARCH, URL: assetURL, Sha256: shaHex, Signature: sig,
		}}}
		_ = json.NewEncoder(w).Encode(m)
	})
	mux.HandleFunc("/asset.bin", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	m, err := FetchManifest(context.Background(), srv.URL+"/manifest.json", srv.Client())
	if err != nil {
		t.Fatalf("FetchManifest: %v", err)
	}
	if m.Version != "9.9.9" {
		t.Errorf("version = %q", m.Version)
	}
	tgt, err := m.SelectTarget()
	if err != nil {
		t.Fatalf("SelectTarget: %v", err)
	}
	if err := tgt.VerifySignature(m.Version, base64.StdEncoding.EncodeToString(pub)); err != nil {
		t.Fatalf("VerifySignature: %v", err)
	}
	path, err := Download(context.Background(), tgt, t.TempDir(), srv.Client())
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if !strings.Contains(path, "goremote-update-") {
		t.Errorf("path = %q", path)
	}
}

func TestFetchManifestHTTPRejected(t *testing.T) {
	t.Parallel()
	_, err := FetchManifest(context.Background(), "http://example.com/manifest.json")
	if err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("expected https-only error, got %v", err)
	}
}

func TestManifestEmptySha256Rejected(t *testing.T) {
	t.Parallel()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m := Manifest{Version: "1.0.0", Targets: []ManifestTarget{{
			OS:     runtime.GOOS,
			Arch:   runtime.GOARCH,
			URL:    "https://" + r.Host + "/bin",
			Sha256: "", // invalid: empty
		}}}
		_ = json.NewEncoder(w).Encode(m)
	}))
	defer srv.Close()
	_, err := FetchManifest(context.Background(), srv.URL+"/manifest.json", srv.Client())
	if err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("expected sha256 validation error, got %v", err)
	}
}

func TestCopyFileAtomic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src.bin")
	dst := filepath.Join(dir, "dst.bin")
	if err := os.WriteFile(src, []byte("update content"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Pre-existing dst should be replaced atomically.
	if err := os.WriteFile(dst, []byte("original"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(src, dst, 0o700); err != nil {
		t.Fatalf("copyFile: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "update content" {
		t.Errorf("dst content = %q, want 'update content'", got)
	}
	// Verify no leftover temp files in dir.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".update-") {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}
}

func TestWindowsSwapRollsBack(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	dst := filepath.Join(dir, "live.exe")
	if err := os.WriteFile(dst, []byte("live"), 0o700); err != nil {
		t.Fatal(err)
	}
	// downloaded does not exist → both Rename and copyFile fail.
	downloaded := filepath.Join(dir, "nonexistent.bin")
	err := windowsSwap(dst, downloaded)
	if err == nil {
		t.Fatal("expected error when downloaded file does not exist")
	}
	// dst must be restored from the .old backup.
	got, rerr := os.ReadFile(dst)
	if rerr != nil {
		t.Fatalf("dst not restored after failed swap: %v", rerr)
	}
	if string(got) != "live" {
		t.Errorf("dst content after rollback = %q, want 'live'", got)
	}
}
