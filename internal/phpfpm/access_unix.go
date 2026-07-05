//go:build !windows

package phpfpm

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"syscall"

	"wor/internal/osutil"
)

// ownerGroupName returns the group name that owns dir (the group of
// dir's existing on-disk owner, e.g. the deploying user's primary
// group), so a pool user can be added to it without disturbing dir's
// existing ownership.
func ownerGroupName(dir string) (string, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return "", err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", fmt.Errorf("cannot determine owner of %s", dir)
	}
	g, err := user.LookupGroupId(fmt.Sprintf("%d", stat.Gid))
	if err != nil {
		return "", fmt.Errorf("cannot resolve group id %d for %s: %w", stat.Gid, dir, err)
	}
	return g.Name, nil
}

// GrantGroupAccess adds poolUser to docRoot's existing owner group and
// grants that group read+traverse access to every file/dir under
// docRoot, WITHOUT chowning anything -- docRoot's existing ownership
// (e.g. the deploying user) is left untouched; only its permission bits
// gain a group-read bit. This is the per-service-pool equivalent of the
// "add the web server user to a shared group" convention traditional
// single-pool PHP hosting uses. Returns the group name poolUser was
// added to, so callers can persist it (domainmodel.Service.PHPPoolGroup)
// without repeating the owner lookup later.
func GrantGroupAccess(docRoot, poolUser string) (string, error) {
	group, err := ownerGroupName(docRoot)
	if err != nil {
		return "", err
	}
	if err := addUserToGroup(poolUser, group); err != nil {
		return "", err
	}
	// g+rX: read everywhere, +x only where already executable (i.e.
	// directories and already-executable files) -- capital X, not x,
	// so this never makes a plain data file spuriously executable.
	cmd, cerr := osutil.SudoCommand("chmod", "-R", "g+rX", docRoot)
	if cerr != nil {
		return "", cerr
	}
	if out, runErr := cmd.CombinedOutput(); runErr != nil {
		return "", fmt.Errorf("cannot grant group access to %s (%s): %w: %s", docRoot, osutil.ElevationHint(), runErr, strings.TrimSpace(string(out)))
	}
	return group, nil
}

func addUserToGroup(userName, group string) error {
	var cmd *exec.Cmd
	var err error
	if osutil.IsMacOS() {
		cmd, err = osutil.SudoCommand("dseditgroup", "-o", "edit", "-a", userName, "-t", "user", group)
	} else {
		cmd, err = osutil.SudoCommand("usermod", "-aG", group, userName)
	}
	if err != nil {
		return err
	}
	out, runErr := cmd.CombinedOutput()
	if runErr != nil {
		return fmt.Errorf("cannot add %s to group %s (%s): %w: %s", userName, group, osutil.ElevationHint(), runErr, strings.TrimSpace(string(out)))
	}
	return nil
}
