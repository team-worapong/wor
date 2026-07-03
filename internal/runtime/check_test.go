package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakePlatform struct {
	paths   map[string]string
	outputs map[string]string
	calls   *[]string
}

func (f fakePlatform) LookPath(name string) (string, error) {
	if path, ok := f.paths[name]; ok {
		return path, nil
	}
	return "", errors.New("not found")
}

func (f fakePlatform) CommandOutput(ctx context.Context, name string, args ...string) (string, error) {
	key := name + " " + strings.Join(args, " ")
	if f.calls != nil {
		*f.calls = append(*f.calls, key)
	}
	if output, ok := f.outputs[key]; ok {
		return output, nil
	}
	return "", errors.New("no output")
}

func TestCheckerReportsMissingRequiredRuntimeAsIssue(t *testing.T) {
	t.Parallel()

	checker := NewChecker(fakePlatform{})
	result := checker.Check(context.Background(), Target{
		Name:          "Git",
		Command:       "git",
		VersionArgs:   []string{"version"},
		VersionSource: VersionFromCommand,
		Requirement:   RequirementRequired,
		Category:      CategoryCoreTools,
	})

	if result.Status != StatusIssue {
		t.Fatalf("Status = %q", result.Status)
	}
	if !result.IsIssue() {
		t.Fatal("expected missing required runtime to be an issue")
	}
	if result.Requirement != RequirementRequired {
		t.Fatalf("Requirement = %q", result.Requirement)
	}
}

func TestCheckerReportsMissingOptionalRuntimeAsInfo(t *testing.T) {
	t.Parallel()

	checker := NewChecker(fakePlatform{})
	result := checker.Check(context.Background(), Target{
		Name:          "PHP",
		Command:       "php",
		VersionArgs:   []string{"--version"},
		VersionSource: VersionFromCommand,
		Requirement:   RequirementOptional,
		Category:      CategoryOptionalRuntimes,
	})

	if result.Status != StatusInfo {
		t.Fatalf("Status = %q", result.Status)
	}
	if result.IsIssue() {
		t.Fatal("missing optional runtime should not be an issue")
	}
	if result.Message != "optional tool not found in PATH" {
		t.Fatalf("Message = %q", result.Message)
	}
}

func TestCheckerReportsCommandVersion(t *testing.T) {
	t.Parallel()

	checker := NewChecker(fakePlatform{
		paths:   map[string]string{"go": "/usr/bin/go"},
		outputs: map[string]string{"go version": "go version go1.26.4 darwin/arm64\n"},
	})
	result := checker.Check(context.Background(), Target{
		Name:          "Go",
		Command:       "go",
		VersionArgs:   []string{"version"},
		VersionSource: VersionFromCommand,
		Requirement:   RequirementRequired,
		Category:      CategoryCoreTools,
	})

	if result.Status != StatusOK {
		t.Fatalf("Status = %q, Message = %q", result.Status, result.Message)
	}
	if result.Version != "go version go1.26.4 darwin/arm64" {
		t.Fatalf("Version = %q", result.Version)
	}
}

func TestCheckerUsesCommandCandidates(t *testing.T) {
	t.Parallel()

	checker := NewChecker(fakePlatform{
		paths:   map[string]string{"python3": "/usr/bin/python3"},
		outputs: map[string]string{"python3 --version": "Python 3.13.1\n"},
	})
	result := checker.Check(context.Background(), Target{
		Name:          "Python",
		Command:       "python",
		Commands:      []string{"python3", "python"},
		VersionArgs:   []string{"--version"},
		VersionSource: VersionFromCommand,
		Requirement:   RequirementOptional,
		Category:      CategoryOptionalRuntimes,
	})

	if result.Status != StatusOK {
		t.Fatalf("Status = %q, Message = %q", result.Status, result.Message)
	}
	if result.Command != "python3" {
		t.Fatalf("Command = %q", result.Command)
	}
	if result.Version != "Python 3.13.1" {
		t.Fatalf("Version = %q", result.Version)
	}
}

func TestPM2CheckDoesNotExecutePM2ForVersion(t *testing.T) {
	t.Parallel()

	var calls []string
	checker := NewChecker(fakePlatform{
		paths: map[string]string{
			"pm2": "/usr/bin/pm2",
			"npm": "/usr/bin/npm",
		},
		outputs: map[string]string{
			"npm root -g": "/missing/global/root",
		},
		calls: &calls,
	})
	result := checker.Check(context.Background(), Target{
		Name:          "PM2",
		Command:       "pm2",
		VersionSource: VersionFromNPMGlobalPackage,
		PackageName:   "pm2",
		Requirement:   RequirementOptional,
		Category:      CategoryOptionalRuntimes,
	})

	if result.Status != StatusWarning {
		t.Fatalf("Status = %q", result.Status)
	}
	for _, call := range calls {
		if strings.HasPrefix(call, "pm2 ") {
			t.Fatalf("pm2 command should not be executed for version detection; calls = %v", calls)
		}
	}
}

func TestDefaultTargetsCoverDoctorScope(t *testing.T) {
	t.Parallel()

	targets := DefaultTargets()
	byName := make(map[string]Target, len(targets))
	for _, target := range targets {
		byName[target.Name] = target
	}

	for _, name := range []string{"Go", "Git"} {
		target, ok := byName[name]
		if !ok {
			t.Fatalf("missing target %q", name)
		}
		if target.Requirement != RequirementRequired {
			t.Fatalf("%s Requirement = %q", name, target.Requirement)
		}
		if target.Category != CategoryCoreTools {
			t.Fatalf("%s Category = %q", name, target.Category)
		}
	}

	for _, name := range []string{"Node.js", "npm", "PM2", "PHP", "Python"} {
		target, ok := byName[name]
		if !ok {
			t.Fatalf("missing target %q", name)
		}
		if target.Requirement != RequirementOptional {
			t.Fatalf("%s Requirement = %q", name, target.Requirement)
		}
		if target.Category != CategoryOptionalRuntimes {
			t.Fatalf("%s Category = %q", name, target.Category)
		}
	}

	for _, name := range []string{"Nginx", "Apache"} {
		target, ok := byName[name]
		if !ok {
			t.Fatalf("missing target %q", name)
		}
		if target.Requirement != RequirementOptional {
			t.Fatalf("%s Requirement = %q", name, target.Requirement)
		}
		if target.Category != CategoryOptionalWebServers {
			t.Fatalf("%s Category = %q", name, target.Category)
		}
	}
}
