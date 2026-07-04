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
		resourceArgs := args[1:]
		action := parseResourceAction(resourceArgs)
		switch action {
		case "add":
			request, err := parseDomainAddRequest(resourceArgs)
			if err != nil {
				renderer.Error("domain: %v", err)
				return 1
			}
			metadata, err := app.DomainAdd(request)
			if err != nil {
				renderer.Error("domain: %v", err)
				return 1
			}
			renderer.RenderDomainAdded(domainAddedView(metadata))
		case "list":
			if err := parseDomainListRequest(resourceArgs); err != nil {
				renderer.Error("domain: %v", err)
				return 1
			}
			items, err := app.DomainList()
			if err != nil {
				renderer.Error("domain: %v", err)
				return 1
			}
			renderer.RenderDomainList(domainListView(items))
		case "show":
			domainName, err := parseDomainShowRequest(resourceArgs)
			if err != nil {
				renderer.Error("domain: %v", err)
				return 1
			}
			report, err := app.DomainShow(domainName)
			if err != nil {
				renderer.Error("domain: %v", err)
				return 1
			}
			renderer.RenderDomain(domainDetailsView(report))
		default:
			renderer.Error("domain: %v", errors.New("usage: wor domain <add|list|show>"))
			return 1
		}
	case "service":
		resourceArgs := args[1:]
		action := parseResourceAction(resourceArgs)
		switch action {
		case "add":
			request, err := parseServiceAddRequest(resourceArgs)
			if err != nil {
				renderer.Error("service: %v", err)
				return 1
			}
			metadata, err := app.ServiceAdd(ctx, request)
			if err != nil {
				renderer.Error("service: %v", err)
				return 1
			}
			renderer.RenderServiceAdded(serviceAddedView(metadata))
		case "list":
			request, err := parseServiceListRequest(resourceArgs)
			if err != nil {
				renderer.Error("service: %v", err)
				return 1
			}
			items, err := app.ServiceList(request)
			if err != nil {
				renderer.Error("service: %v", err)
				return 1
			}
			renderer.RenderServiceList(serviceListView(items))
		case "show":
			fqdn, err := parseServiceShowRequest(resourceArgs)
			if err != nil {
				renderer.Error("service: %v", err)
				return 1
			}
			report, err := app.ServiceShow(fqdn)
			if err != nil {
				renderer.Error("service: %v", err)
				return 1
			}
			renderer.RenderService(serviceDetailsView(report))
		default:
			renderer.Error("service: %v", errors.New("usage: wor service <add|list|show>"))
			return 1
		}
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

func parseResourceAction(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return strings.TrimSpace(args[0])
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

func parseDomainListRequest(args []string) error {
	if len(args) != 1 || args[0] != "list" {
		return errors.New("usage: wor domain list")
	}
	return nil
}

func parseDomainShowRequest(args []string) (string, error) {
	if len(args) != 2 || args[0] != "show" {
		return "", errors.New("usage: wor domain show <domain>")
	}
	return args[1], nil
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

func parseServiceListRequest(args []string) (service.ListRequest, error) {
	if len(args) == 0 || args[0] != "list" {
		return service.ListRequest{}, errors.New("usage: wor service list [--domain <domain>]")
	}

	var request service.ListRequest
	for index := 1; index < len(args); index++ {
		arg := strings.TrimSpace(args[index])
		switch {
		case arg == "":
			continue
		case arg == "--domain":
			index++
			if index >= len(args) || strings.TrimSpace(args[index]) == "" {
				return service.ListRequest{}, errors.New("--domain requires a value")
			}
			request.Domain = args[index]
		case strings.HasPrefix(arg, "--domain="):
			request.Domain = strings.TrimPrefix(arg, "--domain=")
			if strings.TrimSpace(request.Domain) == "" {
				return service.ListRequest{}, errors.New("--domain requires a value")
			}
		default:
			return service.ListRequest{}, fmt.Errorf("unexpected argument %q", arg)
		}
	}
	return request, nil
}

func parseServiceShowRequest(args []string) (string, error) {
	if len(args) != 2 || args[0] != "show" {
		return "", errors.New("usage: wor service show <fqdn>")
	}
	return args[1], nil
}
