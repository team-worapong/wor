# WOR

WOR is a Runtime Manager for Web Applications.

WOR is not a web framework, package manager, container platform, or process
manager. Its role is to orchestrate runtime and infrastructure capabilities
through clear interfaces over time.

Phase 1 contains a clean Go project foundation, read-only diagnostics, and an
explicit setup wizard for user configuration. It does not manage runtimes,
deployment, SSL, or web servers yet.

Future integrations such as runtime management, deployment, SSL, and web server
management must be exposed through explicit commands and implemented behind
provider abstractions.

`wor setup` is a re-runnable setup wizard. It performs read-only environment
detection, shows a summary, and writes WOR user configuration only after explicit
confirmation. It does not install packages, edit system configuration, reload
services, choose a service-specific runtime, or create an initial website.

Configuration is resolved through the central config package. The priority is
explicit options, environment variables, user configuration file, then default
values. The user configuration file records the selected `WOR_HOME`, and
environment variables can override it.

## Commands

```sh
wor version
wor help
wor env
wor doctor
wor setup
wor domain add <domain>
wor service add <fqdn>
```

## Development

```sh
go test ./...
go build ./cmd/wor
```

The current diagnostics are read-only. `wor setup` can write the WOR user
configuration file and create the centralized Phase 1 directory layout under the
selected `WOR_HOME` after confirmation. It does not install programs, edit
system files, deploy applications, manage runtimes, configure web servers, or
manage SSL certificates.

Service templates are currently a metadata registry for future service creation.
Runtime-specific validation belongs to future service creation workflows, not
`wor setup`.

`wor domain add` and `wor service add` create local WOR metadata under
`WOR_HOME` only. They do not edit hosts files, generate web server
configuration, enable sites, install packages, or start/reload services.
