package cliapp

// wor diagnose -- single-service root-cause analysis. See
// docs/diagnose.md for the agreed design. The division of labor with
// the neighboring commands:
//
//	wor doctor   -- whole-host health (runtimes installed, workspace ok)
//	wor info     -- one service's status, no judgement
//	wor diagnose -- one service's ROOT CAUSE + suggested fix commands
//	wor run      -- the recovery command (ensure everything enabled is up)
//
// Checks run outside-in along the request path (config -> dns -> web
// server -> ssl -> process -> port -> http -> files -> disk -> logs).
// Every failing check contributes a *cause* (see diagCause); causes of
// the same kind merge, log evidence corroborates them, and the final
// verdict names exactly ONE root cause with its evidence and fix --
// per the project owner's core requirement: the admin must never have
// to interpret a pile of [FAIL] rows themselves; that synthesis is the
// whole reason wor diagnose exists over running systemctl/pm2/nginx -t
// /curl by hand. Up to two lower-ranked "Other possibilities" follow,
// as the safety net for when the ranking guesses wrong.
//
// Three hard rules, all agreed with the project owner before
// implementation:
//
//  1. Read-only. Never auto-fix anything -- print suggested commands
//     only; the admin decides and executes (AGENTS.md Safety Rules).
//  2. Non-interactive. No check may trigger the confirm-once sudo
//     prompt (osutil.SudoCommand); everything runs unelevated,
//     degrading to "not verified (needs root)" instead of prompting --
//     so `wor diagnose` is safe in cron/monitoring, where its exit
//     code (0 = healthy, 1 = problem found) is the whole point.
//  3. Fast. Every network probe is bounded by a short timeout; the
//     whole command must finish promptly even when everything is down,
//     because a diagnostic tool that hangs during an outage is worse
//     than none.

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"wor/internal/domainmodel"
	"wor/internal/hostprovider"
	"wor/internal/hostsfile"
	"wor/internal/osutil"
	"wor/internal/phpfpm"
	"wor/internal/pm2"
	"wor/internal/ssl"
	"wor/internal/systemd"
)

// Probe timeouts. Deliberately short (rule 3 above): during an outage
// the interesting result is usually "refused"/"timed out", and both
// should surface in seconds, not minutes.
const (
	diagDNSTimeout  = 3 * time.Second
	diagHTTPTimeout = 5 * time.Second
	diagDialTimeout = 1500 * time.Millisecond
)

// diagLogLines is how many trailing lines each log source contributes
// to pattern scanning; the Evidence block is capped per source
// (diagEvidencePerSource) and per line width (diagEvidenceWidth) so
// the verdict stays readable -- real nginx/journal lines routinely run
// several hundred characters.
const (
	diagLogLines           = 30
	diagEvidencePerSource  = 3
	diagEvidenceWidth      = 160
	diagOtherPossibilities = 2
)

// diagLogFreshness bounds how old a web-server error-log line may be
// and still count as evidence. diagLogs runs AFTER the http probes, so
// a problem that exists right now has just written a fresh line;
// anything older predates the current state of the config and belongs
// to a previous incident. See freshLogLines.
const diagLogFreshness = time.Hour

// crash-loop heuristic: a process that pm2/systemd restarted this many
// times while its current uptime is still this short is flapping.
const (
	diagFlapRestarts = 5
	diagFlapWindow   = 10 * time.Minute
)

const (
	diagPass = "pass"
	diagWarn = "warn"
	diagFail = "fail"
	diagSkip = "skip"
)

// diagConfidence is the agreed three-level static ranking. Ordering
// matters: rankedCauses sorts descending on it.
type diagConfidence int

const (
	confLow diagConfidence = iota
	confMed
	confHigh
)

// diagCause is one candidate root cause. kind is a coarse family tag
// ("proc", "perm", "port", "tls", ...) used for merging: a second
// failure of the same kind -- e.g. an http 403 and a files-blocked
// check, or a "permission denied" log line -- is the same underlying
// problem seen from a different layer, so it merges into (and can
// raise the confidence of) the existing cause instead of appearing as
// a separate entry the admin would have to correlate themselves.
//
// details collects the [FAIL]-row detail of every check that merged
// into this cause; textDetail marks which of them supplied the current
// text. The rest render as the Summary's "Cascade" list -- downstream
// symptoms *proven* to be this same problem by the kind merge, never
// guessed causal links.
type diagCause struct {
	kind       string
	text       string
	conf       diagConfidence
	seq        int // creation order = request-path layer order, the tiebreaker
	fixes      []string
	details    []string
	textDetail string
}

// cascade returns the merged-in symptoms other than the one that named
// this cause.
func (c *diagCause) cascade() []string {
	var out []string
	skippedText := false
	for _, dt := range c.details {
		if dt == "" {
			continue
		}
		if dt == c.textDetail && !skippedText {
			skippedText = true
			continue
		}
		out = append(out, dt)
	}
	return out
}

func (c *diagCause) addFixes(fixes ...string) {
	for _, f := range fixes {
		if f == "" {
			continue
		}
		dup := false
		for _, have := range c.fixes {
			if have == f {
				dup = true
				break
			}
		}
		if !dup {
			c.fixes = append(c.fixes, f)
		}
	}
}

// evidenceItem is one raw evidence line tagged with where it came
// from; rendering groups by source (see renderEvidence).
type evidenceItem struct {
	source string
	line   string
}

// diagnosis accumulates one target's check results as they print, then
// renders the closing verdict (root cause / evidence / fix).
type diagnosis struct {
	a        *App
	useColor bool
	target   string // "domain/service", for fix-command rendering
	dir      string // service source directory, for fix-command rendering

	failed     bool
	failCount  int
	warnCount  int
	seq        int
	causes     []*diagCause
	notes      []string // secondary "Also worth checking" suggestions
	evidence   []evidenceItem
	sslEnabled bool // set by diagSSL, read by diagHTTP to pick the probe scheme

	// procKnown/procRunning carry the process layer's conclusion into
	// the port layer, which needs it to tell "our process listening"
	// from "some OTHER process squatting on our port".
	procKnown   bool
	procRunning bool
}

func (d *diagnosis) line(status, label, detail string) {
	var code, plain string
	switch status {
	case diagPass:
		code, plain = ansiGreen, "[PASS]"
	case diagWarn:
		code, plain = ansiYellow, "[WARN]"
	case diagFail:
		code, plain = ansiRed, "[FAIL]"
	default:
		code, plain = ansiDim, "[SKIP]"
	}
	fmt.Fprintf(d.a.Out, "%s %-9s %s\n", colorize(d.useColor, code, plain), label, detail)
}

func (d *diagnosis) pass(label, detail string) { d.line(diagPass, label, detail) }
func (d *diagnosis) skip(label, detail string) { d.line(diagSkip, label, detail) }

func (d *diagnosis) warn(label, detail string) {
	d.line(diagWarn, label, detail)
	d.warnCount++
}

// fail prints a [FAIL] line, marks the whole diagnosis failed (exit
// code 1), and records/merges a candidate root cause. The row's own
// detail is kept alongside the cause so the Summary can list merged-in
// failures as that cause's Cascade.
func (d *diagnosis) fail(label, detail, kind string, conf diagConfidence, cause string, fixes ...string) {
	d.line(diagFail, label, detail)
	d.failed = true
	d.failCount++
	d.addCauseDetail(kind, cause, conf, label+": "+detail, fixes...)
}

// addCause records a candidate root cause, merging with an existing
// cause of the same kind: confidence takes the max (a second sighting
// from another layer is corroboration), and when the newcomer is more
// confident its wording wins too -- e.g. a vague http 403 cause gets
// replaced by the files check's precise "www-data cannot traverse X"
// once that check runs.
func (d *diagnosis) addCause(kind, text string, conf diagConfidence, fixes ...string) {
	d.addCauseDetail(kind, text, conf, "", fixes...)
}

func (d *diagnosis) addCauseDetail(kind, text string, conf diagConfidence, detail string, fixes ...string) {
	if c := d.causeByKind(kind); c != nil {
		if conf > c.conf {
			c.conf = conf
			c.text = text
			c.textDetail = detail
		}
		if detail != "" {
			c.details = append(c.details, detail)
		}
		c.addFixes(fixes...)
		return
	}
	d.seq++
	c := &diagCause{kind: kind, text: text, conf: conf, seq: d.seq, textDetail: detail}
	if detail != "" {
		c.details = append(c.details, detail)
	}
	c.addFixes(fixes...)
	d.causes = append(d.causes, c)
}

func (d *diagnosis) causeByKind(kind string) *diagCause {
	for _, c := range d.causes {
		if c.kind == kind {
			return c
		}
	}
	return nil
}

// rankedCauses orders candidates by confidence (high first), breaking
// ties by request-path layer order -- the earlier layer is the more
// likely origin, later same-confidence failures its symptoms.
func (d *diagnosis) rankedCauses() []*diagCause {
	ranked := make([]*diagCause, len(d.causes))
	copy(ranked, d.causes)
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].conf != ranked[j].conf {
			return ranked[i].conf > ranked[j].conf
		}
		return ranked[i].seq < ranked[j].seq
	})
	return ranked
}

func (d *diagnosis) note(s string) {
	for _, have := range d.notes {
		if have == s {
			return
		}
	}
	d.notes = append(d.notes, s)
}

func (d *diagnosis) addEvidence(source string, lines ...string) {
	for _, l := range lines {
		if l = strings.TrimSpace(l); l != "" {
			d.evidence = append(d.evidence, evidenceItem{source: source, line: l})
		}
	}
}

