// Package hostprovider abstracts the two supported web servers
// (nginx, apache), porting lib/webserver.sh and
// lib/providers/webserver/{nginx,apache}.sh. Each provider knows how to
// locate its own sites-available/sites-enabled/log directories per OS,
// generate a virtual host file from wor's embedded templates, enable/
// disable a host, and test/reload the server.
package hostprovider

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"wor/internal/config"
	"wor/internal/osutil"
)

// WriteParams carries everything a provider needs to render one virtual
// host file. Aliases/Preferred/SSL fields are optional.
type WriteParams struct {
	Host              string
	Domain            string
	Service           string
	SvcType           string // service template id: static, node, go, python, php
	Port              int    // 0 if the template does not use a port
	SiteFile          string
	Aliases           []string
	Preferred         string // which of Host/Aliases[0] should be the canonical redirect target
	SSLEnabled        bool
	SSLCertFile       string
	SSLKeyFile        string
	SSLChainFile      string // Apache only
	PHPFPMEndpoint    string
	DefaultPublicPath string
	DocumentRoot      string // resolved absolute path to the service's document root
}

// Provider is the interface every host (web server) provider implements.
type Provider struct {
	Name string
	impl providerImpl
}

type providerImpl interface {
	sitesAvailable() string
	sitesEnabled() string
	logDir() string
	binary() (string, bool)
	hostConfigName(host string) string
	defaultConfigName() string
	hostFiles(host string) []string
	hostExistsExtra(host, available, enabled string) bool
	writeConfig(p WriteParams, siteFile string) error
	enableHost(siteFile, enabledFile string) error
	test() error
	reload() error
	running() bool                            // whether the web server process is currently up
	start() error                             // start it if not (used by `wor run`)
	documentRootTemplate(svcType string) bool // whether template needs DOCUMENT_ROOT injected (apache quirk)
}

func New(name string, cfg *config.Config) (*Provider, error) {
	switch name {
	case "nginx":
		return &Provider{Name: "nginx", impl: newNginx(cfg)}, nil
	case "apache":
		return &Provider{Name: "apache", impl: newApache(cfg)}, nil
	default:
		return nil, fmt.Errorf("unsupported host provider: %s", name)
	}
}

func (p *Provider) SitesAvailable() string { return p.impl.sitesAvailable() }
func (p *Provider) SitesEnabled() string   { return p.impl.sitesEnabled() }
func (p *Provider) LogDir() string         { return p.impl.logDir() }
func (p *Provider) Binary() (string, bool) { return p.impl.binary() }

func (p *Provider) HostConfigName(host string) string { return p.impl.hostConfigName(host) }
func (p *Provider) DefaultConfigName() string         { return p.impl.defaultConfigName() }
func (p *Provider) SiteAvailableFile(host string) string {
	return filepath.Join(p.SitesAvailable(), p.HostConfigName(host))
}
func (p *Provider) SiteEnabledFile(host string) string {
	return filepath.Join(p.SitesEnabled(), p.HostConfigName(host))
}
func (p *Provider) DefaultHostFile() string {
	return filepath.Join(p.SitesAvailable(), p.DefaultConfigName())
}
func (p *Provider) DefaultHostEnabledFile() string {
	return filepath.Join(p.SitesEnabled(), p.DefaultConfigName())
}

// HostExists mirrors nginx_host_exists/apache_host_exists: checks the
// well-known file locations first, then greps existing configs for a
// matching server_name/ServerName as a legacy-compatibility fallback.
func (p *Provider) HostExists(host string) bool {
	for _, f := range p.impl.hostFiles(host) {
		if pathExists(f) {
			return true
		}
	}
	return p.impl.hostExistsExtra(host, p.SitesAvailable(), p.SitesEnabled())
}

