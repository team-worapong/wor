package cliapp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"wor/internal/dbbackup"
	"wor/internal/domainmodel"
	"wor/internal/hostprovider"
	"wor/internal/hostsfile"
	"wor/internal/osutil"
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

// cmdDoctor mirrors commands/doctor.sh cmd_doctor(): a read-only health
// report. The returned bool is true when a required dependency is
// missing (non-zero shell exit code equivalent).
func (a *App) cmdDoctor(args []string) (bool, error) {
	fail := false
	notInitialized := !a.workspaceInitialized()
	provider := a.Cfg.HostProviderName()

	fmt.Fprintln(a.Out, "WOR Doctor")
	fmt.Fprintln(a.Out, "==========")
	fmt.Fprintln(a.Out)

	fmt.Fprintln(a.Out, "Environment")
	fmt.Fprintln(a.Out, "-----------")
	a.ok("OS           : %s", osutil.OSName())
	a.ok("Environment  : %s", a.Cfg.Env)
	a.ok("Config       : %s", a.Cfg.ConfigFile)
	a.ok("WOR_HOME     : %s", a.Cfg.WorHome)
	a.ok("Host Provider: %s", provider)
	fmt.Fprintln(a.Out)

	fmt.Fprintln(a.Out, "Directories")
	fmt.Fprintln(a.Out, "-----------")
	for _, d := range []string{a.Cfg.WorHome, a.Cfg.Domains, a.Cfg.Backups, a.Cfg.Configs, a.Cfg.Logs, a.Cfg.SSL} {
		if dirExists(d) {
			a.ok("%s", d)
		} else {
			a.info("not initialized: %s", d)
		}
	}
	fmt.Fprintln(a.Out)

	fmt.Fprintln(a.Out, "Required Dependencies")
	fmt.Fprintln(a.Out, "---------------------")
	for _, name := range []string{"git", "node", "npm"} {
		if !osutil.Exists(name) {
			a.warn("%s not found", name)
			fail = true
			continue
		}
		a.ok("%s : %s", name, osutil.RunVersion(name, versionFlagFor(name)))
		a.info("%s bin : %s", name, osutil.Which(name))
	}
	fmt.Fprintln(a.Out)

	fmt.Fprintln(a.Out, "Go / Python Runtimes")
	fmt.Fprintln(a.Out, "--------------------")
	if osutil.Exists("go") {
		a.ok("go        : %s", osutil.RunVersion("go", "version"))
		a.info("go bin    : %s", osutil.Which("go"))
	} else if a.goRuntimeRequired() {
		a.warn("go        : not installed (Not Supported -- a registered service needs it)")
		fail = true
	} else {
		a.info("go        : not installed")
	}
	pythonBin := "python3"
	if !osutil.Exists(pythonBin) && osutil.Exists("python") {
		pythonBin = "python"
	}
	if osutil.Exists(pythonBin) {
		a.ok("python    : %s", osutil.RunVersion(pythonBin, "--version"))
		a.info("python bin: %s", osutil.Which(pythonBin))
	} else if a.pythonRuntimeRequired() {
		a.warn("python    : not installed (Not Supported -- a registered service needs it)")
		fail = true
	} else {
		a.info("python    : not installed")
	}
	if osutil.IsLinux() {
		if osutil.Exists("systemctl") {
			a.ok("systemd   : available (used as the process provider for go/python services)")
		} else if a.goRuntimeRequired() || a.pythonRuntimeRequired() {
			a.warn("systemd   : not installed (go/python services need it, or PM2 as a fallback)")
		} else {
			a.info("systemd   : not installed")
		}
	} else {
		a.info("systemd   : n/a on this OS (go/python services use PM2 here)")
	}
	fmt.Fprintln(a.Out)

	fmt.Fprintln(a.Out, "Optional Dependencies")
	fmt.Fprintln(a.Out, "---------------------")
	for _, name := range []string{"pm2", "zip", "gzip", "sqlite3"} {
		if !osutil.Exists(name) {
			a.info("%s : not installed", name)
			continue
		}
		if name == "pm2" {
			a.ok("pm2 : %s", pm2.Version())
		} else {
			a.ok("%s : %s", name, osutil.RunVersion(name, versionFlagFor(name)))
		}
		a.info("%s bin : %s", name, osutil.Which(name))
	}
	fmt.Fprintln(a.Out)

	nginxP, _ := hostprovider.New("nginx", a.Cfg)
	if bin, ok := nginxP.Binary(); ok {
		version := osutil.RunVersion(bin, "-v")
		version = strings.TrimPrefix(version, "nginx version: ")
		if provider == "nginx" {
			a.ok("nginx     : %s", version)
		} else {
			a.info("nginx     : %s", version)
		}
		a.info("nginx bin : %s", bin)
		if provider == "skip" {
			a.warn("Provider is skip but nginx is installed. Host commands are disabled until host_provider is set to nginx or apache.")
		}
		if provider == "nginx" {
			if nginxP.Test() == nil {
				a.ok("nginx config test passed")
			} else {
				a.warn("nginx config test failed")
			}
		}
	} else {
		if provider == "nginx" {
			a.warn("nginx     : not installed (HOST_PROVIDER=nginx)")
			a.warn("Provider mismatch: HOST_PROVIDER=nginx but nginx runtime was not found.")
			if a.providerRequired() {
				fail = true
			}
		} else {
			a.info("nginx     : not installed")
		}
	}

	apacheP, _ := hostprovider.New("apache", a.Cfg)
	if bin, ok := apacheP.Binary(); ok {
		version := osutil.RunVersion(bin, "-v")
		if provider == "apache" {
			a.ok("apache    : %s", version)
		} else {
			a.info("apache    : %s", version)
		}
		a.info("apache bin: %s", bin)
		if provider == "skip" {
			a.warn("Provider is skip but apache is installed. Host commands are disabled until host_provider is set to nginx or apache.")
		}
		if provider == "apache" {
			if apacheP.Test() == nil {
				a.ok("apache config test passed")
			} else {
				a.warn("apache config test failed")
			}
		}
	} else {
		if provider == "apache" {
			a.warn("apache    : not installed (HOST_PROVIDER=apache)")
			a.warn("Provider mismatch: HOST_PROVIDER=apache but apache runtime was not found.")
			if a.providerRequired() {
				fail = true
			}
		} else {
			a.info("apache    : not installed")
		}
	}

	if ep, ok := hostprovider.PHPFPMEndpoint(a.Cfg); ok {
		a.ok("php-fpm   : %s", ep)
	} else if osutil.Exists("php-fpm") {
		a.ok("php-fpm   : installed (%s)", osutil.Which("php-fpm"))
		a.warn("PHP_FPM_ENDPOINT is not configured")
	} else {
		if a.phpRuntimeRequired() {
			a.warn("php-fpm   : not installed")
			fail = true
		} else {
			a.info("php-fpm   : not installed")
		}
	}

	fmt.Fprintln(a.Out)
	fmt.Fprintln(a.Out, "Database")
	fmt.Fprintln(a.Out, "--------")
	engine := dbbackup.DetectEngine()
	a.ok("Engine     : %s", engine)
	dbClient, hasClient := dbbackup.ClientBin(dbEngineForDetect())
	dbDump, hasDump := dbbackup.DumpBin(dbEngineForDetect())
	if hasClient {
		a.ok("Client     : %s", dbClient)
	} else {
		a.info("Client     : not installed")
	}
	if hasDump {
		a.ok("Dump Tool  : %s", dbDump)
	} else {
		a.info("Dump Tool  : not installed")
	}
	status := "Not Installed"
	if hasClient && hasDump {
		status = "Ready"
	} else if hasClient || hasDump {
		status = "Partial"
	}
	a.info("Status     : %s", status)
	fmt.Fprintln(a.Out)

	fmt.Fprintln(a.Out, "Result")
	fmt.Fprintln(a.Out, "------")
	switch {
	case !fail && notInitialized:
		fmt.Fprintf(a.Out, "%s is installed. Runtime workspace is not initialized. Run: wor setup\n", ProductName)
	case !fail:
		fmt.Fprintln(a.Out, "WOR Ready")
		fmt.Fprintln(a.Out, "Next: wor create app.example.com")
	default:
		fmt.Fprintln(a.Out, "WOR has required dependency issues")
	}

	return fail, nil
}

