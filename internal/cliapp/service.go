package cliapp

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"wor/internal/domainmodel"
	"wor/internal/hostprovider"
	"wor/internal/hostsfile"
	"wor/internal/osutil"
	"wor/internal/phpfpm"
	"wor/internal/pm2"
	"wor/internal/servicefiles"
	"wor/internal/systemd"
)

// requireNodeRuntime mirrors lib/webserver.sh require_node_runtime().
func (a *App) requireNodeRuntime() error {
	var missing []string
	for _, name := range []string{"node", "npm", "pm2"} {
		if !commandExists(name) {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return a.errf("template requires Node.js runtime. Missing: %v. Install it, then run: wor doctor", missing)
	}
	return nil
}

// requireGoRuntime hard-blocks service creation when the `go` toolchain
// isn't on PATH -- go templates need it both to scaffold and to build
// (see cmdDeploy's build step). systemd is only checked on Linux, since
// go services fall back to PM2 on macOS/Windows (domainmodel.ProcessProviderFor).
func (a *App) requireGoRuntime() error {
	if !commandExists("go") {
		return a.errf("template requires the Go runtime. Missing: go. Install it, then run: wor doctor")
	}
	if osutil.IsLinux() && !commandExists("systemctl") {
		return a.errf("template requires systemd (systemctl not found) on Linux. Install it, then run: wor doctor")
	}
	if !osutil.IsLinux() && !commandExists("pm2") {
		return a.errf("template requires PM2 on this OS (no systemd). Missing: pm2. Install it, then run: wor doctor")
	}
	return nil
}

// pythonBinary resolves which python interpreter to invoke for python
// templates: python3 preferred, falling back to python. Shared by
// requirePythonRuntime (existence + pip check), systemdUnitFor
// (ExecStart), and cmdDeploy (pip install step) so all three agree on
// the same interpreter.
func pythonBinary() string {
	if p := osutil.Which("python3"); p != "" {
		return p
	}
	if p := osutil.Which("python"); p != "" {
		return p
	}
	return "python3"
}

// requirePythonRuntime is the python equivalent of requireGoRuntime.
// Unlike go, python needs no build step, but the same process-provider
// runtime (systemd or PM2) must be present, and pip must be available
// since cmdDeploy uses it to install requirements.txt changes.
func (a *App) requirePythonRuntime() error {
	if !commandExists("python3") && !commandExists("python") {
		return a.errf("template requires Python. Missing: python3. Install it, then run: wor doctor")
	}
	if err := exec.Command(pythonBinary(), "-m", "pip", "--version").Run(); err != nil {
		return a.errf("template requires pip (python3 -m pip). Missing: pip. Install it, then run: wor doctor")
	}
	if osutil.IsLinux() && !commandExists("systemctl") {
		return a.errf("template requires systemd (systemctl not found) on Linux. Install it, then run: wor doctor")
	}
	if !osutil.IsLinux() && !commandExists("pm2") {
		return a.errf("template requires PM2 on this OS (no systemd). Missing: pm2. Install it, then run: wor doctor")
	}
	return nil
}

// runtimeVersionLabel returns a short "installed" indicator for
// template's runtime, for display next to a template choice in
// `wor create`'s picker (e.g. "node v24.16.0", "not installed").
// static has no runtime, so it returns "".
func runtimeVersionLabel(template string) string {
	switch template {
	case "node":
		if !osutil.Exists("node") {
			return "not installed"
		}
		return strings.TrimSpace(osutil.RunVersion("node", "--version"))
	case "go":
		if !osutil.Exists("go") {
			return "not installed"
		}
		fields := strings.Fields(osutil.RunVersion("go", "version"))
		if len(fields) >= 3 {
			return fields[2]
		}
		return strings.TrimSpace(osutil.RunVersion("go", "version"))
	case "python":
		bin := "python3"
		if !osutil.Exists(bin) {
			if osutil.Exists("python") {
				bin = "python"
			} else {
				return "not installed"
			}
		}
		v := strings.TrimSpace(osutil.RunVersion(bin, "--version"))
		return strings.TrimPrefix(v, "Python ")
	case "php":
		if !osutil.Exists("php") {
			return "not installed"
		}
		fields := strings.Fields(osutil.RunVersion("php", "--version"))
		if len(fields) >= 2 {
			return fields[0] + " " + fields[1]
		}
		return strings.TrimSpace(osutil.RunVersion("php", "--version"))
	default:
		return ""
	}
}

// requirePHPRuntime mirrors require_php_runtime(), now version-aware: it
// succeeds if either a per-service PHP-FPM version can be detected (see
// phpfpm.DetectVersions) or the legacy host-wide PHP_FPM_ENDPOINT
// resolves. New php services prefer a per-service pool when one is
// available (see resolvePHPVersion), but this check never hard-requires
// it, so hosts that haven't set up a per-version pool.d layout (or are
// on Windows) keep working exactly as before.
func (a *App) requirePHPRuntime() error {
	if len(phpfpm.DetectVersions()) > 0 {
		return nil
	}
	if _, ok := hostprovider.PHPFPMEndpoint(a.Cfg); ok {
		return nil
	}
	return a.errf("template requires PHP-FPM runtime. Install a per-version PHP-FPM (/etc/php/<version>/fpm on Linux, php@<version> via Homebrew on macOS) or configure PHP_FPM_ENDPOINT in %s/host.env", a.Cfg.Configs)
}

// phpVersionNumbers renders versions' numbers as a comma-joined list
// for error messages (e.g. "8.3, 8.4").
func phpVersionNumbers(versions []phpfpm.Version) string {
	nums := make([]string, len(versions))
	for i, v := range versions {
		nums[i] = v.Number
	}
	return strings.Join(nums, ", ")
}

// resolvePHPVersion decides which PHP-FPM version (if any) a new php
// service should get its own dedicated pool under. requested is the
// --php-version= flag value (may be empty); noPool forces the legacy
// shared/global PHP_FPM_ENDPOINT behavior even when per-version pools
// are detected. Returns "" when the service should use the legacy
// endpoint instead of a per-service pool -- e.g. no versions are
// auto-detectable on this host/OS at all, matching the no-forced-
// migration decision: a service only ever gets a per-service pool when
// one can actually be resolved, never as a hard requirement.
func (a *App) resolvePHPVersion(requested string, noPool bool) (string, error) {
	if noPool {
		return "", nil
	}
	versions := phpfpm.DetectVersions()
	if len(versions) == 0 {
		return "", nil
	}
	if requested != "" {
		for _, v := range versions {
			if v.Number == requested {
				return requested, nil
			}
		}
		return "", a.errf("PHP version %s not found (detected: %s)", requested, phpVersionNumbers(versions))
	}
	if len(versions) == 1 {
		return versions[0].Number, nil
	}
	return "", a.errf("multiple PHP-FPM versions detected (%s); specify --php-version=", phpVersionNumbers(versions))
}

// setupPHPPool creates domain/service's dedicated php-fpm pool: its
// pool identity (unix user + group -- see below), the pool config file
// itself (validated + reloaded by phpfpm.WritePool), and finally
// records the result on the service (Store.SetServicePHPFPM). Called
// right after the service's document root exists (servicefiles.Create)
// and the service is registered (Store.AddService) -- config on disk
// only ever reflects a pool that was actually created, never a
// half-applied one.
//
// Pool identity differs by OS: on Linux, systemd runs php-fpm's master
// as root, so it can chown the pool's socket to (and run the pool's
// workers as) a dedicated unix user wor creates just for this service --
// full isolation between services. On macOS, Homebrew's php-fpm master
// runs as the current login user (not root), and an unprivileged master
// cannot chown a socket to, or switch a worker to, a *different* unix
// user -- attempting to caused a real "failed to chown() the socket"
// failure the first time this shipped. So macOS pools instead run as
// that same current user: no isolation between services on macOS
// (found/decided 2026-07-05), only Linux gets the originally-designed
// per-service unix-user isolation.
func (a *App) setupPHPPool(domain, service, phpVersion string) error {
	version, ok := phpfpm.ResolveVersion(phpVersion)
	if !ok {
		return a.errf("PHP %s is no longer detected on this host", phpVersion)
	}

	var poolUser, group, listenOwner string
	if osutil.IsMacOS() {
		u, g, err := phpfpm.CurrentUnixUser()
		if err != nil {
			return err
		}
		poolUser, group = u, g
	} else {
		poolUser = phpfpm.PoolName(domain, service)
		docRoot := filepath.Join(a.Store.ServiceDir(domain, service), "public")
		if err := phpfpm.EnsureUser(poolUser); err != nil {
			return err
		}
		g, err := phpfpm.GrantGroupAccess(docRoot, poolUser)
		if err != nil {
			return err
		}
		group = g
		// The socket's owner must be the WEB SERVER's run user, not the
		// pool user: the socket is 0660 and nginx/apache is the process
		// that connect()s to it. listen.owner = pool user produced a
		// real 502 on Debian (www-data denied on the socket) while every
		// wor-side check passed. Falls back to the pool user if the
		// detected web user doesn't resolve to a real account (e.g. no
		// web server installed yet) -- php-fpm refuses to start a pool
		// whose listen.owner doesn't exist.
		if webUser := webServerRunUser(a.Cfg.HostProviderName()); webUserExists(webUser) {
			listenOwner = webUser
		}
	}

	pool := phpfpm.Pool{Domain: domain, Service: service, Version: version, User: poolUser, Group: group,
		ListenOwner: listenOwner, ListenGroup: listenOwner}
	if err := phpfpm.WritePool(pool); err != nil {
		return err
	}

	// php-fpm only chown()s a pool's socket when it BINDS it: if a
	// socket for this pool already existed (stale file from an older
	// config), the reload WritePool just did keeps the old ownership,
	// and the web server stays locked out (502) no matter what the
	// fresh config says -- a full restart is what re-creates the
	// socket (real Debian host, 2026-07-07). Detect the mismatch and
	// offer the restart: it briefly interrupts every pool under this
	// php-fpm master, so the admin decides.
	if listenOwner != "" {
		sock := phpfpm.SocketPath(version, domain, service)
		if socketDeniesUser(sock, listenOwner) {
			a.warn("pool socket %s is not connectable by the web server user (%s) -- stale socket ownership; php-fpm reload does not re-chown an existing socket", sock, listenOwner)
			if a.confirmYesDefaultYes(fmt.Sprintf("Restart php-fpm %s now to re-create the socket (briefly interrupts its other pools)?", version.Number)) {
				if err := phpfpm.Restart(version); err != nil {
					a.warn("restart failed: %s -- run manually: sudo systemctl restart %s", err, version.ReloadUnit)
				}
			} else {
				a.info("Run later: sudo systemctl restart %s", version.ReloadUnit)
			}
		}
	}

	return a.Store.SetServicePHPFPM(domain, service, phpVersion, group, 0)
}

// teardownPHPPool removes domain/service's dedicated php-fpm pool (pool
// config file + unix user) and clears its PHP-FPM record, best-effort
// like systemd.RemoveUnit: failures are warned about, not fatal, so a
// service can still be removed even if its pool was already gone or
// PHP-FPM itself was uninstalled in the meantime.
func (a *App) teardownPHPPool(domain, service string) {
	phpVersion := a.Store.GetServicePHPVersion(domain, service)
	if phpVersion == "" {
		return
	}
	if version, ok := phpfpm.ResolveVersion(phpVersion); ok {
		if err := phpfpm.RemovePool(version, domain, service); err != nil {
			a.warn("could not remove php-fpm pool: %s", err)
		}
	} else {
		a.warn("could not resolve PHP %s to remove its php-fpm pool (already uninstalled?)", phpVersion)
	}
	// macOS pools run as the current login user (see setupPHPPool) --
	// there's no dedicated per-service unix user to remove there.
	if !osutil.IsMacOS() {
		if err := phpfpm.RemoveUser(phpfpm.PoolName(domain, service)); err != nil {
			a.warn("could not remove php-fpm pool user: %s", err)
		}
	}
	if err := a.Store.ClearServicePHPFPM(domain, service); err != nil {
		a.warn("could not clear php-fpm record: %s", err)
	}
}

// requireTemplateRuntime hard-blocks service creation (in both
// `wor create` and `wor service add`) when template's runtime isn't
// installed -- no interactive "configure now?" prompt, per the
// go/python/systemd redesign: detect and block, with a clear message
// pointing at `wor doctor`.
func (a *App) requireTemplateRuntime(template string) error {
	if domainmodel.TemplateRequiresNode(template) {
		if err := a.requireNodeRuntime(); err != nil {
			return err
		}
	}
	if domainmodel.TemplateRequiresGo(template) {
		if err := a.requireGoRuntime(); err != nil {
			return err
		}
	}
	if domainmodel.TemplateRequiresPython(template) {
		if err := a.requirePythonRuntime(); err != nil {
			return err
		}
	}
	if domainmodel.TemplateRequiresPHP(template) {
		if err := a.requirePHPRuntime(); err != nil {
			return err
		}
	}
	return nil
}

// requireServiceExists errors out with a clear message if domain/service
// isn't a registered service, instead of letting the target fall
// through to Store.GetServiceType's "static" fallback -- which exists
// so a service record written before the Type field existed still
// resolves to something, but silently gives the same answer for a
// typo'd or never-created domain/service. Without this check,
// `start`/`stop`/`restart`/`logs` on a nonexistent target were
// misreporting it as "a static service with nothing to do" (a quiet
// [OK]) instead of failing loudly.
func (a *App) requireServiceExists(domain, service string) error {
	if !a.Store.ServiceExists(domain, service) {
		return a.errf("service not found: %s/%s", domain, service)
	}
	return nil
}

// buildGoService compiles a go template's service directory into the
// binary named by entry, via `go build -o <entry> .`. Called both at
// service creation and by `wor deploy` on every pulled commit (see
// deploy.go) -- go, unlike node/python, needs a build step before its
// entry point is runnable at all.
func (a *App) buildGoService(dir, entry string) error {
	cmd := exec.Command("go", "build", "-o", entry, ".")
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = a.Out, a.Err
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go build failed: %w", err)
	}
	return nil
}

