# WOR Configuration

WOR resolves configuration in this order:

1. Default values.
2. User configuration file.
3. Environment variables.

Later sources override earlier sources.

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

## Environment Variables

- `WOR_CONFIG`: Override the configuration file path.
- `WOR_HOME`: Override the WOR home directory.
- `WOR_DATA_DIR`: Override the data directory.
- `WOR_CACHE_DIR`: Override the cache directory.
- `WOR_OUTPUT`: Output format. Phase 1 supports `text`.
- `WOR_DEBUG`: Boolean debug flag. Accepted true values are `1`, `true`, `yes`,
  `y`, and `on`. Accepted false values are `0`, `false`, `no`, `n`, and `off`.
