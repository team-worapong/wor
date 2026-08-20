package cliapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPrepareACMEWebrootUsesTheOperatorAccount pins the one place
// outside `wor setup` that hands a directory to somebody on an ordinary
// run.
//
// It used to call osutil.ClaimOwnership, which chowns to whoever is
// typing. That meant an admin who is not the operator took the ACME tree
// away from the operator on every certificate operation -- putting back,
// one directory at a time, exactly the drift setup had just swept out.
//
// Observed here without needing root, and without asserting on a chown
// that a test cannot perform: an operator account that does not resolve
// is an error EnsureOwnedBy raises and ClaimOwnership cannot, because
// ClaimOwnership never looks a name up at all. A writable directory plus
// a configured-but-unknown account therefore separates the two
// implementations exactly.
func TestPrepareACMEWebrootUsesTheOperatorAccount(t *testing.T) {
	app := newOperatorTestApp(t, "", "wor-no-such-account-exists")
	app.Cfg.SSL = filepath.Join(app.Cfg.WorHome, "ssl")
	app.Cfg.ACME = filepath.Join(app.Cfg.SSL, "acme")
	if err := os.MkdirAll(app.Cfg.ACME, 0o755); err != nil {
		t.Fatal(err)
	}

	err := app.prepareACMEWebroot()

	if err == nil {
		t.Fatal("a writable ACME tree was accepted without resolving the configured operator account; " +
			"that is ClaimOwnership's behaviour, which claims the tree for whoever ran the command")
	}
	if !strings.Contains(err.Error(), "wor-no-such-account-exists") {
		t.Errorf("expected the unresolvable operator account to be named in %q", err)
	}
}

// TestPrepareACMEWebrootWithoutAnOperatorKeepsOldBehaviour is the other
// half: a host that never ran the operator step must be completely
// unaffected. With no account configured EnsureOwnedBy falls through to
// ClaimOwnership, which is a silent no-op on a directory the caller can
// already write.
func TestPrepareACMEWebrootWithoutAnOperatorKeepsOldBehaviour(t *testing.T) {
	app := newOperatorTestApp(t, "", "")
	app.Cfg.SSL = filepath.Join(app.Cfg.WorHome, "ssl")
	app.Cfg.ACME = filepath.Join(app.Cfg.SSL, "acme")

	if err := app.prepareACMEWebroot(); err != nil {
		t.Fatalf("no operator configured should mean no ownership decision to make: %v", err)
	}

	// And it still has to have created the tree certbot writes into.
	challenge := filepath.Join(app.Cfg.ACME, ".well-known", "acme-challenge")
	if info, err := os.Stat(challenge); err != nil || !info.IsDir() {
		t.Errorf("expected the challenge directory %s to exist: %v", challenge, err)
	}
}
