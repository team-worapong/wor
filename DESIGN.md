# Design notes: wor-cli (bash) -> wor (Go)

This document records the deliberate differences from the original shell
CLI, with the reasoning behind each. Anything not mentioned here should
behave exactly as before (same directory conventions, same PM2 naming
`wor_<domain>_<service>`, same host file naming `wor__<host>.conf` /
`000_wor_default.conf`, same template variables).

Sections 1-8 are the original design from porting bash to Go. Section 9
onward covers features/redesigns added later, after the first porting pass
was complete.

## 1. Config files are JSON, not hand-written JS

The shell CLI stored `services.config.js` / `databases.config.js` /
`backup.config.js` as `module.exports = {...}` files, read/written by
shelling out to `node -e '...'`. That made Node.js a hard dependency just
for "managing config" -- even for a pure static site with no Node.js
service at all -- and there is no workable Windows equivalent of that
approach without assuming Node is on PATH before wor can even list
domains.

The Go version stores the same data as `services.config.json` /
`databases.config.json` / `backup.config.json`, read/written with
`encoding/json`. Structure and fields are identical (`domain`,
`services[].name/type/hosts/port/...`); only the file extension changed.
There is no code-generation step anymore. If old `*.config.js` files from
wor-cli v1 exist, they must be converted to `.json` manually (strip the
leading `module.exports = ` and the trailing `;`); this version has no
automatic migration yet.

`wor.config.js` (the generated PM2 ecosystem file) likewise became
`wor.config.json`. PM2 supports `pm2 start ecosystem.json` out of the box,
so nothing is lost by this change.

## 2. No shelling out to gzip/zip/tail/ss/lsof/netstat

The original shell version was built on dozens of small Unix utilities,
each one a point where a Windows port would break. The Go version replaces
them with the standard library:

- database backup compression: `compress/gzip` instead of piping through
  the `gzip` program
- source backup: `archive/zip` instead of shelling out to `zip`
- checking whether a port is free (the auto-port picker of
  `wor service add`): try `net.Listen("tcp", ...)` instead of parsing
  `ss`/`lsof`/`netstat` output
