package cli

import (
	"fmt"
	"strings"

	"github.com/team-worapong/wor/internal/doctor"
	"github.com/team-worapong/wor/internal/engine"
	"github.com/team-worapong/wor/internal/output"
	worRuntime "github.com/team-worapong/wor/internal/runtime"
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
	renderer.Text("Read-only diagnostic. No packages will be installed and no system configuration will be changed.")

	for index, group := range report.Groups {
		if index > 0 {
			renderer.Text("")
		}
		renderer.Text("%s (%s)", group.Title, group.Requirement)
		renderer.Table([]string{"Tool", "Required", "Path", "Version", "Status"}, doctorRows(group.Results))
	}

	if report.HasIssues {
		renderer.Warning("doctor completed with required issues")
		return
	}

	renderer.Success("doctor completed")
}

func doctorRows(results []worRuntime.CheckResult) [][]string {
	rows := make([][]string, 0, len(results))
	for _, result := range results {
		rows = append(rows, []string{
			result.Name,
			string(result.Requirement),
			valueOrDash(result.Path),
			valueOrDash(result.Version),
			doctorStatus(result),
		})
	}
	return rows
}

func doctorStatus(result worRuntime.CheckResult) string {
	status := string(result.Status)
	if result.Message != "" {
		status += ": " + result.Message
	}
	return status
}

func valueOrDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}
