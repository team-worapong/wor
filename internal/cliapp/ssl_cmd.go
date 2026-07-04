package cliapp

import (
	"fmt"

	"wor/internal/domainmodel"
	"wor/internal/ssl"
)

// resolveSSLTarget resolves a host or domain/service argument down to
// the service's primary host (first entry in its Hosts list) plus any
// remaining hosts as aliases, matching commands/ssl.sh
// ssl_primary_host_for_target()/ssl_aliases_for_service().
func (a *App) resolveSSLTarget(target string) (primary string, aliases []string, domain, service string, err error) {
	resolved := target
	if !containsSlash(target) {
		if r, ok := a.Store.ResolveHost(target); ok {
			resolved = r
		} else {
			return "", nil, "", "", a.errf("host not found in services.config.json: %s", target)
		}
	}
	domain, service, err = domainmodel.ParseTarget(resolved)
	if err != nil {
		return "", nil, "", "", err
	}
	hosts, err := a.Store.ListHostsForService(domain, service)
	if err != nil {
		return "", nil, "", "", err
	}
	if len(hosts) == 0 {
		return "", nil, "", "", a.errf("service has no registered hosts: %s/%s", domain, service)
	}
	primary = hosts[0]
	for _, h := range hosts[1:] {
		aliases = append(aliases, h)
	}
	return primary, aliases, domain, service, nil
}

func containsSlash(s string) bool {
	for _, r := range s {
		if r == '/' {
			return true
		}
	}
	return false
}

