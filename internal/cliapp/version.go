package cliapp

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"wor/internal/dbbackup"
	"wor/internal/hostprovider"
	"wor/internal/osutil"
	"wor/internal/phpfpm"
)

// cmdVersion is deliberately just "did the binary install correctly"
// (product/version/OS/distro/build target/bin path/workspace state)
// -- it does not report on Node/npm/PM2 or any other runtime, and
// deliberately omits WOR_HOME/Config too. Those are config/environment
// concerns owned by `wor doctor` (which also now shows OS/Distro/Build
// in its own Environment section) and `wor env`; install.sh's own
// "Next steps" already documents the intended split: `wor version`
// confirms the binary, `wor doctor` confirms every runtime and the
// environment it's running in. Duplicating that here too just means
// more places that can disagree or drift out of sync.
func (a *App) cmdVersion() {
	exe, _ := os.Executable()
	fmt.Fprintln(a.Out, ProductName)
	fmt.Fprintln(a.Out, strings.Repeat("-", len(ProductName)))
	fmt.Fprintf(a.Out, "Version  : %s\n", Version)
	fmt.Fprintf(a.Out, "OS       : %s\n", osutil.OSName())
	if distro, ok := osutil.LinuxDistro(); ok {
		fmt.Fprintf(a.Out, "Distro   : %s\n", distro)
	}
	// Build is the GOOS/GOARCH this binary was actually compiled for
	// (e.g. "linux/amd64") -- useful for confirming the right one of
	// scripts/build.sh --release's 5 cross-compiled targets got
	// installed, distinct from OS (the host's own OS/family label).
	fmt.Fprintf(a.Out, "Build    : %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(a.Out, "Bin      : %s\n", exe)
	if a.workspaceInitialized() {
		a.docOK("Workspace initialized")
	} else {
		a.docFail("Workspace not initialized (run: wor setup)")
	}
}

func (a *App) cmdEnv(args []string) error {
	fmt.Fprintln(a.Out, "WOR Environment")
	fmt.Fprintln(a.Out, "---------------")
	fmt.Fprintf(a.Out, "OS                 : %s\n", osutil.OSName())
	fmt.Fprintf(a.Out, "WOR_ENV            : %s\n", a.Cfg.Env)
	fmt.Fprintf(a.Out, "CONFIG_FILE        : %s\n", a.Cfg.ConfigFile)
	fmt.Fprintf(a.Out, "WOR_HOME           : %s\n", a.Cfg.WorHome)
	fmt.Fprintf(a.Out, "WOR_DOMAINS        : %s\n", a.Cfg.Domains)
	fmt.Fprintf(a.Out, "WOR_BACKUPS        : %s\n", a.Cfg.Backups)
	fmt.Fprintf(a.Out, "HOST_PROVIDER      : %s\n", a.Cfg.HostProviderName())

	nginxP, _ := hostprovider.New("nginx", a.Cfg)
	apacheP, _ := hostprovider.New("apache", a.Cfg)
	if bin, ok := nginxP.Binary(); ok {
		fmt.Fprintf(a.Out, "NGINX_BIN          : %s\n", bin)
	} else {
		fmt.Fprintln(a.Out, "NGINX_BIN          : not found")
	}
	fmt.Fprintf(a.Out, "NGINX_AVAILABLE    : %s\n", nginxP.SitesAvailable())
	fmt.Fprintf(a.Out, "NGINX_ENABLED      : %s\n", nginxP.SitesEnabled())
	fmt.Fprintf(a.Out, "NGINX_LOG_DIR      : %s\n", nginxP.LogDir())
	if bin, ok := apacheP.Binary(); ok {
		fmt.Fprintf(a.Out, "APACHE_BIN         : %s\n", bin)
	} else {
		fmt.Fprintln(a.Out, "APACHE_BIN         : not found")
	}
	fmt.Fprintf(a.Out, "APACHE_AVAILABLE   : %s\n", apacheP.SitesAvailable())
	fmt.Fprintf(a.Out, "APACHE_ENABLED     : %s\n", apacheP.SitesEnabled())
	fmt.Fprintf(a.Out, "APACHE_LOG_DIR     : %s\n", apacheP.LogDir())
	if ep, ok := hostprovider.PHPFPMEndpoint(a.Cfg); ok {
		fmt.Fprintf(a.Out, "PHP_FPM_ENDPOINT   : %s\n", ep)
	} else {
		fmt.Fprintln(a.Out, "PHP_FPM_ENDPOINT   : not configured")
	}
	if versions := phpfpm.DetectVersions(); len(versions) > 0 {
		fmt.Fprintf(a.Out, "PHP_FPM_VERSIONS   : %s (per-service pools available)\n", phpVersionNumbers(versions))
	} else {
		fmt.Fprintln(a.Out, "PHP_FPM_VERSIONS   : none detected (per-service pools unavailable, using PHP_FPM_ENDPOINT)")
	}
	fmt.Fprintf(a.Out, "DB_ENGINE          : %s\n", dbbackup.DetectEngine())
	if bin, ok := dbbackup.ClientBin(dbEngineForDetect()); ok {
		fmt.Fprintf(a.Out, "DB_CLIENT_BIN      : %s\n", bin)
	} else {
		fmt.Fprintln(a.Out, "DB_CLIENT_BIN      : not found")
	}
	return nil
}

// dbEngineForDetect maps the human label back to an engine id for the
// client-bin lookup, mirroring the shell version showing whichever
// client happens to be installed.
func dbEngineForDetect() string {
	switch dbbackup.DetectEngine() {
	case "MariaDB":
		return "mariadb"
	case "MySQL":
		return "mysql"
	case "PostgreSQL":
		return "postgresql"
	case "SQL Server":
		return "sqlserver"
	case "SQLite":
		return "sqlite"
	default:
		return ""
	}
}
