package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/team-worapong/wor/internal/paths"
)

const (
	EnvConfig   = "WOR_CONFIG"
	EnvHome     = "WOR_HOME"
	EnvDataDir  = "WOR_DATA_DIR"
	EnvCacheDir = "WOR_CACHE_DIR"
	EnvOutput   = "WOR_OUTPUT"
	EnvDebug    = "WOR_DEBUG"
)

// Config is the effective read-only configuration used by WOR.
type Config struct {
	ConfigFile   string
	HomeDir      string
	DataDir      string
	CacheDir     string
	OutputFormat string
	Debug        bool
}

type fileConfig struct {
	HomeDir      string `json:"home_dir"`
	DataDir      string `json:"data_dir"`
	CacheDir     string `json:"cache_dir"`
	OutputFormat string `json:"output_format"`
	Debug        *bool  `json:"debug"`
}

// Loader combines defaults, an optional user config file, and environment
// variables. Later sources override earlier ones.
type Loader struct {
	env   Env
	paths paths.Paths
}

func NewLoader(env Env, resolvedPaths paths.Paths) Loader {
	return Loader{
		env:   env,
		paths: resolvedPaths,
	}
}

func (l Loader) Load() (Config, error) {
	cfg := Config{
		ConfigFile:   l.paths.ConfigFile,
		HomeDir:      l.paths.HomeDir,
		DataDir:      l.paths.DataDir,
		CacheDir:     l.paths.CacheDir,
		OutputFormat: "text",
		Debug:        false,
	}

	if value := strings.TrimSpace(l.env.Get(EnvConfig)); value != "" {
		cfg.ConfigFile = filepath.Clean(value)
	}

	if err := applyFile(&cfg, cfg.ConfigFile); err != nil {
		return Config{}, err
	}

	if value := strings.TrimSpace(l.env.Get(EnvHome)); value != "" {
		cfg.HomeDir = filepath.Clean(value)
	}
	if value := strings.TrimSpace(l.env.Get(EnvDataDir)); value != "" {
		cfg.DataDir = filepath.Clean(value)
	}
	if value := strings.TrimSpace(l.env.Get(EnvCacheDir)); value != "" {
		cfg.CacheDir = filepath.Clean(value)
	}
	if value := strings.TrimSpace(l.env.Get(EnvOutput)); value != "" {
		cfg.OutputFormat = strings.ToLower(value)
	}
	if value := strings.TrimSpace(l.env.Get(EnvDebug)); value != "" {
		debug, err := parseBool(value)
		if err != nil {
			return Config{}, fmt.Errorf("%s: %w", EnvDebug, err)
		}
		cfg.Debug = debug
	}

	if cfg.OutputFormat == "" {
		cfg.OutputFormat = "text"
	}
	cfg.OutputFormat = strings.ToLower(strings.TrimSpace(cfg.OutputFormat))
	if cfg.OutputFormat != "text" {
		return Config{}, fmt.Errorf("output_format %q is not supported in phase 1", cfg.OutputFormat)
	}

	return cfg, nil
}

func applyFile(cfg *Config, path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read config file %s: %w", path, err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil
	}

	var file fileConfig
	if err := json.Unmarshal(data, &file); err != nil {
		return fmt.Errorf("parse config file %s: %w", path, err)
	}

	if strings.TrimSpace(file.HomeDir) != "" {
		cfg.HomeDir = filepath.Clean(file.HomeDir)
	}
	if strings.TrimSpace(file.DataDir) != "" {
		cfg.DataDir = filepath.Clean(file.DataDir)
	}
	if strings.TrimSpace(file.CacheDir) != "" {
		cfg.CacheDir = filepath.Clean(file.CacheDir)
	}
	if strings.TrimSpace(file.OutputFormat) != "" {
		cfg.OutputFormat = strings.ToLower(strings.TrimSpace(file.OutputFormat))
	}
	if file.Debug != nil {
		cfg.Debug = *file.Debug
	}

	return nil
}

func parseBool(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "y", "on":
		return true, nil
	case "0", "false", "no", "n", "off":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean value %q", value)
	}
}
