package cliapp

import (
	"fmt"

	"wor/internal/domainmodel"
	"wor/internal/osutil"
	"wor/internal/phpfpm"
	"wor/internal/pm2"
	"wor/internal/ssl"
	"wor/internal/systemd"
)

func (a *App) promptCreateTemplate() (string, error) {
	fmt.Fprintln(a.Err)
	fmt.Fprintln(a.Err, "Select Service Type")
	fmt.Fprintln(a.Err, "1. static (recommended)")
	fmt.Fprintf(a.Err, "2. node %s\n", runtimeVersionLabel("node"))
	fmt.Fprintf(a.Err, "3. go %s\n", runtimeVersionLabel("go"))
	fmt.Fprintf(a.Err, "4. python %s\n", runtimeVersionLabel("python"))
	fmt.Fprintf(a.Err, "5. php %s\n", runtimeVersionLabel("php"))
	choice := a.promptDefault("Choose", "1")
	switch choice {
	case "1", "static":
		return "static", nil
	case "2", "node":
		return "node", nil
	case "3", "go":
		return "go", nil
	case "4", "python":
		return "python", nil
	case "5", "php":
		return "php", nil
	default:
		return "", a.errf("invalid service type choice: %s", choice)
	}
}

// promptPHPVersion shows a numbered picker for a multiple-detected
// PHP-FPM versions, mirroring promptCreateTemplate's style. Only called
// when more than one version was detected -- a single detected version
// is picked automatically (see cmdCreate), and zero detected versions
// falls back to the legacy host-wide PHP_FPM_ENDPOINT without asking.
func (a *App) promptPHPVersion(versions []phpfpm.Version) string {
	fmt.Fprintln(a.Err)
	fmt.Fprintln(a.Err, "Select PHP Version")
	for i, v := range versions {
		fmt.Fprintf(a.Err, "%d. %s\n", i+1, v.Number)
	}
	choice := a.promptDefault("Choose", "1")
	var idx int
	fmt.Sscanf(choice, "%d", &idx)
	if idx >= 1 && idx <= len(versions) {
		return versions[idx-1].Number
	}
	for _, v := range versions {
		if v.Number == choice {
			return choice
		}
	}
	return versions[0].Number
}

