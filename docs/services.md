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
    PATH_INFO routing:
      - the generated nginx/apache config supports front-controller URLs
        of the form `/index.php/controller/action` (PATH_INFO). nginx
        matches `\.php(/|$)`, splits the script from the path with
        `fastcgi_split_path_info`, and passes `PATH_INFO`; apache sets
        `AcceptPathInfo On`. This fixes a redirect loop that framework
        routers hit when PATH_INFO was missing. See DESIGN.md section 16.

Custom web-server config per service

Every host wor generates includes any `*.conf` file a user drops into the
service's own custom-config directory:

    WOR_HOME/domains/<domain>/<service>/.wor/<nginx|apache>/*.conf

- nginx snippets are included inside the service's `server { }` block;
  apache snippets inside its `<VirtualHost>` -- in both cases right after
  wor's own generated directives.
- **Which block they land in depends on the HTTPS redirect.** nginx now
  generates a `:80` and a `:443` server block, matching apache's two
  VirtualHosts. With the redirect off, both blocks serve and the snippets
  are included in both. With the redirect on, the `:80` block does
  nothing but answer ACME challenges and redirect, so the snippets are
  included in the `:443` block only -- a snippet in a redirect-only block
  could never run. Set the redirect with
  `wor ssl redirect <host> on|off`.
- The include is always present in the generated vhost and uses a
  wildcard (`include`/`IncludeOptional`), so an empty or missing
  directory is never an error: drop a file in any time and run
  `wor host reload`.
- You may ADD directives/locations. On nginx you may NOT redefine a
  `location` wor already emits (e.g. `location /`, `location ~ \.php`) --
  nginx rejects duplicate locations on reload. To change wor's core
  routing itself, edit nginx's main config directly.
- wor writes a non-loaded reference file, `default.conf.example`, into
  that directory. It shows wor's current default config for the service
  plus these rules. It does not end in `.conf`, so it is never loaded,
  and wor regenerates it on each host write -- do not edit it in place;
  copy it to a new `*.conf` file to build on it.

Custom PHP settings per service

A php service with its own pool configures itself through two optional
files in its own tree, beside the web-server snippets:

    WOR_HOME/domains/<domain>/<service>/.wor/php.ini       PHP ini settings
    WOR_HOME/domains/<domain>/<service>/.wor/php-fpm.ini   pool tuning

- **Two files, because they are two different layers.** `php.ini` holds PHP
  ini settings and becomes `php_value[...]` in the pool, which the
  application can still `ini_set()` past exactly as it could past php.ini.
  `php-fpm.ini` holds php-fpm pool directives -- `pm`, `pm.max_children` and
  friends -- written into the pool as-is; they govern the worker processes,
  and the application can neither see nor change them. One file would mean
  one line silently meaning `php_value` and the next meaning a raw
  directive, decided by a lookup table the reader cannot see.
- **wor reads these files; php does not.** A php-fpm pool cannot include a
  php.ini of its own -- the only per-pool ini mechanism is the
  `php_value` / `php_admin_value` family inside the pool's `*.conf`, and
  `env[PHP_INI_SCAN_DIR]` cannot substitute for it, because the master
  parses php.ini once at startup and every worker is a fork of that. So wor
  parses these files and renders directives into the service's pool config,
  which `php-fpm -t` then validates before anything is reloaded. A bad
  value is rolled back to the previous pool config, so it can never take
  down the other pools sharing that master.
- Each file is a plain `key = value` list; `;` and `#` start a comment.
  `[sections]` are not supported -- each file already applies to exactly one
  service.
- **Only an allowlist of keys is accepted in each**, and a key outside it
  fails rather than being skipped, so a setting is never silently dropped.
  wor lists the current set in the `.example` file it writes beside each one
  and in its own error messages.
- Some keys are refused on purpose and say why. In `php.ini`: `error_log`
  (the php-fpm master opens it as root), `extension`/`zend_extension` and
  `sendmail_path` (they name code to run), `open_basedir` and
  `disable_functions` (host-level containment a service must not be able to
  widen), and `opcache.*` (allocated once by the master, so a per-pool value
  is ignored rather than applied). In `php-fpm.ini`: `user`, `group`,
  `listen` and the socket ownership, which define the pool's identity -- a
  service able to set them could run as another service's account or take
  over another service's socket -- plus the log paths the root master opens.
- The few keys PHP classifies as PHP_INI_SYSTEM (e.g. `max_file_uploads`)
  become `php_admin_value[...]`, because `php_value` cannot set those at all.
- **Pool tuning replaces wor's defaults rather than being appended to them.**
  Without a `php-fpm.ini` a pool gets `pm = dynamic` with
  `pm.max_children = 5`; setting `pm = static` or `pm = ondemand` switches
  the block to the directives that mode actually uses. The pool file
  therefore lists each directive exactly once, with the value in force.
- **`wor deploy` applies both files.** They are validated before the deploy
  touches the tree, and the pool is re-rendered after the build. An invalid
  file fails the deploy instead of shipping code against settings that could
  not be applied. When the rendered pool would be byte-for-byte what is
  already on disk, the write, the config test and the php-fpm reload are all
  skipped -- reloading the shared master cycles the workers of every other
  service under it, which no code-only deploy should be doing.
- **`wor service reload <domain>/<service>` applies them on their own**, for
  a settings-only change: it re-renders the pool, validates, reloads php-fpm
  and prints what is now in force -- without reinstalling dependencies,
  rebuilding or restarting anything, and without the skip above, because
  somebody asking for a reload means it. It is also the only way to apply
  these files to a service whose source is not a git repository, since
  `wor deploy` requires one. Config only: the process is
  `wor service restart`, the vhost is `wor host reload`.
- **`wor info` shows the settings and flags drift**; `wor diagnose` warns
  about the same drift and about a file that no longer parses; `wor health`
  reports drift across the whole fleet, so a file edited and never applied
  is found without having to suspect that one service first. Drift means the
  pool file on disk differs from what these files render to -- which also
  catches a pool config edited by hand. All three are read-only and never
  elevate: a pool file they cannot read is reported as unchecked rather than
  assumed correct.
- **Only a php service with its own pool reads these files.** A service on
  the shared host-wide `PHP_FPM_ENDPOINT` (`--no-php-pool`, a service
  created before per-service pools, or Windows) has no pool to configure,
  and a node/go/python/static service has nothing to do with PHP at all. In
  both cases wor warns on deploy that the files are not applied to anything,
  and does not even parse them -- an invalid file under a service that would
  never read it fails nothing, because there was no setting to drop.
  (`.wor/nginx` and `.wor/apache` are unaffected: those work for every
  service type.)
- wor writes a non-loaded reference file beside each one -- `php.ini.example`
  and `php-fpm.ini.example` -- regenerated whenever the pool is rendered; do
  not edit them in place.
- Both live in `.wor`, above the document root, so the web server never
  serves them.

TLS certificate files

Whatever the SSL provider, the generated vhost points at wor's own copy:

    WOR_HOME/ssl/hosts/<host>/fullchain.pem
    WOR_HOME/ssl/hosts/<host>/privkey.pem

mode 0600, owned by whoever owns WOR_HOME. This is what lets one
ownership model work on both platforms without any ACL: the web server's
master process is what reads a certificate, and that is root on Linux
and the login user (the operator) on macOS.

Let's Encrypt certificates are copied here from certbot's store rather
than referenced in place, because /etc/letsencrypt/archive is root-only
and an unprivileged master -- Homebrew's nginx on macOS -- cannot read
it at all. `wor ssl sync <host>` refreshes the copy and runs
automatically as certbot's renewal hook. See docs/commands.md and
DESIGN.md section 21.
