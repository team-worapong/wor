// Package domainmodel is wor's config-file layer: the domain/service/
// database registry that the shell CLI stored as hand-rolled
// `module.exports = {...}` JS files (read and written by shelling out
// to `node`). The Go rewrite stores the same information as JSON
// (services.config.json, databases.config.json, backup.config.json)
// so no Node.js runtime is required to manage configuration -- a
// deliberate, documented change from wor-cli v1 (see DESIGN.md).
package domainmodel

import "wor/internal/osutil"

// ServiceRuntime flags which runtime features a service template needs.
type ServiceRuntime struct {
	Node    bool `json:"node"`
	Go      bool `json:"go"`
	Python  bool `json:"python"`
	PHP     bool `json:"php"`
	PM2     bool `json:"pm2"`
	Systemd bool `json:"systemd"`
	Port    bool `json:"port"`
}

// Service is one deployable unit under a domain (an nginx/apache vhost
// target, optionally backed by a long-running process managed by PM2
// or systemd, or by PHP-FPM).
type Service struct {
	Name         string            `json:"name"`
	Enabled      bool              `json:"enabled"`
	Type         string            `json:"type"`
	Hosts        []string          `json:"hosts"`
	PublicPath   string            `json:"publicPath"`
	DocumentRoot string            `json:"documentRoot"`
	Runtime      ServiceRuntime    `json:"runtime"`
	Port         int               `json:"port,omitempty"`
	EntryPoint   string            `json:"entryPoint,omitempty"`
	Env          map[string]string `json:"env,omitempty"`
	DomainType   string            `json:"domainType,omitempty"`
	HostsEntries []string          `json:"hostsEntries,omitempty"`

	// PHPVersion, PHPPoolGroup, and PHPMaxChildren are only meaningful
	// for php-template services, and only once this service has its own
	// per-service php-fpm pool (see internal/phpfpm). PHPVersion empty
	// means this php service has no dedicated pool and still falls back
	// to the host-wide PHP_FPM_ENDPOINT config value -- the deliberate
	// backward-compat/migration behavior agreed for the per-service
	// php-fpm feature: existing php services are never migrated
	// automatically, only newly created or explicitly edited ones ever
	// get a non-empty PHPVersion. PHPPoolGroup records the document
	// root's owner group that the pool's dedicated unix user was granted
	// read access to (internal/phpfpm.GrantGroupAccess), so later
	// pool/removal operations don't need to re-stat the document root
	// and can't drift from what was actually granted at creation time.
	PHPVersion     string `json:"phpVersion,omitempty"`
	PHPPoolGroup   string `json:"phpPoolGroup,omitempty"`
	PHPMaxChildren int    `json:"phpMaxChildren,omitempty"`
}

// UsesPerServicePHPFPM reports whether svc has its own dedicated
// php-fpm pool (as opposed to relying on the host-wide
// PHP_FPM_ENDPOINT fallback). Only ever true for php-template services.
func (svc Service) UsesPerServicePHPFPM() bool { return svc.PHPVersion != "" }

// ServicesConfig is services.config.json.
type ServicesConfig struct {
	Domain   string    `json:"domain"`
	Services []Service `json:"services"`
}

// ServiceRef is one service paired with the domain it belongs to, as
// returned by Store.ListAllServices -- for commands (like `wor service
// status`) that need a flat view across every domain without repeating
// the ListDomains+LoadServices loop themselves.
type ServiceRef struct {
	Domain  string
	Service Service
}

// Database is one registered backup profile reference under a domain.
type Database struct {
	Profile string `json:"profile"`
	Label   string `json:"label"`
	Enabled bool   `json:"enabled"`
	Backup  bool   `json:"backup"`
}

// DatabasesConfig is databases.config.json.
type DatabasesConfig struct {
	Domain    string     `json:"domain"`
	Databases []Database `json:"databases"`
}

// SourceBackupConfig controls `wor source backup` behavior.
type SourceBackupConfig struct {
	Enabled           bool     `json:"enabled"`
	RetentionDays     int      `json:"retentionDays"`
	Compression       string   `json:"compression"`
	UseGitIgnore      bool     `json:"useGitIgnore"`
	Include           []string `json:"include"`
	Exclude           []string `json:"exclude"`
	VerifyAfterBackup bool     `json:"verifyAfterBackup"`
}

