package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/team-worapong/wor/internal/paths"
)

func TestLoadWithOptionsAppliesExplicitEnvironmentFileThenDefaults(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.json")
	if err := os.WriteFile(configFile, []byte(`{
		"environment": "production",
		"wor_home": "/file/home",
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
		EnvEnvironment: "development",
		EnvHome:        "/env/home",
		EnvDataDir:     "/env/data",
		EnvDebug:       "false",
	}
	debug := true

	cfg, err := LoadWithOptions(nil, LoadOptions{
		Env:   env,
		Paths: resolved,
		Explicit: ExplicitOptions{
			WORHome:  "/explicit/home",
			CacheDir: "/explicit/cache",
			Debug:    &debug,
		},
	})
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.WORHome != filepath.Clean("/explicit/home") {
		t.Fatalf("WORHome = %q", cfg.WORHome)
	}
	if cfg.Environment != "development" {
		t.Fatalf("Environment = %q", cfg.Environment)
	}
	if cfg.DataDir != filepath.Clean("/env/data") {
		t.Fatalf("DataDir = %q", cfg.DataDir)
	}
	if cfg.CacheDir != filepath.Clean("/explicit/cache") {
		t.Fatalf("CacheDir = %q", cfg.CacheDir)
	}
	if !cfg.Debug {
		t.Fatal("Debug = false")
	}
}

func TestLoadWithOptionsUsesEnvConfigFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configFile := filepath.Join(dir, "selected.json")
	if err := os.WriteFile(configFile, []byte(`{
		"wor_home": "/file/home",
		"cache_dir": "/file/cache"
	}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadWithOptions(nil, LoadOptions{
		Env: Env{EnvConfig: configFile},
		Paths: paths.Paths{
			ConfigFile: filepath.Join(dir, "default.json"),
			HomeDir:    "/default/home",
			DataDir:    "/default/data",
			CacheDir:   "/default/cache",
		},
	})
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.ConfigFile != configFile {
		t.Fatalf("ConfigFile = %q", cfg.ConfigFile)
	}
	if cfg.WORHome != filepath.Clean("/file/home") {
		t.Fatalf("WORHome = %q", cfg.WORHome)
	}
	if cfg.CacheDir != filepath.Clean("/file/cache") {
		t.Fatalf("CacheDir = %q", cfg.CacheDir)
	}
}

func TestLoaderSupportsLegacyHomeDirField(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.json")
	if err := os.WriteFile(configFile, []byte(`{"home_dir": "/legacy/home"}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := NewLoader(nil, paths.Paths{
		ConfigFile: configFile,
		HomeDir:    "/default/home",
		DataDir:    "/default/data",
		CacheDir:   "/default/cache",
	}).Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.WORHome != filepath.Clean("/legacy/home") {
		t.Fatalf("WORHome = %q", cfg.WORHome)
	}
}

func TestDefaultsUseResolvedPaths(t *testing.T) {
	t.Parallel()

	resolved := paths.Paths{
		ConfigFile: "/default/config.json",
		HomeDir:    "/default/home",
		DataDir:    "/default/data",
		CacheDir:   "/default/cache",
	}

	cfg := Defaults(resolved)
	if cfg.ConfigFile != resolved.ConfigFile {
		t.Fatalf("ConfigFile = %q", cfg.ConfigFile)
	}
	if cfg.WORHome != resolved.HomeDir {
		t.Fatalf("WORHome = %q", cfg.WORHome)
	}
	if cfg.DataDir != resolved.DataDir {
		t.Fatalf("DataDir = %q", cfg.DataDir)
	}
	if cfg.CacheDir != resolved.CacheDir {
		t.Fatalf("CacheDir = %q", cfg.CacheDir)
	}
	if cfg.OutputFormat != "text" {
		t.Fatalf("OutputFormat = %q", cfg.OutputFormat)
	}
}

func TestWORHomeLayoutUsesCanonicalDirectories(t *testing.T) {
	t.Parallel()

	home := filepath.Join(t.TempDir(), "wor-home")
	layout := LayoutForHome(home)
	expected := []string{
		filepath.Join(home, "domains"),
		filepath.Join(home, "runtime"),
		filepath.Join(home, "templates"),
		filepath.Join(home, "logs"),
		filepath.Join(home, "ssl"),
		filepath.Join(home, "configs"),
		filepath.Join(home, "cache"),
		filepath.Join(home, "data"),
		filepath.Join(home, "backups"),
	}

	if got := layout.Directories(); !sameStrings(got, expected) {
		t.Fatalf("Directories = %#v", got)
	}
}

func TestLoaderRejectsUnsupportedOutputFormat(t *testing.T) {
	t.Parallel()

	_, err := NewLoader(Env{EnvOutput: "json"}, paths.Paths{}).Load()
	if err == nil {
		t.Fatal("expected unsupported output format error")
	}
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
