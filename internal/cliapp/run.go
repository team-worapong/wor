package cliapp

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"wor/internal/domainmodel"
	"wor/internal/osutil"
	"wor/internal/phpfpm"
	"wor/internal/pm2"
	"wor/internal/systemd"
)

// cmdRun implements `wor run`: brings every enabled service (and the
// runtimes it depends on) up, starting anything that isn't already
// running. Deliberately named "run" rather than "start"/"up" -- unlike
// `wor service start/stop/restart`, this is a one-directional "ensure
// desired state" command (more like `terraform apply` or `docker-compose
// up`) with no symmetric "wor down"/"wor stop-all" counterpart planned.
//
// Order of operations (agreed before implementation):
//  1. Global checks, once: the active web server provider, then the pm2
//     daemon (only if some enabled service actually needs pm2) --
//     including a one-time offer to register `pm2 startup` if it was
//     never set up, so pm2-managed services survive a reboot.
//  2. Per-service loop: for each enabled service, make sure its runtime
//     is up (per-service php-fpm pool, or nothing extra for pm2/systemd
//     since starting the service *is* starting its runtime there), then
//     start the service itself if it isn't already running.
//
// A failed service is skipped, not fatal -- the loop continues through
// the rest, and the final summary line reports how many succeeded vs
// failed. Progress is printed per service (ok/fail) as it resolves.
func (a *App) cmdRun(args []string) error {
	refs, err := a.Store.ListAllServices()
	if err != nil {
		return err
	}

	var enabled []domainmodel.ServiceRef
	needPM2 := false
	for _, ref := range refs {
		if !ref.Service.Enabled {
			continue
		}
		enabled = append(enabled, ref)
		if domainmodel.ProcessProviderFor(ref.Service.Type) == "pm2" {
			needPM2 = true
		}
	}
	if len(enabled) == 0 {
		fmt.Fprintln(a.Out, "No enabled services found.")
		return nil
	}

	fmt.Fprintln(a.Out, "wor run")
	fmt.Fprintln(a.Out, "-------")

	// --- global checks, once, before the per-service loop ---
	if provider, err := a.Provider(); err != nil {
		a.warn("host provider unavailable: %s", err)
	} else if _, ok := provider.Binary(); ok {
		if provider.IsRunning() {
			a.ok("%s already running", provider.Name)
		} else if err := provider.Start(); err != nil {
			a.warn("could not start %s: %s", provider.Name, err)
		} else {
			a.ok("%s started", provider.Name)
		}
	}

	if needPM2 {
		if err := a.ensurePM2Runtime(); err != nil {
			a.warn("pm2 runtime: %s", err)
		}
	}

	fmt.Fprintln(a.Out)

	// --- per-service loop ---
	useColor := a.colorEnabled()
	started, failed := 0, 0
	for _, ref := range enabled {
		target := ref.Domain + "/" + ref.Service.Name
		if err := a.runService(ref.Domain, ref.Service); err != nil {
			fmt.Fprintf(a.Out, "  %s %s: %s\n", tag(useColor, ansiRed, "●", "[fail]"), target, err)
			failed++
			continue
		}
		fmt.Fprintf(a.Out, "  %s %s\n", tag(useColor, ansiGreen, "●", "[ok]"), target)
		started++
	}

	fmt.Fprintln(a.Out)
	a.info("%d/%d services running (%d failed)", started, len(enabled), failed)
	return nil
}

