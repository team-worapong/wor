// Package cliapp wires together every wor subcommand, porting bin/wor's
// dispatch table and commands/*.sh. Each subcommand is implemented as a
// method on *App so it has direct access to resolved configuration, the
// domain registry, and the active host provider.
package cliapp

import (
	"bufio"
	"fmt"
	"io"
	"os"

	"wor/internal/config"
	"wor/internal/domainmodel"
	"wor/internal/hostprovider"
	"wor/internal/osutil"
	"wor/internal/version"
	"wor/internal/worlock"
)

// Version and ProductName are re-exported from internal/version, which
// is the single source of truth for both (see that package's doc
// comment for why it's a separate leaf package rather than living
// here directly).
const Version = version.Number
const ProductName = version.ProductName

// App holds everything a subcommand needs.
type App struct {
	Cfg   *config.Config
	Store *domainmodel.Store

	Out io.Writer
	Err io.Writer
	In  *bufio.Reader
}

func New() (*App, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("loading configuration: %w", err)
	}
	app := &App{
		Cfg:   cfg,
		Store: domainmodel.NewStore(cfg.Domains),
		Out:   os.Stdout,
		Err:   os.Stderr,
		In:    bufio.NewReader(os.Stdin),
	}
	// Wire osutil's confirm-once elevation gate to an interactive y/n
	// prompt, so the first time (per process) a command actually needs
	// to escalate via sudo, the user sees why before it happens.
	osutil.SetElevationPrompt(func(reason string) bool {
		return app.confirmYesDefaultYes(fmt.Sprintf("wor needs to %s", reason))
	})
	return app, nil
}

// Provider builds the active host provider (nginx or apache) per the
// resolved configuration.
func (a *App) Provider() (*hostprovider.Provider, error) {
	return hostprovider.New(a.Cfg.HostProviderName(), a.Cfg)
}

func (a *App) ok(format string, args ...interface{}) {
	fmt.Fprintf(a.Out, "[OK] "+format+"\n", args...)
}
func (a *App) info(format string, args ...interface{}) {
	fmt.Fprintf(a.Out, "[INFO] "+format+"\n", args...)
}
func (a *App) warn(format string, args ...interface{}) {
	fmt.Fprintf(a.Err, "[WARN] "+format+"\n", args...)
}
func (a *App) errf(format string, args ...interface{}) error {
	return fmt.Errorf(format, args...)
}

// Run dispatches argv (excluding argv[0]) to the matching subcommand,
// mirroring bin/wor dispatch_command(). It returns a process exit code.
func (a *App) Run(args []string) int {
	if osutil.IsSudoElevated() {
		fmt.Fprintln(a.Err, "ERROR: do not run wor via sudo (e.g. `sudo wor host add ...`).")
		fmt.Fprintln(a.Err, "Run it as your normal user instead -- wor will ask for elevated (sudo)")
		fmt.Fprintln(a.Err, "permission itself, only for the specific actions that actually need it.")
		return 1
	}
	if len(args) == 0 {
		a.usage()
		return 1
	}
	cmd, rest := args[0], args[1:]

	if requiresInitializedWorkspace(cmd) && !a.workspaceInitialized() {
		fmt.Fprintln(a.Err, "ERROR: workspace not initialized.")
		fmt.Fprintln(a.Err, "Run `wor setup` first, then re-run this command.")
		return 1
	}

	if commandNeedsLock(cmd, rest) {
		lock, err := worlock.Acquire(a.Cfg.WorHome)
		if err != nil {
			fmt.Fprintf(a.Err, "ERROR: %s\n", err)
			fmt.Fprintln(a.Err, "Another wor command appears to be running against the same WOR_HOME. Wait for it to finish and try again.")
			return 1
		}
		defer lock.Release()
	}

	var err error
	switch cmd {
	case "version", "--version", "-v":
		a.cmdVersion()
	case "setup":
		err = a.cmdSetup(rest)
	case "doctor":
		var failed bool
		failed, err = a.cmdDoctor(rest)
		if err == nil && failed {
			return 1
		}
	case "diagnose":
		var failed bool
		failed, err = a.cmdDiagnose(rest)
		if err == nil && failed {
			return 1
		}
	case "health":
		var failed bool
		failed, err = a.cmdHealth(rest)
		if err == nil && failed {
			return 1
		}
	case "env":
		err = a.cmdEnv(rest)
	case "clean":
		err = a.cmdClean(rest)
	case "reset":
		err = a.cmdReset(rest)
	case "create":
		err = a.cmdCreate(rest)
	case "domain":
		err = a.cmdDomain(rest)
	case "service":
		err = a.cmdService(rest)
	case "run":
		err = a.cmdRun(rest)
	case "host":
		err = a.cmdHost(rest)
	case "database":
		err = a.cmdDatabase(rest)
	case "source":
		err = a.cmdSource(rest)
	case "deploy":
		err = a.cmdDeploy(rest)
	case "rollback":
		err = a.cmdRollback(rest)
	case "ssl":
		err = a.cmdSSL(rest)
	case "info":
		err = a.cmdInfo(rest)
	case "help", "-h", "--help", "":
		a.usage()
	default:
		a.usage()
		return 1
	}
	if err != nil {
		fmt.Fprintf(a.Err, "ERROR: %s\n", err)
		return 1
	}
	return 0
}

