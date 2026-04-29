package logging

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// FileSink is an io.Writer backed by a log file with simple size-based
// rotation. Once the underlying file exceeds MaxBytes after a write, the
// current file is renamed to <path>.1 (replacing any existing .1) and a
// fresh file is opened. There is exactly one rotated generation; older
// archives are dropped.
//
// FileSink is safe for concurrent use.
type FileSink struct {
	path     string
	maxBytes int64

	mu   sync.Mutex
	f    *os.File
	size int64
}

const (
	defaultLogMaxBytes = 10 * 1024 * 1024 // 10 MiB
	logDirPerm         = 0o700
	logFilePerm        = 0o600
)

// OpenFileSink creates the parent directory if needed and opens (or
// appends to) the log file at path. If maxBytes <= 0 a 10 MiB default
// is used.
func OpenFileSink(path string, maxBytes int64) (*FileSink, error) {
	if path == "" {
		return nil, errors.New("logging: empty file sink path")
	}
	path = filepath.Clean(path)
	if err := os.MkdirAll(filepath.Dir(path), logDirPerm); err != nil {
		return nil, err
	}
	// #nosec G304 -- the log path is application-configured and opened directly without shell evaluation.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, logFilePerm)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if maxBytes <= 0 {
		maxBytes = defaultLogMaxBytes
	}
	return &FileSink{path: path, maxBytes: maxBytes, f: f, size: st.Size()}, nil
}

// Path returns the log file path on disk.
func (s *FileSink) Path() string { return s.path }

// Write appends p to the underlying file and rotates if the size
// threshold is crossed.
func (s *FileSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return 0, errors.New("logging: file sink closed")
	}
	n, err := s.f.Write(p)
	s.size += int64(n)
	if err != nil {
		return n, err
	}
	if s.size >= s.maxBytes {
		_ = s.rotateLocked()
	}
	return n, nil
}

func (s *FileSink) rotateLocked() error {
	if err := s.f.Close(); err != nil {
		return err
	}
	rotated := s.path + ".1"
	_ = os.Remove(rotated)
	if err := os.Rename(s.path, rotated); err != nil {
		// Try to recover: reopen original even if rename failed.
		// #nosec G304 -- the log path is application-configured and opened directly without shell evaluation.
		f, oerr := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, logFilePerm)
		if oerr == nil {
			s.f = f
			st, _ := f.Stat()
			if st != nil {
				s.size = st.Size()
			}
		}
		return err
	}
	// #nosec G304 -- the log path is application-configured and opened directly without shell evaluation.
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, logFilePerm)
	if err != nil {
		return err
	}
	s.f = f
	s.size = 0
	return nil
}

// Close flushes and closes the underlying file. Subsequent Writes return
// an error.
func (s *FileSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return nil
	}
	err := s.f.Close()
	s.f = nil
	return err
}

// MultiWriter wraps io.MultiWriter so callers don't need to import "io"
// just to compose stderr + file output.
func MultiWriter(ws ...io.Writer) io.Writer { return io.MultiWriter(ws...) }
