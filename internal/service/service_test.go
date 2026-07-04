package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/team-worapong/wor/internal/config"
	"github.com/team-worapong/wor/internal/domain"
	worRuntime "github.com/team-worapong/wor/internal/runtime"
)

type fakeRuntimeChecker struct {
	results map[string]worRuntime.CheckResult
}

func (f fakeRuntimeChecker) Check(ctx context.Context, target worRuntime.Target) worRuntime.CheckResult {
	if result, ok := f.results[target.Name]; ok {
		return result
	}
	return worRuntime.CheckResult{
		Name:        target.Name,
		Command:     target.Command,
		Status:      worRuntime.StatusIssue,
		Requirement: worRuntime.RequirementRequired,
		Message:     "required tool not found in PATH",
	}
}

func TestServiceIDRules(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"app.example.com":        "app",
		"api.app.example.com":    "api-app",
		"v1.api.app.example.com": "v1-api-app",
	}
	for input, want := range tests {
		got, err := ServiceID(input, "example.com")
		if err != nil {
			t.Fatalf("ServiceID(%q): %v", input, err)
		}
		if got != want {
			t.Fatalf("ServiceID(%q) = %q", input, got)
		}
	}
}

func TestInvalidServiceIDInputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		fqdn       string
		domainName string
	}{
		{name: "missing subdomain", fqdn: "example.com", domainName: "example.com"},
		{name: "not under domain", fqdn: "app.example.net", domainName: "example.com"},
		{name: "empty label", fqdn: "api..example.com", domainName: "example.com"},
		{name: "unsupported character", fqdn: "bad_label.example.com", domainName: "example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got, err := ServiceID(tt.fqdn, tt.domainName); err == nil {
				t.Fatalf("ServiceID = %q, expected error", got)
			}
		})
	}
}

func TestAddStaticServiceCreatesPublicDirectoryAndMetadata(t *testing.T) {
	t.Parallel()

	cfg := testConfig(t)
	addDomain(t, cfg, "example.com")
	manager := NewManager(cfg, fakeRuntimeChecker{})

	metadata, err := manager.Add(context.Background(), AddRequest{FQDN: "app.example.com"})
	if err != nil {
		t.Fatalf("add service: %v", err)
	}

	wantServicePath := filepath.Join(cfg.WORHome, "domains", "com-example", "app")
	if metadata.ServiceID != "app" {
		t.Fatalf("ServiceID = %q", metadata.ServiceID)
	}
	if metadata.DomainID != "com-example" {
		t.Fatalf("DomainID = %q", metadata.DomainID)
	}
	if metadata.ServiceTemplate != TemplateStatic {
		t.Fatalf("ServiceTemplate = %q", metadata.ServiceTemplate)
	}
	if metadata.ServicePath != wantServicePath {
		t.Fatalf("ServicePath = %q", metadata.ServicePath)
	}
	if metadata.PublicPath != filepath.Join(wantServicePath, PublicDirName) {
		t.Fatalf("PublicPath = %q", metadata.PublicPath)
	}
	if _, err := os.Stat(metadata.PublicPath); err != nil {
		t.Fatalf("public directory not created: %v", err)
	}

	stored := readServiceMetadata(t, filepath.Join(wantServicePath, MetadataFileName))
	if stored.FQDN != "app.example.com" || stored.ServiceTemplate != TemplateStatic {
		t.Fatalf("stored metadata = %#v", stored)
	}
}

func TestAddServiceRejectsInvalidFQDN(t *testing.T) {
	t.Parallel()

	cfg := testConfig(t)
	addDomain(t, cfg, "example.com")
	manager := NewManager(cfg, fakeRuntimeChecker{})

	_, err := manager.Add(context.Background(), AddRequest{FQDN: "api..example.com"})
	if err == nil {
		t.Fatal("expected invalid fqdn error")
	}

	if _, statErr := os.Stat(filepath.Join(cfg.WORHome, "domains", "com-example", "api")); !os.IsNotExist(statErr) {
		t.Fatalf("service directory should not be created, stat error = %v", statErr)
	}
}

