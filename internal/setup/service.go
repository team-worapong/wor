package setup

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/team-worapong/wor/internal/config"
)

func (s Service) Run(ctx context.Context, request Request, interactor Interactor) (Result, error) {
	if interactor == nil {
		return Result{}, errors.New("setup interactor is required")
	}

	loadedConfigExists, err := s.fs.Exists(s.config.ConfigFile)
	if err != nil {
		return Result{}, fmt.Errorf("inspect config file: %w", err)
	}

	detections := s.detectCommon(ctx)

	environment, err := s.selectEnvironment(interactor, loadedConfigExists)
	if err != nil {
		return Result{}, err
	}

	worHome, err := s.selectWORHome(interactor, environment, loadedConfigExists)
	if err != nil {
		return Result{}, err
	}

	webServerProvider, err := s.selectWebServerProvider(interactor, webServerDetections(detections), loadedConfigExists)
	if err != nil {
		return Result{}, err
	}

	sslProvider, err := s.selectSSLProvider(interactor, findDetection(detections, "Certbot"), loadedConfigExists)
	if err != nil {
		return Result{}, err
	}

	layout := config.LayoutForHome(worHome)
	summary := Summary{
		Environment:       environment,
		WORHome:           worHome,
		WebServerProvider: webServerProvider,
		SSLProvider:       sslProvider,
		ConfigFile:        s.config.ConfigFile,
		Directories:       layout.Directories(),
		Detections:        detections,
		DryRun:            request.DryRun,
		ExistingConfig:    loadedConfigExists,
	}

	interactor.ShowSummary(summary)
	if request.DryRun {
		interactor.Info("Dry run: no config file will be written and no directories will be created.")
		return Result{Summary: summary, DryRun: true}, nil
	}

	confirmed, err := interactor.Confirm("Proceed with setup?", true)
	if err != nil {
		return Result{}, err
	}
	if !confirmed {
		interactor.Info("Setup cancelled. No config file was written and no directories were created.")
		return Result{Summary: summary, Cancelled: true}, nil
	}

	if err := s.apply(summary); err != nil {
		return Result{}, err
	}

	interactor.Info("Setup completed.")
	return Result{Summary: summary, Applied: true}, nil
}

func (s Service) selectEnvironment(interactor Interactor, existingConfig bool) (string, error) {
	defaultValue := s.system.DefaultSetupEnvironment()
	if existingConfig && validEnvironment(s.config.Environment) {
		defaultValue = s.config.Environment
	}

	return interactor.Select("Select Environment:", []Option{
		{Value: EnvironmentProduction, Label: "Production"},
		{Value: EnvironmentDevelopment, Label: "Development"},
	}, defaultValue)
}

func (s Service) selectWORHome(interactor Interactor, environment string, existingConfig bool) (string, error) {
	defaultValue, err := s.system.DefaultWORHome(environment)
	if err != nil {
		return "", fmt.Errorf("resolve default WOR_HOME: %w", err)
	}
	if existingConfig && strings.TrimSpace(s.config.WORHome) != "" {
		defaultValue = s.config.WORHome
	}

	value, err := interactor.Input("WOR_HOME", defaultValue)
	if err != nil {
		return "", err
	}
	return s.cleanHomePath(value, defaultValue)
}

func (s Service) selectWebServerProvider(interactor Interactor, detections []Detection, existingConfig bool) (string, error) {
	interactor.ShowDetections("Detected Web Server Providers:", detections)

	defaultValue := WebServerSkip
	if existingConfig && validWebServerProvider(s.config.WebServerProvider) {
		defaultValue = s.config.WebServerProvider
	}

	for {
		choice, err := interactor.Select("Select Web Server Provider:", []Option{
			{Value: WebServerNginx, Label: "nginx"},
			{Value: WebServerApache, Label: "apache"},
			{Value: WebServerSkip, Label: "skip"},
		}, defaultValue)
		if err != nil {
			return "", err
		}
		if choice == WebServerSkip || providerFound(detections, choice) {
			return choice, nil
		}

		interactor.Warning(fmt.Sprintf("%s is not installed. Please choose another provider or skip.", providerDisplayName(choice)))
		next, err := interactor.Select("Choose next action:", []Option{
			{Value: "retry", Label: "choose again"},
			{Value: WebServerSkip, Label: "skip"},
		}, WebServerSkip)
		if err != nil {
			return "", err
		}
		if next == WebServerSkip {
			return WebServerSkip, nil
		}
	}
}

