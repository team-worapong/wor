package hostprovider

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"wor/internal/config"
	"wor/internal/domainmodel"
	"wor/internal/osutil"
	"wor/internal/render"
	"wor/internal/templates"
)

type apacheProvider struct {
	cfg *config.Config
}

func newApache(cfg *config.Config) *apacheProvider { return &apacheProvider{cfg: cfg} }

func (a *apacheProvider) sitesAvailable() string {
	if a.cfg.ApacheSitesAvailable != "" {
		return a.cfg.ApacheSitesAvailable
	}
	if osutil.IsMacOS() {
		for _, p := range []string{"/opt/homebrew/etc/httpd/servers", "/usr/local/etc/httpd/servers"} {
			if dirExists(p) {
				return p
			}
		}
		return "/opt/homebrew/etc/httpd/servers"
	}
	if osutil.IsWindows() {
		return `C:\Apache24\conf\sites-available`
	}
	if dirExists("/etc/apache2/sites-available") {
		return "/etc/apache2/sites-available"
	}
	return "/etc/httpd/conf.d"
}

func (a *apacheProvider) sitesEnabled() string {
	if a.cfg.ApacheSitesEnabled != "" {
		return a.cfg.ApacheSitesEnabled
	}
	if osutil.IsMacOS() || osutil.IsWindows() {
		return a.sitesAvailable()
	}
	if dirExists("/etc/apache2/sites-enabled") {
		return "/etc/apache2/sites-enabled"
	}
	return a.sitesAvailable()
}

func (a *apacheProvider) logDir() string {
	if a.cfg.ApacheLogDir != "" {
		return a.cfg.ApacheLogDir
	}
	if osutil.IsMacOS() {
		for _, p := range []string{"/opt/homebrew/var/log/httpd", "/usr/local/var/log/httpd"} {
			if dirExists(p) {
				return p
			}
		}
		return "/opt/homebrew/var/log/httpd"
	}
	if osutil.IsWindows() {
		return `C:\Apache24\logs`
	}
	if dirExists("/var/log/apache2") {
		return "/var/log/apache2"
	}
	return "/var/log/httpd"
}

func (a *apacheProvider) binary() (string, bool) {
	if a.cfg.Apache.Bin != "" && osutil.IsExecutableFile(a.cfg.Apache.Bin) {
		return a.cfg.Apache.Bin, true
	}
	names := []string{"apachectl", "apache2ctl", "httpd"}
	if osutil.IsWindows() {
		names = []string{"httpd.exe"}
	}
	for _, n := range names {
		if p := osutil.Which(n); p != "" {
			return p, true
		}
	}
	for _, p := range apacheFallbackPaths() {
		if osutil.IsExecutableFile(p) {
			return p, true
		}
	}
	return "", false
}

func apacheFallbackPaths() []string {
	if osutil.IsWindows() {
		return []string{`C:\Apache24\bin\httpd.exe`}
	}
	return []string{
		"/usr/sbin/apachectl", "/usr/local/bin/apachectl", "/opt/homebrew/bin/apachectl",
		"/usr/sbin/apache2ctl", "/usr/sbin/httpd", "/usr/local/sbin/httpd", "/opt/homebrew/bin/httpd",
	}
}

func (a *apacheProvider) hostConfigName(host string) string { return "wor__" + host + ".conf" }
func (a *apacheProvider) defaultConfigName() string           { return "000_wor_default.conf" }

// documentRootTemplate mirrors apache_template_uses_document_root():
// every process-supervised template (node, go, python) proxies
// everything to its backing process and must not declare a competing
// DocumentRoot; only static/php serve files directly from disk.
func (a *apacheProvider) documentRootTemplate(svcType string) bool {
	return !domainmodel.TemplateRequiresProcessSupervisor(svcType)
}

func (a *apacheProvider) hostFiles(host string) []string {
	avail, enabled := a.sitesAvailable(), a.sitesEnabled()
	name := a.hostConfigName(host)
	return []string{
		filepath.Join(avail, name), filepath.Join(enabled, name),
		filepath.Join(avail, host), filepath.Join(avail, host+".conf"),
		filepath.Join(enabled, host), filepath.Join(enabled, host+".conf"),
	}
}

func (a *apacheProvider) hostExistsExtra(host, avail, enabled string) bool {
	return grepDirsForPattern([]string{avail, enabled}, "ServerName", host) ||
		grepDirsForPattern([]string{avail, enabled}, "ServerAlias", host)
}

