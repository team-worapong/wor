package cliapp

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"wor/internal/config"
	"wor/internal/hostprovider"
	"wor/internal/osutil"
)

// ensureRootDirs creates every base WOR_HOME directory and the
// host.env scaffold, matching lib/paths.sh ensure_root_dirs().
func (a *App) ensureRootDirs() error {
	for _, d := range []string{
		a.Cfg.WorHome, a.Cfg.Domains, a.Cfg.Backups,
		filepath.Join(a.Cfg.Configs, "database"), a.Cfg.Logs, a.Cfg.SSL,
		filepath.Join(a.Cfg.WorHome, "tmp"), filepath.Join(a.Cfg.WorHome, "scripts"), filepath.Join(a.Cfg.WorHome, "bin"),
	} {
		if err := osutil.EnsureDir(d); err != nil {
			return err
		}
	}
	return config.SaveHostEnv(filepath.Join(a.Cfg.Configs, "host.env"), a.Cfg.HostProviderName())
}

func (a *App) cmdSetup(args []string) error {
	if len(args) > 0 {
		return a.errf("unknown option for wor setup: %s", args[0])
	}

	// wor setup can be re-run any time. If a config file already exists,
	// every step below defaults to (and, for WOR_HOME, pre-fills) the
	// currently configured value instead of a hardcoded default, so
	// re-running setup doesn't silently reset prior choices.
	_, statErr := os.Stat(a.Cfg.ConfigFile)
	configExisted := statErr == nil
	existingEnv := a.Cfg.Env
	existingWorHome := a.Cfg.WorHome
	existingHostProvider := a.Cfg.HostProvider
	existingSSLProvider := a.Cfg.SSLProvider
	existingPHPFPMEndpoint := a.Cfg.PHPFPMEndpoint

	fmt.Fprintln(a.Out, "WOR Setup Wizard")
	fmt.Fprintln(a.Out, "================")
	fmt.Fprintln(a.Out)

	// Step 1: environment.
	defaultEnv := "2"
	if osutil.IsLinux() {
		defaultEnv = "1"
	}
	if configExisted {
		switch existingEnv {
		case "production":
			defaultEnv = "1"
		case "development":
			defaultEnv = "2"
		}
	}
	envChoice := a.promptDefault("Select Environment (1=Production, 2=Development)", defaultEnv)
	if envChoice == "1" {
		a.Cfg.Env = "production"
	} else {
		a.Cfg.Env = "development"
	}

	// Step 2: WOR_HOME.
	defaultHome := config.DefaultWorHome(a.Cfg.Env)
	homeMenuDefault := "1"
	customLabel := "2=Custom"
	if configExisted && existingWorHome != "" {
		homeMenuDefault = "2"
		customLabel = fmt.Sprintf("2=Custom: %s", existingWorHome)
	}
	homeChoice := a.promptDefault(fmt.Sprintf("Select WOR_HOME (1=Default: %s, %s)", defaultHome, customLabel), homeMenuDefault)
	if homeChoice == "2" {
		switch {
		case existingWorHome != "" && dirExists(existingWorHome):
			// Already recorded and verified to exist on disk -- skip
			// re-prompting entirely and reuse it as-is.
			a.Cfg.WorHome = existingWorHome
			a.ok("Using existing WOR_HOME: %s", existingWorHome)
		case existingWorHome != "":
			// Recorded but not found on disk (moved/deleted) -- ask the
			// user to confirm or correct it, pre-filled with the old value.
			a.warn("Recorded WOR_HOME not found on disk: %s", existingWorHome)
			a.Cfg.WorHome = a.promptDefault("Enter WOR_HOME", existingWorHome)
		default:
			a.Cfg.WorHome = a.prompt("Enter WOR_HOME: ")
		}
	} else {
		a.Cfg.WorHome = defaultHome
	}
	a.Cfg.Domains = filepath.Join(a.Cfg.WorHome, "domains")
	a.Cfg.Backups = filepath.Join(a.Cfg.WorHome, "backups")
	a.Cfg.Configs = filepath.Join(a.Cfg.WorHome, "configs")
	a.Cfg.Logs = filepath.Join(a.Cfg.WorHome, "logs")
	a.Cfg.SSL = filepath.Join(a.Cfg.WorHome, "ssl")
	a.Store.DomainsDir = a.Cfg.Domains

	// Step 3: web server provider.
	if err := a.setupWebServer(configExisted, existingHostProvider); err != nil {
		return err
	}

	// Step 4: SSL provider.
	a.setupSSL(configExisted, existingSSLProvider)

	// Step 5: PHP / PHP-FPM.
	a.setupPHP(existingPHPFPMEndpoint)

	// Summary.
	fmt.Fprintln(a.Out)
	fmt.Fprintln(a.Out, "Setup Summary")
	fmt.Fprintln(a.Out, "=============")
	fmt.Fprintf(a.Out, "environment  : %s\n", a.Cfg.Env)
	fmt.Fprintf(a.Out, "WOR_HOME     : %s\n", a.Cfg.WorHome)
	fmt.Fprintf(a.Out, "host_provider: %s\n", a.Cfg.HostProviderName())
	fmt.Fprintf(a.Out, "ssl_provider : %s\n", a.Cfg.SSLProviderName())
	if a.Cfg.PHPEnabled {
		if a.Cfg.PHPFPMEndpoint != "" {
			fmt.Fprintf(a.Out, "php_fpm      : %s\n", a.Cfg.PHPFPMEndpoint)
		} else {
			fmt.Fprintln(a.Out, "php_fpm      : not configured (PHP services will fail preflight)")
		}
	} else {
		fmt.Fprintln(a.Out, "php_fpm      : php not installed")
	}
	fmt.Fprintf(a.Out, "config       : %s\n", a.Cfg.ConfigFile)
	fmt.Fprintln(a.Out)

	if !a.confirmYesDefaultYes("Proceed with setup?") {
		return a.errf("setup cancelled")
	}

	if err := a.Cfg.Save(); err != nil {
		return err
	}
	if err := a.ensureRootDirs(); err != nil {
		return err
	}
	a.ok("Config written: %s", a.Cfg.ConfigFile)
	a.ok("WOR_HOME ready: %s", a.Cfg.WorHome)

	if a.confirmYesDefaultNo("Would you like to create your first website? (y to run wor create)") {
		return a.cmdCreate(nil)
	}
	return nil
}

