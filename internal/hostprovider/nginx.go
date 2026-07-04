package hostprovider

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"wor/internal/config"
	"wor/internal/osutil"
	"wor/internal/render"
	"wor/internal/templates"
)

type nginxProvider struct {
	cfg *config.Config
}

func newNginx(cfg *config.Config) *nginxProvider { return &nginxProvider{cfg: cfg} }

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

func (n *nginxProvider) sitesAvailable() string {
	if n.cfg.NginxSitesAvailable != "" {
		return n.cfg.NginxSitesAvailable
	}
	if osutil.IsMacOS() {
		for _, p := range []string{"/opt/homebrew/etc/nginx/servers", "/usr/local/etc/nginx/servers"} {
			if dirExists(p) {
				return p
			}
		}
		return "/opt/homebrew/etc/nginx/servers"
	}
	if osutil.IsWindows() {
		return `C:\nginx\conf\sites-available`
	}
	return "/etc/nginx/sites-available"
}

func (n *nginxProvider) sitesEnabled() string {
	if n.cfg.NginxSitesEnabled != "" {
		return n.cfg.NginxSitesEnabled
	}
	// macOS Homebrew and Windows both use a single flat config directory
	// (no unprivileged symlink step needed); Linux keeps the classic
	// sites-available/sites-enabled split.
	if osutil.IsMacOS() || osutil.IsWindows() {
		return n.sitesAvailable()
	}
	return "/etc/nginx/sites-enabled"
}

func (n *nginxProvider) logDir() string {
	if n.cfg.NginxLogDir != "" {
		return n.cfg.NginxLogDir
	}
	if osutil.IsMacOS() {
		for _, p := range []string{"/opt/homebrew/var/log/nginx", "/usr/local/var/log/nginx"} {
			if dirExists(p) {
				return p
			}
		}
		return "/opt/homebrew/var/log/nginx"
	}
	if osutil.IsWindows() {
		return `C:\nginx\logs`
	}
	return "/var/log/nginx"
}

func (n *nginxProvider) binary() (string, bool) {
	if n.cfg.Nginx.Bin != "" && osutil.IsExecutableFile(n.cfg.Nginx.Bin) {
		return n.cfg.Nginx.Bin, true
	}
	name := "nginx"
	if osutil.IsWindows() {
		name = "nginx.exe"
	}
	if p := osutil.Which(name); p != "" {
		return p, true
	}
	for _, p := range nginxFallbackPaths() {
		if osutil.IsExecutableFile(p) {
			return p, true
		}
	}
	return "", false
}

func nginxFallbackPaths() []string {
	if osutil.IsWindows() {
		return []string{`C:\nginx\nginx.exe`}
	}
	return []string{
		"/usr/sbin/nginx", "/sbin/nginx", "/usr/local/sbin/nginx", "/usr/local/bin/nginx",
		"/opt/homebrew/bin/nginx", "/opt/homebrew/sbin/nginx",
	}
}

func (n *nginxProvider) hostConfigName(host string) string    { return "wor__" + host + ".conf" }
func (n *nginxProvider) defaultConfigName() string             { return "000_wor_default.conf" }
func (n *nginxProvider) documentRootTemplate(string) bool      { return true }

// hostFiles returns every location wor might have written a config for
// host, including legacy unprefixed names from early builds.
func (n *nginxProvider) hostFiles(host string) []string {
	avail, enabled := n.sitesAvailable(), n.sitesEnabled()
	name := n.hostConfigName(host)
	return []string{
		filepath.Join(avail, name), filepath.Join(enabled, name),
		filepath.Join(avail, host), filepath.Join(avail, host+".conf"),
		filepath.Join(enabled, host), filepath.Join(enabled, host+".conf"),
	}
}

func (n *nginxProvider) hostExistsExtra(host, avail, enabled string) bool {
	return grepDirsForPattern([]string{avail, enabled}, "server_name", host)
}

func (n *nginxProvider) enableHost(siteFile, enabledFile string) error {
	avail, enabled := n.sitesAvailable(), n.sitesEnabled()
	if enabled == avail {
		return nil
	}
	if err := osutil.EnsureDir(enabled); err != nil {
		return err
	}
	if !osutil.SupportsSymlinks {
		data, err := os.ReadFile(siteFile)
		if err != nil {
			return err
		}
		return osutil.WriteFilePrivileged(enabledFile, data)
	}
	os.Remove(enabledFile)
	if err := os.Symlink(siteFile, enabledFile); err != nil {
		return runSudo("ln", "-sf", siteFile, enabledFile)
	}
	return nil
}

