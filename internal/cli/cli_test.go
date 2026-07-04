package cli

import (
	"testing"

	"github.com/team-worapong/wor/internal/service"
)

func TestParseDomainAddRequest(t *testing.T) {
	t.Parallel()

	request, err := parseDomainAddRequest([]string{"add", "example.com"})
	if err != nil {
		t.Fatalf("parse domain add: %v", err)
	}
	if request.Domain != "example.com" {
		t.Fatalf("Domain = %q", request.Domain)
	}

	if _, err := parseDomainAddRequest([]string{"list"}); err == nil {
		t.Fatal("expected usage error")
	}
}

func TestParseServiceAddRequest(t *testing.T) {
	t.Parallel()

	request, err := parseServiceAddRequest([]string{
		"add",
		"app.example.com",
		"--template",
		service.TemplateStaticGo,
		"--app-route=/backend",
	})
	if err != nil {
		t.Fatalf("parse service add: %v", err)
	}
	if request.FQDN != "app.example.com" {
		t.Fatalf("FQDN = %q", request.FQDN)
	}
	if request.TemplateName != service.TemplateStaticGo {
		t.Fatalf("TemplateName = %q", request.TemplateName)
	}
	if request.ApplicationRoute != "/backend" {
		t.Fatalf("ApplicationRoute = %q", request.ApplicationRoute)
	}
}
