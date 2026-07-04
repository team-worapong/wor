package setup

import (
	"context"
	"os"

	"github.com/team-worapong/wor/internal/config"
	"github.com/team-worapong/wor/internal/platform"
	worRuntime "github.com/team-worapong/wor/internal/runtime"
)

const (
	EnvironmentProduction  = "production"
	EnvironmentDevelopment = "development"

	WebServerNginx  = "nginx"
	WebServerApache = "apache"
	WebServerSkip   = "skip"

	SSLProviderSelfSigned  = "self-signed"
	SSLProviderLetsEncrypt = "letsencrypt"
	SSLProviderNone        = "none"
)

type Request struct {
	DryRun bool
}

type Result struct {
	Summary   Summary
	Applied   bool
	Cancelled bool
	DryRun    bool
}

type Summary struct {
	Environment       string
	WORHome           string
	WebServerProvider string
	SSLProvider       string
	ConfigFile        string
	Directories       []string
	Detections        []Detection
	DryRun            bool
	ExistingConfig    bool
}

type Detection struct {
	Name      string
	Command   string
	Found     bool
	Supported bool
	Path      string
	Version   string
	Status    string
	Message   string
}

type Option struct {
	Value       string
	Label       string
	Description string
	Aliases     []string
	Default     bool
}

type Interactor interface {
	Select(prompt string, options []Option, defaultValue string) (string, error)
	Input(prompt, defaultValue string) (string, error)
	Confirm(prompt string, defaultYes bool) (bool, error)
	ShowDetections(title string, detections []Detection)
	ShowSummary(summary Summary)
	Info(message string)
	Warning(message string)
}

type FileSystem interface {
	Exists(path string) (bool, error)
	WriteConfig(path string, cfg config.Config) error
	MkdirAll(path string) error
}

type OSFileSystem struct{}

func (OSFileSystem) Exists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (OSFileSystem) WriteConfig(path string, cfg config.Config) error {
	return config.WriteFile(path, cfg)
}

func (OSFileSystem) MkdirAll(path string) error {
	return os.MkdirAll(path, 0o755)
}

type Service struct {
	system  platform.System
	config  config.Config
	fs      FileSystem
	checker checker
}

func New(system platform.System, cfg config.Config, fs FileSystem) Service {
	if fs == nil {
		fs = OSFileSystem{}
	}
	return Service{
		system:  system,
		config:  cfg,
		fs:      fs,
		checker: worRuntime.NewChecker(system),
	}
}

type checker interface {
	Check(ctx context.Context, target worRuntime.Target) worRuntime.CheckResult
	CheckAll(ctx context.Context, targets []worRuntime.Target) []worRuntime.CheckResult
}
