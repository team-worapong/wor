package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/team-worapong/wor/internal/config"
	"github.com/team-worapong/wor/internal/domain"
	"github.com/team-worapong/wor/internal/engine"
	"github.com/team-worapong/wor/internal/output"
	"github.com/team-worapong/wor/internal/platform"
	"github.com/team-worapong/wor/internal/service"
	"github.com/team-worapong/wor/internal/setup"
)

const appName = "wor"

func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	renderer := output.New(stdout, stderr, output.FormatText)

	app, err := engine.New(config.FromOSEnv(), platform.Current(), engine.Options{
		AppName: appName,
	})
	if err != nil {
		renderer.Error("%v", err)
		return 1
	}

	command := parseCommand(args)
	switch command {
	case "version":
		renderVersion(renderer, app.Version())
	case "help":
		renderHelp(renderer, app.Help())
	case "env":
		renderEnvironment(renderer, app.Environment())
	case "doctor":
		report, err := app.Doctor(ctx)
		if err != nil {
			renderer.Error("doctor: %v", err)
			return 1
		}
		renderDoctor(renderer, report)
	case "setup":
		request, err := parseSetupRequest(args[1:])
		if err != nil {
			renderer.Error("setup: %v", err)
			return 1
		}
		interactor := newTerminalInteractor(stdin, renderer)
		if _, err := app.Setup(ctx, request, interactor); err != nil {
			renderer.Error("setup: %v", err)
			return 1
		}
	case "domain":
		request, err := parseDomainAddRequest(args[1:])
		if err != nil {
			renderer.Error("domain: %v", err)
			return 1
		}
		metadata, err := app.DomainAdd(request)
		if err != nil {
			renderer.Error("domain: %v", err)
			return 1
		}
		renderDomainAdded(renderer, metadata)
	case "service":
		request, err := parseServiceAddRequest(args[1:])
		if err != nil {
			renderer.Error("service: %v", err)
			return 1
		}
		metadata, err := app.ServiceAdd(ctx, request)
		if err != nil {
			renderer.Error("service: %v", err)
			return 1
		}
		renderServiceAdded(renderer, metadata)
	default:
		renderer.Error("unknown command %q", command)
		renderer.Text("")
		renderHelp(renderer, app.Help())
		return 1
	}

	return 0
}

func parseCommand(args []string) string {
	if len(args) == 0 {
		return "help"
	}
	switch args[0] {
	case "-h", "--help":
		return "help"
	default:
		return args[0]
	}
}

func parseSetupRequest(args []string) (setup.Request, error) {
	var request setup.Request
	for _, arg := range args {
		switch strings.TrimSpace(arg) {
		case "":
			continue
		case "--dry-run":
			request.DryRun = true
		default:
			return setup.Request{}, fmt.Errorf("unknown flag %q", arg)
		}
	}
	return request, nil
}

func parseDomainAddRequest(args []string) (domain.AddRequest, error) {
	if len(args) != 2 || args[0] != "add" {
		return domain.AddRequest{}, errors.New("usage: wor domain add <domain>")
	}
	return domain.AddRequest{Domain: args[1]}, nil
}

func parseServiceAddRequest(args []string) (service.AddRequest, error) {
	if len(args) < 2 || args[0] != "add" {
		return service.AddRequest{}, errors.New("usage: wor service add <fqdn> [--template <name>] [--app-route <route>]")
	}

	var request service.AddRequest
	for index := 1; index < len(args); index++ {
		arg := strings.TrimSpace(args[index])
		switch {
		case arg == "":
			continue
		case arg == "--template":
			index++
			if index >= len(args) || strings.TrimSpace(args[index]) == "" {
				return service.AddRequest{}, errors.New("--template requires a value")
			}
			request.TemplateName = args[index]
		case strings.HasPrefix(arg, "--template="):
			request.TemplateName = strings.TrimPrefix(arg, "--template=")
			if strings.TrimSpace(request.TemplateName) == "" {
				return service.AddRequest{}, errors.New("--template requires a value")
			}
		case arg == "--app-route":
			index++
			if index >= len(args) || strings.TrimSpace(args[index]) == "" {
				return service.AddRequest{}, errors.New("--app-route requires a value")
			}
			request.ApplicationRoute = args[index]
		case strings.HasPrefix(arg, "--app-route="):
			request.ApplicationRoute = strings.TrimPrefix(arg, "--app-route=")
			if strings.TrimSpace(request.ApplicationRoute) == "" {
				return service.AddRequest{}, errors.New("--app-route requires a value")
			}
		case strings.HasPrefix(arg, "-"):
			return service.AddRequest{}, fmt.Errorf("unknown flag %q", arg)
		default:
			if request.FQDN != "" {
				return service.AddRequest{}, fmt.Errorf("unexpected argument %q", arg)
			}
			request.FQDN = arg
		}
	}
	if request.FQDN == "" {
		return service.AddRequest{}, errors.New("usage: wor service add <fqdn> [--template <name>] [--app-route <route>]")
	}
	return request, nil
}
