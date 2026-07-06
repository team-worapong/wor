package cliapp

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"wor/internal/domainmodel"
	"wor/internal/systemd"
)

func TestMatchLogPattern(t *testing.T) {
	cases := []struct {
		line      string
		wantMatch bool
		wantCause string // substring the matched cause must contain
	}{
		{"Error: listen EADDRINUSE: address already in use :::3100", true, "EADDRINUSE"},
		{"Error: Cannot find module 'express'", true, "dependency missing"},
		{"code: 'MODULE_NOT_FOUND'", true, "MODULE_NOT_FOUND"},
		{"ModuleNotFoundError: No module named 'flask'", true, "Python dependency"},
		{"EACCES: permission denied, open '/opt/wor/x'", true, "permission denied"},
		{"FATAL ERROR: JavaScript heap out of memory", true, "out of memory"},
		// NOTE: a line containing BOTH "out of memory" and "oom-kill"
		// matches the former -- first pattern in the table wins (see
		// TestMatchLogPatternFirstWins). This line has only oom-kill.
		{"systemd[1]: wor_shop_api.service: Failed with result 'oom-kill'.", true, "OOM"},
		{"Error: connect ECONNREFUSED 127.0.0.1:3306", true, "ECONNREFUSED"},
		{"ENOENT: no such file or directory, open '.env'", true, "ENOENT"},
		{"SSL routines: certificate has expired", true, "expired"},
		{"server started on port 3100", false, ""},
		{"", false, ""},
	}
	for _, c := range cases {
		p, ok := matchLogPattern(c.line)
		if ok != c.wantMatch {
			t.Errorf("matchLogPattern(%q) match = %v, want %v", c.line, ok, c.wantMatch)
			continue
		}
		if ok && !strings.Contains(p.cause, c.wantCause) {
			t.Errorf("matchLogPattern(%q) cause = %q, want it to contain %q", c.line, p.cause, c.wantCause)
		}
	}
}

// TestMatchLogPatternOOMKillBeforeGenericOOM guards the table order
// assumption: a line containing both fingerprints still yields a
// single, sensible match.
func TestMatchLogPatternFirstWins(t *testing.T) {
	p, ok := matchLogPattern("out of memory: oom-kill invoked")
	if !ok {
		t.Fatal("expected a match")
	}
	if p.substr != "out of memory" {
		t.Errorf("first matching pattern should win, got %q", p.substr)
	}
}

func TestDescribeSystemdResult(t *testing.T) {
	cases := []struct {
		result    string
		exit      int
		wantKind  string
		wantConf  diagConfidence
		wantCause string
	}{
		{"oom-kill", 0, "oom", confHigh, "OOM killer"},
		{"exit-code", 3, "proc", confHigh, "status 3"},
		{"start-limit-hit", 0, "proc", confHigh, "crash loop"},
		{"signal", 0, "proc", confHigh, "signal"},
		{"core-dump", 0, "proc", confHigh, "signal"},
		{"something-new", 0, "proc", confMed, "something-new"},
	}
	for _, c := range cases {
		kind, conf, cause, fix := describeSystemdResult(systemd.DiagState{Result: c.result, ExecMainStatus: c.exit}, "shop/api")
		if kind != c.wantKind || conf != c.wantConf {
			t.Errorf("describeSystemdResult(%q) = (%s, %v), want (%s, %v)", c.result, kind, conf, c.wantKind, c.wantConf)
		}
		if !strings.Contains(cause, c.wantCause) {
			t.Errorf("describeSystemdResult(%q) cause = %q, want it to contain %q", c.result, cause, c.wantCause)
		}
		if fix == "" {
			t.Errorf("describeSystemdResult(%q) returned an empty fix", c.result)
		}
	}
}

func TestConnErrShort(t *testing.T) {
	err := fmt.Errorf("Get \"http://127.0.0.1:3100/\": dial tcp 127.0.0.1:3100: connect: connection refused")
	if got := connErrShort(err); got != "connection refused" {
		t.Errorf("connErrShort = %q, want %q", got, "connection refused")
	}
	plain := errors.New("boom")
	if got := connErrShort(plain); got != "boom" {
		t.Errorf("connErrShort(plain) = %q, want %q", got, "boom")
	}
}

