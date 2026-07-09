# WOR Runtime Manager

[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go)](...)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![Platform](https://img.shields.io/badge/platform-Linux%20%7C%20macOS%20%7C%20Windows-success)](...)

WOR is a cross-platform Runtime Manager that deploys, manages and diagnoses modern web services with a consistent workflow. Cross-platform (Linux / macOS / Windows), an Infrastructure & Operations tool for managing Node.js/PHP services, static sites, nginx/apache host configuration, SSL certificates, and database backups under one filesystem convention.

This is a from-scratch Go port of the original bash CLI. The command
surface (subcommands, flags, directory layout) is kept as close to the
original as possible; a few implementation details changed on purpose
to make cross-platform support real rather than aspirational -- see
`DESIGN.md` for the full list and reasoning.

## Status

`go build ./...`, `go vet ./...`, and `go test ./...` all pass, and the
cross-compile targets listed below build cleanly. Most of this project
was written and reviewed without access to a Go toolchain in the
authoring environment; the first real build/test/vet pass on the
user's machine has since happened and the handful of issues it turned
up have been fixed.

## Build

Requires Go 1.21+. No external dependencies (standard library only),
so there is no `go mod download` step.

```bash
go build -o wor ./cmd/wor
```

Cross-compile for other platforms from any machine with Go installed:

```bash
GOOS=linux   GOARCH=amd64 go build -o dist/bin/wor-linux-amd64     ./cmd/wor
GOOS=linux   GOARCH=arm64 go build -o dist/bin/wor-linux-arm64     ./cmd/wor
GOOS=darwin  GOARCH=arm64 go build -o dist/bin/wor-macos-arm64     ./cmd/wor
GOOS=darwin  GOARCH=amd64 go build -o dist/bin/wor-macos-amd64     ./cmd/wor
GOOS=windows GOARCH=amd64 go build -o dist/bin/wor-windows-amd64.exe ./cmd/wor
```

(`scripts/build.sh --release` does this for you and also packages a distributable zip via `scripts/release.sh` -- raw binaries land in `dist/bin/`, packaged zips in `dist/release/`.)

## Install on a server

Browse all downloadable files and release versions at:
<https://wor.worapong.com/download>

One-liner -- downloads the latest release and hands off to its bundled
`install.sh` (which will sudo itself):

```bash
curl -fsSL https://wor.worapong.com/download/installer.sh | bash
```

Install a specific version (any release tag listed on the download
page, including beta builds):

```bash
curl -fsSL https://wor.worapong.com/download/installer.sh | bash -s -- v1.0.0-b31
```

Or manually -- download a release archive yourself (both `.tar.gz` and
`.zip` contain the same files), extract, and run the installer. The
folder inside the archive is always `wor-runtime-manager/`, regardless
of version:

```bash
curl -fsSL https://wor.worapong.com/download/releases/v1.0.0-b31.tar.gz -o wor.tar.gz
tar -xzf wor.tar.gz          # or: unzip wor-runtime-manager-<version>.zip
cd wor-runtime-manager
sudo ./install.sh
```

`install.sh` auto-detects the distro (Debian/Ubuntu only for now),
reports which runtime packages are already installed, asks before
installing only the missing ones (it never upgrades or removes
anything already present), copies the matching `bin/wor-linux-<arch>`
into `/usr/local/bin`, and offers to enable the `wor goto` shell
integration in your rc file. Afterwards, as the non-root operator
user, run in order: `wor version` → `wor doctor` → `wor setup`.

## Commands

The wor-cli v1 surface, plus commands added in the Go rewrite (`run`,
`health`, `diagnose`, `rollback`, `path`/`goto`, `shell-init`). See
`docs/commands.md` for the full reference; `internal/cliapp/usage.go`
is the source of truth:

```text
wor version / --version
wor setup
wor doctor
wor env
wor clean
wor reset
wor create [host]
    (interactive only -- prompts for service type, domain id
    override, domain type, and hosts entry; accepts no other flags)

wor domain add|remove <domain-id>

wor path [.|./<path>|<domain>[/<service>]]
    (prints the directory: "." = WOR_HOME, "./logs" = WOR_HOME/logs,
    no argument = numbered picker of WOR_HOME + every domain/service)
wor shell-init
    (prints a shell function; eval "$(wor shell-init)" in
    ~/.bashrc/~/.zshrc enables `wor goto <target>` = cd there)

wor service add <domain>/<service> [--host=] [--port=] [--entry=] [--service-type=static|node|go|python|php] [--php-version=] [--no-php-pool] [--no-start]
wor service remove <domain>/<service> [--cascade] [--yes]
wor service start|stop|restart <domain>/<service>
wor service status
wor service logs <domain>/<service> [--lines=100]

wor run
    (ensure every enabled service and its runtimes are up)

wor host add <host> [--target=] [--server=nginx|apache] [--replace] [--domain-type=] [--add-hosts|--no-hosts]
wor host remove <host> [--yes]
wor host list / test / reload
wor host logs <host> [access|error] [--lines=100]

wor database add <domain>/<profile> [--label=]
wor database remove <domain>/<profile>
wor database backup <domain>/<profile>[/database]

wor source clone <domain> <git-url>
wor source clone <domain>/<service> <git-url>
wor source pull <domain[/service]> [--stash]
wor source backup <domain[/service]> [--gitignore=enable|disable]

wor deploy <host|domain/service> [--pull-only] [--no-pull] [--no-restart] [--force] [--stash]
wor rollback <domain>/<service> [--yes]

wor ssl issue <host> [--provider=letsencrypt|self-signed|custom|none]
wor ssl renew|status|remove <host>
wor ssl install <host> --cert=<path> --key=<path>

wor info <host|domain/service>
wor health
    (fleet-wide sweep: is every enabled service actually serving?)
wor diagnose <host|domain/service>
    (read-only root-cause analysis for one broken target)
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

## Official Builds

Official releases are published at:

- https://wor.worapong.com
- https://github.com/team-worapong/wor

Modified builds should clearly identify themselves as modified and must not imply they are official WOR releases.
Proudly developed in Thailand 🇹🇭

## Maintainer

**Worapong Sriwichian**

Team (^_^)!

Creator and maintainer of WOR Runtime Manager.
- Website: https://www.worapong.com
- GitHub: https://github.com/team-worapong

Copyright © 2026 Worapong Sriwichian.
Licensed under the Apache License 2.0.
