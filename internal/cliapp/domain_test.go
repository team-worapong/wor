package cliapp

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"wor/internal/config"
	"wor/internal/domainmodel"
)

// newTestDomainApp builds an *App wired to a temp WOR_HOME-style layout,
// with a.In pre-loaded with scripted answers (one per line) for the
// Y/n prompts `wor domain remove` asks.
func newTestDomainApp(t *testing.T, answers string) *App {
	t.Helper()
	root := t.TempDir()
	store := domainmodel.NewStore(filepath.Join(root, "domains"))
	return &App{
		Cfg: &config.Config{
			Domains: filepath.Join(root, "domains"),
			Logs:    filepath.Join(root, "logs"),
			Backups: filepath.Join(root, "backups"),
		},
		Store: store,
		Out:   &bytes.Buffer{},
		Err:   &bytes.Buffer{},
		In:    bufio.NewReader(strings.NewReader(answers)),
	}
}

// TestCmdDomainRemoveBlockedByServices guards the new hard block: a
// domain that still has any registered service (even a stopped one)
// must refuse removal outright -- no --cascade/--force escape hatch --
// and name the offending service(s) plus the fix.
func TestCmdDomainRemoveBlockedByServices(t *testing.T) {
	app := newTestDomainApp(t, "")
	domain := "shop-example"
	if err := app.Store.MakeDomainFiles(domain); err != nil {
		t.Fatalf("MakeDomainFiles: %v", err)
	}
	if err := app.Store.AddService(domain, "webapp", "", 3000, "node", ""); err != nil {
		t.Fatalf("AddService: %v", err)
	}

	err := app.cmdDomain([]string{"remove", domain})
	if err == nil {
		t.Fatal("expected an error when the domain still has a registered service")
	}
	errOut := app.Err.(*bytes.Buffer).String()
	if !strings.Contains(errOut, "webapp") {
		t.Errorf("expected the blocked message to name the service, got:\n%s", errOut)
	}
	if !strings.Contains(errOut, "wor service remove shop-example/webapp") {
		t.Errorf("expected the blocked message to suggest the fix command, got:\n%s", errOut)
	}
	if _, statErr := os.Stat(app.Store.DomainDir(domain)); statErr != nil {
		t.Error("domain folder should not have been touched while removal is blocked")
	}
}

// setupDomainWithLogsAndBackups registers a domain with both its logs
// and backups directories present, so all three "wor domain remove"
// prompts (backups, logs, web data -- in that order) fire.
func setupDomainWithLogsAndBackups(t *testing.T, app *App, domain string) (logsDir, backupsDir string) {
	t.Helper()
	if err := app.Store.MakeDomainFiles(domain); err != nil {
		t.Fatalf("MakeDomainFiles: %v", err)
	}
	logsDir = filepath.Join(app.Cfg.Logs, domain)
	backupsDir = filepath.Join(app.Cfg.Backups, domain)
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(logs): %v", err)
	}
	if err := os.MkdirAll(backupsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(backups): %v", err)
	}
	return logsDir, backupsDir
}

// TestCmdDomainRemoveCommitsAllOnWebDataYes covers the new order and
// commit semantics: backups and logs are asked about (and recorded)
// first, but only actually removed once Web Data -- asked last -- is
// answered yes.
func TestCmdDomainRemoveCommitsAllOnWebDataYes(t *testing.T) {
	// backups=y, logs=y, web data=y
	app := newTestDomainApp(t, "y\ny\ny\n")
	domain := "shop-example"
	logsDir, backupsDir := setupDomainWithLogsAndBackups(t, app, domain)

	if err := app.cmdDomain([]string{"remove", domain}); err != nil {
		t.Fatalf("cmdDomain remove: %v", err)
	}

	if _, err := os.Stat(backupsDir); !os.IsNotExist(err) {
		t.Error("backups should have been removed (answered y, and web data committed with y)")
	}
	if _, err := os.Stat(logsDir); !os.IsNotExist(err) {
		t.Error("logs should have been removed (answered y, and web data committed with y)")
	}
	if _, err := os.Stat(app.Store.DomainDir(domain)); !os.IsNotExist(err) {
		t.Error("web data should have been removed (answered y)")
	}
}

// TestCmdDomainRemoveWebDataNoCancelsEverything covers the key new
// rule: answering "n" to Web Data (the last prompt) cancels the WHOLE
// batch -- backups/logs are NOT removed even though they were each
// answered "y", because Web Data is the final commit gate.
func TestCmdDomainRemoveWebDataNoCancelsEverything(t *testing.T) {
	// backups=y, logs=y, web data=n
	app := newTestDomainApp(t, "y\ny\nn\n")
	domain := "shop-example"
	logsDir, backupsDir := setupDomainWithLogsAndBackups(t, app, domain)

	if err := app.cmdDomain([]string{"remove", domain}); err != nil {
		t.Fatalf("cmdDomain remove: %v", err)
	}

	if _, err := os.Stat(backupsDir); err != nil {
		t.Error("backups should NOT have been removed -- web data was declined, cancelling the batch")
	}
	if _, err := os.Stat(logsDir); err != nil {
		t.Error("logs should NOT have been removed -- web data was declined, cancelling the batch")
	}
	if _, err := os.Stat(app.Store.DomainDir(domain)); err != nil {
		t.Error("web data should NOT have been removed (answered n)")
	}
}

