package cliapp

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"wor/internal/domainmodel"
	"wor/internal/pm2"
	"wor/internal/systemd"
)

func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func hasBuildScript(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return false
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return false
	}
	_, ok := pkg.Scripts["build"]
	return ok
}

func (a *App) cmdDeploy(args []string) error {
	if len(args) == 0 {
		a.usage()
		return a.errf("deploy target required")
	}
	target := args[0]
	fl := parseFlags(args[1:])
	pullOnly := fl.Has("pull-only")
	noPull := fl.Has("no-pull")
	noRestart := fl.Has("no-restart")
	force := fl.Has("force")
	stash := fl.Has("stash")

	resolved := target
	if !strings.Contains(target, "/") {
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
	serviceDir := a.Store.ServiceDir(domain, service)
	if _, err := os.Stat(filepath.Join(serviceDir, ".git")); err != nil {
		return a.errf("not a git repository: %s", serviceDir)
	}
	name := pm2.Name(domain, service)
	svcType := a.Store.GetServiceType(domain, service)
	if err := a.requireTemplateRuntime(svcType); err != nil {
		return err
	}

	before, _ := gitOutput(serviceDir, "rev-parse", "HEAD")
	if !noPull {
		if err := a.gitPull(serviceDir, resolved, stash); err != nil {
			return err
		}
	}
	after, _ := gitOutput(serviceDir, "rev-parse", "HEAD")
	changed := before != after

	if pullOnly {
		a.ok("Pull only completed")
		return nil
	}

	// --force skips the "did the dependency manifest change" diff check
	// for both npm and pip below. That's what makes `wor rollback` and
	// `wor source clone` work when they delegate here with --no-pull
	// --force: in both cases before == after (nothing was pulled *by
	// deploy*), so the changed-manifest heuristic would never fire, yet
	// dependencies are exactly what may be missing or stale (a fresh
	// clone has no node_modules at all; a rollback may have reset
	// package-lock.json to an older revision than what's installed).
	pkgPath := filepath.Join(serviceDir, "package.json")
	if _, err := os.Stat(pkgPath); err == nil && (changed || force) {
		installDeps := force
		if !installDeps {
			diffOut, _ := gitOutput(serviceDir, "diff", "--name-only", before, after)
			for _, f := range strings.Split(diffOut, "\n") {
				if f == "package.json" || f == "package-lock.json" || f == "npm-shrinkwrap.json" {
					installDeps = true
					break
				}
			}
		}
		if installDeps {
			// npm ci requires a lockfile; fall back to npm install for
			// repos that don't commit one.
			npmArgs := []string{"ci"}
			if !fileExists(filepath.Join(serviceDir, "package-lock.json")) &&
				!fileExists(filepath.Join(serviceDir, "npm-shrinkwrap.json")) {
				npmArgs = []string{"install"}
			}
			cmd := exec.Command("npm", npmArgs...)
			cmd.Dir = serviceDir
			cmd.Stdout, cmd.Stderr = a.Out, a.Err
			if err := cmd.Run(); err != nil {
				return err
			}
		}
	}
	if _, err := os.Stat(pkgPath); err == nil && (changed || force) && hasBuildScript(serviceDir) {
		cmd := exec.Command("npm", "run", "build")
		cmd.Dir = serviceDir
		cmd.Stdout, cmd.Stderr = a.Out, a.Err
		if err := cmd.Run(); err != nil {
			return err
		}
	}

	// python mirrors node's "did the dependency manifest change" check --
	// requirements.txt in the diff between before/after triggers a pip
	// install, same gating (changed, or forced) as node's npm ci above.
	reqPath := filepath.Join(serviceDir, "requirements.txt")
	if _, err := os.Stat(reqPath); err == nil && (changed || force) {
		installReqs := force
		if !installReqs {
			diffOut, _ := gitOutput(serviceDir, "diff", "--name-only", before, after)
			for _, f := range strings.Split(diffOut, "\n") {
				if f == "requirements.txt" {
					installReqs = true
					break
				}
			}
		}
		if installReqs {
			cmd := exec.Command(pythonBinary(), "-m", "pip", "install", "-r", "requirements.txt")
			cmd.Dir = serviceDir
			cmd.Stdout, cmd.Stderr = a.Out, a.Err
			if err := cmd.Run(); err != nil {
				return err
			}
		}
	}

	// go has no equivalent of node's "did package.json change" heuristic
	// -- an updated source file with no dependency-manifest change would
	// otherwise never get rebuilt. Per the go/python/systemd redesign,
	// go rebuilds unconditionally on every deploy where a new commit was
	// pulled (or --force), regardless of which files changed.
	if domainmodel.TemplateRequiresGo(svcType) && (changed || force) {
		entry := a.Store.GetServiceEntryPoint(domain, service)
		a.info("Building Go binary...")
		if err := a.buildGoService(serviceDir, entry); err != nil {
			return err
		}
	}

	provider := domainmodel.ProcessProviderFor(svcType)
	switch {
	case !noRestart && provider == "pm2":
		if err := pm2.WriteEcosystem(a.Store, domain); err != nil {
			return err
		}
		if _, err := pm2.RunCapture("describe", name); err == nil {
			if err := pm2.Run("restart", name); err != nil {
				return err
			}
		} else {
			if err := pm2.Run("start", pm2.EcosystemPath(a.Store.DomainDir(domain)), "--only", name); err != nil {
				return err
			}
		}
		pm2.Save()
		if _, err := pm2.RunCapture("describe", name); err != nil {
			return a.errf("PM2 health check failed: %s", name)
		}
	case !noRestart && provider == "systemd":
		entry := a.Store.GetServiceEntryPoint(domain, service)
		if err := systemd.WriteUnit(a.systemdUnitFor(domain, service, svcType, entry)); err != nil {
			return err
		}
		if err := systemd.Restart(domain, service); err != nil {
			return err
		}
		if !systemd.IsActive(domain, service) {
			return a.errf("systemd health check failed: %s", systemd.Name(domain, service))
		}
	default:
		a.info("%s service deployed. Reloading host provider.", svcType)
		hostProvider, err := a.Provider()
		if err != nil {
			return err
		}
		if err := hostProvider.Reload(); err != nil {
			return err
		}
	}
	a.ok("Deploy completed: %s", resolved)
	return nil
}
