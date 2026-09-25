//go:build !windows

package osutil

import (
	"strings"
	"testing"
)

// Non-interactive mode must neither ask the elevation question nor let
// sudo ask for a password: the question is skipped and sudo gets -n.
func TestSudoCommandNonInteractiveUsesSudoN(t *testing.T) {
	if IsRoot() {
		t.Skip("root never goes through sudo")
	}
	asked := false
	SetElevationPrompt(func(string) bool { asked = true; return true })
	SetNonInteractive(true)
	t.Cleanup(func() {
		SetElevationPrompt(nil)
		SetNonInteractive(false)
	})

	cmd, err := SudoCommand("systemctl", "reload", "nginx")
	if err != nil {
		t.Fatalf("SudoCommand: %v", err)
	}
	if got := strings.Join(cmd.Args, " "); got != "sudo -n systemctl reload nginx" {
		t.Errorf("args = %q, want %q", got, "sudo -n systemctl reload nginx")
	}
	if asked {
		t.Error("the elevation question was asked in non-interactive mode")
	}
}
