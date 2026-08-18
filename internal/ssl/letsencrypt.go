package ssl

import (
	"fmt"
	"os"
	"strings"

	"wor/internal/osutil"
)

// LetsEncryptCertDir returns certbot's live certificate directory for a
// host (Unix convention: /etc/letsencrypt/live/<host>).
func LetsEncryptCertDir(host string) string { return "/etc/letsencrypt/live/" + host }
func letsEncryptRenewalConf(host string) string {
	return "/etc/letsencrypt/renewal/" + host + ".conf"
}

// IssueLetsEncrypt obtains a certificate with certbot's webroot
// authenticator, writing challenges into webroot and registering
// deployHook so every later renewal refreshes wor's own copy.
//
// It used to use certbot's --nginx/--apache plugins. Those work by
// editing the vhost, which wor regenerates from templates on every
// write -- two tools owning one generated file, which is exactly what
// DESIGN.md section 17 avoids for user customisation. Splitting the
// nginx template into separate :80 and :443 blocks made the conflict
// concrete: both blocks carry the same server_name, so which one the
// plugin patches its challenge into is not something to depend on, and
// picking the :443 block would leave an HTTP-01 challenge unanswered.
//
// With webroot, certbot writes a file and touches nothing else. As a
// side effect the host provider stops mattering here at all, so nginx
// and apache now issue certificates through one code path, and wor --
// not certbot -- performs the reload, which routes it through the
// validate-then-reload helper the rest of the project uses.
//
// certbot has no official native Windows support, so this returns a
// clear error there rather than attempting something unreliable.
func IssueLetsEncrypt(primaryHost string, aliases []string, webroot, deployHook string) error {
	if osutil.IsWindows() {
		return fmt.Errorf("Let's Encrypt via certbot is not supported on Windows; use --provider=self-signed or --provider=custom")
	}
	if !osutil.Exists("certbot") {
		return fmt.Errorf("certbot not found")
	}
	if webroot == "" {
		return fmt.Errorf("no ACME webroot configured; run wor setup")
	}

	args := []string{"certonly", "--webroot", "-w", webroot}
	for _, d := range append([]string{primaryHost}, aliases...) {
		args = append(args, "-d", d)
	}
	if deployHook != "" {
		args = append(args, "--deploy-hook", deployHook)
	}

	if pathExistsAny(letsEncryptRenewalConf(primaryHost)) || pathExistsAny(LetsEncryptCertDir(primaryHost)) {
		// --force-renewal is deliberately NOT added: re-running issue
		// on an unexpired certificate should reuse it, not spend a
		// rate-limited issuance. What this does do is let certbot
		// rewrite the renewal config, which is how a host moves off the
		// old --nginx authenticator onto webroot.
		args = append(args, "--non-interactive", "--keep-until-expiring")
	}
	cmd, err := osutil.SudoCommand("certbot", args...)
	if err != nil {
		return err
	}
	return cmd.Run()
}

// RenewalConfHasDeployHook reports whether host's renewal config already
// runs a deploy hook. Used to tell an operator that a host predating
// this change will not refresh wor's copy on its own until it is
// reissued.
func RenewalConfHasDeployHook(host string) (bool, error) {
	data, err := osutil.ReadFilePrivileged(letsEncryptRenewalConf(host))
	if err != nil {
		return false, err
	}
	return strings.Contains(string(data), "renew_hook") || strings.Contains(string(data), "deploy_hook"), nil
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
