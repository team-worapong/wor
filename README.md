# WOR

WOR is a Runtime Manager for Web Applications.

WOR is not a web framework, package manager, container platform, deployment
platform, SSL manager, or web server manager. Its role is to provide a command
line foundation for managing application runtimes and environments over time.

Phase 1 contains only a clean Go project foundation and read-only commands.

## Commands

```sh
wor version
wor help
wor env
wor doctor
```

## Development

```sh
go test ./...
go build ./cmd/wor
```

The current implementation is read-only. It does not install programs, edit
system files, deploy applications, manage runtimes, configure web servers, or
manage SSL certificates.
