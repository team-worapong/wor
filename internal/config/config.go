// Package config resolves the WOR runtime root and provider settings,
// porting lib/config.sh (WOR_HOME resolution, ~/.wor/config,
// $WOR_HOME/configs/host.env) to a cross-platform Go equivalent.
package config

import (
	"os"
	"path/filepath"

	"wor/internal/osutil"
)

// RuntimeBin captures a detected tool's path and version string, mirroring
// the *_BIN / *_VERSION pairs the shell version stored in ~/.wor/config.
type RuntimeBin struct {
	Bin     string
	Version string
}

// Config holds every resolved WOR setting for the current run.
type Config struct {
	Env        string // "production" | "development"
	ConfigFile string // resolved path to ~/.wor/config

	WorHome string
	Domains string
	Backups string
	Configs string
	Logs    string
	SSL     string
	Tmp     string

	HostProvider string // "", "nginx", "apache", or "skip"
	SSLProvider  string // "", "letsencrypt", "self-signed", "custom", "none"

	Nginx  RuntimeBin
	Apache RuntimeBin

	NginxSitesAvailable string
	NginxSitesEnabled   string
	NginxLogDir         string
	NginxTestCommand    string
	NginxReloadCommand  string

	ApacheSitesAvailable string
	ApacheSitesEnabled   string
	ApacheLogDir         string
	ApacheTestCommand    string
	ApacheReloadCommand  string

	Certbot RuntimeBin
	PHP     RuntimeBin
	PHPFPM  RuntimeBin

	PHPEnabled     bool
	PHPFPMEndpoint string

	Node RuntimeBin
	NPM  RuntimeBin
	PM2  RuntimeBin
	Git  RuntimeBin
}

// HostProviderName returns the configured host provider, defaulting to
// "nginx" the way lib/webserver.sh host_provider() does.
func (c *Config) HostProviderName() string {
	if c.HostProvider == "" {
		return "nginx"
	}
	return c.HostProvider
}

// SSLProviderName returns the configured default SSL provider, defaulting
// to "none".
func (c *Config) SSLProviderName() string {
	if c.SSLProvider == "" {
		return "none"
	}
	return c.SSLProvider
}

// Load resolves configuration in the same precedence order as the shell
// CLI: explicit process environment variables first, then ~/.wor/config,
// then $WOR_HOME/configs/host.env, then platform defaults.
func Load() (*Config, error) {
	c := &Config{}

	c.ConfigFile = os.Getenv("WOR_CONFIG_FILE")
	if c.ConfigFile == "" {
		home, _ := os.UserHomeDir()
		c.ConfigFile = filepath.Join(home, ".wor", "config")
	}

	configFileExists := false
	if _, statErr := os.Stat(c.ConfigFile); statErr == nil {
		configFileExists = true
	}
	userCfg, err := ParseKV(c.ConfigFile)
	if err != nil {
		return nil, err
	}
	_, hasEnvironmentKey := lookup(userCfg, "environment", "WOR_ENV")

	// --- environment ---
	c.Env = os.Getenv("WOR_ENV")
	if c.Env == "" {
		if v, ok := lookup(userCfg, "environment", "WOR_ENV"); ok {
			c.Env = v
		}
	}

	// --- WOR_HOME (env var, then user config file) ---
	c.WorHome = os.Getenv("WOR_HOME")
	if c.WorHome == "" {
		if v, ok := lookup(userCfg, "wor_home", "WOR_HOME"); ok {
			c.WorHome = v
		}
	}

	if c.Env == "" {
		if configFileExists && !hasEnvironmentKey && c.WorHome != "" {
			c.Env = inferEnvironmentFromWorHome(c.WorHome)
		} else {
			c.Env = defaultEnvForOS()
		}
	}

	if c.WorHome == "" {
		c.WorHome = DefaultWorHome(c.Env)
	}
	c.Domains = filepath.Join(c.WorHome, "domains")
	c.Backups = filepath.Join(c.WorHome, "backups")
	c.Configs = filepath.Join(c.WorHome, "configs")
	c.Logs = filepath.Join(c.WorHome, "logs")
	c.SSL = filepath.Join(c.WorHome, "ssl")
	if tmp := os.Getenv("TMPDIR"); tmp != "" {
		c.Tmp = tmp
	} else {
		c.Tmp = os.TempDir()
	}

	applyBinFields(c, userCfg)

	hostEnvPath := filepath.Join(c.Configs, "host.env")
	hostCfg, err := ParseKV(hostEnvPath)
	if err != nil {
		return nil, err
	}
	applyBinFields(c, hostCfg)

	return c, nil
}

