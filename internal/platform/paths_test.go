package platform

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestPathsLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-specific path layout")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")

	p := NewPaths()

	cfg, err := p.ConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cfg, filepath.Join(home, ".config", AppName); got != want {
		t.Errorf("ConfigDir fallback = %q want %q", got, want)
	}

	cache, err := p.CacheDir()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cache, filepath.Join(home, ".cache", AppName); got != want {
		t.Errorf("CacheDir fallback = %q want %q", got, want)
	}

	data, err := p.DataDir()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := data, filepath.Join(home, ".local", "share", AppName); got != want {
		t.Errorf("DataDir fallback = %q want %q", got, want)
	}

	logs, err := p.LogDir()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := logs, filepath.Join(home, ".local", "state", AppName); got != want {
		t.Errorf("LogDir fallback = %q want %q", got, want)
	}
}

func TestPathsLinuxXDGOverrides(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-specific path layout")
	}
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "cfg"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))

	p := NewPaths()
	cases := []struct {
		name string
		got  func() (string, error)
		want string
	}{
		{"config", p.ConfigDir, filepath.Join(root, "cfg", AppName)},
		{"cache", p.CacheDir, filepath.Join(root, "cache", AppName)},
		{"data", p.DataDir, filepath.Join(root, "data", AppName)},
		{"log", p.LogDir, filepath.Join(root, "state", AppName)},
	}
	for _, tc := range cases {
		got, err := tc.got()
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got != tc.want {
			t.Errorf("%s = %q want %q", tc.name, got, tc.want)
		}
	}
}

func TestPathsDarwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-specific path layout")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	p := NewPaths()

	cfg, err := p.ConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cfg, filepath.Join(home, "Library", "Application Support", AppName); got != want {
		t.Errorf("ConfigDir = %q want %q", got, want)
	}
	data, err := p.DataDir()
	if err != nil {
		t.Fatal(err)
	}
	if data != cfg {
		t.Errorf("DataDir = %q want %q", data, cfg)
	}
	logs, err := p.LogDir()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := logs, filepath.Join(home, "Library", "Logs", AppName); got != want {
		t.Errorf("LogDir = %q want %q", got, want)
	}
	cache, err := p.CacheDir()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cache, filepath.Join(home, "Library", "Caches", AppName); got != want {
		t.Errorf("CacheDir = %q want %q", got, want)
	}
}

func TestPathsWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-specific path layout")
	}
	root := t.TempDir()
	t.Setenv("AppData", filepath.Join(root, "Roaming"))
	t.Setenv("LocalAppData", filepath.Join(root, "Local"))

	p := NewPaths()
	cfg, err := p.ConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cfg, filepath.Join(root, "Roaming", AppName); got != want {
		t.Errorf("ConfigDir = %q want %q", got, want)
	}
	cache, err := p.CacheDir()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cache, filepath.Join(root, "Local", AppName); got != want {
		t.Errorf("CacheDir = %q want %q", got, want)
	}
}
