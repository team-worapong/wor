package cliapp

import (
	"os"
	"path/filepath"
	"strings"

	"wor/internal/dbbackup"
	"wor/internal/domainmodel"
)

func (a *App) cmdDatabase(args []string) error {
	if len(args) < 2 {
		a.usage()
		return a.errf("database action and target are required")
	}
	action, target := args[0], args[1]
	rest := args[2:]
	fl := parseFlags(rest)

	domain := target
	profile := ""
	database := ""
	if idx := strings.Index(target, "/"); idx >= 0 {
		domain = target[:idx]
		remainder := target[idx+1:]
		if idx2 := strings.Index(remainder, "/"); idx2 >= 0 {
			profile, database = remainder[:idx2], remainder[idx2+1:]
		} else {
			profile = remainder
		}
	}
	if err := domainmodel.RequireSlug(domain); err != nil {
		return err
	}
	if err := domainmodel.RequireSlug(profile); err != nil {
		return err
	}

	envFile := dbbackup.ProfilePath(a.Cfg.Configs, profile)

	switch action {
	case "add":
		// A database profile can only be attached to a domain that
		// already exists -- unlike the old behavior, this must NOT
		// auto-create the domain via MakeDomainFiles.
		if _, err := os.Stat(a.Store.DomainDir(domain)); err != nil {
			return a.errf("domain not found: %s", domain)
		}
		if err := os.MkdirAll(filepath.Join(a.Cfg.Configs, "database"), 0o755); err != nil {
			return err
		}
		label := fl.Get("label", profile)
		dbCfg, err := a.Store.LoadDatabases(domain)
		if err != nil {
			return err
		}
		for _, d := range dbCfg.Databases {
			if d.Profile == profile {
				a.warn("Database profile already exists: %s/%s", domain, profile)
				return nil
			}
		}
		dbCfg.Databases = append(dbCfg.Databases, domainmodel.Database{
			Profile: profile, Label: label, Enabled: true, Backup: true,
		})
		if err := a.Store.SaveDatabases(dbCfg); err != nil {
			return err
		}
		if _, err := os.Stat(envFile); os.IsNotExist(err) {
			if err := os.WriteFile(envFile, []byte(dbbackup.DefaultProfileEnv), 0o600); err != nil {
				return err
			}
		}
		a.ok("Database profile ready: %s/%s", domain, profile)
		return nil

	case "remove":
		if _, err := os.Stat(a.Store.DomainDir(domain)); err != nil {
			return a.errf("domain not found: %s", domain)
		}
		dbCfg, err := a.Store.LoadDatabases(domain)
		if err != nil {
			return err
		}
		found := false
		out := dbCfg.Databases[:0]
		for _, d := range dbCfg.Databases {
			if d.Profile == profile {
				found = true
				continue
			}
			out = append(out, d)
		}
		if !found {
			return a.errf("database profile not found: %s/%s", domain, profile)
		}
		dbCfg.Databases = out
		if err := a.Store.SaveDatabases(dbCfg); err != nil {
			return err
		}
		if _, err := os.Stat(envFile); os.IsNotExist(err) {
			a.warn("env file not found, skipped: %s", envFile)
		} else if err := os.Remove(envFile); err != nil {
			return err
		}
		a.ok("Database profile removed: %s/%s", domain, profile)
		return nil

	case "backup":
		if _, err := os.Stat(envFile); err != nil {
			return a.errf("database env not found: %s", envFile)
		}
		p, err := dbbackup.LoadProfile(envFile)
		if err != nil {
			return err
		}
		backupCfg, err := a.Store.LoadBackupConfig(domain)
		if err != nil {
			return err
		}
		results, err := dbbackup.Backup(p, dbbackup.Options{
			Domain: domain, Profile: profile, BackupsDir: a.Cfg.Backups, Database: database,
		})
		for _, r := range results {
			a.info("Backing up %s database: %s", p.Engine, r.Database)
			a.ok("%s", r.Path)
		}
		if err != nil {
			return err
		}
		return dbbackup.ApplyRetention(a.Cfg.Backups, domain, profile, backupCfg.Database.RetentionDays)

	default:
		a.usage()
		return a.errf("unknown database action: %s", action)
	}
}
