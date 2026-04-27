//go:build darwin

package platform

import (
	"fmt"
	"os/exec"
	"strings"
)

func osNotify(title, body string) error {
	path, err := exec.LookPath("osascript")
	if err != nil {
		return ErrNotifierUnavailable
	}
	script := fmt.Sprintf(`display notification %q with title %q`, escapeAppleScript(body), escapeAppleScript(title))
	if err := exec.Command(path, "-e", script).Run(); err != nil {
		return ErrNotifierUnavailable
	}
	return nil
}

func escapeAppleScript(s string) string {
	return strings.ReplaceAll(s, `"`, `\"`)
}
