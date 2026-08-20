package cliapp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"wor/internal/domainmodel"
	"wor/internal/osutil"
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
	case "issue", "renew", "status", "remove", "install", "redirect", "sync":
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
		preferred := fl.Get("preferred", "")

		// Every provider follows the same three steps: obtain the
		// certificate, apply the resulting state to the vhost, then
		// persist the state. Persisting last means a vhost the web
		// server rejects leaves no state behind claiming a certificate
		// is in use.
		switch provider {
		case "letsencrypt":
			force := a.resolveForceHTTPS(fl, "letsencrypt", primary)
			// certbot's webroot authenticator answers the challenge out
			// of a directory the *existing* vhost has to serve. A host
			// created before this version has no ACME location in its
			// generated config, so the challenge would 404 (or, for a
			// SPA, be answered with index.html) and issuance would fail
			// with no way out. Regenerating the vhost from current
			// state first adds that location -- it is emitted whether
			// or not the host has a certificate yet, precisely so this
			// works.
			if err := a.prepareACMEWebroot(); err != nil {
				return err
			}
			if err := a.rewriteHostConfigWithSSL(primary, domain, service, svcType, aliases, preferred); err != nil {
				return fmt.Errorf("could not prepare %s to answer the ACME challenge: %w", primary, err)
			}
			if err := ssl.IssueLetsEncrypt(primary, aliases, a.Cfg.ACME, a.renewHookCommand(primary)); err != nil {
				return err
			}
			// The vhost points at wor's own copy, not certbot's store:
			// /etc/letsencrypt/archive is root-only, which an
			// unprivileged web server master (Homebrew on macOS) cannot
			// read at all. See internal/ssl/sync.go.
			cert, key, _, err := a.copyManagedCertificate(primary)
			if err != nil {
				return err
			}
			st := ssl.State{
				Provider:  "letsencrypt",
				CertFile:  cert,
				KeyFile:   key,
				AutoRenew: "enabled",
			}
			st.SetForceHTTPS(force)
			if err := a.applyHostConfigWithState(primary, domain, service, svcType, aliases, preferred, st); err != nil {
				return err
			}
			if err := ssl.WriteState(a.Cfg.SSL, primary, st); err != nil {
				return err
			}
			a.giveCertificateFilesToOperator(primary)
			a.ok("Let's Encrypt SSL installed: %s", primary)
			a.reportRedirect(primary, force)
			a.warnIfRenewalHookMissing(primary)
		case "self-signed":
			cert, key, err := ssl.IssueSelfSigned(a.Cfg.SSL, primary, aliases)
			if err != nil {
				return err
			}
			st := ssl.State{
				Provider:  "self-signed",
				CertFile:  cert,
				KeyFile:   key,
				AutoRenew: "unsupported",
			}
			st.SetForceHTTPS(a.resolveForceHTTPS(fl, "self-signed", primary))
			if err := a.applyHostConfigWithState(primary, domain, service, svcType, aliases, preferred, st); err != nil {
				return err
			}
			if err := ssl.WriteState(a.Cfg.SSL, primary, st); err != nil {
				return err
			}
			a.ok("Self-signed SSL installed: %s", primary)
			a.reportRedirect(primary, st.ForceHTTPSOr(false))
		case "custom":
			return a.errf("use: wor ssl install %s --cert=/path/fullchain.pem --key=/path/privkey.pem", primary)
		case "none":
			a.ok("SSL skipped: %s", primary)
		}
		return nil

	case "sync":
		return a.sslSync(primary, domain, service, svcType, aliases, fl.Get("preferred", ""))

	case "redirect":
		// `wor ssl redirect <host> on|off` -- the "add or remove it
		// later" path. Only the flag changes; the certificate is left
		// exactly as it is.
		var want bool
		switch onOff := positionalArg(args[2:]); onOff {
		case "on":
			want = true
		case "off":
			want = false
		default:
			return a.errf("use: wor ssl redirect %s on|off", primary)
		}
		st, ok, err := ssl.LoadState(a.Cfg.SSL, primary)
		if err != nil {
			return err
		}
		if !ok {
			return a.errf("%s has no certificate on record; run `wor ssl issue %s` first", primary, primary)
		}
		if st.Recorded() && st.ForceHTTPSOr(false) == want {
			a.info("HTTPS redirect for %s is already %s.", primary, onOffLabel(want))
			return nil
		}
		st.SetForceHTTPS(want)
		if err := a.applyHostConfigWithState(primary, domain, service, svcType, aliases, fl.Get("preferred", ""), st); err != nil {
			return err
		}
		if err := ssl.WriteState(a.Cfg.SSL, primary, st); err != nil {
			return err
		}
		a.ok("HTTPS redirect for %s is now %s.", primary, onOffLabel(want))
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
		if st, ok, _ := ssl.LoadState(a.Cfg.SSL, primary); ok {
			label := onOffLabel(a.storedForceHTTPS(st))
			if !st.Recorded() {
				label += " (inherited; never set for this host)"
			}
			fmt.Fprintf(a.Out, "HTTP -> HTTPS Redirect: %s\n", label)
		}
		return nil

	case "remove":
		// The state has to go before the vhost is regenerated, not
		// after. buildWriteParams reads the state file to decide
		// whether to emit the SSL directives at all, so regenerating
		// first produced a vhost still pointing at certificate files
		// this command was about to delete -- an invalid config that
		// only surfaced at the next reload, when it took every site on
		// the machine down with it.
		_, hasState, _ := ssl.LoadState(a.Cfg.SSL, primary)
		deleteFiles := false
		if hasState {
			// --yes keeps the files (only the config entry goes); that
			// inversion is long-standing behaviour, left as it is.
			if !fl.Has("yes") && !fl.Has("y") {
				deleteFiles = a.confirmYesDefaultNo(fmt.Sprintf("Delete WOR-managed certificate files for %s?", primary))
			}
			if err := ssl.RemoveState(a.Cfg.SSL, primary); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		// The certificate files are deleted only after the SSL-free
		// vhost has been validated and reloaded. Deleting first meant
		// that if the config test failed for any reason -- including a
		// pre-existing problem in an unrelated host, since the test is
		// machine-wide -- the rollback restored a vhost referencing
		// files that no longer existed, leaving the whole web server
		// unable to reload. That is the exact failure this change is
		// meant to prevent, so the destructive step goes last.
		if err := a.rewriteHostConfigWithSSL(primary, domain, service, svcType, aliases, ""); err != nil {
			return err
		}
		if deleteFiles {
			if err := ssl.RemoveHostDir(a.Cfg.SSL, primary); err != nil {
				a.warn("could not delete the certificate files for %s: %s", primary, err)
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
		st := ssl.State{
			Provider:  "custom",
			CertFile:  dstCert,
			KeyFile:   dstKey,
			AutoRenew: "unsupported",
		}
		st.SetForceHTTPS(a.resolveForceHTTPS(fl, "custom", primary))
		if err := a.applyHostConfigWithState(primary, domain, service, svcType, aliases, "", st); err != nil {
			return err
		}
		if err := ssl.WriteState(a.Cfg.SSL, primary, st); err != nil {
			return err
		}
		a.ok("Custom SSL installed: %s", primary)
		a.reportRedirect(primary, st.ForceHTTPSOr(false))
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
	return a.writeHostConfig(provider, host, domain, service, svcType, port, siteFile, aliases, preferred)
}

// applyHostConfigWithState is like rewriteHostConfigWithSSL but uses the
// certificate state it is handed instead of whatever is on record. Used
// while issuing, installing or changing a certificate, when the new
// state has deliberately not been persisted yet -- so a vhost the web
// server rejects leaves no state file claiming otherwise.
func (a *App) applyHostConfigWithState(host, domain, service, svcType string, aliases []string, preferred string, st ssl.State) error {
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
	params.SSLCertFile = st.CertFile
	params.SSLKeyFile = st.KeyFile
	params.ForceHTTPS = a.storedForceHTTPS(st)
	return a.applyHostParams(provider, params)
}

// certificateOwner returns the uid/gid wor's certificate copies should
// belong to, derived from WOR_HOME's own ownership.
//
// Derived, never stored. The renewal hook runs from certbot as plain
// root with no SUDO_USER to read, so the operator has to be worked out
// some other way -- and baking a numeric uid into the hook command at
// issue time would go stale the moment the machine is rebuilt or the
// account changes, handing a private key to whichever unrelated account
// then holds that number. WOR_HOME is the operator's workspace by
// definition, and reading its owner is correct at the moment it is
// read.
//
// The guard matters: a root-owned WOR_HOME would make this hand the
// certificate to root and quietly recreate the problem the copy exists
// to fix. That is not hypothetical -- osutil.ClaimOwnership exists
// because WOR_HOME has been found root-owned in the field, left behind
// by an older install.
func (a *App) certificateOwner() (uid, gid int, err error) {
	uid, gid, err = osutil.FileOwner(a.Cfg.WorHome)
	if err != nil {
		return 0, 0, err
	}
	if uid != 0 {
		return uid, gid, nil
	}
	// A root-owned WOR_HOME is legitimate on a server whose only
	// account is root -- DESIGN.md section 4 supports exactly that, and
	// there ClaimOwnership never had anything to change. Distinguish it
	// from the accident by who is asking: if the operator running wor
	// is root, root really is the owner. If an unprivileged user is
	// looking at a root-owned WOR_HOME, that is the leftover-from-an-
	// older-install case, and handing the private key to root would
	// quietly recreate the unreadable-certificate problem this copy
	// exists to solve.
	if os.Geteuid() == 0 {
		return uid, gid, nil
	}
	return 0, 0, a.errf("WOR_HOME (%s) is owned by root, but you are not, so wor cannot tell which user should own the certificate files.\n"+
		"Give it back to the operator first (for example: sudo chown -R $(id -un) %s), then run this again.",
		a.Cfg.WorHome, a.Cfg.WorHome)
}

// renewHookCommand is the command certbot runs after each renewal. It
// is registered with --renew-hook, so certbot stores it but does not
// run it during the issuance wor itself drives -- see
// ssl.IssueLetsEncrypt for why running it there deadlocks wor against
// its own $WOR_HOME lock.
//
// Two things in it are easy to get wrong and both fail silently months
// later, which is why they are spelled out rather than left to the
// environment:
//
// The binary path is resolved, not assumed. The hook is stored in
// /etc/letsencrypt/renewal/<host>.conf and runs unattended, when "wor"
// will not be on root's PATH in every context.
//
// WOR_HOME is passed explicitly. certbot runs hooks as root, and wor
// resolves WOR_HOME from $WOR_HOME, then ~/.wor/config, then a
// per-OS default -- as root, "~" is root's home, so the config file the
// operator wrote is invisible and the default (on macOS, $HOME/wor)
// resolves to root's own directory instead of theirs. The hook would
// quietly sync into the wrong workspace.
//
// It is passed via env(1) rather than as a bare "WOR_HOME=... wor ..."
// prefix, because certbot validates a hook before it runs it: it takes
// the first whitespace-separated token of the string and refuses the
// whole command unless that token is an executable on PATH
// (certbot/_internal/hooks.py). A bare assignment makes that first
// token "WOR_HOME=/opt/wor", so certbot aborts the entire issuance with
// "Unable to find renew-hook command WOR_HOME=... in the PATH" before
// it ever contacts the ACME server. (That validation happens when the
// flag is parsed, so it still applies even though the hook itself no
// longer runs at issuance time.) Running the hook through a shell --
// which certbot does -- would have handled the prefix fine; the check
// that rejects it happens earlier, and does not. env(1) is POSIX, is on
// PATH everywhere wor supports, and sets the variable for exactly one
// command without a subshell.
//
// If the binary is moved or WOR_HOME changes after this is registered,
// the hook stops doing anything useful -- loudly for a moved WOR_HOME
// (the host has no state there, so sync errors out) and silently for a
// moved binary. The expiry warning in `wor health` is the net under
// both.
func (a *App) renewHookCommand(host string) string {
	self, err := os.Executable()
	if err != nil || self == "" {
		a.warn("could not resolve wor's own path; the renewal hook for %s will not be registered", host)
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}
	return fmt.Sprintf("env WOR_HOME=%s %s ssl sync %s", shellQuote(a.Cfg.WorHome), shellQuote(self), host)
}

// shellQuote wraps s in single quotes so a path with spaces survives
// the shell certbot runs hooks through.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// warnIfRenewalHookMissing checks that the hook actually landed in the
// renewal config.
//
// certbot only rewrites that file when it really obtains a certificate.
// Re-running issue against a certificate that is still valid can leave
// the previous settings in place -- including, for a host issued before
// this change, the old --nginx authenticator and no hook at all. The
// operator would then have a working site whose renewals silently stop
// refreshing wor's copy, which is precisely the failure this design is
// built to avoid. Better to say so than to assume.
func (a *App) warnIfRenewalHookMissing(host string) {
	present, err := ssl.RenewalConfHasRenewHook(host)
	if err != nil || present {
		return
	}
	a.warn("certbot's renewal config for %s has no renewal hook, so renewals will not refresh wor's copy of the certificate.", host)
	a.info("This happens when the existing certificate was still valid and certbot kept it rather than reissuing.")
	a.info("Run `wor ssl sync %s` after each renewal, or force a reissue to register the hook.", host)
}

// copyManagedCertificate refreshes wor's copy of host's certificate
// from the provider's own store.
func (a *App) copyManagedCertificate(host string) (cert, key string, changed bool, err error) {
	uid, gid, err := a.certificateOwner()
	if err != nil {
		return "", "", false, err
	}
	certDir := ssl.LetsEncryptCertDir(host)
	return ssl.CopyCertificate(a.Cfg.SSL, host,
		filepath.Join(certDir, ssl.CertFileName),
		filepath.Join(certDir, ssl.KeyFileName),
		uid, gid)
}

// sslSync refreshes wor's copy of a host's certificate from the
// provider's store and applies it. It is three things at once: what
// certbot's deploy hook calls after every renewal, how a host issued
// before wor kept its own copy is migrated, and the manual repair when
// the copy and the source have drifted.
//
// It never prompts when invoked from the hook, because certbot runs
// hooks non-interactively -- running as root there, /etc/letsencrypt is
// directly readable and nothing needs elevating. Run by hand as the
// operator, reading the private key does need elevation, and goes
// through osutil's existing confirm-once gate.
func (a *App) sslSync(host, domain, service, svcType string, aliases []string, preferred string) error {
	st, ok, err := ssl.LoadState(a.Cfg.SSL, host)
	if err != nil {
		return err
	}
	if !ok {
		return a.errf("%s has no certificate on record; nothing to sync", host)
	}
	srcCert, srcKey, syncable := ssl.SourcePaths(host, st)
	// certbot tells a deploy hook which lineage it just renewed. Trust
	// that over the name-derived path: a certificate that was ever
	// deleted and recreated lives under "<host>-0001", and copying from
	// the stale "live/<host>" would report success while serving the
	// old certificate until it expired.
	if lineage := os.Getenv("RENEWED_LINEAGE"); syncable && lineage != "" {
		srcCert = filepath.Join(lineage, ssl.CertFileName)
		srcKey = filepath.Join(lineage, ssl.KeyFileName)
	}
	if !syncable {
		// self-signed and custom certificates already live in
		// WOR_HOME: the copy is the original, so there is no source to
		// refresh from.
		a.info("%s uses a %s certificate, which wor already owns; nothing to sync.", host, st.Provider)
		return nil
	}

	uid, gid, err := a.certificateOwner()
	if err != nil {
		a.recordSync(host, ssl.SyncResult{OK: false, Source: srcCert, Error: err.Error()})
		return err
	}
	cert, key, changed, err := ssl.CopyCertificate(a.Cfg.SSL, host, srcCert, srcKey, uid, gid)
	if err != nil {
		a.recordSync(host, ssl.SyncResult{OK: false, Source: srcCert, Error: err.Error()})
		return err
	}

	// The vhost needs rewriting when the certificate bytes moved, and
	// also when they did not but the recorded paths still point
	// somewhere else -- which is exactly the migration case: a host
	// issued before wor kept its own copy has identical content but
	// state pointing at /etc/letsencrypt. Comparing only `changed`
	// would leave it there forever.
	needsRewrite := changed || st.CertFile != cert || st.KeyFile != key
	st.CertFile, st.KeyFile = cert, key
	if !needsRewrite {
		// Nothing moved. Do not reload: this runs from a renewal hook,
		// and churning the web server for an unchanged certificate is
		// exactly the noise that makes people stop trusting hooks.
		a.recordSync(host, ssl.SyncResult{OK: true, Changed: false, Source: srcCert})
		a.giveCertificateFilesToOperator(host)
		a.info("%s is already up to date.", host)
		return nil
	}

	if err := a.applyHostConfigWithState(host, domain, service, svcType, aliases, preferred, st); err != nil {
		a.recordSync(host, ssl.SyncResult{OK: false, Changed: true, Source: srcCert, Error: err.Error()})
		return err
	}
	if err := ssl.WriteState(a.Cfg.SSL, host, st); err != nil {
		a.recordSync(host, ssl.SyncResult{OK: false, Changed: true, Source: srcCert, Error: err.Error()})
		return err
	}
	a.recordSync(host, ssl.SyncResult{OK: true, Changed: true, Source: srcCert})
	a.giveCertificateFilesToOperator(host)
	a.ok("Certificate for %s refreshed from %s", host, srcKey)
	return nil
}

// recordSync stores the outcome so `wor ssl status`/`health`/`diagnose`
// can explain a stale certificate later. A failure to record is only
// worth a warning: it must never turn a successful sync into a failed
// one.
func (a *App) recordSync(host string, r ssl.SyncResult) {
	r.At = time.Now().Format(time.RFC3339)
	if err := ssl.WriteSyncResult(a.Cfg.SSL, host, r); err != nil {
		a.warn("could not record the sync result for %s: %s", host, err)
	}
}

// prepareACMEWebroot makes sure the challenge directory exists and
// belongs to the operator account before certbot is invoked.
//
// ensureRootDirs only runs from `wor setup`, so on a workspace created
// by an earlier version this directory does not exist -- and certbot,
// running as root, would create it itself, leaving a root-owned tree
// inside WOR_HOME.
//
// EnsureOwnedBy, not ClaimOwnership: this is the one directory outside
// `wor setup` that gets handed to somebody on a plain `wor ssl` run, and
// "claim it for whoever is typing" is exactly the rule the operator
// account exists to replace. Left as ClaimOwnership, an admin who is not
// the operator would sudo-chown the ACME tree to themselves on every
// certificate operation -- reintroducing, one directory at a time, the
// drift that setup had just finished sweeping out.
//
// With no operator configured EnsureOwnedBy falls through to
// ClaimOwnership, so hosts that never ran the operator step behave
// exactly as before.
func (a *App) prepareACMEWebroot() error {
	for _, dir := range []string{a.Cfg.ACME, filepath.Join(a.Cfg.ACME, ".well-known"), filepath.Join(a.Cfg.ACME, ".well-known", "acme-challenge")} {
		if err := osutil.EnsureDir(dir); err != nil {
			return err
		}
		if err := osutil.EnsureOwnedBy(dir, a.Cfg.OperatorUser); err != nil {
			return err
		}
	}
	return nil
}

// giveCertificateFilesToOperator hands everything wor keeps for host
// back to the operator. Only meaningful when running as root from the
// renewal hook, where the state and sync records would otherwise be
// created root-owned and mode 0600 -- unreadable to the operator
// afterwards. A failure here is a warning, not an error: the
// certificate itself is already in place.
func (a *App) giveCertificateFilesToOperator(host string) {
	uid, gid, err := a.certificateOwner()
	if err != nil {
		return
	}
	if err := ssl.ChownHostDir(a.Cfg.SSL, host, uid, gid); err != nil {
		a.warn("could not set ownership on the certificate files for %s: %s", host, err)
	}
}

// storedForceHTTPS resolves a host's redirect setting, supplying the
// behaviour the active web server had before the setting existed for
// state files that predate it.
//
// apache used to redirect to HTTPS whenever a certificate was present
// and gave no way to switch it off; nginx never redirected. Reading an
// absent value as the active provider's old behaviour is what keeps an
// upgrade from changing either one silently -- in particular from
// dropping an apache site back to plaintext on :80. The first explicit
// `wor ssl issue`/`wor ssl redirect` records a real value and this
// fallback stops applying.
func (a *App) storedForceHTTPS(st ssl.State) bool {
	return st.ForceHTTPSOr(a.Cfg.HostProviderName() == "apache")
}

// isLocalHostname reports whether host can only ever resolve on this
// machine or LAN, and so can never carry a publicly trusted
// certificate: a bare name with no dot, or one of the reserved suffixes
// (RFC 6761/6762).
//
// Only used to pick the default answer for the redirect prompt. A local
// site forced onto HTTPS with a certificate no browser trusts is a site
// nobody can open without clicking through a warning every time, so the
// default there is off.
//
// Service.DomainType is deliberately not consulted: it is stored once
// per service while Hosts is a list, so a service bound to both a
// public and a local name has a single value that must be wrong for one
// of them. The hostname itself is the only thing that describes the
// host being asked about.
func isLocalHostname(host string) bool {
	h := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if h == "" {
		return false
	}
	if !strings.Contains(h, ".") {
		return true
	}
	for _, suffix := range []string{".local", ".localhost", ".test", ".example", ".invalid"} {
		if strings.HasSuffix(h, suffix) {
			return true
		}
	}
	return false
}

// defaultForceHTTPS is the redirect setting offered when a certificate
// is issued: on for a certificate browsers will trust, off for a
// self-signed one, and off for any local hostname regardless.
//
// WOR_ENV is deliberately not an input. It is machine-wide, while this
// is a per-host decision, and internal/config infers it when the user
// never set one -- so a production host left at "development" would
// quietly stop redirecting, with nothing reporting it. The provider
// already carries the same intent and is always explicit. See
// docs/ssl-redesign.md.
func defaultForceHTTPS(provider, host string) bool {
	if isLocalHostname(host) {
		return false
	}
	return provider == "letsencrypt" || provider == "custom"
}

// resolveForceHTTPS decides the redirect setting for a newly issued
// certificate: --redirect/--no-redirect for automation, otherwise a
// prompt whose default comes from defaultForceHTTPS. The answer is
// resolved once, here, and then stored -- it is never recomputed from
// the provider or hostname on later reads.
func (a *App) resolveForceHTTPS(fl flags, provider, host string) bool {
	switch {
	case fl.Has("redirect"):
		return true
	case fl.Has("no-redirect"):
		return false
	}
	question := fmt.Sprintf("Redirect http://%s to https?", host)
	if defaultForceHTTPS(provider, host) {
		return a.confirmYesDefaultYes(question)
	}
	return a.confirmYesDefaultNo(question)
}

// reportRedirect tells the operator which way the redirect ended up,
// and how to change it, so the setting is never something they have to
// remember answering.
func (a *App) reportRedirect(host string, force bool) {
	a.info("HTTP -> HTTPS redirect: %s (change it with `wor ssl redirect %s on|off`)", onOffLabel(force), host)
}

func onOffLabel(v bool) string {
	if v {
		return "on"
	}
	return "off"
}

// positionalArg returns the first argument that is not a --flag.
func positionalArg(args []string) string {
	for _, arg := range args {
		if !strings.HasPrefix(arg, "--") {
			return arg
		}
	}
	return ""
}
