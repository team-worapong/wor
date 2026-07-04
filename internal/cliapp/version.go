package cliapp

import (
	"fmt"
	"os"

	"wor/internal/dbbackup"
	"wor/internal/hostprovider"
	"wor/internal/osutil"
	"wor/internal/pm2"
)

func (a *App) cmdVersion() {
	exe, _ := os.Executable()
	fmt.Fprintln(a.Out, "WOR CLI")
	fmt.Fprintln(a.Out, "-------")
	fmt.Fprintf(a.Out, "Version  : %s\n", Version)
	fmt.Fprintf(a.Out, "OS       : %s\n", osutil.OSName())
	fmt.Fprintf(a.Out, "WOR_HOME : %s\n", a.Cfg.WorHome)
	fmt.Fprintf(a.Out, "Bin      : %s\n", exe)
	fmt.Fprintf(a.Out, "Node     : %s\n", versionOrNotFound("node", "--version"))
	fmt.Fprintf(a.Out, "npm      : %s\n", versionOrNotFound("npm", "-v"))
	fmt.Fprintf(a.Out, "PM2      : %s\n", pm2.Version())
}

func versionOrNotFound(bin string, args ...string) string {
	if !osutil.Exists(bin) {
		return "not found"
	}
	return osutil.RunVersion(bin, args...)
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