// cmdDiagnose implements `wor diagnose <host|domain/service>` --
// single-target only; the fleet-wide sweep is `wor health` (cmdHealth,
// below). Returns (true, nil) when a problem was found, which Run
// turns into exit code 1 the same way `wor doctor` does.
func (a *App) cmdDiagnose(args []string) (bool, error) {
	if len(args) == 0 {
		a.usage()
		return false, a.errf("target required: wor diagnose <host|domain/service> (for a fleet-wide sweep: wor health)")
	}

	// Target resolution: same shape as cmdInfo -- a bare host resolves
	// through the registry, "domain/service" is used directly.
	target := args[0]
	resolved := target
	probeHost := ""
	if !containsSlash(target) {
		r, ok := a.Store.ResolveHost(target)
		if !ok {
			return false, a.errf("host not found in services.config.json: %s", target)
		}
		resolved = r
		probeHost = target
	}
	domain, service, err := domainmodel.ParseTarget(resolved)
	if err != nil {
		return false, err
	}
	cfg, err := a.Store.LoadServices(domain)
	if err != nil {
		return false, a.errf("could not load services for domain %s: %s", domain, err)
	}
	svc := cfg.FindService(service)
	if svc == nil {
		return false, a.errf("service not found: %s/%s", domain, service)
	}
	hosts, _ := a.Store.ListHostsForService(domain, service)
	if probeHost == "" && len(hosts) > 0 {
		probeHost = hosts[0]
	}

	d := &diagnosis{
		a:        a,
		useColor: a.colorEnabled(),
		target:   domain + "/" + service,
		dir:      a.Store.ServiceDir(domain, service),
	}

	a.printDiagHeader(d, svc, probeHost, len(hosts))

	// Check rows print live, one per completed check -- during an
	// outage the probes below can take several seconds each, and a
	// silent screen is worse than a progressing checklist. That is why
	// the Summary (root cause) comes AFTER the Checks section, closest
	// to the prompt, rather than buffering everything to put it first.
	diagSection(a.Out, "Checks")
	a.diagConfig(d, svc)
	a.diagDNS(d, probeHost, domain, service)
	a.diagWebServer(d, probeHost)
	a.diagSSL(d, probeHost)
	a.diagProcess(d, domain, svc)
	a.diagPort(d, svc)
	a.diagHTTP(d, probeHost, svc)
	a.diagReachability(d, domain, service, svc.Type)
	a.diagDisk(d)
	a.diagLogs(d, domain, svc, probeHost)
	a.diagDeployCorrelation(d)

	d.printVerdict()
	return d.failed, nil
}

// printDiagHeader renders the summary block the owner asked for:
//
//	Target : com-moodasoft/cdn
//	Host   : cdn.moodasoft.com  [ssl: letsencrypt]  (+1 more)
//	Runtime: php 8.4 (dedicated php-fpm pool)
//	Server : nginx (nginx/1.24.0)
func (a *App) printDiagHeader(d *diagnosis, svc *domainmodel.Service, probeHost string, hostCount int) {
	fmt.Fprintln(a.Out, "WOR Diagnose")
	fmt.Fprintln(a.Out, "------------")
	fmt.Fprintf(a.Out, "Target : %s\n", d.target)
	if probeHost == "" {
		fmt.Fprintln(a.Out, "Host   : (none registered)")
	} else {
		extra := ""
		if hostCount > 1 {
			extra = fmt.Sprintf("  (+%d more)", hostCount-1)
		}
		fmt.Fprintf(a.Out, "Host   : %s%s%s\n", probeHost, hostSSLSuffix(a, probeHost), extra)
	}
	fmt.Fprintf(a.Out, "Runtime: %s\n", a.diagRuntimeLabel(svc))
	fmt.Fprintf(a.Out, "Server : %s\n", a.diagServerLabel())
}

// diagRuntimeLabel names the service's runtime with a live version
// where cheap to get (osutil.RunVersion is a single exec).
func (a *App) diagRuntimeLabel(svc *domainmodel.Service) string {
	switch {
	case domainmodel.NodeTemplates[svc.Type]:
		return strings.TrimSpace("node " + osutil.RunVersion("node", "--version"))
	case domainmodel.GoTemplates[svc.Type]:
		return strings.TrimPrefix(osutil.RunVersion("go", "version"), "go version ")
	case domainmodel.PythonTemplates[svc.Type]:
		bin := "python3"
		if !osutil.Exists(bin) && osutil.Exists("python") {
			bin = "python"
		}
		return osutil.RunVersion(bin, "--version")
	case domainmodel.TemplateRequiresPHP(svc.Type):
		if svc.UsesPerServicePHPFPM() {
			return fmt.Sprintf("php %s (dedicated php-fpm pool)", svc.PHPVersion)
		}
		return "php (host-wide PHP_FPM_ENDPOINT)"
	default:
		return "static (no runtime process)"
	}
}

// diagServerLabel names the active host provider with its version.
func (a *App) diagServerLabel() string {
	provider, err := a.Provider()
	if err != nil {
		return "(not configured)"
	}
	bin, ok := provider.Binary()
	if !ok {
		return provider.Name + " (not installed)"
	}
	version := osutil.RunVersion(bin, "-v")
	version = strings.TrimPrefix(version, "nginx version: ")
	return fmt.Sprintf("%s (%s)", provider.Name, version)
}

// --- layer 1: config -------------------------------------------------

// resolveDocroot returns the absolute document root for a static/php
// service. DocumentRoot and PublicPath are commonly stored *relative*
// to the service directory (a real-host run surfaced "public" stored
// verbatim in DocumentRoot, which the first version of this check
// stat'ed as-is and falsely reported missing), so any relative value
// is resolved against serviceDir first.
func resolveDocroot(serviceDir string, svc *domainmodel.Service) string {
	docroot := svc.DocumentRoot
	if docroot == "" {
		docroot = svc.PublicPath
	}
	if docroot == "" {
		docroot = "public"
	}
	if !filepath.IsAbs(docroot) {
		docroot = filepath.Join(serviceDir, docroot)
	}
	return docroot
}

func (a *App) diagConfig(d *diagnosis, svc *domainmodel.Service) {
	if !svc.Enabled {
		d.fail("config", "service is disabled", "config", confHigh,
			"service is disabled in services.config.json",
			fmt.Sprintf("set \"enabled\": true for %s in services.config.json, then: wor run", d.target))
		return
	}
	if err := a.requireTemplateRuntime(svc.Type); err != nil {
		d.fail("config", fmt.Sprintf("runtime check failed: %s", err), "runtime", confHigh,
			fmt.Sprintf("required runtime for %s template is missing", svc.Type),
			"wor doctor")
		return
	}

	if domainmodel.TemplateRequiresProcessSupervisor(svc.Type) {
		entry := svc.EntryPoint
		if entry == "" {
			entry = domainmodel.DefaultEntryPoint(svc.Type)
		}
		entryPath := filepath.Join(d.dir, entry)
		info, err := os.Stat(entryPath)
		if err != nil {
			d.fail("config", fmt.Sprintf("entry point missing: %s", entryPath), "deploy", confMed,
				fmt.Sprintf("entry point %s does not exist (source not deployed, or binary not built)", entry),
				fmt.Sprintf("wor deploy %s", d.target))
			return
		}
		if domainmodel.GoTemplates[svc.Type] && !osutil.IsWindows() && info.Mode().Perm()&0o111 == 0 {
			d.fail("config", fmt.Sprintf("entry binary is not executable: %s", entryPath), "exec", confMed,
				"go entry binary exists but is not executable",
				fmt.Sprintf("chmod +x %s", entryPath))
			return
		}
		if svc.Port == 0 {
			d.warn("config", fmt.Sprintf("enabled (%s), entry %s found -- but no port configured", svc.Type, entry))
			return
		}
		d.pass("config", fmt.Sprintf("enabled (%s, port %d), entry %s found", svc.Type, svc.Port, entry))
		return
	}

	// static/php: the web server reads files off disk -- the document
	// root itself must exist.
	docroot := resolveDocroot(d.dir, svc)
	if !dirExists(docroot) {
		d.fail("config", fmt.Sprintf("document root missing: %s", docroot), "deploy", confMed,
			"document root directory does not exist (source not deployed?)",
			fmt.Sprintf("wor deploy %s", d.target))
		return
	}
	d.pass("config", fmt.Sprintf("enabled (%s), document root found", svc.Type))
}

// --- layer 2: dns / hosts file ---------------------------------------

func (a *App) diagDNS(d *diagnosis, probeHost, domain, service string) {
	if probeHost == "" {
		d.warn("dns", "no host registered for this service (wor host add <host> --target="+d.target+")")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), diagDNSTimeout)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupHost(ctx, probeHost)
	if err != nil {
		if a.Store.ServiceDomainType(domain, service) == "local" {
			if exists, _ := hostsfile.EntryExists(probeHost); !exists {
				d.fail("dns", fmt.Sprintf("%s does not resolve and has no hosts-file entry", probeHost), "dns", confHigh,
					fmt.Sprintf("local host %s has no entry in the system hosts file", probeHost),
					fmt.Sprintf("wor host add %s --target=%s --replace --add-hosts", probeHost, d.target))
				return
			}
			d.warn("dns", fmt.Sprintf("%s has a hosts-file entry but did not resolve within %s", probeHost, diagDNSTimeout))
			return
		}
		d.fail("dns", fmt.Sprintf("%s does not resolve: %s", probeHost, err), "dns", confMed,
			fmt.Sprintf("DNS does not resolve %s", probeHost),
			fmt.Sprintf("check the DNS A/AAAA record for %s at your DNS provider", probeHost))
		return
	}
	local := localAddrSet()
	for _, addr := range addrs {
		ip := net.ParseIP(addr)
		if local[addr] || (ip != nil && ip.IsLoopback()) {
			d.pass("dns", fmt.Sprintf("%s -> %s (this machine)", probeHost, addr))
			return
		}
	}
	// Resolving to a foreign address is not automatically wrong (CDN,
	// reverse proxy, NAT'd public IP the interfaces don't show), so
	// this warns rather than fails -- but it's exactly the hint needed
	// when everything below passes and the site is still down.
	shown := addrs
	if len(shown) > 2 {
		shown = shown[:2]
	}
	d.warn("dns", fmt.Sprintf("%s -> %s -- not an address on this machine (CDN/proxy/NAT, or DNS pointing elsewhere)",
		probeHost, strings.Join(shown, ", ")))
}

