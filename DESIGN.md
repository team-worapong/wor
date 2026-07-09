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

## Known gaps / still to verify

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
