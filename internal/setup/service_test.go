package setup

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/team-worapong/wor/internal/config"
	"github.com/team-worapong/wor/internal/platform"
	worRuntime "github.com/team-worapong/wor/internal/runtime"
)

type fakeFileSystem struct {
	exists bool
	writes []config.Config
	dirs   []string
}

func (f *fakeFileSystem) Exists(path string) (bool, error) {
	return f.exists, nil
}

func (f *fakeFileSystem) WriteConfig(path string, cfg config.Config) error {
	cfg.ConfigFile = path
	f.writes = append(f.writes, cfg)
	return nil
}

func (f *fakeFileSystem) MkdirAll(path string) error {
	f.dirs = append(f.dirs, path)
	return nil
}

type fakeChecker struct {
	results map[string]worRuntime.CheckResult
}

func (f fakeChecker) Check(ctx context.Context, target worRuntime.Target) worRuntime.CheckResult {
	if result, ok := f.results[target.Name]; ok {
		return result
	}
	return missingResult(target)
}

func (f fakeChecker) CheckAll(ctx context.Context, targets []worRuntime.Target) []worRuntime.CheckResult {
	results := make([]worRuntime.CheckResult, 0, len(targets))
	for _, target := range targets {
		results = append(results, f.Check(ctx, target))
	}
	return results
}

type fakeInteractor struct {
	selects       []string
	inputs        []string
	confirms      []bool
	prompts       []string
	summary       Summary
	summaryShown  bool
	confirmCalled bool
	warnings      []string
}

func (f *fakeInteractor) Select(prompt string, options []Option, defaultValue string) (string, error) {
	f.prompts = append(f.prompts, prompt)
	if len(f.selects) == 0 {
		return defaultValue, nil
	}
	next := f.selects[0]
	f.selects = f.selects[1:]
	return next, nil
}

func (f *fakeInteractor) Input(prompt, defaultValue string) (string, error) {
	if len(f.inputs) == 0 {
		return defaultValue, nil
	}
	next := f.inputs[0]
	f.inputs = f.inputs[1:]
	return next, nil
}

func (f *fakeInteractor) Confirm(prompt string, defaultYes bool) (bool, error) {
	f.confirmCalled = true
	if len(f.confirms) == 0 {
		return defaultYes, nil
	}
	next := f.confirms[0]
	f.confirms = f.confirms[1:]
	return next, nil
}

func (f *fakeInteractor) ShowDetections(title string, detections []Detection) {}

func (f *fakeInteractor) ShowSummary(summary Summary) {
	f.summary = summary
	f.summaryShown = true
}

func (f *fakeInteractor) Info(message string) {}

func (f *fakeInteractor) Warning(message string) {
	f.warnings = append(f.warnings, message)
}

func TestDryRunDoesNotWriteConfigOrCreateDirectories(t *testing.T) {
	t.Parallel()

	fs := &fakeFileSystem{}
	interactor := &fakeInteractor{
		selects: []string{EnvironmentDevelopment, WebServerSkip, SSLProviderNone},
		inputs:  []string{filepath.Join(t.TempDir(), "wor-home")},
	}
	service := testService(fs)

	result, err := service.Run(context.Background(), Request{DryRun: true}, interactor)
	if err != nil {
		t.Fatalf("run setup: %v", err)
	}

	if !result.DryRun {
		t.Fatal("DryRun = false")
	}
	if len(fs.writes) != 0 {
		t.Fatalf("writes = %d", len(fs.writes))
	}
	if len(fs.dirs) != 0 {
		t.Fatalf("dirs = %d", len(fs.dirs))
	}
	if interactor.confirmCalled {
		t.Fatal("dry-run should not ask for apply confirmation")
	}
	if !interactor.summaryShown {
		t.Fatal("summary was not shown")
	}
}

func TestExistingConfigIsNotOverwrittenWithoutConfirmation(t *testing.T) {
	t.Parallel()

	fs := &fakeFileSystem{exists: true}
	interactor := &fakeInteractor{
		selects:  []string{EnvironmentDevelopment, WebServerSkip, SSLProviderNone},
		inputs:   []string{filepath.Join(t.TempDir(), "wor-home")},
		confirms: []bool{false},
	}
	service := testService(fs)
	service.config.Environment = EnvironmentProduction
	service.config.WORHome = "/existing/wor"

	result, err := service.Run(context.Background(), Request{}, interactor)
	if err != nil {
		t.Fatalf("run setup: %v", err)
	}

	if !result.Cancelled {
		t.Fatal("Cancelled = false")
	}
	if !interactor.confirmCalled {
		t.Fatal("confirmation was not requested")
	}
	if len(fs.writes) != 0 {
		t.Fatalf("writes = %d", len(fs.writes))
	}
	if len(fs.dirs) != 0 {
		t.Fatalf("dirs = %d", len(fs.dirs))
	}
}