// localAddrSet returns every IP bound to this machine's interfaces,
// keyed by string form, for diagDNS's "does the host point at me"
// comparison. Best-effort: an error just yields an empty set (and a
// warn-level "not this machine" line, never a hard fail).
func localAddrSet() map[string]bool {
	set := map[string]bool{}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return set
	}
	for _, addr := range addrs {
		switch v := addr.(type) {
		case *net.IPNet:
			set[v.IP.String()] = true
		case *net.IPAddr:
			set[v.IP.String()] = true
		}
	}
	return set
}

// --- layer 3: web server ---------------------------------------------

// classifyConfigTest interprets an UNELEVATED `nginx -t`/`httpd -t`
// run. Exit 0 is "ok". A non-zero exit is only "broken" when an
// [emerg]/"Syntax error" line points at an actual config problem;
// error lines caused by permission (reading root-only cert/log files
// -- e.g. /etc/letsencrypt/live is 0700, so unelevated -t reliably
// emits "[emerg] cannot load certificate ... Permission denied" on a
// perfectly healthy host, a false FAIL a real-host run surfaced) mean
// the test simply cannot run without root: "unverified".
func classifyConfigTest(out string, err error) (verdict string, evidence []string) {
	if err == nil {
		return "ok", nil
	}
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, "[emerg]") && !strings.Contains(line, "Syntax error") {
			continue
		}
		if strings.Contains(strings.ToLower(line), "permission denied") {
			continue // can't-read-as-non-root, not a broken config
		}
		evidence = append(evidence, strings.TrimSpace(line))
	}
	if len(evidence) > 0 {
		return "broken", evidence
	}
	return "unverified", nil
}

func (a *App) diagWebServer(d *diagnosis, probeHost string) {
	provider, err := a.Provider()
	if err != nil {
		d.skip("server", fmt.Sprintf("not checked: %s", err))
		return
	}
	label := provider.Name
	bin, ok := provider.Binary()
	if !ok {
		d.fail(label, "not installed", "server", confHigh,
			fmt.Sprintf("configured host provider %s is not installed", provider.Name),
			"wor doctor")
		return
	}
	if !provider.IsRunning() {
		d.fail(label, "not running", "server", confHigh,
			fmt.Sprintf("%s (the web server) is not running", provider.Name),
			"wor run")
		return
	}

	detail := "running"
	if probeHost != "" {
		if _, err := os.Stat(provider.SiteAvailableFile(probeHost)); err != nil {
			d.fail(label, fmt.Sprintf("running, but no vhost config for %s", probeHost), "vhost", confMed,
				fmt.Sprintf("no %s vhost config exists for %s", provider.Name, probeHost),
				fmt.Sprintf("wor host add %s --target=%s --replace", probeHost, d.target))
			return
		}
		if provider.SitesEnabled() != provider.SitesAvailable() {
			if _, err := os.Stat(provider.SiteEnabledFile(probeHost)); err != nil {
				d.fail(label, fmt.Sprintf("running, vhost for %s exists but is NOT enabled", probeHost), "vhost", confMed,
					fmt.Sprintf("vhost config for %s is not enabled (missing from sites-enabled)", probeHost),
					fmt.Sprintf("wor host add %s --target=%s --replace", probeHost, d.target))
				return
			}
		}
		detail += ", vhost ok"
	}

	out, testErr := exec.Command(bin, "-t").CombinedOutput()
	switch verdict, evidence := classifyConfigTest(string(out), testErr); verdict {
	case "ok":
		detail += ", config test ok"
	case "broken":
		d.addEvidence(label+" -t", evidence...)
		d.fail(label, "running, but config test FAILED (reloads will fail host-wide)", "server", confHigh,
			fmt.Sprintf("%s configuration is broken (config test failed)", provider.Name),
			fmt.Sprintf("sudo %s -t   # see the exact error, fix the file it names", bin))
		return
	default:
		detail += ", config not verified (needs root: sudo " + bin + " -t)"
	}
	d.pass(label, detail)
}

// --- layer 3b: ssl ----------------------------------------------------

func (a *App) diagSSL(d *diagnosis, probeHost string) {
	if probeHost == "" {
		d.skip("ssl", "(no host)")
		return
	}
	st, ok, _ := ssl.LoadState(a.Cfg.SSL, probeHost)
	if !ok || !st.Enabled {
		d.pass("ssl", "none on record (plain http)")
		return
	}
	d.sslEnabled = true
	if _, err := os.Stat(st.CertFile); err != nil {
		// Distinguish gone from unreadable: letsencrypt keeps
		// /etc/letsencrypt/live at 0700, so stat as a normal user gets
		// EACCES on a host whose cert is perfectly fine -- that's "can't
		// verify without root" (a real-host run surfaced this as a false
		// "cert file missing" FAIL), not a missing certificate.
		if os.IsNotExist(err) {
			d.fail("ssl", fmt.Sprintf("cert file missing: %s", st.CertFile), "tls", confMed,
				fmt.Sprintf("SSL is enabled for %s but its certificate file is gone", probeHost),
				fmt.Sprintf("wor ssl issue %s --provider=%s", probeHost, st.Provider))
			return
		}
		d.warn("ssl", fmt.Sprintf("%s cert not readable without root -- expiry not checked (sudo openssl x509 -enddate -noout -in %s)", st.Provider, st.CertFile))
		return
	}
	notAfter, err := certNotAfter(st.CertFile)
	if err != nil {
		if os.IsPermission(err) {
			d.warn("ssl", fmt.Sprintf("%s cert not readable without root -- expiry not checked", st.Provider))
			return
		}
		d.warn("ssl", fmt.Sprintf("%s cert present, expiry unreadable: %s", st.Provider, err))
		return
	}
	left := time.Until(notAfter)
	days := int(left.Hours() / 24)
	switch {
	case left <= 0:
		d.fail("ssl", fmt.Sprintf("certificate EXPIRED %dd ago (%s)", -days, notAfter.Format("2006-01-02")), "tls", confHigh,
			fmt.Sprintf("SSL certificate for %s has expired", probeHost),
			fmt.Sprintf("wor ssl renew %s", probeHost))
	case days < 14:
		d.warn("ssl", fmt.Sprintf("%s cert expires in %dd (%s) -- renew soon", st.Provider, days, notAfter.Format("2006-01-02")))
		d.note(fmt.Sprintf("wor ssl renew %s   # cert expires in %dd", probeHost, days))
	default:
		d.pass("ssl", fmt.Sprintf("%s cert valid (%dd left)", st.Provider, days))
	}
}

// certNotAfter reads a PEM certificate file and returns its NotAfter
// timestamp, in pure Go (crypto/x509) -- no openssl dependency, unlike
// `wor ssl status`'s shell-out, because diagnose must work on a
// stripped-down host mid-outage. The first CERTIFICATE block is the
// leaf in every fullchain.pem layout wor writes.
func certNotAfter(certFile string) (time.Time, error) {
	data, err := os.ReadFile(certFile)
	if err != nil {
		return time.Time{}, err
	}
	for {
		var block *pem.Block
		block, data = pem.Decode(data)
		if block == nil {
			return time.Time{}, fmt.Errorf("no CERTIFICATE block found")
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return time.Time{}, err
		}
		return cert.NotAfter, nil
	}
}

// --- layer 4: process --------------------------------------------------

func (a *App) diagProcess(d *diagnosis, domain string, svc *domainmodel.Service) {
	switch domainmodel.ProcessProviderFor(svc.Type) {
	case "pm2":
		a.diagProcessPM2(d, domain, svc)
	case "systemd":
		a.diagProcessSystemd(d, domain, svc)
	default:
		if !domainmodel.TemplateRequiresPHP(svc.Type) {
			d.skip("process", "static -- served directly by the web server, no process")
			return
		}
		a.diagProcessPHP(d, domain, svc)
	}
}

func (a *App) diagProcessPM2(d *diagnosis, domain string, svc *domainmodel.Service) {
	d.procKnown = true
	if !osutil.Exists("pm2") {
		d.fail("process", "pm2 not installed", "proc", confHigh,
			"pm2 (this service's process manager) is not installed",
			"npm install -g pm2", "wor run")
		return
	}
	procs, err := pm2.List()
	if err != nil {
		d.fail("process", fmt.Sprintf("pm2 daemon unreachable: %s", err), "proc", confHigh,
			"pm2 daemon is unreachable", "wor run")
		return
	}
	name := pm2.Name(domain, svc.Name)
	info, ok := procs[name]
	if !ok {
		worManaged := 0
		for n := range procs {
			if strings.HasPrefix(n, "wor_") {
				worManaged++
			}
		}
		if worManaged == 0 {
			// The known-weak-spot special case (see docs/diagnose.md and
			// the pm2-no-boot-persistence finding): after a reboot pm2
			// starts empty unless `pm2 startup` was registered. `wor run`
			// both restarts everything and offers to register it.
			d.fail("process", "pm2 is running NO wor-managed processes at all", "proc", confHigh,
				"pm2 lost all wor processes -- typical after a reboot without `pm2 startup` registered",
				"wor run   # restarts everything and offers to register pm2 boot persistence")
			return
		}
		d.fail("process", fmt.Sprintf("%s is not registered with pm2", name), "proc", confMed,
			"the service's process was never started (or was deleted from pm2)",
			"wor run")
		return
	}
	switch info.Status {
	case "online":
		d.procRunning = true
		up := formatUptime(info.Uptime)
		if up == "" {
			up = "<1m" // formatUptime renders sub-minute uptimes as empty
		}
		detail := fmt.Sprintf("pm2 online (up %s, %d restarts)", up, info.Restarts)
		if info.Restarts >= diagFlapRestarts && info.Uptime < diagFlapWindow {
			d.warn("process", detail+" -- flapping: restarted repeatedly, currently up only briefly")
			d.note(fmt.Sprintf("wor service logs %s   # process is flapping (%d restarts)", d.target, info.Restarts))
			return
		}
		d.pass("process", detail)
	case "errored":
		d.fail("process", fmt.Sprintf("pm2 status: errored (%d restarts)", info.Restarts), "proc", confHigh,
			"app crashes on start (pm2 gave up restarting it)",
			fmt.Sprintf("wor service logs %s", d.target),
			fmt.Sprintf("wor service restart %s", d.target))
	default:
		d.fail("process", fmt.Sprintf("pm2 status: %s", info.Status), "proc", confMed,
			fmt.Sprintf("process is %s, not running", info.Status),
			fmt.Sprintf("wor service start %s", d.target))
	}
}

