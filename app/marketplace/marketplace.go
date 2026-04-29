// Package marketplace fetches and installs plugin listings from a static
// JSON manifest URL. The manifest is intentionally a flat document so it
// can be hosted alongside the project's GitHub releases without any
// dynamic backend.
//
// This package depends on stdlib only (net/http, encoding/json,
// crypto/sha256). No third-party HTTP client.
package marketplace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Listing is one row from the marketplace document.
type Listing struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description,omitempty"`
	Homepage    string   `json:"homepage,omitempty"`
	Publisher   string   `json:"publisher,omitempty"`
	License     string   `json:"license,omitempty"`
	OS          []string `json:"os,omitempty"`
	Arch        []string `json:"arch,omitempty"`
	// DownloadURL points at a single archive file (typically
	// .tar.gz / .zip) containing the plugin's manifest.json and binary.
	// The current installer treats it as an opaque blob and writes it to
	// <destDir>/<id>/payload alongside an inline manifest.json copy.
	DownloadURL string `json:"download_url"`
	// SHA256 of the download, lowercase hex. Required.
	SHA256 string `json:"sha256"`
	// SignedBy is informational: the publisher key label this listing
	// expects to verify against post-extract.
	SignedBy string `json:"signed_by,omitempty"`
	// Manifest is an inline copy of the plugin's manifest.json. It is
	// installed verbatim into <destDir>/<id>/manifest.json so the local
	// extplugin Registry can discover the plugin without unpacking the
	// archive itself. May be nil for listings that ship the manifest in
	// the archive only.
	Manifest json.RawMessage `json:"manifest,omitempty"`
}

// Document is the top-level marketplace manifest format.
type Document struct {
	APIVersion string    `json:"api_version"`
	UpdatedAt  time.Time `json:"updated_at,omitempty"`
	Listings   []Listing `json:"listings"`
}

// CurrentAPIVersion is the marketplace document schema version.
const CurrentAPIVersion = "1"

// Default constants for safety bounds.
const (
	defaultFetchMaxBytes    int64 = 1 << 20  // 1 MiB
	defaultDownloadMaxBytes int64 = 64 << 20 // 64 MiB
	defaultTimeout                = 30 * time.Second
)

// Client fetches and installs from a marketplace.
type Client struct {
	HTTP             *http.Client
	FetchMaxBytes    int64
	DownloadMaxBytes int64
	// AllowedSchemes restricts which URL schemes are allowed for
	// fetch/download. Defaults to {"https"}; "http" must be opted in
	// (e.g. for tests).
	AllowedSchemes []string
}

// NewClient returns a Client configured for production: HTTPS-only,
// 30-second total timeout, 1 MiB manifest cap, 64 MiB download cap.
func NewClient() *Client {
	return &Client{
		HTTP:             &http.Client{Timeout: defaultTimeout},
		FetchMaxBytes:    defaultFetchMaxBytes,
		DownloadMaxBytes: defaultDownloadMaxBytes,
		AllowedSchemes:   []string{"https"},
	}
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: defaultTimeout}
}

func (c *Client) fetchMaxBytes() int64 {
	if c.FetchMaxBytes > 0 {
		return c.FetchMaxBytes
	}
	return defaultFetchMaxBytes
}

func (c *Client) downloadMaxBytes() int64 {
	if c.DownloadMaxBytes > 0 {
		return c.DownloadMaxBytes
	}
	return defaultDownloadMaxBytes
}

func (c *Client) checkScheme(s string) error {
	if s == "" {
		return errors.New("marketplace: empty URL scheme")
	}
	allowed := c.AllowedSchemes
	if len(allowed) == 0 {
		allowed = []string{"https"}
	}
	for _, a := range allowed {
		if strings.EqualFold(a, s) {
			return nil
		}
	}
	return fmt.Errorf("marketplace: scheme %q not allowed (allowed=%v)", s, allowed)
}

// Fetch downloads, parses, and validates a marketplace document.
func (c *Client) Fetch(ctx context.Context, manifestURL string) (*Document, error) {
	u, err := url.Parse(manifestURL)
	if err != nil {
		return nil, fmt.Errorf("marketplace: parse url: %w", err)
	}
	if err := c.checkScheme(u.Scheme); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("marketplace: fetch %q: %w", manifestURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("marketplace: %q: HTTP %d", manifestURL, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, c.fetchMaxBytes()+1))
	if err != nil {
		return nil, fmt.Errorf("marketplace: read body: %w", err)
	}
	if int64(len(body)) > c.fetchMaxBytes() {
		return nil, fmt.Errorf("marketplace: manifest exceeds %d bytes", c.fetchMaxBytes())
	}
	var doc Document
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("marketplace: parse manifest: %w", err)
	}
	if err := doc.Validate(); err != nil {
		return nil, err
	}
	return &doc, nil
}

