package cliapp

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"wor/internal/config"
	"wor/internal/osutil"
)

// DefaultOperatorUser is the account name `wor setup` offers when a host
// has not chosen one yet. Deliberately the plain product name: on a
// freshly provisioned server it is free, and anything longer only makes
// `ls -l` harder to read.
const DefaultOperatorUser = "wor"

// operatorUserNone is what an operator types to decline the whole idea
// and keep wor's original "files belong to whoever ran the command"
// behaviour.
const operatorUserNone = "none"

// setupOperatorAccount is `wor setup`'s operator-account step: it asks
// which single account should own everything under WOR_HOME, and
// records the answer on a.Cfg so the steps that follow (ensureRootDirs,
// then alignTreeOwnership) can act on it.
//
// It exists as a setup step rather than as documentation telling the
// admin to run useradd and edit host.env because `wor setup` is
// re-runnable by design, which is what lets a host provisioned before
// this existed be brought up to the same shape as a new one just by
// running it again.
//
// Linux only. macOS is a development machine in wor's model -- one
// person, one login, no second admin to disagree with -- and account
// creation there is `sysadminctl`, a different tool with different
// failure modes, added for a problem that platform does not have. The
// per-service php-fpm pools make the same distinction for the same
// reason (see setupPHPPool).
func (a *App) setupOperatorAccount() {
	if !osutil.IsLinux() {
		return
	}

	fmt.Fprintln(a.Out)
	fmt.Fprintln(a.Out, "Operator account")
	fmt.Fprintln(a.Out, "  One account owns everything under WOR_HOME, whichever admin is logged in.")
	fmt.Fprintln(a.Out, "  Without it, each admin who runs wor takes ownership from the last one and")
	fmt.Fprintln(a.Out, "  cannot write what the other created.")
	fmt.Fprintf(a.Out, "  Answer %q to keep wor's original per-user ownership.\n", operatorUserNone)

	def := a.Cfg.OperatorUser
	if def == "" {
		def = DefaultOperatorUser
	}
	answer := strings.TrimSpace(a.promptDefault("Operator account", def))
	if answer == "" || strings.EqualFold(answer, operatorUserNone) {
		a.Cfg.OperatorUser = ""
		return
	}
	a.Cfg.OperatorUser = answer
}

// applyOperatorAccount creates the operator account if it does not exist
// and records it in host.env. Called after the setup summary is
// confirmed, so nothing is created on a run the operator backs out of.
//
// host.env, not the personal ~/.wor/config, because the value is only
// worth anything if every admin on the host resolves the same one --
// see config.Config.OperatorUser.
func (a *App) applyOperatorAccount() error {
	if !osutil.IsLinux() || a.Cfg.OperatorUser == "" {
		return nil
	}
	if err := a.ensureOperatorAccount(a.Cfg.OperatorUser); err != nil {
		return err
	}
	hostEnv := filepath.Join(a.Cfg.Configs, "host.env")
	if err := config.SetHostEnvKey(hostEnv, "WOR_USER", a.Cfg.OperatorUser); err != nil {
		return fmt.Errorf("cannot record the operator account in %s: %w", hostEnv, err)
	}
	// That write goes through sudo whenever host.env belongs to somebody
	// other than the account running setup, and a file written under
	// sudo comes out owned by root. Left alone, the one file naming the
	// operator account would be the one file the operator account cannot
	// rewrite -- and alignTreeOwnership will not fix it, because it
	// deliberately never touches root-owned files.
	if err := osutil.EnsureOwnedBy(hostEnv, a.Cfg.OperatorUser); err != nil {
		return fmt.Errorf("cannot hand %s to %s: %w", hostEnv, a.Cfg.OperatorUser, err)
	}
	return nil
}

