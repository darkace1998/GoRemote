//go:build unix

package ipc_test

import (
	"os"
	"path/filepath"
	"testing"
)

// testSocketPath uses a short /tmp-based directory so Unix socket tests do
// not trip platform pathname limits on Darwin temp directories.
func testSocketPath(t *testing.T) string {
	t.Helper()
	root := "/tmp"
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return filepath.Join(t.TempDir(), "ipc.sock")
	}
	dir, err := os.MkdirTemp(root, "goremote-ipc-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})
	return filepath.Join(dir, "ipc.sock")
}
