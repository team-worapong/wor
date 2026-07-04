# WOR Configuration

This document describes the current Phase 1 configuration implementation. The
root architecture specification defines the long-term configuration model.

Phase 1 resolves configuration through `internal/config`. Callers should use
`config.Load`, `config.LoadWithOptions`, or `config.Defaults` instead of reading
environment variables, files, or platform paths directly.

The implemented priority is:

1. Explicit options passed to `config.LoadWithOptions`.
2. Environment variables.
3. User configuration file.
4. Default values.

Higher-priority sources in this list override lower-priority sources.
`WOR_CONFIG` is an environment variable that selects which user configuration
file is read. If an explicit config file path is passed to
`config.LoadWithOptions`, it takes precedence over `WOR_CONFIG`.

The root architecture specification lists command flags as the highest
precedence source. Phase 1 exposes this in the config API as explicit options;
the CLI does not add new flags in this phase.

Default values come from the platform-aware path resolver plus `text` output and
`debug` set to `false`.

## Configuration File

By default WOR looks for `config.json` in the user configuration directory for
the current operating system. `wor setup` writes this user configuration file
only after showing a summary and receiving user confirmation. The file points to
the selected `WOR_HOME` through the `wor_home` field.

Example:

```json
{
  "environment": "development",
  "wor_home": "/path/to/wor",
  "data_dir": "/path/to/wor/data",
  "cache_dir": "/path/to/wor/cache",
  "output_format": "text",
  "debug": false
}
```

Missing files are ignored. Invalid JSON returns an error. Phase 1 supports only
text output.

Empty configuration files are ignored.

Supported fields are:

- `environment`
- `wor_home`
- `data_dir`
- `cache_dir`
- `output_format`
- `debug`
- `web_server_provider`
- `ssl_provider`
- `runtime_detections`

Existing files that still use `home_dir` are accepted as a legacy alias for
`wor_home`. New files written by WOR use `wor_home`.

`wor setup --dry-run` does not write the configuration file or create
directories.

`wor setup` does not choose a runtime for future services. Runtime-specific
template validation belongs to future explicit service creation workflows, not
global WOR configuration.

## Environment Variables

- `WOR_CONFIG`: Override the configuration file path.
- `WOR_ENVIRONMENT`: Override the environment name.
- `WOR_HOME`: Override the WOR home directory.
- `WOR_DATA_DIR`: Override the data directory.
- `WOR_CACHE_DIR`: Override the cache directory.
- `WOR_OUTPUT`: Output format. Phase 1 supports `text`.
- `WOR_DEBUG`: Boolean debug flag. Accepted true values are `1`, `true`, `yes`,
  `y`, and `on`. Accepted false values are `0`, `false`, `no`, `n`, and `off`.

Environment variables override values from the user configuration file.

## WOR_HOME Layout

The directory layout under `WOR_HOME` is defined centrally by
`config.Layout(cfg)` and `config.LayoutForHome(path)`.

Current Phase 1 directories are:

- `domains`
- `runtime`
- `templates`
- `logs`
- `ssl`
- `configs`
- `cache`
- `data`
- `backups`

`wor setup` uses this layout when creating directories. Phase 1 does not install
packages, modify system configuration, start services, or manage runtimes.

## Domain And Service Metadata

`wor domain add <domain>` stores domain metadata in:

```text
WOR_HOME/domains/<domain_id>/domain.json
```

`domain_id` is created from the full domain by reversing all labels and joining
them with `-`. For example, `example.co.th` becomes `th-co-example`.

`wor service add <fqdn>` reads the domain catalog from `domain.json` files,
selects the longest matching domain, and stores service metadata in:

```text
WOR_HOME/domains/<domain_id>/<service_id>/service.json
```

Every service foundation also creates a `public/` directory. Runtime
requirements are validated from the selected service template before service
directories or metadata are created.