// ensureOperatorAccount creates name as a login-disabled system account
// if it is not already present. Idempotent, so re-running `wor setup`
// against a host that already has it does nothing at all.
//
// An account that already exists is left completely alone -- not
// re-shelled, not moved. It may well be a real person's login that
// happens to carry this name (which is exactly how the first host this
// was built for ended up with a `wor` account: a laptop whose username
// was `wor`), and quietly turning somebody's login into a nologin
// system account is not setup's decision to make.
//
// The home directory is created and kept separate from WOR_HOME on
// purpose. wor's own config lives at ~/.wor/config, so the account needs
// a real writable home -- but pointing that home *at* WOR_HOME would arm
// `userdel -r`, which removes an account's home directory, to delete
// every domain on the server.
func (a *App) ensureOperatorAccount(name string) error {
	if _, err := user.Lookup(name); err == nil {
		return nil
	}
	a.info("Creating operator account %s", name)
	cmd, err := osutil.SudoCommand("useradd",
		"--system", "--user-group", "--create-home",
		"--shell", "/usr/sbin/nologin", name)
	if err != nil {
		return err
	}
	if out, runErr := cmd.CombinedOutput(); runErr != nil {
		return fmt.Errorf("cannot create operator account %s (%s): %w: %s",
			name, osutil.ElevationHint(), runErr, strings.TrimSpace(string(out)))
	}
	if _, err := user.Lookup(name); err != nil {
		return fmt.Errorf("operator account %s still does not resolve after useradd: %w", name, err)
	}
	a.ok("Operator account ready: %s", name)
	return nil
}

// alignTreeOwnership hands files under WOR_HOME that belong to a
// previous operator account over to the configured one, so a host set up
// before this existed ends up in the same shape as a new one.
//
// Deliberately *not* a recursive chown of WOR_HOME. It is bounded twice
// over, because a setup step that reaches too far is how this turns into
// an outage somewhere it was never meant to touch:
//
// **Where it looks** is a.rootDirs() -- the directories wor itself
// creates -- not all of WOR_HOME. Other tooling keeps state under there
// too: on the host this was built for, a CI pipeline uploads build
// artifacts into `deployments/` under its own account before they are
// copied into each service. wor did not create that directory and has no
// business re-chowning it.
//
// **Whose files it moves** is enumerated rather than discovered: the
// operator account recorded before this run, whoever is running setup
// now, and -- passed in as priorOwners -- whoever owned wor's own
// directories when this run started. Anything else is left alone: a
// per-service account (`wor_<domain>_<service>`, see phpfpm.PoolName)
// owns its files on purpose, and root-owned files are never touched.
//
// priorOwners is a parameter rather than something read here because by
// the time this runs, ensureRootDirs has already handed those
// directories to the new operator, erasing the very answer needed. See
// treeOwnerUIDs.
func (a *App) alignTreeOwnership(priorOwners []int) error {
	if !osutil.IsLinux() || a.Cfg.OperatorUser == "" {
		return nil
	}
	target, err := user.Lookup(a.Cfg.OperatorUser)
	if err != nil {
		return fmt.Errorf("cannot resolve the operator account %q: %w", a.Cfg.OperatorUser, err)
	}
	targetUID, _ := strconv.Atoi(target.Uid)

	for _, uid := range a.previousOperatorUIDs(targetUID, priorOwners) {
		if err := a.chownTreeFromUID(uid, targetUID); err != nil {
			return err
		}
	}
	return nil
}

// previousOperatorUIDs lists the uids whose files alignTreeOwnership
// should move, excluding the target itself and root. See that function
// for why the list is enumerated rather than discovered by walking.
func (a *App) previousOperatorUIDs(targetUID int, priorOwners []int) []int {
	seen := map[int]bool{targetUID: true, 0: true}
	var out []int
	add := func(uid int) {
		if seen[uid] {
			return
		}
		seen[uid] = true
		out = append(out, uid)
	}

	// The account recorded in host.env before this run. Resolved from
	// the pre-run config, not a.Cfg, which setupOperatorAccount has
	// already overwritten with the new answer.
	if prev := a.previousOperatorUser; prev != "" {
		if u, err := user.Lookup(prev); err == nil {
			if uid, convErr := strconv.Atoi(u.Uid); convErr == nil {
				add(uid)
			}
		}
	}
	add(os.Getuid())
	for _, uid := range priorOwners {
		add(uid)
	}

	sort.Ints(out)
	return out
}

// treeOwnerUIDs reports which accounts own wor's own directories right
// now: WOR_HOME and the directories directly beneath it that wor
// created.
//
// It has to be called before ensureRootDirs, and that timing is the
// whole reason it exists separately. Reading WOR_HOME's owner alone is
// not enough: ClaimOwnership hands a directory to whoever runs a
// command, one directory at a time, so the account that owns WOR_HOME
// and the account that owns everything inside it drift apart -- which
// is the exact situation the operator account is here to end. On the
// first host this ran against, WOR_HOME belonged to one admin's login
// while every file under it belonged to another, and sweeping only the
// first left the whole tree behind.
//
// Still enumeration, not discovery: it asks who owns the directories
// wor itself created, never who owns some arbitrary file found by
// walking the tree.
func (a *App) treeOwnerUIDs() []int {
	seen := map[int]bool{}
	var out []int
	for _, dir := range append([]string{a.Cfg.WorHome}, existingDirs(a.sweepDirs())...) {
		uid, _, err := osutil.FileOwner(dir)
		if err != nil || seen[uid] {
			continue
		}
		seen[uid] = true
		out = append(out, uid)
	}
	return out
}