func (n *nginxProvider) test() error {
	if n.cfg.NginxTestCommand != "" {
		return shellRun(n.cfg.NginxTestCommand)
	}
	bin, ok := n.binary()
	if !ok {
		return fmt.Errorf("nginx binary not found")
	}
	if osutil.IsMacOS() || osutil.IsWindows() {
		return exec.Command(bin, "-t").Run()
	}
	return runSudo(bin, "-t")
}

func (n *nginxProvider) reload() error {
	if n.cfg.NginxReloadCommand != "" {
		return shellRun(n.cfg.NginxReloadCommand)
	}
	bin, ok := n.binary()
	if osutil.IsMacOS() {
		if osutil.Exists("brew") {
			return exec.Command("brew", "services", "restart", "nginx").Run()
		}
		if !ok {
			return fmt.Errorf("nginx binary not found")
		}
		return exec.Command(bin, "-s", "reload").Run()
	}
	if osutil.IsWindows() {
		if !ok {
			return fmt.Errorf("nginx binary not found")
		}
		return exec.Command(bin, "-s", "reload").Run()
	}
	if osutil.Exists("systemctl") {
		return runSudo("systemctl", "reload", "nginx")
	}
	if osutil.Exists("service") {
		return runSudo("service", "nginx", "reload")
	}
	if !ok {
		return fmt.Errorf("nginx binary not found")
	}
	return runSudo(bin, "-s", "reload")
}

// hostCheckBlock rejects requests whose Host header doesn't match one
// of the registered names, routing them to the @wor_default location.
// Port of lib/providers/webserver/nginx.sh nginx_host_check_block().
func nginxHostCheckBlock(host string, aliases []string) string {
	names := append([]string{host}, aliases...)
	var escaped []string
	for _, n := range names {
		escaped = append(escaped, regexEscapeHost(n))
	}
	regex := strings.Join(escaped, "|")
	return fmt.Sprintf("if ($host !~ ^(%s)$) {\n        return 421;\n    }", regex)
}

func nginxRedirectBlock(host string, aliases []string, preferred string) string {
	if preferred == "" || len(aliases) == 0 {
		return ""
	}
	var source string
	if preferred == host {
		source = strings.Join(aliases, " ")
	} else {
		source = host
	}
	return fmt.Sprintf("if ($host = %q) {\n        return 301 $scheme://%s$request_uri;\n    }", source, preferred)
}

func (n *nginxProvider) writeConfig(p WriteParams, siteFile string) error {
	serviceTemplate, err := templates.Get("nginx", p.SvcType+".conf")
	if err != nil {
		return err
	}
	httpTemplate, err := templates.Get("webserver/nginx", "http.conf")
	if err != nil {
		return err
	}

	vars := map[string]string{
		"HOST":              p.Host,
		"SERVER_NAMES":      strings.TrimSpace(p.Host + " " + strings.Join(p.Aliases, " ")),
		"NGINX_HOST_CHECK":  nginxHostCheckBlock(p.Host, p.Aliases),
		"NGINX_REDIRECT":    nginxRedirectBlock(p.Host, p.Aliases, p.Preferred),
		"DOMAIN":            p.Domain,
		"SERVICE":           p.Service,
		"PORT":              portString(p.Port),
		"DOCUMENT_ROOT":     p.DocumentRoot,
		"DEFAULT_PUBLIC_PATH": p.DefaultPublicPath,
		"NGINX_LOG_DIR":     n.logDir(),
		"PHP_FPM_ENDPOINT":  p.PHPFPMEndpoint,
	}
	if p.SSLEnabled {
		httpsTpl, err := templates.Get("webserver/nginx", "https.conf")
		if err != nil {
			return err
		}
		vars["NGINX_HTTPS_CONFIG"] = render.Render(httpsTpl, map[string]string{
			"SSL_CERT_FILE": p.SSLCertFile,
			"SSL_KEY_FILE":  p.SSLKeyFile,
		})
	} else {
		vars["NGINX_HTTPS_CONFIG"] = ""
	}
	vars["NGINX_SERVICE_CONFIG"] = render.Render(serviceTemplate, vars)

	out := render.Render(httpTemplate, vars)
	return osutil.WriteFilePrivileged(siteFile, []byte(out))
}

func portString(p int) string {
	if p <= 0 {
		return ""
	}
	return fmt.Sprintf("%d", p)
}

func shellRun(command string) error {
	if osutil.IsWindows() {
		return exec.Command("cmd", "/C", command).Run()
	}
	return exec.Command("bash", "-lc", command).Run()
}

// grepDirsForPattern is a small stand-in for `grep -R "keyword.*host"`
// used as a legacy-compatibility fallback in HostExists.
func grepDirsForPattern(dirs []string, keyword, host string) bool {
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			text := string(data)
			if strings.Contains(text, keyword) && strings.Contains(text, host) {
				return true
			}
		}
	}
	return false
}