func (s Service) selectSSLProvider(interactor Interactor, certbot Detection, existingConfig bool) (string, error) {
	defaultValue := SSLProviderNone
	if existingConfig && validSSLProvider(s.config.SSLProvider) {
		defaultValue = s.config.SSLProvider
	}

	for {
		choice, err := interactor.Select("Select SSL Provider:", []Option{
			{Value: SSLProviderSelfSigned, Label: "self-signed"},
			{Value: SSLProviderLetsEncrypt, Label: "letsencrypt"},
			{Value: SSLProviderNone, Label: "none/skip", Aliases: []string{"skip"}},
		}, defaultValue)
		if err != nil {
			return "", err
		}
		if choice != SSLProviderLetsEncrypt || certbot.Found {
			return choice, nil
		}

		interactor.Warning("certbot was not found. WOR will not install certbot.")
		next, err := interactor.Select("Choose next action:", []Option{
			{Value: "retry", Label: "choose again"},
			{Value: SSLProviderNone, Label: "skip"},
		}, SSLProviderNone)
		if err != nil {
			return "", err
		}
		if next == SSLProviderNone {
			return SSLProviderNone, nil
		}
	}
}

func (s Service) apply(summary Summary) error {
	for _, dir := range summary.Directories {
		if err := s.fs.MkdirAll(dir); err != nil {
			return fmt.Errorf("create directory %s: %w", dir, err)
		}
	}

	cfg := s.configForSummary(summary)
	if err := s.fs.WriteConfig(summary.ConfigFile, cfg); err != nil {
		return err
	}
	return nil
}

func (s Service) configForSummary(summary Summary) config.Config {
	cfg := s.config
	layout := config.LayoutForHome(summary.WORHome)
	cfg.ConfigFile = summary.ConfigFile
	cfg.Environment = summary.Environment
	cfg.WORHome = summary.WORHome
	cfg.DataDir = layout.Data
	cfg.CacheDir = layout.Cache
	cfg.WebServerProvider = summary.WebServerProvider
	cfg.SSLProvider = summary.SSLProvider
	cfg.RuntimeDetections = configDetections(summary.Detections)
	if strings.TrimSpace(cfg.OutputFormat) == "" {
		cfg.OutputFormat = "text"
	}
	return cfg
}

func (s Service) cleanHomePath(value, fallback string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = fallback
	}
	if value == "~" || strings.HasPrefix(value, "~/") {
		home, err := s.system.UserHomeDir()
		if err != nil {
			return "", err
		}
		if value == "~" {
			return home, nil
		}
		value = filepath.Join(home, strings.TrimPrefix(value, "~/"))
	}
	return filepath.Clean(value), nil
}

func providerFound(detections []Detection, provider string) bool {
	switch provider {
	case WebServerNginx:
		return findDetection(detections, "Nginx").Found
	case WebServerApache:
		return findDetection(detections, "Apache").Found
	default:
		return false
	}
}

func providerDisplayName(provider string) string {
	switch provider {
	case WebServerNginx:
		return "Nginx"
	case WebServerApache:
		return "Apache"
	default:
		return provider
	}
}

func validEnvironment(value string) bool {
	return value == EnvironmentProduction || value == EnvironmentDevelopment
}

func validWebServerProvider(value string) bool {
	return value == WebServerNginx || value == WebServerApache || value == WebServerSkip
}

func validSSLProvider(value string) bool {
	return value == SSLProviderSelfSigned || value == SSLProviderLetsEncrypt || value == SSLProviderNone
}

func SetupDirectories(worHome string) []string {
	return config.LayoutForHome(worHome).Directories()
}

func configDetections(detections []Detection) []config.RuntimeDetection {
	items := make([]config.RuntimeDetection, 0, len(detections))
	for _, detection := range detections {
		items = append(items, config.RuntimeDetection{
			Name:    detection.Name,
			Command: detection.Command,
			Found:   detection.Found,
			Path:    detection.Path,
			Version: detection.Version,
			Status:  detection.Status,
			Message: detection.Message,
		})
	}
	return items
}