func TestCertNotAfter(t *testing.T) {
	dir := t.TempDir()
	notAfter := time.Now().Add(30 * 24 * time.Hour).Truncate(time.Second)

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "shop.test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPath := filepath.Join(dir, "fullchain.pem")
	f, err := os.Create(certPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := pem.Encode(f, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		t.Fatal(err)
	}
	f.Close()

	got, err := certNotAfter(certPath)
	if err != nil {
		t.Fatalf("certNotAfter: %v", err)
	}
	// x509 stores UTC with second precision; compare in UTC.
	if !got.Equal(notAfter) {
		t.Errorf("certNotAfter = %v, want %v", got, notAfter)
	}
}

func TestCertNotAfterRejectsNonCertificate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "not-a-cert.pem")
	if err := os.WriteFile(path, []byte("hello, not pem at all"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := certNotAfter(path); err == nil {
		t.Error("expected an error for a non-PEM file")
	}
}

func TestHTTPProbe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	addr := strings.TrimPrefix(srv.URL, "http://")

	code, err := httpProbe(addr, "127.0.0.1", false)
	if err != nil {
		t.Fatalf("httpProbe: %v", err)
	}
	if code != http.StatusOK {
		t.Errorf("httpProbe code = %d, want 200", code)
	}
}

// httpProbe must dial the given addr even when the Host header names
// something that does not resolve there -- that's the whole point of
// probing "via the web server" without trusting DNS.
func TestHTTPProbeIgnoresDNSForHostHeader(t *testing.T) {
	var seenHost string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenHost = r.Host
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	addr := strings.TrimPrefix(srv.URL, "http://")

	code, err := httpProbe(addr, "definitely-not-real.wor-test.invalid", false)
	if err != nil {
		t.Fatalf("httpProbe: %v", err)
	}
	if code != http.StatusOK {
		t.Errorf("code = %d, want 200", code)
	}
	if seenHost != "definitely-not-real.wor-test.invalid" {
		t.Errorf("server saw Host %q, want the probe host", seenHost)
	}
}

// A redirect answer must be reported as-is (the server responded),
// never followed.
func TestHTTPProbeDoesNotFollowRedirects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://elsewhere.invalid/", http.StatusMovedPermanently)
	}))
	defer srv.Close()
	addr := strings.TrimPrefix(srv.URL, "http://")

	code, err := httpProbe(addr, "127.0.0.1", false)
	if err != nil {
		t.Fatalf("httpProbe: %v", err)
	}
	if code != http.StatusMovedPermanently {
		t.Errorf("code = %d, want 301 (unfollowed redirect)", code)
	}
}

func TestHTTPProbeConnectionRefused(t *testing.T) {
	// Reserve a port, then close the listener so nothing accepts on it.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()

	if _, err := httpProbe(addr, "127.0.0.1", false); err == nil {
		t.Error("expected a connection error against a closed port")
	}
}

func TestTCPListening(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	port := l.Addr().(*net.TCPAddr).Port

	if !tcpListening(port) {
		t.Errorf("tcpListening(%d) = false with a live listener", port)
	}
	l.Close()
	if tcpListening(port) {
		t.Errorf("tcpListening(%d) = true after the listener closed", port)
	}
}

func TestEndpointAccepting(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	if !endpointAccepting(l.Addr().String()) {
		t.Error("endpointAccepting(tcp) = false with a live listener")
	}
	if endpointAccepting("127.0.0.1:1") {
		t.Error("endpointAccepting = true against a (presumably) closed port 1")
	}
}

func newTestDiagnosis() (*diagnosis, *strings.Builder) {
	var buf strings.Builder
	return &diagnosis{a: &App{Out: &buf}, target: "shop/api", dir: "/opt/wor/domains/shop/api"}, &buf
}

