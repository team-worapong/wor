package cliapp

import (
	"fmt"
	"os/exec"
	"strings"

	"wor/internal/domainmodel"
	"wor/internal/osutil"
	"wor/internal/phpfpm"
	"wor/internal/pm2"
	"wor/internal/ssl"
	"wor/internal/systemd"
)

// maxDumpLines caps how many lines of a raw sub-process dump (pm2
// describe, systemctl status) `wor info` shows -- long enough to be
// useful, short enough that one target's output can't scroll another
// target's info off screen.
const maxDumpLines = 25

func (a *App) cmdInfo(args []string) error {
	if len(args) == 0 {
		a.usage()
		return a.errf("target required")
	}
	target := args[0]
	resolved := target
	if !containsSlash(target) {
		if r, ok := a.Store.ResolveHost(target); ok {
			resolved = r
		} else {
			return a.errf("host not found in services.config.json: %s", target)
		}
	}
	domain, service, err := domainmodel.ParseTarget(resolved)
	if err != nil {
		return err
	}

	cfg, err := a.Store.LoadServices(domain)
	if err != nil {
		return a.errf("could not load services for domain %s: %s", domain, err)
	}
	svc := cfg.FindService(service)
	if svc == nil {
		return a.errf("service not found: %s/%s", domain, service)
	}

	serviceDir := a.Store.ServiceDir(domain, service)

	fmt.Fprintln(a.Out, "WOR Info")
	fmt.Fprintln(a.Out, "--------")
	fmt.Fprintf(a.Out, "Target   : %s\n", target)
	fmt.Fprintf(a.Out, "Domain   : %s\n", domain)
	fmt.Fprintf(a.Out, "Service  : %s\n", service)
	fmt.Fprintf(a.Out, "Type     : %s\n", svc.Type)
	fmt.Fprintf(a.Out, "Enabled  : %t\n", svc.Enabled)
	fmt.Fprintf(a.Out, "Source   : %s\n", serviceDir)

	hosts, _ := a.Store.ListHostsForService(domain, service)
	fmt.Fprintln(a.Out, "Hosts    :")
	for _, h := range hosts {
		fmt.Fprintf(a.Out, "  - %s%s\n", h, hostSSLSuffix(a, h))
	}

	fmt.Fprintln(a.Out)
	fmt.Fprintln(a.Out, "Process:")
	printProcessInfo(a, domain, service, svc.Type, svc.PHPVersion, svc.PHPPoolGroup)

	fmt.Fprintln(a.Out)
	fmt.Fprintln(a.Out, "Reachability:")
	printReachabilityInfo(a, domain, service, svc.Type)

	fmt.Fprintln(a.Out)
	if _, err := exec.Command("git", "-C", serviceDir, "rev-parse", "--git-dir").Output(); err == nil {
		fmt.Fprintln(a.Out, "Git:")
		branch, _ := gitOutput(serviceDir, "branch", "--show-current")
		commit, _ := gitOutput(serviceDir, "rev-parse", "--short", "HEAD")
		statusOut, _ := gitOutput(serviceDir, "status", "--short")
		changed := 0
		if statusOut != "" {
			changed = len(strings.Split(statusOut, "\n"))
		}
		fmt.Fprintf(a.Out, "  Branch : %s\n", branch)
		fmt.Fprintf(a.Out, "  Commit : %s\n", commit)
		fmt.Fprintf(a.Out, "  Status : %d changed files\n", changed)
	}
	return nil
}

// hostSSLSuffix renders a short "  [ssl: ...]" annotation for one host
// line in `wor info`'s Hosts list, reusing the same ssl.LoadState
// lookup buildWriteParams already does when rendering vhost configs --
// no new discovery, just surfacing state that's already on record.
func hostSSLSuffix(a *App, host string) string {
	st, ok, _ := ssl.LoadState(a.Cfg.SSL, host)
	if !ok || !st.Enabled {
		return "  [ssl: none]"
	}
	return fmt.Sprintf("  [ssl: %s]", st.Provider)
}

