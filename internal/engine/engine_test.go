package engine

import (
	"path/filepath"
	"testing"

	"github.com/team-worapong/wor/internal/config"
	"github.com/team-worapong/wor/internal/platform"
)

func TestEnvironmentReportIsStructuredUseCaseData(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	app, err := New(
		config.Env{
			config.EnvConfig:   filepath.Join(dir, "missing.json"),
			config.EnvHome:     filepath.Join(dir, "home"),
			config.EnvDataDir:  filepath.Join(dir, "data"),
			config.EnvCacheDir: filepath.Join(dir, "cache"),
			config.EnvDebug:    "true",
		},
		platform.Current(),
		Options{AppName: "wor"},
	)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	report := app.Environment()
	if report.Config.WORHome != filepath.Join(dir, "home") {
		t.Fatalf("WORHome = %q", report.Config.WORHome)
	}
	if !report.Config.Debug {
		t.Fatal("Debug = false")
	}
	if report.Runtime.Version == "" {
		t.Fatal("Runtime.Version is empty")
	}
	if len(report.Environment) != len(config.EnvironmentVariables()) {
		t.Fatalf("Environment length = %d", len(report.Environment))
	}
}

func TestHelpReportComesFromEngine(t *testing.T) {
	t.Parallel()

	app, err := New(
		config.Env{config.EnvConfig: filepath.Join(t.TempDir(), "missing.json")},
		platform.Current(),
		Options{AppName: "wor"},
	)
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}

	help := app.Help()
	if help.Usage != "wor <command>" {
		t.Fatalf("Usage = %q", help.Usage)
	}
	if len(help.Commands) != 7 {
		t.Fatalf("Commands length = %d", len(help.Commands))
	}
}
