package hostprovider

import (
	"os"
	"path/filepath"

	"wor/internal/domainmodel"
	"wor/internal/osutil"
	"wor/internal/render"
	"wor/internal/templates"
)

const defaultNotFoundHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Application Not Found</title>
  <link rel="stylesheet" href="/assets/app.css">
</head>
<body>
  <main>
    <h1>Application Not Found</h1>
    <p>This host is not configured on this WOR server.</p>
  </main>
</body>
</html>
`

const defaultNotFoundCSS = `:root { color-scheme: light dark; }
body {
  margin: 0;
  min-height: 100vh;
  display: grid;
  place-items: center;
  font-family: system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
  background: #f6f7f9;
  color: #202124;
}
main {
  width: min(720px, calc(100vw - 48px));
  padding: 40px;
  border-radius: 18px;
  background: #fff;
  box-shadow: 0 10px 30px rgba(0,0,0,.08);
}
h1 { margin: 0 0 12px; font-size: 28px; }
p { margin: 0; color: #5f6368; line-height: 1.6; }
@media (prefers-color-scheme: dark) {
  body { background: #111418; color: #f1f3f4; }
  main { background: #1b1f24; box-shadow: none; }
  p { color: #bdc1c6; }
}
`

// EnsureDefaultDomain creates the "default/web" domain used to serve
// wor's "Application Not Found" page for unmatched hosts, matching
// lib/webserver.sh ensure_default_domain(). This keeps unregistered
// DNS records (or direct-IP requests) from falling through to whatever
// virtual host happens to be listed first.
func EnsureDefaultDomain(store *domainmodel.Store, backupsDir, logsDir string) (publicPath string, err error) {
	domain, service := "default", "web"
	if err := store.MakeDomainFiles(domain); err != nil {
		return "", err
	}
	serviceDir := store.ServiceDir(domain, service)
	public := filepath.Join(serviceDir, "public")
	for _, dir := range []string{serviceDir, public, filepath.Join(public, "assets"),
		filepath.Join(backupsDir, domain, "source"), filepath.Join(backupsDir, domain, "database"),
		filepath.Join(logsDir, domain)} {
		if err := osutil.EnsureDir(dir); err != nil {
			return "", err
		}
	}
	indexPath := filepath.Join(public, "index.html")
	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		if err := os.WriteFile(indexPath, []byte(defaultNotFoundHTML), 0o644); err != nil {
			return "", err
		}
	}
	cssPath := filepath.Join(public, "assets", "app.css")
	if _, err := os.Stat(cssPath); os.IsNotExist(err) {
		if err := os.WriteFile(cssPath, []byte(defaultNotFoundCSS), 0o644); err != nil {
			return "", err
		}
	}

	cfg, err := store.LoadServices(domain)
	if err != nil {
		return "", err
	}
	if cfg.FindService(service) == nil {
		cfg.Services = append(cfg.Services, domainmodel.Service{
			Name: service, Enabled: true, Type: "static",
			Hosts: []string{}, PublicPath: "public", DocumentRoot: "public",
		})
		if err := store.SaveServices(cfg); err != nil {
			return "", err
		}
	}
	return public, nil
}

// EnsureDefaultHost writes and enables the provider's default virtual
// host if one isn't already active, mirroring
// ensure_nginx_default_host()/ensure_apache_default_host(). It returns
// (skipped, error): skipped is true when there was nothing to do
// (binary missing, or a default host already active).
func (p *Provider) EnsureDefaultHost(store *domainmodel.Store, backupsDir, logsDir string) (skipped bool, err error) {
	publicPath, err := EnsureDefaultDomain(store, backupsDir, logsDir)
	if err != nil {
		return false, err
	}
	if _, ok := p.Binary(); !ok {
		return true, nil
	}

	enabledFile := p.DefaultHostEnabledFile()
	if pathExists(enabledFile) {
		return true, nil
	}
	availFile := p.DefaultHostFile()
	if pathExists(availFile) {
		if err := p.EnableHost(availFile, enabledFile); err != nil {
			return false, err
		}
		return false, p.Reload()
	}

	tpl, err := templates.Get(p.Name, "default.conf")
	if err != nil {
		return false, err
	}
	vars := map[string]string{
		"DEFAULT_PUBLIC_PATH": publicPath,
		"NGINX_LOG_DIR":       p.LogDir(),
		"APACHE_LOG_DIR":      p.LogDir(),
	}
	out := render.Render(tpl, vars)
	if err := osutil.WriteFilePrivileged(availFile, []byte(out)); err != nil {
		return false, err
	}
	if err := p.EnableHost(availFile, enabledFile); err != nil {
		return false, err
	}
	return false, p.Reload()
}
