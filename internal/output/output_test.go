package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestTableRendersHeadersAndRows(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	renderer := New(&stdout, &bytes.Buffer{}, FormatText)

	renderer.Table(
		[]string{"Name", "Status"},
		[][]string{{"Go", "ok"}, {"PM2", "missing"}},
	)

	got := stdout.String()
	for _, want := range []string{"Name", "Status", "Go", "ok", "PM2", "missing"} {
		if !strings.Contains(got, want) {
			t.Fatalf("table output missing %q:\n%s", want, got)
		}
	}
}

func TestWarningUsesStderr(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	renderer := New(&stdout, &stderr, FormatText)

	renderer.Warning("careful")

	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "[WARN] careful") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRenderDomainDoesNotExposeInternalIDs(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	renderer := New(&stdout, &bytes.Buffer{}, FormatText)

	renderer.RenderDomain(DomainDetails{
		Domain:    "example.com",
		Path:      "/tmp/wor/domains/com-example",
		CreatedAt: "2026-07-04T00:00:00Z",
		Services:  []string{"app.example.com"},
	})

	got := stdout.String()
	for _, want := range []string{"Domain", "example.com", "Services", "app.example.com"} {
		if !strings.Contains(got, want) {
			t.Fatalf("domain output missing %q:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"Domain ID"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("domain output should not include %q:\n%s", unwanted, got)
		}
	}
}

func TestRenderServiceShowsRequirementsWithoutInternalIDs(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	renderer := New(&stdout, &bytes.Buffer{}, FormatText)

	renderer.RenderService(ServiceDetails{
		FQDN:                "app.example.com",
		Template:            "node",
		RuntimeRequirements: []string{"node", "npm"},
		ProcessRequirements: []string{"pm2"},
		PublicPath:          "/tmp/wor/domains/com-example/app/public",
		ServicePath:         "/tmp/wor/domains/com-example/app",
		CreatedAt:           "2026-07-04T00:00:00Z",
	})

	got := stdout.String()
	for _, want := range []string{"app.example.com", "node, npm", "pm2", "Public Path", "Service Path"} {
		if !strings.Contains(got, want) {
			t.Fatalf("service output missing %q:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"Service ID", "Domain ID"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("service output should not include %q:\n%s", unwanted, got)
		}
	}
}