// systemdUnitFor builds the systemd.Unit description for a go/python
// service, resolving ExecStart to an absolute path (systemd does not
// expand relative paths against WorkingDirectory the way a shell would).
func (a *App) systemdUnitFor(domain, service, template, entry string) systemd.Unit {
	dir := a.Store.ServiceDir(domain, service)
	execStart := filepath.Join(dir, entry)
	if domainmodel.PythonTemplates[template] {
		execStart = pythonBinary() + " " + filepath.Join(dir, entry)
	}
	env := map[string]string{}
	if cfg, err := a.Store.LoadServices(domain); err == nil {
		if svc := cfg.FindService(service); svc != nil {
			for k, v := range svc.Env {
				env[k] = v
			}
		}
	}
	return systemd.Unit{
		Domain:      domain,
		Service:     service,
		Description: fmt.Sprintf("wor service %s/%s (%s)", domain, service, template),
		WorkingDir:  dir,
		ExecStart:   execStart,
		Env:         env,
	}
}

// startWrittenProcess starts domain/service's supervised process --
// shared by `wor service start` and `wor service add`'s default
// auto-start (see the "add" case below), so both entry points agree on
// exactly the same start sequence. Callers must have already written
// the pm2 ecosystem entry / systemd unit file (WriteEcosystem/
// WriteUnit) before calling this -- it only starts what's already on
// disk, it does not write anything itself. provider must be "pm2" or
// "systemd" (callers get this from domainmodel.ProcessProviderFor);
// any other value is a no-op.
func (a *App) startWrittenProcess(domain, service, provider string) error {
	switch provider {
	case "pm2":
		name := pm2.Name(domain, service)
		if err := pm2.Run("start", pm2.EcosystemPath(a.Store.DomainDir(domain)), "--only", name); err != nil {
			return err
		}
		return pm2.Save()
	case "systemd":
		if err := systemd.Enable(domain, service); err != nil {
			a.warn("systemd enable failed: %s", err)
		}
		return systemd.Start(domain, service)
	}
	return nil
}

