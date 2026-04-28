//go:build linux

package platform

import (
	"fmt"
	"os/exec"
	"strings"
)

func NewClipboard() Clipboard { return clipboardImpl{} }

type clipboardImpl struct{}

var readCmds = [][]string{
	{"wl-paste", "--no-newline"},               // Wayland
	{"xclip", "-selection", "clipboard", "-o"}, // X11 xclip
	{"xsel", "--clipboard", "--output"},        // X11 xsel
}

var writeCmds = [][]string{
	{"wl-copy"},                          // Wayland
	{"xclip", "-selection", "clipboard"}, // X11 xclip
	{"xsel", "--clipboard", "--input"},   // X11 xsel
}

func (clipboardImpl) ReadText() (string, error) {
	for _, args := range readCmds {
		out, err := exec.Command(args[0], args[1:]...).Output()
		if err == nil {
			return string(out), nil
		}
	}
	return "", fmt.Errorf("%w: no clipboard tool available (try wl-paste, xclip, or xsel)", ErrClipboardUnavailable)
}

func (clipboardImpl) WriteText(s string) error {
	for _, args := range writeCmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Stdin = strings.NewReader(s)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}
	return fmt.Errorf("%w: no clipboard tool available (try wl-copy, xclip, or xsel)", ErrClipboardUnavailable)
}
