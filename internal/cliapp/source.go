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
	"wor/internal/osutil"
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

// replaceDir swaps dest for the freshly cloned tmp dir without ever
// leaving dest empty: the existing tree is moved aside first (not
// deleted), the new one is moved into place, and only then is the old
// one discarded. If moving the new tree in fails for any reason
// (including tmp/dest being on different filesystems -- see moveDir),
// the original is moved back exactly where it was and the error is
// returned, so a failed `wor source clone` never destroys the
// pre-existing source. Callers are expected to have already taken a
// `wor source backup` of dest before calling this, since the discarded
// old copy is not otherwise recoverable.
func replaceDir(tmp, dest string) error {
	stash := fmt.Sprintf("%s.wor-old-%d", dest, time.Now().UnixNano())
	if err := os.Rename(dest, stash); err != nil {
		os.RemoveAll(tmp)
		return fmt.Errorf("could not move existing %s aside: %w", dest, err)
	}
	if err := moveDir(tmp, dest); err != nil {
		os.RemoveAll(dest)
		if rerr := os.Rename(stash, dest); rerr != nil {
			return fmt.Errorf("%w (additionally failed to restore original %s: %s)", err, dest, rerr)
		}
		return err
	}
	os.RemoveAll(stash)
	return nil
}

// moveDir moves src to dest, the cheap way (os.Rename) when possible.
// os.Rename fails with "invalid cross-device link" (or a platform
// equivalent) when src and dest are on different filesystems --
// plausible here since src is under the configured tmp dir (which may
// be system tmp, e.g. a tmpfs mount) while dest is under WOR_HOME. That
// error isn't simple to detect portably across Linux/macOS/Windows, so
// any rename failure just falls through to a recursive copy instead of
// trying to distinguish the cause.
func moveDir(src, dest string) error {
	if err := os.Rename(src, dest); err == nil {
		return nil
	}
	if err := copyDir(src, dest); err != nil {
		os.RemoveAll(dest)
		return err
	}
	os.RemoveAll(src)
	return nil
}

// copyDir recursively copies src's contents into dest (created if
// needed), preserving file modes and symlinks.
func copyDir(src, dest string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := dest
		if rel != "." {
			target = filepath.Join(dest, rel)
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case info.IsDir():
			return os.MkdirAll(target, info.Mode())
		default:
			return copyFile(path, target, info.Mode())
		}
	})
}

func copyFile(src, dest string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
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
		if !osutil.Exists("git") {
			return a.errf("git is not installed (required for wor source pull)")
		}
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
		if !osutil.Exists("git") {
			return a.errf("git is not installed (required for wor source clone)")
		}
		if len(rest) == 0 || rest[0] == "" {
			return a.errf("git-url is required: wor source clone %s <git-url>", target)
		}
		git := rest[0]
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
			os.RemoveAll(tmp)
			return err
		}

		if _, err := os.Stat(dest); err == nil {
			// Target already has source: back it up, then swap it for
			// the freshly cloned tree. No --replace flag needed -- this
			// is always the desired outcome, the backup is the safety
			// net.
			if _, err := a.sourceBackup(target, ""); err != nil {
				os.RemoveAll(tmp)
				return err
			}
			if err := replaceDir(tmp, dest); err != nil {
				return err
			}
			a.ok("Cloned: %s", target)
			return nil
		}

		os.MkdirAll(filepath.Dir(dest), 0o755)
		if err := moveDir(tmp, dest); err != nil {
			return err
		}
		a.ok("Cloned: %s", target)
		return nil

	default:
		a.usage()
		return a.errf("unknown source action: %s", action)
	}
}
