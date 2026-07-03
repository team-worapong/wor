# WOR Phase 1 Code Layout

The root `ARCHITECTURE.md` file is the primary architecture specification for
WOR. This document is a supporting note for the current Phase 1 code layout and
must not be treated as a second architecture specification.

WOR is a runtime manager for web applications. It is not a web framework,
package manager, container platform, or process manager.

Phase 1 contains only the project foundation and read-only inspection commands.
Runtime management, service management, domain management, SSL management,
deployment, monitoring, and web server integrations are future work described by
the root architecture specification.

Future integrations must be exposed through explicit commands and implemented
behind provider abstractions. Diagnostics must remain read-only and must not
install packages, mutate infrastructure, or apply implicit system changes.

## Layout

- `cmd/wor`: CLI entry point and command routing.
- `internal/cli`: CLI adapter for command parsing and terminal rendering.
- `internal/engine`: current use-case orchestration for Phase 1 commands.
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
case, and renders the response. Business and domain logic must not live in the
command layer. Current Phase 1 reusable workflows live under `internal/engine`
and focused service packages such as `internal/doctor`, `internal/runtime`, and
`internal/config`.

This keeps the same use cases reusable from future surfaces such as REST API,
web admin, desktop application, or background worker without duplicating command
logic.

## Provider Boundary

Provider integrations are not implemented in Phase 1. When they are added, they
should follow the root architecture specification: runtime, process, and web
server integrations should sit behind provider abstractions instead of being
hard-coded into commands.

## Read-only Foundation

The current commands do not create directories, install packages, modify system
configuration, manage web servers, manage SSL certificates, deploy applications,
or start runtimes. This describes the Phase 1 implementation, not a permanent
limit on future WOR capabilities.

`wor doctor` checks availability and version metadata only.
