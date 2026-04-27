//go:build darwin

package platform

import (
	"fmt"
	"os/exec"
	"strings"
)

func NewClipboard() Clipboard { return clipboardImpl{} }

type clipboardImpl struct{}

func (clipboardImpl) ReadText() (string, error) {
	out, err := exec.Command("pbpaste").Output()
	if err != nil {
		return "", fmt.Errorf("%w: pbpaste: %v", ErrClipboardUnavailable, err)
	}
	return string(out), nil
}

func (clipboardImpl) WriteText(s string) error {
	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(s)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: pbcopy: %v", ErrClipboardUnavailable, err)
	}
	return nil
}