func (a *App) cmdService(args []string) error {
	if len(args) == 0 {
		a.usage()
		return a.errf("service action required")
	}
	action := args[0]
	if action == "status" {
		return a.cmdServiceStatus()
	}
	if len(args) < 2 {
		a.usage()
		return a.errf("service target required: domain/service")
	}
	domain, service, err := domainmodel.ParseTarget(args[1])
	if err != nil {
		return err
	}
	if service == "" {
		return a.errf("service required: domain/service")
	}
	name := pm2.Name(domain, service)
	rest := args[2:]
	fl := parseFlags(rest)

	switch action {
	case "add":
		template := domainmodel.AllTemplates[0]
		if fl.Get("service-type", "") != "" {
			template = fl.Get("service-type", "")
		}
		if !domainmodel.IsValidTemplate(template) {
			return a.errf("unknown service type: %s", template)
		}
		if err := a.requireTemplateRuntime(template); err != nil {
			return err
		}
		var phpVersion string
		if domainmodel.TemplateRequiresPHP(template) {
			v, err := a.resolvePHPVersion(fl.Get("php-version", ""), fl.Has("no-php-pool"))
			if err != nil {
				return err
			}
			phpVersion = v
		}
		if err := a.Store.MakeDomainFiles(domain); err != nil {
			return err
		}
		serviceDir := a.Store.ServiceDir(domain, service)
		if _, err := os.Stat(serviceDir); err == nil {
			return a.errf("service folder already exists: %s", serviceDir)
		}
		port := 0
		if domainmodel.TemplateRequiresPort(template) {
			portStr := fl.Get("port", "")
			if portStr == "" {
				p, err := a.findNextPort(3000)
				if err != nil {
					return err
				}
				port = p
			} else {
				if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil || port == 0 {
					return a.errf("invalid port: %s", portStr)
				}
			}
		}
		entry := fl.Get("entry", "")
		if entry == "" {
			entry = domainmodel.DefaultEntryPoint(template)
		}
		if err := servicefiles.Create(serviceDir, service, portOrDefault(port), template); err != nil {
			return err
		}
		if domainmodel.TemplateRequiresGo(template) {
			a.info("Building Go binary...")
			if err := a.buildGoService(serviceDir, entry); err != nil {
				return err
			}
		}
		host := fl.Get("host", "")
		if err := a.Store.AddService(domain, service, host, port, template, entry); err != nil {
			return err
		}
		switch domainmodel.ProcessProviderFor(template) {
		case "pm2":
			if err := pm2.WriteEcosystem(a.Store, domain); err != nil {
				return err
			}
		case "systemd":
			if err := systemd.WriteUnit(a.systemdUnitFor(domain, service, template, entry)); err != nil {
				return err
			}
		}
		if phpVersion != "" {
			if err := a.setupPHPPool(domain, service, phpVersion); err != nil {
				return err
			}
		}
		switch {
		case domainmodel.TemplateRequiresPort(template):
			a.ok("Service ready: %s/%s (%s, port %d)", domain, service, template, port)
		case phpVersion != "":
			a.ok("Service ready: %s/%s (%s, PHP %s pool)", domain, service, template, phpVersion)
		default:
			a.ok("Service ready: %s/%s (%s)", domain, service, template)
		}
		// Default is to bring the service up immediately after creating
		// it -- `wor service add` on its own used to just write config
		// and leave node/go/python services not actually running until
		// a separate `wor service start`/`wor run`. --no-start opts back
		// out of that (e.g. scripted provisioning that wants to
		// configure something -- env vars, secrets -- before the process
		// ever starts). A failed auto-start is reported but does not
		// fail `add` itself: the service *was* successfully created
		// (config/ecosystem/unit already written above), and returning
		// an error here would make `add` look like it failed outright --
		// worse, retrying it would then hit "service folder already
		// exists" with no obvious way forward. `wor service start
		// <domain>/<service>` remains the explicit, second-chance way to
		// bring it up.
		if provider := domainmodel.ProcessProviderFor(template); provider != "" && !fl.Has("no-start") {
			if err := a.startWrittenProcess(domain, service, provider); err != nil {
				a.warn("service created but failed to start: %s", err)
				a.info("Run manually once fixed: wor service start %s/%s", domain, service)
			} else {
				a.ok("Service started: %s/%s", domain, service)
			}
		} else if provider != "" {
			a.info("Service not started (--no-start). Run: wor service start %s/%s", domain, service)
		}
		return nil

	case "remove":
		cascade := fl.Has("cascade")
		yes := fl.Has("yes") || fl.Has("y")
		hosts, _ := a.Store.ListHostsForService(domain, service)
		if len(hosts) > 0 && !cascade {
			fmt.Fprintln(a.Err, "ERROR: service is still referenced by host(s):")
			for _, h := range hosts {
				fmt.Fprintf(a.Err, "  - %s\n", h)
			}
			fmt.Fprintln(a.Err, "\nRemove host(s) first, or use cascade:")
			fmt.Fprintf(a.Err, "  wor service remove %s/%s --cascade\n", domain, service)
			return a.errf("service removal blocked by registered hosts")
		}
		if !yes {
			if cascade && len(hosts) > 0 {
				fmt.Fprintf(a.Out, "This will remove service %s/%s and related host(s):\n", domain, service)
				for _, h := range hosts {
					fmt.Fprintf(a.Out, "  - %s\n", h)
				}
			}
			if !a.requireTyped(fmt.Sprintf("Remove service %s/%s ? Type YES: ", domain, service), "YES") {
				return a.errf("cancelled")
			}
		}
		if cascade && len(hosts) > 0 {
			provider, err := a.Provider()
			if err == nil {
				for _, h := range hosts {
					provider.RemoveHostFiles(h)
					a.Store.RemoveHostFromServices(h)
					if err := hostsfile.Remove(h); err != nil {
						a.warn("could not remove hosts file entry for %s: %s (%s)", h, err, osutil.ElevationHint())
					}
					a.ok("Host removed: %s", h)
				}
				provider.Reload()
			}
		}
		t := a.Store.GetServiceType(domain, service)
		if domainmodel.TemplateRequiresPHP(t) {
			a.teardownPHPPool(domain, service)
		}
		switch domainmodel.ProcessProviderFor(t) {
		case "pm2":
			if commandExists("pm2") {
				pm2.Run("delete", name)
				pm2.Save()
			}
		case "systemd":
			if err := systemd.RemoveUnit(domain, service); err != nil {
				a.warn("could not remove systemd unit: %s", err)
			}
		}
		os.RemoveAll(a.Store.ServiceDir(domain, service))
		a.Store.RemoveService(domain, service)
		pm2.WriteEcosystem(a.Store, domain)
		a.ok("Service removed: %s/%s", domain, service)
		return nil

	case "start":
		if err := a.requireServiceExists(domain, service); err != nil {
			return err
		}
		t := a.Store.GetServiceType(domain, service)
		provider := domainmodel.ProcessProviderFor(t)
		if provider == "" {
			a.ok("%s service is served by host provider; no process to start: %s/%s", t, domain, service)
			return nil
		}
		if err := a.requireTemplateRuntime(t); err != nil {
			return err
		}
		switch provider {
		case "pm2":
			if err := pm2.WriteEcosystem(a.Store, domain); err != nil {
				return err
			}
		case "systemd":
			entry := a.Store.GetServiceEntryPoint(domain, service)
			if err := systemd.WriteUnit(a.systemdUnitFor(domain, service, t, entry)); err != nil {
				return err
			}
		}
		return a.startWrittenProcess(domain, service, provider)

	case "stop":
		if err := a.requireServiceExists(domain, service); err != nil {
			return err
		}
		t := a.Store.GetServiceType(domain, service)
		switch domainmodel.ProcessProviderFor(t) {
		case "pm2":
			if err := pm2.Run("stop", name); err != nil {
				a.warn("PM2 process not found: %s", name)
			}
			return pm2.Save()
		case "systemd":
			if err := systemd.Stop(domain, service); err != nil {
				a.warn("systemd stop failed: %s", err)
			}
			return nil
		default:
			a.ok("%s service is served by host provider; nothing to stop: %s/%s", t, domain, service)
			return nil
		}

	case "restart":
		if err := a.requireServiceExists(domain, service); err != nil {
			return err
		}
		t := a.Store.GetServiceType(domain, service)
		provider := domainmodel.ProcessProviderFor(t)
		if provider == "" {
			a.ok("%s service is served by host provider; reload host instead.", t)
			return nil
		}
		if err := a.requireTemplateRuntime(t); err != nil {
			return err
		}
		switch provider {
		case "pm2":
			if err := pm2.Run("restart", name); err != nil {
				return err
			}
			return pm2.Save()
		case "systemd":
			return systemd.Restart(domain, service)
		}
		return nil

	case "logs":
		if err := a.requireServiceExists(domain, service); err != nil {
			return err
		}
		t := a.Store.GetServiceType(domain, service)
		lines := fl.Get("lines", "100")
		switch domainmodel.ProcessProviderFor(t) {
		case "systemd":
			return systemd.Logs(domain, service, lines)
		case "pm2":
			return pm2.Run("logs", name, "--lines", lines)
		default:
			a.ok("%s service is served by host provider; no process logs. Use: wor host logs <host>", t)
			return nil
		}

	default:
		a.usage()
		return a.errf("unknown service action: %s", action)
	}
}

