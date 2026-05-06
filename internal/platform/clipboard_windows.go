//go:build windows

package platform

import (
	"fmt"
	"os/exec"
	"strings"
)

func NewClipboard() Clipboard { return clipboardImpl{} }

type clipboardImpl struct{}

func (clipboardImpl) ReadText() (string, error) {
	// PowerShell Get-Clipboard is available on Windows 10+
	out, err := exec.Command("powershell", "-NoProfile", "-Command", "Get-Clipboard").Output()
	if err != nil {
		return "", fmt.Errorf("%w: Get-Clipboard: %v", ErrClipboardUnavailable, err)
	}
	// PowerShell adds a trailing CRLF; trim it
	return strings.TrimRight(string(out), "\r\n"), nil
}

func (clipboardImpl) WriteText(s string) error {
	// "$input | Set-Clipboard" reads the process stdin via the PowerShell
	// $input automatic variable and pipes it into Set-Clipboard, which is
	// more reliable than passing the value as a bare -Command argument.
	cmd := exec.Command("powershell", "-NoProfile", "-Command", "$input | Set-Clipboard")
	cmd.Stdin = strings.NewReader(s)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: Set-Clipboard: %v", ErrClipboardUnavailable, err)
	}
	return nil
}
