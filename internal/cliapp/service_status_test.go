package cliapp

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"wor/internal/config"
	"wor/internal/domainmodel"
)

func newTestServiceStatusApp(t *testing.T) *App {
	t.Helper()
	// pm2.List() (invoked whenever a node/pm2 service is present) creates
	// PM2_HOME if missing; point it at a throwaway dir so the test never
	// touches the real ~/.pm2.
	t.Setenv("PM2_HOME", filepath.Join(t.TempDir(), "pm2home"))

	store := domainmodel.NewStore(t.TempDir())
	return &App{
		Cfg:   &config.Config{},
		Store: store,
		Out:   &bytes.Buffer{},
		Err:   &bytes.Buffer{},
	}
}

func TestCmdServiceStatusGroupsAndMarksDisabled(t *testing.T) {
	app := newTestServiceStatusApp(t)

	if err := app.Store.MakeDomainFiles("shop-example-com"); err != nil {
		t.Fatalf("MakeDomainFiles: %v", err)
	}
	if err := app.Store.AddService("shop-example-com", "webapp", "", 3000, "node", ""); err != nil {
		t.Fatalf("AddService(webapp): %v", err)
	}
	if err := app.Store.AddService("shop-example-com", "cms", "", 0, "php", ""); err != nil {
		t.Fatalf("AddService(cms): %v", err)
	}
	if err := app.Store.AddService("shop-example-com", "landing", "", 0, "static", ""); err != nil {
		t.Fatalf("AddService(landing): %v", err)
	}
	if err := app.Store.AddService("shop-example-com", "archived", "", 0, "static", ""); err != nil {
		t.Fatalf("AddService(archived): %v", err)
	}

	// Disable "archived" directly on disk -- disabled services must
	// still be LISTED (owner decision: hiding them made "why did my
	// service disappear" a recurring confusion), marked with the
	// [off] cross and a "disabled" state, but never queried.
	cfg, err := app.Store.LoadServices("shop-example-com")
	if err != nil {
		t.Fatalf("LoadServices: %v", err)
	}
	svc := cfg.FindService("archived")
	if svc == nil {
		t.Fatal("archived service not found")
	}
	svc.Enabled = false
	if err := app.Store.SaveServices(cfg); err != nil {
		t.Fatalf("SaveServices: %v", err)
	}

	if err := app.cmdServiceStatus(); err != nil {
		t.Fatalf("cmdServiceStatus: %v", err)
	}

	out := app.Out.(*bytes.Buffer).String()

	for _, want := range []string{
		"PM2 (node)", "PHP-FPM (php)", "STATIC (no process)",
		"shop-example-com/webapp", "shop-example-com/cms", "shop-example-com/landing",
		":3000", "n/a",
		// The disabled service: listed with the [off] mark (plain-text
		// fallback of the red cross; Out is a buffer, so no color) and
		// a "disabled" state.
		"archived", "[off]", "disabled",
		// Enabled rows carry the [on] mark (plain-text fallback of the
		// blue check -- config state, deliberately not a green health dot).
		"[on]",
		// The pm2/systemd sub-line: process name plus cpu/mem. pm2 isn't
		// installed in the test environment, so pm2.List() fails and the
		// row falls back to "not started" with "-" placeholders -- but
		// the sub-line (with the wor_<domain>_<service> name) must still
		// render regardless of whether pm2 itself is reachable.
		"wor_shop-example-com_webapp", "cpu -", "mem -",
		// The closing pointer to the real health check.
		"wor health",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q, got:\n%s", want, out)
		}
	}
	// php/static services have no supervised process, so they must not
	// get a name/cpu/mem sub-line.
	if strings.Contains(out, "wor_shop-example-com_cms") || strings.Contains(out, "wor_shop-example-com_landing") {
		t.Errorf("php/static rows must not have a process-name sub-line:\n%s", out)
	}
}

func TestCmdServiceStatusNoServices(t *testing.T) {
	app := newTestServiceStatusApp(t)
	if err := app.cmdServiceStatus(); err != nil {
		t.Fatalf("cmdServiceStatus: %v", err)
	}
	out := app.Out.(*bytes.Buffer).String()
	if !strings.Contains(out, "No services found") {
		t.Errorf("expected 'No services found', got:\n%s", out)
	}
}
