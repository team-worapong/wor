package cliapp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"wor/internal/domainmodel"
	"wor/internal/osutil"
	"wor/internal/phpfpm"
)

// phpSettingsExampleName is the reference file wor writes next to
// php.ini. Like the web-server scaffold's default.conf.example it is
// documentation, not config: wor rewrites it whenever it renders a
// pool, and nothing ever reads it back.
const phpSettingsExampleName = phpfpm.SettingsFileName + ".example"

// phpSettingsPath is where a service's own PHP ini overrides live:
// <serviceDir>/.wor/php.ini, alongside the .wor/<nginx|apache>
// directories the generated vhost already includes from. Deliberately
// outside the document root, so the web server can never serve it.
func (a *App) phpSettingsPath(domain, service string) string {
	return filepath.Join(a.phpSettingsDir(domain, service), phpfpm.SettingsFileName)
}

// phpPoolSettingsPath is the companion file that tunes the pool's
// process manager: <serviceDir>/.wor/php-fpm.ini. Two files because
// they configure two different layers -- see internal/phpfpm's
// poolsettings.go.
func (a *App) phpPoolSettingsPath(domain, service string) string {
	return filepath.Join(a.phpSettingsDir(domain, service), phpfpm.PoolSettingsFileName)
}

func (a *App) phpSettingsDir(domain, service string) string {
	return filepath.Join(a.Store.ServiceDir(domain, service), ".wor")
}

// loadPHPSettings reads and validates both of domain/service's settings
// files. A missing file yields no settings and no error -- both are
// optional. Every other problem is an error the caller must surface: an
// override that was asked for and silently dropped is exactly what this
// feature exists to prevent.
func (a *App) loadPHPSettings(domain, service string) ([]phpfpm.Setting, []phpfpm.PoolSetting, error) {
	settings, err := phpfpm.LoadSettings(a.phpSettingsPath(domain, service))
	if err != nil {
		return nil, nil, err
	}
	poolSettings, err := phpfpm.LoadPoolSettings(a.phpPoolSettingsPath(domain, service))
	if err != nil {
		return nil, nil, err
	}
	return settings, poolSettings, nil
}

// checkPHPSettings validates domain/service's php.ini without applying
// it, and warns when a file exists that nothing will ever read.
//
// `wor deploy` calls this before its first side effect (see cmdDeploy):
// a typo in php.ini should stop the deploy while the tree is still
// untouched, not after the pull, the npm build and the restart have
// already happened. It cannot be the only check, though -- the pull
// itself can bring in a new php.ini, which is why cmdDeploy still
// treats the later rewritePHPPool failure as fatal.
//
// The service's pool is checked BEFORE the file is parsed, and that
// order is the whole point. Only a php service with a pool of its own
// ever reads this file, so for anything else -- a node service that
// inherited a stray php.ini when its tree was copied from somewhere --
// refusing to deploy would block work the file has no bearing on. The
// hard-error-on-a-bad-key rule exists so a setting is never silently
// dropped; where no setting was going to be applied in the first place,
// failing protects nothing. It says so and gets out of the way.
func (a *App) checkPHPSettings(domain, service string) error {
	if !a.usesPerServicePHPFPM(domain, service) {
		var present []string
		for _, path := range []string{a.phpSettingsPath(domain, service), a.phpPoolSettingsPath(domain, service)} {
			if _, err := os.Stat(path); err == nil {
				present = append(present, path)
			}
		}
		if present == nil {
			return nil
		}
		for _, path := range present {
			a.warn("%s is not applied to anything and was not checked", path)
		}
		if domainmodel.TemplateRequiresPHP(a.Store.GetServiceType(domain, service)) {
			a.info("This service uses the host-wide PHP_FPM_ENDPOINT; only a service with its own php-fpm pool gets per-service PHP settings.")
		} else {
			a.info("Per-service PHP settings apply to php services with their own php-fpm pool -- this one is %s.", a.Store.GetServiceType(domain, service))
		}
		return nil
	}
	if _, _, err := a.loadPHPSettings(domain, service); err != nil {
		return err
	}
	return nil
}

