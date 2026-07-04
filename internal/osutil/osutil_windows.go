//go:build windows

package osutil

import (
	"os"
	"os/exec"
	"strings"
)

// SupportsSymlinks is false by default on Windows: creating symlinks
// requires either Administrator privileges or Developer Mode enabled.
// Callers should treat failures to symlink as non-fatal and fall back
// to copying the file instead.
const SupportsSymlinks = false

// IsRoot has no meaning on Windows; use IsElevated instead. Kept for
// call-site parity with the Unix build.
func IsRoot() bool { return IsElevated() }

// IsElevated detects an elevated (Administrator) console using the
// well-known `net session` probe: only an elevated process can query
// session information, so a zero exit code implies Administrator
// rights. This avoids depending on golang.org/x/sys/windows so the
// project builds with the standard toolchain only.
func IsElevated() bool {
	cmd := exec.Command("net", "session")
	err := cmd.Run()
	return err == nil
}

// IsSudoElevated always reports false on Windows: there is no `sudo`
// concept here, and blocking an elevated (Administrator) console would
// break every privileged operation on this platform, since
// SudoCommand has no way to escalate mid-process on Windows (see
// below). An elevated console is the only supported way to run
// privileged wor commands on Windows.
func IsSudoElevated() bool { return false }

// SetElevationPrompt is a no-op on Windows: SudoCommand never escalates
// here, so there is nothing to confirm before doing so.
func SetElevationPrompt(fn func(reason string) bool) { _ = fn }

// SudoCommand on Windows has no direct sudo equivalent. wor cannot
// transparently elevate an already-running process, so this simply
// returns the plain command; callers should check IsElevated() first
// and instruct the user to re-open their terminal "as Administrator"
// when a privileged path (system hosts file, Program Files, IIS/nginx
// config directories) is not writable.
func SudoCommand(name string, args ...string) (*exec.Cmd, error) {
	return exec.Command(name, args...), nil
}

func RunPrivileged(name string, args ...string) ([]byte, error) {
	cmd, err := SudoCommand(name, args...)
	if err != nil {
		return nil, err
	}
	return cmd.CombinedOutput()
}

func ElevationHint() string {
	return "Re-open your terminal (or PowerShell) as Administrator and re-run this command."
}

func execCheck(path string, _ os.FileInfo) bool {
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, ".exe"),
		strings.HasSuffix(lower, ".bat"),
		strings.HasSuffix(lower, ".cmd"),
		strings.HasSuffix(lower, ".com"):
		return true
	default:
		// Non-executable extension is still considered "present" for
		// detection purposes (e.g. a shell script invoked via an
		// interpreter); wor only uses this for existence checks.
		return true
	}
}

// ChownToUser is a no-op on Windows; ACL management is out of scope
// for wor v1 and Windows file ownership works differently than POSIX
// uid/gid.
func ChownToUser(path string, uid, gid int) error {
	_ = path
	_ = uid
	_ = gid
	return nil
}