// runService brings one enabled service up: ensures its runtime layer
// is ready, then starts the service itself if it isn't already running.
// Returns an error (never fatal to the caller's loop) describing what
// went wrong for this one service.
func (a *App) runService(domain string, svc domainmodel.Service) error {
	t := svc.Type
	provider := domainmodel.ProcessProviderFor(t)

	if provider == "" {
		// static: served directly by the web server, nothing to start.
		if domainmodel.TemplateRequiresPHP(t) && svc.UsesPerServicePHPFPM() {
			return a.ensurePHPPoolRunning(domain, svc.Name, svc.PHPVersion)
		}
		// legacy php (host-wide PHP_FPM_ENDPOINT) is not managed by wor
		// at all -- nothing to check or start.
		return nil
	}

	if err := a.requireTemplateRuntime(t); err != nil {
		return err
	}

	switch provider {
	case "pm2":
		name := pm2.Name(domain, svc.Name)
		if procs, err := pm2.List(); err == nil {
			if info, ok := procs[name]; ok && info.Status == "online" {
				return nil // already running
			}
		}
		if err := pm2.WriteEcosystem(a.Store, domain); err != nil {
			return err
		}
		if err := pm2.Run("start", pm2.EcosystemPath(a.Store.DomainDir(domain)), "--only", name); err != nil {
			return err
		}
		return pm2.Save()

	case "systemd":
		if systemd.IsActive(domain, svc.Name) {
			return nil // already running
		}
		entry := a.Store.GetServiceEntryPoint(domain, svc.Name)
		if err := systemd.WriteUnit(a.systemdUnitFor(domain, svc.Name, t, entry)); err != nil {
			return err
		}
		if err := systemd.Enable(domain, svc.Name); err != nil {
			a.warn("systemd enable failed for %s/%s: %s", domain, svc.Name, err)
		}
		return systemd.Start(domain, svc.Name)
	}
	return nil
}

// ensurePHPPoolRunning makes sure domain/service's dedicated php-fpm
// pool can actually serve requests: its master php-fpm version is
// running (started if not), and its pool config file is still in
// place. It deliberately does not attempt to recreate a missing pool
// file from scratch (that needs EnsureUser/GrantGroupAccess state this
// function has no business re-deriving) -- it errors clearly instead,
// pointing at re-adding the service.
func (a *App) ensurePHPPoolRunning(domain, service, phpVersion string) error {
	version, ok := phpfpm.ResolveVersion(phpVersion)
	if !ok {
		return fmt.Errorf("PHP %s is no longer detected on this host (run: wor doctor)", phpVersion)
	}
	if !phpfpm.IsRunning(version) {
		if err := phpfpm.Start(version); err != nil {
			return fmt.Errorf("starting php-fpm %s: %w", phpVersion, err)
		}
	}
	poolPath := phpfpm.PoolFilePath(version, domain, service)
	if _, err := os.Stat(poolPath); err != nil {
		return fmt.Errorf("pool config missing for %s/%s (%s); re-add the service to recreate it", domain, service, poolPath)
	}
	return nil
}

// ensurePM2Runtime makes sure PM2 itself is reachable and, if `pm2
// startup` (boot persistence) has never been registered on this host,
// offers to register it now. Closes the gap where nothing in wor ever
// called `pm2 startup`, so every pm2-managed service silently failed to
// come back after a reboot.
func (a *App) ensurePM2Runtime() error {
	if !osutil.Exists("pm2") {
		return fmt.Errorf("pm2 not installed")
	}
	// Any pm2 command auto-spawns pm2's own daemon if it isn't already
	// running, so simply querying it both checks and "starts" it.
	if _, err := pm2.List(); err != nil {
		return fmt.Errorf("pm2 daemon unreachable: %w", err)
	}
	a.ok("pm2 daemon running")

	if osutil.IsWindows() {
		return nil // no boot-persistence mechanism to register there
	}
	if pm2StartupRegistered() {
		return nil
	}
	return a.registerPM2Startup()
}

func currentUsername() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return os.Getenv("USER")
}

// pm2StartupRegistered is a best-effort check for whether `pm2 startup`
// has already been run on this host: it looks for the boot-service file
// pm2 itself generates (a systemd unit on Linux, a launchd plist on
// macOS), named after the current user the same way `pm2 startup`/`pm2
// unstartup` name it. False negatives here just mean `wor run` offers
// to register it again -- harmless, since registration is idempotent.
func pm2StartupRegistered() bool {
	uname := currentUsername()
	switch {
	case osutil.IsLinux():
		_, err := os.Stat(fmt.Sprintf("/etc/systemd/system/pm2-%s.service", uname))
		return err == nil
	case osutil.IsMacOS():
		candidates := []string{
			fmt.Sprintf("/Library/LaunchDaemons/pm2-%s.plist", uname),
			fmt.Sprintf("/Library/LaunchDaemons/pm2.%s.plist", uname),
		}
		if home, err := os.UserHomeDir(); err == nil {
			candidates = append(candidates,
				filepath.Join(home, "Library", "LaunchAgents", fmt.Sprintf("pm2-%s.plist", uname)),
				filepath.Join(home, "Library", "LaunchAgents", fmt.Sprintf("pm2.%s.plist", uname)),
			)
		}
		for _, p := range candidates {
			if _, err := os.Stat(p); err == nil {
				return true
			}
		}
		return false
	default:
		return true
	}
}

