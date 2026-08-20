// Package worlock provides a single, process-wide advisory lock so two
// concurrent `wor` invocations (a cron-triggered `wor deploy` racing an
// interactive `wor service status`, for example) never read or write
// $WOR_HOME's state at the same time. It deliberately covers a whole
// command, not individual files: a single command (e.g. `wor service
// add`) often touches several related files together (services.config.json,
// the PM2 ecosystem file, a generated vhost, ...), so a lock scoped
// narrower than "one command at a time" would still leave
// read-modify-write races between those files.
package worlock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"wor/internal/osutil"
)

// fileName is the lock file's name inside $WOR_HOME. Its content is
// irrelevant -- only the OS-level advisory lock held on the open file
// descriptor matters.
const fileName = ".wor.lock"

// ErrLockHeld reports the one failure that actually means "another wor
// is running": the lock file opened fine, but a different process holds
// the lock on it. Every other Acquire failure is something else --
// usually a permission problem -- and telling the operator to wait for a
// command that does not exist sends them looking in the wrong place.
// Callers distinguish the two with errors.Is.
var ErrLockHeld = errors.New("another wor command is already running")

// Handle represents a held lock. Call Release to give it up. A nil
// Handle is safe to Release (no-op), so callers can always defer
// h.Release() right after checking the Acquire error.
type Handle struct {
	file *os.File
}

// Acquire takes an exclusive, non-blocking lock on $WOR_HOME/.wor.lock.
// If another wor process already holds it, Acquire returns an error
// immediately rather than waiting -- a CLI command should never hang
// silently; the user should see a clear "try again" message right
// away instead of an unexplained pause.
//
// The lock is released automatically by the OS if the holding process
// dies or is killed (including SIGKILL/power loss on the whole
// machine): flock on Unix and LockFileEx on Windows are both tied to
// the open file descriptor/handle's lifetime, not to any content
// written to the lock file. So, unlike a naive "does this file exist"
// scheme, a crashed wor can never leave a stale lock that blocks every
// future command.
//
// If worHome doesn't exist yet, Acquire deliberately does nothing and
// returns (nil, nil) instead of creating it. This matters for `wor
// setup` specifically: it resolves/creates the real WOR_HOME
// interactively (the caller-supplied worHome here is only whatever was
// previously configured, or a not-yet-confirmed default), so acquiring
// a lock must never have the side effect of pre-creating a directory
// the user hasn't chosen or confirmed yet. There is also nothing to
// protect at a path with no state in it. A nil *Handle is safe to
// Release (no-op), so callers don't need to special-case this.
func Acquire(worHome string) (*Handle, error) {
	if worHome == "" {
		return nil, nil
	}
	if _, err := os.Stat(worHome); err != nil {
		return nil, nil
	}
	path := filepath.Join(worHome, fileName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil && os.IsPermission(err) {
		// The lock file exists but belongs to a different account: it is
		// created 0644, so whichever admin ran wor here first owns it.
		//
		// Opening read-only is enough, and is tried before anything that
		// changes ownership. flock locks the open file description, not
		// the file's contents, and this package never writes a byte to
		// the lock file (see fileName's comment) -- so a 0644 file that
		// any account can open for reading gives every account the same
		// mutual exclusion, with nobody having to take ownership away
		// from anybody.
		//
		// Order matters here. When this ran *after* the ClaimOwnership
		// fallback below, simply reading the lock was enough to trigger
		// a sudo chown of WOR_HOME on every invocation by the account
		// that did not own it -- so two admins dragged WOR_HOME back and
		// forth between them, one password prompt at a time, purely to
		// reach a file that either of them could already open.
		f, err = os.OpenFile(path, os.O_RDONLY, 0o644)
	}
	if err != nil && os.IsPermission(err) {
		// Last resort, for the case the read-only open cannot help with:
		// WOR_HOME is root-owned and the lock file does not exist yet,
		// so it has to be *created*. This happens to a WOR_HOME that
		// EnsureDir had to `sudo mkdir`, or one left behind root-owned
		// by an older install. Scoped to worHome itself, not recursive
		// -- see ClaimOwnership's own doc comment for why -- and it
		// claims for the current user, since worlock has no access to
		// the configured operator account (see osutil.EnsureOwnedBy).
		// `wor setup` is what puts ownership right properly; this only
		// has to get far enough to run a command at all.
		if claimErr := osutil.ClaimOwnership(worHome); claimErr == nil {
			f, err = os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("cannot open lock file %s: %w", path, err)
	}
	if lockErr := lockFile(f); lockErr != nil {
		f.Close()
		return nil, fmt.Errorf("%w (could not lock %s): %w", ErrLockHeld, path, lockErr)
	}
	return &Handle{file: f}, nil
}

// Release gives up the lock and closes the underlying file. Safe to
// call on a nil Handle.
func (h *Handle) Release() {
	if h == nil || h.file == nil {
		return
	}
	unlockFile(h.file)
	h.file.Close()
}
