//go:build darwin

package platform

import (
	"os/exec"
)

func osNotify(title, body string) error {
	path, err := exec.LookPath("osascript")
	if err != nil {
		return ErrNotifierUnavailable
	}
	// Pass title and body as osascript argv so they are never interpolated
	// into the script string, avoiding any double-escaping or injection.
	if err := exec.Command(path,
		"-e", "on run argv",
		"-e", "display notification (item 2 of argv) with title (item 1 of argv)",
		"-e", "end run",
		"--", title, body).Run(); err != nil {
		return ErrNotifierUnavailable
	}
	return nil
}
