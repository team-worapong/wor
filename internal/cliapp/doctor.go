package cliapp

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"wor/internal/dbbackup"
	"wor/internal/hostprovider"
	"wor/internal/hostsfile"
	"wor/internal/osutil"
	"wor/internal/phpfpm"
	"wor/internal/pm2"
	"wor/internal/systemd"
)

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

func (a *App) workspaceInitialized() bool {
	for _, d := range []string{a.Cfg.WorHome, a.Cfg.Domains, a.Cfg.Backups, a.Cfg.Configs, a.Cfg.Logs, a.Cfg.SSL} {
		if !dirExists(d) {
			return false
		}
	}
	return true
}

// statusLine prints one checklist line with a ✓/⚠/✗ glyph, colored
// green/yellow/red on a TTY (plain glyph, no color, otherwise -- see
// colorEnabled in statusview.go). The glyph itself always prints
// regardless of color support, since it carries meaning on its own.
func (a *App) statusLine(state, format string, args ...interface{}) {
	var glyph, code string
	switch state {
	case "ok":
		glyph, code = "✓", ansiGreen // ✓
	case "warn":
		glyph, code = "⚠", ansiYellow // ⚠
	default:
		glyph, code = "✗", ansiRed // ✗
	}
	sym := colorize(a.colorEnabled(), code, glyph)
	fmt.Fprintf(a.Out, "  %s %s\n", sym, fmt.Sprintf(format, args...))
}

func (a *App) docOK(format string, args ...interface{})   { a.statusLine("ok", format, args...) }
func (a *App) docWarn(format string, args ...interface{}) { a.statusLine("warn", format, args...) }

// docFail prints a ✗ line and always returns true, so callers combine
// it into their running fail flag with `fail = a.docFail(...) || fail`.
func (a *App) docFail(format string, args ...interface{}) bool {
	a.statusLine("fail", format, args...)
	return true
}

