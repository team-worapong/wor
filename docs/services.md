Service Runtime Template

Cross-platform note: systemd exists on Linux only. On macOS and Windows,
the `go` and `python` templates below fall back to PM2 (the same provider
`node` always uses) instead of systemd, so neither platform is left
without a way to run services -- see DESIGN.md section 6. The `wor doctor`
command reports which process provider is active on the current machine.

- static
    Runtime: none
    Process Provider: none
    Web Server: serves the public/ folder directly

- node.js
    Runtime: Node.js
    Process Provider: PM2
    Entry Point: app.js (default)
    Customizable: yes
    Runtime check:
      - shows the installed Node.js version
      - if not installed: Not Supported

- go
    Runtime: Go
    Process Provider: systemd (Linux) / PM2 (macOS, Windows)
    Entry Point: app [the built binary] (default)
    Customizable: yes
    Runtime check:
      - shows the installed Go version
      - if not installed: Not Supported

- python
    Runtime: Python
    Process Provider: systemd (Linux) / PM2 (macOS, Windows)
    Entry Point: app.py (default)
    Customizable: yes
    Runtime check:
      - shows the installed Python version
      - if not installed: Not Supported

- php
    Runtime: PHP
    Process Provider: php-fpm
    Service Manager: the system php-fpm master (the original default)
    Entry Point: public/index.php
    Customizable: yes
    Runtime check:
      - shows the installed PHP version
      - shows the installed PHP-FPM version
      - if not installed: Not Supported
    Per-service pool (Linux/macOS only, see DESIGN.md section 8):
      - each php service automatically gets its own php-fpm pool (its own
        dedicated socket, its own selectable PHP-FPM version) when the
        machine detects exactly one PHP-FPM version
        (`/etc/php/<version>/fpm` on Linux; Homebrew on macOS -- both the
        versioned `php@<version>` formulas and the plain `php` formula,
        which is the latest version with no version in its name).
        `--php-version=<version>` selects the version when several are
        detected at once, and `--no-php-pool` falls back to the old
        PHP_FPM_ENDPOINT (shared host-wide).
      - **Pool ownership (unix user) differs per OS**: on Linux (php-fpm
        master runs as root via systemd) each pool gets its own dedicated
        unix user (created via `useradd --system --no-create-home`), fully
        isolating services from each other. But on **macOS (Homebrew),
        pools no longer get separate unix users**, because the php-fpm
        master run via `brew services` is an unprivileged process (running
        as the normal login user, not root) and therefore has no rights to
        chown the socket or switch workers to another user -- every pool
        on macOS runs as the same login user that runs the php-fpm master.
        There is no privilege separation between services on macOS.
        (Found and decided 2026-07-05 after hitting a real error on a
        machine in active use.)
      - php services that existed before this feature are not migrated
        automatically -- they keep using the shared PHP_FPM_ENDPOINT until
        recreated with their own dedicated pool.
      - Windows always uses the shared PHP_FPM_ENDPOINT -- PHP-FPM has no
        official Windows build, so there is no local pool for wor to
        manage.
