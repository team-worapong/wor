//go:build !windows

package worlock

import (
	"os"
	"syscall"
)

// lockFile takes a non-blocking exclusive flock on f. LOCK_NB means
// this returns immediately (EWOULDBLOCK) instead of waiting if another
// process already holds the lock, matching Acquire's documented
// fail-fast behavior.
func lockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

func unlockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
