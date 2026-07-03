# WOR Architecture

WOR is a runtime manager for web applications. It is not a web framework,
package manager, container platform, deployment platform, SSL manager, or web
server manager.

Phase 1 contains only the project foundation and read-only inspection commands.

## Layout

- `cmd/wor`: CLI entry point and command routing.
- `internal/cli`: CLI adapter for command parsing and terminal rendering.
- `internal/engine`: reusable application use cases and service orchestration.
- `internal/config`: default, file, and environment configuration loading.
- `internal/platform`: operating system and architecture boundary.
- `internal/paths`: user-level path resolution.
- `internal/runtime`: runtime prerequisite detection.
- `internal/doctor`: doctor workflow orchestration.
- `internal/output`: centralized output rendering.
- `internal/version`: build and version metadata.
- `pkg`: reserved for future public APIs.
- `docs`: project documentation.
- `scripts`: local development scripts.
- `test`: future integration and fixture space.

## Platform Boundary

Code outside `internal/platform` should not branch on `GOOS` directly. Platform
differences such as user data directories, config directories, executable
lookup, and command execution belong behind the platform layer.

## Command Boundary

Commands are adapters only. The command layer receives input, selects the use
case, and renders the response. Business logic belongs in `internal/engine` or
focused service packages such as `internal/doctor`, `internal/runtime`, and
`internal/config`.

This keeps the same use cases reusable from future surfaces such as a REST API,
web admin, desktop application, or background worker without duplicating logic.

## Read-only Foundation

The current commands do not create directories, install packages, modify system
configuration, manage web servers, manage SSL certificates, deploy applications,
or start runtimes.

`wor doctor` checks availability and version metadata only.