func TestApplyWritesConfigAndCreatesOnlyWORHomeDirectories(t *testing.T) {
	t.Parallel()

	home := filepath.Join(t.TempDir(), "wor-home")
	fs := &fakeFileSystem{}
	interactor := &fakeInteractor{
		selects:  []string{EnvironmentDevelopment, WebServerSkip, SSLProviderNone},
		inputs:   []string{home},
		confirms: []bool{true},
	}
	service := testService(fs)

	result, err := service.Run(context.Background(), Request{}, interactor)
	if err != nil {
		t.Fatalf("run setup: %v", err)
	}

	if !result.Applied {
		t.Fatal("Applied = false")
	}
	if len(fs.writes) != 1 {
		t.Fatalf("writes = %d", len(fs.writes))
	}
	written := fs.writes[0]
	if written.Environment != EnvironmentDevelopment {
		t.Fatalf("Environment = %q", written.Environment)
	}
	if written.ConfigFile != service.config.ConfigFile {
		t.Fatalf("ConfigFile = %q", written.ConfigFile)
	}
	if written.WORHome != home {
		t.Fatalf("WORHome = %q", written.WORHome)
	}
	if written.DataDir != config.LayoutForHome(home).Data {
		t.Fatalf("DataDir = %q", written.DataDir)
	}
	if written.CacheDir != config.LayoutForHome(home).Cache {
		t.Fatalf("CacheDir = %q", written.CacheDir)
	}
	if written.WebServerProvider != WebServerSkip {
		t.Fatalf("WebServerProvider = %q", written.WebServerProvider)
	}
	expectedDirs := config.LayoutForHome(home).Directories()
	if len(fs.dirs) != len(expectedDirs) {
		t.Fatalf("dirs = %d", len(fs.dirs))
	}
	if !sameStrings(fs.dirs, expectedDirs) {
		t.Fatalf("dirs = %#v", fs.dirs)
	}
}

func TestMissingOptionalProviderDoesNotFailSetup(t *testing.T) {
	t.Parallel()

	fs := &fakeFileSystem{}
	interactor := &fakeInteractor{
		selects: []string{
			EnvironmentDevelopment,
			WebServerNginx,
			WebServerSkip,
			SSLProviderNone,
		},
		inputs: []string{filepath.Join(t.TempDir(), "wor-home")},
	}
	service := testService(fs)

	result, err := service.Run(context.Background(), Request{DryRun: true}, interactor)
	if err != nil {
		t.Fatalf("run setup: %v", err)
	}

	if result.Summary.WebServerProvider != WebServerSkip {
		t.Fatalf("WebServerProvider = %q", result.Summary.WebServerProvider)
	}
	if len(interactor.warnings) == 0 {
		t.Fatal("expected warning for missing nginx")
	}
	if interactor.warnings[0] != "Nginx is not installed. Please choose another provider or skip." {
		t.Fatalf("warning = %q", interactor.warnings[0])
	}
}

func TestSetupDoesNotAskPHPDetectionQuestion(t *testing.T) {
	t.Parallel()

	fs := &fakeFileSystem{}
	interactor := &fakeInteractor{
		selects: []string{EnvironmentDevelopment, WebServerSkip, SSLProviderNone},
		inputs:  []string{filepath.Join(t.TempDir(), "wor-home")},
	}
	service := testService(fs)

	result, err := service.Run(context.Background(), Request{DryRun: true}, interactor)
	if err != nil {
		t.Fatalf("run setup: %v", err)
	}

	for _, prompt := range interactor.prompts {
		if prompt == "Detect PHP/PHP-FPM?" {
			t.Fatalf("unexpected PHP detection prompt %q", prompt)
		}
	}
	if !hasDetection(result.Summary.Detections, "PHP-FPM") {
		t.Fatal("PHP-FPM detection should still be shown in summary")
	}
}

func TestCommonTargetsIncludeGoAndPython(t *testing.T) {
	t.Parallel()

	targets := commonTargets()
	names := make(map[string]bool, len(targets))
	for _, target := range targets {
		names[target.Name] = true
	}
	for _, name := range []string{"Go", "Python"} {
		if !names[name] {
			t.Fatalf("missing target %q", name)
		}
	}
}

func TestNilInteractorReturnsError(t *testing.T) {
	t.Parallel()

	_, err := testService(&fakeFileSystem{}).Run(context.Background(), Request{}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func testService(fs *fakeFileSystem) Service {
	return Service{
		system: platform.New("darwin", "arm64"),
		config: config.Config{
			ConfigFile:   "/tmp/wor/config.json",
			OutputFormat: "text",
		},
		fs:      fs,
		checker: fakeChecker{results: fakeResults()},
	}
}

func fakeResults() map[string]worRuntime.CheckResult {
	return map[string]worRuntime.CheckResult{
		"Git":    okResult("Git", "git", "/usr/bin/git", "git version 2.50.1"),
		"Go":     okResult("Go", "go", "/usr/local/go/bin/go", "go version go1.26.4 darwin/arm64"),
		"PHP":    okResult("PHP", "php", "/usr/bin/php", "PHP 8.4.0"),
		"Python": okResult("Python", "python3", "/usr/bin/python3", "Python 3.14.0"),
		"PHP-FPM": {
			Name:        "PHP-FPM",
			Command:     "php-fpm",
			Status:      worRuntime.StatusInfo,
			Requirement: worRuntime.RequirementOptional,
			Category:    worRuntime.CategoryOptionalRuntimes,
			Message:     "optional tool not found in PATH",
		},
	}
}

func hasDetection(detections []Detection, name string) bool {
	for _, detection := range detections {
		if detection.Name == name {
			return true
		}
	}
	return false
}

func okResult(name, command, path, version string) worRuntime.CheckResult {
	return worRuntime.CheckResult{
		Name:        name,
		Command:     command,
		Path:        path,
		Version:     version,
		Status:      worRuntime.StatusOK,
		Requirement: worRuntime.RequirementOptional,
	}
}

func missingResult(target worRuntime.Target) worRuntime.CheckResult {
	return worRuntime.CheckResult{
		Name:        target.Name,
		Command:     target.Command,
		Status:      worRuntime.StatusInfo,
		Requirement: worRuntime.RequirementOptional,
		Category:    target.Category,
		Message:     "optional tool not found in PATH",
	}
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
