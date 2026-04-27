package protocol

import (
	"errors"
	"testing"
)

func TestRenderModesDistinct(t *testing.T) {
	seen := map[RenderMode]bool{}
	for _, m := range []RenderMode{RenderTerminal, RenderGraphical, RenderExternal} {
		if m == "" {
			t.Fatalf("empty render mode")
		}
		if seen[m] {
			t.Fatalf("duplicate render mode %q", m)
		}
		seen[m] = true
	}
	if RenderExternal != "external" {
		t.Fatalf("RenderExternal = %q, want %q", RenderExternal, "external")
	}
}

func TestErrUnsupportedDistinct(t *testing.T) {
	if ErrUnsupported == nil {
		t.Fatal("ErrUnsupported is nil")
	}
	if errors.Is(ErrUnsupported, ErrNotSupported) || errors.Is(ErrNotSupported, ErrUnsupported) {
		t.Fatal("ErrUnsupported and ErrNotSupported should be distinct sentinel errors")
	}
	if ErrUnsupported.Error() == "" {
		t.Fatal("ErrUnsupported has empty message")
	}
}
