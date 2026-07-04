package cli

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/team-worapong/wor/internal/doctor"
	"github.com/team-worapong/wor/internal/domain"
	"github.com/team-worapong/wor/internal/engine"
	"github.com/team-worapong/wor/internal/output"
	worRuntime "github.com/team-worapong/wor/internal/runtime"
	"github.com/team-worapong/wor/internal/service"
	"github.com/team-worapong/wor/internal/setup"
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
			{"WOR_HOME", report.Config.WORHome},
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

func renderDomainAdded(renderer *output.Renderer, metadata domain.Metadata) {
	renderer.Success("domain added")
	renderer.Table(
		[]string{"Key", "Value"},
		[][]string{
			{"Domain ID", metadata.DomainID},
			{"Domain name", metadata.DomainName},
			{"Domain path", metadata.DomainPath},
			{"Created at", metadata.CreatedAt},
		},
	)
}

func renderServiceAdded(renderer *output.Renderer, metadata service.Metadata) {
	renderer.Success("service added")
	rows := [][]string{
		{"Service ID", metadata.ServiceID},
		{"Domain ID", metadata.DomainID},
		{"Domain name", metadata.DomainName},
		{"FQDN", metadata.FQDN},
		{"Template", metadata.ServiceTemplate},
	}
	if strings.TrimSpace(metadata.ApplicationRoute) != "" {
		rows = append(rows, []string{"Application route", metadata.ApplicationRoute})
	}
	rows = append(rows,
		[]string{"Public path", metadata.PublicPath},
		[]string{"Service path", metadata.ServicePath},
		[]string{"Created at", metadata.CreatedAt},
	)
	renderer.Table([]string{"Key", "Value"}, rows)
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

func detectionRows(detections []setup.Detection) [][]string {
	rows := make([][]string, 0, len(detections))
	for _, detection := range detections {
		rows = append(rows, []string{
			detection.Name,
			foundText(detection),
			valueOrDash(detection.Version),
			valueOrDash(detection.Path),
			detectionStatus(detection),
		})
	}
	return rows
}

func foundText(detection setup.Detection) string {
	if !detection.Supported {
		return "not supported"
	}
	if detection.Found {
		return "found"
	}
	return "not found"
}

func detectionStatus(detection setup.Detection) string {
	status := detection.Status
	if status == "" {
		status = "unknown"
	}
	if detection.Message != "" {
		return status + ": " + detection.Message
	}
	return status
}

func isWebServerProviderTitle(title string) bool {
	return strings.Contains(strings.ToLower(title), "web server providers")
}

func isWebServerProviderPrompt(prompt string) bool {
	return strings.EqualFold(strings.TrimSpace(prompt), "Select Web Server Provider:")
}

func webServerProviderDetection(detections []setup.Detection, provider string) setup.Detection {
	switch provider {
	case setup.WebServerNginx:
		return detectionByName(detections, "Nginx")
	case setup.WebServerApache:
		return detectionByName(detections, "Apache")
	default:
		return setup.Detection{Name: provider, Supported: false, Status: "unsupported", Message: "not supported"}
	}
}

func detectionByName(detections []setup.Detection, name string) setup.Detection {
	for _, detection := range detections {
		if strings.EqualFold(detection.Name, name) {
			return detection
		}
	}
	return setup.Detection{Name: name, Supported: true, Status: "info", Message: "not found"}
}

func detectionIcon(detection setup.Detection) string {
	if !detection.Supported {
		return "-"
	}
	if detection.Found {
		return "✓"
	}
	return "✕"
}

func detectionChoiceStatus(detection setup.Detection) string {
	if !detection.Supported {
		return "not supported"
	}
	if detection.Found {
		if version := compactVersion(detection.Name, detection.Version); version != "" {
			return version
		}
		return "found"
	}
	return "not found"
}

func compactDetectionStatus(detection setup.Detection) string {
	if !detection.Supported {
		return "not supported"
	}
	if detection.Found {
		if version := compactVersion(detection.Name, detection.Version); version != "" {
			return version
		}
		return "found"
	}
	return "not found"
}

func compactDetectionName(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "node.js":
		return "node"
	case "php-fpm":
		return "php-fpm"
	default:
		return strings.ToLower(strings.TrimSpace(name))
	}
}

var versionPatterns = map[string]*regexp.Regexp{
	"apache":  regexp.MustCompile(`Apache/([0-9][^ ]*)`),
	"certbot": regexp.MustCompile(`certbot ([0-9][^ ]*)`),
	"git":     regexp.MustCompile(`git version ([0-9][^ ]*)`),
	"go":      regexp.MustCompile(`go version go([0-9][^ ]*)`),
	"nginx":   regexp.MustCompile(`nginx/([0-9][^ ]*)`),
	"php":     regexp.MustCompile(`PHP ([0-9][^ ]*)`),
	"php-fpm": regexp.MustCompile(`PHP ([0-9][^ ]*)`),
	"python":  regexp.MustCompile(`Python ([0-9][^ ]*)`),
}

func compactVersion(name, version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return ""
	}

	key := compactDetectionName(name)
	if pattern, ok := versionPatterns[key]; ok {
		matches := pattern.FindStringSubmatch(version)
		if len(matches) > 1 {
			return strings.Trim(matches[1], ",")
		}
	}

	return firstToken(version)
}

func firstToken(value string) string {
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) == 0 {
		return ""
	}
	return strings.Trim(fields[0], ",")
}
