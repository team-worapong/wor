package worlock

import (
	"os"
	"testing"
)

func TestAcquireRelease(t *testing.T) {
	dir := t.TempDir()
	h, err := Acquire(dir)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	h.Release()
}

func TestAcquireContention(t *testing.T) {
	dir := t.TempDir()
	h1, err := Acquire(dir)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	defer h1.Release()

	if _, err := Acquire(dir); err == nil {
		t.Fatal("expected second Acquire to fail while the first lock is still held")
	}
}

func TestAcquireAfterRelease(t *testing.T) {
	dir := t.TempDir()
	h1, err := Acquire(dir)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	h1.Release()

	h2, err := Acquire(dir)
	if err != nil {
		t.Fatalf("second Acquire after release: %v", err)
	}
	h2.Release()
}

func TestReleaseNilHandleDoesNotPanic(t *testing.T) {
	var h *Handle
	h.Release()
}

// Acquire must not create worHome as a side effect: `wor setup` passes
// in whatever was previously configured (or a not-yet-confirmed
// default) before it has actually resolved/created the real WOR_HOME,
// so locking a path that doesn't exist yet should be a silent no-op,
// not a directory-creating side effect.
func TestAcquireOnMissingDirIsNoopNotError(t *testing.T) {
	dir := t.TempDir() + "/does-not-exist-yet"
	h, err := Acquire(dir)
	if err != nil {
		t.Fatalf("Acquire on missing dir: %v", err)
	}
	if h != nil {
		t.Errorf("expected a nil handle for a missing WOR_HOME, got %+v", h)
	}
	if _, statErr := os.Stat(dir); statErr == nil {
		t.Error("Acquire must not create worHome as a side effect")
	}
	h.Release() // must not panic even though h is nil
}

func TestAcquireEmptyWorHomeIsNoop(t *testing.T) {
	h, err := Acquire("")
	if err != nil {
		t.Fatalf("Acquire(\"\"): %v", err)
	}
	if h != nil {
		t.Errorf("expected a nil handle for an empty WOR_HOME, got %+v", h)
	}
}
