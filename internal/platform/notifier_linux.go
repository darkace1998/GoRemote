//go:build linux

package platform

import "os/exec"

func osNotify(title, body string) error {
	path, err := exec.LookPath("notify-send")
	if err != nil {
		return ErrNotifierUnavailable
	}
	// #nosec G204 -- path is resolved from a fixed notify-send executable name and arguments are passed directly.
	if err := exec.Command(path, title, body).Run(); err != nil {
		return ErrNotifierUnavailable
	}
	return nil
}
