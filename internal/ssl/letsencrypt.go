package ssl

import (
	"fmt"
	"os"

	"wor/internal/osutil"
)

// LetsEncryptCertDir returns certbot's live certificate directory for a
// host (Unix convention: /etc/letsencrypt/live/<host>).
func LetsEncryptCertDir(host string) string { return "/etc/letsencrypt/live/" + host }
func letsEncryptRenewalConf(host string) string {
	return "/etc/letsencrypt/renewal/" + host + ".conf"
}

// IssueLetsEncrypt runs certbot against the configured host provider's
// plugin (--nginx or --apache), matching
// lib/providers/ssl/letsencrypt.sh ssl_issue_letsencrypt(). Certbot has
// no official native Windows support, so this returns a clear error
// there rather than attempting something unreliable.
func IssueLetsEncrypt(hostProviderName, primaryHost string, aliases []string) error {
	if osutil.IsWindows() {
		return fmt.Errorf("Let's Encrypt via certbot is not supported on Windows; use --provider=self-signed or --provider=custom")
	}
	if !osutil.Exists("certbot") {
		return fmt.Errorf("certbot not found")
	}
	var pluginFlag string
	switch hostProviderName {
	case "nginx":
		pluginFlag = "--nginx"
	case "apache":
		pluginFlag = "--apache"
	default:
		return fmt.Errorf("unsupported host provider: %s", hostProviderName)
	}

	args := []string{pluginFlag}
	for _, d := range append([]string{primaryHost}, aliases...) {
		args = append(args, "-d", d)
	}

	if pathExistsAny(letsEncryptRenewalConf(primaryHost)) || pathExistsAny(LetsEncryptCertDir(primaryHost)) {
		args = append(args, "--reinstall", "--non-interactive")
	}
	cmd, err := osutil.SudoCommand("certbot", args...)
	if err != nil {
		return err
	}
	return cmd.Run()
}

// RenewLetsEncrypt runs `certbot renew`, matching ssl_renew_letsencrypt().
func RenewLetsEncrypt() error {
	if osutil.IsWindows() {
		return fmt.Errorf("Let's Encrypt via certbot is not supported on Windows")
	}
	if !osutil.Exists("certbot") {
		return fmt.Errorf("certbot not found")
	}
	cmd, err := osutil.SudoCommand("certbot", "renew")
	if err != nil {
		return err
	}
	return cmd.Run()
}

func pathExistsAny(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
