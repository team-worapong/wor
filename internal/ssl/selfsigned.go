package ssl

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"wor/internal/osutil"
)

// IssueSelfSigned generates a self-signed certificate for host (plus
// any SANs) using the system `openssl` binary, matching
// lib/providers/ssl/self_signed.sh ssl_issue_self_signed(). Returns the
// generated cert/key paths.
func IssueSelfSigned(sslRoot, host string, aliases []string) (cert, key string, err error) {
	if !osutil.Exists("openssl") {
		return "", "", fmt.Errorf("openssl not found")
	}
	dir := HostDir(sslRoot, host)
	if err := osutil.EnsureDir(dir); err != nil {
		return "", "", err
	}
	cert = filepath.Join(dir, "fullchain.pem")
	key = filepath.Join(dir, "privkey.pem")

	altNames := "DNS:" + host
	for _, a := range aliases {
		altNames += ",DNS:" + a
	}

	cmd := exec.Command("openssl", "req", "-x509", "-nodes", "-newkey", "rsa:2048", "-days", "825",
		"-keyout", key, "-out", cert, "-subj", "/CN="+host,
		"-addext", "subjectAltName="+altNames)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", "", fmt.Errorf("openssl failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if err := os.Chmod(key, 0o600); err != nil {
		// Non-fatal: matches the shell version's `|| true`.
		_ = err
	}
	return cert, key, nil
}