// DatabaseBackupConfig controls `wor database backup` retention/behavior.
type DatabaseBackupConfig struct {
	Enabled               bool   `json:"enabled"`
	RetentionDays         int    `json:"retentionDays"`
	Compression           string `json:"compression"`
	ExcludeSystemDatabase bool   `json:"excludeSystemDatabase"`
	VerifyAfterBackup     bool   `json:"verifyAfterBackup"`
}

// BackupConfig is backup.config.json.
type BackupConfig struct {
	Source   SourceBackupConfig   `json:"source"`
	Database DatabaseBackupConfig `json:"database"`
}

// DefaultBackupConfig matches the shell version's templates/domain
// defaults (see lib/paths.sh make_domain_files()).
func DefaultBackupConfig() BackupConfig {
	return BackupConfig{
		Source: SourceBackupConfig{
			Enabled:           true,
			RetentionDays:     0,
			Compression:       "zip",
			UseGitIgnore:      true,
			Include:           []string{},
			Exclude:           []string{".git", ".idea", ".vscode", ".DS_Store", "Thumbs.db"},
			VerifyAfterBackup: true,
		},
		Database: DatabaseBackupConfig{
			Enabled:               true,
			RetentionDays:         7,
			Compression:           "gzip",
			ExcludeSystemDatabase: true,
			VerifyAfterBackup:     true,
		},
	}
}

// Service templates. As of the 2026-07-04 redesign (see
// docs/services.md) there are five: static (no runtime), node (Node.js
// + PM2), go (Go + systemd on Linux / PM2 elsewhere), python (Python +
// systemd on Linux / PM2 elsewhere), and php (PHP-FPM, assumed already
// running). The earlier hybrid variants (static-node, node-web,
// node-php, php-node) were removed in the same redesign -- a service
// is one runtime kind, not a mix.
var (
	NodeTemplates   = map[string]bool{"node": true}
	GoTemplates     = map[string]bool{"go": true}
	PythonTemplates = map[string]bool{"python": true}
	PHPTemplates    = map[string]bool{"php": true}
)

// AllTemplates lists every supported service template id, in the same
// order presented by `wor create`'s template picker.
var AllTemplates = []string{"static", "node", "go", "python", "php"}

func TemplateRequiresNode(t string) bool   { return NodeTemplates[t] }
func TemplateRequiresGo(t string) bool     { return GoTemplates[t] }
func TemplateRequiresPython(t string) bool { return PythonTemplates[t] }
func TemplateRequiresPHP(t string) bool    { return PHPTemplates[t] }

// TemplateRequiresProcessSupervisor reports whether template runs as a
// long-running process that wor must start/stop/restart itself (node,
// go, python) as opposed to being served directly by the web server
// (static) or handed off entirely to an already-running system service
// (php, via PHP-FPM).
func TemplateRequiresProcessSupervisor(t string) bool {
	return NodeTemplates[t] || GoTemplates[t] || PythonTemplates[t]
}

// TemplateRequiresPort mirrors TemplateRequiresProcessSupervisor: every
// process-supervised template listens on a local TCP port that the web
// server reverse-proxies to.
func TemplateRequiresPort(t string) bool { return TemplateRequiresProcessSupervisor(t) }

// ProcessProviderFor returns which process supervisor manages
// template's long-running process: "pm2", "systemd", or "" (no
// supervisor). node always uses PM2, so its behavior is identical on
// every OS. go and python use systemd on Linux -- the process
// supervisor already present on virtually every Linux distro, and the
// one docs/services.md specifies -- and fall back to PM2 on macOS and
// Windows, where systemd does not exist, so those platforms are not
// left without a way to run go/python services at all.
func ProcessProviderFor(t string) string {
	switch {
	case NodeTemplates[t]:
		return "pm2"
	case GoTemplates[t], PythonTemplates[t]:
		if osutil.IsLinux() {
			return "systemd"
		}
		return "pm2"
	default:
		return ""
	}
}

// DefaultEntryPoint returns the default entry point file/binary name
// for template, matching docs/services.md. Every process-supervised
// template's entry point is configurable (see the `--entry=` flag on
// `wor service add`); static has none (the whole public/ directory is
// served as-is).
func DefaultEntryPoint(t string) string {
	switch t {
	case "node":
		return "app.js"
	case "go":
		return "app"
	case "python":
		return "app.py"
	case "php":
		return "public/index.php"
	default:
		return ""
	}
}

func IsValidTemplate(t string) bool {
	for _, v := range AllTemplates {
		if v == t {
			return true
		}
	}
	return false
}