func portOrDefault(p int) int {
	if p <= 0 {
		return 3000
	}
	return p
}

// findNextPort mirrors lib/webserver.sh find_next_port(): the first
// port >= start not already configured or actively listening.
func (a *App) findNextPort(start int) (int, error) {
	used, err := a.Store.ConfiguredPorts()
	if err != nil {
		return 0, err
	}
	port := start
	for {
		if !used[port] && !portInUse(port) {
			return port, nil
		}
		port++
	}
}

func commandExists(name string) bool { return osutil.Exists(name) }

// portInUse probes whether a TCP port is currently bound on 127.0.0.1
// by attempting to listen on it -- a portable equivalent to the shell
// version's `ss`/`lsof`/`netstat` probes that works unchanged on
// Windows.
func portInUse(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return true
	}
	ln.Close()
	return false
}

// statusRow is one rendered line of `wor service status`.
type statusRow struct {
	target  string // "domain/service"
	state   string
	port    string
	extra   string // "pid N" and/or uptime, space-joined
	enabled bool   // config state -- drives the row's check/cross mark
	known   bool   // whether live process state was actually queried (pm2/systemd groups)
	ok      bool   // true = running/active; only meaningful when known

	// procName/cpuStr/memStr are only set for pm2/systemd rows (php and
	// static have no supervised process to name or measure), and render
	// as an extra indented line under the row -- see cmdServiceStatus.
	procName string // "wor_<domain>_<service>", the pm2/systemd process name
	cpuStr   string // e.g. "0.4%", or "-" if not available
	memStr   string // e.g. "132mb", or "-" if not available
}

