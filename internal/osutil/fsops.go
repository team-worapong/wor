package osutil

import (
	"fmt"
	"os"
	"path/filepath"
)

// EnsureDir creates dir (and parents) if missing, escalating privileges
// on Unix if the direct attempt fails because of permissions. This
// mirrors lib/paths.sh ensure_wor_dir().
func EnsureDir(dir string) error {
	if _, err := os.Stat(dir); err == nil {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err == nil {
		return nil
	}
	if err := ensureDirPrivileged(dir); err != nil {
		return fmt.Errorf("cannot create directory %s: %w", dir, err)
	}
	return nil
}

// WriteFileAtomic writes data to path by first writing to a temporary
// file in path's own directory, then renaming it into place. Rename is
// atomic on POSIX filesystems (and same-volume on Windows), so a crash
// or kill partway through writing can never leave path half-written --
// any reader either sees the old content in full or the new content in
// full, never a mix. The temp file is deliberately created in the same
// directory as path (not os.TempDir()) so the final rename can never
// cross a filesystem boundary, which would silently turn "atomic
// rename" into a non-atomic copy on some platforms/setups.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := EnsureDir(dir); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".wor-tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	renamed := false
	defer func() {
		if !renamed {
			os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	// Sync before rename so the data is actually on disk before the
	// directory entry is retargeted -- otherwise a crash right after
	// rename could still leave path pointing at a file whose content
	// never made it past the page cache.
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, perm); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	renamed = true
	return nil
}

// WriteFilePrivileged writes data to path, creating parent directories
// as needed and escalating privileges on Unix if a direct write fails.
// Mirrors lib/paths.sh write_file_privileged(). Uses WriteFileAtomic for
// the unprivileged attempt so a crash mid-write can't corrupt an
// existing file; the privileged fallback does the same atomic
// write+rename, just executed inside the elevated shell (see
// writeFilePrivilegedFallback).
func WriteFilePrivileged(path string, data []byte) error {
	if err := EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	if err := WriteFileAtomic(path, data, 0o644); err == nil {
		return nil
	}
	if err := writeFilePrivilegedFallback(path, data); err != nil {
		return fmt.Errorf("cannot write %s: %w (%s)", path, err, ElevationHint())
	}
	return nil
}

// RemoveFilePrivileged removes path if it exists, escalating on Unix
// when needed. Missing files are not an error (matches the shell
// version's `[[ -e "$path" ]] || return 0` guard).
func RemoveFilePrivileged(path string) error {
	if _, err := os.Lstat(path); err != nil {
		return nil
	}
	if err := os.Remove(path); err == nil {
		return nil
	}
	if err := removeFilePrivilegedFallback(path); err != nil {
		return fmt.Errorf("cannot remove %s: %w (%s)", path, err, ElevationHint())
	}
	return nil
}

// IsWritableDir reports whether the process can write into dir.
func IsWritableDir(dir string) bool {
	probe := filepath.Join(dir, ".wor-write-test")
	f, err := os.OpenFile(probe, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(probe)
	return true
}