func (a *App) diagProcessSystemd(d *diagnosis, domain string, svc *domainmodel.Service) {
	d.procKnown = true
	unit := systemd.Name(domain, svc.Name)
	if _, err := os.Stat(systemd.UnitPath(domain, svc.Name)); err != nil {
		d.fail("process", fmt.Sprintf("systemd unit %s is not installed", unit), "proc", confMed,
			"the service's systemd unit was never installed",
			"wor run")
		return
	}
	st, err := systemd.ShowDiagState(domain, svc.Name)
	if err != nil {
		d.fail("process", fmt.Sprintf("cannot query systemd: %s", err), "proc", confMed,
			"systemd state could not be queried",
			fmt.Sprintf("systemctl status %s", unit))
		return
	}
	switch {
	case st.ActiveState == "active":
		d.procRunning = true
		detail := fmt.Sprintf("systemd active (%s, %d restarts)", st.SubState, st.NRestarts)
		if st.NRestarts >= diagFlapRestarts {
			d.warn("process", detail+" -- has been restarted repeatedly")
			d.note(fmt.Sprintf("wor service logs %s   # process has restarted %d times", d.target, st.NRestarts))
			return
		}
		d.pass("process", detail)
	case st.ActiveState == "activating" && st.SubState == "auto-restart":
		d.fail("process", fmt.Sprintf("crash loop: systemd is auto-restarting it (%d restarts)", st.NRestarts), "proc", confHigh,
			"app crashes on start (systemd restart loop)",
			fmt.Sprintf("wor service logs %s", d.target))
	case st.ActiveState == "failed":
		kind, conf, cause, fix := describeSystemdResult(st, d.target)
		d.fail("process", fmt.Sprintf("systemd failed (result: %s, exit status %d, %d restarts)",
			st.Result, st.ExecMainStatus, st.NRestarts), kind, conf, cause, fix,
			fmt.Sprintf("wor service logs %s", d.target))
	default:
		d.fail("process", fmt.Sprintf("systemd %s/%s -- not running", st.ActiveState, st.SubState), "proc", confMed,
			"process is not started",
			"wor run")
	}
}

// describeSystemdResult maps systemd's Result= verdict to a cause
// family + confidence + human-readable root cause + the most direct
// fix suggestion.
func describeSystemdResult(st systemd.DiagState, target string) (kind string, conf diagConfidence, cause, fix string) {
	switch st.Result {
	case "oom-kill":
		return "oom", confHigh,
			"process was killed by the OOM killer -- the machine ran out of memory",
			"check memory (free -m); add swap or reduce the app's memory usage"
	case "exit-code":
		return "proc", confHigh,
			fmt.Sprintf("app exits with status %d on start", st.ExecMainStatus),
			fmt.Sprintf("wor service restart %s   # after fixing what the logs show", target)
	case "start-limit-hit":
		return "proc", confHigh,
			"crash loop: app failed so often systemd stopped retrying (start limit hit)",
			fmt.Sprintf("wor service restart %s   # after fixing what the logs show", target)
	case "signal", "core-dump":
		return "proc", confHigh,
			"app was killed by a signal (crash/core dump)",
			fmt.Sprintf("wor service logs %s", target)
	default:
		return "proc", confMed,
			fmt.Sprintf("systemd reports failure (result: %s)", st.Result),
			fmt.Sprintf("wor service logs %s", target)
	}
}

func (a *App) diagProcessPHP(d *diagnosis, domain string, svc *domainmodel.Service) {
	d.procKnown = true
	if !svc.UsesPerServicePHPFPM() {
		ep, ok := hostprovider.PHPFPMEndpoint(a.Cfg)
		if !ok {
			d.fail("process", "no PHP_FPM_ENDPOINT configured (legacy php service)", "config", confMed,
				"this legacy php service has no PHP-FPM endpoint configured",
				"wor setup")
			return
		}
		if endpointAccepting(ep) {
			d.procRunning = true
			d.pass("process", fmt.Sprintf("host-wide php-fpm endpoint accepting connections (%s)", ep))
			return
		}
		d.fail("process", fmt.Sprintf("host-wide php-fpm endpoint NOT accepting connections (%s)", ep), "proc", confHigh,
			"the shared PHP-FPM this service relies on is not accepting connections",
			"restart php-fpm (e.g. sudo systemctl restart php*-fpm), then: wor host reload")
		return
	}

	v, ok := phpfpm.ResolveVersion(svc.PHPVersion)
	if !ok {
		d.fail("process", fmt.Sprintf("PHP %s is no longer detected on this host", svc.PHPVersion), "runtime", confHigh,
			fmt.Sprintf("PHP %s (this service's pool version) is not installed anymore", svc.PHPVersion),
			"wor doctor")
		return
	}
	if phpfpm.PoolAlive(v, domain, svc.Name) {
		d.procRunning = true
		// The pool answering *wor* is not the same as it answering the
		// web server: the socket is 0660, wor dials it as the admin
		// user, nginx/apache dials it as its own run user. A pool whose
		// listen.owner/listen.group don't include the web user passes
		// every process-level check and still 502s in production (real
		// Debian host, 2026-07-06) -- so check the socket's permission
		// bits from the WEB USER's point of view before declaring PASS.
		if osutil.IsLinux() {
			if provider, err := a.Provider(); err == nil && (provider.Name == "nginx" || provider.Name == "apache") {
				webUser := webServerRunUser(provider.Name)
				sock := phpfpm.SocketPath(v, domain, svc.Name)
				if webUserExists(webUser) && socketDeniesUser(sock, webUser) {
					if og := unixOwnerGroupMode(sock); og != "" {
						d.addEvidence("php-fpm socket", fmt.Sprintf("%s is %s -- %s cannot connect", sock, og, webUser))
					}
					poolFile := phpfpm.PoolFilePath(v, domain, svc.Name)
					d.fail("process", fmt.Sprintf("pool is up but its socket denies the web server user (%s)", webUser), "perm", confHigh,
						fmt.Sprintf("%s cannot connect to this pool's socket -- listen.owner/listen.group in the pool config don't include the web server user, so every request through %s gets 502", webUser, provider.Name),
						// restart, NOT reload: php-fpm only chowns a
						// socket when binding it, and reload keeps
						// already-bound sockets -- a reload here leaves
						// the old ownership in place (verified on a
						// real Debian host, 2026-07-07).
						fmt.Sprintf("sudo sed -i 's/^listen.owner = .*/listen.owner = %s/; s/^listen.group = .*/listen.group = %s/' %s && sudo systemctl restart %s", webUser, webUser, poolFile, v.ReloadUnit))
					return
				}
			}
		}
		d.pass("process", fmt.Sprintf("php-fpm %s dedicated pool accepting connections", svc.PHPVersion))
		return
	}
	if _, err := os.Stat(phpfpm.PoolFilePath(v, domain, svc.Name)); err != nil {
		d.fail("process", fmt.Sprintf("pool config missing: %s", phpfpm.PoolFilePath(v, domain, svc.Name)), "config", confMed,
			"this service's php-fpm pool config file is gone",
			fmt.Sprintf("re-add the service to recreate its pool: wor service remove %s && wor service add %s", d.target, d.target))
		return
	}
	if !phpfpm.IsRunning(v) {
		d.fail("process", fmt.Sprintf("php-fpm %s master is not running", svc.PHPVersion), "proc", confHigh,
			fmt.Sprintf("the php-fpm %s master process is down", svc.PHPVersion),
			"wor run")
		return
	}
	d.fail("process", fmt.Sprintf("php-fpm %s master is up but this pool's socket is not accepting connections", svc.PHPVersion), "proc", confHigh,
		"the service's php-fpm pool did not come up under a running master (bad pool config?)",
		"wor run",
		fmt.Sprintf("sudo %s -t   # validate the php-fpm config", v.FPMBin))
}

// endpointAccepting dials a PHP_FPM_ENDPOINT value ("127.0.0.1:9000"
// or a unix socket path) and reports whether something accepts the
// connection -- the same "dial, don't parse process tables" signal
// phpfpm.PoolAlive uses.
func endpointAccepting(ep string) bool {
	network, addr := "tcp", ep
	if strings.HasPrefix(ep, "unix:") {
		network, addr = "unix", strings.TrimPrefix(ep, "unix:")
	} else if strings.HasPrefix(ep, "/") {
		network, addr = "unix", ep
	}
	conn, err := net.DialTimeout(network, addr, diagDialTimeout)
	if err != nil {
		// Permission denied on a unix socket means it exists and is
		// locked down to the web server's user (Debian's stock www.conf
		// does exactly this: www-data:www-data 0660) -- the wrong user
		// dialed it, not a dead endpoint. Same lesson as
		// phpfpm.PoolAlive's EACCES handling.
		return network == "unix" && errors.Is(err, os.ErrPermission)
	}
	conn.Close()
	return true
}

// --- layer 5: port -----------------------------------------------------

func (a *App) diagPort(d *diagnosis, svc *domainmodel.Service) {
	if !domainmodel.TemplateRequiresPort(svc.Type) {
		return // no row at all: port has no meaning for static/php
	}
	if svc.Port == 0 {
		d.warn("port", "no port configured for this service")
		return
	}
	listening := tcpListening(svc.Port)
	procDown := d.procKnown && !d.procRunning
	switch {
	case listening && !procDown:
		d.pass("port", fmt.Sprintf("something is listening on 127.0.0.1:%d", svc.Port))
	case listening && procDown:
		// The classic EADDRINUSE setup: our process is down but the
		// port is taken, so restarting will crash-loop until whatever
		// holds the port is dealt with.
		if holder := portListenerDetail(svc.Port); holder != "" {
			d.addEvidence("port", "held by "+holder)
		}
		d.fail("port", fmt.Sprintf("port %d is in use by ANOTHER process (ours is down)", svc.Port), "port", confHigh,
			fmt.Sprintf("port %d is occupied by another process -- this service cannot bind it", svc.Port),
			fmt.Sprintf("lsof -nP -iTCP:%d -sTCP:LISTEN   # identify the squatter, stop it or change this service's port", svc.Port),
			fmt.Sprintf("wor service restart %s", d.target))
	case !listening && d.procKnown && d.procRunning:
		d.fail("port", fmt.Sprintf("process is up but nothing listens on 127.0.0.1:%d", svc.Port), "port", confMed,
			fmt.Sprintf("the app is running but not listening on its configured port %d (PORT env ignored, or binding another port)", svc.Port),
			fmt.Sprintf("check the app reads the PORT environment variable (wor sets PORT=%d), then: wor service restart %s", svc.Port, d.target))
	default:
		d.skip("port", "(process not running)")
	}
}

