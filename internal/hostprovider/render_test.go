package hostprovider

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"wor/internal/config"
)

// renderVhost writes a vhost with the given provider and returns the
// generated file's contents.
func renderVhost(t *testing.T, providerName string, mutate func(*WriteParams)) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "sites")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cfg := &config.Config{
		NginxSitesAvailable:  dir,
		NginxSitesEnabled:    dir,
		NginxLogDir:          filepath.Join(root, "log"),
		ApacheSitesAvailable: dir,
		ApacheSitesEnabled:   dir,
		ApacheLogDir:         filepath.Join(root, "log"),
	}
	p, err := New(providerName, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	params := WriteParams{
		Host:              "app.example.com",
		Domain:            "example.com",
		Service:           "web",
		SvcType:           "static",
		SiteFile:          filepath.Join(dir, "vhost.conf"),
		DocumentRoot:      filepath.Join(root, "public"),
		DefaultPublicPath: filepath.Join(root, "default"),
		ACMEWebroot:       filepath.Join(root, "acme"),
	}
	mutate(&params)
	if err := p.WriteConfig(params); err != nil {
		t.Fatalf("WriteConfig: %v", err)
	}
	data, err := os.ReadFile(params.SiteFile)
	if err != nil {
		t.Fatalf("read vhost: %v", err)
	}
	return string(data)
}

func countOccurrences(s, sub string) int { return strings.Count(s, sub) }

func TestNginxWithoutCertificateRendersOnlyPortEighty(t *testing.T) {
	out := renderVhost(t, "nginx", func(p *WriteParams) {})

	if n := countOccurrences(out, "listen 80;"); n != 1 {
		t.Errorf("listen 80 appears %d times, want 1\n%s", n, out)
	}
	if strings.Contains(out, "listen 443") {
		t.Errorf("a host with no certificate must not listen on 443:\n%s", out)
	}
	if !strings.Contains(out, "location /.well-known/acme-challenge/") {
		t.Errorf("the ACME location must be present even before a certificate exists:\n%s", out)
	}
	if !strings.Contains(out, "index index.html") {
		t.Errorf("the service config is missing:\n%s", out)
	}
}

func TestNginxWithCertificateAndNoRedirectServesOnBothPorts(t *testing.T) {
	out := renderVhost(t, "nginx", func(p *WriteParams) {
		p.SSLEnabled = true
		p.SSLCertFile = "/certs/fullchain.pem"
		p.SSLKeyFile = "/certs/privkey.pem"
	})

	if n := countOccurrences(out, "listen 80;"); n != 1 {
		t.Errorf("listen 80 appears %d times, want 1", n)
	}
	if n := countOccurrences(out, "listen 443 ssl;"); n != 1 {
		t.Errorf("listen 443 appears %d times, want 1", n)
	}
	// Both blocks serve, so the service config appears in each.
	if n := countOccurrences(out, "index index.html"); n != 2 {
		t.Errorf("service config appears %d times, want 2 (one per block)\n%s", n, out)
	}
	if strings.Contains(out, "return 301 https://") {
		t.Errorf("ForceHTTPS is off, so nothing should redirect to https:\n%s", out)
	}
	if !strings.Contains(out, "ssl_certificate /certs/fullchain.pem;") {
		t.Errorf("certificate path missing:\n%s", out)
	}
}

func TestNginxWithRedirectKeepsAcmeReachable(t *testing.T) {
	out := renderVhost(t, "nginx", func(p *WriteParams) {
		p.SSLEnabled = true
		p.ForceHTTPS = true
		p.SSLCertFile = "/certs/fullchain.pem"
		p.SSLKeyFile = "/certs/privkey.pem"
	})

	if !strings.Contains(out, "return 301 https://$host$request_uri;") {
		t.Errorf("missing the HTTPS redirect:\n%s", out)
	}
	// The redirect must be inside "location /", not at server level:
	// a server-level return runs before location matching and would
	// swallow the ACME challenge, breaking renewal.
	idxACME := strings.Index(out, "location /.well-known/acme-challenge/")
	idxRedirect := strings.Index(out, "return 301 https://")
	if idxACME < 0 {
		t.Fatalf("ACME location missing:\n%s", out)
	}
	if idxACME > idxRedirect {
		t.Errorf("the ACME location must come before the redirect:\n%s", out)
	}
	if !strings.Contains(out, "location / {\n        return 301 https://") {
		t.Errorf("the redirect must live inside \"location /\", not at server level:\n%s", out)
	}
	// The :80 block redirects only, so the service config appears once.
	if n := countOccurrences(out, "index index.html"); n != 1 {
		t.Errorf("service config appears %d times, want 1 (only the :443 block serves)\n%s", n, out)
	}
}

