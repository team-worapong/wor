package ssl

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"wor/internal/osutil"
)

// wor keeps its own copy of every certificate under
// $WOR_HOME/ssl/hosts/<host>/, whatever the provider. self-signed and
// custom certificates were always written there; Let's Encrypt used to
// be the exception, with the vhost pointing straight at
// /etc/letsencrypt/live/<host>/.
//
// That exception is what took a web server down: the real key lives in
// root-only /etc/letsencrypt/archive/, which the master process can
// read on Linux (it runs as root) but not on macOS, where Homebrew
// starts it as the login user. An unreadable certificate makes the
// whole configuration invalid, and one invalid vhost fails the config
// test for every site on the machine.
//
// Copying removes the asymmetry: one ownership model for all three
// providers, permissions wor controls, and nothing reaching into a
// directory certbot owns. The cost is that the copy can go stale, which
// is why SyncResult below is recorded and why `wor health` reports
// expiry. See docs/ssl-redesign.md.
const (
	CertFileName = "fullchain.pem"
	KeyFileName  = "privkey.pem"
)

// SourcePaths returns where a host's certificate really comes from.
// For letsencrypt that is certbot's live directory; for self-signed and
// custom, wor's copy is itself the original, so there is nothing to
// sync from and ok is false.
func SourcePaths(host string, st State) (cert, key string, ok bool) {
	if st.Provider != "letsencrypt" {
		return "", "", false
	}
	dir := LetsEncryptCertDir(host)
	return filepath.Join(dir, CertFileName), filepath.Join(dir, KeyFileName), true
}

// ManagedPaths returns where wor keeps its own copy for host.
func ManagedPaths(sslRoot, host string) (cert, key string) {
	dir := HostDir(sslRoot, host)
	return filepath.Join(dir, CertFileName), filepath.Join(dir, KeyFileName)
}

// CopyCertificate refreshes wor's copy of host's certificate from
// srcCert/srcKey, giving both files mode 0600 and the ownership in
// uid/gid.
//
// It reports changed=false and writes nothing when the copy already
// matches the source. That matters because this runs from certbot's
// renewal hook: a hook that fired for an unrelated host, or for a
// certificate that did not actually move, must not churn the web
// server with a needless reload.
//
// 0600 owned by the operator is enough on both platforms without any
// ACL, because the process that reads a certificate is the web server's
// *master*: root on Linux, which can read anything, and the login user
// on macOS, which is the operator. Widening the mode instead would
// expose a private key to every process on the machine, which is not a
// trade this makes.
func CopyCertificate(sslRoot, host, srcCert, srcKey string, uid, gid int) (dstCert, dstKey string, changed bool, err error) {
	dstCert, dstKey = ManagedPaths(sslRoot, host)

	certData, err := osutil.ReadFilePrivileged(srcCert)
	if err != nil {
		return "", "", false, err
	}
	keyData, err := osutil.ReadFilePrivileged(srcKey)
	if err != nil {
		return "", "", false, err
	}

	if sameContent(dstCert, certData) && sameContent(dstKey, keyData) {
		return dstCert, dstKey, false, nil
	}

	if err := osutil.EnsureDir(HostDir(sslRoot, host)); err != nil {
		return "", "", false, err
	}
	for _, f := range []struct {
		path string
		data []byte
	}{{dstCert, certData}, {dstKey, keyData}} {
		if err := osutil.WriteFileAtomic(f.path, f.data, 0o600); err != nil {
			return "", "", false, err
		}
		if err := osutil.Chown(f.path, uid, gid); err != nil {
			return "", "", false, fmt.Errorf("cannot give %s to uid %d: %w", f.path, uid, err)
		}
	}
	return dstCert, dstKey, true, nil
}

func sameContent(path string, want []byte) bool {
	got, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return bytes.Equal(sum(got), sum(want))
}

func sum(b []byte) []byte {
	h := sha256.Sum256(b)
	return h[:]
}

// SyncResult is the outcome of the most recent `wor ssl sync` for a
// host, stored beside its certificate state as sync.json.
//
// It exists because the command that matters most here runs unattended:
// certbot's renewal hook, in the middle of the night. wor has no log of
// its own, so a hook that failed would otherwise leave a trace only in
// the renewal job's output, which nobody reads -- and the first visible
// symptom would be an expired certificate weeks later. `wor ssl status`,
// `wor health` and `wor diagnose` read this back so the answer to "why
// is this certificate stale" is already on screen.
type SyncResult struct {
	At      string `json:"at"`
	OK      bool   `json:"ok"`
	Changed bool   `json:"changed"`
	Source  string `json:"source,omitempty"`
	Error   string `json:"error,omitempty"`
}

func syncFile(sslRoot, host string) string {
	return filepath.Join(HostDir(sslRoot, host), "sync.json")
}

// WriteSyncResult records r. A failure to record is not worth failing
// the sync over, so callers may ignore the error -- but it is returned
// rather than swallowed so the decision stays at the call site.
func WriteSyncResult(sslRoot, host string, r SyncResult) error {
	if err := osutil.EnsureDir(HostDir(sslRoot, host)); err != nil {
		return err
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return osutil.WriteFileAtomic(syncFile(sslRoot, host), data, 0o600)
}

// LoadSyncResult returns the last recorded sync outcome for host, or
// ok=false when the host has never been synced.
func LoadSyncResult(sslRoot, host string) (SyncResult, bool) {
	data, err := os.ReadFile(syncFile(sslRoot, host))
	if err != nil {
		return SyncResult{}, false
	}
	var r SyncResult
	if err := json.Unmarshal(data, &r); err != nil {
		return SyncResult{}, false
	}
	return r, true
}

// ChownHostDir gives every file wor keeps for host to uid/gid.
//
// Needed because the renewal hook runs as root: WriteState and
// WriteSyncResult create their files as the running user, so without
// this an unattended renewal leaves ssl.json and sync.json owned by
// root with mode 0600, inside a directory the operator owns. The
// operator's next command then cannot read its own certificate state --
// and buildWriteParams treats an unreadable state as "no certificate",
// which would regenerate the vhost without its HTTPS block and reload.
// A silent downgrade to plaintext is a much worse outcome than the
// permission error that caused it.
func ChownHostDir(sslRoot, host string, uid, gid int) error {
	dir := HostDir(sslRoot, host)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := osutil.Chown(dir, uid, gid); err != nil {
		return err
	}
	for _, e := range entries {
		if err := osutil.Chown(filepath.Join(dir, e.Name()), uid, gid); err != nil {
			return err
		}
	}
	return nil
}