// commandNeedsLock decides whether Run should hold the $WOR_HOME
// advisory lock (see internal/worlock) for the duration of cmd/rest.
// Default is "yes, lock it" -- almost every subcommand reads and/or
// writes services.config.json/databases.config.json/the PM2 ecosystem
// file/vhost configs/etc., and misclassifying one of those as
// lock-free would silently reopen the exact race this lock exists to
// close. Only three kinds of commands are excluded:
//   - version/help: never touch WOR_HOME at all.
//   - `service logs` / `host logs`: these can tail/follow indefinitely
//     (pm2 logs, journalctl -f, ...), so holding an exclusive lock for
//     their whole runtime would block every other wor command on the
//     host for as long as someone is watching logs.
//   - diagnose/health: strictly read-only (they never write anything --
//     see docs/diagnose.md), and every config file they read is written
//     atomically (osutil.WriteFileAtomic) so a concurrent writer can't
//     hand them a torn read. Excluded so the incident-response commands
//     are never the thing left waiting on a lock during an outage --
//     and so a wedged/long-running other command can't block them.
func commandNeedsLock(cmd string, rest []string) bool {
	switch cmd {
	case "version", "--version", "-v", "help", "-h", "--help", "", "diagnose", "health":
		return false
	case "service", "host":
		if len(rest) > 0 && rest[0] == "logs" {
			return false
		}
	}
	return true
}

// requiresInitializedWorkspace decides whether cmd needs a fully set up
// $WOR_HOME (see workspaceInitialized in doctor.go) before it's allowed
// to run at all. Almost every subcommand reads or writes something
// under WOR_HOME (services.config.json, vhost configs, the domains/
// backups/logs trees, ...), and letting one of those run against a
// WOR_HOME that was never created (or only partially created, e.g. the
// user cancelled `wor setup` at the confirm prompt) would surface as a
// confusing low-level "no such file or directory" instead of a clear
// pointer to `wor setup`. Only four kinds of commands are excluded:
//   - version/help: never touch WOR_HOME at all.
//   - setup: this is how a workspace *becomes* initialized -- it must
//     always be reachable, initialized or not.
//   - doctor: a read-only health report that already handles an
//     uninitialized workspace gracefully (reports a ✗ line instead of
//     crashing), and is exactly the command someone would run to find
//     out *why* something else is failing.
func requiresInitializedWorkspace(cmd string) bool {
	switch cmd {
	case "version", "--version", "-v", "help", "-h", "--help", "", "setup", "doctor":
		return false
	}
	return true
}
