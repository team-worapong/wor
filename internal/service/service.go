package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/team-worapong/wor/internal/config"
	"github.com/team-worapong/wor/internal/domain"
	worRuntime "github.com/team-worapong/wor/internal/runtime"
)

const (
	MetadataFileName = "service.json"
	PublicDirName    = "public"
)

type AddRequest struct {
	FQDN             string
	TemplateName     string
	ApplicationRoute string
}

type ListRequest struct {
	Domain string
}

type Metadata struct {
	ServiceID        string `json:"service_id"`
	DomainID         string `json:"domain_id"`
	DomainName       string `json:"domain_name"`
	FQDN             string `json:"fqdn"`
	ServiceTemplate  string `json:"service_template"`
	ApplicationRoute string `json:"application_route,omitempty"`
	PublicPath       string `json:"public_path"`
	ServicePath      string `json:"service_path"`
	CreatedAt        string `json:"created_at"`
}

type RuntimeChecker interface {
	Check(ctx context.Context, target worRuntime.Target) worRuntime.CheckResult
}

type Manager struct {
	config  config.Config
	catalog domain.Catalog
	checker RuntimeChecker
	now     func() time.Time
}

func NewManager(cfg config.Config, checker RuntimeChecker) Manager {
	return Manager{
		config:  cfg,
		catalog: domain.NewCatalog(cfg),
		checker: checker,
		now:     time.Now,
	}
}

func (m Manager) ListServices() ([]Metadata, error) {
	domains, err := m.catalog.ListDomains()
	if err != nil {
		return nil, err
	}

	services := make([]Metadata, 0)
	for _, metadata := range domains {
		items, err := readServicesInDomain(metadata)
		if err != nil {
			return nil, err
		}
		services = append(services, items...)
	}
	sortServicesByFQDN(services)
	return services, nil
}

func (m Manager) ListServicesByDomain(domainName string) ([]Metadata, error) {
	metadata, err := m.catalog.GetDomainByName(domainName)
	if err != nil {
		return nil, err
	}
	services, err := readServicesInDomain(metadata)
	if err != nil {
		return nil, err
	}
	sortServicesByFQDN(services)
	return services, nil
}

func (m Manager) GetServiceByFQDN(fqdn string) (Metadata, error) {
	fqdn, _, err := domain.Normalize(fqdn)
	if err != nil {
		return Metadata{}, err
	}

	services, err := m.ListServices()
	if err != nil {
		return Metadata{}, err
	}
	for _, metadata := range services {
		if metadata.FQDN == fqdn {
			return metadata, nil
		}
	}
	return Metadata{}, fmt.Errorf("service %q not found", fqdn)
}

func TemplateForService(metadata Metadata) (Template, error) {
	template, ok := GetTemplate(metadata.ServiceTemplate)
	if !ok {
		return Template{}, fmt.Errorf("service %q references unknown template %q", metadata.FQDN, metadata.ServiceTemplate)
	}
	return template, nil
}

func (m Manager) Add(ctx context.Context, request AddRequest) (Metadata, error) {
	fqdn, _, err := domain.Normalize(request.FQDN)
	if err != nil {
		return Metadata{}, err
	}

	template, err := selectTemplate(request.TemplateName)
	if err != nil {
		return Metadata{}, err
	}

	matchedDomain, ok, err := m.catalog.FindLongestMatch(fqdn)
	if err != nil {
		return Metadata{}, err
	}
	if !ok {
		return Metadata{}, fmt.Errorf("domain not found for %s; run wor domain add <domain> first", fqdn)
	}

	serviceID, err := ServiceID(fqdn, matchedDomain.DomainName)
	if err != nil {
		return Metadata{}, err
	}

	applicationRoute, err := resolveApplicationRoute(template, request.ApplicationRoute)
	if err != nil {
		return Metadata{}, err
	}

	if err := m.validateRuntimeRequirements(ctx, template); err != nil {
		return Metadata{}, err
	}

	servicePath := filepath.Join(matchedDomain.DomainPath, serviceID)
	publicPath := filepath.Join(servicePath, PublicDirName)
	metadata := Metadata{
		ServiceID:        serviceID,
		DomainID:         matchedDomain.DomainID,
		DomainName:       matchedDomain.DomainName,
		FQDN:             fqdn,
		ServiceTemplate:  template.Name,
		ApplicationRoute: applicationRoute,
		PublicPath:       publicPath,
		ServicePath:      servicePath,
		CreatedAt:        m.now().UTC().Format(time.RFC3339),
	}

	if err := inspectServiceMetadata(filepath.Join(servicePath, MetadataFileName)); err != nil {
		return Metadata{}, err
	}
	if err := os.MkdirAll(publicPath, 0o755); err != nil {
		return Metadata{}, fmt.Errorf("create public directory: %w", err)
	}
	if err := writeServiceMetadata(filepath.Join(servicePath, MetadataFileName), metadata); err != nil {
		return Metadata{}, err
	}
	return metadata, nil
}

func ServiceID(fqdn, domainName string) (string, error) {
	fqdn, _, err := domain.Normalize(fqdn)
	if err != nil {
		return "", err
	}
	domainName, _, err = domain.Normalize(domainName)
	if err != nil {
		return "", err
	}
	if fqdn == domainName {
		return "", fmt.Errorf("service fqdn %q must include a subdomain before %q", fqdn, domainName)
	}
	suffix := "." + domainName
	if !strings.HasSuffix(fqdn, suffix) {
		return "", fmt.Errorf("service fqdn %q does not belong to domain %q", fqdn, domainName)
	}
	subdomain := strings.TrimSuffix(fqdn, suffix)
	return strings.Join(strings.Split(subdomain, "."), "-"), nil
}

