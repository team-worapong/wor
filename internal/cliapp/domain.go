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
		if !a.requireTyped(fmt.Sprintf("Remove domain folder %s ? Type YES: ", dir), "YES") {
			return a.errf("cancelled")
		}
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
		a.ok("Domain removed: %s", domain)
		return nil
	default:
		a.usage()
		return a.errf("unknown domain action: %s", action)
	}
}
