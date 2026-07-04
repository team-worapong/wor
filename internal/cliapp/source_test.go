package cliapp

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"wor/internal/config"
	"wor/internal/domainmodel"
)

func zipEntryNames(t *testing.T, path string) []string {
	t.Helper()
	r, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("zip.OpenReader(%s): %v", path, err)
	}
	defer r.Close()
	names := make([]string, 0, len(r.File))
	for _, f := range r.File {
		names = append(names, f.Name)
	}
	return names
}

func containsName(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

// TestSourceBackupPath guards the backups/<domain>/source/... path
// convention: `wor domain add` pre-creates backups/<domain>/source and
// backups/<domain>/database (internal/cliapp/domain.go), and database
// backups already write to backups/<domain>/database/... (internal/
// dbbackup.ApplyRetention). sourceBackup used to write to
// backups/source/<domain>/... instead -- domain and "source" swapped --
// which didn't match either.
func TestSourceBackupPath(t *testing.T) {
	root := t.TempDir()
	backupsDir := filepath.Join(root, "backups")
	store := domainmodel.NewStore(filepath.Join(root, "domains"))

	app := &App{
		Cfg:   &config.Config{Backups: backupsDir},
		Store: store,
	}

	if err := store.MakeDomainFiles("shop.example.com"); err != nil {
		t.Fatalf("MakeDomainFiles: %v", err)
	}

	out, err := app.sourceBackup("shop.example.com", "")
	if err != nil {
		t.Fatalf("sourceBackup: %v", err)
	}

	wantDir := filepath.Join(backupsDir, "shop.example.com", "source")
	gotDir := filepath.Dir(out)
	if gotDir != wantDir {
		t.Errorf("backup written under %q, want %q (backups/<domain>/source)", gotDir, wantDir)
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("backup zip file missing at %s: %v", out, err)
	}
}

// setupGitIgnoreFixture creates a domain whose source tree has a
// .gitignore (excluding node_modules/ and *.log) alongside a file that
// should survive filtering, for the two tests below.
func setupGitIgnoreFixture(t *testing.T) (app *App, domain string) {
	t.Helper()
	root := t.TempDir()
	store := domainmodel.NewStore(filepath.Join(root, "domains"))
	app = &App{
		Cfg:   &config.Config{Backups: filepath.Join(root, "backups")},
		Store: store,
	}
	domain = "shop.example.com"
	if err := store.MakeDomainFiles(domain); err != nil {
		t.Fatalf("MakeDomainFiles: %v", err)
	}
	domainDir := store.DomainDir(domain)

	if err := os.MkdirAll(filepath.Join(domainDir, "node_modules"), 0o755); err != nil {
		t.Fatalf("MkdirAll(node_modules): %v", err)
	}
	files := map[string]string{
		filepath.Join(domainDir, "node_modules", "pkg.js"): "module.exports = {}",
		filepath.Join(domainDir, "debug.log"):               "some debug output",
		filepath.Join(domainDir, "keep.txt"):                "keep me",
		filepath.Join(domainDir, ".gitignore"):              "node_modules/\n*.log\n",
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("WriteFile(%s): %v", path, err)
		}
	}
	return app, domain
}

// TestSourceBackupGitIgnoreFiltersByDefault covers the new default
// behavior: backup.config.json's source.useGitIgnore defaults to true
// (DefaultBackupConfig), so a .gitignore present at the root of the
// tree being backed up should be honored without any flag.
func TestSourceBackupGitIgnoreFiltersByDefault(t *testing.T) {
	app, domain := setupGitIgnoreFixture(t)

	out, err := app.sourceBackup(domain, "")
	if err != nil {
		t.Fatalf("sourceBackup: %v", err)
	}
	names := zipEntryNames(t, out)

	if containsName(names, "node_modules/pkg.js") {
		t.Errorf("node_modules/pkg.js should be excluded by .gitignore's node_modules/ rule, got entries: %v", names)
	}
	if containsName(names, "debug.log") {
		t.Errorf("debug.log should be excluded by .gitignore's *.log rule, got entries: %v", names)
	}
	if !containsName(names, "keep.txt") {
		t.Errorf("keep.txt should still be included, got entries: %v", names)
	}
}

// TestSourceBackupGitIgnoreDisableOverride covers `--gitignore=disable`:
// even with a .gitignore present, passing "disable" should zip
// everything (aside from the always-on static Exclude list).
func TestSourceBackupGitIgnoreDisableOverride(t *testing.T) {
	app, domain := setupGitIgnoreFixture(t)

	out, err := app.sourceBackup(domain, "disable")
	if err != nil {
		t.Fatalf("sourceBackup: %v", err)
	}
	names := zipEntryNames(t, out)

	if !containsName(names, "node_modules/pkg.js") {
		t.Errorf("--gitignore=disable should include node_modules/pkg.js, got entries: %v", names)
	}
	if !containsName(names, "debug.log") {
		t.Errorf("--gitignore=disable should include debug.log, got entries: %v", names)
	}
}
