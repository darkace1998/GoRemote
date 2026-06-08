package logging

import (
	"errors"
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

// errCloseFile wraps a fileWriter but returns a configurable error from Close.
type errCloseFile struct {
	fileWriter
	closeErr error
}

func (e *errCloseFile) Close() error { return e.closeErr }

// TestFileSink_RotateCloseFailureClearsFile verifies that when rotateLocked
// fails to close the current file, s.f is set to nil so subsequent Writes
// return a clean "file sink closed" error instead of operating on a broken fd.
func TestFileSink_RotateCloseFailureClearsFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.log")
	s, err := OpenFileSink(p, 16) // small threshold to trigger rotation
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	// Inject a closer that always fails.
	s.f = &errCloseFile{fileWriter: s.f, closeErr: errors.New("injected close error")}

	// Trigger rotation by setting size above threshold and calling rotateLocked.
	s.mu.Lock()
	s.size = 32
	// Hold onto the actual file to close it later on Windows
	originalFile, isFile := s.f.(*errCloseFile).fileWriter.(*os.File)
	rotErr := s.rotateLocked()
	s.mu.Unlock()

	if rotErr == nil {
		t.Fatal("expected rotateLocked to return the injected error")
	}

	s.mu.Lock()
	fNil := s.f == nil
	s.mu.Unlock()

	if !fNil {
		t.Error("s.f should be nil after a close failure in rotateLocked")
	}

	// Subsequent Write must return an error, not operate on a broken fd.
	if _, werr := s.Write([]byte("after-fail")); werr == nil {
		t.Error("Write after close failure should return an error")
	}

	// Actually close it so TempDir cleanup on Windows works
	if isFile && originalFile != nil {
		_ = originalFile.Close()
	}
}
