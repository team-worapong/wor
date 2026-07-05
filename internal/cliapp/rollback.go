package cliapp

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"wor/internal/domainmodel"
	"wor/internal/osutil"
)

// cmdRollback implements `wor rollback <domain>/<service> [--yes]`: a
// hard reset of a service's source tree back to its remote branch,
// discarding any uncommitted local changes -- including a stuck
// merge/stash-pop conflict left behind by `wor deploy --stash` or `wor
// source pull --stash` (see gitPull's doc comment in source.go for how
// those arise).
//
// Deliberately domain/service only, never a bare domain: a bare domain
// would reset every service under it at once, which is a much bigger
// blast radius than the single stuck service this exists to recover.
//
// Unlike gitPull's --stash (which tries to preserve local edits across
// a pull), rollback assumes local state is not worth keeping. It backs
// up the current tree via the same mechanism as `wor source backup`
// before doing anything destructive, and never touches existing `git
// stash` entries -- `git reset --hard` + `git clean -fd` don't touch
// refs/stash -- so there are two independent safety nets left behind
// afterward.
func (a *App) cmdRollback(args []string) error {
	if len(args) == 0 {
		a.usage()
		return a.errf("rollback target required: <domain>/<service>")
	}
	target := args[0]
	fl := parseFlags(args[1:])

	domain, service, err := domainmodel.ParseTarget(target)
	if err != nil {
		return err
	}
	if service == "" {
		return a.errf("wor rollback requires <domain>/<service>, not a bare domain (it would affect every service under it)")
	}

	if !osutil.Exists("git") {
		return a.errf("git is not installed (required for wor rollback)")
	}

	dir := a.Store.ServiceDir(domain, service)
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		return a.errf("not a git repository: %s", dir)
	}

	branch, err := gitOutput(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil || branch == "" || branch == "HEAD" {
		return a.errf("could not determine the current branch in %s (detached HEAD?) -- resolve manually with git", dir)
	}

	statusOut, _ := gitOutput(dir, "status", "--porcelain")
	changedCount := 0
	if statusOut != "" {
		changedCount = len(strings.Split(statusOut, "\n"))
	}
	stashOut, _ := gitOutput(dir, "stash", "list")
	stashCount := 0
	if stashOut != "" {
		stashCount = len(strings.Split(stashOut, "\n"))
	}

	fmt.Fprintf(a.Out, "This will reset %s to origin/%s, discarding all uncommitted local changes.\n", dir, branch)
	fmt.Fprintf(a.Out, "  %d uncommitted change(s) in the working tree\n", changedCount)
	fmt.Fprintf(a.Out, "  %d existing stash entry/entries (left untouched)\n", stashCount)

	yes := fl.Has("yes") || fl.Has("y")
	if !yes {
		if !a.requireTyped(fmt.Sprintf("Type YES to roll back %s: ", target), "YES") {
			return a.errf("cancelled")
		}
	}

	backupPath, err := a.sourceBackup(target, "")
	if err != nil {
		return fmt.Errorf("backup before rollback failed, aborting: %w", err)
	}

	if _, err := gitOutput(dir, "fetch", "origin"); err != nil {
		return fmt.Errorf("git fetch failed: %w", err)
	}
	resetCmd := exec.Command("git", "reset", "--hard", "origin/"+branch)
	resetCmd.Dir = dir
	resetCmd.Stdout, resetCmd.Stderr = a.Out, a.Err
	if err := resetCmd.Run(); err != nil {
		return fmt.Errorf("git reset --hard failed: %w", err)
	}
	cleanCmd := exec.Command("git", "clean", "-fd")
	cleanCmd.Dir = dir
	cleanCmd.Stdout, cleanCmd.Stderr = a.Out, a.Err
	if err := cleanCmd.Run(); err != nil {
		return fmt.Errorf("git clean failed: %w", err)
	}

	a.ok("Rolled back %s to origin/%s", target, branch)
	a.info("Backup of previous state: %s", backupPath)
	if stashCount > 0 {
		a.info("%d stash entry/entries from before rollback still exist -- review with `git stash list` in %s", stashCount, dir)
	}

	if a.confirmYesDefaultNo(fmt.Sprintf("Deploy %s now?", target)) {
		// --no-pull: the reset already put the tree at origin/<branch>,
		// pulling again would be a no-op. --force: rollback's own
		// git reset --hard means before == after from deploy's point of
		// view (no new commit pulled *by deploy*), so deploy's normal
		// changed-since-pull heuristic would otherwise skip npm
		// ci/build, pip install, and the go rebuild -- exactly the
		// steps most likely needed to match the code rollback just
		// reset to.
		return a.cmdDeploy([]string{target, "--no-pull", "--force"})
	}
	return nil
}
