package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/team-worapong/wor/internal/paths"
)

func TestLoaderAppliesDefaultsFileThenEnvironment(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.json")
	if err := os.WriteFile(configFile, []byte(`{
		"home_dir": "/file/home",
		"data_dir": "/file/data",
		"cache_dir": "/file/cache",
		"debug": true
	}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	resolved := paths.Paths{
		ConfigFile: configFile,
		HomeDir:    "/default/home",
		DataDir:    "/default/data",
		CacheDir:   "/default/cache",
	}
	env := Env{
		EnvHome:    "/env/home",
		EnvDataDir: "/env/data",
		EnvDebug:   "false",
	}

	cfg, err := NewLoader(env, resolved).Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.HomeDir != filepath.Clean("/env/home") {
		t.Fatalf("HomeDir = %q", cfg.HomeDir)
	}
	if cfg.DataDir != filepath.Clean("/env/data") {
		t.Fatalf("DataDir = %q", cfg.DataDir)
	}
	if cfg.CacheDir != filepath.Clean("/file/cache") {
		t.Fatalf("CacheDir = %q", cfg.CacheDir)
	}
	if cfg.Debug {
		t.Fatal("Debug = true")
	}
}

func TestLoaderRejectsUnsupportedOutputFormat(t *testing.T) {
	t.Parallel()

	_, err := NewLoader(Env{EnvOutput: "json"}, paths.Paths{}).Load()
	if err == nil {
		t.Fatal("expected unsupported output format error")
	}
}
