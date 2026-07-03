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
}

func (f fakePlatform) LookPath(name string) (string, error) {
	if path, ok := f.paths[name]; ok {
		return path, nil
	}
	return "", errors.New("not found")
}

func (f fakePlatform) CommandOutput(ctx context.Context, name string, args ...string) (string, error) {
	key := name + " " + strings.Join(args, " ")
	if output, ok := f.outputs[key]; ok {
		return output, nil
	}
	return "", errors.New("no output")
}

func TestCheckerReportsMissingRuntime(t *testing.T) {
	t.Parallel()

	checker := NewChecker(fakePlatform{})
	result := checker.Check(context.Background(), Target{
		Name:          "Git",
		Command:       "git",
		VersionArgs:   []string{"version"},
		VersionSource: VersionFromCommand,
	})

	if result.Status != StatusMissing {
		t.Fatalf("Status = %q", result.Status)
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
	})

	if result.Status != StatusOK {
		t.Fatalf("Status = %q, Message = %q", result.Status, result.Message)
	}
	if result.Version != "go version go1.26.4 darwin/arm64" {
		t.Fatalf("Version = %q", result.Version)
	}
}