func (a *App) cmdSSL(args []string) error {
	if len(args) < 2 {
		a.usage()
		return a.errf("ssl action and host are required")
	}
	action, target := args[0], args[1]
	switch action {
	case "issue", "renew", "status", "remove", "install":
	default:
		a.usage()
		return a.errf("unknown ssl action: %s", action)
	}
	fl := parseFlags(args[2:])

	primary, aliases, domain, service, err := a.resolveSSLTarget(target)
	if err != nil {
		return err
	}
	svcType := a.Store.GetServiceType(domain, service)

	switch action {
	case "issue":
		provider, err := ssl.NormalizeProvider(fl.Get("provider", a.Cfg.SSLProviderName()))
		if err != nil {
			return err
		}
		switch provider {
		case "letsencrypt":
			hostProviderName := a.Cfg.HostProviderName()
			if err := ssl.IssueLetsEncrypt(hostProviderName, primary, aliases); err != nil {
				return err
			}
			certDir := ssl.LetsEncryptCertDir(primary)
			if err := ssl.WriteState(a.Cfg.SSL, primary, "letsencrypt", certDir+"/fullchain.pem", certDir+"/privkey.pem", "enabled"); err != nil {
				return err
			}
			if err := a.rewriteHostConfigWithSSL(primary, domain, service, svcType, aliases, fl.Get("preferred", "")); err != nil {
				return err
			}
			hp, err := a.Provider()
			if err == nil {
				hp.Reload()
			}
		case "self-signed":
			cert, key, err := ssl.IssueSelfSigned(a.Cfg.SSL, primary, aliases)
			if err != nil {
				return err
			}
			if err := a.rewriteHostConfigWithSSLFiles(primary, domain, service, svcType, aliases, fl.Get("preferred", ""), cert, key); err != nil {
				return err
			}
			if err := ssl.WriteState(a.Cfg.SSL, primary, "self-signed", cert, key, "unsupported"); err != nil {
				return err
			}
			a.ok("Self-signed SSL installed: %s", primary)
		case "custom":
			return a.errf("use: wor ssl install %s --cert=/path/fullchain.pem --key=/path/privkey.pem", primary)
		case "none":
			a.ok("SSL skipped: %s", primary)
		}
		return nil

	case "renew":
		st, ok, _ := ssl.LoadState(a.Cfg.SSL, primary)
		if ok && st.Provider == "letsencrypt" {
			return ssl.RenewLetsEncrypt()
		}
		a.info("Auto renew is not supported for this SSL provider.")
		return nil

	case "status":
		info := ssl.Status(a.Cfg.SSL, primary)
		fmt.Fprintf(a.Out, "SSL Enabled          : %v\n", info.Enabled)
		fmt.Fprintf(a.Out, "Current Provider     : %s\n", info.Provider)
		fmt.Fprintf(a.Out, "Certificate File     : %s\n", orNone(info.CertFile))
		fmt.Fprintf(a.Out, "Private Key File     : %s\n", orNone(info.KeyFile))
		fmt.Fprintf(a.Out, "Certificate Expiration: %s\n", info.Expiration)
		fmt.Fprintf(a.Out, "Auto Renew Status    : %s\n", orDefaultStr(info.AutoRenew, "disabled"))
		return nil

	case "remove":
		if err := a.rewriteHostConfigWithSSL(primary, domain, service, svcType, aliases, ""); err != nil {
			return err
		}
		_, hasState, _ := ssl.LoadState(a.Cfg.SSL, primary)
		if hasState {
			yes := fl.Has("yes") || fl.Has("y")
			if yes {
				ssl.RemoveState(a.Cfg.SSL, primary)
			} else if a.confirmYesDefaultNo(fmt.Sprintf("Delete WOR-managed certificate files for %s?", primary)) {
				ssl.RemoveHostDir(a.Cfg.SSL, primary)
			} else {
				ssl.RemoveState(a.Cfg.SSL, primary)
			}
		}
		a.ok("SSL removed from host config: %s", primary)
		return nil

	case "install":
		cert, key := fl.Get("cert", ""), fl.Get("key", "")
		if cert == "" || key == "" {
			return a.errf("--cert and --key are required")
		}
		dstCert, dstKey, err := ssl.InstallCustom(a.Cfg.SSL, primary, cert, key)
		if err != nil {
			return err
		}
		if err := a.rewriteHostConfigWithSSLFiles(primary, domain, service, svcType, aliases, "", dstCert, dstKey); err != nil {
			return err
		}
		if err := ssl.WriteState(a.Cfg.SSL, primary, "custom", dstCert, dstKey, "unsupported"); err != nil {
			return err
		}
		a.ok("Custom SSL installed: %s", primary)
		return nil
	}
	return nil
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

func orDefaultStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// rewriteHostConfigWithSSL regenerates a host's vhost file honoring
// whatever SSL state is already on record (used by `ssl remove`, which
// clears state first).
func (a *App) rewriteHostConfigWithSSL(host, domain, service, svcType string, aliases []string, preferred string) error {
	provider, err := a.Provider()
	if err != nil {
		return err
	}
	port := 0
	if domainmodel.TemplateRequiresPort(svcType) {
		port, _ = a.Store.GetServicePort(domain, service)
	}
	siteFile := provider.SiteAvailableFile(host)
	if err := a.writeHostConfig(provider, host, domain, service, svcType, port, siteFile, aliases, preferred); err != nil {
		return err
	}
	return provider.Reload()
}

// rewriteHostConfigWithSSLFiles is like rewriteHostConfigWithSSL but
// forces a specific cert/key pair (used right after issuing/installing
// a certificate, before the state file is written).
func (a *App) rewriteHostConfigWithSSLFiles(host, domain, service, svcType string, aliases []string, preferred, cert, key string) error {
	provider, err := a.Provider()
	if err != nil {
		return err
	}
	port := 0
	if domainmodel.TemplateRequiresPort(svcType) {
		port, _ = a.Store.GetServicePort(domain, service)
	}
	siteFile := provider.SiteAvailableFile(host)
	params, err := a.buildWriteParams(provider, host, domain, service, svcType, port, siteFile, aliases, preferred)
	if err != nil {
		return err
	}
	params.SSLEnabled = true
	params.SSLCertFile = cert
	params.SSLKeyFile = key
	if err := provider.WriteConfig(params); err != nil {
		return err
	}
	return provider.Reload()
}
