package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/team-worapong/wor/internal/output"
	"github.com/team-worapong/wor/internal/setup"
)

func TestWebServerProviderChoicesRenderFoundMissingAndUnsupported(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	interactor := newTerminalInteractor(strings.NewReader("\n"), output.New(&stdout, &stderr, output.FormatText))

	interactor.ShowDetections("Detected Web Server Providers:", []setup.Detection{
		{
			Name:      "Nginx",
			Found:     true,
			Supported: true,
			Path:      "/usr/sbin/nginx",
			Version:   "nginx version: nginx/1.31.2",
			Status:    "ok",
		},
		{
			Name:      "Apache",
			Supported: true,
			Path:      "/usr/sbin/apache2",
			Status:    "info",
			Message:   "optional tool not found in PATH",
		},
		{
			Name:      "IIS",
			Supported: false,
			Status:    "unsupported",
			Message:   "not supported on this platform",
		},
	})

	choice, err := interactor.Select("Select Web Server Provider:", []setup.Option{
		{Value: setup.WebServerNginx, Label: "Nginx"},
		{Value: setup.WebServerApache, Label: "Apache"},
		{Value: setup.WebServerSkip, Label: "Skip"},
	}, setup.WebServerSkip)
	if err != nil {
		t.Fatalf("select provider: %v", err)
	}
	if choice != setup.WebServerSkip {
		t.Fatalf("choice = %q", choice)
	}

	got := stdout.String()
	for _, want := range []string{
		"Select Web Server Provider",
		"Nginx  1.31.2",
		"Apache (not found)",
		"IIS    (not supported)",
		"❯ Skip",
		"Select [Skip]:",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"Detected Web Server Providers:", "/usr/sbin/nginx", "/usr/sbin/apache2"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("wizard output should not include executable path %q:\n%s", unwanted, got)
		}
	}
}

func TestSSLProviderChoicesRenderCertbotVersionAndNoPath(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	interactor := newTerminalInteractor(strings.NewReader("\n"), output.New(&stdout, &stderr, output.FormatText))

	interactor.ShowDetections("Detected SSL Providers:", []setup.Detection{
		{
			Name:      "Certbot",
			Found:     true,
			Supported: true,
			Path:      "/opt/homebrew/bin/certbot",
			Version:   "certbot 4.2.0",
			Status:    "ok",
		},
	})

	choice, err := interactor.Select("Select SSL Provider:", []setup.Option{
		{Value: setup.SSLProviderLetsEncrypt, Label: "Let's Encrypt"},
		{Value: setup.SSLProviderSelfSigned, Label: "Self Signed"},
		{Value: setup.SSLProviderNone, Label: "None", Aliases: []string{"skip"}},
	}, setup.SSLProviderNone)
	if err != nil {
		t.Fatalf("select provider: %v", err)
	}
	if choice != setup.SSLProviderNone {
		t.Fatalf("choice = %q", choice)
	}

	got := stdout.String()
	for _, want := range []string{
		"Select SSL Provider",
		"Let's Encrypt certbot 4.2.0",
		"Self Signed",
		"❯ None",
		"Select [None]:",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"Detected SSL Providers:", "/opt/homebrew/bin/certbot"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("wizard output should not include %q:\n%s", unwanted, got)
		}
	}
}

func TestSetupSummaryRuntimeDetectionDoesNotShowExecutablePath(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	interactor := newTerminalInteractor(strings.NewReader(""), output.New(&stdout, &stderr, output.FormatText))

	interactor.ShowSummary(setup.Summary{
		Environment:       setup.EnvironmentDevelopment,
		WORHome:           "/tmp/wor",
		WebServerProvider: setup.WebServerSkip,
		SSLProvider:       setup.SSLProviderNone,
		ConfigFile:        "/tmp/config.json",
		Directories:       []string{"/tmp/wor/logs"},
		Detections: []setup.Detection{
			{
				Name:      "Git",
				Found:     true,
				Supported: true,
				Path:      "/usr/bin/git",
				Version:   "git version 2.45.0",
				Status:    "ok",
			},
			{
				Name:      "PM2",
				Supported: true,
				Status:    "info",
				Message:   "optional tool not found in PATH",
			},
		},
	})

	got := stdout.String()
	for _, want := range []string{
		"Detected Runtimes",
		"✓ git      2.45.0",
		"✕ pm2      not found",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "/usr/bin/git") {
		t.Fatalf("setup summary should not include executable path:\n%s", got)
	}
}

func TestParseSetupRequest(t *testing.T) {
	t.Parallel()

	request, err := parseSetupRequest([]string{"--dry-run"})
	if err != nil {
		t.Fatalf("parse setup request: %v", err)
	}
	if !request.DryRun {
		t.Fatal("DryRun = false")
	}

	if _, err := parseSetupRequest([]string{"--unknown"}); err == nil {
		t.Fatal("expected error for unknown flag")
	}
}
