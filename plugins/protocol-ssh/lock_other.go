//go:build !unix

package ssh

import "os"

// lockFile is a best-effort no-op on non-unix platforms.
func lockFile(_ *os.File) error { return nil }

// unlockFile is a best-effort no-op on non-unix platforms.
func unlockFile(_ *os.File) error { return nil }