// cmdDoctor is a read-only health report: a short Environment block
// plus a ✓/⚠/✗ checklist of the runtimes, database engines, and tools
// WOR can use. Unlike the old bash doctor.sh port, it has no closing
// "Result"/"WOR Ready"/"Next" section -- the checklist itself is the
// result. The returned bool is true when something required is
// missing (non-zero process exit code equivalent): a core runtime
// (PHP/Node.js/Python/Go) is missing, the active host provider isn't
// installed, or the workspace hasn't been initialized. Database
// engines and secondary tools (PM2, git, zip, gzip) are always
// optional -- missing ones print a ⚠, never a ✗.
func (a *App) cmdDoctor(args []string) (bool, error) {
	fail := false
	provider := a.Cfg.HostProviderName()

	fmt.Fprintln(a.Out, "WOR Doctor")
	fmt.Fprintln(a.Out, "==========")
	fmt.Fprintln(a.Out)

	fmt.Fprintln(a.Out, "Environment")
	fmt.Fprintf(a.Out, "  OS            : %s\n", osutil.OSName())
	if distro, ok := osutil.LinuxDistro(); ok {
		fmt.Fprintf(a.Out, "  Distro        : %s\n", distro)
	}
	fmt.Fprintf(a.Out, "  Build         : %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(a.Out, "  WOR_ENV       : %s\n", a.Cfg.Env)
	fmt.Fprintf(a.Out, "  WOR_HOME      : %s\n", a.Cfg.WorHome)
	fmt.Fprintf(a.Out, "  Config        : %s\n", a.Cfg.ConfigFile)
	fmt.Fprintf(a.Out, "  Host Provider : %s\n", provider)
	if a.workspaceInitialized() {
		a.docOK("Workspace initialized")
	} else {
		fail = a.docFail("Workspace not initialized (run: wor setup)") || fail
	}
	fmt.Fprintln(a.Out)

	fmt.Fprintln(a.Out, "Runtimes")

	// Host provider(s): show each one that's actually installed
	// (marking whichever is configured as active), regardless of
	// which is active. If the *configured* provider isn't installed
	// at all, that's a real mismatch worth a ✗ even though the
	// "other" provider's absence is otherwise unremarkable.
	nginxP, _ := hostprovider.New("nginx", a.Cfg)
	if bin, ok := nginxP.Binary(); ok {
		version := strings.TrimPrefix(osutil.RunVersion(bin, "-v"), "nginx version: ")
		label := "Nginx"
		if provider == "nginx" {
			label += " (active)"
		}
		a.docOK("%s %s", label, version)
	} else if provider == "nginx" {
		fail = a.docFail("Nginx not installed (host provider mismatch)") || fail
	}

	apacheP, _ := hostprovider.New("apache", a.Cfg)
	if bin, ok := apacheP.Binary(); ok {
		label := "Apache"
		if provider == "apache" {
			label += " (active)"
		}
		a.docOK("%s %s", label, osutil.RunVersion(bin, "-v"))
	} else if provider == "apache" {
		fail = a.docFail("Apache not installed (host provider mismatch)") || fail
	}

	phpBin := "php"
	if !osutil.Exists(phpBin) && osutil.Exists("php-fpm") {
		phpBin = "php-fpm"
	}
	if osutil.Exists(phpBin) {
		a.docOK("%s", osutil.RunVersion(phpBin, "--version"))
	} else {
		fail = a.docFail("PHP not installed") || fail
	}
	if versions := phpfpm.DetectVersions(); len(versions) > 0 {
		a.docOK("PHP-FPM per-service pools available: %s", phpVersionNumbers(versions))
	} else if _, ok := hostprovider.PHPFPMEndpoint(a.Cfg); ok {
		a.docWarn("no per-version PHP-FPM pool.d layout detected; php services use the shared PHP_FPM_ENDPOINT")
	} else {
		a.docWarn("PHP-FPM not detected (per-version or PHP_FPM_ENDPOINT); php services will fail their runtime check")
	}

	if osutil.Exists("node") {
		a.docOK("Node.js %s", osutil.RunVersion("node", "--version"))
	} else {
		fail = a.docFail("Node.js not installed") || fail
	}

	if osutil.Exists("pm2") {
		a.docOK("PM2 %s", pm2.Version())
	} else {
		a.docWarn("PM2 not installed")
	}

	if osutil.Exists("go") {
		a.docOK("%s", osutil.RunVersion("go", "version"))
	} else {
		fail = a.docFail("Go not installed") || fail
	}

	pythonBin := "python3"
	if !osutil.Exists(pythonBin) && osutil.Exists("python") {
		pythonBin = "python"
	}
	if osutil.Exists(pythonBin) {
		a.docOK("%s", osutil.RunVersion(pythonBin, "--version"))
	} else {
		fail = a.docFail("Python not installed") || fail
	}
	fmt.Fprintln(a.Out)

	fmt.Fprintln(a.Out, "Database")
	if bin, ok := dbbackup.MySQLClientBin(); ok {
		a.docOK("MySQL Client %s", osutil.RunVersion(bin, "--version"))
	} else {
		a.docWarn("MySQL Client not installed")
	}
	if bin, ok := dbbackup.MySQLServerBin(); ok {
		a.docOK("MySQL Server %s", osutil.RunVersion(bin, "--version"))
	} else {
		a.docWarn("MySQL Server not installed")
	}
	if bin, ok := dbbackup.MariaDBBin(); ok {
		a.docOK("MariaDB %s", osutil.RunVersion(bin, "--version"))
	} else {
		a.docWarn("MariaDB not installed")
	}
	if bin, ok := dbbackup.ClientBin("postgresql"); ok {
		a.docOK("PostgreSQL %s", osutil.RunVersion(bin, "--version"))
	} else {
		a.docWarn("PostgreSQL not installed")
	}
	if bin, ok := dbbackup.RedisBin(); ok {
		a.docOK("Redis %s", osutil.RunVersion(bin, "--version"))
	} else {
		a.docWarn("Redis not installed")
	}
	if bin, ok := dbbackup.ClientBin("sqlite"); ok {
		a.docOK("SQLite %s", osutil.RunVersion(bin, "--version"))
	} else {
		a.docWarn("SQLite not installed")
	}
	fmt.Fprintln(a.Out)

	fmt.Fprintln(a.Out, "Other Tools")
	for _, t := range []struct{ bin, label string }{
		{"git", "Git"},
		{"zip", "Zip"},
		{"gzip", "Gzip"},
	} {
		if osutil.Exists(t.bin) {
			a.docOK("%s %s", t.label, osutil.RunVersion(t.bin, versionFlagFor(t.bin)))
		} else {
			a.docWarn("%s not installed", t.label)
		}
	}
	fmt.Fprintln(a.Out)

	// Security: neither check here ever sets fail -- both are hygiene/
	// hardening advice, not "wor itself is broken" the way a missing
	// runtime is. A site that 500s because of this is a real problem,
	// but it's the deployed site that's affected, not wor's own
	// ability to function, so this follows the same severity
	// convention as the optional database engines/tools above (⚠, never ✗).
	fmt.Fprintln(a.Out, "Security")
	if osutil.IsWindows() {
		// Unix owner/group/other permission bits don't carry the same
		// meaning on Windows (ACLs are a completely different model),
		// so both checks below would just be noise there -- skipped
		// entirely rather than printing something misleading.
		a.docOK("Permission checks skipped (Windows uses a different access-control model)")
	} else {
		if loose := scanLooseEnvFiles(a.Cfg.WorHome); len(loose) > 0 {
			a.docWarn(".env file(s) readable beyond their own owner -- %d found:", len(loose))
			for _, p := range loose {
				fmt.Fprintf(a.Out, "      %s\n", p)
			}
			a.info("Fix: find %s \\( -name '.env' -o -name '.env.*' \\) -exec chmod 600 {} +", a.Cfg.WorHome)
		} else if dirExists(a.Cfg.WorHome) {
			a.docOK("No overly-permissive .env files found under WOR_HOME")
		}

		if provider == "nginx" || provider == "apache" {
			switch {
			case osutil.IsDebianFamily():
				webUser := webServerRunUser(provider)
				if !webUserExists(webUser) {
					a.docWarn("could not resolve web server user %q on this system -- WOR_HOME reachability not checked", webUser)
				} else if blocked := checkWorHomeReachability(a, webUser); len(blocked) > 0 {
					a.docWarn("WOR_HOME not reachable by web server user %q -- %d path(s) block traversal:", webUser, len(blocked))
					for _, p := range blocked {
						fmt.Fprintf(a.Out, "      %s\n", p)
					}
					a.info("Fix: %s", worHomeReachabilityFixCommand(webUser, blocked))
				} else {
					a.docOK("WOR_HOME reachable by web server user (%s)", webUser)
				}
			case osutil.IsLinux():
				// RHEL/CentOS/Fedora-family: not auto-checked -- wor
				// doesn't support this family closely enough yet to be
				// confident about the right fix (see install_rhel in
				// scripts/install.sh), and SELinux can independently
				// block access even when POSIX permissions are fine.
				a.docWarn("Non-Debian Linux detected -- WOR_HOME permission issues are possible and not auto-checked here. If a static/php site 500s unexpectedly, check both regular permissions (owner/group/other) AND SELinux context (semanage fcontext / chcon) along the full WOR_HOME path")
			default:
				// macOS: nginx installed via Homebrew commonly runs as
				// the logged-in user rather than a separate system
				// account, so this class of problem is far less common
				// there -- intentionally not checked to avoid noise for
				// the common case.
			}
		}
	}
	fmt.Fprintln(a.Out)

	return fail, nil
}

// scanLooseEnvFiles walks worHome looking for .env / .env.* files
// (the Node/Laravel/etc. convention for secrets -- DB passwords, API
// keys) that are readable by anyone beyond their own owner (any
// group/other permission bit set). This is deliberately independent
// of checkWorHomeReachability above: that one is about letting the
// web server reach files it's *supposed* to serve; this one is a
// file's own last line of defense regardless of how open the
// surrounding directories end up needing to be -- opening WOR_HOME's
// traversal permission for the web server (or, on a shared box,
// simply placing WOR_HOME somewhere every local user can already
// traverse, e.g. /opt) means anyone who can reach a service's
// directory can also read a sibling service's .env unless the file
// itself is locked down.
func scanLooseEnvFiles(worHome string) []string {
	var found []string
	filepath.WalkDir(worHome, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		name := d.Name()
		if name != ".env" && !strings.HasPrefix(name, ".env.") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.Mode().Perm()&0o077 != 0 {
			found = append(found, path)
		}
		return nil
	})
	return found
}