func selectTemplate(name string) (Template, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return DefaultTemplate(), nil
	}
	template, ok := GetTemplate(name)
	if !ok {
		return Template{}, fmt.Errorf("unknown service template %q", name)
	}
	return template, nil
}

func resolveApplicationRoute(template Template, requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return template.DefaultApplicationRoute, nil
	}
	if !supportsCustomApplicationRoute(template) {
		return "", fmt.Errorf("template %q does not support --app-route", template.Name)
	}
	if !strings.HasPrefix(requested, "/") {
		return "", errors.New("application route must start with /")
	}
	if requested == "/" {
		return "", errors.New("application route must not be /")
	}
	if strings.ContainsAny(requested, " \t\r\n") {
		return "", errors.New("application route must not contain whitespace")
	}
	normalized := strings.TrimRight(requested, "/")
	if normalized == "" {
		return "", errors.New("application route must not be /")
	}
	return normalized, nil
}

func supportsCustomApplicationRoute(template Template) bool {
	switch template.Name {
	case TemplateStaticNode, TemplateStaticGo, TemplateStaticPython:
		return true
	default:
		return false
	}
}

func (m Manager) validateRuntimeRequirements(ctx context.Context, template Template) error {
	requirements := RuntimeRequirements(template)
	if len(requirements) == 0 {
		return nil
	}
	if m.checker == nil {
		return errors.New("runtime checker is required")
	}

	missing := make([]string, 0)
	for _, requirement := range requirements {
		target, ok := runtimeTarget(requirement)
		if !ok {
			return fmt.Errorf("runtime requirement %q is not supported", requirement)
		}
		result := m.checker.Check(ctx, target)
		if result.Status != worRuntime.StatusOK {
			missing = append(missing, requirement)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing runtime requirements for template %q: %s", template.Name, strings.Join(missing, ", "))
	}
	return nil
}

func runtimeTarget(requirement string) (worRuntime.Target, bool) {
	switch requirement {
	case RuntimeNode:
		return worRuntime.Target{
			Name:          "Node.js",
			Command:       "node",
			VersionArgs:   []string{"--version"},
			VersionSource: worRuntime.VersionFromCommand,
			Requirement:   worRuntime.RequirementRequired,
			Category:      worRuntime.CategoryOptionalRuntimes,
		}, true
	case RuntimeNPM:
		return worRuntime.Target{
			Name:          "npm",
			Command:       "npm",
			VersionArgs:   []string{"--version"},
			VersionSource: worRuntime.VersionFromCommand,
			Requirement:   worRuntime.RequirementRequired,
			Category:      worRuntime.CategoryOptionalRuntimes,
		}, true
	case RuntimeGo:
		return worRuntime.Target{
			Name:          "Go",
			Command:       "go",
			VersionArgs:   []string{"version"},
			VersionSource: worRuntime.VersionFromCommand,
			Requirement:   worRuntime.RequirementRequired,
			Category:      worRuntime.CategoryOptionalRuntimes,
		}, true
	case RuntimePHP:
		return worRuntime.Target{
			Name:          "PHP",
			Command:       "php",
			VersionArgs:   []string{"--version"},
			VersionSource: worRuntime.VersionFromCommand,
			Requirement:   worRuntime.RequirementRequired,
			Category:      worRuntime.CategoryOptionalRuntimes,
		}, true
	case RuntimePHPFPM:
		return worRuntime.Target{
			Name:          "PHP-FPM",
			Command:       "php-fpm",
			Commands:      []string{"php-fpm", "php-fpm8.4", "php-fpm8.3", "php-fpm8.2", "php-fpm8.1", "php-fpm8.0"},
			VersionArgs:   []string{"--version"},
			VersionSource: worRuntime.VersionFromCommand,
			Requirement:   worRuntime.RequirementRequired,
			Category:      worRuntime.CategoryOptionalRuntimes,
		}, true
	case RuntimePython:
		return worRuntime.Target{
			Name:          "Python",
			Command:       "python",
			Commands:      []string{"python3", "python"},
			VersionArgs:   []string{"--version"},
			VersionSource: worRuntime.VersionFromCommand,
			Requirement:   worRuntime.RequirementRequired,
			Category:      worRuntime.CategoryOptionalRuntimes,
		}, true
	default:
		return worRuntime.Target{}, false
	}
}

func inspectServiceMetadata(path string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("service already exists")
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("inspect service metadata: %w", err)
	}
	return nil
}

func ReadMetadata(path string) (Metadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Metadata{}, fmt.Errorf("read service metadata %s: %w", path, err)
	}

	var metadata Metadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return Metadata{}, fmt.Errorf("parse service metadata %s: %w", path, err)
	}
	if strings.TrimSpace(metadata.FQDN) == "" || strings.TrimSpace(metadata.ServiceTemplate) == "" {
		return Metadata{}, fmt.Errorf("service metadata %s is incomplete", path)
	}
	return metadata, nil
}

func writeServiceMetadata(path string, metadata Metadata) error {
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("encode service metadata: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write service metadata: %w", err)
	}
	return nil
}

func readServicesInDomain(metadata domain.Metadata) ([]Metadata, error) {
	entries, err := os.ReadDir(metadata.DomainPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("domain path for %q does not exist: %w", metadata.DomainName, err)
		}
		return nil, fmt.Errorf("read services for domain %q: %w", metadata.DomainName, err)
	}

	services := make([]Metadata, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		serviceMetadata, err := ReadMetadata(filepath.Join(metadata.DomainPath, entry.Name(), MetadataFileName))
		if err != nil {
			return nil, err
		}
		services = append(services, serviceMetadata)
	}
	return services, nil
}

func sortServicesByFQDN(services []Metadata) {
	sort.Slice(services, func(i, j int) bool {
		return services[i].FQDN < services[j].FQDN
	})
}