// usesPerServicePHPFPM reports whether domain/service has a pool of its
// own (as opposed to the legacy host-wide PHP_FPM_ENDPOINT, or not
// being a php service at all).
func (a *App) usesPerServicePHPFPM(domain, service string) bool {
	return a.Store.GetServicePHPVersion(domain, service) != ""
}

// phpSettingsState is what a read-only inspection can say about a
// service's PHP settings: what its php.ini asks for, what its live pool
// file actually carries, and whether those two agree.
//
// The gap between them is the failure mode this type exists for. wor
// applies php.ini on deploy, so an admin who edits the file and does
// not deploy has a service whose configuration says one thing while the
// running pool does another, with nothing on either side looking wrong.
type phpSettingsState struct {
	Files    []phpSettingsFile // the two .wor files, whether or not they exist
	Err      error             // set when a file exists but does not parse, or the pool cannot be built
	UpToDate bool              // whether the live pool file matches what these files render to
	PoolRead bool              // false when the pool file could not be read (never an error here)
}

// phpSettingsFile is one of the two `.wor` settings files, as an
// inspection sees it: where it is, whether it exists, and the pool
// directives it asks for.
type phpSettingsFile struct {
	Path   string
	Exists bool
	Lines  []string
}

// Drifted reports whether the live pool disagrees with the service's
// `.wor` files. Only meaningful once they parsed and the pool file could
// be read -- with either missing there is nothing to compare, and
// guessing would mean reporting drift that may not exist.
func (s phpSettingsState) Drifted() bool {
	return s.Err == nil && s.PoolRead && !s.UpToDate
}

// poolSettingSourceLines renders pool settings as the `key = value`
// lines the file itself carries, for showing what a service asked for.
// Deliberately not phpfpm.PoolSettingLines, which answers the different
// question of what the pool's process-manager block becomes.
func poolSettingSourceLines(settings []phpfpm.PoolSetting) []string {
	lines := make([]string, 0, len(settings))
	for _, s := range settings {
		lines = append(lines, s.Key+" = "+s.Value)
	}
	return lines
}

// Configured reports whether either file asks for anything at all.
func (s phpSettingsState) Configured() bool {
	for _, f := range s.Files {
		if len(f.Lines) > 0 {
			return true
		}
	}
	return false
}

// readPHPSettingsState gathers that comparison. Read-only and never
// elevating, so `wor info`, `wor diagnose` and `wor health` can all call
// it: an unreadable pool file leaves PoolRead false rather than
// prompting for sudo or failing.
//
// The drift answer compares the WHOLE rendered pool file against the one
// on disk, not just the settings lines. That covers both `.wor` files
// and a pool file edited by hand, in one comparison -- and it is exactly
// the comparison `wor deploy` uses to decide whether writing is worth
// doing at all, so the two can never disagree about what "up to date"
// means.
func (a *App) readPHPSettingsState(domain, service string, svc *domainmodel.Service, v phpfpm.Version) phpSettingsState {
	state := phpSettingsState{Files: []phpSettingsFile{
		{Path: a.phpSettingsPath(domain, service)},
		{Path: a.phpPoolSettingsPath(domain, service)},
	}}
	for i := range state.Files {
		if _, err := os.Stat(state.Files[i].Path); err == nil {
			state.Files[i].Exists = true
		}
	}

	settings, err := phpfpm.LoadSettings(state.Files[0].Path)
	if err != nil {
		state.Err = err
		return state
	}
	state.Files[0].Lines = phpfpm.SettingLines(settings)

	poolSettings, err := phpfpm.LoadPoolSettings(state.Files[1].Path)
	if err != nil {
		state.Err = err
		return state
	}
	// What the FILE asks for, not what the pool ends up with.
	// phpfpm.PoolSettingLines is the renderer: it merges wor's defaults
	// with the file's overrides, so it returns a full process-manager
	// block even for a service that has no php-fpm.ini at all -- which
	// made `wor info` announce a settings file that does not exist and
	// list five directives nobody asked for (found on a real host,
	// 2026-08-21). An inspection has to report what was configured; what
	// is in force is the pool file, and `wor service reload` prints it.
	state.Files[1].Lines = poolSettingSourceLines(poolSettings)

	// A pool wor cannot rebuild -- an incomplete record, most likely a
	// pool created before its group was ever written down -- is "cannot
	// compare", NOT "the settings are invalid". Err is reserved for the
	// settings files themselves, because every caller reports it as
	// "this file is broken and the next deploy will fail", which would
	// be the wrong thing to say about a service that has no settings
	// files at all. The incomplete record is already reported where it
	// belongs, by reapplyOnePHPPoolAccess.
	pool, err := a.buildPHPPool(domain, service, svc)
	if err != nil {
		return state
	}
	pool.Version, pool.Settings, pool.PoolSettings = v, settings, poolSettings
	state.UpToDate, state.PoolRead = phpfpm.PoolUpToDate(pool)
	return state
}