func TestAddServiceUsesLongestMatchingDomain(t *testing.T) {
	t.Parallel()

	cfg := testConfig(t)
	addDomain(t, cfg, "example.com")
	addDomain(t, cfg, "example.co.th")
	manager := NewManager(cfg, fakeRuntimeChecker{})

	metadata, err := manager.Add(context.Background(), AddRequest{FQDN: "api.app.example.co.th"})
	if err != nil {
		t.Fatalf("add service: %v", err)
	}

	if metadata.DomainID != "th-co-example" {
		t.Fatalf("DomainID = %q", metadata.DomainID)
	}
	if metadata.ServiceID != "api-app" {
		t.Fatalf("ServiceID = %q", metadata.ServiceID)
	}
	if _, err := os.Stat(filepath.Join(cfg.WORHome, "domains", "th-co-example", "api-app", PublicDirName)); err != nil {
		t.Fatalf("public directory not created under matched domain: %v", err)
	}
}

func TestListServicesSortedByFQDN(t *testing.T) {
	t.Parallel()

	cfg := testConfig(t)
	addDomain(t, cfg, "example.com")
	manager := NewManager(cfg, fakeRuntimeChecker{})
	for _, fqdn := range []string{"z.example.com", "a.example.com", "api.example.com"} {
		if _, err := manager.Add(context.Background(), AddRequest{FQDN: fqdn}); err != nil {
			t.Fatalf("add service %q: %v", fqdn, err)
		}
	}

	items, err := manager.ListServices()
	if err != nil {
		t.Fatalf("list services: %v", err)
	}
	got := serviceNames(items)
	want := []string{"a.example.com", "api.example.com", "z.example.com"}
	if !sameStrings(got, want) {
		t.Fatalf("services = %#v", got)
	}
}

func TestListServicesByDomain(t *testing.T) {
	t.Parallel()

	cfg := testConfig(t)
	addDomain(t, cfg, "example.com")
	addDomain(t, cfg, "example.co.th")
	manager := NewManager(cfg, fakeRuntimeChecker{})
	if _, err := manager.Add(context.Background(), AddRequest{FQDN: "app.example.com"}); err != nil {
		t.Fatalf("add service: %v", err)
	}
	if _, err := manager.Add(context.Background(), AddRequest{FQDN: "app.example.co.th"}); err != nil {
		t.Fatalf("add service: %v", err)
	}

	items, err := manager.ListServicesByDomain("example.co.th")
	if err != nil {
		t.Fatalf("list services by domain: %v", err)
	}
	got := serviceNames(items)
	want := []string{"app.example.co.th"}
	if !sameStrings(got, want) {
		t.Fatalf("services = %#v", got)
	}
}

func TestGetServiceByFQDN(t *testing.T) {
	t.Parallel()

	cfg := testConfig(t)
	addDomain(t, cfg, "example.com")
	manager := NewManager(cfg, fakeRuntimeChecker{})
	if _, err := manager.Add(context.Background(), AddRequest{FQDN: "app.example.com"}); err != nil {
		t.Fatalf("add service: %v", err)
	}

	metadata, err := manager.GetServiceByFQDN("app.example.com")
	if err != nil {
		t.Fatalf("get service: %v", err)
	}
	if metadata.FQDN != "app.example.com" {
		t.Fatalf("FQDN = %q", metadata.FQDN)
	}
}

