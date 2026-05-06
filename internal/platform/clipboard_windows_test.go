//go:build windows

package platform

import "testing"

// TestWriteTextUsesInputPipe is a compile-time smoke check that clipboard_windows.go
// compiles. The runtime behaviour (piping via $input | Set-Clipboard) is
// exercised by the existing TestClipboardRoundTrip when run on Windows.
func TestWriteTextCompiles(t *testing.T) {
	t.Skip("runtime test requires a live Windows clipboard; compile check passes")
}
