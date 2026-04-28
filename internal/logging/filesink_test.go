package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileSink_WritesAndAppends(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.log")
	s, err := OpenFileSink(p, 0)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := s.Write([]byte("hello\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = s.Close()

	s2, err := OpenFileSink(p, 0)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if _, err := s2.Write([]byte("world\n")); err != nil {
		t.Fatalf("write2: %v", err)
	}
	_ = s2.Close()

	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(got), "hello") || !strings.Contains(string(got), "world") {
		t.Errorf("file content = %q", got)
	}
}

func TestFileSink_RotatesAtThreshold(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.log")
	s, err := OpenFileSink(p, 32) // tiny threshold
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()

	// Write more than 32 bytes to force rotation.
	if _, err := s.Write([]byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := s.Write([]byte("after-rotation\n")); err != nil {
		t.Fatalf("write2: %v", err)
	}

	if _, err := os.Stat(p + ".1"); err != nil {
		t.Errorf("rotated file missing: %v", err)
	}
	cur, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read current: %v", err)
	}
	if !strings.Contains(string(cur), "after-rotation") {
		t.Errorf("current file should contain post-rotation write, got %q", cur)
	}
	if strings.Contains(string(cur), "aaaaaaaaaa") {
		t.Errorf("current file should not contain pre-rotation bytes")
	}
}

func TestFileSink_RotatesOnlyOneGeneration(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.log")
	s, err := OpenFileSink(p, 16)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()

	// Force several rotations.
	for i := 0; i < 5; i++ {
		if _, err := s.Write([]byte("xxxxxxxxxxxxxxxxxxxxxxxx\n")); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}

	// Only .1 should exist; no .2/.3.
	for _, suf := range []string{".2", ".3", ".4"} {
		if _, err := os.Stat(p + suf); err == nil {
			t.Errorf("unexpected archive %s", p+suf)
		}
	}
}

func TestFileSink_CloseRejectsLaterWrites(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.log")
	s, err := OpenFileSink(p, 0)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := s.Write([]byte("after-close")); err == nil {
		t.Errorf("expected write-after-close error")
	}
}
