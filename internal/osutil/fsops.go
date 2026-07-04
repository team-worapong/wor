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

// WriteFilePrivileged writes data to path, creating parent directories
// as needed and escalating privileges on Unix if a direct write fails.
// Mirrors lib/paths.sh write_file_privileged().
func WriteFilePrivileged(path string, data []byte) error {
	if err := EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err == nil {
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
