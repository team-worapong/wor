package cli

import (
	"context"
	"io"

	"github.com/team-worapong/wor/internal/config"
	"github.com/team-worapong/wor/internal/engine"
	"github.com/team-worapong/wor/internal/output"
	"github.com/team-worapong/wor/internal/platform"
)

const appName = "wor"

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
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
