//go:build linux

package platform

import "os/exec"

func osNotify(title, body string) error {
	path, err := exec.LookPath("notify-send")
	if err != nil {
		return ErrNotifierUnavailable
	}
	if err := exec.Command(path, title, body).Run(); err != nil {
		return ErrNotifierUnavailable
	}
	return nil
}
