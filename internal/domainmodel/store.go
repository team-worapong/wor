package domainmodel

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"wor/internal/osutil"
)

// Store gives every command access to the domain registry rooted at a
// resolved WOR_HOME/domains directory.
type Store struct {
	DomainsDir string
}

func NewStore(domainsDir string) *Store { return &Store{DomainsDir: domainsDir} }

func (s *Store) DomainDir(domain string) string { return filepath.Join(s.DomainsDir, domain) }
func (s *Store) ServicesPath(domain string) string {
	return filepath.Join(s.DomainDir(domain), "services.config.json")
}
func (s *Store) DatabasesPath(domain string) string {
	return filepath.Join(s.DomainDir(domain), "databases.config.json")
}
func (s *Store) BackupConfigPath(domain string) string {
	return filepath.Join(s.DomainDir(domain), "backup.config.json")
}
func (s *Store) ServiceDir(domain, service string) string {
	return filepath.Join(s.DomainDir(domain), service)
}

func writeJSON(path string, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return osutil.WriteFilePrivileged(path, data)
}

func readJSON(path string, v interface{}) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// LoadServices reads services.config.json for domain; returns an empty,
// initialized config (not an error) if the file does not exist yet.
func (s *Store) LoadServices(domain string) (*ServicesConfig, error) {
	path := s.ServicesPath(domain)
	cfg := &ServicesConfig{Domain: domain, Services: []Service{}}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return cfg, nil
	}
	if err := readJSON(path, cfg); err != nil {
		return nil, fmt.Errorf("services.config.json for domain %s: %w", domain, err)
	}
	return cfg, nil
}

func (s *Store) SaveServices(cfg *ServicesConfig) error {
	return writeJSON(s.ServicesPath(cfg.Domain), cfg)
}

func (s *Store) LoadDatabases(domain string) (*DatabasesConfig, error) {
	path := s.DatabasesPath(domain)
	cfg := &DatabasesConfig{Domain: domain, Databases: []Database{}}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return cfg, nil
	}
	if err := readJSON(path, cfg); err != nil {
		return nil, fmt.Errorf("databases.config.json for domain %s: %w", domain, err)
	}
	return cfg, nil
}

func (s *Store) SaveDatabases(cfg *DatabasesConfig) error {
	return writeJSON(s.DatabasesPath(cfg.Domain), cfg)
}

func (s *Store) LoadBackupConfig(domain string) (*BackupConfig, error) {
	path := s.BackupConfigPath(domain)
	def := DefaultBackupConfig()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return &def, nil
	}
	cfg := &def
	if err := readJSON(path, cfg); err != nil {
		return nil, fmt.Errorf("backup.config.json for domain %s: %w", domain, err)
	}
	return cfg, nil
}

// MakeDomainFiles ensures the domain directory and its three config
// files exist, matching lib/paths.sh make_domain_files(). Store is the
// internal foundation layer every command builds on -- it validates the
// domain slug itself rather than trusting callers (e.g. cmdDomain) to
// have already done so, so a stray "../"-style or otherwise malformed
// domain id can never reach the filesystem.
func (s *Store) MakeDomainFiles(domain string) error {
	if err := RequireSlug(domain); err != nil {
		return err
	}
	if err := osutil.EnsureDir(s.DomainDir(domain)); err != nil {
		return err
	}
	if _, err := os.Stat(s.ServicesPath(domain)); os.IsNotExist(err) {
		if err := s.SaveServices(&ServicesConfig{Domain: domain, Services: []Service{}}); err != nil {
			return err
		}
	}
	if _, err := os.Stat(s.DatabasesPath(domain)); os.IsNotExist(err) {
		if err := s.SaveDatabases(&DatabasesConfig{Domain: domain, Databases: []Database{}}); err != nil {
			return err
		}
	}
	if _, err := os.Stat(s.BackupConfigPath(domain)); os.IsNotExist(err) {
		def := DefaultBackupConfig()
		if err := writeJSON(s.BackupConfigPath(domain), &def); err != nil {
			return err
		}
	}
	return nil
}

