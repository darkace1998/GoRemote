package powershell

import (
	"errors"
	"os/exec"
	"runtime"
)

// ErrPowerShellNotFound is returned by [discoverBinary] when no PowerShell
// host can be located on PATH.
var ErrPowerShellNotFound = errors.New("powershell: no pwsh or powershell binary found on PATH")

// candidateBinaries returns the ordered list of binary names to look for on
// the current platform. `pwsh` (cross-platform PowerShell 7+) is always
// preferred; `powershell` is only attempted on Windows where it refers to
// Windows PowerShell 5.1.
func candidateBinaries() []string {
	if runtime.GOOS == "windows" {
		return []string{"pwsh.exe", "pwsh", "powershell.exe", "powershell"}
	}
	return []string{"pwsh"}
}

// discoverBinary returns the path to a usable PowerShell host. If override is
// non-empty it is returned verbatim (after a LookPath if it is a bare name);
// otherwise candidateBinaries are tried in order via exec.LookPath and the
// first hit wins.
func discoverBinary(override string) (string, error) {
	if override != "" {
		// If the caller passed an absolute or relative path we trust it
		// verbatim and let exec.Command surface ENOENT later. Otherwise
		// resolve via PATH for a clearer error.
		if containsPathSep(override) {
			return override, nil
		}
		path, err := exec.LookPath(override)
		if err != nil {
			return "", err
		}
		return path, nil
	}
	for _, name := range candidateBinaries() {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", ErrPowerShellNotFound
}

func containsPathSep(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '/' || s[i] == '\\' {
			return true
		}
	}
	return false
}
