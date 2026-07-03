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
	StatusMissing Status = "missing"
	StatusWarning Status = "warning"
)

type VersionSource string

const (
	VersionFromCommand          VersionSource = "command"
	VersionFromNPMGlobalPackage VersionSource = "npm_global_package"
)

type Target struct {
	Name          string
	Command       string
	VersionArgs   []string
	VersionSource VersionSource
	PackageName   string
}

type CheckResult struct {
	Name    string
	Command string
	Path    string
	Version string
	Status  Status
	Message string
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

func Required() []Target {
	return []Target{
		{
			Name:          "Go",
			Command:       "go",
			VersionArgs:   []string{"version"},
			VersionSource: VersionFromCommand,
		},
		{
			Name:          "Git",
			Command:       "git",
			VersionArgs:   []string{"version"},
			VersionSource: VersionFromCommand,
		},
		{
			Name:          "Node.js",
			Command:       "node",
			VersionArgs:   []string{"--version"},
			VersionSource: VersionFromCommand,
		},
		{
			Name:          "npm",
			Command:       "npm",
			VersionArgs:   []string{"--version"},
			VersionSource: VersionFromCommand,
		},
		{
			Name:          "PM2",
			Command:       "pm2",
			VersionSource: VersionFromNPMGlobalPackage,
			PackageName:   "pm2",
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
	path, err := c.platform.LookPath(target.Command)
	if err != nil {
		return CheckResult{
			Name:    target.Name,
			Command: target.Command,
			Status:  StatusMissing,
			Message: "not found in PATH",
		}
	}

	version, err := c.version(ctx, target)
	if err != nil {
		return CheckResult{
			Name:    target.Name,
			Command: target.Command,
			Path:    path,
			Status:  StatusWarning,
			Message: err.Error(),
		}
	}

	return CheckResult{
		Name:    target.Name,
		Command: target.Command,
		Path:    path,
		Version: version,
		Status:  StatusOK,
	}
}

func (c Checker) version(ctx context.Context, target Target) (string, error) {
	switch target.VersionSource {
	case VersionFromCommand:
		return c.commandVersion(ctx, target.Command, target.VersionArgs...)
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