func versionFlagFor(name string) string {
	switch name {
	case "npm", "unzip":
		return "-v"
	default:
		return "--version"
	}
}

func (a *App) cmdClean(args []string) error {
	provider, err := a.Provider()
	if err != nil {
		return err
	}
	fmt.Fprintln(a.Out, "WOR Clean")
	fmt.Fprintln(a.Out, "---------")

	if _, ok := provider.Binary(); ok {
		provider.CleanupWorBrokenSymlinks()
		avail := provider.SitesAvailable()
		files, _ := provider.FindWorHostConfigs(avail)
		for _, f := range files {
			name := filepath.Base(f)
			host := strings.TrimSuffix(strings.TrimPrefix(name, "wor__"), ".conf")
			if host == "000_wor_default" {
				continue
			}
			if _, ok := a.Store.ResolveHost(host); !ok {
				a.info("Removing orphan host config: %s", host)
				os.Remove(f)
				os.Remove(filepath.Join(provider.SitesEnabled(), name))
			}
		}
		if err := provider.Reload(); err != nil {
			a.warn("reload failed: %s", err)
		} else {
			a.ok("Host provider cleaned")
		}
	} else {
		a.info("Host provider clean skipped: %s not available", provider.Name)
	}

	if osutil.Exists("pm2") {
		out, _ := pm2.RunCapture("jlist")
		for _, name := range orphanPM2Names(out, a.Store) {
			a.info("Removing orphan PM2 process: %s", name)
			pm2.Run("delete", name)
		}
		pm2.Save()
	}

	if osutil.IsLinux() && osutil.Exists("systemctl") {
		units, _ := systemd.ListUnits()
		for _, unit := range orphanSystemdUnits(units, a.Store) {
			domain, service, ok := parseWorUnitName(unit)
			if !ok {
				continue
			}
			a.info("Removing orphan systemd unit: %s", unit)
			if err := systemd.RemoveUnit(domain, service); err != nil {
				a.warn("could not remove systemd unit %s: %s", unit, err)
			}
		}
	}

	if hosts, err := hostsfile.ListHosts(); err == nil {
		for _, host := range hosts {
			if _, ok := a.Store.ResolveHost(host); ok {
				continue
			}
			a.info("Removing orphan hosts file entry: %s", host)
			if err := hostsfile.Remove(host); err != nil {
				a.warn("could not remove hosts file entry for %s: %s (%s)", host, err, osutil.ElevationHint())
			}
		}
	}
	return nil
}

