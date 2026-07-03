package cli

import (
	"fmt"
	"strings"

	"github.com/team-worapong/wor/internal/doctor"
	"github.com/team-worapong/wor/internal/engine"
	"github.com/team-worapong/wor/internal/output"
)

func renderVersion(renderer *output.Renderer, report engine.VersionReport) {
	renderer.Text("%s %s", report.Name, report.Version)
	if report.CommitAvailable {
		renderer.Text("Commit: %s", report.Commit)
	}
	if report.BuildDateAvailable {
		renderer.Text("Build date: %s", report.BuildDate)
	}
}

func renderHelp(renderer *output.Renderer, help engine.HelpReport) {
	renderer.Text(help.Title)
	renderer.Text("")
	renderer.Text("Usage:")
	renderer.Text("  %s", help.Usage)
	renderer.Text("")
	renderer.Text("Commands:")

	rows := make([][]string, 0, len(help.Commands))
	for _, command := range help.Commands {
		rows = append(rows, []string{command.Name, command.Description})
	}
	renderer.Table([]string{"Command", "Description"}, rows)
}

func renderEnvironment(renderer *output.Renderer, report engine.EnvironmentReport) {
	renderer.Text("Runtime")
	renderer.Table(
		[]string{"Key", "Value"},
		[][]string{
			{"Version", report.Runtime.Version},
			{"Operating system", report.Runtime.OS},
			{"Architecture", report.Runtime.Arch},
			{"Supported", fmt.Sprintf("%t", report.Runtime.Supported)},
		},
	)

	renderer.Text("")
	renderer.Text("Configuration")
	renderer.Table(
		[]string{"Key", "Value"},
		[][]string{
			{"Config file", report.Config.ConfigFile},
			{"Home directory", report.Config.HomeDir},
			{"Data directory", report.Config.DataDir},
			{"Cache directory", report.Config.CacheDir},
			{"Output format", report.Config.OutputFormat},
			{"Debug", fmt.Sprintf("%t", report.Config.Debug)},
		},
	)

	rows := make([][]string, 0, len(report.Environment))
	for _, variable := range report.Environment {
		value := variable.Value
		if !variable.Set {
			value = "(not set)"
		}
		rows = append(rows, []string{variable.Name, value})
	}

	renderer.Text("")
	renderer.Text("Environment variables")
	renderer.Table([]string{"Name", "Value"}, rows)
}

func renderDoctor(renderer *output.Renderer, report doctor.Report) {
	rows := make([][]string, 0, len(report.Results))
	for _, result := range report.Results {
		versionText := result.Version
		if strings.TrimSpace(versionText) == "" {
			versionText = "-"
		}

		pathText := result.Path
		if strings.TrimSpace(pathText) == "" {
			pathText = "-"
		}

		statusText := string(result.Status)
		if result.Message != "" {
			statusText = statusText + ": " + result.Message
		}

		rows = append(rows, []string{
			result.Name,
			pathText,
			versionText,
			statusText,
		})
	}

	renderer.Table([]string{"Runtime", "Path", "Version", "Status"}, rows)

	if report.HasIssues {
		renderer.Warning("doctor completed with issues")
		return
	}

	renderer.Success("doctor completed")
}