// rewritePHPPool re-renders an existing pool's config file from the
// service's current record plus its current `.wor` settings files, then
// validates and reloads through phpfpm.WritePool. This is what makes an
// edit to those files take effect.
//
// force decides what happens when the pool file on disk already matches
// what this would write. `wor service reload` passes true, because
// somebody typed a command meaning "apply this now" and a silent no-op
// is not an answer to that. `wor deploy` passes false, because it calls
// this on EVERY deploy of a pooled php service: writing anyway would
// mean a privileged write, a `php-fpm -t` and a reload of the shared
// master -- which cycles the workers of every other service under it --
// each time anyone deploys anything, settings changed or not.
//
// Unlike setupPHPPool this creates nothing: no unix user, no group
// grant. It reuses the identity already recorded for the pool, so it
// stays a pure re-render of config wor has written before, and a
// service whose record is incomplete is left alone rather than
// rewritten into something wor would have to guess at.
//
// A service with no pool of its own is not an error -- deploy calls
// this for every service it touches, most of which are not php.
func (a *App) rewritePHPPool(domain, service string, force bool) error {
	cfg, err := a.Store.LoadServices(domain)
	if err != nil {
		return err
	}
	svc := cfg.FindService(service)
	if svc == nil || !svc.UsesPerServicePHPFPM() {
		return nil
	}
	// The service's own record is checked before the host is consulted:
	// an incomplete record is wrong no matter what is installed, and
	// says so more usefully than "PHP 8.3 is missing" would.
	pool, err := a.buildPHPPool(domain, service, svc)
	if err != nil {
		return err
	}
	version, ok := phpfpm.ResolveVersion(svc.PHPVersion)
	if !ok {
		return a.errf("PHP %s is no longer detected on this host, so %s/%s's pool cannot be rewritten", svc.PHPVersion, domain, service)
	}
	pool.Version = version

	settings, poolSettings, err := a.loadPHPSettings(domain, service)
	if err != nil {
		return err
	}
	pool.Settings, pool.PoolSettings = settings, poolSettings

	if !force {
		if upToDate, readable := phpfpm.PoolUpToDate(pool); readable && upToDate {
			return nil
		}
	}
	if err := phpfpm.WritePool(pool); err != nil {
		return err
	}
	a.writePHPSettingsExamples(domain, service, version)
	return nil
}

