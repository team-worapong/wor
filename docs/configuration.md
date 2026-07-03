# WOR Configuration

This document describes the current Phase 1 configuration implementation. The
root architecture specification defines the long-term configuration model.

Phase 1 resolves configuration in this order:

1. Default values.
2. `WOR_CONFIG`, when set, selects the user configuration file path.
3. User configuration file.
4. Environment variables.

Later sources override earlier sources.

The root architecture specification includes command flags as the highest
precedence source. Phase 1 does not implement command flags yet, so the current
implementation uses defaults, a user configuration file, and environment
variables. `WOR_CONFIG` is an environment variable that selects which user
configuration file is read.

Default values come from the platform-aware path resolver plus `text` output and
`debug` set to `false`.

## Configuration File

By default WOR looks for `config.json` in the user configuration directory for
the current operating system.

Example:

```json
{
  "home_dir": "/path/to/wor",
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

- `home_dir`
- `data_dir`
- `cache_dir`
- `output_format`
- `debug`

## Environment Variables

- `WOR_CONFIG`: Override the configuration file path.
- `WOR_HOME`: Override the WOR home directory.
- `WOR_DATA_DIR`: Override the data directory.
- `WOR_CACHE_DIR`: Override the cache directory.
- `WOR_OUTPUT`: Output format. Phase 1 supports `text`.
- `WOR_DEBUG`: Boolean debug flag. Accepted true values are `1`, `true`, `yes`,
  `y`, and `on`. Accepted false values are `0`, `false`, `no`, `n`, and `off`.
