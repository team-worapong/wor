package cliapp

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"wor/internal/domainmodel"
	"wor/internal/hostprovider"
	"wor/internal/osutil"
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

// requirePythonRuntime is the python equivalent of requireGoRuntime.
// Unlike go, python needs no build step, but the same process-provider
// runtime (systemd or PM2) must be present.
func (a *App) requirePythonRuntime() error {
	if !commandExists("python3") && !commandExists("python") {
		return a.errf("template requires Python. Missing: python3. Install it, then run: wor doctor")
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

// requirePHPRuntime mirrors require_php_runtime().
func (a *App) requirePHPRuntime() error {
	if _, ok := hostprovider.PHPFPMEndpoint(a.Cfg); ok {
		return nil
	}
	return a.errf("template requires PHP-FPM runtime. Configure PHP_FPM_ENDPOINT in %s/host.env", a.Cfg.Configs)
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
		python := "python3"
		if p := osutil.Which("python3"); p != "" {
			python = p
		} else if p := osutil.Which("python"); p != "" {
			python = p
		}
		execStart = python + " " + filepath.Join(dir, entry)
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

func (a *App) cmdService(args []string) error {
	if len(args) == 0 {
		a.usage()
		return a.errf("service action required")
	}
	action := args[0]
	if action == "status" {
		// Lists PM2-managed processes only; systemd-managed go/python
		// services are inspected individually via `wor service logs`
		// or `systemctl status wor_<domain>_<service>`.
		return pm2.Run("status")
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
		if domainmodel.TemplateRequiresPort(template) {
			a.ok("Service ready: %s/%s (%s, port %d)", domain, service, template, port)
		} else {
			a.ok("Service ready: %s/%s (%s)", domain, service, template)
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
					a.ok("Host removed: %s", h)
				}
				provider.Reload()
			}
		}
		t := a.Store.GetServiceType(domain, service)
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
			if err := pm2.Run("start", pm2.EcosystemPath(a.Store.DomainDir(domain)), "--only", name); err != nil {
				return err
			}
			return pm2.Save()
		case "systemd":
			entry := a.Store.GetServiceEntryPoint(domain, service)
			if err := systemd.WriteUnit(a.systemdUnitFor(domain, service, t, entry)); err != nil {
				return err
			}
			if err := systemd.Enable(domain, service); err != nil {
				a.warn("systemd enable failed: %s", err)
			}
			return systemd.Start(domain, service)
		}
		return nil

	case "stop":
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
		t := a.Store.GetServiceType(domain, service)
		lines := fl.Get("lines", "100")
		if domainmodel.ProcessProviderFor(t) == "systemd" {
			return systemd.Logs(domain, service, lines)
		}
		return pm2.Run("logs", name, "--lines", lines)

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
