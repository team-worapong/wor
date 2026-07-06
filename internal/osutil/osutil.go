// Package osutil provides cross-platform helpers used throughout wor:
// binary discovery, elevation checks, and OS naming. Platform-specific
// behavior lives in osutil_unix.go and osutil_windows.go (build-tagged).
package osutil

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// OSName returns a short, human-friendly OS name matching the old
// bash `os_name()` helper (macOS / Linux / Windows).
func OSName() string {
	switch runtime.GOOS {
	case "darwin":
		return "macOS"
	case "windows":
		return "Windows"
	case "linux":
		return "Linux"
	default:
		return runtime.GOOS
	}
}

func IsWindows() bool { return runtime.GOOS == "windows" }
func IsMacOS() bool   { return runtime.GOOS == "darwin" }
func IsLinux() bool   { return runtime.GOOS == "linux" }

// LinuxDistro returns a human-friendly Linux distribution label parsed
// from /etc/os-release -- the same file scripts/install.sh sources on
// the shell side for its own OS-family detection. Prefers PRETTY_NAME
// (e.g. "Debian GNU/Linux 13 (trixie)"), falling back to the bare ID
// field (e.g. "debian") if PRETTY_NAME is missing. ok is false on any
// non-Linux OS, or if the file is missing/unreadable/has neither
// field.
func LinuxDistro() (name string, ok bool) {
	if runtime.GOOS != "linux" {
		return "", false
	}
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "", false
	}
	return parseOSRelease(data)
}

// parseOSRelease is split out from LinuxDistro so the parsing logic
// itself can be unit tested against fixed file content, independent of
// runtime.GOOS/the real /etc/os-release on whatever machine tests run.
func parseOSRelease(data []byte) (name string, ok bool) {
	var id string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if v, found := strings.CutPrefix(line, "PRETTY_NAME="); found {
			return strings.Trim(v, `"`), true
		}
		if v, found := strings.CutPrefix(line, "ID="); found {
			id = strings.Trim(v, `"`)
		}
	}
	if id != "" {
		return id, true
	}
	return "", false
}

// IsDebianFamily reports whether the current Linux distro is Debian or
// a derivative (Ubuntu, etc.), mirroring scripts/install.sh's own
// id_like_has("debian") check (ID and the possibly multi-value
// ID_LIKE field) so both sides of the project agree on what counts as
// "Debian family". Always false on non-Linux.
func IsDebianFamily() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	return isDebianFamilyFields(linuxOSReleaseIDFields())
}

func linuxOSReleaseIDFields() (id string, idLike []string) {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "", nil
	}
	return parseOSReleaseIDFields(data)
}

// parseOSReleaseIDFields is split out from linuxOSReleaseIDFields the
// same way parseOSRelease is split from LinuxDistro above -- so it's
// unit-testable against fixed content.
func parseOSReleaseIDFields(data []byte) (id string, idLike []string) {
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if v, found := strings.CutPrefix(line, "ID="); found {
			id = strings.Trim(v, `"`)
		}
		if v, found := strings.CutPrefix(line, "ID_LIKE="); found {
			idLike = strings.Fields(strings.Trim(v, `"`))
		}
	}
	return id, idLike
}

func isDebianFamilyFields(id string, idLike []string) bool {
	if id == "debian" {
		return true
	}
	for _, v := range idLike {
		if v == "debian" {
			return true
		}
	}
	return false
}

// Exists reports whether a command is resolvable on PATH.
func Exists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// Which returns the resolved path of a command on PATH, or "" if not found.
func Which(name string) string {
	p, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	return p
}

// FindBinary mirrors lib/os.sh find_binary(): prefer PATH resolution,
// then fall back to a list of known absolute install locations.
func FindBinary(name string, fallbacks ...string) string {
	if p := Which(name); p != "" {
		return p
	}
	for _, p := range fallbacks {
		if p == "" {
			continue
		}
		if IsExecutableFile(p) {
			return p
		}
	}
	return ""
}

// IsExecutableFile reports whether path exists and is runnable. On Unix
// this checks the executable permission bits; on Windows any regular
// file is considered runnable (Windows has no exec bit).
func IsExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return execCheck(path, info)
}

// FirstLine returns the first non-empty line of a multi-line string,
// trimmed. Used to normalize `--version` output the way the shell
// version piped through `head -1`.
func FirstLine(s string) string {
	for _, line := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

// RunVersion executes `bin` with the given args and returns the first
// line of combined output, ignoring a non-zero exit code (many version
// flags exit non-zero, e.g. some --version implementations).
func RunVersion(bin string, args ...string) string {
	if bin == "" {
		return ""
	}
	out, _ := exec.Command(bin, args...).CombinedOutput()
	line := FirstLine(string(out))
	if line == "" {
		return "installed"
	}
	return line
}

// IsTerminal reports whether f appears to be an interactive terminal,
// as opposed to a pipe, redirect, or regular file. wor has zero
// third-party dependencies by design (see DESIGN.md), so this
// deliberately uses only the standard library's file-mode check rather
// than golang.org/x/term's more precise ioctl-based one -- good enough
// to decide whether `wor service status`/`wor host list` should color
// their output.
func IsTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
