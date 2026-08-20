package cliapp

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"wor/internal/config"
	"wor/internal/domainmodel"
)

// newDoctorIdentityTestApp builds an App whose WOR_HOME is a real temp
// directory, with Cfg.Domains pointed at the same domains dir the Store
// uses -- checkOperatorIdentity gates on Cfg.Domains but enumerates
// through the Store, so the two have to agree for the check to run at
// all.
func newDoctorIdentityTestApp(t *testing.T) *App {
	t.Helper()
	domainsDir := filepath.Join(t.TempDir(), "domains")
	if err := os.MkdirAll(domainsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	return &App{
		Cfg:   &config.Config{Domains: domainsDir},
		Store: domainmodel.NewStore(domainsDir),
		Out:   &bytes.Buffer{},
		Err:   &bytes.Buffer{},
	}
}

// TestCheckOperatorIdentitySilentWithNoDomains keeps the check from
// reporting on an install that has nothing in it yet -- there is no
// ownership story to tell before the first domain exists.
func TestCheckOperatorIdentitySilentWithNoDomains(t *testing.T) {
	app := newDoctorIdentityTestApp(t)

	app.checkOperatorIdentity()

	if out := app.Out.(*bytes.Buffer).String(); out != "" {
		t.Errorf("expected no output for an empty domains dir, got %q", out)
	}
}

// TestCheckOperatorIdentityReportsASingleOwner covers the healthy case,
// which is also the only one a test can construct: everything the test
// creates belongs to the account running the test. The split-ownership
// branch needs a second real uid and so needs root to set up.
func TestCheckOperatorIdentityReportsASingleOwner(t *testing.T) {
	app := newDoctorIdentityTestApp(t)
	if err := app.Store.MakeDomainFiles("shop-example-com"); err != nil {
		t.Fatalf("MakeDomainFiles: %v", err)
	}
	if err := app.Store.AddService("shop-example-com", "site", "", 0, "php", ""); err != nil {
		t.Fatalf("AddService: %v", err)
	}
	if err := os.MkdirAll(app.Store.ServiceDir("shop-example-com", "site"), 0o755); err != nil {
		t.Fatalf("MkdirAll service dir: %v", err)
	}

	app.checkOperatorIdentity()

	out := app.Out.(*bytes.Buffer).String()
	if !strings.Contains(out, "single account") {
		t.Errorf("expected the single-owner report, got %q", out)
	}
	if strings.Contains(out, "split across") {
		t.Errorf("a tree created by one account must not be reported as split, got %q", out)
	}
}