func TestListServicesReturnsMetadataError(t *testing.T) {
	t.Parallel()

	cfg := testConfig(t)
	addDomain(t, cfg, "example.com")
	badPath := filepath.Join(cfg.WORHome, "domains", "com-example", "bad")
	if err := os.MkdirAll(badPath, 0o755); err != nil {
		t.Fatalf("create bad service dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(badPath, MetadataFileName), []byte("{not-json\n"), 0o600); err != nil {
		t.Fatalf("write bad metadata: %v", err)
	}

	_, err := NewManager(cfg, fakeRuntimeChecker{}).ListServices()
	if err == nil {
		t.Fatal("expected metadata error")
	}
	if !strings.Contains(err.Error(), "parse service metadata") {
		t.Fatalf("error = %v", err)
	}
}

func TestAddServiceRequiresDomainCatalogMatch(t *testing.T) {
	t.Parallel()

	cfg := testConfig(t)
	manager := NewManager(cfg, fakeRuntimeChecker{})

	_, err := manager.Add(context.Background(), AddRequest{FQDN: "app.example.com"})
	if err == nil {
		t.Fatal("expected missing domain error")
	}
	if !strings.Contains(err.Error(), "wor domain add <domain>") {
		t.Fatalf("error should suggest domain add: %v", err)
	}
}

func TestRuntimeValidationRunsBeforeCreatingService(t *testing.T) {
	t.Parallel()

	cfg := testConfig(t)
	addDomain(t, cfg, "example.com")
	manager := NewManager(cfg, fakeRuntimeChecker{})

	_, err := manager.Add(context.Background(), AddRequest{
		FQDN:         "app.example.com",
		TemplateName: TemplateNode,
	})
	if err == nil {
		t.Fatal("expected runtime validation error")
	}
	if !strings.Contains(err.Error(), "missing runtime requirements") {
		t.Fatalf("error = %v", err)
	}

	servicePath := filepath.Join(cfg.WORHome, "domains", "com-example", "app")
	if _, statErr := os.Stat(servicePath); !os.IsNotExist(statErr) {
		t.Fatalf("service directory should not be created, stat error = %v", statErr)
	}
}

func TestStaticRuntimeTemplateUsesApplicationRoute(t *testing.T) {
	t.Parallel()

	cfg := testConfig(t)
	addDomain(t, cfg, "example.com")
	manager := NewManager(cfg, fakeRuntimeChecker{
		results: map[string]worRuntime.CheckResult{
			"Go": okRuntime("Go", "go"),
		},
	})

	metadata, err := manager.Add(context.Background(), AddRequest{
		FQDN:             "api.example.com",
		TemplateName:     TemplateStaticGo,
		ApplicationRoute: "/backend",
	})
	if err != nil {
		t.Fatalf("add service: %v", err)
	}

	if metadata.ApplicationRoute != "/backend" {
		t.Fatalf("ApplicationRoute = %q", metadata.ApplicationRoute)
	}
	stored := readServiceMetadata(t, filepath.Join(metadata.ServicePath, MetadataFileName))
	if stored.ApplicationRoute != "/backend" {
		t.Fatalf("stored ApplicationRoute = %q", stored.ApplicationRoute)
	}
}

func TestApplicationRouteValidation(t *testing.T) {
	t.Parallel()

	template, ok := GetTemplate(TemplateStaticGo)
	if !ok {
		t.Fatal("missing static-go template")
	}

	tests := []struct {
		name    string
		route   string
		want    string
		wantErr bool
	}{
		{name: "missing leading slash", route: "app", wantErr: true},
		{name: "root route", route: "/", wantErr: true},
		{name: "whitespace", route: "/bad route", wantErr: true},
		{name: "trailing slash", route: "/backend/", want: "/backend"},
		{name: "multiple trailing slash", route: "/backend///", want: "/backend"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := resolveApplicationRoute(template, tt.route)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("route = %q, expected error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolve route: %v", err)
			}
			if got != tt.want {
				t.Fatalf("route = %q", got)
			}
		})
	}
}

func TestStaticRuntimeTemplateDefaultsApplicationRoute(t *testing.T) {
	t.Parallel()

	cfg := testConfig(t)
	addDomain(t, cfg, "example.com")
	manager := NewManager(cfg, fakeRuntimeChecker{
		results: map[string]worRuntime.CheckResult{
			"Python": okRuntime("Python", "python3"),
		},
	})

	metadata, err := manager.Add(context.Background(), AddRequest{
		FQDN:         "api.example.com",
		TemplateName: TemplateStaticPython,
	})
	if err != nil {
		t.Fatalf("add service: %v", err)
	}

	if metadata.ApplicationRoute != DefaultApplicationRoute {
		t.Fatalf("ApplicationRoute = %q", metadata.ApplicationRoute)
	}
}

func testConfig(t *testing.T) config.Config {
	t.Helper()
	return config.Config{WORHome: t.TempDir()}
}

func addDomain(t *testing.T, cfg config.Config, name string) {
	t.Helper()
	if _, err := domain.NewManager(cfg).Add(domain.AddRequest{Domain: name}); err != nil {
		t.Fatalf("add domain %q: %v", name, err)
	}
}

func readServiceMetadata(t *testing.T, path string) Metadata {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read service metadata: %v", err)
	}
	var metadata Metadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatalf("parse service metadata: %v", err)
	}
	return metadata
}

func okRuntime(name, command string) worRuntime.CheckResult {
	return worRuntime.CheckResult{
		Name:        name,
		Command:     command,
		Path:        filepath.Join("/usr/bin", command),
		Version:     "ok",
		Status:      worRuntime.StatusOK,
		Requirement: worRuntime.RequirementRequired,
	}
}

func serviceNames(items []Metadata) []string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.FQDN)
	}
	return names
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
