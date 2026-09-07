package cliapp

import (
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"wor/internal/dbbackup"
	"wor/internal/hostprovider"
	"wor/internal/hostsfile"
	"wor/internal/osutil"
	"wor/internal/phpfpm"
	"wor/internal/pm2"
	"wor/internal/ssl"
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
func (a *App) cmdDoctor(args []string, jsonMode bool) (bool, error) {
	fail := false
	provider := a.Cfg.HostProviderName()
	r := a.newReporter(jsonMode)

	r.Line("WOR Doctor")
	r.Line("==========")
	r.Blank()

	r.Section("Environment")
	r.Field("OS", osutil.OSName())
	if distro, ok := osutil.LinuxDistro(); ok {
		r.Field("Distro", distro)
	}
	r.Field("Build", fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH))
	r.Field("WOR_ENV", a.Cfg.Env)
	r.Field("WOR_HOME", a.Cfg.WorHome)
	r.Field("Config", a.Cfg.ConfigFile)
	r.Field("Host Provider", provider)
	if a.workspaceInitialized() {
		r.OK("Workspace initialized")
	} else {
		fail = r.Fail("Workspace not initialized (run: wor setup)") || fail
	}
	r.Blank()

	r.Section("Runtimes")

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
		r.OK("%s %s", label, version)
	} else if provider == "nginx" {
		fail = r.Fail("Nginx not installed (host provider mismatch)") || fail
	}

	apacheP, _ := hostprovider.New("apache", a.Cfg)
	if bin, ok := apacheP.Binary(); ok {
		label := "Apache"
		if provider == "apache" {
			label += " (active)"
		}
		r.OK("%s %s", label, osutil.RunVersion(bin, "-v"))
	} else if provider == "apache" {
		fail = r.Fail("Apache not installed (host provider mismatch)") || fail
	}

	phpBin := "php"
	if !osutil.Exists(phpBin) && osutil.Exists("php-fpm") {
		phpBin = "php-fpm"
	}
	if osutil.Exists(phpBin) {
		r.OK("%s", osutil.RunVersion(phpBin, "--version"))
	} else {
		fail = r.Fail("PHP not installed") || fail
	}
	if versions := phpfpm.DetectVersions(); len(versions) > 0 {
		r.OK("PHP-FPM per-service pools available: %s", phpVersionNumbers(versions))
	} else if _, ok := hostprovider.PHPFPMEndpoint(a.Cfg); ok {
		r.Warn("no per-version PHP-FPM pool.d layout detected; php services use the shared PHP_FPM_ENDPOINT")
	} else {
		r.Warn("PHP-FPM not detected (per-version or PHP_FPM_ENDPOINT); php services will fail their runtime check")
	}

	if osutil.Exists("node") {
		r.OK("Node.js %s", osutil.RunVersion("node", "--version"))
	} else {
		fail = r.Fail("Node.js not installed") || fail
	}

	if osutil.Exists("pm2") {
		r.OK("PM2 %s", pm2.Version())
	} else {
		r.Warn("PM2 not installed")
	}

	if osutil.Exists("go") {
		r.OK("%s", osutil.RunVersion("go", "version"))
	} else {
		fail = r.Fail("Go not installed") || fail
	}

	pythonBin := "python3"
	if !osutil.Exists(pythonBin) && osutil.Exists("python") {
		pythonBin = "python"
	}
	if osutil.Exists(pythonBin) {
		r.OK("%s", osutil.RunVersion(pythonBin, "--version"))
	} else {
		fail = r.Fail("Python not installed") || fail
	}
	r.Blank()

	r.Section("Database")
	if bin, ok := dbbackup.MySQLClientBin(); ok {
		r.OK("MySQL Client %s", osutil.RunVersion(bin, "--version"))
	} else {
		r.Warn("MySQL Client not installed")
	}
	if bin, ok := dbbackup.MySQLServerBin(); ok {
		r.OK("MySQL Server %s", osutil.RunVersion(bin, "--version"))
	} else {
		r.Warn("MySQL Server not installed")
	}
	if bin, ok := dbbackup.MariaDBBin(); ok {
		r.OK("MariaDB %s", osutil.RunVersion(bin, "--version"))
	} else {
		r.Warn("MariaDB not installed")
	}
	if bin, ok := dbbackup.ClientBin("postgresql"); ok {
		r.OK("PostgreSQL %s", osutil.RunVersion(bin, "--version"))
	} else {
		r.Warn("PostgreSQL not installed")
	}
	if bin, ok := dbbackup.RedisBin(); ok {
		r.OK("Redis %s", osutil.RunVersion(bin, "--version"))
	} else {
		r.Warn("Redis not installed")
	}
	if bin, ok := dbbackup.ClientBin("sqlite"); ok {
		r.OK("SQLite %s", osutil.RunVersion(bin, "--version"))
	} else {
		r.Warn("SQLite not installed")
	}
	r.Blank()

	r.Section("Other Tools")
	for _, t := range []struct{ bin, label string }{
		{"git", "Git"},
		{"zip", "Zip"},
		{"gzip", "Gzip"},
	} {
		if osutil.Exists(t.bin) {
			r.OK("%s %s", t.label, osutil.RunVersion(t.bin, versionFlagFor(t.bin)))
		} else {
			r.Warn("%s not installed", t.label)
		}
	}
	r.Blank()

	// Security: neither check here ever sets fail -- both are hygiene/
	// hardening advice, not "wor itself is broken" the way a missing
	// runtime is. A site that 500s because of this is a real problem,
	// but it's the deployed site that's affected, not wor's own
	// ability to function, so this follows the same severity
	// convention as the optional database engines/tools above (⚠, never ✗).
	r.Section("Security")
	if osutil.IsWindows() {
		// Unix owner/group/other permission bits don't carry the same
		// meaning on Windows (ACLs are a completely different model),
		// so both checks below would just be noise there -- skipped
		// entirely rather than printing something misleading.
		r.OK("Permission checks skipped (Windows uses a different access-control model)")
	} else {
		a.checkWorHomeOwnership(r)
		a.checkOperatorIdentity(r)
		a.checkCertificateRenewalSchedule(r)

		if loose := scanLooseEnvFiles(a.Cfg.WorHome); len(loose) > 0 {
			r.Warn(".env file(s) readable by any account on this machine -- %d found:", len(loose))
			for _, p := range loose {
				r.Item("%s", p)
			}
			// 0640, not 0600. A service that runs under its own account
			// (a per-service php-fpm pool, and every systemd service
			// once those get their own user) reads its .env as a group
			// member, not as the owner -- so 0600 would lock the service
			// out of its own configuration and take the site down, which
			// is a poor outcome for following a health check's advice.
			// 0640 closes it to everyone else and leaves that group read
			// intact.
			r.Note("Fix: find %s \\( -name '.env' -o -name '.env.*' \\) -exec chmod 640 {} +", a.Cfg.WorHome)
			r.Note("(0640, not 0600: a service running under its own account reads .env through its group.)")
		} else if dirExists(a.Cfg.WorHome) {
			r.OK("No world-readable .env files found under WOR_HOME")
		}

		if provider == "nginx" || provider == "apache" {
			switch {
			case osutil.IsDebianFamily():
				webUser := webServerRunUser(provider)
				if !webUserExists(webUser) {
					r.Warn("could not resolve web server user %q on this system -- WOR_HOME reachability not checked", webUser)
				} else if blocked := checkWorHomeReachability(a, webUser); len(blocked) > 0 {
					r.Warn("WOR_HOME not reachable by web server user %q -- %d path(s) block traversal:", webUser, len(blocked))
					for _, p := range blocked {
						r.Item("%s", p)
					}
					r.Note("Fix: %s", worHomeReachabilityFixCommand(webUser, blocked))
				} else {
					r.OK("WOR_HOME reachable by web server user (%s)", webUser)
				}
			case osutil.IsLinux():
				// RHEL/CentOS/Fedora-family: not auto-checked -- wor
				// doesn't support this family closely enough yet to be
				// confident about the right fix (see install_rhel in
				// scripts/install.sh), and SELinux can independently
				// block access even when POSIX permissions are fine.
				r.Warn("Non-Debian Linux detected -- WOR_HOME permission issues are possible and not auto-checked here. If a static/php site 500s unexpectedly, check both regular permissions (owner/group/other) AND SELinux context (semanage fcontext / chcon) along the full WOR_HOME path")
			default:
				// macOS: nginx installed via Homebrew commonly runs as
				// the logged-in user rather than a separate system
				// account, so this class of problem is far less common
				// there -- intentionally not checked to avoid noise for
				// the common case.
			}
		}
	}
	r.Blank()

	if jsonMode {
		return fail, a.writeJSON(doctorReport{
			Schema:      SchemaVersion,
			Environment: r.Fields(),
			Checks:      r.Checks(),
			Failed:      fail,
		})
	}
	return fail, nil
}