func (a *apacheProvider) enableHost(siteFile, enabledFile string) error {
	avail, enabled := a.sitesAvailable(), a.sitesEnabled()
	if enabled == avail {
		return nil
	}
	if err := osutil.EnsureDir(enabled); err != nil {
		return err
	}
	if osutil.Exists("a2ensite") && avail == "/etc/apache2/sites-available" {
		return runSudo("a2ensite", filepath.Base(siteFile))
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

func (a *apacheProvider) test() error {
	if a.cfg.ApacheTestCommand != "" {
		return shellRun(a.cfg.ApacheTestCommand)
	}
	bin, ok := a.binary()
	if !ok {
		return fmt.Errorf("apache binary not found")
	}
	if osutil.IsMacOS() || osutil.IsWindows() {
		return exec.Command(bin, "configtest").Run()
	}
	return runSudo(bin, "configtest")
}

func (a *apacheProvider) reload() error {
	if a.cfg.ApacheReloadCommand != "" {
		return shellRun(a.cfg.ApacheReloadCommand)
	}
	bin, ok := a.binary()
	if osutil.IsMacOS() {
		if osutil.Exists("brew") {
			return exec.Command("brew", "services", "restart", "httpd").Run()
		}
		if !ok {
			return fmt.Errorf("apache binary not found")
		}
		return exec.Command(bin, "graceful").Run()
	}
	if osutil.IsWindows() {
		if !ok {
			return fmt.Errorf("apache binary not found")
		}
		return exec.Command(bin, "-k", "restart").Run()
	}
	if osutil.Exists("systemctl") {
		if err := runSudo("systemctl", "reload", "apache2"); err == nil {
			return nil
		}
		return runSudo("systemctl", "reload", "httpd")
	}
	if osutil.Exists("service") {
		if err := runSudo("service", "apache2", "reload"); err == nil {
			return nil
		}
		return runSudo("service", "httpd", "reload")
	}
	if !ok {
		return fmt.Errorf("apache binary not found")
	}
	return runSudo(bin, "graceful")
}

func apacheServerAliasLine(aliases []string) string {
	if len(aliases) == 0 {
		return ""
	}
	return "ServerAlias " + strings.Join(aliases, " ")
}

func apacheRedirectBlock(host string, aliases []string, preferred string) string {
	if preferred == "" || len(aliases) == 0 {
		return ""
	}
	var source string
	if preferred == host {
		source = strings.Join(aliases, " ")
	} else {
		source = host
	}
	regex := regexEscapeHost(source)
	return fmt.Sprintf("RewriteEngine On\n    RewriteCond %%{HTTP_HOST} ^%s$ [NC]\n    RewriteRule ^/(.*)$ %%{REQUEST_SCHEME}://%s/$1 [R=301,L]", regex, preferred)
}

func apacheHTTPRedirectBlock(host string, aliases []string, preferred string, sslEnabled bool) string {
	if sslEnabled {
		target := host
		if preferred != "" && len(aliases) > 0 {
			target = preferred
		}
		return fmt.Sprintf("RewriteEngine On\n    RewriteRule ^/(.*)$ https://%s/$1 [R=301,L]", target)
	}
	return apacheRedirectBlock(host, aliases, preferred)
}

func apacheDocumentRootLine(documentRoot string) string {
	if documentRoot == "" {
		return ""
	}
	return fmt.Sprintf("DocumentRoot %q", documentRoot)
}

func apacheSSLChainFileLine(chainFile string) string {
	if chainFile == "" {
		return ""
	}
	return fmt.Sprintf("SSLCertificateChainFile %q", chainFile)
}

func (a *apacheProvider) writeConfig(p WriteParams, siteFile string) error {
	serviceTemplate, err := templates.Get("apache", p.SvcType+".conf")
	if err != nil {
		return err
	}
	httpTemplate, err := templates.Get("webserver/apache", "http.conf")
	if err != nil {
		return err
	}

	documentRootLine := ""
	if a.documentRootTemplate(p.SvcType) {
		documentRootLine = apacheDocumentRootLine(p.DocumentRoot)
	}

	vars := map[string]string{
		"HOST":                     p.Host,
		"SERVER_NAMES":             strings.TrimSpace(p.Host + " " + strings.Join(p.Aliases, " ")),
		"APACHE_SERVER_ALIAS":      apacheServerAliasLine(p.Aliases),
		"APACHE_HTTP_REDIRECT":     apacheHTTPRedirectBlock(p.Host, p.Aliases, p.Preferred, p.SSLEnabled),
		"APACHE_SSL_CHAIN_FILE":    apacheSSLChainFileLine(p.SSLChainFile),
		"DOMAIN":                   p.Domain,
		"SERVICE":                  p.Service,
		"PORT":                     portString(p.Port),
		"DOCUMENT_ROOT":            p.DocumentRoot,
		"APACHE_DOCUMENT_ROOT_LINE": documentRootLine,
		"DEFAULT_PUBLIC_PATH":      p.DefaultPublicPath,
		"APACHE_LOG_DIR":           a.logDir(),
		"PHP_FPM_ENDPOINT":         p.PHPFPMEndpoint,
		"SSL_CERT_FILE":            p.SSLCertFile,
		"SSL_KEY_FILE":             p.SSLKeyFile,
	}
	vars["APACHE_SERVICE_CONFIG"] = render.Render(serviceTemplate, vars)
	if p.SSLEnabled {
		httpsTpl, err := templates.Get("webserver/apache", "https.conf")
		if err != nil {
			return err
		}
		vars["APACHE_HTTPS_VHOST"] = render.Render(httpsTpl, vars)
	} else {
		vars["APACHE_HTTPS_VHOST"] = ""
	}

	out := render.Render(httpTemplate, vars)
	return osutil.WriteFilePrivileged(siteFile, []byte(out))
}
