//go:build !windows

package osutil

import (
	"bytes"
	"strings"
)

func ensureDirPrivileged(dir string) error {
	cmd, err := SudoCommand("mkdir", "-p", dir)
	if err != nil {
		return err
	}
	return cmd.Run()
}

// writeFilePrivilegedFallback writes data to path when the unprivileged
// attempt failed. Previously this ran `sudo tee path`, which writes
// (truncates) path directly -- not atomic, so a crash mid-write here
// could corrupt an existing privileged file (an nginx/apache vhost, a
// systemd unit, /etc/hosts, ...) the same way the unprivileged path
// used to be able to. Fixed the same way: write to a temp file first,
// then rename over the target -- but both steps have to happen inside
// the elevated shell, since only it can write to path's directory at
// all. The whole "cat > tmp && mv tmp path" line is run through `sh -c`
// as one string (not split into argv) for the same reason
// registerPM2Startup (internal/cliapp/run.go) does: it's the only way
// shell operators like `&&` and redirection actually take effect,
// short of reimplementing a shell.
func writeFilePrivilegedFallback(path string, data []byte) error {
	tmp := path + ".wor-tmp"
	script := "cat > " + shellQuote(tmp) + " && mv " + shellQuote(tmp) + " " + shellQuote(path)
	cmd, err := SudoCommand("sh", "-c", script)
	if err != nil {
		return err
	}
	cmd.Stdin = bytes.NewReader(data)
	cmd.Stdout = nil
	return cmd.Run()
}

// shellQuote wraps s in single quotes for safe embedding in a `sh -c`
// script, escaping any single quotes already in s using the standard
// POSIX trick ('\'' -- close the quote, emit an escaped quote, reopen
// the quote). Needed here because writeFilePrivilegedFallback now
// builds a real shell command line instead of passing path as a bare
// argv element (which never went through shell interpretation before).
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func removeFilePrivilegedFallback(path string) error {
	cmd, err := SudoCommand("rm", "-f", path)
	if err != nil {
		return err
	}
	return cmd.Run()
}
