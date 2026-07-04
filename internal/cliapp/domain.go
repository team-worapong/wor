package cliapp

import (
	"fmt"
	"os"
	"path/filepath"

	"wor/internal/domainmodel"
	"wor/internal/pm2"
)

func (a *App) cmdDomain(args []string) error {
	if len(args) < 2 {
		a.usage()
		return a.errf("domain action and domain-id are required")
	}
	action, domain := args[0], args[1]
	if err := domainmodel.RequireSlug(domain); err != nil {
		return err
	}
	switch action {
	case "add":
		if err := a.ensureRootDirs(); err != nil {
			return err
		}
		if err := a.Store.MakeDomainFiles(domain); err != nil {
			return err
		}
		for _, d := range []string{
			filepath.Join(a.Cfg.Backups, domain, "source"),
			filepath.Join(a.Cfg.Backups, domain, "database"),
			filepath.Join(a.Cfg.Logs, domain),
		} {
			os.MkdirAll(d, 0o755)
		}
		if err := pm2.WriteEcosystem(a.Store, domain); err != nil {
			return err
		}
		a.ok("Domain ready: %s", domain)
		return nil
	case "remove":
		dir := a.Store.DomainDir(domain)
		if _, err := os.Stat(dir); err != nil {
			return a.errf("domain not found: %s", domain)
		}

		// A domain is just the folder holding its services' config and
		// source -- removing it says nothing about the services
		// themselves (pm2/systemd processes, host configs, hosts-file
		// entries), which is `wor service remove`'s job. So rather than
		// try to cascade all of that here, block outright -- even for a
		// service that isn't currently running -- and point at the
		// per-service command instead.
		svcCfg, err := a.Store.LoadServices(domain)
		if err != nil {
			return err
		}
		if len(svcCfg.Services) > 0 {
			fmt.Fprintln(a.Err, "ERROR: domain still has registered service(s):")
			for _, svc := range svcCfg.Services {
				fmt.Fprintf(a.Err, "  - %s (%s)\n", svc.Name, svc.Type)
			}
			fmt.Fprintln(a.Err, "\nRemove them first:")
			for _, svc := range svcCfg.Services {
				fmt.Fprintf(a.Err, "  wor service remove %s/%s\n", domain, svc.Name)
			}
			return a.errf("domain removal blocked by registered service(s)")
		}

		logsDir := filepath.Join(a.Cfg.Logs, domain)
		backupsDir := filepath.Join(a.Cfg.Backups, domain)

		// Backups and logs are only asked about and RECORDED here --
		// nothing is deleted yet. Web Data (below) is the final gate for
		// the whole batch: answering "n" there cancels everything,
		// discarding these choices; answering "y" commits all three at
		// once. Order matches what was asked: backups, then logs, then
		// web data.
		removeBackups := false
		if dirExists(backupsDir) {
			removeBackups = a.confirmYN(fmt.Sprintf("Remove backups (%s)?", backupsDir))
			if removeBackups {
				a.info("Backups will be removed: %s", backupsDir)
			} else {
				a.info("Backups will be kept: %s", backupsDir)
			}
		}

		removeLogs := false
		if dirExists(logsDir) {
			removeLogs = a.confirmYN(fmt.Sprintf("Remove logs (%s)?", logsDir))
			if removeLogs {
				a.info("Logs will be removed: %s", logsDir)
			} else {
				a.info("Logs will be kept: %s", logsDir)
			}
		}

		if !a.confirmYN(fmt.Sprintf("Remove web data (%s)?", dir)) {
			a.info("Cancelled: nothing was removed (backups/logs choices above were not applied).")
			return nil
		}

		if removeBackups {
			if err := os.RemoveAll(backupsDir); err != nil {
				a.warn("could not remove backups: %s", err)
			} else {
				a.ok("Backups removed: %s", backupsDir)
			}
		}
		if removeLogs {
			if err := os.RemoveAll(logsDir); err != nil {
				a.warn("could not remove logs: %s", err)
			} else {
				a.ok("Logs removed: %s", logsDir)
			}
		}
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
		a.ok("Web data removed: %s", dir)

		a.ok("Domain removal complete: %s", domain)
		return nil
	default:
		a.usage()
		return a.errf("unknown domain action: %s", action)
	}
}
