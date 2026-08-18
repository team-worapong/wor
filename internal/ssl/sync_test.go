package ssl

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTemp(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// The first copy reports changed, so the caller reloads; an identical
// second copy must report unchanged, so a renewal hook that fires for a
// certificate that did not move does not churn the web server.
func TestCopyCertificateIsIdempotent(t *testing.T) {
	src := t.TempDir()
	sslRoot := t.TempDir()
	host := "app.example.com"

	srcCert := writeTemp(t, src, "fullchain.pem", "CERT-1")
	srcKey := writeTemp(t, src, "privkey.pem", "KEY-1")

	uid, gid := os.Getuid(), os.Getgid()

	cert, key, changed, err := CopyCertificate(sslRoot, host, srcCert, srcKey, uid, gid)
	if err != nil {
		t.Fatalf("first copy: %v", err)
	}
	if !changed {
		t.Error("the first copy must report a change")
	}
	if got, _ := os.ReadFile(cert); string(got) != "CERT-1" {
		t.Errorf("cert content = %q", got)
	}
	if got, _ := os.ReadFile(key); string(got) != "KEY-1" {
		t.Errorf("key content = %q", got)
	}

	_, _, changed, err = CopyCertificate(sslRoot, host, srcCert, srcKey, uid, gid)
	if err != nil {
		t.Fatalf("second copy: %v", err)
	}
	if changed {
		t.Error("copying an unchanged certificate must not report a change")
	}

	// A renewal moves the files, which must be picked up.
	writeTemp(t, src, "fullchain.pem", "CERT-2")
	writeTemp(t, src, "privkey.pem", "KEY-2")
	_, _, changed, err = CopyCertificate(sslRoot, host, srcCert, srcKey, uid, gid)
	if err != nil {
		t.Fatalf("third copy: %v", err)
	}
	if !changed {
		t.Error("a renewed certificate must report a change")
	}
	if got, _ := os.ReadFile(cert); string(got) != "CERT-2" {
		t.Errorf("cert was not refreshed: %q", got)
	}
}

// The private key must never be readable by anything but its owner.
// Widening this to fix a permission problem would hand the key to every
// process on the machine.
func TestCopiedKeyIsOwnerOnly(t *testing.T) {
	src := t.TempDir()
	sslRoot := t.TempDir()
	srcCert := writeTemp(t, src, "fullchain.pem", "CERT")
	srcKey := writeTemp(t, src, "privkey.pem", "KEY")

	_, key, _, err := CopyCertificate(sslRoot, "app.example.com", srcCert, srcKey, os.Getuid(), os.Getgid())
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	info, err := os.Stat(key)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("private key mode = %o, want 600", perm)
	}
}

// Only letsencrypt has a source outside WOR_HOME; for the other two
// providers wor's copy is the original, so there is nothing to sync.
func TestSourcePathsOnlyAppliesToLetsEncrypt(t *testing.T) {
	cert, key, ok := SourcePaths("app.example.com", State{Provider: "letsencrypt"})
	if !ok {
		t.Fatal("letsencrypt must be syncable")
	}
	if cert != "/etc/letsencrypt/live/app.example.com/fullchain.pem" {
		t.Errorf("cert source = %q", cert)
	}
	if key != "/etc/letsencrypt/live/app.example.com/privkey.pem" {
		t.Errorf("key source = %q", key)
	}
	for _, provider := range []string{"self-signed", "custom", ""} {
		if _, _, ok := SourcePaths("app.example.com", State{Provider: provider}); ok {
			t.Errorf("provider %q must not be syncable", provider)
		}
	}
}

// forceHttps is absent from every state file written before it existed,
// and absent must read as off: upgrading wor must not start redirecting
// a site nobody asked to redirect.
func TestForceHTTPSDefaultsToOffForOlderStateFiles(t *testing.T) {
	sslRoot := t.TempDir()
	host := "legacy.example.com"
	dir := HostDir(sslRoot, host)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	legacy := `{
  "enabled": true,
  "provider": "letsencrypt",
  "certFile": "/etc/letsencrypt/live/legacy.example.com/fullchain.pem",
  "keyFile": "/etc/letsencrypt/live/legacy.example.com/privkey.pem",
  "autoRenew": "enabled"
}`
	if err := os.WriteFile(filepath.Join(dir, "ssl.json"), []byte(legacy), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	st, ok, err := LoadState(sslRoot, host)
	if err != nil || !ok {
		t.Fatalf("LoadState: ok=%v err=%v", ok, err)
	}
	// Absent must stay absent, so each provider can supply the
	// behaviour it had before the setting existed.
	if st.Recorded() {
		t.Error("a state file with no forceHttps field must not report a recorded value")
	}
	if st.ForceHTTPSOr(false) {
		t.Error("with an nginx-style legacy default, absent must resolve to off")
	}
	if !st.ForceHTTPSOr(true) {
		t.Error("with an apache-style legacy default, absent must resolve to on -- an upgrade must not drop an existing redirect")
	}
	st.SetForceHTTPS(false)
	if !st.Recorded() || st.ForceHTTPSOr(true) {
		t.Error("an explicit off must override the legacy default")
	}
	if !st.Enabled || st.Provider != "letsencrypt" {
		t.Errorf("the rest of the state must still load: %+v", st)
	}
}

func TestSyncResultRoundTrips(t *testing.T) {
	sslRoot := t.TempDir()
	host := "app.example.com"

	if _, ok := LoadSyncResult(sslRoot, host); ok {
		t.Error("a host that was never synced must report no result")
	}
	want := SyncResult{At: "2026-08-18T03:00:00Z", OK: false, Source: "/etc/letsencrypt/live/app.example.com/fullchain.pem", Error: "boom"}
	if err := WriteSyncResult(sslRoot, host, want); err != nil {
		t.Fatalf("WriteSyncResult: %v", err)
	}
	got, ok := LoadSyncResult(sslRoot, host)
	if !ok {
		t.Fatal("result should load back")
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}
