//go:build !windows

package worlock

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// TestContentionReportsErrLockHeld pins the distinction the CLI's error
// message depends on: a lock that is genuinely held has to be
// identifiable as such, so "wait for the other command to finish" is
// only ever printed when there really is another command.
func TestContentionReportsErrLockHeld(t *testing.T) {
	dir := t.TempDir()
	h1, err := Acquire(dir)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	defer h1.Release()

	_, err = Acquire(dir)
	if err == nil {
		t.Fatal("expected the second Acquire to fail while the lock is held")
	}
	if !errors.Is(err, ErrLockHeld) {
		t.Errorf("contention should report ErrLockHeld, got %v", err)
	}
}

// TestAcquireFallsBackToReadOnlyLockFile covers the second admin account:
// the lock file already exists, created 0644 by somebody else, so opening
// it read-write is denied. flock does not need write access -- and this
// package never writes to the file -- so the lock must still be taken
// rather than the command failing.
//
// Chmod cannot express "denied" to root, which bypasses the permission
// bits entirely, so this can only run unprivileged.
func TestAcquireFallsBackToReadOnlyLockFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file permission bits, so the denial cannot be staged")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, fileName)
	if err := os.WriteFile(path, nil, 0o444); err != nil {
		t.Fatalf("staging a read-only lock file: %v", err)
	}

	// Guard the premise: if this open ever starts succeeding, the test
	// below stops testing the fallback and silently passes for the
	// wrong reason.
	if f, err := os.OpenFile(path, os.O_RDWR, 0o644); err == nil {
		f.Close()
		t.Skip("this filesystem does not enforce the read-only bit")
	} else if !errors.Is(err, fs.ErrPermission) {
		t.Skipf("unexpected error staging the denial: %v", err)
	}

	h, err := Acquire(dir)
	if err != nil {
		t.Fatalf("Acquire on a lock file owned by another account: %v", err)
	}
	if h == nil {
		t.Fatal("expected a real handle from the read-only fallback")
	}
	h.Release()
}

// TestAcquireStillExcludesThroughTheReadOnlyFallback makes sure the
// fallback did not quietly trade mutual exclusion for convenience: a
// read-only descriptor still has to exclude a second caller.
func TestAcquireStillExcludesThroughTheReadOnlyFallback(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file permission bits, so the denial cannot be staged")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, fileName)
	if err := os.WriteFile(path, nil, 0o444); err != nil {
		t.Fatalf("staging a read-only lock file: %v", err)
	}
	if f, err := os.OpenFile(path, os.O_RDWR, 0o644); err == nil {
		f.Close()
		t.Skip("this filesystem does not enforce the read-only bit")
	}

	h1, err := Acquire(dir)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	defer h1.Release()

	if _, err := Acquire(dir); !errors.Is(err, ErrLockHeld) {
		t.Errorf("the read-only fallback must still exclude a second caller, got %v", err)
	}
}
