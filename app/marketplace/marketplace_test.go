package marketplace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newClient() *Client {
	c := NewClient()
	c.AllowedSchemes = []string{"http", "https"} // tests use httptest (http://).
	return c
}

func TestFetchSuccess(t *testing.T) {
	doc := Document{
		APIVersion: CurrentAPIVersion,
		Listings: []Listing{{
			ID:          "io.example.alpha",
			Name:        "Alpha",
			Version:     "1.0.0",
			DownloadURL: "https://example.com/alpha-1.0.0.tar.gz",
			SHA256:      strings.Repeat("a", 64),
		}},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(doc)
	}))
	defer srv.Close()

	got, err := newClient().Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(got.Listings) != 1 || got.Listings[0].ID != "io.example.alpha" {
		t.Fatalf("got %+v", got)
	}
}

func TestFetchRejectsHTTPByDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"api_version":"1","listings":[]}`))
	}))
	defer srv.Close()
	_, err := NewClient().Fetch(context.Background(), srv.URL) // production HTTPS-only.
	if err == nil || !strings.Contains(err.Error(), "scheme") {
		t.Fatalf("expected scheme rejection, got %v", err)
	}
}

func TestFetchRejectsBadAPIVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"api_version":"99","listings":[]}`))
	}))
	defer srv.Close()
	_, err := newClient().Fetch(context.Background(), srv.URL)
	if err == nil || !strings.Contains(err.Error(), "api_version") {
		t.Fatalf("expected api_version error, got %v", err)
	}
}

func TestFetchRejectsOversized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"api_version":"1","listings":[],"description":"`))
		for i := 0; i < 200; i++ {
			w.Write([]byte("xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"))
		}
		w.Write([]byte(`"}`))
	}))
	defer srv.Close()
	c := newClient()
	c.FetchMaxBytes = 1024
	_, err := c.Fetch(context.Background(), srv.URL)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected oversize rejection, got %v", err)
	}
}

func TestFetchRejectsMalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("{not json"))
	}))
	defer srv.Close()
	_, err := newClient().Fetch(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected parse error")
	}
}

func TestListingValidateRejectsPathTraversal(t *testing.T) {
	cases := []string{"../escape", "a/b", `c\d`, ".hidden"}
	for _, id := range cases {
		l := Listing{ID: id, Version: "1", DownloadURL: "https://x", SHA256: strings.Repeat("a", 64)}
		if err := l.Validate(); err == nil {
			t.Errorf("id %q should be rejected", id)
		}
	}
}

func TestListingValidateRejectsBadSHA(t *testing.T) {
	l := Listing{ID: "io.x", Version: "1", DownloadURL: "https://x", SHA256: "not-hex"}
	if err := l.Validate(); err == nil {
		t.Error("expected sha256 error")
	}
}

func TestInstallSuccess(t *testing.T) {
	payload := []byte("PKG_BLOB_v1")
	sum := sha256.Sum256(payload)
	hexSum := hex.EncodeToString(sum[:])

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(payload)
	}))
	defer srv.Close()

	manifestJSON := []byte(`{"id":"io.example.alpha","name":"Alpha","kind":"protocol","version":"1.0.0","api_version":"1.0.0"}`)
	dest := t.TempDir()
	l := Listing{
		ID: "io.example.alpha", Version: "1.0.0",
		DownloadURL: srv.URL,
		SHA256:      hexSum,
		Manifest:    manifestJSON,
	}
	if err := newClient().Install(context.Background(), l, dest); err != nil {
		t.Fatalf("Install: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "io.example.alpha", "payload"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Errorf("payload mismatch")
	}
	mp, err := os.ReadFile(filepath.Join(dest, "io.example.alpha", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(mp) != string(manifestJSON) {
		t.Errorf("manifest mismatch: %s", mp)
	}
}

func TestInstallSHA256Mismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("real bytes"))
	}))
	defer srv.Close()

	dest := t.TempDir()
	l := Listing{
		ID: "io.example.alpha", Version: "1.0.0",
		DownloadURL: srv.URL,
		SHA256:      strings.Repeat("0", 64), // wrong
	}
	err := newClient().Install(context.Background(), l, dest)
	if err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("expected sha mismatch, got %v", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(dest, "io.example.alpha")); len(entries) > 0 {
		t.Errorf("install dir should not exist after failure: %v", entries)
	}
}

func TestInstallOversize(t *testing.T) {
	big := make([]byte, 2048)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(big)
	}))
	defer srv.Close()
	c := newClient()
	c.DownloadMaxBytes = 1024
	sum := sha256.Sum256(big)
	l := Listing{
		ID: "io.example.alpha", Version: "1",
		DownloadURL: srv.URL, SHA256: hex.EncodeToString(sum[:]),
	}
	err := c.Install(context.Background(), l, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected exceeds error, got %v", err)
	}
}

func TestInstallReplacesExisting(t *testing.T) {
	dest := t.TempDir()
	prev := filepath.Join(dest, "io.example.alpha")
	if err := os.MkdirAll(prev, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prev, "stale"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	payload := []byte("new")
	sum := sha256.Sum256(payload)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(payload)
	}))
	defer srv.Close()

	l := Listing{
		ID: "io.example.alpha", Version: "2",
		DownloadURL: srv.URL, SHA256: hex.EncodeToString(sum[:]),
	}
	if err := newClient().Install(context.Background(), l, dest); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if _, err := os.Stat(filepath.Join(prev, "stale")); !os.IsNotExist(err) {
		t.Fatalf("old payload survived: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(prev, "payload"))
	if string(got) != "new" {
		t.Errorf("payload = %q, want %q", got, "new")
	}
}

func TestInstallRejectsBadListing(t *testing.T) {
	err := newClient().Install(context.Background(), Listing{}, t.TempDir())
	if err == nil {
		t.Fatal("expected error")
	}
}

// fmt is here to keep go-vet happy with potential future use.
var _ = fmt.Sprintf