func (a *App) setupWebServer(configExisted bool, existingHostProvider string) error {
	for {
		nginxP, _ := hostprovider.New("nginx", a.Cfg)
		apacheP, _ := hostprovider.New("apache", a.Cfg)
		nginxBin, hasNginx := nginxP.Binary()
		apacheBin, hasApache := apacheP.Binary()

		nginxVersion := "not installed"
		if hasNginx {
			nginxVersion = strings.TrimPrefix(osutil.RunVersion(nginxBin, "-v"), "nginx version: ")
		}
		apacheVersion := "not installed"
		if hasApache {
			apacheVersion = strings.TrimPrefix(osutil.RunVersion(apacheBin, "-v"), "Server version: ")
		}

		fmt.Fprintln(a.Out)
		fmt.Fprintln(a.Out, "Select Web Server Provider")
		fmt.Fprintf(a.Out, "1. nginx : %s\n", nginxVersion)
		fmt.Fprintf(a.Out, "2. apache : %s\n", apacheVersion)
		fmt.Fprintln(a.Out, "3. skip")
		if !hasNginx && !hasApache {
			a.info("nginx/apache not installed. Install one, then re-run wor setup, or choose skip.")
		}

		defaultChoice := "3"
		if configExisted {
			switch existingHostProvider {
			case "nginx":
				defaultChoice = "1"
			case "apache":
				defaultChoice = "2"
			case "skip":
				defaultChoice = "3"
			}
		}
		choice := a.promptDefault("Choose", defaultChoice)
		switch choice {
		case "1", "nginx":
			if hasNginx {
				a.Cfg.HostProvider = "nginx"
				return nil
			}
			a.warn("nginx is not installed.")
		case "2", "apache":
			if hasApache {
				a.Cfg.HostProvider = "apache"
				return nil
			}
			a.warn("apache is not installed.")
		case "3", "skip":
			a.Cfg.HostProvider = "skip"
			return nil
		default:
			return a.errf("invalid web server provider: %s", choice)
		}
	}
}

