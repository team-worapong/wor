package cliapp

import (
	"fmt"
	"io/fs"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"wor/internal/osutil"
)

// cmdServiceChown hands one service's tree back to the account that is
// supposed to own it -- by default the configured operator.
//
// It exists because `wor doctor` could already *detect* a service tree
// owned by the wrong account but could only tell the operator to run
// `sudo chown -R <user> <domains>` by hand. That advice was broader than
// the problem it diagnosed (every service, to repair one) and it knew
// nothing about the php-fpm pool that has to keep reading those files
// afterwards. This is the same repair with both corrected.
//
// It repairs; it does not record. Nothing is written to
// services.config.json and there is deliberately no per-service owner:
// wor's model is still one operator account for the whole tree (see
// setupOperatorAccount), and a service permanently owned by somebody
// else would be swept back by the next `wor setup` anyway. What this
// fixes is a tree that has *drifted* -- files left by a root-run
// command, a CI rsync, or an admin who is not the operator.
func (a *App) cmdServiceChown(domain, service string, args []string) error {
	if osutil.IsWindows() {
		return a.errf("service ownership is a unix concept; there is nothing to change on Windows")
	}
	if err := a.requireServiceExists(domain, service); err != nil {
		return err
	}

	target, err := a.resolveChownTarget(args)
	if err != nil {
		return err
	}
	dir := a.Store.ServiceDir(domain, service)

	if err := a.chownServiceTree(dir, target); err != nil {
		return err
	}
	a.ok("%s/%s now belongs to %s", domain, service, target.Username)

	// Two different questions, answered in order rather than letting one
	// fall out of the other: the chown above settles who *owns* the
	// tree, and this settles what the pool can *read*. The re-grant
	// covers the document root, from the group recorded for this pool.
	// Everything outside it -- `src/.env` at 0640, most importantly --
	// keeps the group the chown deliberately did not touch, which is
	// what the pool user's membership was granted against.
	a.reapplyPHPPoolAccess(domain, service)
	return nil
}

// resolveChownTarget works out which account the tree should end up
// belonging to: the one named on the command line, or the configured
// operator when none is given.
//
// A per-service pool account is refused. It is a plausible thing to type
// -- it is the account most obviously "associated with" the service --
// but it inverts the model: the pool is granted *read* access through
// the group and must never own the code it serves, or a compromised
// worker could rewrite the application it is running.
func (a *App) resolveChownTarget(args []string) (*user.User, error) {
	name := ""
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			name = arg
			break
		}
	}
	if name == "" {
		name = a.Cfg.OperatorUser
	}
	if name == "" {
		return nil, a.errf("no account given and no operator account is configured for this host.\n" +
			"Name one (wor service chown <domain>/<service> <user>), or run `wor setup` to record one.")
	}
	u, err := user.Lookup(name)
	if err != nil {
		return nil, a.errf("no such account: %s", name)
	}
	if isServiceAccountName(u.Username) {
		return nil, a.errf("%s is a php-fpm pool account: it reads the service through the group and must not own the files it serves.\n"+
			"Name the operator account instead.", u.Username)
	}
	return u, nil
}

// chownServiceTree moves everything under dir to target, except what the
// service's own pool account owns.
//
// Owner only, never `user:group`, matching chownTreeFromUID. A bare
// `chown -R <user>` does leave the group alone, so the hand-typed
// version was not wrong about that -- but `chown -R <user>:` is one
// keystroke away and does not, and what it takes away is a php-fpm
// pool's read access to `src/.env` at 0640, while every 0644 file
// around it still looks fine. Spelling the rule out here means nobody
// has to get that keystroke right under pressure.
//
// root-owned files *are* swept here, unlike in alignTreeOwnership, and
// the difference is deliberate: there, root-owned directories under
// WOR_HOME belong to other tooling and are out of bounds; here the scope
// is one service directory wor created, and root-owned files inside it
// are precisely the damage being repaired -- a deploy that ran as root,
// or an `rsync -a` that carried root ownership in with it.
func (a *App) chownServiceTree(dir string, target *user.User) error {
	args := []string{dir}
	for _, uid := range poolAccountUIDsUnder(dir) {
		args = append(args, "!", "-uid", strconv.Itoa(uid))
	}
	// `chown -h` acts on a symlink itself; following one could otherwise
	// walk a link out of the service directory and chown whatever it
	// points at.
	args = append(args, "-exec", "chown", "-h", target.Uid, "{}", "+")

	cmd, err := osutil.SudoCommand("find", args...)
	if err != nil {
		return err
	}
	if out, runErr := cmd.CombinedOutput(); runErr != nil {
		return fmt.Errorf("cannot hand %s to %s (%s): %w: %s",
			dir, target.Username, osutil.ElevationHint(), runErr, strings.TrimSpace(string(out)))
	}
	return nil
}

// poolAccountUIDsUnder lists the uids of any php-fpm pool accounts that
// own something inside dir, so chownServiceTree can step around them.
//
// Discovered from the tree rather than derived from the service's name
// on purpose: a service that was renamed, or one left by an older pool
// naming scheme, still gets its files skipped.
//
// A walk is affordable here in a way it is not in checkOperatorIdentity,
// which samples directory owners across every service on the host. This
// looks at one service, once, in a command whose next step shells out to
// find(1) over the same tree anyway.
func poolAccountUIDsUnder(dir string) []int {
	seen := map[int]bool{}
	var out []int
	_ = filepath.WalkDir(dir, func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		uid, _, ownErr := osutil.FileOwner(path)
		if ownErr != nil || seen[uid] {
			return nil
		}
		seen[uid] = true
		if isServiceAccountUID(uid) {
			out = append(out, uid)
		}
		return nil
	})
	return out
}

// isServiceAccountName reports whether name is one of the dedicated
// per-service accounts wor creates for php-fpm pools (phpfpm.PoolName's
// "wor_<domain>_<service>" convention), as opposed to a human operator.
func isServiceAccountName(name string) bool {
	return strings.HasPrefix(name, "wor_")
}
