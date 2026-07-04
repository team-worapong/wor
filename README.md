# wor (Go rewrite)

Cross-platform (Linux / macOS / Windows) rewrite of `wor-cli`, an
Infrastructure & Operations tool for managing Node.js/PHP services,
static sites, nginx/apache host configuration, SSL certificates, and
database backups under one filesystem convention.

This is a from-scratch Go port of the original bash CLI. The command
surface (subcommands, flags, directory layout) is kept as close to the
original as possible; a few implementation details changed on purpose
to make cross-platform support real rather than aspirational -- see
`DESIGN.md` for the full list and reasoning.

## Status

This code was written and reviewed without access to a Go toolchain or
internet connectivity in the authoring environment, so it has **not
been compiled or run yet**. Every file was written carefully and
cross-checked by hand (import usage, function signatures, struct
fields), but a project this size will likely have a handful of small
compile errors on the first build. Please run:

```bash
cd wor-go
go build ./...
```

and fix/report anything that comes up -- none of it should be
architectural, just the kind of typo `go build` catches in seconds.

## Build

Requires Go 1.21+. No external dependencies (standard library only),
so there is no `go mod download` step.

```bash
go build -o wor ./cmd/wor
```

Cross-compile for other platforms from any machine with Go installed:

```bash
GOOS=linux   GOARCH=amd64 go build -o dist/wor-linux-amd64     ./cmd/wor
GOOS=linux   GOARCH=arm64 go build -o dist/wor-linux-arm64     ./cmd/wor
GOOS=darwin  GOARCH=arm64 go build -o dist/wor-macos-arm64     ./cmd/wor
GOOS=darwin  GOARCH=amd64 go build -o dist/wor-macos-amd64     ./cmd/wor
GOOS=windows GOARCH=amd64 go build -o dist/wor-windows-amd64.exe ./cmd/wor
```

## Commands

Same surface as wor-cli v1:

```text
wor version / --version
wor setup
wor doctor
wor env
wor clean
wor reset [--yes]
wor create [host]
    (interactive only -- prompts for service type, domain id
    override, domain type, and hosts entry; accepts no other flags)

wor domain add|remove <domain-id>

wor service add <domain>/<service> [--host=] [--port=] [--entry=] [--service-type=static|node|go|python|php]
wor service remove <domain>/<service> [--cascade] [--yes]
wor service start|stop|restart <domain>/<service>
wor service status
wor service logs <domain>/<service> [--lines=100]

wor host add <host> [--target=] [--server=nginx|apache] [--replace] [--domain-type=] [--add-hosts|--no-hosts]
wor host remove <host> [--yes]
wor host list / test / reload
wor host logs <host> [access|error] [--lines=100]

wor database add <domain>/<profile> [--label=]
wor database remove <domain>/<profile>
wor database backup <domain>/<profile>[/database]

wor source clone <domain[/service]> --git=<url> [--replace]
wor source pull <domain[/service]>
wor source backup <domain[/service]>

wor deploy <host|domain/service> [--pull-only] [--no-pull] [--no-restart] [--force]

wor ssl issue <host> [--provider=letsencrypt|self-signed|custom|none]
wor ssl renew|status|remove <host>
wor ssl install <host> --cert=<path> --key=<path>

wor info <host|domain/service>
```

## Service templates

Five service types, chosen via `wor create`'s wizard or `--service-type=`
on `wor service add` (see `docs/services.md` for the full spec):

| Template | Runtime  | Process provider                 | Default entry point   |
|----------|----------|-----------------------------------|-----------------------|
| static   | none     | none (web server serves `public/`) | --                    |
| node     | Node.js  | PM2 (every OS)                    | `app.js`              |
| go       | Go       | systemd (Linux) / PM2 (else)      | `app` (compiled binary) |
| python   | Python   | systemd (Linux) / PM2 (else)      | `app.py`               |
| php      | PHP-FPM  | PHP-FPM (assumed already running) | `public/index.php`    |

`wor create`/`wor service add` hard-block creating a service whose
runtime isn't installed (`wor doctor` shows what's missing) rather than
offering to configure it interactively. `go` services are rebuilt
(`go build`) at creation and on every `wor deploy` that pulled a new
commit -- unlike node, this isn't conditional on which files changed.

## Project layout

```text
cmd/wor/                  entrypoint
internal/
  osutil/                 OS detection, elevation, privileged file ops (build-tagged unix/windows)
  config/                 WOR_HOME + ~/.wor/config + host.env resolution
  domainmodel/            services.config.json / databases.config.json / backup.config.json + resolution logic
  hostprovider/           nginx + apache: paths, vhost generation, enable/reload/test
  templates/              embedded nginx/apache vhost templates (unchanged from wor-cli v1)
  render/                 {{VAR}} template substitution
  servicefiles/           starter source tree generator (static/node/go/python/php scaffolds)
  pm2/                    PM2 process manager wrapper + JSON ecosystem file generator
  systemd/                systemd unit generator + start/stop/restart/status/logs (Linux go/python services)
  ssl/                    letsencrypt/self-signed/custom certificate management
  dbbackup/               mysql/mariadb/postgresql/sqlserver/sqlite backup (Go-native gzip, no restore)
  hostsfile/              WOR-managed block in /etc/hosts or the Windows hosts file
  cliapp/                 every subcommand, wired together
```
