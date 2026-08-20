# WOR HCP 0.1.3

WOR HCP is a web controller panel for WOR Host.

## Website layout

Copy this entire directory to `/download/hcp/` on `wor.worapong.com`:

```text
hcp/
├── releases/
├── checksums.txt
├── installer-hcp.sh
├── latest.txt
└── README.md
```

## Install the latest release

Go to any existing WOR Go service. The service does not need to be named `wor-hcp`.

```bash
wor goto example/admin-panel
curl -fsSL https://wor.worapong.com/download/hcp/installer-hcp.sh | bash
```

The installer detects `domain/service` from the current directory and reads the configured Go `entryPoint` automatically.

## Install a specific version

```bash
curl -fsSL https://wor.worapong.com/download/hcp/installer-hcp.sh | bash -s -- v0.1.3
```

## Install to an explicit target

```bash
curl -fsSL https://wor.worapong.com/download/hcp/installer-hcp.sh | bash -s -- --target=example/admin-panel
```

## Preview without installing

```bash
curl -fsSL https://wor.worapong.com/download/hcp/installer-hcp.sh | bash -s -- v0.1.3 --target=example/admin-panel --dry-run
```

## First administrator

After the first installation, create the administrator from the service directory:

```bash
./app admin create
```

If the service uses a custom `entryPoint`, replace `./app` with that configured path.

## Reset an administrator password

```bash
./app admin reset-password <username>
```

## Safety

The installer verifies SHA-256 checksums, backs up the existing binary and `public/`, preserves the WOR HCP data directory, and rolls back when the service cannot restart.

By default, SQLite and the encryption key are stored in `.wor-hcp/` inside the service directory. Set `WOR_HCP_DATA_DIR` only when a different persistent location is required.

Run all administrator CLI commands from the WOR service directory so the CLI and systemd use the same `.wor-hcp/` database.

When WOR needs elevated privileges to restart a Linux service, the installer reads the confirmation and sudo password directly from the terminal. This also works when the installer itself is piped from `curl`.