// cmdReset always requires typed "RESET" confirmation -- there is
// deliberately no --yes/-y bypass. This wipes every WOR-managed
// pm2/systemd process, host config, hosts-file entry, and the entire
// domains/backups/logs/ssl trees; a flag that skips the prompt is too easy
// to have sitting in a script or shell history and fire unattended for
// something this destructive.
func (a *App) cmdReset(args []string) error {
	fmt.Fprintln(a.Out, "WARNING: The following WOR resources will be removed:")
	fmt.Fprintln(a.Out, "  - PM2 processes starting with wor_")
	fmt.Fprintln(a.Out, "  - systemd units matching wor_*.service (Linux)")
	fmt.Fprintln(a.Out, "  - Host configs matching wor__*.conf")
	fmt.Fprintln(a.Out, "  - Provider default config 000_wor_default.conf")
	fmt.Fprintln(a.Out, "  - WOR-HOSTS block entries in the system hosts file")
	fmt.Fprintf(a.Out, "  - %s/*\n", a.Cfg.Domains)
	fmt.Fprintf(a.Out, "  - %s/*\n", a.Cfg.Backups)
	fmt.Fprintf(a.Out, "  - %s/*\n", a.Cfg.Logs)
	fmt.Fprintf(a.Out, "  - %s/* (SSL certs/state)\n", a.Cfg.SSL)
	fmt.Fprintln(a.Out)
	fmt.Fprintln(a.Out, "It will NOT remove non-WOR host configs.")
	if !a.requireTyped("Type RESET to continue: ", "RESET") {
		return a.errf("cancelled")
	}

	if osutil.Exists("pm2") {
		out, _ := pm2.RunCapture("jlist")
		for _, name := range worPM2Names(out) {
			pm2.Run("delete", name)
		}
		pm2.Save()
	}

	if osutil.IsLinux() && osutil.Exists("systemctl") {
		units, _ := systemd.ListUnits()
		for _, unit := range units {
			domain, service, ok := parseWorUnitName(unit)
			if !ok {
				continue
			}
			if err := systemd.RemoveUnit(domain, service); err != nil {
				a.warn("could not remove systemd unit %s: %s", unit, err)
			}
		}
	}

	provider, err := a.Provider()
	if err == nil {
		if _, ok := provider.Binary(); ok {
			provider.RemoveAllWorFiles()
			provider.CleanupWorBrokenSymlinks()
			provider.Reload()
		}
	}

	if err := hostsfile.RemoveAll(); err != nil {
		a.warn("could not clear hosts file entries: %s (%s)", err, osutil.ElevationHint())
	}

	os.RemoveAll(a.Cfg.Domains)
	os.RemoveAll(a.Cfg.Backups)
	os.RemoveAll(a.Cfg.Logs)
	os.RemoveAll(a.Cfg.SSL)
	os.MkdirAll(a.Cfg.Domains, 0o755)
	os.MkdirAll(a.Cfg.Backups, 0o755)
	os.MkdirAll(a.Cfg.Logs, 0o755)
	os.MkdirAll(a.Cfg.SSL, 0o755)
	if provider != nil {
		provider.EnsureDefaultHost(a.Store, a.Cfg.Backups, a.Cfg.Logs)
	}
	a.ok("WOR reset completed")
	return nil
}