func TestAddCauseMergesSameKind(t *testing.T) {
	d, _ := newTestDiagnosis()

	// A vague medium-confidence 403 cause first...
	d.addCause("perm", "the web server denies access (403)", confMed, "wor doctor")
	// ...then the files check pins it down at high confidence: same
	// kind, so it must merge -- higher confidence wins the wording.
	d.addCause("perm", "www-data cannot traverse /home/wor", confHigh, "setfacl -m u:www-data:--x /home/wor")

	if len(d.causes) != 1 {
		t.Fatalf("causes = %d, want 1 (same kind must merge)", len(d.causes))
	}
	c := d.causes[0]
	if c.conf != confHigh {
		t.Errorf("conf = %v, want high after corroboration", c.conf)
	}
	if !strings.Contains(c.text, "traverse") {
		t.Errorf("text = %q, want the higher-confidence wording", c.text)
	}
	if len(c.fixes) != 2 {
		t.Errorf("fixes = %v, want both fixes kept", c.fixes)
	}

	// Lower confidence arriving later must NOT downgrade text or conf.
	d.addCause("perm", "vague again", confLow, "setfacl -m u:www-data:--x /home/wor")
	if c.conf != confHigh || strings.Contains(c.text, "vague") || len(c.fixes) != 2 {
		t.Errorf("low-confidence merge corrupted the cause: %+v", c)
	}
}

func TestRankedCausesConfidenceThenLayerOrder(t *testing.T) {
	d, _ := newTestDiagnosis()
	d.addCause("deploy", "docroot missing", confMed)   // layer 1
	d.addCause("proc", "app not reachable", confMed)   // layer 4
	d.addCause("perm", "files blocked", confHigh)      // layer 7, but high
	d.addCause("enoent", "log says ENOENT", confLow)   // layer 8

	ranked := d.rankedCauses()
	got := []string{ranked[0].kind, ranked[1].kind, ranked[2].kind, ranked[3].kind}
	want := []string{"perm", "deploy", "proc", "enoent"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ranking = %v, want %v", got, want)
		}
	}
}

func TestMergeLogPatternCorroboratesSameKind(t *testing.T) {
	d, _ := newTestDiagnosis()
	d.addCause("perm", "www-data cannot traverse /home/wor", confMed, "setfacl ...")

	p, _ := matchLogPattern("stat() failed (13: Permission denied)")
	d.mergeLogPattern(p, "check ownership")

	if len(d.causes) != 1 {
		t.Fatalf("causes = %d, want 1 (log line corroborates, not duplicates)", len(d.causes))
	}
	if d.causes[0].conf != confHigh {
		t.Errorf("conf = %v, want high after log corroboration", d.causes[0].conf)
	}
}

func TestMergeLogPatternEnrichesProcessCause(t *testing.T) {
	d, _ := newTestDiagnosis()
	d.addCause("proc", "app crashes on start (pm2 gave up restarting it)", confHigh, "wor service logs shop/api")

	p, _ := matchLogPattern("Error: Cannot find module 'express'")
	d.mergeLogPattern(p, "cd /dir && npm install")

	if len(d.causes) != 1 {
		t.Fatalf("causes = %d, want 1 (deps pattern folds into the proc cause)", len(d.causes))
	}
	c := d.causes[0]
	if !strings.Contains(c.text, "Cannot find module") {
		t.Errorf("text = %q, want it enriched with the log-derived reason", c.text)
	}
	hasNpm := false
	for _, f := range c.fixes {
		if strings.Contains(f, "npm install") {
			hasNpm = true
		}
	}
	if !hasNpm {
		t.Errorf("fixes = %v, want the pattern's fix attached", c.fixes)
	}
}

func TestMergeLogPatternStandaloneIsLowConfidence(t *testing.T) {
	d, _ := newTestDiagnosis()
	p, _ := matchLogPattern("connect ECONNREFUSED 127.0.0.1:3306")
	d.mergeLogPattern(p, "check the database")

	if len(d.causes) != 1 {
		t.Fatalf("causes = %d, want 1", len(d.causes))
	}
	if d.causes[0].conf != confLow {
		t.Errorf("conf = %v, want low for an uncorroborated log pattern", d.causes[0].conf)
	}
}

