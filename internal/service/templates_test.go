package service

import (
	"reflect"
	"testing"
)

func TestListTemplates(t *testing.T) {
	t.Parallel()

	templates := ListTemplates()
	got := make([]string, 0, len(templates))
	for _, template := range templates {
		got = append(got, template.Name)
	}

	want := []string{
		TemplateStatic,
		TemplateStaticNode,
		TemplateStaticGo,
		TemplateStaticPython,
		TemplateNode,
		TemplateGo,
		TemplatePHP,
		TemplatePython,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("templates = %#v", got)
	}
}

func TestDefaultTemplateIsStatic(t *testing.T) {
	t.Parallel()

	if got := DefaultTemplate().Name; got != TemplateStatic {
		t.Fatalf("DefaultTemplate = %q", got)
	}
}

func TestRegistryDoesNotIncludeStaticPHP(t *testing.T) {
	t.Parallel()

	if _, ok := GetTemplate("static-php"); ok {
		t.Fatal("static-php should not be registered")
	}
}

func TestRuntimeRequirements(t *testing.T) {
	t.Parallel()

	tests := map[string][]string{
		TemplateStatic:       nil,
		TemplateStaticNode:   {RuntimeNode},
		TemplateStaticGo:     {RuntimeGo},
		TemplateStaticPython: {RuntimePython},
		TemplateNode:         {RuntimeNode, RuntimeNPM},
		TemplateGo:           {RuntimeGo},
		TemplatePHP:          {RuntimePHP, RuntimePHPFPM},
		TemplatePython:       {RuntimePython},
	}

	for name, want := range tests {
		template, ok := GetTemplate(name)
		if !ok {
			t.Fatalf("missing template %q", name)
		}
		if got := RuntimeRequirements(template); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s runtime requirements = %#v", name, got)
		}
	}
}

func TestProcessRequirements(t *testing.T) {
	t.Parallel()

	tests := map[string][]string{
		TemplateStatic:       nil,
		TemplateStaticNode:   {ProcessPM2},
		TemplateStaticGo:     {ProcessPlatform},
		TemplateStaticPython: {ProcessPlatform},
		TemplateNode:         {ProcessPM2},
		TemplateGo:           {ProcessPlatform},
		TemplatePHP:          {ProcessWebServerPHPFPM},
		TemplatePython:       {ProcessPlatform},
	}

	for name, want := range tests {
		template, ok := GetTemplate(name)
		if !ok {
			t.Fatalf("missing template %q", name)
		}
		if got := ProcessRequirements(template); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s process requirements = %#v", name, got)
		}
	}
}

func TestStaticRuntimeTemplatesDefaultApplicationRoute(t *testing.T) {
	t.Parallel()

	for _, name := range []string{TemplateStaticNode, TemplateStaticGo, TemplateStaticPython} {
		template, ok := GetTemplate(name)
		if !ok {
			t.Fatalf("missing template %q", name)
		}
		if template.DefaultApplicationRoute != DefaultApplicationRoute {
			t.Fatalf("%s default route = %q", name, template.DefaultApplicationRoute)
		}
		if !hasRoute(template.RouteModel, DefaultApplicationRoute) {
			t.Fatalf("%s route model missing %s", name, DefaultApplicationRoute)
		}
	}
}

func TestAllTemplatesHavePublicDirectory(t *testing.T) {
	t.Parallel()

	for _, template := range ListTemplates() {
		if !template.HasPublicDirectory {
			t.Fatalf("%s should have a public directory", template.Name)
		}
	}
}

func TestPlatformProcessProvider(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"linux":   ProcessSystemd,
		"darwin":  ProcessLaunchd,
		"windows": ProcessWindowsService,
	}
	for osName, want := range tests {
		got, ok := PlatformProcessProvider(osName)
		if !ok {
			t.Fatalf("%s provider should be supported", osName)
		}
		if got != want {
			t.Fatalf("%s provider = %q", osName, got)
		}
	}
	if got, ok := PlatformProcessProvider("plan9"); ok || got != "" {
		t.Fatalf("unsupported provider = %q, %t", got, ok)
	}
}

func hasRoute(routes []Route, path string) bool {
	for _, route := range routes {
		if route.Path == path {
			return true
		}
	}
	return false
}
