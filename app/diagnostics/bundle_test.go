package diagnostics

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func readFromZip(t *testing.T, b []byte, name string) string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatalf("zip read: %v", err)
	}
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("open %s: %v", name, err)
			}
			defer rc.Close()
			data, err := io.ReadAll(rc)
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}
			return string(data)
		}
	}
	t.Fatalf("zip entry %q not found; have: %v", name, names(zr))
	return ""
}

func names(zr *zip.Reader) []string {
	out := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		out = append(out, f.Name)
	}
	return out
}

func TestBuild_AllInputsPresent(t *testing.T) {
	dir := t.TempDir()
	settingsP := filepath.Join(dir, "settings.json")
	wsP := filepath.Join(dir, "workspace.json")
	logP := filepath.Join(dir, "goremote.log")
	pluginRoot := filepath.Join(dir, "plugins")
	writeFile(t, settingsP, `{"theme":"dark"}`)
	writeFile(t, wsP, `{"connections":[{"id":"a","host":"h","password":"sekret","tags":["x"]}]}`)
	writeFile(t, logP, "log line 1\nlog line 2\n")
	writeFile(t, filepath.Join(pluginRoot, "p1", "manifest.json"), `{"id":"p1","version":"0.1.0"}`)

	var buf bytes.Buffer
	res, err := Build(context.Background(), &buf, Inputs{
		Version:       "9.9.9",
		SettingsPath:  settingsP,
		WorkspacePath: wsP,
		LogPaths:      []string{logP},
		PluginRoot:    pluginRoot,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if res.BytesWritten <= 0 {
		t.Errorf("BytesWritten = %d", res.BytesWritten)
	}
	got, _ := unzipNames(buf.Bytes())
	want := []string{"manifest.json", "settings.json", "workspace.json", "logs/goremote.log", "plugins/p1/manifest.json", "os-info.json"}
	for _, w := range want {
		found := false
		for _, g := range got {
			if g == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing %q in bundle (got %v)", w, got)
		}
	}

	// Workspace must be redacted.
	ws := readFromZip(t, buf.Bytes(), "workspace.json")
	if strings.Contains(ws, "sekret") {
		t.Errorf("workspace not redacted: %s", ws)
	}
	if !strings.Contains(ws, redactedPlaceholder) {
		t.Errorf("expected %s placeholder; got: %s", redactedPlaceholder, ws)
	}

	// Manifest JSON parses and carries the version.
	man := readFromZip(t, buf.Bytes(), "manifest.json")
	var m map[string]any
	if err := json.Unmarshal([]byte(man), &m); err != nil {
		t.Fatalf("manifest not JSON: %v", err)
	}
	if m["version"] != "9.9.9" {
		t.Errorf("manifest version = %v", m["version"])
	}
}

func TestBuild_MissingPathsAreNotFatal(t *testing.T) {
	var buf bytes.Buffer
	res, err := Build(context.Background(), &buf, Inputs{
		Version:       "0.0.1",
		SettingsPath:  "/nonexistent/settings.json",
		WorkspacePath: "/nonexistent/workspace.json",
		LogPaths:      []string{"/nonexistent/log"},
	})
	if err != nil {
		t.Fatalf("Build should not fail on missing inputs: %v", err)
	}
	if len(res.Notes) < 3 {
		t.Errorf("expected notes for each missing input, got %v", res.Notes)
	}
	got, _ := unzipNames(buf.Bytes())
	hasManifest := false
	for _, n := range got {
		if n == "manifest.json" {
			hasManifest = true
		}
	}
	if !hasManifest {
		t.Errorf("manifest.json missing from partial bundle")
	}
}

func TestBuild_LogTailRespectsLimit(t *testing.T) {
	dir := t.TempDir()
	logP := filepath.Join(dir, "x.log")
	body := strings.Repeat("a", 1024) + "TAIL_MARKER" + strings.Repeat("b", 256)
	writeFile(t, logP, body)

	var buf bytes.Buffer
	if _, err := Build(context.Background(), &buf, Inputs{
		LogPaths:     []string{logP},
		LogTailBytes: 300,
	}); err != nil {
		t.Fatalf("Build: %v", err)
	}
	got := readFromZip(t, buf.Bytes(), "logs/x.log")
	if int64(len(got)) > 300 {
		t.Errorf("tail too long: %d", len(got))
	}
	if !strings.Contains(got, "TAIL_MARKER") {
		t.Errorf("tail should include trailing marker; got %q", got)
	}
}

func TestBuild_RedactsNestedSecrets(t *testing.T) {
	dir := t.TempDir()
	wsP := filepath.Join(dir, "workspace.json")
	writeFile(t, wsP, `{"folders":[{"name":"a","creds":{"PASSWORD":"p1","apiKey":"k1","other":"keep"}}]}`)

	var buf bytes.Buffer
	if _, err := Build(context.Background(), &buf, Inputs{WorkspacePath: wsP}); err != nil {
		t.Fatalf("Build: %v", err)
	}
	ws := readFromZip(t, buf.Bytes(), "workspace.json")
	if strings.Contains(ws, "p1") || strings.Contains(ws, "k1") {
		t.Errorf("nested secrets leaked: %s", ws)
	}
	if !strings.Contains(ws, "keep") {
		t.Errorf("non-secret field lost: %s", ws)
	}
}

func TestBuild_NonJSONWorkspaceFallsBack(t *testing.T) {
	dir := t.TempDir()
	wsP := filepath.Join(dir, "workspace.json")
	writeFile(t, wsP, "not json at all")

	var buf bytes.Buffer
	if _, err := Build(context.Background(), &buf, Inputs{WorkspacePath: wsP}); err != nil {
		t.Fatalf("Build: %v", err)
	}
	ws := readFromZip(t, buf.Bytes(), "workspace.json")
	if !strings.Contains(ws, "not parseable") {
		t.Errorf("expected fallback note; got %q", ws)
	}
}

func TestBuild_EnvAllowlistDoesNotLeakArbitraryVars(t *testing.T) {
	t.Setenv("AWS_SECRET_ACCESS_KEY", "do-not-leak")
	t.Setenv("GOREMOTE_TEST_VAR", "ok")

	var buf bytes.Buffer
	if _, err := Build(context.Background(), &buf, Inputs{}); err != nil {
		t.Fatalf("Build: %v", err)
	}
	osInfo := readFromZip(t, buf.Bytes(), "os-info.json")
	if strings.Contains(osInfo, "do-not-leak") {
		t.Errorf("env allowlist leaked AWS secret: %s", osInfo)
	}
	if !strings.Contains(osInfo, "GOREMOTE_TEST_VAR") {
		t.Errorf("expected GOREMOTE_ var in env: %s", osInfo)
	}
}