// printProcessInfo shows the live status of whatever actually runs
// domain/service, dispatching on domainmodel.ProcessProviderFor the
// same way `wor service status` (cmdServiceStatus) does -- pm2/systemd
// process dumps, or php-fpm pool detail for dedicated pools, or a
// plain "no process" note for static/legacy-fpm services.
func printProcessInfo(a *App, domain, service, svcType, phpVersion, phpPoolGroup string) {
	switch domainmodel.ProcessProviderFor(svcType) {
	case "pm2":
		name := pm2.Name(domain, service)
		fmt.Fprintf(a.Out, "  Provider  : pm2 (%s)\n", name)
		if out, err := pm2.RunCapture("describe", name); err == nil {
			fmt.Fprintln(a.Out, truncateLines(out, maxDumpLines))
		} else {
			fmt.Fprintln(a.Out, "  not started")
		}

	case "systemd":
		unit := systemd.Name(domain, service)
		fmt.Fprintf(a.Out, "  Provider  : systemd (%s)\n", unit)
		out, _ := exec.Command("systemctl", "status", unit, "--no-pager", "-l").CombinedOutput()
		if len(strings.TrimSpace(string(out))) == 0 {
			fmt.Fprintln(a.Out, "  not started")
		} else {
			fmt.Fprintln(a.Out, truncateLines(string(out), maxDumpLines))
		}

	default:
		if !domainmodel.TemplateRequiresPHP(svcType) {
			fmt.Fprintln(a.Out, "  static -- served directly by the web server, no process")
			return
		}
		if phpVersion == "" {
			fmt.Fprintln(a.Out, "  Provider  : host-wide PHP_FPM_ENDPOINT (legacy, assumed running)")
			return
		}
		v, ok := phpfpm.ResolveVersion(phpVersion)
		if !ok {
			fmt.Fprintf(a.Out, "  PHP %s no longer detected on this host\n", phpVersion)
			return
		}
		state := "not running"
		if phpfpm.PoolAlive(v, domain, service) {
			state = "running"
		}
		fmt.Fprintf(a.Out, "  Provider  : php-fpm dedicated pool (PHP %s) -- %s\n", phpVersion, state)
		fmt.Fprintf(a.Out, "  Socket    : %s\n", phpfpm.SocketPath(v, domain, service))
		fmt.Fprintf(a.Out, "  Pool file : %s\n", phpfpm.PoolFilePath(v, domain, service))
		if phpPoolGroup != "" {
			fmt.Fprintf(a.Out, "  Pool group: %s\n", phpPoolGroup)
		}
	}
}

// printReachabilityInfo answers whether the web server user can
// actually traverse down to this one service's files -- the scoped,
// single-target version of `wor doctor`'s Security section (see
// checkServiceReachability), surfaced here so a 502/403 caused by
// WOR_HOME sitting under a restrictive path (e.g. a home directory
// instead of /opt/wor) can be spotted from `wor info` directly instead
// of requiring a full `wor doctor` run.
func printReachabilityInfo(a *App, domain, service, svcType string) {
	if osutil.IsWindows() {
		fmt.Fprintln(a.Out, "  skipped (Windows uses a different access-control model)")
		return
	}
	provider, err := a.Provider()
	if err != nil {
		fmt.Fprintf(a.Out, "  skipped: %s\n", err)
		return
	}
	if provider.Name != "nginx" && provider.Name != "apache" {
		fmt.Fprintf(a.Out, "  skipped (%s)\n", provider.Name)
		return
	}
	if !osutil.IsDebianFamily() {
		fmt.Fprintln(a.Out, "  not checked (only auto-detected on Debian/Ubuntu)")
		return
	}
	webUser := webServerRunUser(provider.Name)
	if !webUserExists(webUser) {
		fmt.Fprintf(a.Out, "  could not resolve web server user %q -- not checked\n", webUser)
		return
	}
	blocked := checkServiceReachability(a, webUser, domain, service, svcType)
	if len(blocked) == 0 {
		fmt.Fprintf(a.Out, "  reachable by web server user (%s)\n", webUser)
		return
	}
	fmt.Fprintf(a.Out, "  BLOCKED for %s -- %d path(s) block traversal:\n", webUser, len(blocked))
	for _, p := range blocked {
		fmt.Fprintf(a.Out, "    %s\n", p)
	}
	fmt.Fprintf(a.Out, "  Fix: %s\n", worHomeReachabilityFixCommand(webUser, blocked))
}

// truncateLines caps a raw sub-process dump (pm2 describe, systemctl
// status) at max lines, trimming trailing whitespace first so a
// command's own trailing newline doesn't count as an extra blank line.
func truncateLines(out string, max int) string {
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) > max {
		lines = lines[:max]
	}
	return strings.Join(lines, "\n")
}
