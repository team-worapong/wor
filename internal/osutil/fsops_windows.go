//go:build windows

package osutil

import (
	"errors"
	"os"
)

// Windows has no transparent privilege-escalation mechanism for an
// already-running process (no sudo). If a direct write/mkdir/remove
// fails here, the caller must be re-launched from an elevated
// (Administrator) console; we surface that via ElevationHint().

func ensureDirPrivileged(_ string) error {
	return errors.New("directory not writable; " + ElevationHint())
}

func writeFilePrivilegedFallback(path string, data []byte) error {
	// One retry in case the failure was transient (e.g. AV lock); if it
	// still fails, tell the caller to elevate. Uses the same atomic
	// write+rename as the primary (unprivileged) attempt in
	// WriteFilePrivileged, for the same corruption-on-crash reason.
	if err := WriteFileAtomic(path, data, 0o644); err == nil {
		return nil
	}
	return errors.New("file not writable; " + ElevationHint())
}

func removeFilePrivilegedFallback(path string) error {
	if err := os.Remove(path); err == nil {
		return nil
	}
	return errors.New("file not removable; " + ElevationHint())
}

// ClaimOwnership is a no-op on Windows: there is no sudo-style
// escalation to chown a directory mid-process (see SudoCommand above),
// and Windows ACLs work differently from POSIX uid/gid ownership
// anyway. If WOR_HOME genuinely isn't writable here, that surfaces
// through the normal EnsureDir/WriteFilePrivileged ElevationHint path
// instead, telling the user to re-open their terminal as
// Administrator.
func ClaimOwnership(_ string) error {
	return nil
}
