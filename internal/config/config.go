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
	EnvConfig      = "WOR_CONFIG"
	EnvEnvironment = "WOR_ENVIRONMENT"
	EnvHome        = "WOR_HOME"
	EnvDataDir     = "WOR_DATA_DIR"
	EnvCacheDir    = "WOR_CACHE_DIR"
	EnvOutput      = "WOR_OUTPUT"
	EnvDebug       = "WOR_DEBUG"
)

// Config is the effective read-only configuration used by WOR.
type Config struct {
	ConfigFile        string
	Environment       string
	WORHome           string
	DataDir           string
	CacheDir          string
	OutputFormat      string
	Debug             bool
	WebServerProvider string
	SSLProvider       string
	RuntimeDetections []RuntimeDetection
}

type RuntimeDetection struct {
	Name    string `json:"name"`
	Command string `json:"command,omitempty"`
	Found   bool   `json:"found"`
	Path    string `json:"path,omitempty"`
	Version string `json:"version,omitempty"`
	Status  string `json:"status,omitempty"`
	Message string `json:"message,omitempty"`
}

type fileConfig struct {
	Environment       string             `json:"environment"`
	WORHome           string             `json:"wor_home"`
	HomeDir           string             `json:"home_dir,omitempty"`
	DataDir           string             `json:"data_dir"`
	CacheDir          string             `json:"cache_dir"`
	OutputFormat      string             `json:"output_format"`
	Debug             *bool              `json:"debug"`
	WebServerProvider string             `json:"web_server_provider"`
	SSLProvider       string             `json:"ssl_provider"`
	RuntimeDetections []RuntimeDetection `json:"runtime_detections"`
}

// Loader combines defaults, an optional user config file, and environment
// variables. Later sources override earlier ones.
type Loader struct {
	env   Env
	paths paths.Paths
}

type LoadOptions struct {
	AppName  string
	Env      Env
	Paths    paths.Paths
	Explicit ExplicitOptions
}

type ExplicitOptions struct {
	ConfigFile   string
	Environment  string
	WORHome      string
	DataDir      string
	CacheDir     string
	OutputFormat string
	Debug        *bool
}

func Load(system paths.Platform) (Config, error) {
	return LoadWithOptions(system, LoadOptions{})
}

func LoadWithOptions(system paths.Platform, options LoadOptions) (Config, error) {
	appName := strings.TrimSpace(options.AppName)
	if appName == "" {
		appName = "wor"
	}

	resolvedPaths := options.Paths
	if resolvedPaths.ConfigFile == "" && strings.TrimSpace(options.Explicit.ConfigFile) != "" {
		resolvedPaths.ConfigFile = filepath.Clean(options.Explicit.ConfigFile)
	}
	if resolvedPaths.ConfigFile == "" {
		if system == nil {
			return Config{}, errors.New("platform is required when paths are not provided")
		}
		var err error
		resolvedPaths, err = paths.NewResolver(system, appName).Resolve()
		if err != nil {
			return Config{}, err
		}
	}

	env := options.Env
	if env == nil {
		env = FromOSEnv()
	}

	return load(env, resolvedPaths, options.Explicit)
}

func Defaults(resolvedPaths paths.Paths) Config {
	return Config{
		ConfigFile:   resolvedPaths.ConfigFile,
		WORHome:      resolvedPaths.HomeDir,
		DataDir:      resolvedPaths.DataDir,
		CacheDir:     resolvedPaths.CacheDir,
		OutputFormat: "text",
		Debug:        false,
	}
}

func NewLoader(env Env, resolvedPaths paths.Paths) Loader {
	return Loader{
		env:   env,
		paths: resolvedPaths,
	}
}

func (l Loader) Load() (Config, error) {
	return load(l.env, l.paths, ExplicitOptions{})
}

