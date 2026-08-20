//go:build !windows

package phpfpm

import "testing"

// TestReapplyGroupAccessRejectsEmptyGroup guards the one input that must
// never reach the shell: an empty group name. `chgrp -R "" <docroot>` is
// a confusing failure at best, and the empty string is exactly what a
// service record written before the pool group was persisted yields --
// so it has to be refused explicitly, before any privileged command is
// built.
func TestReapplyGroupAccessRejectsEmptyGroup(t *testing.T) {
	if err := ReapplyGroupAccess(t.TempDir(), ""); err == nil {
		t.Error("ReapplyGroupAccess with an empty group: expected an error, got nil")
	}
}