func TestNginxRedirectUsesTheCanonicalHostWhenOneIsSet(t *testing.T) {
	out := renderVhost(t, "nginx", func(p *WriteParams) {
		p.SSLEnabled = true
		p.ForceHTTPS = true
		p.Aliases = []string{"www.example.com"}
		p.Preferred = "app.example.com"
	})
	if !strings.Contains(out, "return 301 https://app.example.com$request_uri;") {
		t.Errorf("a configured canonical host should be the redirect target:\n%s", out)
	}
}

func TestApacheRedirectExcludesAcmeAndDropsDeadServiceConfig(t *testing.T) {
	out := renderVhost(t, "apache", func(p *WriteParams) {
		p.SSLEnabled = true
		p.ForceHTTPS = true
		p.SSLCertFile = "/certs/fullchain.pem"
		p.SSLKeyFile = "/certs/privkey.pem"
	})

	if !strings.Contains(out, "RewriteCond %{REQUEST_URI} !^/\\.well-known/acme-challenge/") {
		t.Errorf("mod_rewrite runs before Alias, so the redirect must exclude the ACME path:\n%s", out)
	}
	if !strings.Contains(out, "Alias /.well-known/acme-challenge/") {
		t.Errorf("ACME alias missing:\n%s", out)
	}
	// Only the :443 vhost serves.
	if n := countOccurrences(out, "DirectoryIndex index.html"); n != 1 {
		t.Errorf("service config appears %d times, want 1\n%s", n, out)
	}
	if n := countOccurrences(out, "<VirtualHost *:443>"); n != 1 {
		t.Errorf(":443 vhost appears %d times, want 1", n)
	}
}

func TestApacheWithoutForceHTTPSDoesNotRedirect(t *testing.T) {
	out := renderVhost(t, "apache", func(p *WriteParams) {
		p.SSLEnabled = true
		p.SSLCertFile = "/certs/fullchain.pem"
		p.SSLKeyFile = "/certs/privkey.pem"
	})
	if strings.Contains(out, "https://") && strings.Contains(out, "[R=301,L]") {
		t.Errorf("apache used to redirect whenever a certificate existed; it must now wait to be asked:\n%s", out)
	}
	if n := countOccurrences(out, "DirectoryIndex index.html"); n != 2 {
		t.Errorf("service config appears %d times, want 2 (both vhosts serve)\n%s", n, out)
	}
}

// SSLCertificateChainFile has been removed: it was never populated, and
// is unnecessary when serving fullchain.pem.
func TestApacheNoLongerEmitsAChainFileDirective(t *testing.T) {
	out := renderVhost(t, "apache", func(p *WriteParams) {
		p.SSLEnabled = true
		p.SSLCertFile = "/certs/fullchain.pem"
		p.SSLKeyFile = "/certs/privkey.pem"
	})
	if strings.Contains(out, "SSLCertificateChainFile") {
		t.Errorf("unexpected chain file directive:\n%s", out)
	}
	if strings.Contains(out, "{{") {
		t.Errorf("unsubstituted template variable left in the output:\n%s", out)
	}
}

// Every placeholder must be substituted; a leftover {{VAR}} is a config
// the web server would reject.
func TestNoUnsubstitutedPlaceholders(t *testing.T) {
	for _, name := range []string{"nginx", "apache"} {
		for _, force := range []bool{false, true} {
			out := renderVhost(t, name, func(p *WriteParams) {
				p.SSLEnabled = true
				p.ForceHTTPS = force
				p.SSLCertFile = "/certs/fullchain.pem"
				p.SSLKeyFile = "/certs/privkey.pem"
				p.CustomConfigBaseDir = "/srv/app/.wor"
			})
			if strings.Contains(out, "{{") {
				t.Errorf("%s (forceHTTPS=%v) left a placeholder behind:\n%s", name, force, out)
			}
		}
	}
}

// The redirect-only :80 block still needs the host check. Without it,
// a request with an unknown Host would be answered with a redirect to
// that Host -- an open redirect wherever this block is the fallback.
func TestNginxRedirectBlockKeepsTheHostCheck(t *testing.T) {
	out := renderVhost(t, "nginx", func(p *WriteParams) {
		p.SSLEnabled = true
		p.ForceHTTPS = true
		p.SSLCertFile = "/certs/fullchain.pem"
		p.SSLKeyFile = "/certs/privkey.pem"
	})
	eighty := out[:strings.Index(out, "listen 443 ssl;")]
	if !strings.Contains(eighty, "return 421;") {
		t.Errorf("the :80 redirect block must keep the host check:\n%s", eighty)
	}
	if !strings.Contains(eighty, "location @wor_default") {
		t.Errorf("the :80 redirect block must keep the 421 fallback location:\n%s", eighty)
	}
}
