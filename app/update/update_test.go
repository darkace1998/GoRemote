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
		var srv *httptest.Server
		_ = srv
		// We can't reference srv before it exists; reconstruct URL from r.Host.
		assetURL := "http://" + r.Host + "/asset.bin"
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
	srv := httptest.NewServer(mux)
	defer srv.Close()

	m, err := FetchManifest(context.Background(), srv.URL+"/manifest.json")
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
	path, err := Download(context.Background(), tgt, t.TempDir())
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if !strings.Contains(path, "goremote-update-") {
		t.Errorf("path = %q", path)
	}
}
