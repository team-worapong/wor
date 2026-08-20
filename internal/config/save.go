package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"wor/internal/osutil"
)

// Save writes the resolved settings back to c.ConfigFile in the same
// `key=value` format load_user_config() reads, matching
// commands/setup.sh setup_write_config().
func (c *Config) Save() error {
	dir := filepath.Dir(c.ConfigFile)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var b []byte
	line := func(k, v string) {
		b = append(b, []byte(fmt.Sprintf("%s=%s\n", k, v))...)
	}
	line("environment", c.Env)
	line("wor_home", c.WorHome)
	line("host_provider", c.HostProviderName())
	line("nginx_bin", c.Nginx.Bin)
	line("nginx_version", c.Nginx.Version)
	line("apache_bin", c.Apache.Bin)
	line("apache_version", c.Apache.Version)
	line("ssl_provider", c.SSLProviderName())
	line("certbot_bin", c.Certbot.Bin)
	line("certbot_version", c.Certbot.Version)
	line("php_enabled", fmt.Sprintf("%v", c.PHPEnabled))
	line("php_bin", c.PHP.Bin)
	line("php_version", c.PHP.Version)
	line("php_fpm_bin", c.PHPFPM.Bin)
	line("php_fpm_version", c.PHPFPM.Version)
	if c.PHPFPMEndpoint != "" {
		line("PHP_FPM_ENDPOINT", c.PHPFPMEndpoint)
	}
	line("node_bin", c.Node.Bin)
	line("node_version", c.Node.Version)
	line("npm_bin", c.NPM.Bin)
	line("npm_version", c.NPM.Version)
	line("pm2_bin", c.PM2.Bin)
	line("pm2_version", c.PM2.Version)
	line("git_bin", c.Git.Bin)
	line("git_version", c.Git.Version)
	return osutil.WriteFileAtomic(c.ConfigFile, b, 0o600)
}

// SaveHostEnv writes $WOR_HOME/configs/host.env, matching
// lib/paths.sh ensure_root_dirs()'s host.env scaffold.
func SaveHostEnv(path, hostProvider string) error {
	if _, err := os.Stat(path); err == nil {
		return nil // never overwrite an existing host.env
	}
	content := fmt.Sprintf(`HOST_PROVIDER=%s
# Optional overrides:
# NGINX_SITES_AVAILABLE=/etc/nginx/sites-available
# NGINX_SITES_ENABLED=/etc/nginx/sites-enabled
# NGINX_LOG_DIR=/var/log/nginx
# NGINX_TEST_COMMAND=nginx -t
# NGINX_RELOAD_COMMAND=systemctl reload nginx
# APACHE_SITES_AVAILABLE=/etc/apache2/sites-available
# APACHE_SITES_ENABLED=/etc/apache2/sites-enabled
# APACHE_LOG_DIR=/var/log/apache2
# APACHE_TEST_COMMAND=apachectl configtest
# APACHE_RELOAD_COMMAND=systemctl reload apache2
# PHP_FPM_ENDPOINT=unix:/run/php/php8.4-fpm.sock
`, hostProvider)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return osutil.WriteFileAtomic(path, []byte(content), 0o644)
}

// SetHostEnvKey upserts a single `KEY=value` line in host.env, leaving
// every other line -- including the commented-out override hints
// SaveHostEnv scaffolds -- exactly as it found them.
//
// SaveHostEnv deliberately never touches an existing host.env, so a
// setting that has to be written to an already-provisioned host needs
// this instead. Writes go through WriteFilePrivileged because host.env
// lives inside WOR_HOME and so belongs to the operator account, which
// is not necessarily the account running `wor setup`.
//
// An existing line is matched case-insensitively on the key, so a file
// that already spells it `wor_user` is updated in place rather than
// gaining a second, contradictory line in the other case -- ParseKV
// would then let file order decide the winner.
func SetHostEnvKey(path, key, value string) error {
	var lines []string
	data, err := os.ReadFile(path)
	if err != nil && os.IsPermission(err) {
		data, err = osutil.ReadFilePrivileged(path)
	}
	switch {
	case err == nil:
		lines = strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	case os.IsNotExist(err):
		lines = nil
	default:
		return err
	}

	replaced := false
	for i, line := range lines {
		name, _, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(name), key) {
			continue
		}
		lines[i] = key + "=" + value
		replaced = true
	}
	if !replaced {
		lines = append(lines, key+"="+value)
	}

	return osutil.WriteFilePrivileged(path, []byte(strings.Join(lines, "\n")+"\n"))
}
