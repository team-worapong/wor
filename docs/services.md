Services Runtime Template

Cross-platform note: systemd only exists on Linux. On macOS and
Windows, the `go` and `python` templates below fall back to PM2 (the
same process provider `node` always uses) instead of systemd, so
neither platform is left without a way to run these services -- see
DESIGN.md section 6. `wor doctor` reports which provider is active on
the current OS.

- static
    Runtime: None
    Process Provider: None
    Web Server: Serve public/ directly

- node.js
    Runtime: Node.js
    Process Provider: PM2
    Entry Point: app.js (default)
    Configurable: Yes
    Runtime Check:
      - Display installed Node.js version
      - If not installed: Not Supported

- go
    Runtime: Go
    Process Provider: systemd (Linux) / PM2 (macOS, Windows)
    Entry Point: app [executable binary] (default)
    Configurable: Yes
    Runtime Check:
      - Display installed Go version
      - If not installed: Not Supported

- python
    Runtime: Python
    Process Provider: systemd (Linux) / PM2 (macOS, Windows)
    Entry Point: app.py (default)
    Configurable: Yes
    Runtime Check:
      - Display installed Python version
      - If not installed: Not Supported

- php
    Runtime: PHP
    Process Provider: php-fpm
    Service Manager: default php runtime
    Entry Point: public/index.php
    Configurable: Yes
    Runtime Check:
      - Display installed PHP version
      - Display installed PHP-FPM version
      - If not installed: Not Supported