func tcpListening(port int) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), diagDialTimeout)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// portListenerDetail best-effort identifies what listens on port, via
// lsof (present on most Linux distros and macOS). Returns "" whenever
// that doesn't work -- the diagnosis is complete without it, this is
// bonus evidence only.
func portListenerDetail(port int) string {
	if !osutil.Exists("lsof") {
		return ""
	}
	out, err := exec.Command("lsof", "-nP", fmt.Sprintf("-iTCP:%d", port), "-sTCP:LISTEN").Output()
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return ""
	}
	fields := strings.Fields(lines[1])
	if len(fields) < 2 {
		return strings.TrimSpace(lines[1])
	}
	return fmt.Sprintf("%s (pid %s)", fields[0], fields[1])
}

// --- layer 6: http probes ----------------------------------------------

func (a *App) diagHTTP(d *diagnosis, probeHost string, svc *domainmodel.Service) {
	// Probe 1: the app directly, bypassing the web server -- the
	// decisive split between "the app is broken" and "the web
	// server/proxy layer in front of it is broken". Kind "proc": a
	// dead direct probe is the process problem seen from one layer
	// out, so it corroborates (merges into) the process layer's cause
	// instead of standing beside it as a separate item.
	if domainmodel.TemplateRequiresPort(svc.Type) && svc.Port != 0 {
		addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(svc.Port))
		code, err := httpProbe(addr, "127.0.0.1", false)
		switch {
		case err == nil && code < 500:
			d.pass("http-app", fmt.Sprintf("direct %s -> %d", addr, code))
		case err == nil:
			d.warn("http-app", fmt.Sprintf("direct %s -> %d (app is up but erroring)", addr, code))
			d.note(fmt.Sprintf("wor service logs %s   # app answers %d when reached directly", d.target, code))
		case isTimeout(err):
			d.fail("http-app", fmt.Sprintf("direct %s -> no response within %s (app hung?)", addr, diagHTTPTimeout), "proc", confMed,
				"the app accepts connections but never answers (hung, deadlocked, or overloaded)",
				fmt.Sprintf("wor service restart %s", d.target))
		default:
			d.fail("http-app", fmt.Sprintf("direct %s -> %s", addr, connErrShort(err)), "proc", confMed,
				"the app is not accepting connections",
				fmt.Sprintf("wor service logs %s", d.target))
		}
	}

	// Probe 2: through the web server, exactly as a browser on this
	// machine would -- except dialing 127.0.0.1 directly so the result
	// reflects THIS host even when DNS points at a CDN/proxy. TLS
	// verification is deliberately skipped: wor supports self-signed
	// certs, and this probe asks "does the stack serve requests", not
	// "does a browser trust the cert" (the ssl layer above already
	// checked expiry). Kinds chosen for merging: 502/504 are the app
	// problem seen through the proxy ("proc"), 403 is the permission
	// problem the files layer will pin down precisely ("perm").
	if probeHost == "" {
		d.skip("http-host", "(no host to probe through the web server)")
		return
	}
	webPort := "80"
	if d.sslEnabled {
		webPort = "443"
	}
	code, err := httpProbe(net.JoinHostPort("127.0.0.1", webPort), probeHost, d.sslEnabled)
	prefix := fmt.Sprintf("via %s :%s -> ", probeHost, webPort)
	switch {
	case err != nil && isTimeout(err):
		d.fail("http-host", prefix+"no response (web server hung?)", "server", confMed,
			"the web server accepts connections but never answers",
			"wor host reload")
	case err != nil:
		d.fail("http-host", prefix+connErrShort(err), "server", confMed,
			fmt.Sprintf("the web server is not accepting connections on port %s", webPort),
			"wor run")
	case code >= 200 && code < 400:
		d.pass("http-host", fmt.Sprintf("%s%d", prefix, code))
	case code == 502 || code == 503:
		d.fail("http-host", fmt.Sprintf("%s%d", prefix, code), "proc", confMed,
			fmt.Sprintf("the web server is up but cannot reach the app behind it (%d)", code),
			fmt.Sprintf("wor service restart %s", d.target))
	case code == 504:
		d.fail("http-host", prefix+"504", "proc", confMed,
			"the app behind the web server timed out (hung or too slow)",
			fmt.Sprintf("wor service restart %s", d.target))
	case code == 403:
		d.fail("http-host", prefix+"403", "perm", confMed,
			"the web server denies access to the service's files (permissions or docroot)",
			"wor doctor   # see the Security section for the exact setfacl fix")
	case code == 404:
		d.warn("http-host", prefix+"404 (served, but nothing at / -- wrong docroot or route?)")
	default:
		d.warn("http-host", fmt.Sprintf("%s%d (served, app-level error)", prefix, code))
		d.note(fmt.Sprintf("wor service logs %s   # app answers %d through the web server", d.target, code))
	}
}

// httpProbe GETs "/" as host, dialing addr directly (regardless of
// what DNS says about host) with tight timeouts and no redirect
// following -- a redirect answer is itself a valid "the server
// responded" result, and following it could leave this machine
// entirely.
func httpProbe(addr, host string, useTLS bool) (int, error) {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			dialer := net.Dialer{Timeout: diagDialTimeout}
			return dialer.DialContext(ctx, network, addr)
		},
		TLSClientConfig:       &tls.Config{ServerName: host, InsecureSkipVerify: true},
		ResponseHeaderTimeout: diagHTTPTimeout,
		DisableKeepAlives:     true,
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   diagHTTPTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	scheme := "http"
	if useTLS {
		scheme = "https"
	}
	req, err := http.NewRequest("GET", scheme+"://"+host+"/", nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	return resp.StatusCode, nil
}

// isTimeout distinguishes "connected but never answered" (hung app,
// the 504-by-hand case) from "could not connect at all". http.Client
// wraps timeouts in *url.Error, hence errors.As plus the string
// fallback for context.DeadlineExceeded flowing through client.Timeout.
func isTimeout(err error) bool {
	var nerr net.Error
	if errors.As(err, &nerr) && nerr.Timeout() {
		return true
	}
	return strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "deadline exceeded")
}

// connErrShort strips the noisy `Get "http://...": dial tcp ...:`
// wrapping down to the part an admin actually reads ("connection
// refused", "no route to host", ...).
func connErrShort(err error) string {
	msg := err.Error()
	if i := strings.LastIndex(msg, ": "); i >= 0 {
		return msg[i+2:]
	}
	return msg
}

// --- layer 7: filesystem reachability + disk ---------------------------

func (a *App) diagReachability(d *diagnosis, domain, service, svcType string) {
	if osutil.IsWindows() {
		d.skip("files", "permission traversal not checked on Windows")
		return
	}
	provider, err := a.Provider()
	if err != nil || (provider.Name != "nginx" && provider.Name != "apache") {
		d.skip("files", "reachability not checked (no nginx/apache provider)")
		return
	}
	if !osutil.IsDebianFamily() {
		d.skip("files", "reachability only auto-checked on Debian/Ubuntu")
		return
	}
	webUser := webServerRunUser(provider.Name)
	if !webUserExists(webUser) {
		d.skip("files", fmt.Sprintf("web server user %q not resolvable -- not checked", webUser))
		return
	}
	blocked := checkServiceReachability(a, webUser, domain, service, svcType)
	if len(blocked) == 0 {
		d.pass("files", fmt.Sprintf("reachable by web server user (%s)", webUser))
		return
	}
	d.addEvidence("files", blocked...)
	// Kind "perm" at high confidence: this is the precise version of
	// what an http 403 (perm, medium) or a "permission denied" log
	// line only hints at -- merging replaces their vaguer wording and
	// puts this cause on top (the agreed files+http cross-layer boost).
	d.fail("files", fmt.Sprintf("%d path(s) block traversal for %s", len(blocked), webUser), "perm", confHigh,
		fmt.Sprintf("the web server user (%s) cannot traverse to this service's files", webUser),
		worHomeReachabilityFixCommand(webUser, blocked))
}

func (a *App) diagDisk(d *diagnosis) {
	pct, ok := diskUsagePercent(a.Cfg.WorHome)
	if !ok {
		d.skip("disk", "usage not checked on this platform")
		return
	}
	switch {
	case pct >= 98:
		d.fail("disk", fmt.Sprintf("filesystem holding WOR_HOME is %d%% full", pct), "disk", confHigh,
			"the disk is (nearly) full -- logs, sockets, and databases fail silently in this state",
			"free disk space (old backups/logs are the usual candidates)")
	case pct >= 90:
		d.warn("disk", fmt.Sprintf("filesystem holding WOR_HOME is %d%% full", pct))
	default:
		d.pass("disk", fmt.Sprintf("%d%% used", pct))
	}
}

// --- layer 8: logs ------------------------------------------------------

// logPattern maps a known error fingerprint (matched case-insensitively
// against log lines) to a cause family + human-readable cause + fix.
// Deliberately a plain slice: adding a pattern is a one-line change, no
// plugin system (Simplicity First). <target>/<dir>/<host> in fix are
// substituted at match time.
type logPattern struct {
	substr string
	kind   string
	cause  string
	fix    string
}