// buildPHPPool assembles the identity half of an already-created pool:
// which unix user its workers run as, which group reads its document
// root, and who owns its socket. It mirrors setupPHPPool's, minus the
// parts that create things -- on macOS the pool runs as the login user
// (no privilege separation there, see setupPHPPool), on Linux as the
// dedicated user named after the pool, in the group recorded when the
// pool was made. The caller fills in Version and the settings.
func (a *App) buildPHPPool(domain, service string, svc *domainmodel.Service) (phpfpm.Pool, error) {
	pool := phpfpm.Pool{Domain: domain, Service: service}
	if osutil.IsMacOS() {
		u, g, err := phpfpm.CurrentUnixUser()
		if err != nil {
			return phpfpm.Pool{}, err
		}
		pool.User, pool.Group = u, g
		return pool, nil
	}
	// An empty recorded group would render `group = ` into the pool
	// file, which php-fpm rejects -- and a rejected config takes
	// WritePool's rollback path. Refuse to rewrite instead, and point at
	// the repair, exactly as reapplyOnePHPPoolAccess does for the same
	// missing record.
	if svc.PHPPoolGroup == "" {
		return phpfpm.Pool{}, a.errf("service %s/%s has a php-fpm pool but no recorded pool group -- re-create the pool before changing its PHP settings", domain, service)
	}
	pool.User = phpfpm.PoolName(domain, service)
	pool.Group = svc.PHPPoolGroup
	if webUser := webServerRunUser(a.Cfg.HostProviderName()); webUserExists(webUser) {
		pool.ListenOwner, pool.ListenGroup = webUser, webUser
	}
	return pool, nil
}

// writePHPSettingsExamples refreshes the reference file beside each
// settings file, documenting what may go in it. Best-effort, like
// writeCustomConfigScaffold: the settings files themselves are
// optional, so failing to write their documentation must not fail the
// pool write that already succeeded.
func (a *App) writePHPSettingsExamples(domain, service string, version phpfpm.Version) {
	dir := a.phpSettingsDir(domain, service)
	if err := osutil.EnsureDir(dir); err != nil {
		a.warn("could not create %s: %s", dir, err)
		return
	}
	examples := []struct {
		name    string
		content string
	}{
		{phpfpm.SettingsFileName + ".example", renderPHPSettingsExample(a.phpSettingsPath(domain, service), version.Number)},
		{phpfpm.PoolSettingsFileName + ".example", renderPHPPoolSettingsExample(a.phpPoolSettingsPath(domain, service), version.Number)},
	}
	for _, e := range examples {
		path := filepath.Join(dir, e.name)
		if err := osutil.WriteFileAtomic(path, []byte(e.content), 0o644); err != nil {
			a.warn("could not write reference file %s: %s", path, err)
		}
	}
}

// cmdServiceReload implements `wor service reload <domain>/<service>`:
// re-render this service's php-fpm pool from its current `.wor` settings
// files, validate it, reload php-fpm, and print what is now in force.
//
// `wor deploy` does this too, but deploy is the wrong tool for a
// settings-only change: it insists on a git repository (so a service
// whose source was uploaded rather than cloned could not apply its
// settings at all), and it would also reinstall dependencies, rebuild
// and restart for a change that touches none of that.
//
// Deliberately narrow: this reloads *configuration*, not the service.
// Restarting a process is `wor service restart`, and re-rendering a
// vhost is `wor host reload`; folding those together would make one
// verb mean three different amounts of disruption.
func (a *App) cmdServiceReload(domain, service string) error {
	if !a.usesPerServicePHPFPM(domain, service) {
		svcType := a.Store.GetServiceType(domain, service)
		if domainmodel.TemplateRequiresPHP(svcType) {
			return a.errf("%s/%s uses the host-wide PHP_FPM_ENDPOINT, not a pool of its own -- there is no per-service PHP config to reload", domain, service)
		}
		return a.errf("%s/%s is a %s service with no php-fpm pool -- nothing to reload here (process: wor service restart, web server: wor host reload)", domain, service, svcType)
	}
	// force: somebody typed a command meaning "apply this now", so a
	// pool that already matches is still rewritten and reloaded rather
	// than silently skipped.
	if err := a.rewritePHPPool(domain, service, true); err != nil {
		return err
	}

	version := a.Store.GetServicePHPVersion(domain, service)
	settings, poolSettings, err := a.loadPHPSettings(domain, service)
	if err != nil {
		return err
	}
	a.ok("Reloaded php-fpm %s pool for %s/%s", version, domain, service)

	// Print what actually landed, not just "done": the pool wor rewrote
	// is somewhere else on disk, so showing it here is the only
	// confirmation the admin gets that the edit was understood.
	//
	// The process-manager block is always printed, because wor generates
	// one whether or not php-fpm.ini exists -- "these are your pool's
	// current limits" is what somebody running this wants to know. But
	// the heading only names the file when the file is what produced
	// them: naming a path that does not exist reads as "wor read this",
	// which is the mistake this output made until a rehearsal on a real
	// host showed it (2026-08-21).
	source := "wor defaults"
	if len(poolSettings) > 0 {
		source = a.phpPoolSettingsPath(domain, service)
	}
	fmt.Fprintf(a.Out, "Process manager in force (%s):\n", source)
	for _, line := range phpfpm.PoolSettingLines(poolSettings) {
		fmt.Fprintf(a.Out, "  %s\n", line)
	}
	if len(settings) == 0 {
		a.info("No PHP settings applied -- the pool uses PHP %s's own php.ini. Add %s to override.", version, a.phpSettingsPath(domain, service))
		return nil
	}
	fmt.Fprintf(a.Out, "PHP settings in force (%s):\n", a.phpSettingsPath(domain, service))
	for _, line := range phpfpm.SettingLines(settings) {
		fmt.Fprintf(a.Out, "  %s\n", line)
	}
	return nil
}