func load(env Env, resolvedPaths paths.Paths, explicit ExplicitOptions) (Config, error) {
	cfg := Defaults(resolvedPaths)
	if value := strings.TrimSpace(env.Get(EnvConfig)); value != "" {
		cfg.ConfigFile = filepath.Clean(value)
	}
	if value := strings.TrimSpace(explicit.ConfigFile); value != "" {
		cfg.ConfigFile = filepath.Clean(value)
	}

	if err := applyFile(&cfg, cfg.ConfigFile); err != nil {
		return Config{}, err
	}

	if err := applyEnv(&cfg, env); err != nil {
		return Config{}, err
	}
	applyExplicit(&cfg, explicit)
	if err := validate(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func applyEnv(cfg *Config, env Env) error {
	if value := strings.TrimSpace(env.Get(EnvEnvironment)); value != "" {
		cfg.Environment = strings.ToLower(value)
	}
	if value := strings.TrimSpace(env.Get(EnvHome)); value != "" {
		cfg.WORHome = filepath.Clean(value)
	}
	if value := strings.TrimSpace(env.Get(EnvDataDir)); value != "" {
		cfg.DataDir = filepath.Clean(value)
	}
	if value := strings.TrimSpace(env.Get(EnvCacheDir)); value != "" {
		cfg.CacheDir = filepath.Clean(value)
	}
	if value := strings.TrimSpace(env.Get(EnvOutput)); value != "" {
		cfg.OutputFormat = strings.ToLower(value)
	}
	if value := strings.TrimSpace(env.Get(EnvDebug)); value != "" {
		debug, err := parseBool(value)
		if err != nil {
			return fmt.Errorf("%s: %w", EnvDebug, err)
		}
		cfg.Debug = debug
	}
	return nil
}

func applyExplicit(cfg *Config, explicit ExplicitOptions) {
	if value := strings.TrimSpace(explicit.ConfigFile); value != "" {
		cfg.ConfigFile = filepath.Clean(value)
	}
	if value := strings.TrimSpace(explicit.Environment); value != "" {
		cfg.Environment = strings.ToLower(value)
	}
	if value := strings.TrimSpace(explicit.WORHome); value != "" {
		cfg.WORHome = filepath.Clean(value)
	}
	if value := strings.TrimSpace(explicit.DataDir); value != "" {
		cfg.DataDir = filepath.Clean(value)
	}
	if value := strings.TrimSpace(explicit.CacheDir); value != "" {
		cfg.CacheDir = filepath.Clean(value)
	}
	if value := strings.TrimSpace(explicit.OutputFormat); value != "" {
		cfg.OutputFormat = strings.ToLower(value)
	}
	if explicit.Debug != nil {
		cfg.Debug = *explicit.Debug
	}
}

func validate(cfg *Config) error {
	if cfg.OutputFormat == "" {
		cfg.OutputFormat = "text"
	}
	cfg.OutputFormat = strings.ToLower(strings.TrimSpace(cfg.OutputFormat))
	if cfg.OutputFormat != "text" {
		return fmt.Errorf("output_format %q is not supported in phase 1", cfg.OutputFormat)
	}
	return nil
}

func WriteFile(path string, cfg Config) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("config file path is required")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	data, err := json.MarshalIndent(fileConfigFromConfig(cfg), "", "  ")
	if err != nil {
		return fmt.Errorf("encode config file: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write config file %s: %w", path, err)
	}
	return nil
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

	if strings.TrimSpace(file.Environment) != "" {
		cfg.Environment = strings.ToLower(strings.TrimSpace(file.Environment))
	}
	if strings.TrimSpace(file.WORHome) != "" {
		cfg.WORHome = filepath.Clean(file.WORHome)
	} else if strings.TrimSpace(file.HomeDir) != "" {
		cfg.WORHome = filepath.Clean(file.HomeDir)
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
	if strings.TrimSpace(file.WebServerProvider) != "" {
		cfg.WebServerProvider = strings.ToLower(strings.TrimSpace(file.WebServerProvider))
	}
	if strings.TrimSpace(file.SSLProvider) != "" {
		cfg.SSLProvider = strings.ToLower(strings.TrimSpace(file.SSLProvider))
	}
	if len(file.RuntimeDetections) > 0 {
		cfg.RuntimeDetections = file.RuntimeDetections
	}

	return nil
}

func fileConfigFromConfig(cfg Config) fileConfig {
	return fileConfig{
		Environment:       cfg.Environment,
		WORHome:           cfg.WORHome,
		DataDir:           cfg.DataDir,
		CacheDir:          cfg.CacheDir,
		OutputFormat:      cfg.OutputFormat,
		Debug:             boolPtr(cfg.Debug),
		WebServerProvider: cfg.WebServerProvider,
		SSLProvider:       cfg.SSLProvider,
		RuntimeDetections: cfg.RuntimeDetections,
	}
}

func boolPtr(value bool) *bool {
	return &value
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
