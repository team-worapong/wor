package cliapp

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"

	"wor/internal/domainmodel"
	"wor/internal/hostprovider"
	"wor/internal/hostsfile"
	"wor/internal/osutil"
	"wor/internal/phpfpm"
	"wor/internal/ssl"
)

// hostWizardResult carries the outcome of the interactive domain-type
// wizard shared by `wor host add` and `wor create`.
type hostWizardResult struct {
	DomainType     string // "local" | "public"
	HostsEntry     string
	AutoConfigured bool
}

func (a *App) promptHostTarget() (string, error) {
	targets, err := a.Store.ListServiceTargets()
	if err != nil {
		return "", err
	}
	if len(targets) == 0 {
		fmt.Fprintln(a.Err, "No services found in WOR_HOME.")
		fmt.Fprintln(a.Err, "Run one of these first:")
		fmt.Fprintln(a.Err, "  wor service add <domain>/<service>")
		fmt.Fprintln(a.Err, "  wor create app.example.com")
		return "", a.errf("no service target available")
	}
	fmt.Fprintln(a.Err)
	fmt.Fprintln(a.Err, "Select target service")
	fmt.Fprintln(a.Err)
	for i, t := range targets {
		fmt.Fprintf(a.Err, "%d. %s\n", i+1, t)
	}
	fmt.Fprintln(a.Err)
	choice := a.prompt("Choose target [1]: ")
	if choice == "" {
		choice = "1"
	}
	idx, err := strconv.Atoi(choice)
	if err != nil || idx < 1 || idx > len(targets) {
		return "", a.errf("invalid target choice: %s", choice)
	}
	return targets[idx-1], nil
}

func (a *App) promptDomainType(defaultType string) (string, error) {
	defaultChoice := "2"
	if defaultType == "local" {
		defaultChoice = "1"
	}
	fmt.Fprintln(a.Err)
	fmt.Fprintln(a.Err, "Domain Type")
	fmt.Fprintln(a.Err)
	fmt.Fprintln(a.Err, "1. Local Development")
	fmt.Fprintln(a.Err, "2. Public Domain")
	fmt.Fprintln(a.Err)
	choice := a.prompt(fmt.Sprintf("Choose domain type [%s]: ", defaultChoice))
	if choice == "" {
		choice = defaultChoice
	}
	switch choice {
	case "1", "local", "Local":
		return "local", nil
	case "2", "public", "Public":
		return "public", nil
	default:
		return "", a.errf("invalid domain type: %s", choice)
	}
}

// configureLocalHostsEntry mirrors lib/hosts.sh configure_local_hosts_entry().
// mode is "ask" (interactive), "yes", "no", or "summary" (dry-run, used
// by `wor create`'s pre-confirmation summary step).
func (a *App) configureLocalHostsEntry(host, mode string) (hostWizardResult, error) {
	entry := "127.0.0.1 " + host
	if exists, _ := hostsfile.EntryExists(host); exists {
		a.ok("hosts entry already exists: %s", entry)
		return hostWizardResult{DomainType: "local", HostsEntry: entry}, nil
	}
	switch mode {
	case "yes":
		// proceed to add below
	case "no":
		a.warn("Add this manually:")
		fmt.Fprintf(a.Out, "  %s\n", entry)
		return hostWizardResult{DomainType: "local", HostsEntry: ""}, nil
	case "summary":
		return hostWizardResult{DomainType: "local", HostsEntry: entry}, nil
	default:
		fmt.Fprintln(a.Out)
		fmt.Fprintln(a.Out, "Add local hosts entry?")
		fmt.Fprintln(a.Out, entry)
		if !a.confirmYesDefaultYes("Requires sudo/Administrator.") {
			a.warn("Add this manually:")
			fmt.Fprintf(a.Out, "  %s\n", entry)
			return hostWizardResult{DomainType: "local", HostsEntry: ""}, nil
		}
	}
	if !osutil.IsElevated() {
		a.warn("Not running elevated (%s). Attempting to write the hosts file anyway.", osutil.ElevationHint())
	}
	if err := hostsfile.Add(host); err != nil {
		return hostWizardResult{}, fmt.Errorf("cannot add hosts entry (%s): %w", osutil.ElevationHint(), err)
	}
	a.ok("hosts entry added")
	return hostWizardResult{DomainType: "local", HostsEntry: entry, AutoConfigured: true}, nil
}