// printPHPSettingsInfo is `wor info`'s per-service settings block: what
// the service asks for, and a warning when the running pool disagrees.
func (a *App) printPHPSettingsInfo(domain, service string, svc *domainmodel.Service, v phpfpm.Version) {
	state := a.readPHPSettingsState(domain, service, svc, v)
	if state.Err != nil {
		fmt.Fprintf(a.Out, "  Settings  : INVALID -- %s\n", state.Err)
		return
	}
	if !state.Configured() {
		// A file that exists but sets nothing is a different statement
		// from no file at all, and an admin who just wrote one wants to
		// be told which of the two wor is looking at.
		anyExists := false
		for _, f := range state.Files {
			anyExists = anyExists || f.Exists
		}
		if anyExists {
			fmt.Fprintln(a.Out, "  Settings  : none (the .wor settings files set nothing)")
		} else {
			fmt.Fprintf(a.Out, "  Settings  : none (no %s or %s)\n", phpfpm.SettingsFileName, phpfpm.PoolSettingsFileName)
		}
		return
	}
	fmt.Fprintln(a.Out, "  Settings  :")
	for _, f := range state.Files {
		if len(f.Lines) == 0 {
			continue
		}
		fmt.Fprintf(a.Out, "    %s\n", f.Path)
		for _, line := range f.Lines {
			fmt.Fprintf(a.Out, "      %s\n", line)
		}
	}
	if state.Drifted() {
		fmt.Fprintf(a.Out, "    NOT APPLIED -- the running pool differs; run: wor service reload %s/%s\n", domain, service)
	}
}

// phpSettingsHealth is `wor health`'s one-line verdict on a service's
// settings, in the same shape as certificateHealth: a line to print and
// whether it counts as a problem.
//
// Drift belongs in the fleet sweep, not only in the per-target commands.
// Editing a settings file and forgetting to apply it leaves a service
// running configuration nobody can see by reading its repository, and
// `wor info`/`wor diagnose` only find that if somebody already suspects
// that one service. A warning, never a failure: the service is serving
// fine, and the exit code has to stay 0 so cron does not alarm on it.
func (a *App) phpSettingsHealth(ref domainmodel.ServiceRef) (line string, problem bool) {
	svc := ref.Service
	if !svc.UsesPerServicePHPFPM() {
		return "", false
	}
	v, ok := phpfpm.ResolveVersion(svc.PHPVersion)
	if !ok {
		return "", false
	}
	state := a.readPHPSettingsState(ref.Domain, svc.Name, &svc, v)
	switch {
	case state.Err != nil:
		return "php settings: invalid -- the next deploy of this service will fail", true
	case state.Drifted():
		return fmt.Sprintf("php settings: the running pool does not match this service's .wor files (wor service reload %s/%s)", ref.Domain, svc.Name), true
	case state.Configured():
		return "php settings: applied", false
	}
	return "", false
}

