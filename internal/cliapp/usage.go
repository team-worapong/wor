package cliapp

import "fmt"

func (a *App) usage() {
	fmt.Fprintf(a.Err, `%s (Go) v%s

Usage:
  wor version
  wor upgrade [--yes]
      (compares this binary against the release the download site
      publishes, shows both, and installs the newer one once you
      confirm. --yes skips the confirmation.)
  wor --version
  wor setup
  wor doctor
  wor env
  wor clean
  wor reset
  wor create [host]
      (interactive only -- no other flags accepted; prompts for
      service type, domain id override, domain type, and hosts entry)

  wor domain add <domain-id>
  wor domain remove <domain-id>

  wor path [.|./<path>|<domain>[/<service>]]
      (prints the absolute directory of a domain or service; "." means
      WOR_HOME itself, "./<path>" means WOR_HOME/<path> -- any subtree,
      e.g. ./logs. For scripting: cd "$(wor path myapp/backend)".
      With no argument, shows a numbered menu -- WOR_HOME first, then
      every domain and domain/service -- and prints whichever one you
      select)
  wor shell-init
      (prints a shell function for ~/.bashrc / ~/.zshrc -- install with
      eval "$(wor shell-init)" -- after which
      "wor goto <domain>[/<service>]" cd's straight into that folder,
      and a bare "wor goto" opens the numbered picker)

  wor service add <domain>/<service> [--host=<host>] [--port=<port>] [--entry=<entry-point>] [--service-type=static|node|go|python|php] [--php-version=<version>] [--no-php-pool] [--no-start]
      (php services get their own dedicated php-fpm pool automatically
      when exactly one PHP-FPM version is detected on this host;
      --php-version= picks one when several are detected, --no-php-pool
      forces the legacy shared PHP_FPM_ENDPOINT instead. node/go/python
      services are started automatically after being created; --no-start
      skips that and leaves the service stopped until you run
      "wor service start <domain>/<service>" or "wor run" yourself.)
  wor service remove <domain>/<service> [--cascade] [--yes]
  wor service start <domain>/<service>
  wor service stop <domain>/<service>
  wor service restart <domain>/<service>
  wor service reload <domain>/<service>
      (php services with their own pool only: re-renders the pool from
      .wor/php.ini and .wor/php-fpm.ini, validates it, reloads php-fpm,
      and prints what is now in force. Config only -- to restart the
      process use "wor service restart", to re-render a vhost use
      "wor host reload".)
  wor service status
  wor service logs <domain>/<service> [--lines=100]
  wor service chown <domain>/<service> [<user>]
      (hands the service's files back to the operator account, or to
      <user> if you name one. Changes the owner only, never the group,
      and re-grants the php-fpm pool afterwards -- for a tree left
      owned by root or by another admin. Needs elevation.)

  wor run
      (ensures every enabled service -- and the runtimes/web server it
      needs -- is up, starting anything that isn't; offers to register
      "pm2 startup" if it was never set up. Skips a failed service and
      keeps going; ends with a started/failed summary line.)

  wor host add <host> [--target=<domain>/<service>] [--server=nginx|apache] [--replace] [--domain-type=local|public] [--add-hosts|--no-hosts]
  wor host remove <host> [--yes]
  wor host list
  wor host test
  wor host reload
  wor host logs <host> [access|error] [--lines=100]

  wor database add <domain>/<profile> [--label="Label"]
  wor database remove <domain>/<profile>
  wor database backup <domain>/<profile>[/database]

  wor source clone <domain> <git-url>
  wor source clone <domain>/<service> <git-url>
      (if the target already has source, it's backed up via
      "wor source backup" automatically, then replaced -- no flag needed.
      Replacing the tree would also take .env and .wor with it, so it
      asks what to do with each before doing anything.)
  wor source pull <domain> [--stash]
  wor source pull <domain>/<service> [--stash]
  wor source backup <domain> [--gitignore=enable|disable]
  wor source backup <domain>/<service> [--gitignore=enable|disable]

  wor deploy <host|domain/service> [--pull-only] [--no-pull] [--no-restart] [--force] [--stash]
      (for a php service with its own pool, also re-renders that pool
      from the service's .wor/php.ini and .wor/php-fpm.ini -- see
      docs/services.md. An invalid file fails the deploy before the tree
      is touched; an unchanged pool is left alone rather than reloaded.)
  wor rollback <domain>/<service> [--yes]
      (hard-resets the service's source to origin/<branch>, discarding
      uncommitted local changes -- backs up via "wor source backup"
      first; requires domain/service, never a bare domain)

  wor ssl issue <host> [--provider=letsencrypt|self-signed|custom|none] [--preferred=<host>] [--redirect|--no-redirect]
      (asks whether plain HTTP should redirect to HTTPS. The default
      offered is on for a letsencrypt/custom certificate and off for a
      self-signed one or any local hostname, because a redirect to a
      certificate the browser does not trust makes the site unreachable
      without clicking through a warning. --redirect/--no-redirect
      answer it without prompting.)
  wor ssl redirect <host> on|off
      (turns the HTTP -> HTTPS redirect on or off for a host that
      already has a certificate, without reissuing it)
  wor ssl sync <host>
      (refreshes wor's own copy of the certificate from certbot's store
      and reloads. Registered automatically as certbot's deploy hook, so
      renewals pick themselves up; run it by hand to migrate a host
      issued before wor kept its own copy, or to repair a copy that has
      drifted.)
  wor ssl renew <host>
  wor ssl status <host>
  wor ssl remove <host> [--yes]
  wor ssl install <host> --cert=/path/fullchain.pem --key=/path/privkey.pem
  wor info <host|domain/service>
  wor health
      (fleet-wide health sweep: for every enabled service, checks its
      process/pool, port, and one real HTTP request through the web
      server, then flags the broken ones with a pointer to
      "wor diagnose <target>". Answers "are my services serving?" --
      unlike "wor doctor", which answers "is this machine set up
      right?". Read-only; exit code 1 when a problem is found, so it
      can drive cron/monitoring.)
  wor diagnose <host|domain/service>
      (read-only root-cause analysis for ONE down/misbehaving service:
      checks config, dns, web server, ssl expiry, process state, port,
      http reachability, file permissions, disk, and logs, then prints
      the root cause, evidence, and copy-pasteable fix commands -- it
      never changes anything itself. Exit code 1 when a problem is
      found. The recovery story: wor health -> wor diagnose <target>
      -> wor run.)

Environment:
  WOR_ENV=%s
  WOR_HOME=%s
  Config=%s
`, ProductName, Version, a.Cfg.Env, a.Cfg.WorHome, a.Cfg.ConfigFile)
}
