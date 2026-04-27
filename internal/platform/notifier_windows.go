//go:build windows

package platform

import (
	"fmt"
	"os/exec"
	"os/user"
)

func osNotify(title, body string) error {
	path, err := exec.LookPath("msg")
	if err != nil {
		return ErrNotifierUnavailable
	}
	u, err := user.Current()
	if err != nil {
		return ErrNotifierUnavailable
	}
	if err := exec.Command(path, u.Username, fmt.Sprintf("%s: %s", title, body)).Run(); err != nil {
		return ErrNotifierUnavailable
	}
	return nil
}