// runHostDomainWizard mirrors lib/hosts.sh run_host_domain_wizard():
// determines whether host is a local-dev or public domain (reusing any
// value already recorded for domain/service), then configures the
// hosts file entry for local domains.
func (a *App) runHostDomainWizard(host, domain, service, requestedType, hostsMode string) (hostWizardResult, error) {
	if err := domainmodel.ValidateDomainName(host); err != nil {
		return hostWizardResult{}, err
	}
	requestedType, err := domainmodel.NormalizeDomainType(requestedType)
	if err != nil {
		return hostWizardResult{}, err
	}

	domainType := ""
	if domain != "" && service != "" {
		if existing := a.Store.ServiceDomainType(domain, service); existing != "" {
			domainType, err = domainmodel.NormalizeDomainType(existing)
			if err != nil {
				return hostWizardResult{}, err
			}
			a.ok("Domain Type already configured: %s", domainType)
		}
	}
	if domainType == "" {
		if requestedType != "" {
			domainType = requestedType
		} else {
			domainType, err = a.promptDomainType(domainmodel.SuggestDomainTypeForHost(host))
			if err != nil {
				return hostWizardResult{}, err
			}
		}
	}

	var result hostWizardResult
	switch domainType {
	case "local":
		result, err = a.configureLocalHostsEntry(host, hostsMode)
		if err != nil {
			return hostWizardResult{}, err
		}
	case "public":
		a.info("Public domain selected. Make sure DNS points to this server.")
		result = hostWizardResult{DomainType: "public"}
	default:
		return hostWizardResult{}, a.errf("invalid domain type: %s", domainType)
	}

	if domain != "" && service != "" {
		if err := a.Store.SetServiceDomainMetadata(domain, service, result.DomainType, result.HostsEntry); err != nil {
			return hostWizardResult{}, err
		}
	}
	return result, nil
}

func (a *App) domainTypeSummaryNote(result hostWizardResult, host string) {
	switch result.DomainType {
	case "local":
		entry := result.HostsEntry
		if entry == "" {
			entry = "127.0.0.1 " + host
		}
		fmt.Fprintf(a.Out, "Hosts Entry : %s\n", entry)
	case "public":
		fmt.Fprintln(a.Out, "DNS Note    : Make sure DNS points to this server.")
	}
}

func (a *App) cmdHost(args []string) error {
	if len(args) == 0 {
		a.usage()
		return a.errf("host action required")
	}
	action := args[0]

	switch action {
	case "list":
		if a.Cfg.HostProviderName() == "skip" {
			a.warn("HOST_PROVIDER=skip. Host configuration is disabled.")
			return nil
		}
		provider, err := a.Provider()
		if err != nil {
			return err
		}
		return a.printHostList(provider)
	case "test":
		provider, err := a.Provider()
		if err != nil {
			return err
		}
		if err := provider.Test(); err != nil {
			return err
		}
		a.ok("%s config test passed", provider.Name)
		return nil
	case "reload":
		provider, err := a.Provider()
		if err != nil {
			return err
		}
		if err := provider.Reload(); err != nil {
			return err
		}
		a.ok("%s reloaded", provider.Name)
		return nil
	}

	if len(args) < 2 {
		a.usage()
		return a.errf("host and action required")
	}
	host := args[1]
	rest := args[2:]
	fl := parseFlags(rest)

	switch action {
	case "add":
		_, err := a.hostAdd(host, fl)
		return err
	case "remove":
		return a.hostRemove(host, fl)
	case "logs":
		return a.hostLogs(host, rest, fl)
	default:
		a.usage()
		return a.errf("unknown host action: %s", action)
	}
}

// hostListRow is one rendered line of `wor host list`.
type hostListRow struct {
	host    string
	target  string // "domain/service", or "-" if unresolvable
	port    string
	ssl     bool
	enabled bool
}

