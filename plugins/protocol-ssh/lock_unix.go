//go:build unix

package ssh

import (
	"os"

	"golang.org/x/sys/unix"
)

// lockFile acquires an exclusive advisory lock on f.
func lockFile(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_EX)
}

// unlockFile releases any advisory lock held on f.
func unlockFile(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_UN)
}
