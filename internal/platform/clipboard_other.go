//go:build !darwin && !linux && !windows

package platform

func NewClipboard() Clipboard { return unsupportedClipboard{} }

type unsupportedClipboard struct{}

func (unsupportedClipboard) ReadText() (string, error) { return "", ErrClipboardUnavailable }
func (unsupportedClipboard) WriteText(string) error    { return ErrClipboardUnavailable }
