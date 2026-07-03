package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Platform interface {
	LookPath(name string) (string, error)
	CommandOutput(ctx context.Context, name string, args ...string) (string, error)
}

type Status string

const (
	StatusOK      Status = "ok"
	StatusIssue   Status = "issue"
	StatusWarning Status = "warning"
	StatusInfo    Status = "info"
)

type Requirement string

const (
	RequirementRequired Requirement = "required"
	RequirementOptional Requirement = "optional"
)

type Category string

const (
	CategoryCoreTools          Category = "core_tools"
	CategoryOptionalRuntimes   Category = "optional_runtimes"
	CategoryOptionalWebServers Category = "optional_web_servers"
)

type VersionSource string

const (
	VersionFromCommand          VersionSource = "command"
	VersionFromNPMGlobalPackage VersionSource = "npm_global_package"
)

type Target struct {
	Name          string
	Command       string
	Commands      []string
	VersionArgs   []string
	VersionSource VersionSource
	PackageName   string
	Requirement   Requirement
	Category      Category
}

type CheckResult struct {
	Name        string
	Command     string
	Path        string
	Version     string
	Status      Status
	Requirement Requirement
	Category    Category
	Message     string
}

type Checker struct {
	platform Platform
	timeout  time.Duration
}

func NewChecker(platform Platform) Checker {
	return Checker{
		platform: platform,
		timeout:  3 * time.Second,
	}
}

func DefaultTargets() []Target {
	return []Target{
		{
			Name:          "Go",
			Command:       "go",
			VersionArgs:   []string{"version"},
			VersionSource: VersionFromCommand,
			Requirement:   RequirementRequired,
			Category:      CategoryCoreTools,
		},
		{
			Name:          "Git",
			Command:       "git",
			VersionArgs:   []string{"version"},
			VersionSource: VersionFromCommand,
			Requirement:   RequirementRequired,
			Category:      CategoryCoreTools,
		},
		{
			Name:          "Node.js",
			Command:       "node",
			VersionArgs:   []string{"--version"},
			VersionSource: VersionFromCommand,
			Requirement:   RequirementOptional,
			Category:      CategoryOptionalRuntimes,
		},
		{
			Name:          "npm",
			Command:       "npm",
			VersionArgs:   []string{"--version"},
			VersionSource: VersionFromCommand,
			Requirement:   RequirementOptional,
			Category:      CategoryOptionalRuntimes,
		},
		{
			Name:          "PM2",
			Command:       "pm2",
			VersionSource: VersionFromNPMGlobalPackage,
			PackageName:   "pm2",
			Requirement:   RequirementOptional,
			Category:      CategoryOptionalRuntimes,
		},
		{
			Name:          "PHP",
			Command:       "php",
			VersionArgs:   []string{"--version"},
			VersionSource: VersionFromCommand,
			Requirement:   RequirementOptional,
			Category:      CategoryOptionalRuntimes,
		},
		{
			Name:          "Python",
			Command:       "python",
			Commands:      []string{"python3", "python"},
			VersionArgs:   []string{"--version"},
			VersionSource: VersionFromCommand,
			Requirement:   RequirementOptional,
			Category:      CategoryOptionalRuntimes,
		},
		{
			Name:          "Nginx",
			Command:       "nginx",
			VersionArgs:   []string{"-v"},
			VersionSource: VersionFromCommand,
			Requirement:   RequirementOptional,
			Category:      CategoryOptionalWebServers,
		},
		{
			Name:          "Apache",
			Command:       "apache",
			Commands:      []string{"apache2", "httpd"},
			VersionArgs:   []string{"-v"},
			VersionSource: VersionFromCommand,
			Requirement:   RequirementOptional,
			Category:      CategoryOptionalWebServers,
		},
	}
}

func (c Checker) CheckAll(ctx context.Context, targets []Target) []CheckResult {
	results := make([]CheckResult, 0, len(targets))
	for _, target := range targets {
		results = append(results, c.Check(ctx, target))
	}
	return results
}

func (c Checker) Check(ctx context.Context, target Target) CheckResult {
	target = normalizeTarget(target)
	result := CheckResult{
		Name:        target.Name,
		Command:     target.Command,
		Requirement: target.Requirement,
		Category:    target.Category,
	}

	command, path, err := c.lookPath(target)
	if err != nil {
		result.Status = missingStatus(target.Requirement)
		result.Message = missingMessage(target.Requirement)
		return result
	}

	result.Command = command
	result.Path = path

	version, err := c.version(ctx, target, command)
	if err != nil {
		result.Status = StatusWarning
		result.Message = err.Error()
		return result
	}

	result.Version = version
	result.Status = StatusOK
	return result
}

func (c Checker) version(ctx context.Context, target Target, command string) (string, error) {
	switch target.VersionSource {
	case VersionFromCommand:
		return c.commandVersion(ctx, command, target.VersionArgs...)
	case VersionFromNPMGlobalPackage:
		return c.npmGlobalPackageVersion(ctx, target.PackageName)
	default:
		return "", fmt.Errorf("unknown version source %q", target.VersionSource)
	}
}

func (c Checker) commandVersion(ctx context.Context, command string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	output, err := c.platform.CommandOutput(ctx, command, args...)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(output) == "" {
		return "", errors.New("empty version output")
	}
	return firstLine(output), nil
}

func (c Checker) npmGlobalPackageVersion(ctx context.Context, packageName string) (string, error) {
	if strings.TrimSpace(packageName) == "" {
		return "", errors.New("missing npm package name")
	}

	root, err := c.commandVersion(ctx, "npm", "root", "-g")
	if err != nil {
		return "", fmt.Errorf("npm global root unavailable: %w", err)
	}

	packageFile := filepath.Join(strings.TrimSpace(root), packageName, "package.json")
	data, err := os.ReadFile(packageFile)
	if err != nil {
		return "", fmt.Errorf("version metadata unavailable")
	}

	var metadata struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		return "", fmt.Errorf("parse version metadata: %w", err)
	}
	if strings.TrimSpace(metadata.Version) == "" {
		return "", errors.New("version metadata is empty")
	}

	return metadata.Version, nil
}

func firstLine(output string) string {
	output = strings.ReplaceAll(output, "\r\n", "\n")
	output = strings.ReplaceAll(output, "\r", "\n")
	line, _, _ := strings.Cut(output, "\n")
	return strings.TrimSpace(line)
}

func (c Checker) lookPath(target Target) (string, string, error) {
	for _, command := range target.commandCandidates() {
		path, err := c.platform.LookPath(command)
		if err == nil {
			return command, path, nil
		}
	}
	return "", "", fmt.Errorf("%s not found in PATH", target.Command)
}

func (t Target) commandCandidates() []string {
	if len(t.Commands) > 0 {
		return t.Commands
	}
	return []string{t.Command}
}

func (r CheckResult) IsIssue() bool {
	return r.Requirement == RequirementRequired && r.Status != StatusOK
}

func normalizeTarget(target Target) Target {
	if target.Command == "" {
		target.Command = strings.ToLower(target.Name)
	}
	if target.Requirement == "" {
		target.Requirement = RequirementOptional
	}
	if target.Category == "" {
		target.Category = CategoryOptionalRuntimes
	}
	return target
}

func missingStatus(requirement Requirement) Status {
	if requirement == RequirementRequired {
		return StatusIssue
	}
	return StatusInfo
}

func missingMessage(requirement Requirement) string {
	if requirement == RequirementRequired {
		return "required tool not found in PATH"
	}
	return "optional tool not found in PATH"
}