// TestCmdDomainRemoveKeepsBackupsAndLogsWhenDeclined covers declining
// backups/logs individually while still committing web data: each
// keep/remove choice made earlier is still honored once Web Data says
// yes.
func TestCmdDomainRemoveKeepsBackupsAndLogsWhenDeclined(t *testing.T) {
	// backups=n, logs=n, web data=y
	app := newTestDomainApp(t, "n\nn\ny\n")
	domain := "shop-example"
	logsDir, backupsDir := setupDomainWithLogsAndBackups(t, app, domain)

	if err := app.cmdDomain([]string{"remove", domain}); err != nil {
		t.Fatalf("cmdDomain remove: %v", err)
	}

	if _, err := os.Stat(backupsDir); err != nil {
		t.Error("backups should still exist (answered n)")
	}
	if _, err := os.Stat(logsDir); err != nil {
		t.Error("logs should still exist (answered n)")
	}
	if _, err := os.Stat(app.Store.DomainDir(domain)); !os.IsNotExist(err) {
		t.Error("web data should have been removed (answered y)")
	}
}

// TestCmdDomainRemoveSkipsPromptWhenPathMissing checks that the
// backups/logs prompts are only asked when those directories actually
// exist -- only one answer is scripted (for Web Data, the only prompt
// left once backups/logs don't exist), so if either of the other two
// prompts fired anyway, it would either block on empty input or
// consume the wrong scripted answer and fail this test.
func TestCmdDomainRemoveSkipsPromptWhenPathMissing(t *testing.T) {
	app := newTestDomainApp(t, "n\n")
	domain := "shop-example"
	if err := app.Store.MakeDomainFiles(domain); err != nil {
		t.Fatalf("MakeDomainFiles: %v", err)
	}

	if err := app.cmdDomain([]string{"remove", domain}); err != nil {
		t.Fatalf("cmdDomain remove: %v", err)
	}
	if _, err := os.Stat(app.Store.DomainDir(domain)); err != nil {
		t.Error("web data should still exist (answered n)")
	}
}

// TestCmdDomainRemoveDefaultYesOnEmptyAnswer covers confirmYN's "[Y/n]"
// convention: pressing enter with no input means yes.
func TestCmdDomainRemoveDefaultYesOnEmptyAnswer(t *testing.T) {
	app := newTestDomainApp(t, "\n")
	domain := "shop-example"
	if err := app.Store.MakeDomainFiles(domain); err != nil {
		t.Fatalf("MakeDomainFiles: %v", err)
	}

	if err := app.cmdDomain([]string{"remove", domain}); err != nil {
		t.Fatalf("cmdDomain remove: %v", err)
	}
	if _, err := os.Stat(app.Store.DomainDir(domain)); !os.IsNotExist(err) {
		t.Error("web data should have been removed (empty answer defaults to yes)")
	}
}

// TestCmdDomainRemoveRepromptsOnInvalidAnswer covers confirmYN's
// stricter validation (unlike confirmYesDefaultYes elsewhere in the
// CLI, which silently treats any unrecognized input as "no"): garbage
// input must print an error and re-prompt on the same question rather
// than being misread as an answer.
func TestCmdDomainRemoveRepromptsOnInvalidAnswer(t *testing.T) {
	// Web Data prompt: "asdf" (invalid, reprompt), "maybe" (invalid,
	// reprompt), "n" (valid -> keep).
	app := newTestDomainApp(t, "asdf\nmaybe\nn\n")
	domain := "shop-example"
	if err := app.Store.MakeDomainFiles(domain); err != nil {
		t.Fatalf("MakeDomainFiles: %v", err)
	}

	if err := app.cmdDomain([]string{"remove", domain}); err != nil {
		t.Fatalf("cmdDomain remove: %v", err)
	}
	if _, err := os.Stat(app.Store.DomainDir(domain)); err != nil {
		t.Error("web data should still exist (final valid answer was n)")
	}
	errOut := app.Err.(*bytes.Buffer).String()
	if strings.Count(errOut, "Please answer Y, y, N, or n.") != 2 {
		t.Errorf("expected exactly 2 reprompt errors (for \"asdf\" and \"maybe\"), got:\n%s", errOut)
	}
}
