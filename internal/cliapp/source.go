package cliapp

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"wor/internal/domainmodel"
	"wor/internal/gitignore"
)

// sourceBackup zips a domain or service source tree into
// $WOR_HOME/backups/<domain>/source/<domain>_<name>_<timestamp>.zip,
// honoring the domain's backup.config.json exclude list. Uses Go's
// archive/zip instead of shelling out to `zip`, so it works unmodified
// on Windows. The domain-first path matches the directories `wor domain
// add` pre-creates (internal/cliapp/domain.go) and the sibling database
// backup convention (internal/dbbackup.ApplyRetention uses
// <domain>/database/...).
//
// gitignoreOverride is "enable", "disable", or "" (use the domain's
// backup.config.json source.useGitIgnore default, wired up here for the
// first time -- it was a previously-unused config field). When the
// effective setting is enabled and a .gitignore file exists at the root
// of the tree being backed up, its patterns are combined with
// backupCfg.Source.Exclude (a file is skipped if either one excludes
// it) -- see internal/gitignore for the (deliberately root-only, not
// full git semantics) matching rules.
func (a *App) sourceBackup(target, gitignoreOverride string) (string, error) {
	domain, service, err := domainmodel.ParseTarget(target)
	if err != nil {
		return "", err
	}
	var src, name string
	if service == "" {
		src, name = a.Store.DomainDir(domain), "domain"
	} else {
		src, name = a.Store.ServiceDir(domain, service), service
	}
	if _, err := os.Stat(src); err != nil {
		return "", a.errf("source path not found: %s", src)
	}

	backupCfg, err := a.Store.LoadBackupConfig(domain)
	if err != nil {
		return "", err
	}

	useGitIgnore := backupCfg.Source.UseGitIgnore
	switch gitignoreOverride {
	case "enable":
		useGitIgnore = true
	case "disable":
		useGitIgnore = false
	}
	var gi *gitignore.Matcher
	if useGitIgnore {
		gi, err = gitignore.Load(filepath.Join(src, ".gitignore"))
		if err != nil {
			return "", fmt.Errorf("reading .gitignore: %w", err)
		}
	}

	ts := time.Now().Format("20060102_150405")
	outDir := filepath.Join(a.Cfg.Backups, domain, "source")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", err
	}
	outFile := filepath.Join(outDir, fmt.Sprintf("%s_%s_%s.zip", domain, name, ts))

	if err := zipDir(src, outFile, backupCfg.Source.Exclude, gi); err != nil {
		return "", err
	}
	if backupCfg.Source.VerifyAfterBackup {
		if err := verifyZip(outFile); err != nil {
			return "", fmt.Errorf("backup verification failed: %w", err)
		}
	}
	return outFile, nil
}

func excluded(relPath string, patterns []string) bool {
	base := filepath.Base(relPath)
	for _, pat := range patterns {
		if pat == "" {
			continue
		}
		if base == pat || strings.Contains(relPath, "/"+pat+"/") || strings.HasPrefix(relPath, pat+"/") {
			return true
		}
		if ok, _ := filepath.Match(pat, base); ok {
			return true
		}
	}
	return false
}

func zipDir(src, dest string, excludePatterns []string, gi *gitignore.Matcher) error {
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	zw := zip.NewWriter(out)
	defer zw.Close()

	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil || rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if excluded(rel, excludePatterns) || gi.Match(rel, info.IsDir()) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			return nil
		}
		w, err := zw.Create(rel)
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(w, f)
		return err
	})
}

func verifyZip(path string) error {
	r, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			return err
		}
		_, err = io.Copy(io.Discard, rc)
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func (a *App) cmdSource(args []string) error {
	if len(args) < 2 {
		a.usage()
		return a.errf("source action and target are required")
	}
	action, target := args[0], args[1]
	rest := args[2:]
	fl := parseFlags(rest)

	switch action {
	case "backup":
		gitignoreFlag := fl.Get("gitignore", "")
		if gitignoreFlag != "" && gitignoreFlag != "enable" && gitignoreFlag != "disable" {
			return a.errf("invalid --gitignore value: %s (expected enable or disable)", gitignoreFlag)
		}
		out, err := a.sourceBackup(target, gitignoreFlag)
		if err != nil {
			return err
		}
		a.ok("%s", out)
		return nil

	case "pull":
		domain, service, err := domainmodel.ParseTarget(target)
		if err != nil {
			return err
		}
		dir := a.Store.DomainDir(domain)
		if service != "" {
			dir = a.Store.ServiceDir(domain, service)
		}
		if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
			return a.errf("not a git repository: %s", dir)
		}
		cmd := exec.Command("git", "pull")
		cmd.Dir = dir
		cmd.Stdout, cmd.Stderr = a.Out, a.Err
		return cmd.Run()

	case "clone":
		git := fl.Get("git", "")
		if git == "" {
			return a.errf("--git is required")
		}
		replace := fl.Has("replace")
		domain, service, err := domainmodel.ParseTarget(target)
		if err != nil {
			return err
		}
		dest := a.Store.DomainDir(domain)
		if service != "" {
			dest = a.Store.ServiceDir(domain, service)
		}
		tmp := filepath.Join(a.Cfg.Tmp, fmt.Sprintf("wor-clone-%d", time.Now().UnixNano()))
		cloneCmd := exec.Command("git", "clone", git, tmp)
		cloneCmd.Stdout, cloneCmd.Stderr = a.Out, a.Err
		if err := cloneCmd.Run(); err != nil {
			return err
		}
		if _, err := os.Stat(dest); err == nil {
			if !replace {
				os.RemoveAll(tmp)
				return a.errf("target exists. Use --replace to backup and replace: %s", dest)
			}
			if _, err := a.sourceBackup(target, ""); err != nil {
				return err
			}
			os.RemoveAll(dest)
		}
		os.MkdirAll(filepath.Dir(dest), 0o755)
		if err := os.Rename(tmp, dest); err != nil {
			return err
		}
		a.ok("Cloned: %s", target)
		return nil

	default:
		a.usage()
		return a.errf("unknown source action: %s", action)
	}
}
