package service

import "strings"

const (
	TemplateStatic       = "static"
	TemplateStaticNode   = "static-node"
	TemplateStaticGo     = "static-go"
	TemplateStaticPython = "static-python"
	TemplateNode         = "node"
	TemplateGo           = "go"
	TemplatePHP          = "php"
	TemplatePython       = "python"

	RuntimeNode   = "node"
	RuntimeNPM    = "npm"
	RuntimeGo     = "go"
	RuntimePHP    = "php"
	RuntimePHPFPM = "php-fpm"
	RuntimePython = "python"

	ProcessPM2             = "pm2"
	ProcessPlatform        = "platform"
	ProcessWebServerPHPFPM = "webserver-php-fpm"

	ProcessSystemd        = "systemd"
	ProcessLaunchd        = "launchd"
	ProcessWindowsService = "windows-service"

	RouteTargetPublic = "public"
	RouteTargetNode   = "node"
	RouteTargetGo     = "go"
	RouteTargetPHP    = "php"
	RouteTargetPython = "python"

	RootRoute               = "/"
	DefaultApplicationRoute = "/app"
)

type Template struct {
	Name                    string
	Description             string
	RuntimeRequirements     []string
	ProcessRequirements     []string
	HasPublicDirectory      bool
	DefaultApplicationRoute string
	RouteModel              []Route
}

type Route struct {
	Path   string
	Target string
}

func ListTemplates() []Template {
	items := templateDefinitions()
	templates := make([]Template, 0, len(items))
	for _, template := range items {
		templates = append(templates, cloneTemplate(template))
	}
	return templates
}

func GetTemplate(name string) (Template, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, template := range templateDefinitions() {
		if template.Name == name {
			return cloneTemplate(template), true
		}
	}
	return Template{}, false
}

func DefaultTemplate() Template {
	template, _ := GetTemplate(TemplateStatic)
	return template
}

func RuntimeRequirements(template Template) []string {
	return cloneStrings(template.RuntimeRequirements)
}

func ProcessRequirements(template Template) []string {
	return cloneStrings(template.ProcessRequirements)
}

func PlatformProcessProvider(osName string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(osName)) {
	case "linux":
		return ProcessSystemd, true
	case "darwin":
		return ProcessLaunchd, true
	case "windows":
		return ProcessWindowsService, true
	default:
		return "", false
	}
}

func templateDefinitions() []Template {
	return []Template{
		{
			Name:               TemplateStatic,
			Description:        "Static assets served from the public directory.",
			HasPublicDirectory: true,
			RouteModel: []Route{
				{Path: RootRoute, Target: RouteTargetPublic},
			},
		},
		{
			Name:                    TemplateStaticNode,
			Description:             "Static public assets with a Node.js application route.",
			RuntimeRequirements:     []string{RuntimeNode},
			ProcessRequirements:     []string{ProcessPM2},
			HasPublicDirectory:      true,
			DefaultApplicationRoute: DefaultApplicationRoute,
			RouteModel: []Route{
				{Path: RootRoute, Target: RouteTargetPublic},
				{Path: DefaultApplicationRoute, Target: RouteTargetNode},
			},
		},
		{
			Name:                    TemplateStaticGo,
			Description:             "Static public assets with a Go application route.",
			RuntimeRequirements:     []string{RuntimeGo},
			ProcessRequirements:     []string{ProcessPlatform},
			HasPublicDirectory:      true,
			DefaultApplicationRoute: DefaultApplicationRoute,
			RouteModel: []Route{
				{Path: RootRoute, Target: RouteTargetPublic},
				{Path: DefaultApplicationRoute, Target: RouteTargetGo},
			},
		},
		{
			Name:                    TemplateStaticPython,
			Description:             "Static public assets with a Python application route.",
			RuntimeRequirements:     []string{RuntimePython},
			ProcessRequirements:     []string{ProcessPlatform},
			HasPublicDirectory:      true,
			DefaultApplicationRoute: DefaultApplicationRoute,
			RouteModel: []Route{
				{Path: RootRoute, Target: RouteTargetPublic},
				{Path: DefaultApplicationRoute, Target: RouteTargetPython},
			},
		},
		{
			Name:                    TemplateNode,
			Description:             "Node.js application served at the root route.",
			RuntimeRequirements:     []string{RuntimeNode, RuntimeNPM},
			ProcessRequirements:     []string{ProcessPM2},
			HasPublicDirectory:      true,
			DefaultApplicationRoute: RootRoute,
			RouteModel: []Route{
				{Path: RootRoute, Target: RouteTargetNode},
			},
		},
		{
			Name:                    TemplateGo,
			Description:             "Go application served at the root route.",
			RuntimeRequirements:     []string{RuntimeGo},
			ProcessRequirements:     []string{ProcessPlatform},
			HasPublicDirectory:      true,
			DefaultApplicationRoute: RootRoute,
			RouteModel: []Route{
				{Path: RootRoute, Target: RouteTargetGo},
			},
		},
		{
			Name:                    TemplatePHP,
			Description:             "PHP application served from the public web root.",
			RuntimeRequirements:     []string{RuntimePHP, RuntimePHPFPM},
			ProcessRequirements:     []string{ProcessWebServerPHPFPM},
			HasPublicDirectory:      true,
			DefaultApplicationRoute: RootRoute,
			RouteModel: []Route{
				{Path: RootRoute, Target: RouteTargetPHP},
			},
		},
		{
			Name:                    TemplatePython,
			Description:             "Python application served at the root route.",
			RuntimeRequirements:     []string{RuntimePython},
			ProcessRequirements:     []string{ProcessPlatform},
			HasPublicDirectory:      true,
			DefaultApplicationRoute: RootRoute,
			RouteModel: []Route{
				{Path: RootRoute, Target: RouteTargetPython},
			},
		},
	}
}

func cloneTemplate(template Template) Template {
	template.RuntimeRequirements = cloneStrings(template.RuntimeRequirements)
	template.ProcessRequirements = cloneStrings(template.ProcessRequirements)
	template.RouteModel = cloneRoutes(template.RouteModel)
	return template
}

func cloneStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	cloned := make([]string, len(values))
	copy(cloned, values)
	return cloned
}

func cloneRoutes(values []Route) []Route {
	if len(values) == 0 {
		return nil
	}
	cloned := make([]Route, len(values))
	copy(cloned, values)
	return cloned
}
