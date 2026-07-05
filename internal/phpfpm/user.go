package phpfpm

import (
	"fmt"
	"os/exec"
	"os/user"
	"strings"

	"wor/internal/osutil"
)

// CurrentUnixUser returns the current process's username and primary
// group name. Used for macOS pools (see docs/services.md's macOS
// isolation caveat), which run as this same unprivileged user rather
// than a dedicated per-service user: Homebrew's php-fpm master runs
// unprivileged too, and an unprivileged master cannot chown a socket to
// (or switch a worker to) a *different* unix user -- only Linux, where
// systemd runs php-fpm as root, can support full per-service user
// isolation.
func CurrentUnixUser() (username, group string, err error) {
	u, err := user.Current()
	if err != nil {
		return "", "", fmt.Errorf("cannot resolve current user: %w", err)
	}
	g, err := user.LookupGroupId(u.Gid)
	if err != nil {
		return "", "", fmt.Errorf("cannot resolve primary group for %s: %w", u.Username, err)
	}
	return u.Username, g.Name, nil
}

// UserExists reports whether name is a valid unix account, via the `id`
// command (present on both Linux and macOS).
func UserExists(name string) bool {
	return exec.Command("id", "-u", name).Run() == nil
}

// EnsureUser creates a dedicated, login-disabled system user for a
// php-fpm pool if one doesn't already exist. Idempotent: calling it
// again for an existing user is a no-op, so service edit/redeploy flows
// can call it unconditionally.
func EnsureUser(name string) error {
	if osutil.IsWindows() {
		return fmt.Errorf("per-service php-fpm pools are not supported on Windows")
	}
	if UserExists(name) {
		return nil
	}
	var cmd *exec.Cmd
	var err error
	if osutil.IsMacOS() {
		cmd, err = osutil.SudoCommand("sysadminctl", "-addUser", name, "-shell", "/usr/bin/false")
	} else {
		cmd, err = osutil.SudoCommand("useradd", "--system", "--no-create-home", "--shell", "/usr/sbin/nologin", name)
	}
	if err != nil {
		return err
	}
	out, runErr := cmd.CombinedOutput()
	if runErr != nil {
		return fmt.Errorf("cannot create pool user %s (%s): %w: %s", name, osutil.ElevationHint(), runErr, strings.TrimSpace(string(out)))
	}
	return nil
}

// RemoveUser deletes a pool's dedicated system user. A missing user is
// not an error, matching systemd.RemoveUnit's tolerance for cleaning up
// a partially-created service.
func RemoveUser(name string) error {
	if osutil.IsWindows() || !UserExists(name) {
		return nil
	}
	var cmd *exec.Cmd
	var err error
	if osutil.IsMacOS() {
		cmd, err = osutil.SudoCommand("sysadminctl", "-deleteUser", name)
	} else {
		cmd, err = osutil.SudoCommand("userdel", name)
	}
	if err != nil {
		return err
	}
	out, runErr := cmd.CombinedOutput()
	if runErr != nil {
		return fmt.Errorf("cannot remove pool user %s (%s): %w: %s", name, osutil.ElevationHint(), runErr, strings.TrimSpace(string(out)))
	}
	return nil
}