// Validate checks that the manifest is well-formed.
func (d *Document) Validate() error {
	if d == nil {
		return errors.New("marketplace: nil document")
	}
	if d.APIVersion == "" {
		return errors.New("marketplace: missing api_version")
	}
	if d.APIVersion != CurrentAPIVersion {
		return fmt.Errorf("marketplace: unsupported api_version %q (want %q)",
			d.APIVersion, CurrentAPIVersion)
	}
	for i, l := range d.Listings {
		if err := l.Validate(); err != nil {
			return fmt.Errorf("marketplace: listing[%d]: %w", i, err)
		}
	}
	return nil
}

// Validate checks that the listing has the minimum fields required for
// install.
func (l *Listing) Validate() error {
	if l == nil {
		return errors.New("nil listing")
	}
	if l.ID == "" {
		return errors.New("missing id")
	}
	if l.Version == "" {
		return errors.New("missing version")
	}
	if l.DownloadURL == "" {
		return errors.New("missing download_url")
	}
	if !isValidSHA256(l.SHA256) {
		return fmt.Errorf("invalid sha256 %q (must be 64 hex chars)", l.SHA256)
	}
	// id must be reverse-DNS-style; reject path traversal.
	if strings.ContainsAny(l.ID, "/\\") || strings.HasPrefix(l.ID, ".") {
		return fmt.Errorf("invalid id %q", l.ID)
	}
	return nil
}

func isValidSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9':
		case c >= 'a' && c <= 'f':
		case c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}

// Install downloads l.DownloadURL, verifies its SHA-256, and writes the
// payload (and the inline Manifest, if present) to <destDir>/<id>/.
//
// The destination directory is created atomically by writing to a temp
// directory first and renaming on success, so a partial download cannot
// leave a half-installed plugin behind.
func (c *Client) Install(ctx context.Context, l Listing, destDir string) error {
	if err := l.Validate(); err != nil {
		return err
	}
	if destDir == "" {
		return errors.New("marketplace: empty dest dir")
	}
	u, err := url.Parse(l.DownloadURL)
	if err != nil {
		return fmt.Errorf("marketplace: parse download_url: %w", err)
	}
	if err := c.checkScheme(u.Scheme); err != nil {
		return err
	}
	if err := os.MkdirAll(destDir, 0o750); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(destDir, ".install-"+l.ID+"-")
	if err != nil {
		return fmt.Errorf("marketplace: stage dir: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(stage)
		}
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, l.DownloadURL, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("marketplace: download %q: %w", l.DownloadURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("marketplace: download %q: HTTP %d", l.DownloadURL, resp.StatusCode)
	}

	payloadPath, err := safeStagePath(stage, "payload")
	if err != nil {
		return err
	}
	// #nosec G304 -- payload is written to an application-created staging directory.
	pf, err := os.OpenFile(payloadPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	h := sha256.New()
	limited := io.LimitReader(resp.Body, c.downloadMaxBytes()+1)
	written, err := io.Copy(io.MultiWriter(pf, h), limited)
	cerr := pf.Close()
	if err != nil {
		return fmt.Errorf("marketplace: write payload: %w", err)
	}
	if cerr != nil {
		return cerr
	}
	if written > c.downloadMaxBytes() {
		return fmt.Errorf("marketplace: download exceeds %d bytes", c.downloadMaxBytes())
	}
	gotHex := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(gotHex, l.SHA256) {
		return fmt.Errorf("marketplace: sha256 mismatch: got %s want %s", gotHex, l.SHA256)
	}

	if len(l.Manifest) > 0 {
		mp, err := safeStagePath(stage, "manifest.json")
		if err != nil {
			return err
		}
		if err := os.WriteFile(mp, []byte(l.Manifest), 0o600); err != nil {
			return err
		}
	}

	final := filepath.Join(destDir, l.ID)
	// If a previous install is present, replace it atomically by moving
	// it aside, then renaming the staged dir into place, then removing
	// the old one.
	old := final + ".old"
	if _, err := os.Stat(final); err == nil {
		_ = os.RemoveAll(old)
		if err := os.Rename(final, old); err != nil {
			return fmt.Errorf("marketplace: archive previous install: %w", err)
		}
	}
	if err := os.Rename(stage, final); err != nil {
		// Try to restore the previous install on failure.
		if _, serr := os.Stat(old); serr == nil {
			_ = os.Rename(old, final)
		}
		return fmt.Errorf("marketplace: rename stage→final: %w", err)
	}
	cleanup = false
	_ = os.RemoveAll(old)
	return nil
}

func safeStagePath(stage, name string) (string, error) {
	if name == "" {
		return "", errors.New("marketplace: empty stage path")
	}
	clean := filepath.Clean(name)
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("marketplace: unsafe stage path %q", name)
	}
	full := filepath.Join(stage, clean)
	rel, err := filepath.Rel(stage, full)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("marketplace: unsafe stage path %q", name)
	}
	return full, nil
}