func (a *App) cmdCreate(args []string) error {
	fl := parseFlags(args)
	var host string
	for _, arg := range args {
		if len(arg) > 0 && arg[0] != '-' {
			host = arg
			break
		}
	}
	// wor create is a pure wizard: it accepts an optional positional
	// host (e.g. `wor create app.example.com`) and nothing else. Every
	// other choice -- template/service type, domain id override,
	// hosts-file handling -- is prompted for interactively. Automation
	// goes through `wor domain/service/host add` instead, which accept
	// the full set of -- flags (including --service-type=).
	if fl.Any() {
		return a.errf("wor create is interactive only and accepts no -- flags (only an optional host argument). Use wor domain/service/host add for automation")
	}
	if host == "" {
		host = a.prompt("Domain Name: ")
	}
	if host == "" {
		return a.errf("host is required")
	}
	if err := domainmodel.ValidateDomainName(host); err != nil {
		return err
	}

	fmt.Fprintln(a.Out)
	fmt.Fprintln(a.Out, "WOR Create Wizard")
	fmt.Fprintln(a.Out, "-----------------")
	fmt.Fprintf(a.Out, "Host: %s\n", host)

	domain, service, err := domainmodel.HostToDomainService(host, "")
	if err != nil {
		return err
	}
	suggestedDomain := domain
	override := a.promptDefault(fmt.Sprintf("Domain id [%s] (Enter to accept, or type an override)", suggestedDomain), suggestedDomain)
	if override != suggestedDomain {
		domain, service, err = domainmodel.HostToDomainService(host, override)
		if err != nil {
			return err
		}
	}

	apexHost := domainmodel.IsApexDomain(host)
	aliasHost := ""
	if apexHost {
		aliasHost = domainmodel.RootDomainAliasHost(host)
	}

	fmt.Fprintln(a.Out)
	fmt.Fprintln(a.Out, "Resolved Target")
	fmt.Fprintln(a.Out, "---------------")
	fmt.Fprintf(a.Out, "Domain  : %s\n", domain)
	fmt.Fprintf(a.Out, "Service : %s\n", service)

	if err := a.validateCreateTargetAvailable(host, domain, service, aliasHost); err != nil {
		return err
	}

	template, err := a.promptCreateTemplate()
	if err != nil {
		return err
	}
	fmt.Fprintln(a.Out)
	fmt.Fprintln(a.Out, "Runtime Preflight")
	fmt.Fprintln(a.Out, "-----------------")
	if err := a.requireTemplateRuntime(template); err != nil {
		return err
	}
	a.ok("Runtime ready for template: %s", template)

	phpVersion := ""
	if domainmodel.TemplateRequiresPHP(template) {
		versions := phpfpm.DetectVersions()
		switch len(versions) {
		case 0:
			// No per-version pool.d layout detected -- fall back to the
			// legacy host-wide PHP_FPM_ENDPOINT silently; requireTemplateRuntime
			// already confirmed something usable exists.
		case 1:
			phpVersion = versions[0].Number
			a.ok("Using PHP %s (only version detected)", phpVersion)
		default:
			phpVersion = a.promptPHPVersion(versions)
		}
	}

	port := 0
	if domainmodel.TemplateRequiresPort(template) {
		suggested, err := a.findNextPort(3000)
		if err != nil {
			return err
		}
		if a.confirmYesDefaultYes(fmt.Sprintf("Use auto port %d?", suggested)) {
			port = suggested
		} else {
			portStr := a.prompt("Custom port: ")
			if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil || port == 0 {
				return a.errf("invalid port: %s", portStr)
			}
		}
	}

	// wor create prompts for everything interactively -- no --domain-type=,
	// --add-hosts, or --no-hosts flags (removed along with every other
	// -- flag; see the fl.Any() guard above).
	hostsMode := "ask"
	wizard, err := a.runHostDomainWizard(host, "", "", "", "summary")
	if err != nil {
		return err
	}

	fmt.Fprintln(a.Out)
	fmt.Fprintln(a.Out, "Create Summary")
	fmt.Fprintln(a.Out, "--------------")
	fmt.Fprintf(a.Out, "Host      : %s\n", host)
	fmt.Fprintf(a.Out, "Domain    : %s\n", domain)
	fmt.Fprintf(a.Out, "Service   : %s\n", service)
	fmt.Fprintf(a.Out, "Template  : %s\n", template)
	fmt.Fprintf(a.Out, "Provider  : %s\n", a.Cfg.HostProviderName())
	fmt.Fprintf(a.Out, "Domain Type: %s\n", wizard.DomainType)
	a.domainTypeSummaryNote(wizard, host)
	fmt.Fprintf(a.Out, "WOR_HOME  : %s\n", a.Cfg.WorHome)
	fmt.Fprintf(a.Out, "Source    : %s\n", a.Store.ServiceDir(domain, service))
	if domainmodel.TemplateRequiresPort(template) {
		fmt.Fprintf(a.Out, "Port      : %d\n", port)
	}
	if phpVersion != "" {
		fmt.Fprintf(a.Out, "PHP       : %s (dedicated pool)\n", phpVersion)
	}
	fmt.Fprintln(a.Out)
	if !a.confirmYesDefaultNo("Create this service?") {
		return a.errf("cancelled")
	}

	if err := a.cmdDomain([]string{"add", domain}); err != nil {
		return err
	}
	serviceArgs := []string{"add", fmt.Sprintf("%s/%s", domain, service), "--host=" + host, "--service-type=" + template}
	if domainmodel.TemplateRequiresPort(template) {
		serviceArgs = append(serviceArgs, fmt.Sprintf("--port=%d", port))
	}
	if phpVersion != "" {
		serviceArgs = append(serviceArgs, "--php-version="+phpVersion)
	}
	if err := a.cmdService(serviceArgs); err != nil {
		return err
	}

	hostArgs := []string{"--target=" + domain + "/" + service, "--domain-type=" + wizard.DomainType}
	if wizard.DomainType == "local" {
		switch hostsMode {
		case "yes":
			hostArgs = append(hostArgs, "--add-hosts")
		case "no":
			hostArgs = append(hostArgs, "--no-hosts")
		}
	}
	// Call hostAdd directly (rather than through cmdHost) so we get back
	// the *real* wizard result -- in particular whether the hosts file
	// was auto-configured -- for the alias step below.
	wizard, err = a.hostAdd(host, parseFlags(hostArgs))
	if err != nil {
		return err
	}

	aliasAdded := false
	preferredHost := ""
	if apexHost && aliasHost != "" && aliasHost != host {
		if a.promptAddWebsiteAlias(host, aliasHost) {
			if err := a.validateHostAliasAvailable(aliasHost, domain, service); err != nil {
				return err
			}
			preferredHost = a.promptPreferredWebsiteAddress(host, aliasHost)
			a.Store.AddHostToService(domain, service, aliasHost)
			if wizard.DomainType == "local" && wizard.AutoConfigured {
				a.Store.SetServiceDomainMetadata(domain, service, wizard.DomainType, "127.0.0.1 "+aliasHost)
			}
			provider, err := a.Provider()
			if err != nil {
				return err
			}
			svcType := a.Store.GetServiceType(domain, service)
			siteFile := provider.SiteAvailableFile(host)
			if err := a.writeHostConfig(provider, host, domain, service, svcType, port, siteFile, []string{aliasHost}, preferredHost); err != nil {
				return err
			}
			if err := provider.Reload(); err != nil {
				return err
			}
			aliasAdded = true
			a.ok("Host alias ready: %s -> %s/%s", aliasHost, domain, service)
		}
	}

	if a.confirmYesDefaultNo("Configure SSL?") {
		defaultProvider := a.Cfg.SSLProviderName()
		if wizard.DomainType == "local" && defaultProvider == "letsencrypt" {
			defaultProvider = "self-signed"
		}
		sslProvider := a.promptSSLProvider(defaultProvider)
		if sslProvider != "none" {
			sslArgs := []string{"issue", host, "--provider=" + sslProvider}
			if preferredHost != "" {
				sslArgs = append(sslArgs, "--preferred="+preferredHost)
			}
			if err := a.cmdSSL(sslArgs); err != nil {
				return err
			}
		}
	}

	if domainmodel.TemplateRequiresProcessSupervisor(template) {
		if err := a.cmdService([]string{"start", fmt.Sprintf("%s/%s", domain, service)}); err != nil {
			return err
		}
	}

	fmt.Fprintln(a.Out)
	fmt.Fprintln(a.Out, "WOR Service Ready")
	fmt.Fprintln(a.Out)
	fmt.Fprintf(a.Out, "Host    : %s\n", host)
	if aliasAdded {
		fmt.Fprintf(a.Out, "Alias   : %s\n", aliasHost)
	}
	fmt.Fprintf(a.Out, "Domain  : %s\n", domain)
	fmt.Fprintf(a.Out, "Service : %s\n", service)
	fmt.Fprintf(a.Out, "Domain Type: %s\n", wizard.DomainType)
	a.domainTypeSummaryNote(wizard, host)
	fmt.Fprintf(a.Out, "Source  : %s\n", a.Store.ServiceDir(domain, service))
	switch domainmodel.ProcessProviderFor(template) {
	case "pm2":
		fmt.Fprintf(a.Out, "Process : pm2 %s\n", pm2.Name(domain, service))
	case "systemd":
		fmt.Fprintf(a.Out, "Process : systemd %s\n", systemd.Name(domain, service))
	default:
		fmt.Fprintln(a.Out, "Process : none")
	}
	if phpVersion != "" {
		fmt.Fprintf(a.Out, "PHP Pool: %s (PHP %s)\n", phpfpm.PoolName(domain, service), phpVersion)
	}
	fmt.Fprintln(a.Out)
	fmt.Fprintln(a.Out, "Next:")
	fmt.Fprintf(a.Out, "  wor source clone %s/%s <git-url>\n", domain, service)
	fmt.Fprintf(a.Out, "  wor deploy %s\n", host)
	return nil
}

