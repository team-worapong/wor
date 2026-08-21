package cliapp

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"wor/internal/domainmodel"
	"wor/internal/osutil"
	"wor/internal/phpfpm"
)

// writePHPSettingsFile puts a php.ini into the service's .wor directory,
// the way an admin or a cloned repository would.
func writePHPSettingsFile(t *testing.T, app *App, domain, service, content string) {
	t.Helper()
	path := app.phpSettingsPath(domain, service)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// writePoolSettingsFile puts a php-fpm.ini into the service's .wor
// directory, the companion to writePHPSettingsFile.
func writePoolSettingsFile(t *testing.T, app *App, domain, service, content string) {
	t.Helper()
	path := app.phpPoolSettingsPath(domain, service)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// The settings file must sit in .wor, beside the web-server snippet
// directories -- above the document root, so the web server can never
// serve it to the internet.
func TestPHPSettingsPathIsOutsideTheDocumentRoot(t *testing.T) {
	app := newPoolAccessTestApp(t, "shop-example-com", "site", "php")

	path := app.phpSettingsPath("shop-example-com", "site")

	if filepath.Base(filepath.Dir(path)) != ".wor" {
		t.Errorf("phpSettingsPath() = %q, want it inside a .wor directory", path)
	}
	docRoot := app.phpPoolDocRoot("shop-example-com", "site")
	if strings.HasPrefix(path, docRoot+string(filepath.Separator)) {
		t.Errorf("phpSettingsPath() = %q is inside the document root %q", path, docRoot)
	}
}

func TestCheckPHPSettingsPassesWithNoFile(t *testing.T) {
	app := newPoolAccessTestApp(t, "shop-example-com", "site", "php")

	if err := app.checkPHPSettings("shop-example-com", "site"); err != nil {
		t.Errorf("checkPHPSettings() error = %v, want nil when there is no php.ini", err)
	}
}

// newPooledPHPTestApp registers a php service that has a pool of its
// own, which is the only case where php.ini means anything.
func newPooledPHPTestApp(t *testing.T, domain, service string) *App {
	t.Helper()
	app := newPoolAccessTestApp(t, domain, service, "php")
	if err := app.Store.SetServicePHPFPM(domain, service, "8.3", "wor_"+domain+"_"+service); err != nil {
		t.Fatalf("SetServicePHPFPM: %v", err)
	}
	return app
}

// This is deploy's pre-flight check: an unusable php.ini has to fail
// before the pull, the build and the restart, not after.
func TestCheckPHPSettingsFailsOnUnsupportedKey(t *testing.T) {
	app := newPooledPHPTestApp(t, "shop-example-com", "site")
	writePHPSettingsFile(t, app, "shop-example-com", "site", "error_log = /etc/shadow\n")

	err := app.checkPHPSettings("shop-example-com", "site")

	if err == nil {
		t.Fatal("checkPHPSettings() accepted error_log")
	}
	if !strings.Contains(err.Error(), "error_log") {
		t.Errorf("error = %q, want it to name the offending key", err)
	}
}

// A php.ini under a service that uses the host-wide PHP_FPM_ENDPOINT is
// read by nothing at all. That is not an error -- the deploy should
// still succeed -- but staying silent about it would leave the admin
// waiting for settings that are never coming.
func TestCheckPHPSettingsWarnsWhenServiceHasNoPool(t *testing.T) {
	app := newPoolAccessTestApp(t, "shop-example-com", "site", "php")
	writePHPSettingsFile(t, app, "shop-example-com", "site", "memory_limit = 512M\n")

	if err := app.checkPHPSettings("shop-example-com", "site"); err != nil {
		t.Fatalf("checkPHPSettings() error = %v, want nil", err)
	}

	out := app.Err.(*bytes.Buffer).String() + app.Out.(*bytes.Buffer).String()
	if !strings.Contains(out, "not applied to anything") {
		t.Errorf("expected a warning that the file is not applied, got %q", out)
	}
	if !strings.Contains(out, "PHP_FPM_ENDPOINT") {
		t.Errorf("a php service without a pool should be told why, got %q", out)
	}
}

// A stray php.ini in a node service's tree -- copied in with the rest
// of the tree, most likely -- is applied to nothing, so validating it
// would only block a deploy it has no bearing on. It is reported and
// skipped, not parsed.
func TestCheckPHPSettingsIgnoresInvalidFileOnNonPHPService(t *testing.T) {
	app := newPoolAccessTestApp(t, "shop-example-com", "webapp", "node")
	writePHPSettingsFile(t, app, "shop-example-com", "webapp", "this is not = a valid [ini\n")

	if err := app.checkPHPSettings("shop-example-com", "webapp"); err != nil {
		t.Fatalf("checkPHPSettings() error = %v, want nil -- a node service's php.ini must not fail its deploy", err)
	}

	out := app.Err.(*bytes.Buffer).String() + app.Out.(*bytes.Buffer).String()
	if !strings.Contains(out, "not applied to anything") {
		t.Errorf("expected a warning that the file is ignored, got %q", out)
	}
	if strings.Contains(out, "PHP_FPM_ENDPOINT") {
		t.Errorf("a node service must not be told about php-fpm endpoints, got %q", out)
	}
}

// No file, no noise: `wor deploy` runs this for every service, and the
// overwhelming majority have no php.ini at all.
func TestCheckPHPSettingsSilentOnNonPHPServiceWithoutFile(t *testing.T) {
	app := newPoolAccessTestApp(t, "shop-example-com", "webapp", "node")

	if err := app.checkPHPSettings("shop-example-com", "webapp"); err != nil {
		t.Fatalf("checkPHPSettings() error = %v, want nil", err)
	}

	if out := app.Err.(*bytes.Buffer).String() + app.Out.(*bytes.Buffer).String(); out != "" {
		t.Errorf("expected no output for a service with no php.ini, got %q", out)
	}
}

// rewritePHPPool runs on every deploy, including deploys of node, go,
// python and static services. Those have no pool to rewrite and must
// not be touched.
func TestRewritePHPPoolSkipsServicesWithoutAPool(t *testing.T) {
	app := newPoolAccessTestApp(t, "shop-example-com", "webapp", "node")

	if err := app.rewritePHPPool("shop-example-com", "webapp", false); err != nil {
		t.Errorf("rewritePHPPool() error = %v, want nil for a service with no pool", err)
	}
}

// A pool whose group was never recorded cannot be re-rendered: writing
// `group = ` would fail php-fpm's own config test, and WritePool would
// then roll the pool file back. Refusing up front keeps a bad record
// from turning into a lost pool.
func TestRewritePHPPoolRefusesWhenPoolGroupMissing(t *testing.T) {
	if osutil.IsMacOS() {
		t.Skip("per-service pool users are Linux-only (see setupPHPPool)")
	}
	app := newPoolAccessTestApp(t, "shop-example-com", "site", "php")
	if err := app.Store.SetServicePHPFPM("shop-example-com", "site", "8.3", ""); err != nil {
		t.Fatalf("SetServicePHPFPM: %v", err)
	}

	err := app.rewritePHPPool("shop-example-com", "site", true)

	if err == nil {
		t.Fatal("rewritePHPPool() succeeded for a pool with no recorded group")
	}
	if !strings.Contains(err.Error(), "pool group") {
		t.Errorf("error = %q, want it to name the missing pool group", err)
	}
}

// The reference file documents the allowlist, so every supported key
// has to appear in it -- otherwise adding a key to allowedSettings
// would leave the documentation quietly out of date.
func TestPHPSettingsExampleListsEverySupportedKey(t *testing.T) {
	content := renderPHPSettingsExample("/srv/example/.wor/php.ini", "8.3")

	for _, key := range phpfpm.AllowedSettingKeys() {
		if !strings.Contains(content, key) {
			t.Errorf("reference file does not mention supported key %q", key)
		}
	}
	if !strings.Contains(content, "/srv/example/.wor/php.ini") {
		t.Error("reference file should name the path of the real settings file")
	}
}

// writeTestPool renders and writes the pool file a service would have
// after its settings were applied, and returns the Version pointing at
// it -- so a test can then change a settings file and see drift.
func writeTestPool(t *testing.T, app *App, domain, service string) phpfpm.Version {
	t.Helper()
	v := phpfpm.Version{Number: "8.3", PoolDir: t.TempDir(), SockDir: t.TempDir()}
	cfg, err := app.Store.LoadServices(domain)
	if err != nil {
		t.Fatalf("LoadServices: %v", err)
	}
	svc := cfg.FindService(service)
	pool, err := app.buildPHPPool(domain, service, svc)
	if err != nil {
		t.Fatalf("buildPHPPool: %v", err)
	}
	pool.Version = v
	pool.Settings, pool.PoolSettings, err = app.loadPHPSettings(domain, service)
	if err != nil {
		t.Fatalf("loadPHPSettings: %v", err)
	}
	if err := os.WriteFile(phpfpm.PoolFilePath(v, domain, service), []byte(phpfpm.PoolFileContent(pool)), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return v
}

func testServiceRecord(t *testing.T, app *App, domain, service string) *domainmodel.Service {
	t.Helper()
	cfg, err := app.Store.LoadServices(domain)
	if err != nil {
		t.Fatalf("LoadServices: %v", err)
	}
	svc := cfg.FindService(service)
	if svc == nil {
		t.Fatalf("service %s/%s not found", domain, service)
	}
	return svc
}

func TestPHPSettingsStateNoDriftWhenPoolMatches(t *testing.T) {
	app := newPooledPHPTestApp(t, "shop-example-com", "site")
	writePHPSettingsFile(t, app, "shop-example-com", "site", "memory_limit = 512M\n")
	v := writeTestPool(t, app, "shop-example-com", "site")

	state := app.readPHPSettingsState("shop-example-com", "site", testServiceRecord(t, app, "shop-example-com", "site"), v)

	if state.Err != nil {
		t.Fatalf("state.Err = %v, want nil", state.Err)
	}
	if !state.Configured() {
		t.Fatal("Configured() = false, want true")
	}
	if state.Drifted() {
		t.Error("Drifted() = true for a pool rendered from these very files")
	}
}

// The case the whole drift check exists for: a settings file was edited
// and nobody applied it, so the file and the running pool disagree while
// neither looks wrong on its own.
func TestPHPSettingsStateDetectsDrift(t *testing.T) {
	app := newPooledPHPTestApp(t, "shop-example-com", "site")
	writePHPSettingsFile(t, app, "shop-example-com", "site", "memory_limit = 512M\n")
	v := writeTestPool(t, app, "shop-example-com", "site")
	writePHPSettingsFile(t, app, "shop-example-com", "site", "memory_limit = 1G\n")

	if !app.readPHPSettingsState("shop-example-com", "site", testServiceRecord(t, app, "shop-example-com", "site"), v).Drifted() {
		t.Error("Drifted() = false after php.ini changed without the pool being rewritten")
	}
}

// Drift is about the whole pool file, so a change to the process
// manager counts even though it touches no php_value line.
func TestPHPSettingsStateDetectsPoolTuningDrift(t *testing.T) {
	app := newPooledPHPTestApp(t, "shop-example-com", "site")
	v := writeTestPool(t, app, "shop-example-com", "site")
	writePoolSettingsFile(t, app, "shop-example-com", "site", "pm.max_children = 40\n")

	if !app.readPHPSettingsState("shop-example-com", "site", testServiceRecord(t, app, "shop-example-com", "site"), v).Drifted() {
		t.Error("Drifted() = false after php-fpm.ini changed without the pool being rewritten")
	}
}

// An unreadable pool file must not be reported as drift: wor cannot see
// what is applied, which is a different statement from "the wrong thing
// is applied".
func TestPHPSettingsStateNoDriftWhenPoolUnreadable(t *testing.T) {
	app := newPooledPHPTestApp(t, "shop-example-com", "site")
	writePHPSettingsFile(t, app, "shop-example-com", "site", "memory_limit = 512M\n")
	v := phpfpm.Version{Number: "8.3", PoolDir: t.TempDir(), SockDir: t.TempDir()}

	state := app.readPHPSettingsState("shop-example-com", "site", testServiceRecord(t, app, "shop-example-com", "site"), v)

	if state.PoolRead {
		t.Fatal("PoolRead = true for a pool file that does not exist")
	}
	if state.Drifted() {
		t.Error("Drifted() = true when the pool file could not be read")
	}
}

func TestPrintPHPSettingsInfoFlagsDrift(t *testing.T) {
	app := newPooledPHPTestApp(t, "shop-example-com", "site")
	writePHPSettingsFile(t, app, "shop-example-com", "site", "memory_limit = 512M\n")
	v := writeTestPool(t, app, "shop-example-com", "site")
	writePHPSettingsFile(t, app, "shop-example-com", "site", "memory_limit = 1G\n")

	app.printPHPSettingsInfo("shop-example-com", "site", testServiceRecord(t, app, "shop-example-com", "site"), v)

	out := app.Out.(*bytes.Buffer).String()
	if !strings.Contains(out, "php_value[memory_limit] = 1G") {
		t.Errorf("info should show what the service asks for, got %q", out)
	}
	if !strings.Contains(out, "wor service reload") {
		t.Errorf("info should point at the command that applies it, got %q", out)
	}
}

// `wor service reload` is about PHP pool configuration only. Pointing a
// node service's admin at the commands that do apply keeps the verb
// from quietly meaning "restart" for some service types.
func TestCmdServiceReloadRefusesServiceWithoutAPool(t *testing.T) {
	app := newPoolAccessTestApp(t, "shop-example-com", "webapp", "node")

	err := app.cmdServiceReload("shop-example-com", "webapp")

	if err == nil {
		t.Fatal("cmdServiceReload() succeeded for a node service")
	}
	if !strings.Contains(err.Error(), "wor service restart") {
		t.Errorf("error = %q, want it to name the command that does apply", err)
	}
}

func TestCmdServiceReloadRefusesLegacyPHPService(t *testing.T) {
	app := newPoolAccessTestApp(t, "shop-example-com", "site", "php")

	err := app.cmdServiceReload("shop-example-com", "site")

	if err == nil {
		t.Fatal("cmdServiceReload() succeeded for a php service with no pool of its own")
	}
	if !strings.Contains(err.Error(), "PHP_FPM_ENDPOINT") {
		t.Errorf("error = %q, want it to explain that there is no per-service pool", err)
	}
}

// The deploy path must not write, validate and reload a pool that
// already says what it should -- see rewritePHPPool's force parameter.
// The decision itself is phpfpm.PoolUpToDate, tested directly in that
// package; what this pins is that rewritePHPPool asks it at all, by
// checking that an up-to-date pool is left untouched by a force=false
// call that would otherwise have to resolve a live PHP version.
func TestRewritePHPPoolSkipsAnUnchangedPool(t *testing.T) {
	if osutil.IsMacOS() {
		t.Skip("per-service pool users are Linux-only (see setupPHPPool)")
	}
	app := newPooledPHPTestApp(t, "shop-example-com", "site")
	writePHPSettingsFile(t, app, "shop-example-com", "site", "memory_limit = 512M\n")
	v := writeTestPool(t, app, "shop-example-com", "site")

	svc := testServiceRecord(t, app, "shop-example-com", "site")
	pool, err := app.buildPHPPool("shop-example-com", "site", svc)
	if err != nil {
		t.Fatal(err)
	}
	pool.Version = v
	pool.Settings, pool.PoolSettings, err = app.loadPHPSettings("shop-example-com", "site")
	if err != nil {
		t.Fatal(err)
	}

	upToDate, readable := phpfpm.PoolUpToDate(pool)
	if !readable || !upToDate {
		t.Fatalf("PoolUpToDate() = (%t, %t) for a pool rendered from these files, want (true, true) -- a deploy would rewrite and reload it", upToDate, readable)
	}
}

// A pooled service whose record predates the pool group is a real
// condition on upgraded hosts. It must not be reported as "your
// settings file is invalid, the next deploy will fail" -- especially
// not for a service that has no settings files at all.
func TestPHPSettingsHealthQuietOnIncompletePoolRecord(t *testing.T) {
	if osutil.IsMacOS() {
		t.Skip("per-service pool users are Linux-only (see setupPHPPool)")
	}
	app := newPoolAccessTestApp(t, "shop-example-com", "site", "php")
	if err := app.Store.SetServicePHPFPM("shop-example-com", "site", "8.3", ""); err != nil {
		t.Fatalf("SetServicePHPFPM: %v", err)
	}
	svc := testServiceRecord(t, app, "shop-example-com", "site")
	v := phpfpm.Version{Number: "8.3", PoolDir: t.TempDir(), SockDir: t.TempDir()}

	state := app.readPHPSettingsState("shop-example-com", "site", svc, v)

	if state.Err != nil {
		t.Errorf("state.Err = %v -- an incomplete pool record is not a broken settings file", state.Err)
	}
	if state.Drifted() {
		t.Error("Drifted() = true when wor could not rebuild the pool to compare against")
	}
}
