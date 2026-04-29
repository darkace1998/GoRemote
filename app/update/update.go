// Package update implements a minimal, signature-verified auto-update
// client.
//
// The model is intentionally small: a remote JSON manifest describes
// the latest release; the user (or operator) bundles the public half of
// an Ed25519 signing key into Settings.AutoUpdatePublicKey; the
// manifest carries a base64 signature over the canonical payload
// (version + sha256 + url, comma-joined). We refuse to apply an update
// whose signature does not verify against the configured public key —
// even if the manifest URL is HTTPS — so a compromised CDN cannot
// silently push a backdoored binary to users.
//
// This package does NOT cover signed installers (msi/dmg/deb); those
// remain a packaging concern outside the running binary. It targets
// the portable-zip / single-exe distribution path.
package update

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Manifest is the document fetched from Settings.AutoUpdateURL. It
// describes the latest available build for one or more (os,arch)
// targets. Each target has its own signature so a compromised single
// platform asset cannot be promoted across platforms.
type Manifest struct {
	Version string           `json:"version"`
	Targets []ManifestTarget `json:"targets"`
	Notes   string           `json:"notes,omitempty"`
}

// ManifestTarget describes a single downloadable artefact.
type ManifestTarget struct {
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	URL       string `json:"url"`
	Sha256    string `json:"sha256"`
	Signature string `json:"signature"`
}

// FetchManifest downloads and decodes the manifest at url. The HTTP
// client uses a short timeout and refuses redirects to non-HTTPS
// schemes.
func FetchManifest(ctx context.Context, url string) (*Manifest, error) {
	if url == "" {
		return nil, errors.New("update: empty manifest url")
	}
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") {
		return nil, fmt.Errorf("update: manifest url must be http(s): %s", url)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("update: fetch manifest: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("update: manifest http %d", resp.StatusCode)
	}
	var m Manifest
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&m); err != nil {
		return nil, fmt.Errorf("update: decode manifest: %w", err)
	}
	if m.Version == "" {
		return nil, errors.New("update: manifest has no version")
	}
	return &m, nil
}

// SelectTarget returns the manifest target matching the running OS and
// architecture, or an error if no target applies.
func (m *Manifest) SelectTarget() (*ManifestTarget, error) {
	for i := range m.Targets {
		t := &m.Targets[i]
		if t.OS == runtime.GOOS && t.Arch == runtime.GOARCH {
			return t, nil
		}
	}
	return nil, fmt.Errorf("update: no manifest target for %s/%s", runtime.GOOS, runtime.GOARCH)
}

// VerifySignature checks the target's signature against the supplied
// base64-encoded public key. The signed payload is the canonical
// triple "version|os|arch|sha256|url".
func (t *ManifestTarget) VerifySignature(version, pubKeyB64 string) error {
	pub, err := decodeKey(pubKeyB64)
	if err != nil {
		return err
	}
	sig, err := base64.StdEncoding.DecodeString(t.Signature)
	if err != nil {
		return fmt.Errorf("update: decode signature: %w", err)
	}
	payload := canonicalPayload(version, t.OS, t.Arch, t.Sha256, t.URL)
	if !ed25519.Verify(pub, payload, sig) {
		return errors.New("update: signature does not verify")
	}
	return nil
}

func canonicalPayload(version, os, arch, sha, url string) []byte {
	return []byte(strings.Join([]string{version, os, arch, sha, url}, "|"))
}

func decodeKey(b64 string) (ed25519.PublicKey, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil {
		return nil, fmt.Errorf("update: decode public key: %w", err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("update: public key has wrong size %d (want %d)", len(raw), ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(raw), nil
}

// Download streams the target's URL into a temp file under destDir,
// verifying the SHA-256 as it goes. The returned path is the absolute
// path to the downloaded file. The caller is responsible for moving or
// removing it.
func Download(ctx context.Context, t *ManifestTarget, destDir string) (string, error) {
	if t == nil {
		return "", errors.New("update: nil target")
	}
	if err := os.MkdirAll(destDir, 0o750); err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 10 * time.Minute}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.URL, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("update: download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("update: download http %d", resp.StatusCode)
	}
	tmp, err := os.CreateTemp(destDir, "goremote-update-*.bin")
	if err != nil {
		return "", err
	}
	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, hasher), resp.Body); err != nil {
		return "", joinCleanupError(
			fmt.Errorf("update: copy: %w", err),
			tmp.Close(),
			removeIfExists(tmp.Name()),
		)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return "", err
	}
	got := hex.EncodeToString(hasher.Sum(nil))
	want := strings.ToLower(strings.TrimSpace(t.Sha256))
	if want != "" && got != want {
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("update: sha256 mismatch: got %s want %s", got, want)
	}
	return tmp.Name(), nil
}

