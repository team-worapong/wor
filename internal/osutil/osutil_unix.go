//go:build !windows

package osutil

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
)

// SupportsSymlinks is true on Unix; used by host providers to decide
// between symlink-based sites-enabled directories and copy-based
// equivalents on platforms without unprivileged symlink support.
const SupportsSymlinks = true

// IsRoot reports whether the current process is running as root.
func IsRoot() bool {
	return os.Geteuid() == 0
}

// IsElevated is the cross-platform name for "has privileged filesystem
// access"; on Unix this is simply IsRoot.
func IsElevated() bool { return IsRoot() }

// IsSudoElevated reports whether the current process was launched via
// `sudo` (as opposed to being logged in as root directly, e.g. on a
// server with no other user account). sudo sets SUDO_USER in the child
// process environment; a direct root login never sets it. wor uses
// this to refuse to run as `sudo wor ...` while still allowing genuine
// root-only environments to work normally.
func IsSudoElevated() bool {
	return IsRoot() && os.Getenv("SUDO_USER") != ""
}

var (
	elevationConfirmed bool
	elevationDeclined  bool
	elevationPrompt    func(reason string) bool
)

// SetElevationPrompt registers the callback used to ask the user for
// confirmation the first time (per process) a command needs to
// escalate via sudo. cliapp wires this to an interactive y/n prompt in
// New(). If never registered (e.g. in tests), escalation proceeds
// without asking, preserving the old silent behavior.
func SetElevationPrompt(fn func(reason string) bool) {
	elevationPrompt = fn
}

// confirmElevation asks at most once per process: a "yes" is
// remembered so later privileged calls in the same command don't ask
// again, and a "no" is remembered too, so the rest of the command
// fails fast instead of re-prompting for every subsequent privileged
// operation it happens to attempt.
func confirmElevation(reason string) error {
	if elevationConfirmed {
		return nil
	}
	if elevationDeclined {
		return errors.New("elevation declined; cancelled")
	}
	if elevationPrompt != nil && !elevationPrompt(reason) {
		elevationDeclined = true
		return errors.New("elevation declined; cancelled")
	}
	elevationConfirmed = true
	return nil
}

// SudoCommand builds an *exec.Cmd that runs `name args...` directly if
// already root, or prefixes it with `sudo` otherwise, asking for
// confirmation (once per process) the first time escalation is
// actually needed. This mirrors lib/os.sh sudo_cmd(), plus the
// confirm-once gate.
func SudoCommand(name string, args ...string) (*exec.Cmd, error) {
	if IsRoot() {
		return exec.Command(name, args...), nil
	}
	if err := confirmElevation(fmt.Sprintf("run '%s' with elevated (sudo) privileges", name)); err != nil {
		return nil, err
	}
	full := append([]string{name}, args...)
	return exec.Command("sudo", full...), nil
}

// RunPrivileged runs a command with elevation applied as needed and
// returns combined output plus error.
func RunPrivileged(name string, args ...string) ([]byte, error) {
	cmd, err := SudoCommand(name, args...)
	if err != nil {
		return nil, err
	}
	return cmd.CombinedOutput()
}

// ElevationHint is printed when a privileged operation cannot proceed.
func ElevationHint() string {
	return "Re-run with sudo, or as a user that can sudo."
}

func execCheck(path string, info os.FileInfo) bool {
	return info.Mode()&0o111 != 0
}

// ChownToUser best-effort chowns a path to the given uid/gid (Unix only).
func ChownToUser(path string, uid, gid int) error {
	return os.Chown(path, uid, gid)
}