func TestRenderEvidenceGroupsDedupsAndTruncates(t *testing.T) {
	long := strings.Repeat("x", diagEvidenceWidth+40)
	items := []evidenceItem{
		{"nginx error log", `2026/07/06 18:43:44 [crit] 7255#7255: *107 stat() failed (13: Permission denied)`},
		{"nginx error log", `2026/07/06 18:44:01 [crit] 7255#7255: *108 stat() failed (13: Permission denied)`},
		{"nginx error log", `2026/07/06 18:45:22 [crit] 7255#7255: *109 stat() failed (13: Permission denied)`},
		{"files", "/home/wor"},
		{"files", long},
	}
	lines := renderEvidence(items)

	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "nginx error log:") || !strings.Contains(joined, "files:") {
		t.Fatalf("missing source group headers:\n%s", joined)
	}
	if !strings.Contains(joined, "(x3)") {
		t.Errorf("three same-message lines should collapse to one with (x3):\n%s", joined)
	}
	// grouped: 2 source headers + 1 deduped crit line + 2 files lines
	if len(lines) != 5 {
		t.Errorf("got %d lines, want 5:\n%s", len(lines), joined)
	}
	for _, l := range lines {
		if len(l) > diagEvidenceWidth+16 { // indent + " (xN)" allowance
			t.Errorf("line not truncated (%d chars): %.60s...", len(l), l)
		}
	}
}

func TestEvidenceCoreNormalizesRepeats(t *testing.T) {
	// Real repeats differ in timestamp AND pid#tid AND connection id --
	// everything numeric must be ignored (the round-2 build failure was
	// exactly this: "*107" vs "*108" defeating the dedup).
	a := evidenceCore(`2026/07/06 18:43:44 [crit] 7255#7255: *107 stat() failed (13: Permission denied)`)
	b := evidenceCore(`2026/07/06 19:01:02 [crit] 8100#8101: *342 stat() failed (13: Permission denied)`)
	if a != b {
		t.Errorf("cores differ despite identical message: %q vs %q", a, b)
	}
	if evidenceCore("[emerg] host not found") == evidenceCore("[crit] stat() failed") {
		t.Error("different messages must not share a core")
	}
}

func TestPrintVerdictSummaryCascadeAndRunbook(t *testing.T) {
	d, buf := newTestDiagnosis()

	// http 403 (perm, medium) first, then the files check pins the same
	// problem down at high confidence -- one cause, 403 in its cascade.
	d.fail("http-host", "via shop.test :443 -> 403", "perm", confMed,
		"the web server denies access to the service's files", "wor doctor")
	d.fail("files", "1 path(s) block traversal for www-data", "perm", confHigh,
		"www-data cannot traverse /home/wor", "setfacl -m u:www-data:--x /home/wor")
	d.fail("tls", "certificate EXPIRED 3d ago", "tls", confHigh,
		"SSL certificate has expired", "wor ssl renew shop.test")
	d.warn("dns", "resolves elsewhere")

	d.printVerdict()
	out := buf.String()

	if !strings.Contains(out, "Status: FAILED (3 fail, 1 warn)") {
		t.Errorf("missing/wrong status line:\n%s", out)
	}
	if !strings.Contains(out, "Root cause:\n  www-data cannot traverse /home/wor") {
		t.Errorf("root cause must be the high-confidence perm cause:\n%s", out)
	}
	if !strings.Contains(out, "Cascade") || !strings.Contains(out, "http-host: via shop.test :443 -> 403") {
		t.Errorf("the merged 403 row must appear as cascade:\n%s", out)
	}
	if !strings.Contains(out, "1. Fix the root cause -- www-data cannot traverse /home/wor:") ||
		!strings.Contains(out, "setfacl -m u:www-data:--x /home/wor") {
		t.Errorf("runbook step 1 must fix the root cause:\n%s", out)
	}
	if !strings.Contains(out, "2. If it persists -- SSL certificate has expired:") {
		t.Errorf("independent tls cause must be the next numbered step:\n%s", out)
	}
	if !strings.Contains(out, "3. Verify:") || !strings.Contains(out, "wor diagnose shop/api") {
		t.Errorf("runbook must end with a verify step:\n%s", out)
	}
}