// ListDomains returns every domain directory name under DomainsDir.
func (s *Store) ListDomains() ([]string, error) {
	entries, err := os.ReadDir(s.DomainsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

// FindService returns the service named `name` within a services
// config, or nil if not present.
func (c *ServicesConfig) FindService(name string) *Service {
	for i := range c.Services {
		if c.Services[i].Name == name {
			return &c.Services[i]
		}
	}
	return nil
}

// ServiceExists reports whether domain/service is registered.
func (s *Store) ServiceExists(domain, service string) bool {
	cfg, err := s.LoadServices(domain)
	if err != nil {
		return false
	}
	return cfg.FindService(service) != nil
}

// AddService registers a new service, mirroring lib/hosts.sh
// add_service_to_config(). Returns an error if the service already
// exists (matches the shell version's exit code 10 case). entryPoint
// may be empty, in which case DefaultEntryPoint(template) is used.
func (s *Store) AddService(domain, name, host string, port int, template, entryPoint string) error {
	cfg, err := s.LoadServices(domain)
	if err != nil {
		return err
	}
	if cfg.FindService(name) != nil {
		return fmt.Errorf("service already exists: %s/%s", domain, name)
	}
	hasSupervisor := TemplateRequiresProcessSupervisor(template)
	hasPHP := TemplateRequiresPHP(template)
	provider := ProcessProviderFor(template)
	if entryPoint == "" {
		entryPoint = DefaultEntryPoint(template)
	}
	svc := Service{
		Name:         name,
		Enabled:      true,
		Type:         template,
		Hosts:        []string{},
		PublicPath:   "public",
		DocumentRoot: "public",
		Runtime: ServiceRuntime{
			Node:    NodeTemplates[template],
			Go:      GoTemplates[template],
			Python:  PythonTemplates[template],
			PHP:     hasPHP,
			PM2:     provider == "pm2",
			Systemd: provider == "systemd",
			Port:    hasSupervisor,
		},
		EntryPoint: entryPoint,
	}
	if host != "" {
		svc.Hosts = []string{host}
	}
	if hasSupervisor {
		svc.Port = port
		svc.Env = map[string]string{"PORT": fmt.Sprintf("%d", port)}
		if NodeTemplates[template] {
			svc.Env["NODE_ENV"] = "production"
		}
	}
	cfg.Services = append(cfg.Services, svc)
	return s.SaveServices(cfg)
}

func (s *Store) RemoveService(domain, name string) error {
	cfg, err := s.LoadServices(domain)
	if err != nil {
		return err
	}
	out := cfg.Services[:0]
	for _, svc := range cfg.Services {
		if svc.Name != name {
			out = append(out, svc)
		}
	}
	cfg.Services = out
	return s.SaveServices(cfg)
}

func (s *Store) AddHostToService(domain, service, host string) error {
	cfg, err := s.LoadServices(domain)
	if err != nil {
		return err
	}
	svc := cfg.FindService(service)
	if svc == nil {
		return fmt.Errorf("service not found: %s/%s", domain, service)
	}
	for _, h := range svc.Hosts {
		if h == host {
			return s.SaveServices(cfg)
		}
	}
	svc.Hosts = append(svc.Hosts, host)
	return s.SaveServices(cfg)
}

// RemoveHostFromServices scrubs host from every service in every
// domain, matching lib/hosts.sh remove_host_from_services().
func (s *Store) RemoveHostFromServices(host string) error {
	domains, err := s.ListDomains()
	if err != nil {
		return err
	}
	for _, domain := range domains {
		cfg, err := s.LoadServices(domain)
		if err != nil {
			continue
		}
		changed := false
		for i := range cfg.Services {
			hosts := cfg.Services[i].Hosts
			out := hosts[:0]
			for _, h := range hosts {
				if h != host {
					out = append(out, h)
				} else {
					changed = true
				}
			}
			cfg.Services[i].Hosts = out
		}
		if changed {
			if err := s.SaveServices(cfg); err != nil {
				return err
			}
		}
	}
	return nil
}

// ResolveHost searches every domain's services for one whose Hosts list
// contains host, returning "domain/service". Port of lib/hosts.sh
// resolve_host().
func (s *Store) ResolveHost(host string) (string, bool) {
	domains, err := s.ListDomains()
	if err != nil {
		return "", false
	}
	for _, domain := range domains {
		cfg, err := s.LoadServices(domain)
		if err != nil {
			continue
		}
		for _, svc := range cfg.Services {
			for _, h := range svc.Hosts {
				if h == host {
					return domain + "/" + svc.Name, true
				}
			}
		}
	}
	return "", false
}

// ListServiceTargets lists every "domain/service" pair, for the
// interactive `wor host add` target picker.
func (s *Store) ListServiceTargets() ([]string, error) {
	domains, err := s.ListDomains()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, domain := range domains {
		cfg, err := s.LoadServices(domain)
		if err != nil {
			continue
		}
		for _, svc := range cfg.Services {
			out = append(out, domain+"/"+svc.Name)
		}
	}
	return out, nil
}

// ListAllServices returns every registered service across every domain,
// domains sorted alphabetically and services in services.config.json
// order within each domain. Domains whose services.config.json fails to
// load are skipped (matching ListServiceTargets' tolerance).
func (s *Store) ListAllServices() ([]ServiceRef, error) {
	domains, err := s.ListDomains()
	if err != nil {
		return nil, err
	}
	var out []ServiceRef
	for _, domain := range domains {
		cfg, err := s.LoadServices(domain)
		if err != nil {
			continue
		}
		for _, svc := range cfg.Services {
			out = append(out, ServiceRef{Domain: domain, Service: svc})
		}
	}
	return out, nil
}

func (s *Store) GetServiceType(domain, service string) string {
	cfg, err := s.LoadServices(domain)
	if err != nil {
		return "static"
	}
	if svc := cfg.FindService(service); svc != nil {
		return svc.Type
	}
	return "static"
}

// GetServiceEntryPoint returns the registered entry point for
// domain/service, falling back to the template's default if the
// service has none recorded (e.g. config written before EntryPoint
// existed) or the service can't be found.
func (s *Store) GetServiceEntryPoint(domain, service string) string {
	cfg, err := s.LoadServices(domain)
	if err != nil {
		return ""
	}
	svc := cfg.FindService(service)
	if svc == nil {
		return ""
	}
	if svc.EntryPoint != "" {
		return svc.EntryPoint
	}
	return DefaultEntryPoint(svc.Type)
}

func (s *Store) GetServicePort(domain, service string) (int, error) {
	cfg, err := s.LoadServices(domain)
	if err != nil {
		return 0, err
	}
	svc := cfg.FindService(service)
	if svc == nil {
		return 0, fmt.Errorf("service not found: %s/%s", domain, service)
	}
	if svc.Port != 0 {
		return svc.Port, nil
	}
	if v, ok := svc.Env["PORT"]; ok {
		var p int
		fmt.Sscanf(v, "%d", &p)
		if p != 0 {
			return p, nil
		}
	}
	return 3000, nil
}

// GetServicePHPVersion returns domain/service's per-service php-fpm
// version, or "" if it has none recorded (not a php service, service
// not found, or still on the host-wide PHP_FPM_ENDPOINT fallback).
func (s *Store) GetServicePHPVersion(domain, service string) string {
	cfg, err := s.LoadServices(domain)
	if err != nil {
		return ""
	}
	if svc := cfg.FindService(service); svc != nil {
		return svc.PHPVersion
	}
	return ""
}

// SetServicePHPFPM records that domain/service now has its own
// dedicated php-fpm pool: the PHP-FPM version it runs under, the group
// its pool user was granted document-root access through (see
// internal/phpfpm.GrantGroupAccess), and an optional pm.max_children
// override (0 keeps phpfpm.DefaultMaxChildren). Callers should only
// call this after the pool, its unix user, and its group access have
// all actually been created -- see Service.PHPVersion's doc comment
// for why this is opt-in rather than applied retroactively.
func (s *Store) SetServicePHPFPM(domain, service, version, poolGroup string, maxChildren int) error {
	cfg, err := s.LoadServices(domain)
	if err != nil {
		return err
	}
	svc := cfg.FindService(service)
	if svc == nil {
		return fmt.Errorf("service not found: %s/%s", domain, service)
	}
	svc.PHPVersion = version
	svc.PHPPoolGroup = poolGroup
	svc.PHPMaxChildren = maxChildren
	return s.SaveServices(cfg)
}

// ClearServicePHPFPM removes domain/service's per-service php-fpm
// record, reverting it to the host-wide PHP_FPM_ENDPOINT fallback. Used
// when a per-service pool is torn down (e.g. `wor service remove`) --
// it does not itself remove the pool file, unix user, or group access;
// callers are expected to have already done that via internal/phpfpm.
func (s *Store) ClearServicePHPFPM(domain, service string) error {
	cfg, err := s.LoadServices(domain)
	if err != nil {
		return err
	}
	svc := cfg.FindService(service)
	if svc == nil {
		return fmt.Errorf("service not found: %s/%s", domain, service)
	}
	svc.PHPVersion = ""
	svc.PHPPoolGroup = ""
	svc.PHPMaxChildren = 0
	return s.SaveServices(cfg)
}

func (s *Store) ListHostsForService(domain, service string) ([]string, error) {
	cfg, err := s.LoadServices(domain)
	if err != nil {
		return nil, err
	}
	svc := cfg.FindService(service)
	if svc == nil {
		return nil, nil
	}
	return svc.Hosts, nil
}

// SetServiceDomainMetadata records whether a service's hosts are
// "local" or "public", and appends a hosts-file entry note. Port of
// lib/hosts.sh set_service_domain_metadata().
func (s *Store) SetServiceDomainMetadata(domain, service, domainType, hostsEntry string) error {
	cfg, err := s.LoadServices(domain)
	if err != nil {
		return err
	}
	svc := cfg.FindService(service)
	if svc == nil {
		return fmt.Errorf("service not found: %s/%s", domain, service)
	}
	if svc.DomainType == "" {
		svc.DomainType = domainType
	}
	if domainType == "local" && hostsEntry != "" {
		found := false
		for _, e := range svc.HostsEntries {
			if e == hostsEntry {
				found = true
				break
			}
		}
		if !found {
			svc.HostsEntries = append(svc.HostsEntries, hostsEntry)
		}
	}
	return s.SaveServices(cfg)
}

func (s *Store) ServiceDomainType(domain, service string) string {
	cfg, err := s.LoadServices(domain)
	if err != nil {
		return ""
	}
	if svc := cfg.FindService(service); svc != nil {
		return svc.DomainType
	}
	return ""
}

// ConfiguredPorts collects every port in use across all services'
// `port`/`env.PORT` fields, for `find_next_port`.
func (s *Store) ConfiguredPorts() (map[int]bool, error) {
	domains, err := s.ListDomains()
	if err != nil {
		return nil, err
	}
	ports := map[int]bool{}
	for _, domain := range domains {
		cfg, err := s.LoadServices(domain)
		if err != nil {
			continue
		}
		for _, svc := range cfg.Services {
			if svc.Port != 0 {
				ports[svc.Port] = true
			}
			if v, ok := svc.Env["PORT"]; ok {
				var p int
				fmt.Sscanf(v, "%d", &p)
				if p != 0 {
					ports[p] = true
				}
			}
		}
	}
	return ports, nil
}