func (a *App) validateCreateTargetAvailable(host, domain, service, aliasHost string) error {
	if a.Store.ServiceExists(domain, service) {
		return a.errf("service already exists: %s/%s", domain, service)
	}
	if existing, ok := a.Store.ResolveHost(host); ok {
		return a.errf("host is already registered: %s -> %s", host, existing)
	}
	if provider, err := a.Provider(); err == nil && provider.HostExists(host) {
		return a.errf("host config already exists: %s", host)
	}
	if aliasHost != "" && aliasHost != host {
		return a.validateHostAliasAvailable(aliasHost, domain, service)
	}
	return nil
}

func (a *App) validateHostAliasAvailable(aliasHost, domain, service string) error {
	if err := domainmodel.ValidateDomainName(aliasHost); err != nil {
		return err
	}
	if existing, ok := a.Store.ResolveHost(aliasHost); ok && existing != domain+"/"+service {
		return a.errf("host alias is already registered: %s -> %s", aliasHost, existing)
	}
	if provider, err := a.Provider(); err == nil && provider.HostExists(aliasHost) {
		if existing, ok := a.Store.ResolveHost(aliasHost); !ok || existing != domain+"/"+service {
			return a.errf("host alias config already exists: %s", aliasHost)
		}
	}
	return nil
}

