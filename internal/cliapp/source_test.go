package cliapp

import (
	"archive/zip"
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
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

	if err := store.MakeDomainFiles("shop-example-com"); err != nil {
		t.Fatalf("MakeDomainFiles: %v", err)
	}

	out, err := app.sourceBackup("shop-example-com", "")
	if err != nil {
		t.Fatalf("sourceBackup: %v", err)
	}

	wantDir := filepath.Join(backupsDir, "shop-example-com", "source")
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
	domain = "shop-example-com"
	if err := store.MakeDomainFiles(domain); err != nil {
		t.Fatalf("MakeDomainFiles: %v", err)
	}
	domainDir := store.DomainDir(domain)

	if err := os.MkdirAll(filepath.Join(domainDir, "node_modules"), 0o755); err != nil {
		t.Fatalf("MkdirAll(node_modules): %v", err)
	}
	files := map[string]string{
		filepath.Join(domainDir, "node_modules", "pkg.js"): "module.exports = {}",
		filepath.Join(domainDir, "debug.log"):              "some debug output",
		filepath.Join(domainDir, "keep.txt"):               "keep me",
		filepath.Join(domainDir, ".gitignore"):             "node_modules/\n*.log\n",
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

// newDotWorTestApp builds an App whose prompts read from answers, plus
// the two directories preserveDotWor works between: dest (the service
// tree about to be replaced) and tmp (the fresh clone that will take
// its place).
func newDotWorTestApp(t *testing.T, answers string) (app *App, dest, tmp string) {
	t.Helper()
	app = newTestServiceApp(t)
	app.In = bufio.NewReader(strings.NewReader(answers))
	root := t.TempDir()
	dest, tmp = filepath.Join(root, "dest"), filepath.Join(root, "tmp")
	for _, dir := range []string{dest, tmp} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return app, dest, tmp
}

func writeDotWorFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, ".wor", name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Keeping .wor is the default, because a clone replaces the whole tree
// and the pre-clone backup honors .gitignore -- so pressing enter must
// never be the answer that loses the admin's own configuration.
func TestPreserveDotWorKeepsByDefault(t *testing.T) {
	app, dest, tmp := newDotWorTestApp(t, "\n")
	writeDotWorFile(t, dest, "php.ini", "memory_limit = 512M\n")

	if err := app.preserveDotWor(dest, tmp); err != nil {
		t.Fatalf("preserveDotWor() error = %v", err)
	}

	got, err := os.ReadFile(filepath.Join(tmp, ".wor", "php.ini"))
	if err != nil {
		t.Fatalf("current .wor was not carried into the new tree: %v", err)
	}
	if string(got) != "memory_limit = 512M\n" {
		t.Errorf("php.ini = %q, want the current tree's copy", got)
	}
}

// When both trees have a .wor, wor cannot know which hand-written
// config is the wanted one, so the repo's copy is set aside rather than
// merged or silently dropped.
func TestPreserveDotWorSetsRepoCopyAside(t *testing.T) {
	app, dest, tmp := newDotWorTestApp(t, "1\n")
	writeDotWorFile(t, dest, "php.ini", "memory_limit = 512M\n")
	writeDotWorFile(t, tmp, "php.ini", "memory_limit = 1G\n")

	if err := app.preserveDotWor(dest, tmp); err != nil {
		t.Fatalf("preserveDotWor() error = %v", err)
	}

	live, err := os.ReadFile(filepath.Join(tmp, ".wor", "php.ini"))
	if err != nil || string(live) != "memory_limit = 512M\n" {
		t.Errorf("live php.ini = %q (err %v), want the current tree's copy", live, err)
	}
	saved, err := os.ReadFile(filepath.Join(tmp, ".wor.new", "php.ini"))
	if err != nil || string(saved) != "memory_limit = 1G\n" {
		t.Errorf(".wor.new/php.ini = %q (err %v), want the repo's copy", saved, err)
	}
}

// Deleting is destructive, so it takes the explicit choice AND the
// confirmation -- and a "no" at the confirmation returns to the menu
// rather than falling through to the delete.
func TestPreserveDotWorDeleteNeedsConfirmation(t *testing.T) {
	app, dest, tmp := newDotWorTestApp(t, "2\nn\n1\n")
	writeDotWorFile(t, dest, "php.ini", "memory_limit = 512M\n")

	if err := app.preserveDotWor(dest, tmp); err != nil {
		t.Fatalf("preserveDotWor() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(tmp, ".wor", "php.ini")); err != nil {
		t.Errorf("declining the confirmation should have kept .wor: %v", err)
	}
}

func TestPreserveDotWorDeletes(t *testing.T) {
	app, dest, tmp := newDotWorTestApp(t, "2\ny\n")
	writeDotWorFile(t, dest, "php.ini", "memory_limit = 512M\n")

	if err := app.preserveDotWor(dest, tmp); err != nil {
		t.Fatalf("preserveDotWor() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(tmp, ".wor")); !os.IsNotExist(err) {
		t.Errorf("the new tree should have no .wor, stat err = %v", err)
	}
}

// A service tree without a .wor must not prompt at all -- `wor source
// clone` is used on plenty of them.
func TestPreserveDotWorSilentWhenAbsent(t *testing.T) {
	app, dest, tmp := newDotWorTestApp(t, "")

	if err := app.preserveDotWor(dest, tmp); err != nil {
		t.Fatalf("preserveDotWor() error = %v", err)
	}

	if out := app.Err.(*bytes.Buffer).String(); out != "" {
		t.Errorf("expected no prompt when there is no .wor, got %q", out)
	}
}