// printHostList implements `wor host list`. Earlier this just printed
// every ".conf" filename in sites-available with no other information
// -- not even whether a site was actually enabled (symlinked into
// sites-enabled). This instead groups by enabled/disabled (comparing
// sites-available against sites-enabled; on macOS/Windows nginx/apache
// use one flat directory for both, so every listed site there counts as
// enabled) and, per site, resolves its target domain/service
// (Store.ResolveHost), port (Store.GetServicePort, only when the
// target's template actually uses one), and SSL state (ssl.LoadState).
func (a *App) printHostList(provider *hostprovider.Provider) error {
	avail := provider.SitesAvailable()
	enabled := provider.SitesEnabled()

	entries, err := os.ReadDir(avail)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintln(a.Out, "No sites found.")
			return nil
		}
		return err
	}

	enabledNames := map[string]bool{}
	sameDir := enabled == avail
	if !sameDir {
		if enabledEntries, err := os.ReadDir(enabled); err == nil {
			for _, e := range enabledEntries {
				enabledNames[e.Name()] = true
			}
		}
	}

	var enabledRows, disabledRows []hostListRow
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".conf") || name == provider.DefaultConfigName() {
			continue
		}
		host := strings.TrimSuffix(strings.TrimPrefix(name, "wor__"), ".conf")

		row := hostListRow{host: host, target: "-", port: "n/a", enabled: sameDir || enabledNames[name]}
		if target, ok := a.Store.ResolveHost(host); ok {
			row.target = target
			// target is "<domain>/<service>" built directly from what's
			// on disk, which should always be a valid slug/service pair
			// when written by wor itself -- but don't silently drop the
			// port lookup if it somehow isn't (e.g. services.config.json
			// hand-edited into a bad state); a warning at least makes
			// an otherwise-invisible "n/a" explainable instead of
			// looking like the service just has no port configured.
			if domain, service, err := domainmodel.ParseTarget(target); err == nil {
				svcType := a.Store.GetServiceType(domain, service)
				if domainmodel.TemplateRequiresPort(svcType) {
					if p, err := a.Store.GetServicePort(domain, service); err == nil && p != 0 {
						row.port = fmt.Sprintf(":%d", p)
					}
				}
			} else {
				a.warn("host %s resolves to invalid target %q: %s", host, target, err)
			}
		}
		if st, ok, _ := ssl.LoadState(a.Cfg.SSL, host); ok {
			row.ssl = st.Enabled
		}

		if row.enabled {
			enabledRows = append(enabledRows, row)
		} else {
			disabledRows = append(disabledRows, row)
		}
	}

	if len(enabledRows) == 0 && len(disabledRows) == 0 {
		fmt.Fprintln(a.Out, "No sites found.")
		return nil
	}

	// One flat table under a "WOR Hosts <server> (<version>)" title. The
	// per-row checkmark carries the enabled state, so the old
	// ENABLED/DISABLED group headers are redundant; enabled rows still
	// sort first. Deliberate color semantics (owner decision after a
	// real outage where all-green output coexisted with two unreachable
	// sites): this listing reports CONFIG state only, so it uses a blue
	// check / red cross for enabled/disabled and a plain "ssl" marker --
	// no green anywhere, because green reads as "the site is healthy",
	// which only `wor diagnose` actually verifies.
	fmt.Fprintf(a.Out, "WOR Hosts %s\n\n", a.diagServerLabel())

	useColor := a.colorEnabled()
	tw := tabwriter.NewWriter(a.Out, 0, 4, 3, ' ', 0)
	printRows := func(rows []hostListRow, isEnabled bool) {
		for _, row := range rows {
			var mark string
			if isEnabled {
				mark = tag(useColor, ansiBlue, "✓", "[on]")
			} else {
				mark = tag(useColor, ansiRed, "✗", "[off]")
			}
			sslTag := "-"
			if row.ssl {
				sslTag = "ssl"
			}
			fmt.Fprintf(tw, "  %s\t%s\t-> %s\t%s\t%s\n", mark, row.host, row.target, row.port, sslTag)
		}
	}
	printRows(enabledRows, true)
	printRows(disabledRows, false)
	tw.Flush()
	return nil
}