func (p *Provider) RemoveHostFiles(host string) error {
	for _, f := range p.impl.hostFiles(host) {
		if pathExists(f) {
			if err := osutil.RemoveFilePrivileged(f); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *Provider) EnableHost(siteFile, enabledFile string) error {
	return p.impl.enableHost(siteFile, enabledFile)
}

func (p *Provider) Test() error   { return p.impl.test() }
func (p *Provider) Reload() error { return p.impl.reload() }

// IsRunning reports whether the web server process is currently up.
// Used by `wor run` to decide whether it needs to start the web
// server before touching anything else -- unlike Reload(), which on
// Linux assumes the service is already active (`systemctl reload`
// fails outright otherwise).
func (p *Provider) IsRunning() bool { return p.impl.running() }

// Start starts the web server if it isn't already running. Safe to
// call when it's already up (each impl checks first or the underlying
// tool itself treats it as a no-op, e.g. `brew services start`).
func (p *Provider) Start() error { return p.impl.start() }

func (p *Provider) WriteConfig(params WriteParams) error {
	return p.impl.writeConfig(params, params.SiteFile)
}

// CleanupBrokenSymlinks removes dangling symlinks from sites-enabled
// (Unix only; a no-op where symlinks aren't used).
func (p *Provider) CleanupBrokenSymlinks() error {
	return cleanupBrokenSymlinks(p.SitesEnabled(), "*")
}

func (p *Provider) CleanupWorBrokenSymlinks() error {
	return cleanupBrokenSymlinks(p.SitesEnabled(), "wor")
}

// RemoveAllWorFiles deletes every wor-managed host file (wor__*.conf and
// the default host file) from both sites-available and sites-enabled.
func (p *Provider) RemoveAllWorFiles() error {
	avail := p.SitesAvailable()
	enabled := p.SitesEnabled()
	patterns := []string{"wor__*.conf", p.DefaultConfigName()}
	for _, dir := range uniqueDirs(avail, enabled) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			for _, pat := range patterns {
				if ok, _ := filepath.Match(pat, e.Name()); ok {
					_ = osutil.RemoveFilePrivileged(filepath.Join(dir, e.Name()))
				}
			}
		}
	}
	return nil
}

// FindWorHostConfigs lists every wor__*.conf file directly inside dir.
func (p *Provider) FindWorHostConfigs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if ok, _ := filepath.Match("wor__*.conf", e.Name()); ok {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out, nil
}

func pathExists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// systemctlActive reports whether unit is active, via a plain
// (unelevated) `systemctl is-active` -- read-only state, so it doesn't
// need to go through osutil.SudoCommand's confirm-once elevation gate,
// matching systemd.querySample's same reasoning for wor-managed units.
func systemctlActive(unit string) bool {
	out, err := exec.Command("systemctl", "is-active", unit).Output()
	return err == nil && strings.TrimSpace(string(out)) == "active"
}

// brewServiceStarted reports whether Homebrew considers name's service
// started, by parsing `brew services list`'s second column. A
// deliberately simple line/field scanner (same tradeoff as this
// project's other best-effort text parsers), not a full table parser.
func brewServiceStarted(name string) bool {
	out, err := exec.Command("brew", "services", "list").Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == name {
			return fields[1] == "started"
		}
	}
	return false
}

// processRunning is the Windows fallback for "is this running" -- there
// is no systemd/Homebrew equivalent there, so this just checks whether a
// process with this image name is listed by tasklist.
func processRunning(imageName string) bool {
	out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq "+imageName).Output()
	return err == nil && strings.Contains(string(out), imageName)
}

// runWithOutput runs cmd with stdout/stderr attached to the current
// process, so the underlying tool's own diagnostic output (nginx's
// "syntax is ok", apache's configtest error with a line number, etc.)
// reaches the user instead of being silently discarded -- which is
// what a bare cmd.Run() does, since exec.Cmd defaults Stdout/Stderr to
// nil (not inherited) when left unset.
func runWithOutput(cmd *exec.Cmd) error {
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// runSudo builds and runs a possibly-elevated command via
// osutil.SudoCommand, propagating a declined-elevation error the same
// way as any other failure. Shared by the nginx/apache providers to
// avoid repeating the (cmd, err) handling at every call site.
func runSudo(name string, args ...string) error {
	cmd, err := osutil.SudoCommand(name, args...)
	if err != nil {
		return err
	}
	return runWithOutput(cmd)
}

func uniqueDirs(dirs ...string) []string {
	seen := map[string]bool{}
	var out []string
	for _, d := range dirs {
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	return out
}

// cleanupBrokenSymlinks removes dangling symlinks in dir. If prefix is
// "wor", only wor-managed names are considered; "*" matches any name.
// On platforms without symlink support this is a no-op.
func cleanupBrokenSymlinks(dir, scope string) error {
	if !osutil.SupportsSymlinks {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		info, err := os.Lstat(filepath.Join(dir, e.Name()))
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			continue
		}
		if scope == "wor" {
			isDefault := e.Name() == "000_wor_default.conf"
			isWor := strings.HasPrefix(e.Name(), "wor__") && strings.HasSuffix(e.Name(), ".conf")
			if !isDefault && !isWor {
				continue
			}
		}
		target := filepath.Join(dir, e.Name())
		if _, err := os.Stat(target); os.IsNotExist(err) {
			os.Remove(target)
		}
	}
	return nil
}

var hostBlockCharsRe = regexp.MustCompile(`[.\[\\*^$()+?{}|]`)

// regexEscapeHost escapes a hostname for embedding in a generated
// nginx/apache regex, matching lib/webserver.sh regex_escape_host().
func regexEscapeHost(host string) string {
	return hostBlockCharsRe.ReplaceAllStringFunc(host, func(s string) string { return "\\" + s })
}