var logPatterns = []logPattern{
	{"eaddrinuse", "port", "port already in use (EADDRINUSE)", "lsof -i :<port of this service>   # find the conflicting process, then: wor service restart <target>"},
	{"cannot find module", "deps", "Node.js dependency missing (Cannot find module)", "cd <dir> && npm install && wor service restart <target>"},
	{"module_not_found", "deps", "Node.js dependency missing (MODULE_NOT_FOUND)", "cd <dir> && npm install && wor service restart <target>"},
	{"modulenotfounderror", "deps", "Python dependency missing (ModuleNotFoundError)", "cd <dir> && pip install -r requirements.txt && wor service restart <target>"},
	{"permission denied", "perm", "permission denied on a file or socket", "check ownership/permissions under <dir>; wor doctor's Security section shows the exact fix"},
	{"out of memory", "oom", "out of memory", "check free memory (free -m); add swap or reduce the app's memory usage"},
	{"oom-kill", "oom", "killed by the OOM killer (out of memory)", "check free memory (free -m); add swap or reduce the app's memory usage"},
	{"econnrefused", "db", "the app's own upstream (often the database) refuses connections (ECONNREFUSED)", "check the database/upstream service this app connects to"},
	{"enoent", "enoent", "a file or directory the app needs is missing (ENOENT)", "check the entry point and any paths in <dir>/.env"},
	{"certificate has expired", "tls", "a TLS certificate in the chain has expired", "wor ssl status <host> / wor ssl renew <host>"},
}

// matchLogPattern reports the first known error fingerprint found in
// one log line (case-insensitive substring match). Split out from
// diagLogs so the pattern table stays unit-testable without an App.
func matchLogPattern(line string) (logPattern, bool) {
	lower := strings.ToLower(line)
	for _, p := range logPatterns {
		if strings.Contains(lower, p.substr) {
			return p, true
		}
	}
	return logPattern{}, false
}

// logSource is one place diagLogs pulls trailing lines from.
type logSource struct {
	name  string
	lines []string
}

func (a *App) diagLogs(d *diagnosis, domain string, svc *domainmodel.Service, probeHost string) {
	var sources []logSource

	switch domainmodel.ProcessProviderFor(svc.Type) {
	case "pm2":
		// pm2's default per-process log location: PM2_HOME/logs/<name>-error.log.
		path := filepath.Join(pm2.Home(), "logs", pm2.Name(domain, svc.Name)+"-error.log")
		if lines := tailFileLines(path, diagLogLines); len(lines) > 0 {
			sources = append(sources, logSource{"pm2 error log", lines})
		}
	case "systemd":
		if out, err := systemd.RecentLogs(domain, svc.Name, diagLogLines); err == nil && out != "" {
			sources = append(sources, logSource{"journalctl", strings.Split(out, "\n")})
		}
	}
	if probeHost != "" {
		if provider, err := a.Provider(); err == nil {
			path := filepath.Join(provider.LogDir(), probeHost+".error.log")
			// Freshness-filtered: the http-host probe above just
			// exercised this vhost, so a live problem is guaranteed a
			// fresh line here -- stale lines from before a config fix
			// were twice observed (real host, 2026-07-06) hijacking the
			// verdict with an error that no longer existed.
			if lines := freshLogLines(tailFileLines(path, diagLogLines), time.Now()); len(lines) > 0 {
				sources = append(sources, logSource{provider.Name + " error log", lines})
			}
		}
	}

	if len(sources) == 0 {
		d.skip("logs", "no logs readable (may need root, or nothing logged yet)")
		return
	}

	matched := 0
	seen := map[string]bool{}
	staleSeen := false
	for _, src := range sources {
		for _, line := range src.lines {
			// A quoted path with wor's /domains/ layout marker that is
			// NOT under the current WOR_HOME means some config still in
			// effect was written for a previous installation (old
			// WOR_HOME) -- name that directly instead of letting the
			// line's "permission denied" surface as a generic perm
			// cause pointing at the wrong tree (the exact misdiagnosis
			// a real host produced on 2026-07-06).
			if !staleSeen {
				if p := staleWorPath(line, a.Cfg.WorHome); p != "" {
					staleSeen = true
					matched++
					oldRoot := p
					if i := strings.Index(p, "/domains/"); i > 0 {
						oldRoot = p[:i]
					}
					d.addCause("config",
						fmt.Sprintf("the web server references %s -- outside the current WOR_HOME (%s); a config from a previous installation is still active", oldRoot, a.Cfg.WorHome),
						confHigh,
						fmt.Sprintf("sudo grep -rln %q /etc/nginx /etc/apache2 2>/dev/null   # find the stale config; wor setup / wor host add regenerate wor-managed ones", oldRoot))
					d.addEvidence(src.name, line)
				}
			}
			p, ok := matchLogPattern(line)
			if !ok || seen[p.substr] {
				continue
			}
			seen[p.substr] = true
			matched++
			fix := strings.ReplaceAll(p.fix, "<target>", d.target)
			fix = strings.ReplaceAll(fix, "<dir>", d.dir)
			fix = strings.ReplaceAll(fix, "<host>", probeHost)
			d.mergeLogPattern(p, fix)
			d.addEvidence(src.name, line)
		}
	}

	names := make([]string, 0, len(sources))
	for _, s := range sources {
		names = append(names, s.name)
	}
	if matched > 0 {
		d.warn("logs", fmt.Sprintf("%d known error pattern(s) in %s -- see evidence below", matched, strings.Join(names, ", ")))
		return
	}
	d.pass("logs", fmt.Sprintf("scanned %s -- no known error patterns", strings.Join(names, ", ")))

	// A failed diagnosis with zero evidence still deserves raw log
	// tail: show the last few lines of the process-level source so the
	// admin doesn't have to go hunting.
	if d.failed && len(d.evidence) == 0 {
		src := sources[0]
		start := len(src.lines) - diagEvidencePerSource
		if start < 0 {
			start = 0
		}
		for _, line := range src.lines[start:] {
			d.addEvidence(src.name, line)
		}
	}
}

// mergeLogPattern folds one matched log fingerprint into the causes
// collected by the layers above, instead of dumping it on the admin as
// yet another finding to correlate:
//
//  1. A cause of the same kind already exists -> the log line is
//     independent corroboration: raise that cause to high confidence
//     (merging via addCause) and attach the pattern's fix.
//  2. No same-kind cause, but the process layer failed -> the pattern
//     is almost certainly WHY the process is failing (crash loop from
//     a missing module, EADDRINUSE, a dead database...): enrich the
//     process cause's wording with it, raise it to high, add the fix.
//  3. Nothing to attach to -> a standalone low-confidence cause; a log
//     line alone, with every layer passing, is a weak signal.
func (d *diagnosis) mergeLogPattern(p logPattern, fix string) {
	if c := d.causeByKind(p.kind); c != nil {
		d.addCauseDetail(p.kind, p.cause, confHigh, "log evidence: "+p.cause, fix)
		return
	}
	if c := d.causeByKind("proc"); c != nil {
		if !strings.Contains(c.text, p.cause) {
			c.text += " -- " + p.cause
		}
		if c.conf < confHigh {
			c.conf = confHigh
		}
		c.addFixes(fix)
		return
	}
	d.addCause(p.kind, p.cause, confLow, fix)
}

// nginx error-log lines lead with "2006/01/02 15:04:05"; apache's with
// "[Mon Jan 02 15:04:05.000000 2006]".
var (
	nginxLogTSRe  = regexp.MustCompile(`^(\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2})`)
	apacheLogTSRe = regexp.MustCompile(`^\[\w{3} (\w{3} \d{2} \d{2}:\d{2}:\d{2})\.\d+ (\d{4})\]`)
	// quotedAbsPathRe pulls quoted absolute paths out of web-server
	// error-log lines (nginx quotes every filesystem path it reports).
	quotedAbsPathRe = regexp.MustCompile(`"(/[^"]+)"`)
)

// freshLogLines drops web-server error-log lines older than
// diagLogFreshness. Lines without a parseable leading timestamp are
// KEPT (fail open) so an unknown log format is never silently hidden.
// Timestamps are parsed in local time, matching how nginx/apache write
// them.
func freshLogLines(lines []string, now time.Time) []string {
	var out []string
	for _, line := range lines {
		if ts, ok := webLogTime(line, now.Location()); ok && now.Sub(ts) > diagLogFreshness {
			continue
		}
		out = append(out, line)
	}
	return out
}

func webLogTime(line string, loc *time.Location) (time.Time, bool) {
	if m := nginxLogTSRe.FindStringSubmatch(line); m != nil {
		if ts, err := time.ParseInLocation("2006/01/02 15:04:05", m[1], loc); err == nil {
			return ts, true
		}
	}
	if m := apacheLogTSRe.FindStringSubmatch(line); m != nil {
		if ts, err := time.ParseInLocation("Jan 02 15:04:05 2006", m[1]+" "+m[2], loc); err == nil {
			return ts, true
		}
	}
	return time.Time{}, false
}

// staleWorPath returns the first quoted path in line that contains
// wor's "/domains/" layout marker but is NOT under worHome -- the
// fingerprint of a config left behind by a previous installation with
// a different WOR_HOME. "" when the line has no such path. The
// /domains/ requirement keeps legitimately-external paths (sockets
// under /run, certs under /etc/letsencrypt) from false-positiving.
func staleWorPath(line, worHome string) string {
	for _, m := range quotedAbsPathRe.FindAllStringSubmatch(line, -1) {
		p := m[1]
		if !strings.Contains(p, "/domains/") {
			continue
		}
		if p == worHome || strings.HasPrefix(p, worHome+"/") {
			continue
		}
		return p
	}
	return ""
}

// tailFileLines returns the last n lines of path, or nil if the file
// is missing/unreadable -- reusing lastNLines (logs.go), the same
// helper `wor host logs`'s tailer is built on.
func tailFileLines(path string, n int) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	lines, err := lastNLines(f, n)
	if err != nil {
		return nil
	}
	return lines
}