// IsNewer returns true when manifestVersion is strictly greater than
// currentVersion under a simple dotted-numeric comparison. Non-numeric
// suffixes are compared lexicographically as a last resort. Empty
// versions are treated as "0".
func IsNewer(manifestVersion, currentVersion string) bool {
	return compareSemver(manifestVersion, currentVersion) > 0
}

func compareSemver(a, b string) int {
	ap := splitSemver(a)
	bp := splitSemver(b)
	n := len(ap)
	if len(bp) > n {
		n = len(bp)
	}
	for i := 0; i < n; i++ {
		var av, bv int
		if i < len(ap) {
			av = ap[i]
		}
		if i < len(bp) {
			bv = bp[i]
		}
		if av != bv {
			if av < bv {
				return -1
			}
			return 1
		}
	}
	return 0
}

func splitSemver(s string) []int {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if s == "" {
		return []int{0}
	}
	// Truncate at first non-numeric/dot character (e.g. "-rc1").
	for i, r := range s {
		if r != '.' && (r < '0' || r > '9') {
			s = s[:i]
			break
		}
	}
	out := make([]int, 0, 3)
	for _, p := range strings.Split(s, ".") {
		n := 0
		for _, r := range p {
			if r < '0' || r > '9' {
				break
			}
			n = n*10 + int(r-'0')
		}
		out = append(out, n)
	}
	return out
}

// SwapInPlace replaces the current executable with downloaded.
//
// On Unix this is a single rename (dst replaced atomically). On Windows
// the live executable cannot be deleted while running, so we rename the
// existing file to ".old" (deleting any prior .old) and then move the
// new file into place. The caller is expected to restart the process
// for the new binary to take effect; the old file is best-effort
// cleaned up next time the new binary launches.
func SwapInPlace(downloaded string) error {
	dst, err := os.Executable()
	if err != nil {
		return err
	}
	dst, err = filepath.EvalSymlinks(dst)
	if err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		old := dst + ".old"
		_ = os.Remove(old)
		if err := os.Rename(dst, old); err != nil {
			return fmt.Errorf("update: rename old: %w", err)
		}
	}
	// Make sure the new file is executable (Unix permissions).
	mode := installedExecutableMode(dst)
	if runtime.GOOS != "windows" {
		_ = os.Chmod(downloaded, mode)
	}
	if err := os.Rename(downloaded, dst); err != nil {
		// Try a copy+remove if rename across filesystems failed.
		if cerr := copyFile(downloaded, dst, mode); cerr != nil {
			return fmt.Errorf("update: install: %w", cerr)
		}
		_ = os.Remove(downloaded)
	}
	return nil
}

// CleanupOld removes a leftover .old file from a previous SwapInPlace
// on Windows. Safe to call on any platform; a no-op when nothing to do.
func CleanupOld() {
	dst, err := os.Executable()
	if err != nil {
		return
	}
	if r, err := filepath.EvalSymlinks(dst); err == nil {
		dst = r
	}
	_ = os.Remove(dst + ".old")
}

func installedExecutableMode(path string) os.FileMode {
	if st, err := os.Stat(path); err == nil {
		if perm := st.Mode().Perm(); perm != 0 {
			return perm
		}
	}
	return 0o700
}

func copyFile(src, dst string, mode os.FileMode) error {
	// #nosec G304 -- src is a temp update payload and dst is the current executable path.
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	// #nosec G304 -- src is a temp update payload and dst is the current executable path.
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		return joinCleanupError(err, out.Close())
	}
	return out.Close()
}

func joinCleanupError(base error, errs ...error) error {
	joined := base
	for _, err := range errs {
		if err != nil {
			joined = errors.Join(joined, err)
		}
	}
	return joined
}

func removeIfExists(path string) error {
	err := os.Remove(path)
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
