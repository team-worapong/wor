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