// diagDeployCorrelation adds a rollback hint when the diagnosis failed
// AND the service's source changed very recently -- "it broke right
// after a deploy" is common enough (and `wor rollback` direct enough a
// recovery) to deserve an explicit callout.
func (a *App) diagDeployCorrelation(d *diagnosis) {
	if !d.failed {
		return
	}
	out, err := gitOutput(d.dir, "log", "-1", "--format=%ct")
	if err != nil {
		return
	}
	ts, err := strconv.ParseInt(out, 10, 64)
	if err != nil {
		return
	}
	age := time.Since(time.Unix(ts, 0))
	if age > 0 && age < time.Hour {
		d.note(fmt.Sprintf("wor rollback %s   # last source change was only %s ago -- if the failure started then, roll back first, debug later", d.target, formatUptime(age)))
	}
}

// --- verdict -------------------------------------------------------------

// renderEvidence groups raw evidence lines by source, deduplicates
// near-identical repeats (same message, different timestamp -- nginx
// error logs repeat the same [crit] line once per request) into a
// single "(xN)" line, caps each source at diagEvidencePerSource lines,
// and truncates to diagEvidenceWidth -- real log lines run several
// hundred characters and would drown the verdict otherwise.
func renderEvidence(items []evidenceItem) []string {
	type entry struct {
		text  string
		count int
	}
	var order []string
	grouped := map[string][]*entry{}
	for _, it := range items {
		entries, seenSource := grouped[it.source]
		if !seenSource {
			order = append(order, it.source)
		}
		core := evidenceCore(it.line)
		merged := false
		for _, e := range entries {
			if evidenceCore(e.text) == core {
				e.count++ // entries hold pointers, so this mutates in place
				merged = true
				break
			}
		}
		if !merged {
			grouped[it.source] = append(entries, &entry{text: it.line, count: 1})
		}
	}

	var out []string
	for _, source := range order {
		out = append(out, source+":")
		for i, e := range grouped[source] {
			if i >= diagEvidencePerSource {
				out = append(out, fmt.Sprintf("  ... (%d more)", len(grouped[source])-diagEvidencePerSource))
				break
			}
			line := truncateLine(e.text, diagEvidenceWidth)
			if e.count > 1 {
				line += fmt.Sprintf(" (x%d)", e.count)
			}
			out = append(out, "  "+line)
		}
	}
	return out
}

// evidenceCore normalizes one log line for duplicate detection.
// Repeats of the same message differ in more than the timestamp:
// nginx error lines carry a per-request connection id and pid
// ("*107", "7255#7255") that change every occurrence. So comparison
// starts at the first "[" (log level marker in nginx/journal/pm2
// formats) when one appears near the head of the line, then strips
// ALL digits -- what remains is the message's fixed wording, which is
// what "the same error, repeated" actually means. (Two genuinely
// different messages that differ only in numbers would merge too;
// acceptable for an evidence display.) Only the first 80 chars count.
func evidenceCore(line string) string {
	if i := strings.Index(line, "["); i >= 0 && i <= 40 {
		line = line[i:]
	}
	var b strings.Builder
	for _, r := range line {
		if r >= '0' && r <= '9' {
			continue
		}
		b.WriteRune(r)
	}
	core := b.String()
	if len(core) > 80 {
		core = core[:80]
	}
	return core
}

func truncateLine(s string, width int) string {
	if len(s) <= width {
		return s
	}
	return s[:width-3] + "..."
}

// diagSection prints one section header with the fixed-width rule the
// agreed output layout uses.
func diagSection(w io.Writer, title string) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, title)
	fmt.Fprintln(w, strings.Repeat("-", 48))
}

// printVerdict renders the closing synthesis in the agreed layout:
// Summary (status + counts + ONE root cause + its proven Cascade),
// Evidence, then a numbered Suggested-fix runbook. The contract
// (owner's core requirement): the admin must not have to re-derive
// the conclusion from the [FAIL] rows -- the Cascade list explicitly
// marks which other failures are the same problem (proven by the kind
// merge, never a guessed causal link), and remaining independent
// causes become "If it persists" runbook steps rather than a puzzle.
func (d *diagnosis) printVerdict() {
	out := d.a.Out

	diagSection(out, "Summary")
	if !d.failed {
		fmt.Fprintf(out, "Status: OK (0 fail, %d warn)\n", d.warnCount)
		fmt.Fprintln(out)
		fmt.Fprintln(out, "No problems found on this machine.")
		fmt.Fprintln(out, "If the site is still down from outside, look beyond this host:")
		fmt.Fprintln(out, "  real DNS records, firewall rules, and any CDN/proxy in front.")
		d.printNotes()
		return
	}

	ranked := d.rankedCauses()
	root := ranked[0]

	fmt.Fprintf(out, "Status: FAILED (%d fail, %d warn)\n", d.failCount, d.warnCount)
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Root cause:")
	fmt.Fprintf(out, "  %s\n", root.text)
	if cascade := root.cascade(); len(cascade) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Cascade (same problem, seen from other layers):")
		for _, c := range cascade {
			fmt.Fprintf(out, "  - %s\n", c)
		}
	}

	if lines := renderEvidence(d.evidence); len(lines) > 0 {
		diagSection(out, "Evidence")
		for _, l := range lines {
			fmt.Fprintf(out, "  %s\n", l)
		}
	}

	diagSection(out, "Suggested fix (run yourself -- wor diagnose never changes anything)")
	step := 1
	step = printFixGroup(out, step, "Fix the root cause -- "+root.text+":", root.fixes)
	for i, c := range ranked[1:] {
		if i >= diagOtherPossibilities {
			break
		}
		step = printFixGroup(out, step, "If it persists -- "+c.text+":", c.fixes)
	}
	printFixGroup(out, step, "Verify:", []string{"wor diagnose " + d.target})
	d.printNotes()
}

// printFixGroup renders one numbered runbook step: a one-line
// description of WHAT is being fixed, then its commands indented under
// it. Steps with no commands are skipped; returns the next step number.
func printFixGroup(w io.Writer, step int, title string, fixes []string) int {
	if len(fixes) == 0 {
		return step
	}
	fmt.Fprintf(w, "%d. %s\n", step, title)
	for _, f := range fixes {
		fmt.Fprintf(w, "     %s\n", f)
	}
	return step + 1
}

func (d *diagnosis) printNotes() {
	if len(d.notes) == 0 {
		return
	}
	out := d.a.Out
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Also worth checking:")
	for _, n := range d.notes {
		fmt.Fprintf(out, "  %s\n", n)
	}
}

// --- wor health: quick sweep across every enabled service ----------------

// cmdHealth implements `wor health`: the "the server is down and I
// don't yet know which service" entry point, e.g. right after a
// reboot. Split out of `wor diagnose --all` (owner decision
// 2026-07-06): "diagnose" analyzes a patient you already know is sick,
// while this command's job is finding the sick ones -- a health check,
// so it's named one. The three commands tell one story: wor health
// (who's broken) -> wor diagnose <target> (why + fix) -> wor run
// (bring it back).
//
// Deliberately shallow per service -- process state, a port dial, and
// one real HTTP request through the web server -- with a pointer to
// the full single-target diagnosis for anything that fails. Not to be
// confused with `wor doctor`, which checks whether the MACHINE is set
// up right (runtimes installed, workspace initialized); health checks
// whether the SERVICES are actually serving. Same non-interactive/
// read-only/exit-code contract as diagnose, so it can drive cron.
func (a *App) cmdHealth(args []string) (bool, error) {
	refs, err := a.Store.ListAllServices()
	if err != nil {
		return false, err
	}
	var enabled []domainmodel.ServiceRef
	needPM2 := false
	for _, ref := range refs {
		if !ref.Service.Enabled {
			continue
		}
		enabled = append(enabled, ref)
		if domainmodel.ProcessProviderFor(ref.Service.Type) == "pm2" {
			needPM2 = true
		}
	}
	if len(enabled) == 0 {
		fmt.Fprintln(a.Out, "No enabled services found.")
		return false, nil
	}

	// One pm2 jlist for the whole sweep (pm2.List's own design point),
	// not one shell-out per service.
	var pm2Procs map[string]pm2.ProcessInfo
	if needPM2 && osutil.Exists("pm2") {
		pm2Procs, _ = pm2.List()
	}

	// Live usage is collected up front (one shared ~200ms sampling
	// window, see collectResources) because the card layout below
	// (owner mockup, approved 2026-07-07) inlines cpu/mem into each
	// service's card. Resource numbers are informational only: no
	// colors, no effect on the exit code.
	host, usage := a.collectResources(enabled, pm2Procs)

	const rule = "--------------------------------------------"
	fmt.Fprintf(a.Out, "WOR Health (%d enabled services)\n", len(enabled))
	fmt.Fprintln(a.Out, rule)
	if host.cpuKnown {
		fmt.Fprintf(a.Out, "Host CPU    : %.0f%% (%d cores)\n", host.cpuPct, host.cores)
	}
	if host.memKnown {
		fmt.Fprintf(a.Out, "Host Memory : %s / %s (%.0f%%)\n",
			formatMemBytes(host.memUsed), formatMemBytes(host.memTotal),
			float64(host.memUsed)/float64(host.memTotal)*100)
	}
	if diskUsed, diskTotal, ok := diskUsageBytes(a.Cfg.WorHome); ok {
		fmt.Fprintf(a.Out, "Disk Usage  : %s / %s (%.0f%%)\n",
			formatMemBytes(diskUsed), formatMemBytes(diskTotal),
			float64(diskUsed)/float64(diskTotal)*100)
	}
	fmt.Fprintln(a.Out, rule)

	useColor := a.colorEnabled()
	healthy, warned := 0, 0
	var failedTargets []string
	for _, ref := range enabled {
		target := ref.Domain + "/" + ref.Service.Name
		svcOK, proc, httpOK, httpCode, httpURL, httpNote := a.quickServiceCheck(ref, pm2Procs)

		// Three levels (owner decision 2026-07-07): FAIL as before,
		// WARN for a 404 answer or no registered host -- visible in
		// the dot color and the footer count, but the exit code stays
		// 0 so cron/monitoring only alerts on real failures.
		probeRan := httpURL != "" || httpNote != ""
		level := 0
		switch {
		case !svcOK:
			level = 2
			failedTargets = append(failedTargets, target)
		case httpCode == "404" || (probeRan && httpURL == ""):
			level = 1
			warned++
		default:
			healthy++
		}
		dot := tag(useColor, ansiGreen, "●", "[ok]")
		switch level {
		case 1:
			dot = tag(useColor, ansiYellow, "●", "[warn]")
		case 2:
			dot = tag(useColor, ansiRed, "●", "[FAIL]")
		}

		fmt.Fprintln(a.Out)
		fmt.Fprintf(a.Out, "%s %s\n", dot, target)

		// Status: the process layer's verdict. A service that failed
		// only at the HTTP layer still shows Online here -- its ✗ http
		// line below carries the failure.
		status := "Online"
		if !domainmodel.TemplateRequiresProcessSupervisor(ref.Service.Type) && !domainmodel.TemplateRequiresPHP(ref.Service.Type) {
			status = "Online (served by web server)"
		}
		if !svcOK && !probeRan {
			status = "FAILED -- " + proc
		}
		fmt.Fprintf(a.Out, "    Status : %s\n", status)
		fmt.Fprintf(a.Out, "    Runtime: %s\n", a.healthRuntimeLabel(ref))
		// CPU/Memory/Uptime lines are simply absent when unknown
		// (static services, or platforms without the reader) -- owner
		// chose hidden lines over "-" placeholders.
		if u, ok := usage[target]; ok {
			if u.cpuKnown {
				fmt.Fprintf(a.Out, "    CPU    : %.1f%%\n", u.cpuPct)
			}
			if u.memKnown {
				memLine := formatMemBytes(u.memBytes)
				if host.memKnown {
					memLine += fmt.Sprintf(" (%.1f%%)", float64(u.memBytes)/float64(host.memTotal)*100)
				}
				fmt.Fprintf(a.Out, "    Memory : %s\n", memLine)
			}
		}
		if info, ok := pm2Procs[pm2.Name(ref.Domain, ref.Service.Name)]; ok && info.Status == "online" {
			if up := formatUptime(info.Uptime); up != "" {
				fmt.Fprintf(a.Out, "    Uptime : %s\n", up)
			}
		}

		switch {
		case !probeRan:
			// process layer failed; there is no http verdict to show
		case httpURL == "":
			fmt.Fprintf(a.Out, "    %s %s\n", tag(useColor, ansiDim, "ℹ", "[i]"), httpNote)
		default:
			mark := tag(useColor, ansiGreen, "✓", "[ok]")
			switch {
			case level == 1:
				mark = tag(useColor, ansiYellow, "⚠", "[warn]")
			case !httpOK:
				mark = tag(useColor, ansiRed, "✗", "[FAIL]")
			}
			verdict := httpCode
			switch {
			case httpNote != "" && httpCode == "---":
				verdict = httpNote
			case httpNote != "":
				verdict = httpCode + " (" + httpNote + ")"
			}
			fmt.Fprintf(a.Out, "    %s %s -> %s\n", mark, httpURL, verdict)
		}
	}

	fmt.Fprintln(a.Out)
	fmt.Fprintln(a.Out, rule)
	fmt.Fprintf(a.Out, "Healthy : %d\n", healthy)
	fmt.Fprintf(a.Out, "Warning : %d\n", warned)
	fmt.Fprintf(a.Out, "Failed  : %d\n", len(failedTargets))
	for _, t := range failedTargets {
		fmt.Fprintf(a.Out, "    wor diagnose %s   # root cause + fix\n", t)
	}
	if len(failedTargets) > 0 {
		fmt.Fprintln(a.Out, "To bring everything enabled back up: wor run")
		return true, nil
	}
	return false, nil
}

