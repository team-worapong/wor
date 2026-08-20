//go:build !windows

package osutil

import (
	"bytes"
	"fmt"
	"os"
	"os/user"
	"strconv"
	"strings"
	"syscall"
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

func readFilePrivilegedFallback(path string) ([]byte, error) {
	cmd, err := SudoCommand("cat", path)
	if err != nil {
		return nil, err
	}
	// Only stdout is captured; stderr keeps streaming so sudo's own
	// prompt and any error text still reach the operator.
	cmd.Stderr = os.Stderr
	return cmd.Output()
}

// FileOwner returns the numeric uid/gid owning path. Symlinks are
// followed (Stat, not Lstat): WOR_HOME is allowed to be a symlink, and
// what matters is who owns the directory it points at.
func FileOwner(path string) (uid, gid int, err error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, 0, err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, fmt.Errorf("cannot read ownership of %s on this platform", path)
	}
	return int(st.Uid), int(st.Gid), nil
}

// Chown sets path's owner, escalating only if the direct call is
// refused. Running as the owner already -- the common case when an
// operator invokes wor themselves -- never prompts.
func Chown(path string, uid, gid int) error {
	if err := os.Chown(path, uid, gid); err == nil {
		return nil
	}
	cmd, err := SudoCommand("chown", fmt.Sprintf("%d:%d", uid, gid), path)
	if err != nil {
		return err
	}
	return cmd.Run()
}

func removeFilePrivilegedFallback(path string) error {
	cmd, err := SudoCommand("rm", "-f", path)
	if err != nil {
		return err
	}
	return cmd.Run()
}

// ClaimOwnership makes dir writable by whichever user is currently
// running wor, escalating via `sudo chown` if it isn't already. This
// exists specifically for WOR_HOME's own top-level directories (see
// cliapp.ensureRootDirs and worlock.Acquire): those two places are the
// ones that hit "permission denied" when WOR_HOME (default /opt/wor,
// under root-owned /opt) either got created root-owned by
// ensureDirPrivileged's own `sudo mkdir`, or was simply left
// root-owned by something else entirely -- e.g. a prior install of the
// old shell-script wor-cli, which is the actual real-world case this
// was first found from.
//
// Deliberately NOT recursive: a WOR_HOME subtree can legitimately be
// owned by a different, per-service system user on purpose (see the
// per-service PHP-FPM isolation design under domains/<domain>/<service>),
// and a recursive chown here would silently undo that every time this
// runs. Callers pass one specific directory at a time -- the ones
// ensureRootDirs() itself creates/manages directly -- never an
// arbitrary subtree.
//
// The common case, where dir is already writable (freshly created
// unprivileged, or already fixed by an earlier call), is a silent
// no-op: no sudo prompt at all.
func ClaimOwnership(dir string) error {
	if IsWritableDir(dir) {
		return nil
	}
	uid, gid := os.Getuid(), os.Getgid()
	cmd, err := SudoCommand("chown", fmt.Sprintf("%d:%d", uid, gid), dir)
	if err != nil {
		return err
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cannot chown %s to the current user: %w (%s)", dir, err, ElevationHint())
	}
	return nil
}

// EnsureOwnedBy makes path -- a directory or a single file -- belong to
// username, the one account wor's state is supposed to be owned by,
// whoever happens to be invoking wor at the time.
//
// This exists because ClaimOwnership's "chown it to me" rule only holds
// while there is exactly one admin account. With two, each one takes the
// directories away from the other on every run, one sudo prompt at a
// time, and neither can write what the other created. Naming the owner
// explicitly ends that: the answer stops depending on who is logged in.
//
// The self-healing ClaimOwnership was written for is kept -- a directory
// that EnsureDir had to create through `sudo mkdir`, and so came out
// root-owned, is handed to username here just the same. What changes is
// only *which* account it is handed to.
//
// An empty username means no canonical account is configured, and the
// historical behaviour applies unchanged. That is what keeps this safe
// to ship ahead of the account existing: nothing moves until the setting
// is set.
//
// Ownership, not writability, is the test. ClaimOwnership probes whether
// it can write and stops if it can, which cannot distinguish "correctly
// owned" from "owned by someone else who left it group-writable" -- the
// exact case this is here to catch.
func EnsureOwnedBy(path, username string) error {
	if username == "" {
		return ClaimOwnership(path)
	}
	u, err := user.Lookup(username)
	if err != nil {
		return fmt.Errorf("cannot resolve the configured wor account %q: %w", username, err)
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return fmt.Errorf("account %q has a non-numeric uid %q", username, u.Uid)
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return fmt.Errorf("account %q has a non-numeric gid %q", username, u.Gid)
	}

	curUID, curGID, err := FileOwner(path)
	if err != nil {
		return err
	}
	if curUID == uid && curGID == gid {
		return nil
	}

	cmd, cerr := SudoCommand("chown", "-h", fmt.Sprintf("%d:%d", uid, gid), path)
	if cerr != nil {
		return cerr
	}
	if runErr := cmd.Run(); runErr != nil {
		return fmt.Errorf("cannot chown %s to %s: %w (%s)", path, username, runErr, ElevationHint())
	}
	return nil
}