// registerPM2Startup asks pm2 itself what command should be run to
// install its boot service (`pm2 startup` only detects the platform and
// prints a ready-to-run sudo command -- it never applies it itself),
// shows the user exactly what will be executed, then runs it through
// the same confirm-once elevation gate every other privileged wor
// operation uses (osutil.SudoCommand) instead of just printing it for
// the user to copy/paste.
func (a *App) registerPM2Startup() error {
	// Deliberately no explicit platform argument: pm2 startup
	// auto-detects it (systemd/launchd/upstart/...) on its own. An
	// earlier version of this passed a guessed platform keyword
	// ("systemd" / "launchd"), but "launchd" isn't one of pm2's actual
	// accepted platform names (its own list is things like "macos",
	// "darwin", "ubuntu", "systemd", ...) -- passing an unrecognized
	// value made `pm2 startup` exit 1 immediately on macOS. Letting pm2
	// detect for itself avoids depending on knowing its exact keyword
	// list for every OS/pm2 version.
	// pm2 startup's own exit code is not a reliable success/failure
	// signal: it appears to exit non-zero even on a completely normal
	// run that successfully printed the suggested sudo command (an
	// "advisory only, nothing applied yet" convention, confirmed by
	// testing on a real host) -- so this deliberately ignores runErr
	// and looks at the actual output first. Only if the sudo line is
	// genuinely missing does it treat this as a real failure, and then
	// includes pm2's own output verbatim so the actual reason is
	// visible instead of just a bare "exit status 1".
	out, runErr := pm2.RunCapture("startup")
	cmdLine := extractSudoCommand(out)
	if cmdLine == "" {
		if runErr != nil {
			return fmt.Errorf("pm2 startup did not print a startup command (%w):\n%s", runErr, out)
		}
		return fmt.Errorf("pm2 startup did not print a startup command:\n%s", out)
	}
	rest := strings.TrimSpace(strings.TrimPrefix(cmdLine, "sudo "))
	if rest == "" {
		return fmt.Errorf("empty pm2 startup command")
	}

	a.info("Registering pm2 as a boot service so it survives reboots. This will run:")
	fmt.Fprintf(a.Out, "  sudo sh -c %q\n", rest)

	// Run the whole line through a real shell (`sh -c`), rather than
	// splitting it into argv and exec'ing the first word directly. pm2's
	// suggested command embeds "$PATH" (e.g. `env PATH=$PATH:/usr/local/bin
	// ...`), which only expands when a shell evaluates it -- exec'ing
	// `env` directly (the earlier version of this) passed the literal,
	// unexpanded string "$PATH:/usr/local/bin" to env, which set PATH to
	// that garbage value verbatim and broke pm2's own internal `mkdir`
	// call with "command not found" (confirmed via a real run on the
	// user's host). `sh -c` expands "$PATH" the same way it would if the
	// user pasted this line into their own terminal.
	cmd, err := osutil.SudoCommand("sh", "-c", rest)
	if err != nil {
		return err
	}
	cmd.Stdout, cmd.Stderr = a.Out, a.Err
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pm2 startup registration failed: %w", err)
	}
	a.ok("pm2 boot startup registered")
	return pm2.Save()
}

// extractSudoCommand finds the line `pm2 startup` prints that begins
// with "sudo " -- the exact command it wants run to install its boot
// service. A deliberately simple line scanner (same tradeoff as this
// project's other best-effort text parsers, e.g. internal/gitignore,
// phpfpm.DetectListenAddrs) rather than depending on pm2's own output
// format staying byte-for-byte stable across versions.
func extractSudoCommand(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "sudo ") {
			return line
		}
	}
	return ""
}
