package platform

import (
	"bytes"
	"strings"
	"testing"
)

func TestNotifierFallbackToStderr(t *testing.T) {
	var buf bytes.Buffer
	n := notifierImpl{
		notify: func(title, body string) error { return ErrNotifierUnavailable },
		stderr: &buf,
	}
	if err := n.Notify("hello", "world"); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "hello") || !strings.Contains(out, "world") {
		t.Errorf("stderr fallback missing message: %q", out)
	}
}

func TestNotifierBackendSuccess(t *testing.T) {
	var buf bytes.Buffer
	called := false
	n := notifierImpl{
		notify: func(title, body string) error { called = true; return nil },
		stderr: &buf,
	}
	if err := n.Notify("t", "b"); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("backend not invoked")
	}
	if buf.Len() != 0 {
		t.Errorf("unexpected stderr output: %q", buf.String())
	}
}

func TestNotifierNilBackendFallsBack(t *testing.T) {
	var buf bytes.Buffer
	n := notifierImpl{notify: nil, stderr: &buf}
	if err := n.Notify("a", "b"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "a") {
		t.Errorf("expected stderr fallback, got %q", buf.String())
	}
}

func TestNewNotifierReturnsInstance(t *testing.T) {
	if n := NewNotifier(); n == nil {
		t.Fatal("NewNotifier returned nil")
	}
}
