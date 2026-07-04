package hostprovider

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

// DetectListenAddrs asks the installed php-fpm binary what its pool(s)
// actually listen on, instead of guessing from the fixed
// unixPHPFPMSockets list above. It runs `php-fpm -t` (config test
// mode -- validates and exits without binding anything, safe to run
// even while a real php-fpm master is already running) to get
// php-fpm's own self-reported main config file path from its stable
// "configuration file <path> test is successful" message, then follows
// any `include=` directive in that file (resolving globs, as
// php-fpm.d/*.conf commonly is) and scans every resulting file for
// `listen = ` lines. This works regardless of distro/Homebrew layout
// and finds TCP listeners too (e.g. some Homebrew php versions default
// to 127.0.0.1:9000, not a unix socket, which unixPHPFPMSockets can
// never match) -- but it shells out and reads files, so it's used for
// the interactive `wor setup` PHP step rather than on every
// PHPFPMEndpoint() call.
func DetectListenAddrs(fpmBin string) []string {
	if fpmBin == "" {
		return nil
	}
	out, _ := exec.Command(fpmBin, "-t").CombinedOutput()
	confFile := ""
	const marker = "configuration file "
	for _, line := range strings.Split(string(out), "\n") {
		idx := strings.Index(line, marker)
		if idx == -1 {
			continue
		}
		rest := line[idx+len(marker):]
		if end := strings.Index(rest, " test is successful"); end != -1 {
			confFile = strings.TrimSpace(rest[:end])
			break
		}
	}
	if confFile == "" {
		return nil
	}

	files := append([]string{confFile}, resolveFPMIncludes(confFile)...)
	seen := map[string]bool{}
	var eps []string
	for _, f := range files {
		for _, ep := range parseFPMListen(f) {
			if !seen[ep] {
				seen[ep] = true
				eps = append(eps, ep)
			}
		}
	}
	return eps
}

// resolveFPMIncludes returns every file matched by confFile's
// `include=` directive(s) (relative patterns are resolved against
// confFile's own directory, matching php-fpm's own behavior).
func resolveFPMIncludes(confFile string) []string {
	data, err := os.ReadFile(confFile)
	if err != nil {
		return nil
	}
	dir := filepath.Dir(confFile)
	var files []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ";") || !strings.HasPrefix(line, "include") {
			continue
		}
		_, pattern, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		pattern = strings.TrimSpace(pattern)
		if !filepath.IsAbs(pattern) {
			pattern = filepath.Join(dir, pattern)
		}
		matches, _ := filepath.Glob(pattern)
		files = append(files, matches...)
	}
	return files
}

// parseFPMListen scans confFile for `listen = ` directives (any
// section -- this is a deliberately simple line scanner, not a full
// INI parser, since php-fpm pool configs are the only thing it needs
// to read), normalizing unix socket paths to wor's "unix:/path" form.
func parseFPMListen(confFile string) []string {
	data, err := os.ReadFile(confFile)
	if err != nil {
		return nil
	}
	var eps []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != "listen" {
			continue
		}
		val = strings.TrimSpace(val)
		if val == "" {
			continue
		}
		if strings.HasPrefix(val, "/") {
			val = "unix:" + val
		}
		eps = append(eps, val)
	}
	return eps
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
