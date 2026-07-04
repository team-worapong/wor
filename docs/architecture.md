# WOR Phase 1 Code Layout

The root `ARCHITECTURE.md` file is the primary architecture specification for
WOR. This document is a supporting note for the current Phase 1 code layout and
must not be treated as a second architecture specification.

WOR is a runtime manager for web applications. It is not a web framework,
package manager, container platform, or process manager.

Phase 1 contains the project foundation, read-only inspection commands, and an
explicit setup wizard for user configuration. Runtime management, service
management, domain management, SSL management, deployment, monitoring, and web
server integrations are future work described by the root architecture
specification.

Future integrations must be exposed through explicit commands and implemented
behind provider abstractions. Diagnostics must remain read-only and must not
install packages, mutate infrastructure, or apply implicit system changes.

## Layout

- `cmd/wor`: CLI entry point and command routing.
- `internal/cli`: CLI adapter for command parsing and terminal rendering.
- `internal/engine`: current use-case orchestration for Phase 1 commands.
- `internal/config`: centralized configuration loading, explicit option
  overrides, and `WOR_HOME` layout helpers.
- `internal/domain`: domain metadata, domain IDs, and the domain catalog stored
  under `WOR_HOME/domains`.
- `internal/platform`: operating system and architecture boundary.
- `internal/paths`: user-level path resolution.
- `internal/runtime`: runtime prerequisite detection.
- `internal/doctor`: doctor workflow orchestration.
- `internal/service`: service-domain metadata such as the Phase 1 service
  template registry.
- `internal/setup`: re-runnable setup wizard workflow and explicit apply plan.
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

## Service Template Registry

Phase 1 includes a service template registry as metadata. It defines names,
runtime requirements, process requirements, public-directory expectations, and
route models for service creation workflows.

`wor setup` records WOR configuration and provider defaults only. It does not
choose a runtime for future services. Runtime-specific validation should happen
when a future explicit service creation command asks to create a service from a
template.

## Domain And Service Foundation

`wor domain add` creates a domain catalog entry under `WOR_HOME/domains`.
Domain IDs are derived from the full domain name by reversing all labels and
joining them with `-`, so multi-level domains do not depend on a two-label root
assumption.

`wor service add` uses the domain catalog metadata and chooses the longest
matching domain. Service IDs are derived from the subdomain labels before the
matched domain. Service creation validates runtime requirements from the service
template registry before creating any service directory or metadata.

This foundation writes only WOR-owned metadata and directories. It does not edit
hosts files, create web server config, enable sites, install packages, or
start/stop/reload services.

## Read-only Foundation

Most current commands are read-only diagnostics. `wor setup` is the only Phase 1
command that can write project state, and it does so only after explicit user
confirmation. Its writes are limited to the WOR user configuration file and the
centralized directory layout under the selected `WOR_HOME`.

The current implementation does not install packages, modify system
configuration, manage web servers, manage SSL certificates, deploy applications,
or start runtimes. This describes the Phase 1 implementation, not a permanent
limit on future WOR capabilities.

`wor doctor` checks availability and version metadata only.
`wor setup --dry-run` shows the planned setup without writing files or creating
directories.
