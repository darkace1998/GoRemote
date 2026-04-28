// Package diagnostics builds a support bundle: a single zip archive
// containing the user's settings, workspace (with secrets redacted), a
// tail of the log file, plugin manifests, and OS info. The bundle is
// hand-shareable with project maintainers without leaking credentials.
//
// This package depends on stdlib only.
package diagnostics

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Inputs configures Build. Any zero-valued path is silently skipped: the
// bundler is best-effort, so a missing log file or absent plugin root
// must not abort the export.
type Inputs struct {
	Version       string
	SettingsPath  string
	WorkspacePath string
	LogPaths      []string
	// LogTailBytes caps the per-log attachment size, taken from the end
	// of the file. 0 keeps the whole file.
	LogTailBytes int64
	PluginRoot   string
}

// Result describes what made it into the bundle. Useful for surfacing a
// summary in the UI.
type Result struct {
	BytesWritten int64
	Notes        []string
}

// Build writes a support bundle to w. It always produces a syntactically
// valid zip even when individual inputs are missing; the Result.Notes
// slice records what was skipped and why.
func Build(ctx context.Context, w io.Writer, in Inputs) (*Result, error) {
	if w == nil {
		return nil, errors.New("diagnostics: nil writer")
	}
	cw := &countingWriter{w: w}
	zw := zip.NewWriter(cw)
	res := &Result{}
	addNote := func(s string) { res.Notes = append(res.Notes, s) }

	if err := writeBundleManifest(zw, in); err != nil {
		return nil, err
	}
	if in.SettingsPath != "" {
		if err := copyFile(zw, in.SettingsPath, "settings.json"); err != nil {
			addNote("settings.json: " + err.Error())
		}
	}
	if in.WorkspacePath != "" {
		if err := writeRedactedWorkspace(zw, in.WorkspacePath); err != nil {
			addNote("workspace.json: " + err.Error())
		}
	}
	for _, p := range in.LogPaths {
		if p == "" {
			continue
		}
		if err := copyLogTail(zw, p, in.LogTailBytes); err != nil {
			addNote("log " + filepath.Base(p) + ": " + err.Error())
		}
	}
	if in.PluginRoot != "" {
		if err := copyPluginManifests(zw, in.PluginRoot); err != nil {
			addNote("plugins: " + err.Error())
		}
	}
	if err := writeOSInfo(zw); err != nil {
		addNote("osinfo: " + err.Error())
	}

	if err := zw.Close(); err != nil {
		return nil, err
	}
	res.BytesWritten = cw.n
	if err := ctx.Err(); err != nil {
		return res, err
	}
	return res, nil
}

func writeBundleManifest(zw *zip.Writer, in Inputs) error {
	doc := map[string]any{
		"schema":      "goremote-diagnostics-1",
		"version":     in.Version,
		"generatedAt": time.Now().UTC().Format(time.RFC3339),
		"goVersion":   runtime.Version(),
		"goos":        runtime.GOOS,
		"goarch":      runtime.GOARCH,
	}
	return writeJSON(zw, "manifest.json", doc)
}

func copyFile(zw *zip.Writer, src, dst string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	w, err := zw.Create(dst)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, f)
	return err
}

// SecretKeys lists JSON object keys whose values are stripped from the
// workspace before bundling. The match is case-insensitive.
var SecretKeys = []string{
	"password", "passphrase", "secret", "token", "apiKey", "api_key",
	"clientSecret", "client_secret", "privateKey", "private_key",
	"sessionPassword", "session_password",
}

const redactedPlaceholder = "[REDACTED]"

func writeRedactedWorkspace(zw *zip.Writer, src string) error {
	raw, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		w, werr := zw.Create("workspace.json")
		if werr != nil {
			return werr
		}
		_, werr = fmt.Fprintf(w, "{\"_note\":\"workspace.json not parseable as JSON\",\"err\":%q}\n", err.Error())
		return werr
	}
	doc = redactValue(doc, secretKeySet())
	return writeJSON(zw, "workspace.json", doc)
}

func secretKeySet() map[string]struct{} {
	out := make(map[string]struct{}, len(SecretKeys))
	for _, k := range SecretKeys {
		out[strings.ToLower(k)] = struct{}{}
	}
	return out
}

func redactValue(v any, keys map[string]struct{}) any {
	switch x := v.(type) {
	case map[string]any:
		for k, vv := range x {
			if _, ok := keys[strings.ToLower(k)]; ok {
				if vv == nil || vv == "" {
					x[k] = vv
				} else {
					x[k] = redactedPlaceholder
				}
				continue
			}
			x[k] = redactValue(vv, keys)
		}
		return x
	case []any:
		for i := range x {
			x[i] = redactValue(x[i], keys)
		}
		return x
	default:
		return v
	}
}

func copyLogTail(zw *zip.Writer, path string, maxBytes int64) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if maxBytes > 0 && st.Size() > maxBytes {
		if _, err := f.Seek(st.Size()-maxBytes, io.SeekStart); err != nil {
			return err
		}
	}
	w, err := zw.Create("logs/" + filepath.Base(path))
	if err != nil {
		return err
	}
	_, err = io.Copy(w, f)
	return err
}

func copyPluginManifests(zw *zip.Writer, root string) error {
	ents, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		mp := filepath.Join(root, e.Name(), "manifest.json")
		if _, err := os.Stat(mp); err != nil {
			continue
		}
		_ = copyFile(zw, mp, "plugins/"+e.Name()+"/manifest.json")
	}
	return nil
}

func writeOSInfo(zw *zip.Writer) error {
	host, _ := os.Hostname()
	wd, _ := os.Getwd()
	doc := map[string]any{
		"hostname":   host,
		"goos":       runtime.GOOS,
		"goarch":     runtime.GOARCH,
		"numCPU":     runtime.NumCPU(),
		"cwd":        wd,
		"executable": currentExecutable(),
		"env":        bundledEnv(),
	}
	return writeJSON(zw, "os-info.json", doc)
}

func currentExecutable() string {
	if e, err := os.Executable(); err == nil {
		return e
	}
	return ""
}

// bundledEnv returns env vars whose name starts with GOREMOTE_, plus a
// small allowlist of debug-relevant ones. Anything that could leak
// credentials (PATH, AWS_*, etc.) is omitted.
func bundledEnv() map[string]string {
	allow := map[string]bool{
		"GOOS": true, "GOARCH": true, "LANG": true, "LC_ALL": true, "TERM": true,
	}
	out := map[string]string{}
	for _, kv := range os.Environ() {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			continue
		}
		k, v := kv[:eq], kv[eq+1:]
		if strings.HasPrefix(k, "GOREMOTE_") || allow[k] {
			out[k] = v
		}
	}
	return out
}

func writeJSON(zw *zip.Writer, name string, v any) error {
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// unzipNames is a tiny helper used by tests to inspect a bundle.
func unzipNames(b []byte) ([]string, error) {
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		out = append(out, f.Name)
	}
	return out, nil
}
