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

// newTestDatabaseApp builds an *App wired to a temp WOR_HOME-style
// layout for `wor database add/remove` tests.
func newTestDatabaseApp(t *testing.T) *App {
	t.Helper()
	root := t.TempDir()
	store := domainmodel.NewStore(filepath.Join(root, "domains"))
	return &App{
		Cfg: &config.Config{
			Domains: filepath.Join(root, "domains"),
			Backups: filepath.Join(root, "backups"),
			Configs: filepath.Join(root, "configs"),
		},
		Store: store,
		Out:   &bytes.Buffer{},
		Err:   &bytes.Buffer{},
	}
}

// TestCmdDatabaseAddBlockedByMissingDomain guards the new rule: `wor
// database add` must NOT auto-create the domain (the old behavior,
// via MakeDomainFiles) -- if WOR_HOME/domains/<domain> doesn't exist
// yet, it should error out instead.
func TestCmdDatabaseAddBlockedByMissingDomain(t *testing.T) {
	app := newTestDatabaseApp(t)

	err := app.cmdDatabase([]string{"add", "shop-example/main"})
	if err == nil {
		t.Fatal("expected an error when the domain doesn't exist")
	}
	if _, statErr := os.Stat(app.Store.DomainDir("shop-example")); statErr == nil {
		t.Error("domain directory should NOT have been auto-created")
	}
}

// TestCmdDatabaseAddDuplicateProfileWarns covers the new rule: adding
// a profile that already exists should be a no-op (label/.env
// untouched) but must print a warning, unlike the old silent skip.
func TestCmdDatabaseAddDuplicateProfileWarns(t *testing.T) {
	app := newTestDatabaseApp(t)
	domain := "shop-example"
	if err := app.Store.MakeDomainFiles(domain); err != nil {
		t.Fatalf("MakeDomainFiles: %v", err)
	}

	if err := app.cmdDatabase([]string{"add", domain + "/main", "--label=First"}); err != nil {
		t.Fatalf("first add: %v", err)
	}

	// Reset buffers so we only inspect output from the second (duplicate) call.
	app.Out = &bytes.Buffer{}
	app.Err = &bytes.Buffer{}

	if err := app.cmdDatabase([]string{"add", domain + "/main", "--label=Second"}); err != nil {
		t.Fatalf("duplicate add: %v", err)
	}

	errOut := app.Err.(*bytes.Buffer).String()
	if !strings.Contains(errOut, "already exists") {
		t.Errorf("expected a warning about the profile already existing, got:\n%s", errOut)
	}

	dbCfg, err := app.Store.LoadDatabases(domain)
	if err != nil {
		t.Fatalf("LoadDatabases: %v", err)
	}
	if len(dbCfg.Databases) != 1 {
		t.Fatalf("expected exactly 1 profile to remain registered, got %d", len(dbCfg.Databases))
	}
	if dbCfg.Databases[0].Label != "First" {
		t.Errorf("duplicate add must not overwrite the existing label, got %q", dbCfg.Databases[0].Label)
	}
}

// TestCmdDatabaseRemoveBlockedByMissingDomain covers the new
// domain-existence check on remove.
func TestCmdDatabaseRemoveBlockedByMissingDomain(t *testing.T) {
	app := newTestDatabaseApp(t)

	err := app.cmdDatabase([]string{"remove", "shop-example/main"})
	if err == nil {
		t.Fatal("expected an error when the domain doesn't exist")
	}
}

// TestCmdDatabaseRemoveBlockedByMissingProfile covers the new
// profile-existence check: removing a profile that was never
// registered should error instead of silently succeeding.
func TestCmdDatabaseRemoveBlockedByMissingProfile(t *testing.T) {
	app := newTestDatabaseApp(t)
	domain := "shop-example"
	if err := app.Store.MakeDomainFiles(domain); err != nil {
		t.Fatalf("MakeDomainFiles: %v", err)
	}

	err := app.cmdDatabase([]string{"remove", domain + "/main"})
	if err == nil {
		t.Fatal("expected an error when the profile isn't registered")
	}
}

// TestCmdDatabaseRemoveDeletesEnvFile covers the fix for a real bug:
// the old remove action only ever dropped the config entry and never
// touched the profile's .env file on disk. Now it should delete it.
func TestCmdDatabaseRemoveDeletesEnvFile(t *testing.T) {
	app := newTestDatabaseApp(t)
	domain := "shop-example"
	if err := app.cmdDatabase([]string{"add", domain + "/main"}); err == nil {
		t.Fatal("expected add to fail before the domain exists")
	}
	if err := app.Store.MakeDomainFiles(domain); err != nil {
		t.Fatalf("MakeDomainFiles: %v", err)
	}
	if err := app.cmdDatabase([]string{"add", domain + "/main"}); err != nil {
		t.Fatalf("add: %v", err)
	}

	envFile := filepath.Join(app.Cfg.Configs, "database", "main.env")
	if _, err := os.Stat(envFile); err != nil {
		t.Fatalf("expected add to have created the env file: %v", err)
	}

	if err := app.cmdDatabase([]string{"remove", domain + "/main"}); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Stat(envFile); !os.IsNotExist(err) {
		t.Error("expected the .env file to have been deleted by remove")
	}

	dbCfg, err := app.Store.LoadDatabases(domain)
	if err != nil {
		t.Fatalf("LoadDatabases: %v", err)
	}
	if len(dbCfg.Databases) != 0 {
		t.Errorf("expected the profile to be gone from config, got %d entries", len(dbCfg.Databases))
	}
}

// TestCmdDatabaseRemoveSkipsMissingEnvFile covers the "skip, don't
// error" rule: if the profile is registered but its .env file was
// already deleted/missing on disk, remove should still succeed
// (dropping the config entry) and just warn, not fail.
func TestCmdDatabaseRemoveSkipsMissingEnvFile(t *testing.T) {
	app := newTestDatabaseApp(t)
	domain := "shop-example"
	if err := app.Store.MakeDomainFiles(domain); err != nil {
		t.Fatalf("MakeDomainFiles: %v", err)
	}
	if err := app.cmdDatabase([]string{"add", domain + "/main"}); err != nil {
		t.Fatalf("add: %v", err)
	}

	envFile := filepath.Join(app.Cfg.Configs, "database", "main.env")
	if err := os.Remove(envFile); err != nil {
		t.Fatalf("pre-removing env file: %v", err)
	}

	if err := app.cmdDatabase([]string{"remove", domain + "/main"}); err != nil {
		t.Fatalf("remove should still succeed when the env file is already gone: %v", err)
	}

	errOut := app.Err.(*bytes.Buffer).String()
	if !strings.Contains(errOut, "env file not found") {
		t.Errorf("expected a warning that the env file was missing, got:\n%s", errOut)
	}

	dbCfg, err := app.Store.LoadDatabases(domain)
	if err != nil {
		t.Fatalf("LoadDatabases: %v", err)
	}
	if len(dbCfg.Databases) != 0 {
		t.Errorf("expected the profile to still be dropped from config, got %d entries", len(dbCfg.Databases))
	}
}
