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
					if err := hostsfile.Remove(h); err != nil {
						a.warn("could not remove hosts file entry for %s: %s (%s)", h, err, osutil.ElevationHint())
					}
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
	target string // "domain/service"
	state  string
	port   string
	extra  string // "pid N" and/or uptime, space-joined
	known  bool   // whether live process state was actually queried (pm2/systemd groups)
	ok     bool   // true = running/active; only meaningful when known

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
		if !svc.Enabled {
			continue
		}
		target := ref.Domain + "/" + svc.Name
		portStr := "-"
		if domainmodel.TemplateRequiresPort(svc.Type) {
			if p, err := a.Store.GetServicePort(ref.Domain, svc.Name); err == nil && p != 0 {
				portStr = fmt.Sprintf(":%d", p)
			}
		}

		switch domainmodel.ProcessProviderFor(svc.Type) {
		case "pm2":
			row := statusRow{target: target, port: portStr, procName: pm2.Name(ref.Domain, svc.Name)}
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
			row := statusRow{target: target, port: portStr, procName: pm2.Name(ref.Domain, svc.Name), cpuStr: "-", memStr: "-"}
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
				groups["php"].rows = append(groups["php"].rows, statusRow{
					target: target, state: "assumed running (fpm)", port: "n/a",
				})
			} else {
				groups["static"].rows = append(groups["static"].rows, statusRow{
					target: target, state: "served by web server", port: "n/a",
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
		fmt.Fprintln(a.Out, "No enabled services found.")
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
			var glyph string
			switch {
			case !row.known:
				glyph = tag(useColor, ansiGray, "·", "[--]")
			case row.ok:
				glyph = tag(useColor, ansiGreen, "●", "[ok]")
			default:
				glyph = tag(useColor, ansiRed, "●", "[fail]")
			}
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n", glyph, row.target, row.state, row.port, row.extra)
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
			fmt.Fprintf(a.Out, "      %s\n", colorize(useColor, ansiGray, sub))
		}
	}
	return nil
}