func versionFlagFor(name string) string {
	switch name {
	case "npm", "unzip":
		return "-v"
	default:
		return "--version"
	}
}

// providerRequired mirrors doctor_provider_required(): Linux+production
// always requires the provider; otherwise only if at least one domain
// has registered services.
func (a *App) providerRequired() bool {
	if osutil.IsLinux() && a.Cfg.Env == "production" {
		return true
	}
	targets, _ := a.Store.ListServiceTargets()
	return len(targets) > 0
}

// anyServiceOfType reports whether any registered service across every
// domain matches predicate -- shared by phpRuntimeRequired,
// goRuntimeRequired, and pythonRuntimeRequired.
func (a *App) anyServiceOfType(predicate func(string) bool) bool {
	domains, _ := a.Store.ListDomains()
	for _, d := range domains {
		cfg, err := a.Store.LoadServices(d)
		if err != nil {
			continue
		}
		for _, s := range cfg.Services {
			if predicate(s.Type) {
				return true
			}
		}
	}
	return false
}

// phpRuntimeRequired mirrors doctor_php_runtime_required(): true if any
// registered service uses the php template.
func (a *App) phpRuntimeRequired() bool {
	return a.anyServiceOfType(domainmodel.TemplateRequiresPHP)
}

// goRuntimeRequired/pythonRuntimeRequired are the go/python equivalents
// of phpRuntimeRequired, added for the go/python/systemd redesign.
func (a *App) goRuntimeRequired() bool {
	return a.anyServiceOfType(domainmodel.TemplateRequiresGo)
}

