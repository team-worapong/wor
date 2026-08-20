//go:build !windows

package osutil

import (
	"os"
	"os/user"
	"testing"
)

// TestEnsureOwnedByEmptyNameKeepsOldBehaviour is the compatibility
// guarantee: with no operator account configured, EnsureOwnedBy must
// behave exactly like ClaimOwnership, so shipping this cannot disturb a
// host that never opts in. A freshly created temp dir is writable by the
// test's own account, which is ClaimOwnership's silent no-op path.
func TestEnsureOwnedByEmptyNameKeepsOldBehaviour(t *testing.T) {
	dir := t.TempDir()
	if err := EnsureOwnedBy(dir, ""); err != nil {
		t.Errorf("EnsureOwnedBy with no configured account: %v", err)
	}
}

// TestEnsureOwnedByIsANoopWhenAlreadyCorrect covers the case that runs
// on every single wor invocation once an operator account is set: the
// directory already belongs to it. This must not shell out to sudo --
// if it did, every command would prompt for a password forever.
func TestEnsureOwnedByIsANoopWhenAlreadyCorrect(t *testing.T) {
	me, err := user.Current()
	if err != nil {
		t.Skipf("cannot resolve the current account: %v", err)
	}
	// Sudo is not available in most test environments, so a call that
	// wrongly decided to chown would fail loudly rather than silently
	// succeed -- which is exactly the signal this test wants.
	if err := EnsureOwnedBy(t.TempDir(), me.Username); err != nil {
		t.Errorf("EnsureOwnedBy on a directory already owned by %s should be a no-op, got: %v", me.Username, err)
	}
}

// TestEnsureOwnedByRejectsAnUnknownAccount makes a typo in host.env
// report itself instead of being silently ignored. Silently ignoring it
// would leave the operator believing ownership was pinned when nothing
// was pinning it.
func TestEnsureOwnedByRejectsAnUnknownAccount(t *testing.T) {
	err := EnsureOwnedBy(t.TempDir(), "definitely-not-a-real-account-9f3a2b")
	if err == nil {
		t.Fatal("expected an error for an account that does not exist, got nil")
	}
}

// TestEnsureOwnedByReportsAMissingDirectory keeps a path that is not
// there from being mistaken for one that is correctly owned.
func TestEnsureOwnedByReportsAMissingDirectory(t *testing.T) {
	me, err := user.Current()
	if err != nil {
		t.Skipf("cannot resolve the current account: %v", err)
	}
	missing := t.TempDir() + "/not-created"
	if err := EnsureOwnedBy(missing, me.Username); err == nil {
		t.Error("expected an error for a missing directory, got nil")
	} else if !os.IsNotExist(err) {
		t.Errorf("expected a not-exist error, got %v", err)
	}
}
