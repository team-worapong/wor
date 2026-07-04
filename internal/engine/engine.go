package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/team-worapong/wor/internal/config"
	"github.com/team-worapong/wor/internal/doctor"
	"github.com/team-worapong/wor/internal/domain"
	"github.com/team-worapong/wor/internal/platform"
	worRuntime "github.com/team-worapong/wor/internal/runtime"
	"github.com/team-worapong/wor/internal/service"
	"github.com/team-worapong/wor/internal/setup"
	"github.com/team-worapong/wor/internal/version"
)

type Options struct {
	AppName string
}

type Engine struct {
	appName string
	env     config.Env
	system  platform.System
	config  config.Config
}

func New(env config.Env, system platform.System, options Options) (Engine, error) {
	appName := strings.TrimSpace(options.AppName)
	if appName == "" {
		appName = "wor"
	}

	cfg, err := config.LoadWithOptions(system, config.LoadOptions{
		AppName: appName,
		Env:     env,
	})
	if err != nil {
		return Engine{}, fmt.Errorf("configuration: %w", err)
	}

	return Engine{
		appName: appName,
		env:     env,
		system:  system,
		config:  cfg,
	}, nil
}

func (e Engine) Version() VersionReport {
	return newVersionReport(version.Current())
}

func (e Engine) Help() HelpReport {
	return HelpReport{
		Title: "WOR - Runtime Manager for Web Applications",
		Usage: e.appName + " <command>",
		Commands: []CommandHelp{
			{Name: "version", Description: "Show version information"},
			{Name: "help", Description: "Show help"},
			{Name: "env", Description: "Show effective environment and configuration"},
			{Name: "doctor", Description: "Inspect local runtime prerequisites"},
			{Name: "setup", Description: "Run the re-runnable setup wizard"},
			{Name: "domain add", Description: "Register a domain in WOR_HOME"},
			{Name: "service add", Description: "Create a service foundation for a registered domain"},
		},
	}
}

func (e Engine) Environment() EnvironmentReport {
	return EnvironmentReport{
		Runtime: RuntimeReport{
			Version:   e.Version().String(),
			OS:        e.system.OS(),
			Arch:      e.system.Arch(),
			Supported: e.system.IsSupported(),
		},
		Config:      e.config,
		Environment: e.environmentVariables(),
	}
}

func (e Engine) Doctor(ctx context.Context) (doctor.Report, error) {
	return doctor.New(e.system).Run(ctx)
}

func (e Engine) Setup(ctx context.Context, request setup.Request, interactor setup.Interactor) (setup.Result, error) {
	return setup.New(e.system, e.config, nil).Run(ctx, request, interactor)
}

func (e Engine) DomainAdd(request domain.AddRequest) (domain.Metadata, error) {
	return domain.NewManager(e.config).Add(request)
}

func (e Engine) ServiceAdd(ctx context.Context, request service.AddRequest) (service.Metadata, error) {
	return service.NewManager(e.config, worRuntime.NewChecker(e.system)).Add(ctx, request)
}

func (e Engine) environmentVariables() []EnvironmentVariable {
	keys := config.EnvironmentVariables()
	variables := make([]EnvironmentVariable, 0, len(keys))
	for _, key := range keys {
		value := strings.TrimSpace(e.env.Get(key))
		variables = append(variables, EnvironmentVariable{
			Name:  key,
			Value: value,
			Set:   value != "",
		})
	}
	return variables
}