// statusGroup is one process-provider section of `wor service status`
// (PM2, systemd, PHP-FPM, static), rendered in that order regardless of
// which domains/services happen to populate them.
type statusGroup struct {
	key   string
	label string
	rows  []statusRow
}

// cmdServiceStatus implements `wor service status`. Earlier this simply
// ran `pm2 status`, which only ever showed node services -- go/python
// (systemd-managed on Linux) and php/static services were invisible.
// This instead enumerates every enabled service across every domain
// (Store.ListAllServices) and groups it by the process provider that
// actually manages it (domainmodel.ProcessProviderFor), querying each
// provider's live state: pm2 jlist once for every node service, plus a
// single batched systemctl sampling pass (systemd.GetInfoBatch) for
// every go/python service, so `wor service status` pays pm2/systemd's
// query cost once regardless of how many services it reports on. php
// (PHP-FPM, assumed already running) and static (no process) have
// nothing to query, so they're listed with an n/a state instead of
// being silently omitted.
func (a *App) cmdServiceStatus() error {
	refs, err := a.Store.ListAllServices()
	if err != nil {
		return err
	}

	order := []string{"pm2", "systemd", "php", "static"}
	labels := map[string]string{
		"pm2":     "PM2 (node)",
		"systemd": "SYSTEMD (go/python)",
		"php":     "PHP-FPM (php)",
		"static":  "STATIC (no process)",
	}
	groups := map[string]*statusGroup{}
	for _, key := range order {
		groups[key] = &statusGroup{key: key, label: labels[key]}
	}

	needPM2 := false
	var systemdRefs []systemd.Ref
	for _, ref := range refs {
		if !ref.Service.Enabled {
			continue
		}
		switch domainmodel.ProcessProviderFor(ref.Service.Type) {
		case "pm2":
			needPM2 = true
		case "systemd":
			systemdRefs = append(systemdRefs, systemd.Ref{Domain: ref.Domain, Service: ref.Service.Name})
		}
	}

	var pm2Procs map[string]pm2.ProcessInfo
	if needPM2 {
		if procs, err := pm2.List(); err != nil {
			a.warn("pm2 status unavailable: %s", err)
		} else {
			pm2Procs = procs
		}
	}
	var systemdInfo map[systemd.Ref]systemd.Info
	if len(systemdRefs) > 0 {
		systemdInfo = systemd.GetInfoBatch(systemdRefs)
	}

	for _, ref := range refs {
		svc := ref.Service
		target := ref.Domain + "/" + svc.Name
		portStr := "-"
		if domainmodel.TemplateRequiresPort(svc.Type) {
			if p, err := a.Store.GetServicePort(ref.Domain, svc.Name); err == nil && p != 0 {
				portStr = fmt.Sprintf(":%d", p)
			}
		}

		groupKey := domainmodel.ProcessProviderFor(svc.Type)
		if groupKey == "" {
			groupKey = "static"
			if domainmodel.TemplateRequiresPHP(svc.Type) {
				groupKey = "php"
			}
		}

		// Disabled services are listed (owner decision: "why did my
		// service disappear" is a recurring confusion when they're
		// hidden) but never queried -- there is no process to ask about.
		if !svc.Enabled {
			groups[groupKey].rows = append(groups[groupKey].rows, statusRow{
				target: target, state: "disabled", port: portStr,
			})
			continue
		}

		switch groupKey {
		case "pm2":
			row := statusRow{target: target, port: portStr, enabled: true, procName: pm2.Name(ref.Domain, svc.Name)}
			if info, ok := pm2Procs[row.procName]; ok {
				row.known = true
				row.state = info.Status
				row.ok = info.Status == "online"
				row.cpuStr = formatPercent(info.CPUPercent)
				row.memStr = formatBytes(info.MemoryBytes)
				if info.PID != 0 {
					row.extra = fmt.Sprintf("pid %d", info.PID)
				}
				if u := formatUptime(info.Uptime); u != "" {
					if row.extra != "" {
						row.extra += "  "
					}
					row.extra += u
				}
			} else {
				row.state = "not started"
				row.cpuStr, row.memStr = "-", "-"
			}
			groups["pm2"].rows = append(groups["pm2"].rows, row)

		case "systemd":
			// The systemd unit name convention ("wor_<domain>_<service>")
			// is identical to pm2's, just with a ".service" suffix pm2.Name
			// doesn't have -- reuse it rather than duplicating the format.
			row := statusRow{target: target, port: portStr, enabled: true, procName: pm2.Name(ref.Domain, svc.Name), cpuStr: "-", memStr: "-"}
			if info, ok := systemdInfo[systemd.Ref{Domain: ref.Domain, Service: svc.Name}]; ok {
				row.known = true
				row.state = info.State
				row.ok = info.Active
				if info.PID != 0 {
					row.extra = fmt.Sprintf("pid %d", info.PID)
				}
				if info.CPUKnown {
					row.cpuStr = formatPercent(info.CPUPercent)
				}
				if info.MemKnown {
					row.memStr = formatBytes(info.MemoryBytes)
				}
			} else {
				row.state = "unknown"
			}
			groups["systemd"].rows = append(groups["systemd"].rows, row)

		default:
			if domainmodel.TemplateRequiresPHP(svc.Type) {
				row := statusRow{target: target, port: "n/a", enabled: true}
				if svc.PHPVersion != "" {
					// Dedicated per-service pool -- check this pool's
					// own socket (phpfpm.PoolAlive) rather than
					// phpfpm.IsRunning: IsRunning answers "is the shared
					// master service up", which depends on correctly
					// guessing its Homebrew/systemd service name and on
					// pgrep matching its binary path -- both of which
					// can (and, per a live report, did) return a false
					// "not running" for a pool that was actually up and
					// serving traffic the whole time. PoolAlive
					// sidesteps both by dialing the pool's socket
					// directly.
					if v, ok := phpfpm.ResolveVersion(svc.PHPVersion); ok {
						row.known = true
						row.ok = phpfpm.PoolAlive(v, ref.Domain, svc.Name)
						if row.ok {
							row.state = fmt.Sprintf("running (php %s pool)", svc.PHPVersion)
						} else {
							row.state = fmt.Sprintf("not running (php %s pool)", svc.PHPVersion)
						}
					} else {
						row.known = true
						row.ok = false
						row.state = fmt.Sprintf("php %s no longer detected on this host", svc.PHPVersion)
					}
				} else {
					// Legacy host-wide PHP_FPM_ENDPOINT: wor never
					// manages this master's lifecycle and has no
					// per-service way to probe it, so there's nothing
					// to actually check -- "assumed" is the honest
					// answer, not "unknown". Rendered ok (green) rather
					// than the gray "unknown" dot, since that dot means
					// "we expected a supervised process and got no
					// answer back", which isn't this case.
					row.known = true
					row.ok = true
					row.state = "assumed running (fpm)"
				}
				groups["php"].rows = append(groups["php"].rows, row)
			} else {
				// static has no supervised process at all -- nothing
				// here can fail, so it's always shown ok. Whether the
				// web server itself is up is `wor host list`'s job, not
				// this row's.
				groups["static"].rows = append(groups["static"].rows, statusRow{
					target: target, state: "served by web server", port: "n/a",
					enabled: true, known: true, ok: true,
				})
			}
		}
	}

	any := false
	for _, key := range order {
		if len(groups[key].rows) > 0 {
			any = true
			break
		}
	}
	if !any {
		fmt.Fprintln(a.Out, "No services found.")
		return nil
	}

	useColor := a.colorEnabled()
	first := true
	for _, key := range order {
		g := groups[key]
		if len(g.rows) == 0 {
			continue
		}
		if !first {
			fmt.Fprintln(a.Out)
		}
		first = false
		fmt.Fprintln(a.Out, colorize(useColor, ansiPink, g.label))

		// Render the primary rows through a tabwriter first so their
		// columns align across the whole group, capturing the aligned
		// result instead of writing straight to a.Out -- that way each
		// row's proc-name/cpu/mem sub-line (which isn't part of the
		// tabwriter's column grid) can be spliced in right after its
		// row without disturbing the alignment pass.
		var buf bytes.Buffer
		tw := tabwriter.NewWriter(&buf, 0, 4, 3, ' ', 0)
		for _, row := range g.rows {
			// The mark carries CONFIG state only (blue check = enabled,
			// red cross = disabled) -- deliberately no green dot: green
			// read as "the site is healthy", which this command never
			// verifies (that's `wor diagnose`; see the closing hint).
			// Live process state is the STATE column instead, rendered
			// red when an enabled service's process isn't running. It's
			// the last column so its ANSI codes can't skew tabwriter's
			// width calculation for anything after it.
			var mark string
			if row.enabled {
				mark = tag(useColor, ansiBlue, "✓", "[on]")
			} else {
				mark = tag(useColor, ansiRed, "✗", "[off]")
			}
			state := row.state
			switch {
			case !row.enabled:
				state = colorize(useColor, ansiDim, state)
			case !row.ok:
				state = colorize(useColor, ansiRed, state)
			}
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n", mark, row.target, row.port, row.extra, state)
		}
		tw.Flush()
		lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")

		for i, row := range g.rows {
			if i < len(lines) {
				fmt.Fprintln(a.Out, lines[i])
			}
			if row.procName == "" {
				continue
			}
			sub := fmt.Sprintf("%-28s cpu %-7s mem %s", row.procName, row.cpuStr, row.memStr)
			fmt.Fprintf(a.Out, "      %s\n", colorize(useColor, ansiDim, sub))
		}
	}

	// The one-line antidote to "everything is green but the site is
	// down": say out loud what this command does NOT check.
	fmt.Fprintln(a.Out)
	fmt.Fprintln(a.Out, colorize(useColor, ansiDim, "(process status only -- for end-to-end health: wor health)"))
	return nil
}