// hostAdd implements `wor host add`. It returns the resolved
// hostWizardResult so `wor create` can reuse the outcome (e.g. whether
// the hosts file was auto-configured) when it separately registers a
// www. alias for an apex domain.
func (a *App) hostAdd(host string, fl flags) (hostWizardResult, error) {
	server := fl.Get("server", a.Cfg.HostProviderName())
	replace := fl.Has("replace")
	domainType := fl.Get("domain-type", "")
	hostsMode := "ask"
	if fl.Has("add-hosts") {
		hostsMode = "yes"
	} else if fl.Has("no-hosts") {
		hostsMode = "no"
	}

	if err := domainmodel.ValidateDomainName(host); err != nil {
		return hostWizardResult{}, err
	}
	if server != "nginx" && server != "apache" {
		return hostWizardResult{}, a.errf("unsupported host provider: %s", server)
	}

	target := fl.Get("target", "")
	if target == "" {
		t, err := a.promptHostTarget()
		if err != nil {
			return hostWizardResult{}, err
		}
		target = t
	}
	domain, service, err := domainmodel.ParseTarget(target)
	if err != nil {
		return hostWizardResult{}, err
	}
	if !a.Store.ServiceExists(domain, service) {
		return hostWizardResult{}, a.errf("service not found: %s/%s", domain, service)
	}

	provider, err := hostprovider.New(server, a.Cfg)
	if err != nil {
		return hostWizardResult{}, err
	}
	svcType := a.Store.GetServiceType(domain, service)
	if domainmodel.TemplateRequiresPHP(svcType) {
		if err := a.requirePHPRuntime(); err != nil {
			return hostWizardResult{}, err
		}
	}

	if provider.HostExists(host) {
		if !replace {
			a.warn("Host already exists: %s", host)
			if !a.confirmYesDefaultNo("Replace existing host config?") {
				a.Store.AddHostToService(domain, service, host)
				a.ok("Host already exists, skipped: %s -> %s/%s", host, domain, service)
				return hostWizardResult{}, nil
			}
		}
		provider.RemoveHostFiles(host)
	}

	if _, regenerated, err := provider.EnsureDefaultHost(a.Store, a.Cfg.Backups, a.Cfg.Logs); err != nil {
		return hostWizardResult{}, err
	} else if regenerated {
		a.warn("default host config pointed at a stale path (previous WOR_HOME?) -- regenerated for the current workspace")
	}

	result, err := a.runHostDomainWizard(host, domain, service, domainType, hostsMode)
	if err != nil {
		return hostWizardResult{}, err
	}

	port := 0
	if domainmodel.TemplateRequiresPort(svcType) {
		port, err = a.Store.GetServicePort(domain, service)
		if err != nil {
			return hostWizardResult{}, err
		}
	}

	siteFile := provider.SiteAvailableFile(host)
	if err := a.writeHostConfig(provider, host, domain, service, svcType, port, siteFile, nil, ""); err != nil {
		return hostWizardResult{}, err
	}
	enabledFile := provider.SiteEnabledFile(host)
	if err := provider.EnableHost(siteFile, enabledFile); err != nil {
		return hostWizardResult{}, err
	}
	a.Store.AddHostToService(domain, service, host)
	if err := provider.Reload(); err != nil {
		return hostWizardResult{}, err
	}
	a.ok("Host ready: %s -> %s/%s", host, domain, service)
	fmt.Fprintf(a.Out, "Domain Type: %s\n", result.DomainType)
	a.domainTypeSummaryNote(result, host)
	return result, nil
}