// renderPHPSettingsExample builds the reference file for php.ini.
func renderPHPSettingsExample(settingsPath, phpVersion string) string {
	b := newExampleFile("wor per-service PHP settings")
	b.line("; To set PHP options for THIS service only, create:")
	b.line(";")
	b.line(";     " + settingsPath)
	b.line(";")
	b.line("; wor reads that file and renders its settings into this")
	b.line("; service's own php-fpm pool (PHP " + phpVersion + "). It is not read by")
	b.line("; php itself, so only the keys below are supported.")
	b.line(";")
	b.line("; Most become php_value[...], so the application can still")
	b.line("; ini_set() past them; the few PHP classifies as PHP_INI_SYSTEM")
	b.line("; become php_admin_value[...], because php_value cannot set")
	b.line("; those at all.")
	b.keys(phpfpm.AllowedSettingKeys())
	b.example([]string{
		"memory_limit = 512M",
		"upload_max_filesize = 64M",
		"post_max_size = 64M",
		"max_execution_time = 120",
	})
	return b.String()
}

// renderPHPPoolSettingsExample builds the reference file for
// php-fpm.ini.
func renderPHPPoolSettingsExample(settingsPath, phpVersion string) string {
	b := newExampleFile("wor per-service php-fpm pool settings")
	b.line("; To tune the process manager for THIS service only, create:")
	b.line(";")
	b.line(";     " + settingsPath)
	b.line(";")
	b.line("; These are php-fpm pool directives (PHP " + phpVersion + "), written")
	b.line("; into the pool as-is. They govern the worker processes; PHP")
	b.line("; ini settings such as memory_limit go in " + phpfpm.SettingsFileName + ".")
	b.line(";")
	b.line("; Anything not set here keeps wor's default: pm = dynamic with")
	b.line("; pm.max_children = " + fmt.Sprint(phpfpm.DefaultMaxChildren) + ". Setting pm = static or ondemand")
	b.line("; switches the block to the directives that mode uses.")
	b.keys(phpfpm.AllowedPoolSettingKeys())
	b.example([]string{
		"pm = dynamic",
		"pm.max_children = 40",
		"pm.start_servers = 8",
		"pm.min_spare_servers = 4",
		"pm.max_spare_servers = 12",
	})
	return b.String()
}

// exampleFile builds the reference files the two renderers above share:
// same header, same "not loaded" warning, same supported-keys and
// example blocks, so the two can never explain themselves differently.
type exampleFile struct{ b strings.Builder }

func newExampleFile(title string) *exampleFile {
	e := &exampleFile{}
	e.line("; ============================================================")
	e.line("; " + title + " -- REFERENCE ONLY (not loaded)")
	e.line("; ============================================================")
	e.line(";")
	e.line("; This file is NOT active, and wor rewrites it every time it")
	e.line("; renders this service's pool -- do not edit it in place.")
	e.line(";")
	return e
}

func (e *exampleFile) line(s string) { e.b.WriteString(s + "\n") }

func (e *exampleFile) keys(keys []string) {
	e.line(";")
	e.line("; Every line is a plain `key = value` -- no [sections]. An")
	e.line("; unsupported key, a duplicate key or a malformed line is an")
	e.line("; error rather than a skipped line, so a setting is never")
	e.line("; silently dropped.")
	e.line(";")
	e.line("; Supported keys:")
	for _, key := range keys {
		e.line(";   " + key)
	}
}

func (e *exampleFile) example(lines []string) {
	e.line(";")
	e.line("; Example:")
	e.line(";")
	for _, l := range lines {
		e.line(";     " + l)
	}
	e.line(";")
	e.line("; Apply with: wor service reload <domain>/<service>")
	e.line("; (wor deploy applies it too, as part of a deploy.)")
}

func (e *exampleFile) String() string { return e.b.String() }
