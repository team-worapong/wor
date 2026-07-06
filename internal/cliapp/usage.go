package cliapp

import "fmt"

func (a *App) usage() {
	fmt.Fprintf(a.Err, `%s (Go) v%s

Usage:
  wor version
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
  wor service status
  wor service logs <domain>/<service> [--lines=100]

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
      "wor source backup" automatically, then replaced -- no flag needed)
  wor source pull <domain> [--stash]
  wor source pull <domain>/<service> [--stash]
  wor source backup <domain> [--gitignore=enable|disable]
  wor source backup <domain>/<service> [--gitignore=enable|disable]

  wor deploy <host|domain/service> [--pull-only] [--no-pull] [--no-restart] [--force] [--stash]
  wor rollback <domain>/<service> [--yes]
      (hard-resets the service's source to origin/<branch>, discarding
      uncommitted local changes -- backs up via "wor source backup"
      first; requires domain/service, never a bare domain)

  wor ssl issue <host> [--provider=letsencrypt|self-signed|custom|none] [--preferred=<host>]
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
