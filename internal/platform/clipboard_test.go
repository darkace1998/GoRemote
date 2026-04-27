package platform

import (
	"errors"
	"testing"
)

func TestClipboardRoundTrip(t *testing.T) {
	cb := NewClipboard()
	const want = "goremote-clipboard-test"
	if err := cb.WriteText(want); err != nil {
		if errors.Is(err, ErrClipboardUnavailable) {
			t.Skip("no clipboard backend available on this system")
		}
		t.Fatalf("WriteText: %v", err)
	}
	got, err := cb.ReadText()
	if err != nil {
		if errors.Is(err, ErrClipboardUnavailable) {
			t.Skip("no clipboard backend available on this system")
		}
		t.Fatalf("ReadText: %v", err)
	}
	if got != want {
		t.Fatalf("ReadText = %q want %q", got, want)
	}
}