// buildWriteParams resolves the default public path, document root,
// PHP-FPM endpoint, and any on-record SSL state for a host, matching
// lib/webserver.sh write_host_config()'s variable exports.
func (a *App) buildWriteParams(provider *hostprovider.Provider, host, domain, service, svcType string, port int, siteFile string, aliases []string, preferred string) (hostprovider.WriteParams, error) {
	// template_document_root() always resolves to "public" for every
	// template (see lib/webserver.sh); process-supervised templates'
	// (node/go/python) apache vhosts simply never reference
	// {{DOCUMENT_ROOT}} so the value is harmlessly unused there.
	docRoot := filepath.Join(a.Store.ServiceDir(domain, service), "public")
	params := hostprovider.WriteParams{
		Host: host, Domain: domain, Service: service, SvcType: svcType, Port: port,
		SiteFile: siteFile, Aliases: aliases, Preferred: preferred,
		DefaultPublicPath: filepath.Join(a.Cfg.Domains, "default", "web", "public"),
		DocumentRoot:      docRoot,
	}
	if domainmodel.TemplateRequiresPHP(svcType) {
		// A non-empty PHPVersion means this service has its own
		// dedicated php-fpm pool (see internal/phpfpm) -- point the
		// vhost at that pool's own socket instead of the host-wide
		// PHP_FPM_ENDPOINT. Empty PHPVersion is the backward-compat
		// path: this php service predates the per-service-pool feature
		// (or was created with --no-php-pool), so it keeps using the
		// shared endpoint exactly as before.
		if phpVersion := a.Store.GetServicePHPVersion(domain, service); phpVersion != "" {
			version, ok := phpfpm.ResolveVersion(phpVersion)
			if !ok {
				return params, fmt.Errorf("PHP %s (used by %s/%s's dedicated pool) is no longer detected on this host; run wor doctor", phpVersion, domain, service)
			}
			ep := "unix:" + phpfpm.SocketPath(version, domain, service)
			if provider.Name == "nginx" {
				params.PHPFPMEndpoint = hostprovider.FormatEndpointForNginx(ep)
			} else {
				params.PHPFPMEndpoint = hostprovider.FormatEndpointForApache(ep)
			}
		} else {
			var ep string
			var err error
			if provider.Name == "nginx" {
				ep, err = hostprovider.PHPFPMEndpointForNginx(a.Cfg)
			} else {
				ep, err = hostprovider.PHPFPMEndpointForApache(a.Cfg)
			}
			if err != nil {
				return params, err
			}
			params.PHPFPMEndpoint = ep
		}
	}
	if st, ok, _ := ssl.LoadState(a.Cfg.SSL, host); ok {
		params.SSLEnabled = st.Enabled
		params.SSLCertFile = st.CertFile
		params.SSLKeyFile = st.KeyFile
	}
	return params, nil
}

// writeHostConfig builds params for host and writes the vhost file.
func (a *App) writeHostConfig(provider *hostprovider.Provider, host, domain, service, svcType string, port int, siteFile string, aliases []string, preferred string) error {
	params, err := a.buildWriteParams(provider, host, domain, service, svcType, port, siteFile, aliases, preferred)
	if err != nil {
		return err
	}
	return provider.WriteConfig(params)
}

func (a *App) hostRemove(host string, fl flags) error {
	yes := fl.Has("yes") || fl.Has("y")
	if !yes {
		if !a.requireTyped(fmt.Sprintf("Remove host %s ? Type YES: ", host), "YES") {
			return a.errf("cancelled")
		}
	}
	provider, err := a.Provider()
	if err != nil {
		return err
	}
	if err := provider.RemoveHostFiles(host); err != nil {
		return err
	}
	if err := a.Store.RemoveHostFromServices(host); err != nil {
		a.warn("could not update services.config.json for %s: %s", host, err)
	}
	if err := hostsfile.Remove(host); err != nil {
		a.warn("could not remove hosts file entry for %s: %s (%s)", host, err, osutil.ElevationHint())
	}
	if _, hasState, _ := ssl.LoadState(a.Cfg.SSL, host); hasState {
		if err := ssl.RemoveHostDir(a.Cfg.SSL, host); err != nil {
			a.warn("could not remove SSL files for %s: %s", host, err)
		}
	}
	if err := provider.Reload(); err != nil {
		return err
	}
	a.ok("Host removed: %s", host)
	return nil
}

func (a *App) hostLogs(host string, rest []string, fl flags) error {
	logType := "access"
	if len(rest) > 0 && (rest[0] == "access" || rest[0] == "error") {
		logType = rest[0]
	}
	lines := fl.Get("lines", "100")
	provider, err := a.Provider()
	if err != nil {
		return err
	}
	path := filepath.Join(provider.LogDir(), fmt.Sprintf("%s.%s.log", host, logType))
	return tailFollow(a.Out, path, lines)
}