// chownTreeFromUID moves every file owned by uid, within the directories
// wor manages, to target -- in one pass.
//
// **The group is deliberately left alone.** Only the owning user is
// changed. Nothing about the operator story needs the group: the
// operator writes as the file's owner. Services, on the other hand,
// read through it -- a per-service php-fpm pool is granted access by
// being made a member of the document root's group
// (phpfpm.GrantGroupAccess), and on the host this was written for that
// group is `wor`, the account that happened to create the service.
//
// Rewriting the group here would therefore revoke every pooled service's
// access the moment the operator account changed. It would not even show
// up in most files, because a service tree is mostly 0644 and readable
// through the `other` bits regardless -- but `src/.env` is 0640 by
// design, and losing group read on that one file takes the application
// down while everything around it still looks fine.
//
// Shelling out to find(1) rather than walking in Go and calling chown
// per file: this runs elevated, and one sudo invocation for the whole
// sweep is one password prompt instead of thousands. `-uid` matches
// numerically, so an account that has since been deleted from
// /etc/passwd is still swept up, and `chown -h` acts on symlinks
// themselves -- following them could otherwise walk a link inside a
// service directory straight out to somewhere in /etc and chown that.
func (a *App) chownTreeFromUID(uid, targetUID int) error {
	dirs := existingDirs(a.sweepDirs())
	if len(dirs) == 0 {
		return nil
	}
	owner := accountLabel(uid)
	a.info("Moving files owned by %s to %s", owner, a.Cfg.OperatorUser)

	cmd, err := osutil.SudoCommand("find", findChownArgs(dirs, uid, targetUID)...)
	if err != nil {
		return err
	}
	if out, runErr := cmd.CombinedOutput(); runErr != nil {
		return fmt.Errorf("cannot move %s's files to %s (%s): %w: %s",
			owner, a.Cfg.OperatorUser, osutil.ElevationHint(),
			runErr, strings.TrimSpace(string(out)))
	}
	return nil
}

// sweepDirs is the set of directories alignTreeOwnership recurses into:
// wor's own directories that sit directly under WOR_HOME.
//
// WOR_HOME itself is excluded, and that exclusion is the entire point.
// find(1) descends into whatever it is given, so passing WOR_HOME would
// walk every directory under it -- including the ones wor did not create
// and must not touch -- making the whole boundary meaningless. WOR_HOME's
// own ownership is settled separately, and without recursion, by
// ensureRootDirs.
//
// Derived from rootDirs rather than written out again, so a directory
// added there is swept without anyone having to remember this exists.
// Nested entries (configs/database, the ACME tree) are dropped because
// their parent is already in the list and find would only visit them
// twice.
func (a *App) sweepDirs() []string {
	var out []string
	for _, d := range a.rootDirs() {
		if d != a.Cfg.WorHome && filepath.Dir(d) == a.Cfg.WorHome {
			out = append(out, d)
		}
	}
	return out
}

// existingDirs filters paths down to the ones actually present, so a
// WOR_HOME missing an optional directory does not make find(1) fail the
// whole sweep over a path nobody needed.
func existingDirs(paths []string) []string {
	var out []string
	for _, p := range paths {
		if dirExists(p) {
			out = append(out, p)
		}
	}
	return out
}

// findChownArgs builds the argv chownTreeFromUID hands to find(1). Split
// out so the owner-only guarantee documented above is assertable without
// root and a real tree to chown -- there is no other way to observe it,
// and it is the one detail of this whole migration that quietly breaks a
// running site when it is wrong.
func findChownArgs(dirs []string, fromUID, toUID int) []string {
	args := append([]string{}, dirs...)
	// A bare uid, with no ":group", changes the owner and nothing else.
	return append(args, "-uid", strconv.Itoa(fromUID),
		"-exec", "chown", "-h", strconv.Itoa(toUID), "{}", "+")
}
