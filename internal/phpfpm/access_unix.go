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
// (e.g. the deploying user) is left untouched; only its group and
// permission bits change. This is the per-service-pool equivalent of the
// "add the web server user to a shared group" convention traditional
// single-pool PHP hosting uses. Returns the group name poolUser was
// added to, so callers can persist it (domainmodel.Service.PHPPoolGroup)
// and hand it back to ReapplyGroupAccess later without repeating the
// owner lookup -- which would drift, since docRoot's owner group is
// whichever operator account happened to create the service.
func GrantGroupAccess(docRoot, poolUser string) (string, error) {
	group, err := ownerGroupName(docRoot)
	if err != nil {
		return "", err
	}
	if err := addUserToGroup(poolUser, group); err != nil {
		return "", err
	}
	if err := applyGroupAccess(docRoot, group); err != nil {
		return "", err
	}
	return group, nil
}

// ReapplyGroupAccess re-runs GrantGroupAccess's permission pass over
// docRoot using a group that was already recorded for this pool
// (domainmodel.Service.PHPPoolGroup), instead of re-deriving it from
// docRoot's current owner the way GrantGroupAccess does.
//
// Needed because the grant is not self-maintaining: files that appear
// under docRoot after the pool was created -- a `git pull`, an `npm run
// build`, a `pip install` -- are owned by whoever ran the command, with
// that account's primary group, and the pool user is not a member of it.
// GrantGroupAccess was only ever called once, at service creation
// (cliapp.setupPHPPool), so those files stayed unreadable to the pool
// that is supposed to serve them.
//
// The group is passed in rather than looked up precisely because a
// second operator account is the case that breaks: re-deriving it would
// return *that* operator's group and quietly move the service onto a
// different group than the one the pool user was added to.
func ReapplyGroupAccess(docRoot, group string) error {
	if group == "" {
		return fmt.Errorf("no pool group recorded for %s", docRoot)
	}
	return applyGroupAccess(docRoot, group)
}

// applyGroupAccess makes every file and directory under docRoot readable
// by group, and makes that stay true for files created afterwards.
//
// Three steps, all required:
//
//   - chgrp -R: files created by a different operator account carry that
//     account's primary group. Only the group is changed; the owning
//     *user* of each file is left alone, so the operator who wrote a file
//     keeps write access to it.
//   - chmod -R g+rX: capital X, so +x lands only where something is
//     already executable (directories, real binaries) and a plain data
//     file is never made spuriously executable.
//   - chmod g+s on directories only: the setgid bit makes new entries
//     inherit their parent directory's group instead of the creating
//     user's primary group, which is what stops this from drifting again
//     on the next deploy. Deliberately not `chmod -R g+s`: on a regular
//     file that bit means setgid-on-exec (or mandatory locking), never
//     "inherit", so it must not be applied indiscriminately.
//
// setgid fixes the group of future files but not their mode, so a
// deploying account with a group-hostile umask (0077) can still produce
// unreadable files. That case is why the chgrp+chmod pass above is
// re-run on deploy rather than relying on setgid alone.
func applyGroupAccess(docRoot, group string) error {
	steps := [][]string{
		{"chgrp", "-R", group, docRoot},
		{"chmod", "-R", "g+rX", docRoot},
		{"find", docRoot, "-type", "d", "-exec", "chmod", "g+s", "{}", "+"},
	}
	for _, step := range steps {
		cmd, err := osutil.SudoCommand(step[0], step[1:]...)
		if err != nil {
			return err
		}
		if out, runErr := cmd.CombinedOutput(); runErr != nil {
			return fmt.Errorf("cannot grant group access to %s (%s): %w: %s",
				docRoot, osutil.ElevationHint(), runErr, strings.TrimSpace(string(out)))
		}
	}
	return nil
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
