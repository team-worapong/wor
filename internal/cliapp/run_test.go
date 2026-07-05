package cliapp

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"wor/internal/config"
	"wor/internal/domainmodel"
)

func newTestRunApp(t *testing.T) *App {
	t.Helper()
	// Anything that shells out to pm2 (pm2.List/pm2.Run/pm2.Save) creates
	// PM2_HOME if missing -- point it at a throwaway dir so a test never
	// touches the real ~/.pm2, matching newTestServiceStatusApp's setup.
	t.Setenv("PM2_HOME", filepath.Join(t.TempDir(), "pm2home"))

	store := domainmodel.NewStore(t.TempDir())
	return &App{
		// No HostProvider configured -- HostProviderName() defaults to
		// "nginx", but with no nginx binary on the test machine's PATH,
		// provider.Binary() reports not-found and cmdRun's web-server
		// check is a no-op, so tests never shell out to a real nginx.
		Cfg:   &config.Config{},
		Store: store,
		Out:   &bytes.Buffer{},
		Err:   &bytes.Buffer{},
	}
}

func TestCmdRunNoEnabledServices(t *testing.T) {
	app := newTestRunApp(t)
	if err := app.cmdRun(nil); err != nil {
		t.Fatalf("cmdRun: %v", err)
	}
	out := app.Out.(*bytes.Buffer).String()
	if !strings.Contains(out, "No enabled services found") {
		t.Errorf("expected 'No enabled services found', got:\n%s", out)
	}
}

func TestCmdRunStaticServiceOK(t *testing.T) {
	app := newTestRunApp(t)
	if err := app.Store.MakeDomainFiles("shop.example.com"); err != nil {
		t.Fatalf("MakeDomainFiles: %v", err)
	}
	if err := app.Store.AddService("shop.example.com", "landing", "", 0, "static", ""); err != nil {
		t.Fatalf("AddService(landing): %v", err)
	}

	if err := app.cmdRun(nil); err != nil {
		t.Fatalf("cmdRun: %v", err)
	}
	out := app.Out.(*bytes.Buffer).String()
	if !strings.Contains(out, "shop.example.com/landing") {
		t.Errorf("expected landing service in output, got:\n%s", out)
	}
	if !strings.Contains(out, "1/1 services running (0 failed)") {
		t.Errorf("expected a 1/1 success summary, got:\n%s", out)
	}
}

func TestCmdRunSkipsDisabledServices(t *testing.T) {
	app := newTestRunApp(t)
	if err := app.Store.MakeDomainFiles("shop.example.com"); err != nil {
		t.Fatalf("MakeDomainFiles: %v", err)
	}
	if err := app.Store.AddService("shop.example.com", "landing", "", 0, "static", ""); err != nil {
		t.Fatalf("AddService(landing): %v", err)
	}
	cfg, err := app.Store.LoadServices("shop.example.com")
	if err != nil {
		t.Fatalf("LoadServices: %v", err)
	}
	svc := cfg.FindService("landing")
	svc.Enabled = false
	if err := app.Store.SaveServices(cfg); err != nil {
		t.Fatalf("SaveServices: %v", err)
	}

	if err := app.cmdRun(nil); err != nil {
		t.Fatalf("cmdRun: %v", err)
	}
	out := app.Out.(*bytes.Buffer).String()
	if !strings.Contains(out, "No enabled services found") {
		t.Errorf("expected 'No enabled services found' when the only service is disabled, got:\n%s", out)
	}
}

// runService's per-service php-fpm branch only touches svc's own fields
// (PoolFilePath/ResolveVersion), so it can be exercised directly without
// registering the service in the Store first.
func TestRunServicePHPPoolVersionNotDetected(t *testing.T) {
	app := newTestRunApp(t)
	svc := domainmodel.Service{Name: "cms", Enabled: true, Type: "php", PHPVersion: "8.3"}

	err := app.runService("shop.example.com", svc)
	if err == nil {
		t.Fatal("expected an error when the recorded PHP version isn't detected on this host")
	}
	if !strings.Contains(err.Error(), "no longer detected") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestRunServiceLegacyPHPIsNoop(t *testing.T) {
	app := newTestRunApp(t)
	// No PHPVersion recorded -- the legacy host-wide PHP_FPM_ENDPOINT
	// path, which wor never manages the lifecycle of.
	svc := domainmodel.Service{Name: "cms", Enabled: true, Type: "php"}

	if err := app.runService("shop.example.com", svc); err != nil {
		t.Fatalf("legacy php service should be a no-op, got: %v", err)
	}
}

func TestExtractSudoCommand(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "finds sudo line among pm2's other output",
			in: "[PM2] Init System found: systemd\n" +
				"[PM2] To setup the Startup Script, copy/paste the following command:\n" +
				"sudo env PATH=$PATH:/usr/bin /usr/lib/node_modules/pm2/bin/pm2 startup systemd -u team --hp /home/team\n",
			want: "sudo env PATH=$PATH:/usr/bin /usr/lib/node_modules/pm2/bin/pm2 startup systemd -u team --hp /home/team",
		},
		{
			name: "no sudo line present",
			in:   "[PM2] Init System found: systemd\nalready configured\n",
			want: "",
		},
		{
			name: "empty input",
			in:   "",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractSudoCommand(tt.in); got != tt.want {
				t.Errorf("extractSudoCommand(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
