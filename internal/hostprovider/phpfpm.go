package hostprovider

import (
	"fmt"
	"strings"

	"wor/internal/config"
	"wor/internal/osutil"
)

// unixPHPFPMSockets are common PHP-FPM unix socket locations checked in
// order, matching lib/os.sh php_fpm_endpoint().
var unixPHPFPMSockets = []string{
	"/run/php/php8.4-fpm.sock",
	"/run/php/php8.3-fpm.sock",
	"/run/php/php-fpm.sock",
	"/var/run/php/php8.4-fpm.sock",
	"/var/run/php/php-fpm.sock",
	"/opt/homebrew/var/run/php-fpm.sock",
}

// PHPFPMEndpoint resolves the configured or auto-detected PHP-FPM
// FastCGI endpoint. On Windows there is no unix-socket convention, so
// only an explicit PHP_FPM_ENDPOINT (typically 127.0.0.1:9000) is used.
func PHPFPMEndpoint(cfg *config.Config) (string, bool) {
	if cfg.PHPFPMEndpoint != "" {
		return cfg.PHPFPMEndpoint, true
	}
	if osutil.IsWindows() {
		return "", false
	}
	for _, sock := range unixPHPFPMSockets {
		if pathExists(sock) {
			return "unix:" + sock, true
		}
	}
	return "", false
}

// PHPFPMEndpointForNginx returns the endpoint formatted for nginx's
// fastcgi_pass directive (unchanged from the resolved value).
func PHPFPMEndpointForNginx(cfg *config.Config) (string, error) {
	ep, ok := PHPFPMEndpoint(cfg)
	if !ok {
		return "", fmt.Errorf("PHP_FPM_ENDPOINT is not configured")
	}
	return ep, nil
}

// PHPFPMEndpointForApache reformats a unix-socket endpoint into
// Apache's mod_proxy_fcgi SetHandler syntax, matching
// lib/os.sh php_fpm_endpoint_for_apache().
func PHPFPMEndpointForApache(cfg *config.Config) (string, error) {
	ep, ok := PHPFPMEndpoint(cfg)
	if !ok {
		return "", fmt.Errorf("PHP_FPM_ENDPOINT is not configured")
	}
	if strings.HasPrefix(ep, "unix:") {
		sock := strings.TrimPrefix(ep, "unix:")
		return fmt.Sprintf("proxy:unix:%s|fcgi://localhost/", sock), nil
	}
	return fmt.Sprintf("proxy:fcgi://%s", ep), nil
}