func (a *App) promptAddWebsiteAlias(host, aliasHost string) bool {
	fmt.Fprintln(a.Out)
	fmt.Fprintln(a.Out, "Add website alias?")
	fmt.Fprintln(a.Out)
	fmt.Fprintln(a.Out, aliasHost)
	fmt.Fprintln(a.Out, "  -> ", host)
	choice := a.promptDefault("Choose alias option (1=Yes, 2=No)", "2")
	return choice == "1"
}

func (a *App) promptPreferredWebsiteAddress(host, aliasHost string) string {
	fmt.Fprintln(a.Out)
	fmt.Fprintln(a.Out, "Preferred website address?")
	fmt.Fprintf(a.Out, "1. %s (redirect %s -> %s)\n", host, aliasHost, host)
	fmt.Fprintf(a.Out, "2. %s (redirect %s -> %s)\n", aliasHost, host, aliasHost)
	fmt.Fprintln(a.Out, "3. No redirect (both remain active)")
	choice := a.promptDefault("Choose", "1")
	switch choice {
	case "2":
		return aliasHost
	case "3":
		return ""
	default:
		return host
	}
}

// promptSSLProvider shows a numbered SSL provider picker with the
// installed openssl/certbot version appended to each relevant option,
// mirroring wor setup's setupSSL() step. letsencrypt is marked
// "(not available)" (and falls back to self-signed if chosen anyway)
// under the same conditions setupSSL() checks: certbot missing, this
// OS is Windows, or the host provider isn't nginx/apache.
func (a *App) promptSSLProvider(defaultProvider string) string {
	certbotFound := osutil.Exists("certbot")
	opensslFound := osutil.Exists("openssl")
	hostProvider := a.Cfg.HostProviderName()
	letsencryptCompatible := certbotFound && !osutil.IsWindows() && (hostProvider == "nginx" || hostProvider == "apache")

	opensslVersion := "not installed"
	if opensslFound {
		opensslVersion = osutil.RunVersion("openssl", "version")
	}
	certbotVersion := "not installed"
	if certbotFound {
		certbotVersion = osutil.RunVersion("certbot", "--version")
	}

	options := []struct{ id, name string }{
		{"1", "letsencrypt"},
		{"2", "self-signed"},
		{"3", "custom"},
		{"4", "none"},
	}
	defaultChoice := "1"
	for _, o := range options {
		if o.name == defaultProvider {
			defaultChoice = o.id
		}
	}

	fmt.Fprintln(a.Out)
	fmt.Fprintln(a.Out, "SSL Provider")
	for _, o := range options {
		label := ssl.ProviderLabel(o.name)
		switch o.name {
		case "letsencrypt":
			if letsencryptCompatible {
				label = fmt.Sprintf("%s : %s", label, certbotVersion)
			} else {
				label = fmt.Sprintf("%s : %s (not available)", label, certbotVersion)
			}
		case "self-signed":
			label = fmt.Sprintf("%s : %s", label, opensslVersion)
		}
		fmt.Fprintf(a.Out, "%s. %s\n", o.id, label)
	}
	choice := a.promptDefault("Choose", defaultChoice)
	switch choice {
	case "1", "letsencrypt", "lets-encrypt":
		if !letsencryptCompatible {
			a.warn("letsencrypt is not available here (needs certbot, a non-Windows OS, and host_provider=nginx or apache). Falling back to self-signed.")
			return "self-signed"
		}
		return "letsencrypt"
	case "2", "self-signed", "selfsigned":
		return "self-signed"
	case "3", "custom":
		return "custom"
	case "4", "none":
		return "none"
	default:
		return choice
	}
}
