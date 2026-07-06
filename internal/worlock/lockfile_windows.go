//go:build windows

package worlock

import (
	"os"
	"syscall"
	"unsafe"
)

// Windows has no flock, but the same exclusive, non-blocking,
// handle-lifetime-scoped lock is available from the standard library
// via the raw kernel32 LockFileEx/UnlockFileEx calls (loaded through
// syscall.NewLazyDLL, not a third-party package -- consistent with
// this project's zero-dependency policy). This keeps Windows a real,
// fully-locked platform rather than a best-effort fallback: like
// flock, the OS releases the lock the moment the process's handle
// table goes away, including on a hard crash.
var (
	modkernel32      = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx   = modkernel32.NewProc("LockFileEx")
	procUnlockFileEx = modkernel32.NewProc("UnlockFileEx")
)

const (
	lockfileFailImmediately = 0x00000001
	lockfileExclusiveLock   = 0x00000002
)

func lockFile(f *os.File) error {
	var overlapped syscall.Overlapped
	r, _, err := procLockFileEx.Call(
		f.Fd(),
		uintptr(lockfileFailImmediately|lockfileExclusiveLock),
		0,
		1, 0,
		uintptr(unsafe.Pointer(&overlapped)),
	)
	if r == 0 {
		return err
	}
	return nil
}

func unlockFile(f *os.File) error {
	var overlapped syscall.Overlapped
	r, _, err := procUnlockFileEx.Call(
		f.Fd(),
		0,
		1, 0,
		uintptr(unsafe.Pointer(&overlapped)),
	)
	if r == 0 {
		return err
	}
	return nil
}
