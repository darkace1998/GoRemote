package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// NewPaths returns the default Paths implementation. It uses
// os.UserConfigDir / os.UserCacheDir internally and layers XDG / macOS
// specific fallbacks on top for paths not covered by the standard
// library (data and log directories).
func NewPaths() Paths {
	return pathsImpl{}
}

type pathsImpl struct{}

// ConfigDir returns <user-config-dir>/goremote where <user-config-dir>
// follows OS conventions: $XDG_CONFIG_HOME (or ~/.config) on Linux,
// ~/Library/Application Support on macOS, %AppData% on Windows.
func (pathsImpl) ConfigDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("platform: resolve config dir: %w", err)
	}
	return filepath.Join(base, AppName), nil
}

// CacheDir returns <user-cache-dir>/goremote.
func (pathsImpl) CacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("platform: resolve cache dir: %w", err)
	}
	return filepath.Join(base, AppName), nil
}

// DataDir returns the application data directory:
//   - Linux:   $XDG_DATA_HOME/goremote, falling back to ~/.local/share/goremote
//   - macOS:   ~/Library/Application Support/goremote
//   - Windows: %AppData%/goremote
func (p pathsImpl) DataDir() (string, error) {
	switch runtime.GOOS {
	case "linux":
		if v := os.Getenv("XDG_DATA_HOME"); v != "" {
			return filepath.Join(v, AppName), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("platform: resolve data dir: %w", err)
		}
		return filepath.Join(home, ".local", "share", AppName), nil
	default:
		// On macOS and Windows the config dir is also the natural data
		// location (Application Support / %AppData%).
		return p.ConfigDir()
	}
}

// LogDir returns the directory for application log files:
//   - Linux:   $XDG_STATE_HOME/goremote, falling back to ~/.local/state/goremote
//   - macOS:   ~/Library/Logs/goremote
//   - Windows: %LocalAppData%/goremote/Logs
func (p pathsImpl) LogDir() (string, error) {
	switch runtime.GOOS {
	case "linux":
		if v := os.Getenv("XDG_STATE_HOME"); v != "" {
			return filepath.Join(v, AppName), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("platform: resolve log dir: %w", err)
		}
		return filepath.Join(home, ".local", "state", AppName), nil
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("platform: resolve log dir: %w", err)
		}
		return filepath.Join(home, "Library", "Logs", AppName), nil
	default:
		cache, err := p.CacheDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(cache, "Logs"), nil
	}
}