// applyBinFields fills any still-empty fields from m, mirroring the
// shell version's "only set if currently unset" merge semantics used by
// both load_user_config and load_host_provider_config.
func applyBinFields(c *Config, m map[string]string) {
	setIfEmpty(&c.HostProvider, m, "host_provider", "HOST_PROVIDER")
	if v, ok := lookup(m, "ssl_provider", "WOR_DEFAULT_SSL_PROVIDER"); ok && c.SSLProvider == "" {
		c.SSLProvider = normalizeSSLProvider(v)
	}
	setIfEmpty(&c.Nginx.Bin, m, "nginx_bin", "NGINX_BIN")
	setIfEmpty(&c.Nginx.Version, m, "nginx_version", "NGINX_VERSION")
	setIfEmpty(&c.Apache.Bin, m, "apache_bin", "APACHE_BIN")
	setIfEmpty(&c.Apache.Version, m, "apache_version", "APACHE_VERSION")
	setIfEmpty(&c.Certbot.Bin, m, "certbot_bin", "CERTBOT_BIN")
	setIfEmpty(&c.Certbot.Version, m, "certbot_version", "CERTBOT_VERSION")
	setIfEmpty(&c.PHP.Bin, m, "php_bin", "PHP_BIN")
	setIfEmpty(&c.PHP.Version, m, "php_version", "PHP_VERSION")
	setIfEmpty(&c.PHPFPM.Bin, m, "php_fpm_bin", "PHP_FPM_BIN")
	setIfEmpty(&c.PHPFPM.Version, m, "php_fpm_version", "PHP_FPM_VERSION")
	setIfEmpty(&c.PHPFPMEndpoint, m, "PHP_FPM_ENDPOINT")
	setIfEmpty(&c.Node.Bin, m, "node_bin", "NODE_BIN")
	setIfEmpty(&c.Node.Version, m, "node_version", "NODE_VERSION")
	setIfEmpty(&c.NPM.Bin, m, "npm_bin", "NPM_BIN")
	setIfEmpty(&c.NPM.Version, m, "npm_version", "NPM_VERSION")
	setIfEmpty(&c.PM2.Bin, m, "pm2_bin", "PM2_BIN")
	setIfEmpty(&c.PM2.Version, m, "pm2_version", "PM2_VERSION")
	setIfEmpty(&c.Git.Bin, m, "git_bin", "GIT_BIN")
	setIfEmpty(&c.Git.Version, m, "git_version", "GIT_VERSION")

	setIfEmpty(&c.NginxSitesAvailable, m, "NGINX_SITES_AVAILABLE")
	setIfEmpty(&c.NginxSitesEnabled, m, "NGINX_SITES_ENABLED")
	setIfEmpty(&c.NginxLogDir, m, "NGINX_LOG_DIR")
	setIfEmpty(&c.NginxTestCommand, m, "NGINX_TEST_COMMAND")
	setIfEmpty(&c.NginxReloadCommand, m, "NGINX_RELOAD_COMMAND")
	setIfEmpty(&c.ApacheSitesAvailable, m, "APACHE_SITES_AVAILABLE")
	setIfEmpty(&c.ApacheSitesEnabled, m, "APACHE_SITES_ENABLED")
	setIfEmpty(&c.ApacheLogDir, m, "APACHE_LOG_DIR")
	setIfEmpty(&c.ApacheTestCommand, m, "APACHE_TEST_COMMAND")
	setIfEmpty(&c.ApacheReloadCommand, m, "APACHE_RELOAD_COMMAND")

	if v, ok := lookup(m, "php_enabled", "PHP_ENABLED"); ok {
		c.PHPEnabled = v == "true" || v == "1" || v == "yes"
	}
}

func setIfEmpty(dst *string, m map[string]string, aliases ...string) {
	if *dst != "" {
		return
	}
	if v, ok := lookup(m, aliases...); ok {
		*dst = v
	}
}

func normalizeSSLProvider(v string) string {
	switch v {
	case "skip", "none", "":
		return "none"
	default:
		return v
	}
}

func inferEnvironmentFromWorHome(home string) string {
	if osutil.IsWindows() {
		// ProgramData is the closest analogue to /opt or /srv on Windows.
		programData := os.Getenv("ProgramData")
		if programData != "" && hasPathPrefix(home, programData) {
			return "production"
		}
		return "development"
	}
	for _, prefix := range []string{"/opt/", "/srv/", "/var/"} {
		if hasPathPrefix(home, prefix) || home == prefix[:len(prefix)-1] {
			return "production"
		}
	}
	return "development"
}

func hasPathPrefix(path, prefix string) bool {
	if len(path) < len(prefix) {
		return false
	}
	return path[:len(prefix)] == prefix
}

func defaultEnvForOS() string {
	if osutil.IsLinux() {
		return "production"
	}
	return "development"
}

// DefaultWorHome mirrors lib/config.sh DEFAULT_WOR_HOME(), extended
// with a Windows convention: ProgramData for production, the user
// profile for development.
func DefaultWorHome(env string) string {
	if osutil.IsWindows() {
		if env == "production" {
			programData := os.Getenv("ProgramData")
			if programData == "" {
				programData = `C:\ProgramData`
			}
			return filepath.Join(programData, "wor")
		}
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "wor")
	}
	if osutil.IsMacOS() {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "wor")
	}
	// Linux and other Unix.
	if env == "production" {
		return "/opt/wor"
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "wor")
}
