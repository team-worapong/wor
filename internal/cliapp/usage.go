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
  wor reset [--yes]
  wor create [host]
      (interactive only -- no other flags accepted; prompts for
      service type, domain id override, domain type, and hosts entry)

  wor domain add <domain-id>
  wor domain remove <domain-id>

  wor service add <domain>/<service> [--host=<host>] [--port=<port>] [--entry=<entry-point>] [--service-type=static|node|go|python|php]
  wor service remove <domain>/<service> [--cascade] [--yes]
  wor service start <domain>/<service>
  wor service stop <domain>/<service>
  wor service restart <domain>/<service>
  wor service status
  wor service logs <domain>/<service> [--lines=100]

  wor host add <host> [--target=<domain>/<service>] [--server=nginx|apache] [--replace] [--domain-type=local|public] [--add-hosts|--no-hosts]
  wor host remove <host> [--yes]
  wor host list
  wor host test
  wor host reload
  wor host logs <host> [access|error] [--lines=100]

  wor database add <domain>/<profile> [--label="Label"]
  wor database remove <domain>/<profile>
  wor database backup <domain>/<profile>[/database]

  wor source clone <domain> --git=<git-url> [--replace]
  wor source clone <domain>/<service> --git=<git-url> [--replace]
  wor source pull <domain>
  wor source pull <domain>/<service>
  wor source backup <domain> [--gitignore=enable|disable]
  wor source backup <domain>/<service> [--gitignore=enable|disable]

  wor deploy <host|domain/service> [--pull-only] [--no-pull] [--no-restart] [--force]
  wor ssl issue <host> [--provider=letsencrypt|self-signed|custom|none]
  wor ssl renew <host>
  wor ssl status <host>
  wor ssl remove <host>
  wor ssl install <host> --cert=/path/fullchain.pem --key=/path/privkey.pem
  wor info <host|domain/service>

Environment:
  WOR_ENV=%s
  WOR_HOME=%s
  Config=%s
`, ProductName, Version, a.Cfg.Env, a.Cfg.WorHome, a.Cfg.ConfigFile)
}