// healthRuntimeLabel names what actually runs one service, in the card
// layout's "Runtime:" wording: the process provider plus the template,
// or the php-fpm pool detail (with a live worker count on Linux --
// free, since PoolWorkerPIDs is the same /proc scan the resource
// numbers already use).
func (a *App) healthRuntimeLabel(ref domainmodel.ServiceRef) string {
	svc := ref.Service
	switch domainmodel.ProcessProviderFor(svc.Type) {
	case "pm2":
		return fmt.Sprintf("pm2 (%s)", svc.Type)
	case "systemd":
		return fmt.Sprintf("systemd (%s)", svc.Type)
	}
	if domainmodel.TemplateRequiresPHP(svc.Type) {
		if svc.UsesPerServicePHPFPM() {
			if n := len(phpfpm.PoolWorkerPIDs(ref.Domain, svc.Name)); n > 0 {
				return fmt.Sprintf("php-fpm %s (dedicated pool, %d workers)", svc.PHPVersion, n)
			}
			return fmt.Sprintf("php-fpm %s (dedicated pool)", svc.PHPVersion)
		}
		return "php-fpm (host-wide endpoint)"
	}
	return "static"
}

// quickServiceCheck is cmdHealth's shallow per-service health call:
// the process/pool layer first, then (when that passes) an actual HTTP
// request through the web server -- the end-to-end signal. The http
// probe was added after a real outage where two php services showed
// "pool accepting" here while nginx answered 403/502 for both: process
// health alone systematically misses permission/vhost/proxy problems,
// which never kill a process. It also gives static services (no
// process to check at all) their first real coverage.
//
// Returns the overall verdict, the process-layer summary (rendered
// into the card's Status line on failure), and the http probe's parts
// for its own ✓/⚠/✗ line: httpOK is the probe's own verdict, httpURL
// the probed URL ("" with a non-empty httpNote = nothing to probe,
// e.g. no host registered), and httpNote an optional annotation
// ("may be normal for APIs", "no response (timeout)"). All three
// empty means the probe never ran because the process layer already
// failed.
func (a *App) quickServiceCheck(ref domainmodel.ServiceRef, pm2Procs map[string]pm2.ProcessInfo) (ok bool, proc string, httpOK bool, httpCode, httpURL, httpNote string) {
	svc := ref.Service
	portOK := func() (bool, string) {
		if svc.Port == 0 {
			return true, ""
		}
		if tcpListening(svc.Port) {
			return true, fmt.Sprintf(", :%d accepting", svc.Port)
		}
		return false, fmt.Sprintf(", but :%d NOT accepting connections", svc.Port)
	}
	// withHTTP pairs a passed process-layer summary with the http
	// probe's verdict.
	withHTTP := func(base string) (bool, string, bool, string, string, string) {
		hok, code, url, note := a.quickHTTPCheck(ref.Domain, svc.Name)
		return hok, base, hok, code, url, note
	}

	switch domainmodel.ProcessProviderFor(svc.Type) {
	case "pm2":
		if pm2Procs == nil {
			return false, "pm2 unavailable", false, "", "", ""
		}
		info, found := pm2Procs[pm2.Name(ref.Domain, svc.Name)]
		if !found {
			return false, "not registered with pm2 (lost after reboot?)", false, "", "", ""
		}
		if info.Status != "online" {
			return false, "pm2 status: " + info.Status, false, "", "", ""
		}
		if portUp, extra := portOK(); !portUp {
			return false, "pm2 online" + extra, false, "", "", ""
		}
		return withHTTP("pm2 online")
	case "systemd":
		st, err := systemd.ShowDiagState(ref.Domain, svc.Name)
		if err != nil {
			return false, "cannot query systemd", false, "", "", ""
		}
		if st.ActiveState != "active" {
			return false, fmt.Sprintf("systemd %s (result: %s)", st.ActiveState, st.Result), false, "", "", ""
		}
		if portUp, extra := portOK(); !portUp {
			return false, "systemd active" + extra, false, "", "", ""
		}
		return withHTTP("systemd active")
	default:
		if !domainmodel.TemplateRequiresPHP(svc.Type) {
			return withHTTP("static")
		}
		if !svc.UsesPerServicePHPFPM() {
			if ep, found := hostprovider.PHPFPMEndpoint(a.Cfg); found && endpointAccepting(ep) {
				return withHTTP("legacy php-fpm endpoint accepting")
			}
			return false, "legacy php-fpm endpoint not accepting connections", false, "", "", ""
		}
		v, found := phpfpm.ResolveVersion(svc.PHPVersion)
		if !found {
			return false, fmt.Sprintf("PHP %s no longer installed", svc.PHPVersion), false, "", "", ""
		}
		if !phpfpm.PoolAlive(v, ref.Domain, svc.Name) {
			return false, fmt.Sprintf("php-fpm %s pool not accepting connections", svc.PHPVersion), false, "", "", ""
		}
		return withHTTP(fmt.Sprintf("php-fpm %s pool accepting", svc.PHPVersion))
	}
}

// quickHTTPCheck sends one GET through the web server for the
// service's first registered host (dialing 127.0.0.1 with the Host
// header, exactly like the full diagnosis's http-host probe), and
// returns the verdict plus badge content + detail for the sub-line
// ("[ 502 ] https://cdn.example.com" style). code "---" marks probes
// that got no HTTP status at all (refused/timeout); code "" means
// there was nothing to probe, and detail says why. 404 is deliberately
// ok-with-note, not FAIL: plenty of API services have nothing at "/"
// and a false FAIL here would teach admins to distrust the sweep --
// the note keeps it visible for the cases where 404 IS the problem.
func (a *App) quickHTTPCheck(domain, service string) (ok bool, code, url, note string) {
	hosts, _ := a.Store.ListHostsForService(domain, service)
	if len(hosts) == 0 {
		return true, "", "", "no host registered -- HTTP not probed"
	}
	host := hosts[0]
	useTLS := false
	webPort := "80"
	if st, found, _ := ssl.LoadState(a.Cfg.SSL, host); found && st.Enabled {
		useTLS = true
		webPort = "443"
	}
	label := "http://" + host
	if useTLS {
		label = "https://" + host
	}
	status, err := httpProbe(net.JoinHostPort("127.0.0.1", webPort), host, useTLS)
	switch {
	case err != nil && isTimeout(err):
		return false, "---", label, "no response (timeout)"
	case err != nil:
		return false, "---", label, connErrShort(err)
	case status == 404:
		return true, "404", label, "may be normal for APIs"
	case status >= 200 && status < 400:
		return true, strconv.Itoa(status), label, ""
	default:
		return false, strconv.Itoa(status), label, ""
	}
}