// doctorReport is the machine-readable form of `wor doctor`. Failed is
// the same verdict the exit code carries, restated here because a
// reader that captured stdout should not also have to have captured the
// process status to know whether anything was wrong.
type doctorReport struct {
	Schema      int           `json:"schema"`
	Environment []reportField `json:"environment"`
	Checks      []reportCheck `json:"checks"`
	Failed      bool          `json:"failed"`
}

// scanLooseEnvFiles walks worHome looking for .env / .env.* files
// (the Node/Laravel/etc. convention for secrets -- DB passwords, API
// keys) that are readable by any account on the machine (any "other"
// permission bit set).
//
// Group bits are deliberately allowed. This check used to require 0600
// and told the operator to run `chmod 600` over the whole tree, which
// on a host where services run under their own accounts -- a
// per-service php-fpm pool today, every systemd service once those get
// their own user -- takes the site down: the service reads its .env as
// a group member, not as the owner, and 0600 locks it out of its own
// configuration. Advice from a health check has to be safe to follow.
// "No other bits" still closes the exposure this exists for, since that
// exposure is about unrelated local accounts, not the service's own.
//
// This is deliberately independent
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
		if info.Mode().Perm()&0o007 != 0 {
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

// checkWorHomeOwnership warns when WOR_HOME is owned by root.
//
// wor derives the owner for the certificate files it copies from this
// directory (see App.certificateOwner), so a root-owned WOR_HOME makes
// `wor ssl sync` refuse rather than hand a private key to root. That
// refusal would otherwise first surface inside certbot's renewal hook
// in the middle of the night; reporting it here means it is visible
// while somebody is actually looking.
//
// It is not a hypothetical state: osutil.ClaimOwnership exists because
// WOR_HOME has been found root-owned in the field, left behind by an
// older install.
func (a *App) checkWorHomeOwnership(r *reporter) {
	if !dirExists(a.Cfg.WorHome) {
		return
	}
	uid, _, err := osutil.FileOwner(a.Cfg.WorHome)
	if err != nil {
		return
	}
	if uid != 0 {
		return
	}
	// Root-owned and run by root is a supported setup (a server with no
	// other account), not a problem -- see App.certificateOwner.
	if os.Geteuid() == 0 {
		return
	}
	r.Warn("WOR_HOME (%s) is owned by root, but you are not", a.Cfg.WorHome)
	r.Note("Certificate sync cannot run while it is. Fix: sudo chown -R $(id -un) %s", a.Cfg.WorHome)
}

// checkOperatorIdentity reports how many different accounts have written
// into WOR_HOME's domain tree.
//
// wor has no notion of a canonical operator account: it runs as whoever
// invoked it, creates directories 0755 owned by that account
// (osutil.EnsureDir), keeps its own config at that account's
// ~/.wor/config, and chowns WOR_HOME's base directories to it on every
// run (osutil.ClaimOwnership, via ensureRootDirs). One admin account and
// none of that shows. Two -- an operator who reaches the same server
// under different login names, say a local SSH key on one machine and a
// cloud console's own account from another -- and the tree ends up split
// between them: each side cannot write the other's service directories,
// their wor configs disagree about WOR_HOME, and every alternate run
// chowns the base directories back and forth behind a sudo prompt.
//
// Reported, not repaired. Consolidating onto one account means chowning
// a live tree and deciding which account wins, which is a deliberate
// migration and not something a health check should start on its own.
//
// Only directories are examined -- each domain, and each service under
// it -- rather than walking every file: the divergence shows at exactly
// that level, and a full walk of every service's node_modules would cost
// far more than the answer is worth.
func (a *App) checkOperatorIdentity(r *reporter) {
	if !dirExists(a.Cfg.Domains) {
		return
	}
	domains, err := a.Store.ListDomains()
	if err != nil {
		return
	}

	// uid -> one example path owned by it, for a report that points at
	// something the operator can actually go and look at.
	sample := map[int]string{}
	note := func(path string) {
		uid, _, err := osutil.FileOwner(path)
		if err != nil {
			return
		}
		// Pool users own the directories of the services they run (see
		// setupPHPPool); they are not operators and their presence here
		// is by design, not drift.
		if isServiceAccountUID(uid) {
			return
		}
		if _, seen := sample[uid]; !seen {
			sample[uid] = path
		}
	}

	for _, domain := range domains {
		note(a.Store.DomainDir(domain))
		cfg, err := a.Store.LoadServices(domain)
		if err != nil {
			continue
		}
		for i := range cfg.Services {
			svc := &cfg.Services[i]
			serviceDir := a.Store.ServiceDir(domain, svc.Name)
			note(serviceDir)
			// And the document root inside it, which drifts separately
			// from the service directory containing it. A deploy that
			// rsyncs as root rewrites everything under `public/` and
			// leaves the service directory itself untouched, so
			// sampling only the outer directory reported a clean host
			// while the operator could not write the very files it was
			// about to deploy next. `wor setup` does not repair that
			// either -- alignTreeOwnership never touches root-owned
			// files, deliberately -- so this is the check that has to
			// see it. `wor service chown` is the repair.
			note(resolveDocroot(serviceDir, svc))
		}
	}
	if len(sample) == 0 {
		return
	}

	uids := make([]int, 0, len(sample))
	for uid := range sample {
		uids = append(uids, uid)
	}
	sort.Ints(uids)

	// With an operator account configured there is a right answer to
	// compare against, so the report is about conformance to it rather
	// than about how many accounts happen to be involved.
	if want := a.Cfg.OperatorUser; want != "" {
		wrong := make([]int, 0, len(uids))
		for _, uid := range uids {
			if accountLabel(uid) != want {
				wrong = append(wrong, uid)
			}
		}
		if len(wrong) == 0 {
			r.OK("Service tree is owned by the configured wor account (%s)", want)
			return
		}
		r.Warn("%d director(ies) are not owned by the configured wor account (%s)", len(wrong), want)
		for _, uid := range wrong {
			r.Item("%s owns %s", accountLabel(uid), sample[uid])
		}
		// Deliberately not `sudo chown -R <want> <domains>`, which this
		// used to print. Two things were wrong with it. It reaches every
		// service on the host to repair the one that drifted. And it
		// chowns pool-owned files along with everything else -- the
		// very files the sampling above skips on purpose, because a
		// pool account owning them is by design and not drift, so the
		// advice contradicted the diagnosis that produced it.
		//
		// It also stops at ownership: a tree damaged by `rsync -a` has
		// lost the setgid bit and the pool's group read as well, and
		// nothing about a chown puts those back. `wor service chown`
		// does the re-grant afterwards.
		r.Note("Fix, per service: wor service chown <domain>/<service>")
		return
	}

	if len(uids) == 1 {
		if uids[0] == os.Getuid() {
			r.OK("Service tree is owned by a single account (%s)", accountLabel(uids[0]))
		} else {
			r.Warn("Service tree is owned by %s, but you are running wor as %s",
				accountLabel(uids[0]), accountLabel(os.Getuid()))
			r.Note("Nothing pins that down yet. Set wor_user in %s/host.env to make it the account wor expects.", a.Cfg.Configs)
		}
		return
	}

	r.Warn("Service tree is split across %d accounts -- wor assumes one operator", len(uids))
	for _, uid := range uids {
		r.Item("%s owns %s", accountLabel(uid), sample[uid])
	}
	r.Note("You are currently %s. Whichever account did not create a directory cannot write it,", accountLabel(os.Getuid()))
	r.Note("so `wor deploy` and `wor source pull` will fail for that half of the tree.")
	r.Note("Set wor_user in %s/host.env to name the account they should all be.", a.Cfg.Configs)
}

// isServiceAccountUID reports whether uid belongs to one of the
// dedicated per-service accounts wor creates (phpfpm.PoolName's
// "wor_<domain>_<service>" convention), as opposed to a human operator.
func isServiceAccountUID(uid int) bool {
	u, err := user.LookupId(strconv.Itoa(uid))
	if err != nil {
		return false
	}
	return isServiceAccountName(u.Username)
}

// accountLabel renders uid as a username, falling back to the bare
// numeric id for an account that no longer exists in the passwd
// database -- which is itself worth seeing in a health report.
func accountLabel(uid int) string {
	if u, err := user.LookupId(strconv.Itoa(uid)); err == nil {
		return u.Username
	}
	return fmt.Sprintf("uid %d", uid)
}

// checkCertificateRenewalSchedule warns when Let's Encrypt certificates
// exist but nothing on the machine is scheduled to renew them.
//
// Debian's certbot package installs a systemd timer; Homebrew's formula
// installs no equivalent, and on the machine this was first found on
// there was neither a timer nor a root crontab -- so the certificates
// were simply going to expire, and the deploy hook that refreshes wor's
// copy would never have fired either. Nothing reports that on its own,
// which is exactly why it belongs in doctor.
//
// Warning only, and deliberately not offered as an auto-fix: what the
// right schedule looks like differs per platform and per operator, and
// installing a root-level timer unprompted is not something a read-only
// health check should do.
func (a *App) checkCertificateRenewalSchedule(r *reporter) {
	hosts := a.letsEncryptHosts()
	if len(hosts) == 0 {
		return
	}
	if certbotRenewalScheduled() {
		r.OK("Certificate renewal is scheduled (%d Let's Encrypt host(s))", len(hosts))
		return
	}
	r.Warn("%d Let's Encrypt host(s) but no renewal schedule found on this machine", len(hosts))
	r.Note("Nothing will renew them, and they expire in 90 days. Check with: sudo certbot renew --dry-run")
	r.Note("Then schedule `certbot renew` (a systemd timer on Linux, a launchd job or cron entry on macOS).")
}

// letsEncryptHosts lists the registered hosts whose recorded certificate
// came from Let's Encrypt.
func (a *App) letsEncryptHosts() []string {
	refs, err := a.Store.ListAllServices()
	if err != nil {
		return nil
	}
	var out []string
	for _, ref := range refs {
		for _, host := range ref.Service.Hosts {
			if st, ok, _ := ssl.LoadState(a.Cfg.SSL, host); ok && st.Provider == "letsencrypt" {
				out = append(out, host)
			}
		}
	}
	return out
}

// certbotRenewalScheduled reports whether anything on this machine is
// set up to run `certbot renew`. Best effort by design: it checks the
// places certbot's own packaging uses, and a false negative costs only
// a warning telling the operator to look, never a failed command.
func certbotRenewalScheduled() bool {
	// Debian/RHEL packaging: a systemd timer, or a drop-in under cron.
	for _, p := range []string{
		"/etc/systemd/system/timers.target.wants/certbot.timer",
		"/lib/systemd/system/certbot.timer",
		"/usr/lib/systemd/system/certbot.timer",
		"/etc/cron.d/certbot",
		"/etc/cron.daily/certbot",
	} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	// macOS: a launchd job, wherever it was installed from.
	for _, dir := range []string{"/Library/LaunchDaemons", "/Library/LaunchAgents"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if strings.Contains(strings.ToLower(e.Name()), "certbot") {
				return true
			}
		}
	}
	return false
}