- `wor host logs`: a small hand-written tail-and-follow loop instead of
  `tail -f` (which doesn't exist on Windows)

## 3. Host provider paths differ per OS

nginx/apache have different sites-available/sites-enabled/log directory
conventions per OS:

- Linux: `/etc/nginx/sites-available` + `/etc/nginx/sites-enabled`
  (classic Debian style), `/etc/apache2/sites-available` or
  `/etc/httpd/conf.d`
- macOS (Homebrew): a single flat directory (`servers/` for nginx,
  `servers/` under `httpd` for apache) -- there is no separate "enabled"
  directory, so enabling a host is a no-op as soon as the file is written
- Windows: no widely used standard convention. This version defaults to
  `C:\nginx\conf\sites-available` / `C:\Apache24\conf\sites-available`,
  with the "enabled" directory equal to the "available" directory (the
  same flat-directory model as macOS, avoiding the problem that creating
  symlinks on Windows requires Administrator rights or Developer Mode).
  These are merely reasonable defaults, not universally correct values --
  override via `host.env` (`NGINX_SITES_AVAILABLE=` etc.) to match the
  actual nginx/Apache on that Windows machine.

All of this sits behind a single interface (`internal/hostprovider`); more
accurate Windows defaults can be added later without touching any command
code.

## 4. Privilege elevation

On Unix the original model stands: if not root, wrap privileged operations
(`mkdir`, writes into `/etc/nginx/...`, `tee`, `rm`, `ln`, `systemctl
reload`, `certbot`) with `sudo`. Two additions over the shell version:

- **`wor` refuses to run as `sudo wor ...`.** `osutil.IsSudoElevated()`
  checks both root *and* the presence of the `SUDO_USER` environment
  variable (which `sudo` always sets for its child process, but a direct
  root login does not). `App.Run()` checks this before dispatching to any
  subcommand and errors immediately if found. This check is deliberately
  narrower than "reject if root": a server with no user account other than
  root (logging in and running `wor` directly, never via `sudo`) is
  unaffected, because `SUDO_USER` is not set in that case. PM2 itself
  already refuses to run under sudo (see `internal/pm2`); this closes the
  same hole for every subcommand, so a user can't accidentally end up with
  root-owned git clone/npm install/PM2 dump artifacts just by prefixing
  the whole command with `sudo`.
- **`osutil.SudoCommand` asks for confirmation only the first time (per
  process) it actually needs to add `sudo`.** It never asks up front, and
  never asks again for the rest of the same command. `cliapp.New()` wires
  this mechanism to a `[Y/n]` prompt (`osutil.SetElevationPrompt`). If the
  user declines, every subsequent privileged operation in the same command
  errors immediately without asking again. Environments where the relevant
  paths are already writable without elevation (e.g. Homebrew-installed
  nginx directories on macOS) never see this prompt at all, because the
  unprivileged write succeeds on the first try and never reaches
  `SudoCommand`.

Windows has no mechanism to re-run a command with elevated rights from an
already-running process, so this version does not build a new
UAC-launching flow -- opening an Administrator console remains the only
way to run privileged commands on Windows. `IsSudoElevated()` always
returns `false` on Windows, deliberately, so Windows users are not blocked
the way `sudo wor` is blocked on Unix. `osutil.IsElevated()` checks for an
already-elevated console via `net session` (succeeds only for
Administrator). When a privileged write fails, the error message tells the
user to open a new terminal as Administrator, rather than attempting a
silent auto-elevation via a UAC prompt (which would break anyway).

## 5. SSL: Let's Encrypt is Unix-only

Certbot has no trustworthy official Windows build. `wor ssl issue
--provider=letsencrypt` errors clearly on Windows, pointing to
`self-signed` or `custom` instead of attempting something fragile.
self-signed (via `openssl` if installed) and custom (bring your own
cert/key) work on every OS.

## 6. Service templates: added go/python + systemd (new vs v1)

wor-cli v1 had no `go` or `python` template. This version adds them (see
`docs/services.md`) along with a major cleanup: the 4 mixed templates
(`static-node`, `node-web`, `node-php`, `php-node`) were removed, leaving
just 5: `static`, `node`, `go`, `python`, `php` -- one service is one
runtime kind, not a mix (the cases the mixed templates used to cover are
served better by splitting into a static service and a process-backed
service as separate services under the same domain).

There are now 2 process supervisors:

- **node** always uses PM2 (as in v1), on every OS
- **go** and **python** use **systemd** on Linux (already present on
  virtually every distro, and simpler to reason about than adding a second
  PM2-based process manager), falling back to **PM2** on macOS and
  Windows, which have no systemd. `domainmodel.ProcessProviderFor` is the
  single place that makes this OS-based decision. `internal/systemd`
  mirrors the structure of `internal/pm2` (generate unit,
  start/stop/restart/status/logs, same `wor_<domain>_<service>` naming),
  so the two providers feel nearly identical from the CLI.
- **static** has no process to manage at all
- **php** has no *process* to manage (the php-fpm master is assumed to be
  started as its own system service already), but since the per-service
  php-fpm pool feature (section 8), wor does manage one thing under that
  master: the per-service pool `.conf` files, which wor writes/deletes,
  validates, and reloads php-fpm for itself. wor still never
  starts/stops/restarts the php-fpm master process itself -- it only
  adds/removes pool files under it, just as `wor host reload` only tells
  nginx/apache to reload and never manages them as processes.

Every systemctl/journalctl invocation goes through the same confirm-once
sudo gate described in section 4.

`go` has an extra step that node and python don't: it must build.
`wor service add --service-type=go` and `wor create` run `go build`
immediately after scaffolding, and `wor deploy` re-runs it every time
`git pull` brings in new commits (unconditionally -- not based on a
node-style diff heuristic against package.json, because editing a `.go`
file with no dependency change still requires recompiling).

`wor create` also changed shape in this cleanup: it accepts no `--` flags
at all (only the optional positional host argument), reinforcing the
original intent of being "interactive only". The one flag whose real
capability was removed, `--domain=` (overriding the auto-derived domain
id), became a confirm/override prompt instead of simply disappearing.
Automation still goes through `wor domain/service/host add`, which gained
`--service-type=` (renamed from `--template=` to match the existing
`--domain-type=` and the internal `Service.Type` field name) and a new
`--entry=` flag for overriding the service's entry point file/binary name.

`wor create`/`wor service add` block service creation immediately with a
clear "runtime not found" error if the chosen template's runtime is not
installed -- deliberately no "set it up now?" prompt like some other
wizards in this CLI. `wor doctor` is the single place that reports what's
missing and how to fix it.

## 7. Deliberately not done (same as v1)

- No restore/drop/migrate for databases -- backup only
- `wor create` remains interactive-only; automation goes through
  `wor domain/service/host add`
- Templates cannot be changed after a service is created (immutable)

## 8. Per-service php-fpm pool

Designed and scoped before any code was written (per the project
convention of discussing/confirming design first for changes that affect
architecture). Previously every php service shared one `PHP_FPM_ENDPOINT`
host-wide (a single config value, or a socket auto-detected from a fixed
candidate list -- `internal/hostprovider/phpfpm.go`). From this feature
onward, each php service can have its own pool via `internal/phpfpm`:

- **Isolation**: its own unix socket, its own `pm.*` values, its own
  PHP-FPM version. **Unix user isolation differs per OS** (revised from
  the initial design -- see details below): on Linux each pool gets its
  own dedicated unix user (created via `useradd --system
  --no-create-home`); this user is added to the original owning group of
  the service's document root and given `chmod g+rX` read access -- the
  document root's original owner is never chown'd. On **macOS every pool
  runs as the same user that runs the php-fpm master (no more per-service
  unix users)**.
- **Platform scope**: Linux (the Debian/Ubuntu `/etc/php/<version>/fpm`
  layout) and macOS (Homebrew, both the versioned `php@<version>` formulas
  and the plain `php` formula, which is the current version with no
  separate version name) only. Windows keeps the old behavior (a single
  global TCP endpoint), unchanged -- PHP-FPM has no official Windows
  build, so there is no local pool for wor to manage. RHEL-family Linux
  uses a different package layout than `/etc/php/<version>/fpm` and is not
  yet supported by auto-detect (`phpfpm.DetectVersions`).
- **Lifecycle**: wor writes the pool `.conf` file, validates the resulting
  config with `php-fpm -t` *before* touching anything live, and only then
  reloads php-fpm (`systemctl reload phpX.Y-fpm` on Linux,
  `brew services restart php@X.Y` on macOS -- Homebrew's LaunchAgent
  wrapper has no reload command), and only when validation passes. If
  validation fails, the pool file is rolled back, never leaving a broken
  config behind to trip up the next real reload.
- **Backward compat / no forced migration**:
  `domainmodel.Service.PHPVersion` is empty for every php service that
  predates this feature, and stays empty until a dedicated pool is
  actually created. An empty value means "use the old host-wide shared
  `PHP_FPM_ENDPOINT`" -- host config rendering
  (`cliapp.buildWriteParams`) checks this field directly. New php services
  automatically get a dedicated pool when the machine detects exactly one
  PHP-FPM version; `--php-version=` selects one when several are found,
  and `--no-php-pool` deliberately falls back to the old shared endpoint.

### Design revision 2026-07-05: dropped unix user isolation on macOS

Found through real testing on the user's macOS machine (running `wor run`
against a pre-existing php service): the initial design of "full unix user
isolation on both Linux and macOS" is simply not possible on macOS,
because the php-fpm master run via Homebrew (`brew services start`) runs
as the normal login user, not root, and a non-root process cannot
`chown()` a socket to another unix user or switch workers to run as
another user at all -- attempting it produced the real error
`failed to chown() the socket` the first time a pool was actually used.