// setupPHP detects PHP and PHP-FPM and records a PHP_FPM_ENDPOINT,
// closing the gap where `wor create`/`wor service add` would hard-block
// PHP services with "Configure PHP_FPM_ENDPOINT in .../host.env" and
// leave the user to find and set it by hand. The common case (a single
// endpoint found at one of the usual unix socket paths) just asks for
// a yes/no confirmation; if that fails but php-fpm's own config says
// otherwise (see hostprovider.DetectListenAddrs), those are presented
// as a numbered menu instead of making the user type a path blind, the
// same way setupWebServer numbers its nginx/apache/skip choices. PHP is
// optional: if it's not installed, this records that and returns
// without prompting for anything, exactly like letsencrypt being
// unavailable doesn't block setupSSL.
func (a *App) setupPHP(existingEndpoint string) {
	fmt.Fprintln(a.Out)
	fmt.Fprintln(a.Out, "PHP / PHP-FPM")

	if !osutil.Exists("php") {
		fmt.Fprintln(a.Out, "php     : not installed")
		a.info("PHP not installed -- skipping PHP-FPM setup. Install PHP and re-run wor setup if you plan to host PHP sites.")
		a.Cfg.PHPEnabled = false
		return
	}
	a.Cfg.PHP.Bin = osutil.Which("php")
	a.Cfg.PHP.Version = osutil.RunVersion("php", "--version")
	a.Cfg.PHPEnabled = true
	fmt.Fprintf(a.Out, "php     : %s\n", a.Cfg.PHP.Version)

	if osutil.Exists("php-fpm") {
		a.Cfg.PHPFPM.Bin = osutil.Which("php-fpm")
		a.Cfg.PHPFPM.Version = osutil.RunVersion("php-fpm", "--version")
		fmt.Fprintf(a.Out, "php-fpm : %s\n", a.Cfg.PHPFPM.Version)
	} else {
		fmt.Fprintln(a.Out, "php-fpm : not installed")
	}

	// Reuse whatever was already recorded (env var, prior config file,
	// or host.env) as the starting point, so re-running setup doesn't
	// silently drop a manually-configured endpoint.
	if existingEndpoint != "" {
		a.Cfg.PHPFPMEndpoint = existingEndpoint
	}

	if ep, ok := hostprovider.PHPFPMEndpoint(a.Cfg); ok {
		if a.confirmYesDefaultYes(fmt.Sprintf("Detected PHP-FPM endpoint: %s. Use it?", ep)) {
			a.Cfg.PHPFPMEndpoint = ep
			return
		}
	} else if candidates := hostprovider.DetectListenAddrs(a.Cfg.PHPFPM.Bin); len(candidates) > 0 {
		// The fixed unix-socket list came up empty, but php-fpm's own
		// config says otherwise (e.g. it's not started yet so no socket
		// file exists on disk, or it listens on 127.0.0.1:port instead
		// of a unix socket) -- offer what was actually found as a
		// numbered menu instead of making the user type it blind.
		fmt.Fprintln(a.Out, "Detected PHP-FPM listen address(es):")
		for i, c := range candidates {
			fmt.Fprintf(a.Out, "%d. %s\n", i+1, c)
		}
		manualOption := len(candidates) + 1
		fmt.Fprintf(a.Out, "%d. Enter manually\n", manualOption)
		choice := a.promptDefault("Choose", "1")
		if n, err := strconv.Atoi(choice); err == nil && n >= 1 && n <= len(candidates) {
			a.Cfg.PHPFPMEndpoint = candidates[n-1]
			return
		}
	} else {
		a.warn("No PHP-FPM endpoint found at the usual unix socket locations or in php-fpm's own config.")
	}

	a.Cfg.PHPFPMEndpoint = a.promptDefault(
		"Enter PHP-FPM endpoint (unix:/path/to.sock or host:port), or leave blank to configure later",
		a.Cfg.PHPFPMEndpoint,
	)
}

func (a *App) setupSSL(configExisted bool, existingSSLProvider string) {
	certbotFound := osutil.Exists("certbot")
	opensslFound := osutil.Exists("openssl")
	// Let's Encrypt (via certbot) only works against the nginx/apache
	// plugin wor's ssl package invokes -- if the chosen host provider is
	// "skip" (or we're on Windows, where certbot isn't supported at
	// all), it can't actually be used here regardless of whether
	// certbot happens to be installed.
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

	fmt.Fprintln(a.Out)
	fmt.Fprintln(a.Out, "Default SSL Provider")
	fmt.Fprintf(a.Out, "1. self-signed : %s\n", opensslVersion)
	if letsencryptCompatible {
		fmt.Fprintf(a.Out, "2. letsencrypt : %s\n", certbotVersion)
	} else {
		fmt.Fprintf(a.Out, "2. letsencrypt : %s (not available)\n", certbotVersion)
	}
	fmt.Fprintln(a.Out, "3. skip/none")

	defaultChoice := "1"
	if a.Cfg.Env == "production" {
		defaultChoice = "2"
	}
	if configExisted {
		switch existingSSLProvider {
		case "self-signed":
			defaultChoice = "1"
		case "letsencrypt":
			defaultChoice = "2"
		case "none", "skip":
			defaultChoice = "3"
		}
	}

	choice := a.promptDefault("Choose", defaultChoice)
	switch choice {
	case "2", "letsencrypt":
		if letsencryptCompatible {
			a.Cfg.SSLProvider = "letsencrypt"
			return
		}
		a.warn("letsencrypt is not available here (needs certbot, a non-Windows OS, and host_provider=nginx or apache). Falling back to self-signed.")
		a.Cfg.SSLProvider = "self-signed"
	case "3", "skip", "none":
		a.Cfg.SSLProvider = "none"
	default:
		a.Cfg.SSLProvider = "self-signed"
	}
}