func (a *App) pythonRuntimeRequired() bool {
	return a.anyServiceOfType(domainmodel.TemplateRequiresPython)
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

func (a *App) cmdReset(args []string) error {
	yes := false
	for _, arg := range args {
		if arg == "--yes" || arg == "-y" {
			yes = true
		}
	}
	if !yes {
		fmt.Fprintln(a.Out, "WARNING: WOR reset will remove:")
		fmt.Fprintln(a.Out, "  - PM2 processes starting with wor_")
		fmt.Fprintln(a.Out, "  - systemd units matching wor_*.service (Linux)")
		fmt.Fprintln(a.Out, "  - Host configs matching wor__*.conf")
		fmt.Fprintln(a.Out, "  - Provider default config 000_wor_default.conf")
		fmt.Fprintln(a.Out, "  - WOR-HOSTS block entries in the system hosts file")
		fmt.Fprintf(a.Out, "  - %s/*\n", a.Cfg.Domains)
		fmt.Fprintf(a.Out, "  - %s/*\n", a.Cfg.Backups)
		fmt.Fprintf(a.Out, "  - %s/*\n", a.Cfg.Logs)
		fmt.Fprintln(a.Out)
		fmt.Fprintln(a.Out, "It will NOT remove non-WOR host configs.")
		if !a.requireTyped("Type RESET to continue: ", "RESET") {
			return a.errf("cancelled")
		}
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
	os.MkdirAll(a.Cfg.Domains, 0o755)
	os.MkdirAll(a.Cfg.Backups, 0o755)
	os.MkdirAll(a.Cfg.Logs, 0o755)
	if provider != nil {
		provider.EnsureDefaultHost(a.Store, a.Cfg.Backups, a.Cfg.Logs)
	}
	a.ok("WOR reset completed")
	return nil
}