Two options were presented to the user (elevate the macOS php-fpm master
to run as root to preserve privilege separation, versus dropping unix user
isolation on macOS only). **The user chose to drop privilege separation on
macOS.** Linux is unaffected (systemd already runs php-fpm as root; the
per-service unix user isolation still works exactly per the original
design).

The result is that every macOS pool now runs as the same login user as the
php-fpm master (`EnsureUser`/`GrantGroupAccess`/`RemoveUser` are never
called on macOS), while Linux keeps the entire original flow
(`internal/cliapp/service.go`, the `setupPHPPool`/`teardownPHPPool`
functions, branching on `osutil.IsMacOS()`).

**Caveat**: this fix applies only to pools created/modified **after** the
fix was deployed (following this feature's existing no-forced-migration
pattern). php services whose pools were created on macOS before the fix
still have the old-style `.conf` files and dedicated unix users lying
around; they do not self-heal. They must be `wor service remove`d and
`wor service add`ed again (back up the source first with
`wor source backup`, because `service remove` deletes the service's
entire directory). There is no lightweight "repair an existing pool in
place" command yet.

It was also discovered that always guessing the Homebrew formula name as
`php@<version>` can be wrong -- some machines install PHP via the plain
`php` formula (no version in the name), where that version happens to be
the latest, with no separate `php@X.Y` keg at all, making both the binary
path guess and the service name wrong (`internal/phpfpm` used to hardcode
`ReloadUnit: "php@" + version`). Fixed by adding
`resolveHomebrewPHPBinary`, which tries the versioned path first and only
falls back to the plain `php` formula when that binary actually confirms
the desired version (checked via `<binary> -v`, not guessed, so machines
with several PHP versions installed at once can't grab the wrong one).

## 9. `wor run`: make every enabled service run (new)

A new command that checks and starts every enabled service on the machine,
along with the runtimes/web server it needs. Deliberately named `run`
rather than `start`/`up` because it is a one-way command -- "bring the
system to the desired state," like `terraform apply`/`docker-compose up`
-- with no paired `wor down`/`wor stop-all` to follow (design agreed
before coding through several rounds of discussion).

Order of operations:
1. One-time checks before the per-service loop: the active web server
   provider (started if not running -- new `Provider.IsRunning()`/
   `Provider.Start()` added to `internal/hostprovider`, since previously
   there was only `Reload()`, which always assumed the server was already
   running) and the pm2 daemon (only if any service actually needs pm2).
2. **Close the pm2 boot-persistence gap**: if `pm2 startup` has never been
   registered on this machine (nothing in wor ever called it, so
   pm2-backed services never came back after a reboot), it offers to
   register it right away: it first runs `pm2 startup` itself to obtain
   the suggested command (pm2 applies nothing itself, it only prints a
   `sudo ...` command for you to run), always shows the user the full
   command first, then runs it via `osutil.SudoCommand` (the same
   confirm-once elevation gate used elsewhere in the project, not just
   printing it for manual copy-paste).
3. Loop over each enabled service: check/start the runtime it needs first
   (for php with a dedicated pool -- new `phpfpm.IsRunning()`/
   `phpfpm.Start()` added for the same reason as the web server provider),
   then start the service itself if it is not running (pm2/systemd use the
   same path `wor service start` already uses).

Failed services are skipped and do not abort the whole command. Results
are shown per service as ok/fail along the way, ending with a one-line
summary of how many succeeded/failed.

### Notes from real testing (correct diagnosis required real output)

Several parts of `wor run` could not be diagnosed correctly until real
output from the user's machine was seen. An important lesson: features
that depend on external tools' behavior (pm2, Homebrew, launchd) cannot be
verified by reading code alone:

- **`pm2 startup` platform keyword**: wrongly guessed that macOS uses the
  word `launchd` (not actually a keyword pm2 recognizes). Fixed by passing
  no platform argument at all and letting pm2 auto-detect.
- **`pm2 startup`'s exit code is not a reliable success signal**: even
  when pm2 succeeds normally (detects the platform and prints the correct
  suggested command), the exit code is still non-zero. Fixed by checking
  the output content for a `sudo ...` line instead of the exit code.
- **`$PATH` doesn't expand without a real shell**: the command pm2
  suggests contains `env PATH=$PATH:/usr/local/bin ...`, which needs a
  shell to expand `$PATH` before `env`/`sudo` see it. Exec'ing the command
  directly (splitting argv yourself) leaves `$PATH` unexpanded as a raw
  string with a literal `$`, breaking the effective PATH (`mkdir` not
  found). Fixed by running the whole line through `sh -c` instead of
  parsing argv manually.

## 10. Redesign of `wor service status` and `wor host list`

`service status` used to just call `pm2 status` directly, showing only
node services -- go/python (systemd on Linux) and php/static services were
invisible. It now gathers every enabled service from every domain
(`Store.ListAllServices`), groups them by actual process provider
(`domainmodel.ProcessProviderFor`), and queries each provider's real
status: one `pm2 jlist` for all node services, plus one batched
`systemctl` sample (`systemd.GetInfoBatch`) for all go/python services, so
the pm2/systemd query cost is paid once regardless of service count. php
(assumed php-fpm already running) and static (no process) have nothing to
query, so they show an n/a status instead of being silently hidden.

`host list` used to just dump the `.conf` filenames in sites-available. It
now compares sites-available against sites-enabled to split
ENABLED/DISABLED, showing each site's resolved target (`domain/service`),
port, and SSL badge.

Both commands render through shared helpers in
`internal/cliapp/statusview.go`: ANSI colors on a real terminal, plain
bracket tags (`[ok]`/`[fail]`/`[on]`/`[off]`/`[ssl]`) otherwise (colors
can be disabled via the `NO_COLOR` env var). No external color library at
all (this project aims for zero third-party dependencies).

## 11. Redesign of `wor doctor`

From the old long format with Environment/Directories/Required-Optional
-Dependencies/Result/"WOR Ready"/"Next" sections, changed to a plain
✓/⚠/✗ checklist grouped into Environment (trimmed to just
OS/WOR_ENV/WOR_HOME/Config/Host Provider + a single line stating whether
the workspace is initialized), Runtimes, Database, Other Tools -- no
closing "Result" section anymore.

PHP/Node.js/Python/Go are ✗ immediately if not installed (the old
condition checking "is there actually a service needing this runtime?"
was removed entirely). Nginx and Apache are both shown if both are
installed (with an "(active)" label on the one matching HOST_PROVIDER),
and are ✗ only if the *active* one is missing (host provider doesn't
match what's actually installed) -- a missing non-active one is not a
problem. Databases (MySQL Client/Server, MariaDB, PostgreSQL, Redis,
SQLite) and other tools (git/zip/gzip) are always optional; missing ones
are only ⚠, not ✗.

## 12. Redesign of `wor domain remove` confirmation

`domain remove` has **no** `--cascade`/force flags at all -- it blocks
immediately if the domain's `services.config.json` still has even one
service (even a stopped one), listing the remaining services with the
exact fix command (`wor service remove <domain>/<service>`), because a
"domain" in wor's sense is only a config/source folder -- it does not
cover the services' pm2/systemd processes or host configs at all; those
must be cleared through `service remove` first (which already handles
that cleanup).

Once no services remain, it asks step by step with `[Y/n]` (default yes)
in the order **Backups -> Logs -> Web Data**: Backups/Logs only have
their "decision recorded" (with an immediate preview of delete-or-keep);
nothing is actually deleted yet. **Web Data, asked last, is the
confirmation point for the whole set**: answering "n" cancels everything
(the Backups/Logs choices made earlier are simply discarded, nothing is
deleted), answering "y" runs all three as chosen in one go (Backups
first, then Logs, then Web Data itself).

## 13. `wor source backup` filters files through `.gitignore`

By default (enabled), files being zipped are also filtered through the
source tree's own `.gitignore`, not just the exclude list configured in
`backup.config.json`. The new package `internal/gitignore` (no external
dependencies, per project policy) is a matcher that **deliberately reads
only the single `.gitignore` file at the root** of the directory being
zipped; nested per-subfolder `.gitignore` files as real git supports are
not handled (a trade-off chosen to avoid writing a far more complex
full-blown matcher). It supports comments, blank lines, negation with
`!`, anchoring with a leading/medial `/`, directory-only patterns with a
trailing `/`, and the `*`/`?`/`[...]`/`**` wildcards -- last matching
rule wins, as in real git. `wor source backup <target>
--gitignore=enable|disable` overrides this default for a single run
without modifying config.

## 14. `wor source clone` no longer needs `--replace`

If the target already has source, it is always backed up (via
`wor source backup`) and replaced automatically, with no extra flag
(`--replace` has been removed from usage; old scripts still passing it
are simply ignored, not errored). The replacement always moves the old
tree aside first (never deletes it outright) and only truly discards the
old one after the new tree has been moved into place successfully. If the
move fails, the old tree is moved back (rollback).

Directory moves (`moveDir`) try `os.Rename` first (faster), then fall
back to copy+remove if the rename fails with "invalid cross-device link"
(possible when the configured tmp directory and WOR_HOME are on different
filesystems). It makes no attempt to inspect specific errnos across
Linux/macOS/Windows -- any rename failure falls back to copy the same
way.

## 15. `wor database add`/`remove`: stricter validation

`add` no longer auto-creates the domain -- it errors immediately with
"domain not found" if `WOR_HOME/domains/<domain>` does not actually
exist. A duplicate profile (already present in `databases.config.json`)
does not error but prints a `[WARN]` instead (leaving the existing
label/.env untouched). `remove` errors if the domain does not exist, and
errors if the profile is not registered (both used to be silent no-ops).
It also fixes a real bug found: `remove` never deleted the
`<profile>.env` file under `configs/database/`, only the config entry. It
now deletes the `.env` file too (if the file is already gone, it only
warns, not errors).

## 16. PHP templates support PATH_INFO (front-controller URLs)

The first PHP templates only matched `location ~ \.php$` (nginx) and
relied on Apache's default path-info handling. That broke framework
routers that use front-controller URLs like
`/index.php/controller/action` (PATH_INFO): nginx never routed such a URL
to the PHP handler (it does not end in `.php`), so it fell through
`try_files` to a bare `/index.php` with an empty `PATH_INFO`, and a
router expecting PATH_INFO would emit its canonical redirect -- producing
an infinite redirect loop.

Fixes (in `internal/templates/assets/{nginx,apache}/php.conf`):

- nginx now matches `location ~ \.php(/|$)`, splits the script path from
  the trailing path with `fastcgi_split_path_info ^(.+?\.php)(/.*)$`, and
  passes `fastcgi_param PATH_INFO $fastcgi_path_info`. A guard,
  `if (!-f $document_root$fastcgi_script_name) { return 404; }`, is added
  because widening the match to `\.php(/|$)` otherwise lets a request like
  `/uploads/evil.jpg/x.php` reach PHP; the guard rejects it unless the
  resolved script actually exists (the nginx.com-recommended hardening,
  safe even if `cgi.fix_pathinfo=1`).
- apache adds `AcceptPathInfo On` to the service `<Directory>` block.
  Apache's `<FilesMatch "\.php$">` already matches on the resolved real
  file, so the ACE case above is inherently blocked; the only missing
  piece was `AcceptPathInfo`, which by default 404s trailing path-info on
  a real file.

Existing already-generated vhosts do not self-heal -- they must be
regenerated (re-run the host write) to pick up the new templates.

## 17. Per-service custom web-server config include

Each generated vhost now includes any `*.conf` a user drops into the
service's own directory:

    WOR_HOME/domains/<domain>/<service>/.wor/<nginx|apache>/*.conf

Rationale and shape:

- The vhost is fully regenerated from templates on every write, so
  anything a user customizes must live *outside* that file. A separate
  per-service directory pulled in via `include` (nginx) /
  `IncludeOptional` (apache) is the way to let users extend the vhost
  without their edits being overwritten.
- The directory lives under a `.wor/` namespace inside the service dir,
  not a bare `config/`, specifically to avoid colliding with the many app
  frameworks that ship their own `config/` at the repo root (the service
  dir *is* the deploy root). `.wor/nginx/` and `.wor/apache/` are separate
  so switching web server picks up the right set automatically.
- The include is emitted unconditionally and uses a wildcard, which both
  nginx and `IncludeOptional` tolerate when the directory is empty or
  missing -- so users can add snippets at any time and just
  `wor host reload`, with no hidden "did you opt in at create time" state.
- Snippets are included *after* wor's own service directives, inside the
  `server`/`<VirtualHost>` block. On nginx a snippet may ADD locations but
  may not redefine one wor already emits (nginx errors on duplicate
  `location`); heavy overrides belong in nginx's main config, which any
  operator doing that already knows. This "light additions only" scope is
  the intended one.
- wor writes a non-loaded reference file, `default.conf.example`, into the
  directory on each host write. It deliberately does not end in `.conf`
  (so the `*.conf` include never loads it) and contains wor's current
  default service block plus the rules above, so a developer can see
  exactly what wor generates and copy it as a starting point. It is
  regenerated each write and must not be edited in place.

Implementation: `hostprovider.WriteParams.CustomConfigBaseDir` carries the
service's `.wor` path; the nginx/apache providers emit the include from
it (`nginxCustomInclude`/`apacheCustomInclude`) and expose
`RenderServiceConfig` so `cliapp.writeCustomConfigScaffold`
(`internal/cliapp/customconfig.go`) can (best-effort) create the directory
and refresh the reference file. A failure scaffolding never blocks writing
the vhost, since the include tolerates a missing directory.

## 18. Windows code signing, and a signed release pipeline

Found on a real Windows 11 machine (2026-08-16): `wor version` failed with
`Program 'wor.exe' failed to run: An Application Control policy has
blocked this file`. This is Windows' Code Integrity layer -- Smart App
Control -- refusing to create the process at all, because the binary is
unsigned. It is not reachable from wor's own code: nothing in this repo
can make an unsigned binary run on such a machine. Practically, this made
the Windows target undistributable rather than merely rough.

Full reasoning, the alternatives evaluated, and the manual setup steps
live in `docs/code-signing.md`. The short version of the decisions:

- **Azure Artifact Signing (ex-Trusted Signing) was ruled out on
  geography, not price.** Its Public Trust certificates are restricted to
  a published list of countries that does not include Thailand, with no
  exception process, and billing starts (non-refundable, non-pro-rated) at
  account creation regardless of whether identity validation later
  succeeds.
- **EV certificates were ruled out on value.** Microsoft's current
  documentation states EV stopped bypassing SmartScreen in 2024 and that
  paying the premium for that reason is no longer justified; since the
  June 2023 CA/Browser Forum hardware-key rule applies to OV as well, the
  two tiers are now operationally near-identical.
- **SignPath Foundation was chosen**: free for OSS, recommended by
  Microsoft's own code signing guidance, and wor-host meets its criteria.
  The certificate is issued to SignPath Foundation, so that -- not the
  maintainer's name -- is the publisher Windows displays. Accepted
  deliberately: it also means inheriting an established publisher identity
  instead of starting from zero reputation.

Three consequences for the build, all deliberate:

1. **`go build` alone can no longer produce a releasable Windows
   binary.** Go emits no Win32 VERSIONINFO resource, so the `.exe` reports
   an empty ProductName/ProductVersion, and SignPath enforces both as a
   signing precondition. `scripts/build.sh` now runs `go-winres` before
   the windows target (`make_windows_resource`), stamping the version read
   from `internal/version/version.go` -- the same single source of truth
   `scripts/release.sh` already reads for archive names, so the resource
   can never disagree with what `wor version` prints. A missing
   `go-winres` is a **hard error**, not a warning, following the same rule
   as `wor service add` refusing a template whose runtime is absent: a
   build that silently produced an unsignable binary would only fail much
   later, in CI, after a human had already approved the signing request.
   The generated `*.syso` is build output and is gitignored.
2. **`scripts/release.sh` gained `--skip-build`.** Signing happens between
   building and packaging, and it happens off this machine. Without the
   flag, release.sh would rebuild the matrix and silently overwrite the
   signed `.exe` with an unsigned one, producing an archive that looks
   correct locally and is rejected on the user's machine. Packaging logic
   deliberately stays in release.sh so there is still exactly one
   definition of what a release archive contains.
3. **Official releases now come from CI, not a laptop.** SignPath
   Foundation only signs artifacts it can prove were built by a
   GitHub-hosted runner from this repository, which it verifies by reading
   the workflow run metadata from GitHub's own API rather than trusting
   anything the build script asserts. A locally built binary therefore can
   never be signed, by design. `scripts/build.sh` and `scripts/release.sh`
   are unchanged for local/unsigned use; `.github/workflows/release.yml`
   is the only path that produces a signed release.

Also fixed in passing: `scripts/release.sh` was committed with mode
`100644` while the other three scripts were `100755`, so `./scripts/
release.sh` failed with "Permission denied" on any fresh clone. It was
only ever run from a working copy where the bit had been set locally, so
nobody noticed. The CI workflow invokes it directly, which is what
surfaced it.

## 19. Vhost writes are validated before anything reloads

Every path that generates a virtual host now goes through one function,
`cliapp.applyHostParams`: snapshot the current files, write, enable,
run the web server's own config test, and reload only if it passes. A
configuration the server rejects is rolled back and never reloaded.

Before this, the SSL paths wrote a vhost and reloaded with no test in
between, and nothing rolled anything back. That is what turned a single
unreadable certificate into a dead web server: one invalid vhost fails
`nginx -t` / `apachectl configtest` for the *whole machine*, so the
reload took down every site rather than the one being changed. The
per-service php-fpm pool code (section 8) already validated before
touching anything live and rolled back on failure; this applies the
same shape to vhosts.

Consequences worth knowing:

- Enabling now happens for every caller, not just `host add`. On Linux
  the config test only sees files in sites-enabled, so testing a host
  that had been written but not enabled would validate nothing. The step
  is idempotent, and repairs a host whose sites-enabled entry went
  missing.
- `hostprovider.SnapshotHostConfig`/`RestoreHostConfig` record whether
  the sites-enabled entry was a symlink or a plain copy, because a copy
  holds its own content and has to be written back too.
- The rule this encodes: one host failing to come up is acceptable, the
  web server going down for every site is not.

A related ordering bug was fixed at the same time. `wor ssl remove`
regenerated the vhost *before* clearing the certificate state, so the
regenerated file still pointed at certificate files the same command
was about to delete. The state is now cleared first and the files are
deleted last, after the SSL-free vhost has been validated and reloaded.

## 20. HTTP -> HTTPS redirect is an explicit per-host setting

Previously apache redirected to HTTPS whenever a certificate existed,
with no way to switch it off, and nginx never redirected at all. Neither
was something the operator chose. Both providers now read one stored
flag, `forceHttps`, in `$WOR_HOME/ssl/hosts/<host>/ssl.json`.

The value is resolved once, when the certificate is issued, and stored.
It is never recomputed from the provider or the hostname on later reads
-- recomputing would let a behaviour the operator chose change by
itself. The default offered at that moment is on for a `letsencrypt` or
`custom` certificate, off for `self-signed`, and off for any local
hostname regardless (forcing HTTPS with a certificate no browser trusts
makes a site unreachable without clicking through a warning every
visit). `--redirect`/`--no-redirect` answer without prompting, and
`wor ssl redirect <host> on|off` changes it later without reissuing.

**`WOR_ENV` was considered and rejected as an input.** It is
machine-wide while this is per-host, and `internal/config` infers it
when the user never set one -- so a production host left at
`development` would quietly stop redirecting, with nothing reporting it.
The provider field carries the same intent and is always explicit.

The field is a `*bool`, so "never recorded" is distinguishable from
"recorded as off". State files written before it existed have no value,
and `cliapp.storedForceHTTPS` resolves those to *the active provider's
old behaviour* -- on for apache, off for nginx. Reading absent as a flat
false would have silently switched every upgraded apache site back to
serving plaintext on port 80; an upgrade quietly removing a redirect is
the direction that weakens a site.

nginx's template is now split into separate `:80` and `:443` server
blocks, matching apache's two VirtualHosts. The redirect lives inside
`location /` rather than at server level, because a server-level
`return` runs before nginx picks a location and would swallow the ACME
challenge below it. With the redirect on, the `:80` block carries only
the ACME location, the host check, and the redirect -- the service
config and the per-service custom include (section 17) move to the
`:443` block, since a snippet in a redirect-only block could never run.

`WriteParams.SSLChainFile`, `apacheSSLChainFileLine()` and
`{{APACHE_SSL_CHAIN_FILE}}` were removed in the same pass: nothing ever
populated the field, and `SSLCertificateChainFile` is both deprecated
since Apache 2.4.8 and unnecessary when serving `fullchain.pem`.

## 21. wor keeps its own copy of every certificate

The vhost now points at `$WOR_HOME/ssl/hosts/<host>/{fullchain,privkey}.pem`
for all three SSL providers, not just self-signed and custom. Let's
Encrypt used to be the exception, with the vhost referencing
`/etc/letsencrypt/live/<host>/` directly.

That exception is what broke a real machine. The key those symlinks
point at lives in root-only `/etc/letsencrypt/archive/`, which the web
server's *master* process can read on Linux (systemd runs it as root)
but not on macOS, where Homebrew starts it as the login user -- the same
structural problem section 8 hit with php-fpm pools. An unreadable
certificate makes the configuration invalid, and section 19 explains
why that took the whole server down rather than one site.

Copying removes the asymmetry. The copies are mode `0600` owned by the
operator, which is sufficient on both platforms with no ACL, no group
and no chown of anything else: the master reads certificates, and it is
root on Linux and the operator on macOS. `privkey.pem` is never made
group- or world-readable -- fixing an outage by exposing a private key
to every process on the machine is not a trade this makes.

Granting the web server user ACL access to `/etc/letsencrypt` was the
alternative, and was rejected: it cannot work on macOS at all, and it
means reaching into a directory another tool owns.

### `wor ssl sync`

One new subcommand does three jobs: it is what certbot's deploy hook
calls after each renewal, how a host issued before this change is
migrated, and the manual repair when the copy and the source have
drifted. It is idempotent -- an unchanged certificate produces no write
and no reload, so a hook firing for a certificate that did not move does
not churn the web server.

The owner for the copied files is **derived from WOR_HOME's own
ownership, never stored**. The hook runs from certbot as plain root with
no `SUDO_USER` to read, and baking a numeric uid into the hook command
at issue time would go stale the moment the machine is rebuilt or the
account changes, handing a private key to whichever unrelated account
then held that number. A root-owned WOR_HOME is refused *unless the
caller is also root*, which is the supported "server with no account but
root" case from section 4.

This is a general rule worth stating: **store decisions, derive facts.**
A decision the operator made (`forceHttps`) must be stored and never
recomputed, or the behaviour they chose changes by itself. A fact about
the machine (who owns WOR_HOME) must be derived at the point of use and
never stored, or it goes stale and becomes wrong.

The hook carries `WOR_HOME` explicitly and an absolute binary path,
because it runs as root months later: as root, `~/.wor/config` is root's
own, and the per-OS default WOR_HOME (`$HOME/wor` on macOS) resolves to
root's home rather than the operator's, so the hook would sync into the
wrong workspace.

### Issuance moved to `--webroot`

certbot's `--nginx`/`--apache` plugins work by editing the vhost, which
wor regenerates from templates on every write -- two tools owning one
generated file, which is exactly what section 17 avoids for user
customisation. The two-block split in section 20 made the conflict
concrete: both blocks carry the same `server_name`, so which one the
plugin patches is not something to depend on.

With `--webroot`, certbot writes a challenge file into
`$WOR_HOME/ssl/acme` and touches nothing else. The host provider stops
mattering for issuance, so nginx and apache share one code path, and wor
performs the reload itself -- routing it through section 19's
validate-then-reload helper. Every generated `:80` block serves
`/.well-known/acme-challenge/` unconditionally, whether or not the host
has a certificate yet, because a host with no certificate is precisely
the host about to request one; `wor ssl issue` regenerates the vhost
before invoking certbot so that a host created by an older version has
that location before the challenge arrives.

### Detection, because prevention is not enough

Copying introduces one risk the old design did not have: if the deploy
hook ever fails, the copy goes stale and the site serves an **expired**
certificate silently, which is worse than failing loudly. Three layers
answer that:

- the deploy hook (prevention);
- `wor health` reports certificate expiry as its yellow Warning tier,
  so the exit code stays 0 and cron does not alarm on something days
  away (detection of the outcome);
- every `wor ssl sync` records its result in
  `$WOR_HOME/ssl/hosts/<host>/sync.json`, read back by `ssl status`,
  `health` and `diagnose` (detection of the cause). wor has no log of
  its own, so a hook that fails at 03:00 would otherwise leave a trace
  only in the renewal job's output, which nobody reads.

A general wor log file was considered and deliberately deferred: it is a
feature in its own right (rotation, levels, which commands write) and
does not belong inside an SSL change.

`wor doctor` gained two related checks, both warnings that never affect
the exit code: a WOR_HOME owned by root when the caller is not, and
Let's Encrypt certificates with no renewal schedule anywhere on the
machine. The second is not hypothetical -- on the macOS machine this
work came from there was no systemd timer, no launchd job and no root
crontab, so nothing was going to renew anything and the hook would never
have fired. It follows the precedent set in section 9 for `pm2 startup`:
report the gap, offer the command, never install it silently.

## Known gaps / still to verify

- **The SSL rework (sections 19-21) has not been run against a real
  certificate authority yet.** It builds, vets and tests clean on every
  target, and the generated configs are covered by rendering tests, but
  the end-to-end path -- `certbot --webroot`, the deploy hook firing on
  a real renewal, `wor ssl sync` running as root from that hook -- has
  not been exercised. On the machine it was written for, port 80 was
  not reachable from the internet at the time (dynamic DNS switched
  off), so even `certbot --dry-run` could not complete. Verify with a
  dry run before trusting it in production.
- The two existing Let's Encrypt hosts on that machine
  (`team.ddns.net`, `team-pma.ddns.net`) still record
  `authenticator = nginx` in their renewal configs. `wor ssl sync`
  migrates the certificate copy, but moving them onto webroot needs a
  reissue -- and certbot only rewrites a renewal config when it
  actually obtains a certificate, so `wor ssl issue` warns when the
  deploy hook did not end up registered.
- `Preferred` (which of a host's names is canonical) is still not
  persisted anywhere: it is passed on the command line and lost on the
  next regeneration. Pre-existing, but sections 19-21 regenerate vhosts
  more often, so it surfaces more.
- **Partially built/run for real**: during the initial port, the sandbox
  used for writing had no Go toolchain at all, so the code was never
  compiled then. Since then the user has run `go build`/executed it for
  real on their own macOS machine (`./scripts/build.sh`), finding and
  fixing several real bugs invisible to code reading alone (see sections
  8/9 above), but not every path has been tested on a real machine yet.
  In particular:
  - `wor run`'s pm2-startup registration flow has been through 3 rounds
    of fixes (wrong platform keyword -> wrong exit-code check -> `$PATH`
    not expanding); the latest round has not yet been confirmed by the
    user as actually working.
  - Per-service php-fpm pool: confirmed working on macOS after the unix
    user fix (section 8), but never tested on real Linux at all
    (`useradd`, `php-fpm -t`, `systemctl reload`).
- The default nginx/apache paths on Windows (section 3) are only guessed
  conventions, never verified against real nginx/Apache on Windows --
  expect to override via `host.env` at least once.
- PM2 on Windows: PM2 itself is an npm package and should work, but it
  has never been tested as part of this port.
- Short single-dash flags accepted by the old shell version (e.g. `-y`
  alongside `--yes`) are not supported by the Go flag parser
  (`internal/cliapp/args.go`) -- only the long forms work. Easy to add if
  some script depends on the short forms.
- RHEL-family Linux uses a php-fpm package layout different from
  `/etc/php/<version>/fpm` and is not yet supported by auto-detect
  (`phpfpm.DetectVersions`).
- php services whose per-service pools were created on macOS **before**
  the unix user fix (section 8) still have the old-style pool
  `.conf`/unix user left behind; they do not self-heal and must be
  removed+added again manually. There is no lightweight "repair an
  existing pool in place" command yet.
- **Code signing is wired up but not yet live** (section 18). The
  SignPath Foundation application, the SignPath-side project/policy/token
  setup, and the first signed release have not happened yet, so
  `.github/workflows/release.yml` has never run end to end -- the build,
  resource-generation and packaging steps have been tested, the signing
  step has not. Until a signed release ships, every published `wor.exe`
  is still blocked by Smart App Control.
- **Only the Windows binary gets signed.** The Linux and macOS binaries
  are unsigned, and macOS notarization (a separate paid Apple process) is
  not addressed at all -- expect Gatekeeper warnings there.
- A signature is not instant reputation: SmartScreen builds reputation
  per file hash over download volume, and there are 2026 reports of
  correctly signed binaries still being blocked by Smart App Control for
  weeks. Signing is necessary, not provably sufficient.
