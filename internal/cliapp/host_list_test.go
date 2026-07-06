package cliapp

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"wor/internal/config"
	"wor/internal/domainmodel"
	"wor/internal/hostprovider"
	"wor/internal/ssl"
)

// newTestHostListApp builds an *App wired to a temporary WOR_HOME-style
// layout: a domain registry (Store), and a nginx provider whose
// sites-available/sites-enabled directories are separate temp dirs (so
// the enabled/disabled split is meaningful, matching the Linux
// symlink-based layout rather than macOS/Windows' single flat dir).
func newTestHostListApp(t *testing.T) (*App, string, string) {
	t.Helper()
	root := t.TempDir()
	store := domainmodel.NewStore(filepath.Join(root, "domains"))

	avail := filepath.Join(root, "nginx", "sites-available")
	enabled := filepath.Join(root, "nginx", "sites-enabled")
	sslRoot := filepath.Join(root, "ssl")
	for _, d := range []string{avail, enabled, sslRoot} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s): %v", d, err)
		}
	}

	cfg := &config.Config{
		HostProvider:        "nginx",
		NginxSitesAvailable: avail,
		NginxSitesEnabled:   enabled,
		SSL:                 sslRoot,
	}
	app := &App{
		Cfg:   cfg,
		Store: store,
		Out:   &bytes.Buffer{},
		Err:   &bytes.Buffer{},
	}
	return app, avail, enabled
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func TestPrintHostListEnabledDisabledSSL(t *testing.T) {
	app, avail, enabled := newTestHostListApp(t)

	if err := app.Store.MakeDomainFiles("shop-example-com"); err != nil {
		t.Fatalf("MakeDomainFiles: %v", err)
	}
	if err := app.Store.AddService("shop-example-com", "api-gateway", "", 8080, "go", ""); err != nil {
		t.Fatalf("AddService(api-gateway): %v", err)
	}
	if err := app.Store.AddHostToService("shop-example-com", "api-gateway", "api.example.com"); err != nil {
		t.Fatalf("AddHostToService(api): %v", err)
	}
	if err := app.Store.AddService("shop-example-com", "landing", "", 0, "static", ""); err != nil {
		t.Fatalf("AddService(landing): %v", err)
	}
	if err := app.Store.AddHostToService("shop-example-com", "landing", "internal.local"); err != nil {
		t.Fatalf("AddHostToService(internal): %v", err)
	}

	// Default catch-all host file -- must be excluded from the listing.
	writeFile(t, filepath.Join(avail, "000_wor_default.conf"), "# default")

	// api.example.com: enabled + ssl.
	writeFile(t, filepath.Join(avail, "wor__api.example.com.conf"), "# api")
	writeFile(t, filepath.Join(enabled, "wor__api.example.com.conf"), "# api")
	if err := ssl.WriteState(app.Cfg.SSL, "api.example.com", "letsencrypt", "/cert", "/key", "enabled"); err != nil {
		t.Fatalf("ssl.WriteState: %v", err)
	}

	// internal.local: enabled, no ssl.
	writeFile(t, filepath.Join(avail, "wor__internal.local.conf"), "# internal")
	writeFile(t, filepath.Join(enabled, "wor__internal.local.conf"), "# internal")

	// orphan.example.com: available but NOT enabled, and not registered
	// to any service (exercises the "-" target fallback).
	writeFile(t, filepath.Join(avail, "wor__orphan.example.com.conf"), "# orphan")

	if err := app.printHostList(mustProvider(t, app)); err != nil {
		t.Fatalf("printHostList: %v", err)
	}

	out := app.Out.(*bytes.Buffer).String()

	// The old ENABLED/DISABLED group headers are gone -- per-row
	// [on]/[off] marks (plain-text fallback of the blue check / red
	// cross; Out is a buffer, so no color) carry that state now, under
	// a single "WOR Hosts <server>" title.
	if !strings.Contains(out, "WOR Hosts nginx") {
		t.Errorf("expected 'WOR Hosts nginx' title, got:\n%s", out)
	}
	if strings.Contains(out, "ENABLED") || strings.Contains(out, "DISABLED") {
		t.Errorf("group headers must be gone:\n%s", out)
	}
	if strings.Contains(out, "000_wor_default") {
		t.Error("default catch-all host should not be listed")
	}

	// Enabled rows sort before disabled ones.
	apiIdx := strings.Index(out, "api.example.com")
	orphanIdx := strings.Index(out, "orphan.example.com")
	if apiIdx == -1 || orphanIdx == -1 || apiIdx > orphanIdx {
		t.Fatalf("expected enabled api.example.com before disabled orphan.example.com:\n%s", out)
	}

	for _, want := range []string{
		"[on]", "[off]",
		"shop-example-com/api-gateway", ":8080",
		"internal.local",
		"-> -", // unresolved orphan host's target fallback
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q, got:\n%s", want, out)
		}
	}

	// The ssl marker is plain text now ("ssl" / "-"): having a cert on
	// record is config state, not proof the cert works, so it gets no
	// green. The api row has ssl; internal.local must not.
	apiLine, internalLine := "", ""
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "api.example.com") {
			apiLine = line
		}
		if strings.Contains(line, "internal.local") {
			internalLine = line
		}
	}
	if !strings.Contains(apiLine, "ssl") {
		t.Errorf("expected plain ssl marker on api row: %q", apiLine)
	}
	if strings.Contains(internalLine, "ssl") {
		t.Errorf("internal.local must have no ssl marker: %q", internalLine)
	}
}

func TestPrintHostListEmpty(t *testing.T) {
	app, _, _ := newTestHostListApp(t)
	if err := app.printHostList(mustProvider(t, app)); err != nil {
		t.Fatalf("printHostList: %v", err)
	}
	out := app.Out.(*bytes.Buffer).String()
	if !strings.Contains(out, "No sites found") {
		t.Errorf("expected 'No sites found' for an empty sites-available dir, got:\n%s", out)
	}
}

func mustProvider(t *testing.T, app *App) *hostprovider.Provider {
	t.Helper()
	p, err := app.Provider()
	if err != nil {
		t.Fatalf("Provider(): %v", err)
	}
	return p
}
