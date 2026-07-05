package cliapp

import (
	"bytes"
	"testing"

	"wor/internal/config"
	"wor/internal/domainmodel"
)

func newTestServiceApp(t *testing.T) *App {
	t.Helper()
	store := domainmodel.NewStore(t.TempDir())
	return &App{
		Cfg:   &config.Config{},
		Store: store,
		Out:   &bytes.Buffer{},
		Err:   &bytes.Buffer{},
	}
}

// TestCmdServiceStartStopRestartLogsErrorOnMissingService guards against
// the bug where a typo'd or never-created domain/service target (e.g.
// "com-moodasoft/app" when only "test-moodasoft/app" was ever
// registered) silently printed "[OK] ... served by host provider"
// instead of failing: Store.GetServiceType falls back to "static" for
// any service it can't find, which start/stop/restart/logs used to
// treat as a legitimate (if inert) static service.
func TestCmdServiceStartStopRestartLogsErrorOnMissingService(t *testing.T) {
	app := newTestServiceApp(t)

	for _, action := range []string{"start", "stop", "restart", "logs"} {
		err := app.cmdService([]string{action, "nope.example.com/app"})
		if err == nil {
			t.Errorf("%s on a nonexistent service: expected an error, got nil (the [OK]-on-typo bug)", action)
		}
	}
}

func TestRequireServiceExists(t *testing.T) {
	app := newTestServiceApp(t)
	if err := app.Store.MakeDomainFiles("shop-example-com"); err != nil {
		t.Fatalf("MakeDomainFiles: %v", err)
	}
	if err := app.Store.AddService("shop-example-com", "webapp", "", 3000, "node", ""); err != nil {
		t.Fatalf("AddService: %v", err)
	}

	if err := app.requireServiceExists("shop-example-com", "webapp"); err != nil {
		t.Errorf("requireServiceExists on a real service: unexpected error: %v", err)
	}
	if err := app.requireServiceExists("shop-example-com", "missing"); err == nil {
		t.Error("requireServiceExists on a registered domain but missing service: expected an error, got nil")
	}
	if err := app.requireServiceExists("nonexistent-domain.com", "app"); err == nil {
		t.Error("requireServiceExists on a nonexistent domain: expected an error, got nil")
	}
}