func TestPrintVerdictHealthy(t *testing.T) {
	d, buf := newTestDiagnosis()
	d.warn("dns", "resolves elsewhere")
	d.printVerdict()
	out := buf.String()
	if !strings.Contains(out, "Status: OK (0 fail, 1 warn)") {
		t.Errorf("missing OK status:\n%s", out)
	}
	if !strings.Contains(out, "No problems found on this machine.") {
		t.Errorf("missing healthy message:\n%s", out)
	}
}

func TestResolveDocroot(t *testing.T) {
	dir := "/opt/wor/domains/shop/web"
	cases := []struct {
		docRoot, public, want string
	}{
		{"", "", filepath.Join(dir, "public")},
		{"", "public", filepath.Join(dir, "public")},
		{"public", "", filepath.Join(dir, "public")}, // the real-host bug: relative DocumentRoot
		{"dist", "", filepath.Join(dir, "dist")},
		{"/srv/www/shop", "", "/srv/www/shop"}, // absolute stays as-is
	}
	for _, c := range cases {
		svc := &domainmodel.Service{DocumentRoot: c.docRoot, PublicPath: c.public}
		if got := resolveDocroot(dir, svc); got != c.want {
			t.Errorf("resolveDocroot(%q, %q) = %q, want %q", c.docRoot, c.public, got, c.want)
		}
	}
}

func TestClassifyConfigTest(t *testing.T) {
	someErr := errors.New("exit status 1")

	if v, _ := classifyConfigTest("nginx: configuration file test is successful", nil); v != "ok" {
		t.Errorf("exit 0 = %q, want ok", v)
	}

	// The real-host false positive: [emerg] caused purely by reading a
	// root-only cert as non-root must be "unverified", never "broken".
	permOut := `nginx: [emerg] cannot load certificate "/etc/letsencrypt/live/x/fullchain.pem": BIO_new_file() failed (SSL: error:8000000D:system library::Permission denied ...)
nginx: configuration file /etc/nginx/nginx.conf test failed`
	if v, ev := classifyConfigTest(permOut, someErr); v != "unverified" || len(ev) != 0 {
		t.Errorf("permission-denied emerg = (%q, %v), want (unverified, none)", v, ev)
	}

	brokenOut := `nginx: [emerg] unexpected "}" in /etc/nginx/sites-enabled/wor__x.conf:12
nginx: configuration file /etc/nginx/nginx.conf test failed`
	v, ev := classifyConfigTest(brokenOut, someErr)
	if v != "broken" {
		t.Errorf("syntax emerg = %q, want broken", v)
	}
	if len(ev) != 1 || !strings.Contains(ev[0], `unexpected "}"`) {
		t.Errorf("evidence = %v, want the emerg line", ev)
	}

	if v, _ := classifyConfigTest("some unrelated failure", someErr); v != "unverified" {
		t.Errorf("no emerg lines = %q, want unverified", v)
	}
}

func TestTailFileLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "err.log")
	var content strings.Builder
	for i := 1; i <= 50; i++ {
		fmt.Fprintf(&content, "line %d\n", i)
	}
	if err := os.WriteFile(path, []byte(content.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	lines := tailFileLines(path, 10)
	if len(lines) != 10 {
		t.Fatalf("got %d lines, want 10", len(lines))
	}
	if lines[0] != "line 41" || lines[9] != "line 50" {
		t.Errorf("wrong window: first=%q last=%q", lines[0], lines[9])
	}
	if got := tailFileLines(filepath.Join(dir, "missing.log"), 10); got != nil {
		t.Errorf("missing file should yield nil, got %v", got)
	}
}
