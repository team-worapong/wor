package cliapp

import (
	"io"
	"strings"
	"testing"

	"wor/internal/config"
)

func TestIsLocalHostname(t *testing.T) {
	cases := []struct {
		host string
		want bool
	}{
		{"myapp", true},          // no dot at all
		{"app.local", true},      // mDNS
		{"app.test", true},       // RFC 6761 reserved
		{"app.localhost", true},  //
		{"site.example", true},   //
		{"thing.invalid", true},  //
		{"APP.LOCAL", true},      // case-insensitive
		{"app.local.", true},     // trailing root dot
		{"team.ddns.net", false}, // a real, reachable name
		{"example.com", false},
		{"www.example.co.uk", false},
		// A public name that merely contains a reserved word is not
		// local: only a real suffix match counts.
		{"localhost.example.com", false},
		{"testing.example.com", false},
	}
	for _, c := range cases {
		if got := isLocalHostname(c.host); got != c.want {
			t.Errorf("isLocalHostname(%q) = %v, want %v", c.host, got, c.want)
		}
	}
}

// The redirect default is on only for a certificate a browser will
// actually trust, and never for a local hostname -- forcing HTTPS with
// an untrusted certificate makes the site unreachable without clicking
// through a warning on every visit.
func TestDefaultForceHTTPS(t *testing.T) {
	cases := []struct {
		provider string
		host     string
		want     bool
	}{
		{"letsencrypt", "team.ddns.net", true},
		{"custom", "team.ddns.net", true},
		{"self-signed", "team.ddns.net", false},
		{"letsencrypt", "app.local", false},
		{"custom", "app.test", false},
		{"self-signed", "app.local", false},
		{"none", "team.ddns.net", false},
	}
	for _, c := range cases {
		if got := defaultForceHTTPS(c.provider, c.host); got != c.want {
			t.Errorf("defaultForceHTTPS(%q, %q) = %v, want %v", c.provider, c.host, got, c.want)
		}
	}
}

// The flags must answer without prompting, so automation and the
// certbot hook never block on stdin.
func TestResolveForceHTTPSHonoursFlagsWithoutPrompting(t *testing.T) {
	app := &App{}
	if !app.resolveForceHTTPS(parseFlags([]string{"--redirect"}), "self-signed", "app.local") {
		t.Error("--redirect should force the redirect on even where the default is off")
	}
	if app.resolveForceHTTPS(parseFlags([]string{"--no-redirect"}), "letsencrypt", "example.com") {
		t.Error("--no-redirect should force the redirect off even where the default is on")
	}
}

// The sudo carve-out must stay exactly one command wide: it is there so
// certbot's deploy hook works under `sudo certbot renew`, not to make
// wor generally runnable as root.
func TestOnlySSLSyncMayRunUnderSudo(t *testing.T) {
	allowed := [][]string{
		{"ssl", "sync", "example.com"},
	}
	refused := [][]string{
		{"ssl", "issue", "example.com"},
		{"ssl", "renew", "example.com"},
		{"ssl"},
		{"host", "add", "example.com"},
		{"run"},
		{"deploy", "example.com/web"},
		{"sync"},
	}
	for _, args := range allowed {
		if !allowsSudoElevation(args[0], args[1:]) {
			t.Errorf("%v should be allowed under sudo", args)
		}
	}
	for _, args := range refused {
		if allowsSudoElevation(args[0], args[1:]) {
			t.Errorf("%v must NOT be allowed under sudo", args)
		}
	}
}

func TestPositionalArgSkipsFlags(t *testing.T) {
	if got := positionalArg([]string{"--preferred=x", "on"}); got != "on" {
		t.Errorf("positionalArg = %q, want \"on\"", got)
	}
	if got := positionalArg([]string{"--preferred=x"}); got != "" {
		t.Errorf("positionalArg = %q, want \"\"", got)
	}
}

// The renewal hook runs as root, where "~" is root's home and the
// operator's ~/.wor/config is invisible -- so WOR_HOME has to travel
// with the command or the hook syncs into the wrong workspace.
func TestRenewHookCarriesWorHomeAndAnAbsoluteBinary(t *testing.T) {
	app := &App{Cfg: &config.Config{WorHome: "/Users/someone/wor"}, Err: io.Discard}
	hook := app.renewHookCommand("app.example.com")
	if hook == "" {
		t.Fatal("hook should have been built")
	}
	if !strings.Contains(hook, "WOR_HOME='/Users/someone/wor'") {
		t.Errorf("hook must carry WOR_HOME: %s", hook)
	}
	if !strings.HasSuffix(hook, " ssl sync app.example.com") {
		t.Errorf("hook must end with the sync subcommand: %s", hook)
	}
	binary := strings.TrimSuffix(strings.SplitN(hook, " ", 3)[2], " ssl sync app.example.com")
	if !strings.HasPrefix(binary, "'/") {
		t.Errorf("the binary path must be absolute and quoted: %s", hook)
	}
}

// certbot validates a hook before it runs it: it takes the first
// whitespace-separated token and refuses the whole command unless that
// token is an executable on PATH. A hook that began with the
// environment assignment itself ("WOR_HOME=... wor ssl sync ...") made
// that token "WOR_HOME=/opt/wor", so certbot aborted every issuance
// with "Unable to find renew-hook command ... in the PATH" before it
// ever contacted the ACME server. That check runs when the flag is
// parsed, so it still applies now that the hook is registered rather
// than run. Passing the variable through env(1) keeps a real command
// name in front. This asserts the property certbot actually checks
// rather than the exact string, so a later rewrite of the hook cannot
// reintroduce the failure.
func TestRenewHookStartsWithACommandNameCertbotCanFind(t *testing.T) {
	app := &App{Cfg: &config.Config{WorHome: "/opt/wor"}, Err: io.Discard}
	hook := app.renewHookCommand("app.example.com")
	if hook == "" {
		t.Fatal("hook should have been built")
	}
	first := strings.SplitN(hook, " ", 2)[0]
	if strings.ContainsAny(first, "='\"") {
		t.Errorf("certbot rejects a hook whose first token is not a plain command name, got %q: %s", first, hook)
	}
	if first != "env" {
		t.Errorf("first token = %q, want \"env\": %s", first, hook)
	}
}

func TestShellQuoteEscapesQuotes(t *testing.T) {
	if got := shellQuote("/tmp/it's here"); got != `'/tmp/it'\''s here'` {
		t.Errorf("shellQuote = %s", got)
	}
}
