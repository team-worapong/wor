# All wor commands

This document summarizes all `wor` CLI commands as actually implemented in
`internal/cliapp` (see `internal/cliapp/usage.go` as the primary reference
that is always kept in sync with the code -- if this document and `usage.go`
conflict, `usage.go`/the actual code behavior wins).

## General

- `wor version` / `wor --version` -- show the program name and version
- `wor setup` -- first-time system setup wizard (environment, WOR_HOME, host
  provider, SSL provider, php-fpm endpoint). Safe to re-run at any time;
  previously configured values are used as defaults rather than being reset.
  Every wizard run finishes by checking/refreshing the default vhost
  (`000_wor_default.conf`): if the existing file (e.g. left over from an old
  installation that used a different WOR_HOME) points its document root
  somewhere other than the current workspace, it is regenerated with a
  warning -- the check compares file content (stateless), not just "WOR_HOME
  changed during setup", so `wor doctor`/`wor host add`/`wor reset`, which
  call EnsureDefaultHost the same way, get this self-healing behavior too
  (files that still point to the correct path are left untouched; other
  admin edits are preserved).
- `wor doctor` -- read-only system health check. Shows a ✓/⚠/✗ checklist for
  the environment, runtimes (Node.js/Go/Python/PHP/PHP-FPM), the active web
  server, databases (always optional), and other tools (git/zip/gzip, always
  optional). Exit code is non-zero when something "required" is missing (a
  core runtime, a host provider that is configured but not installed, or a
  workspace that has not been initialized).
  It ends with a "Security" section (⚠ only; never makes the exit code
  non-zero): (1) scans for `.env`/`.env.*` files under WOR_HOME that are
  group/other-readable, listing the paths plus a copy-pasteable
  `find ... -exec chmod 600` command to fix them (2) checks whether the web
  server user (guessed/read from the `user` directive in nginx.conf, default
  `www-data`) can actually traverse into WOR_HOME (and all its ancestor
  paths) and into `domains/<domain>/<service>/public` of every registered
  static/php service -- the full check runs only on Debian/Ubuntu (with
  exact `setfacl` commands suggested); other Linux families (RHEL etc.) get
  only a broad warning to also check SELinux; the check is skipped on
  macOS/Windows (macOS: Homebrew nginx usually runs as the login user, not a
  separate system account; Windows: a completely different permission
  model).
- `wor env` -- show the current config/environment values
- `wor clean` -- remove host configs/PM2 processes/systemd units/`/etc/hosts`
  entries that have become orphaned (no domain/service references them
  anymore), but never touches anything still tied to a registered service
- `wor reset` -- wipe everything wor has created back to a clean state
  (PM2 processes prefixed `wor_`, systemd units `wor_*.service`, host
  configs `wor__*.conf`, entries in `/etc/hosts`, the domains/backups/
  logs/ssl folders). **Does not delete** host configs that are not wor's.
  Always requires typing `RESET` to confirm; there is no flag to skip this
  confirmation.

## wor create

```
wor create [host]
```

Purely interactive-only. Accepts no flags other than the optional
positional host argument. It walks through service type, domain id
override, domain type (local/public), and hosts entry setup step by step.
For anything that needs automation, use `wor domain/service/host add`
instead.

## Domain

```
wor domain add <domain-id>
wor domain remove <domain-id>
```

`domain add` creates the domain's base folders/config files (including the
related backup/log folders). `domain remove` **blocks immediately** if the
domain still has any registered services (even stopped ones); each must be
removed with `wor service remove` first, because a "domain" in wor's sense
is only a config/source folder -- it does not include the services'
processes or host configs, which are specifically `service remove`'s job.

Once no services remain, the system asks step by step (in order: Backups ->
Logs -> Web Data). Backups/Logs only "record the decision" up front (it
immediately says whether it will keep or delete) without deleting anything
yet; **Web Data, asked last, is the confirmation point for the whole set**:
answering "n" at Web Data cancels everything (including the Backups/Logs
choices just made -- nothing is deleted at all), answering "y" deletes all
three as chosen, in one go.

## Service

```
wor service add <domain>/<service> [--host=<host>] [--port=<port>]
    [--entry=<entry-point>] [--service-type=static|node|go|python|php]
    [--php-version=<version>] [--no-php-pool] [--no-start]
wor service remove <domain>/<service> [--cascade] [--yes]
wor service start <domain>/<service>
wor service stop <domain>/<service>
wor service restart <domain>/<service>
wor service status
wor service logs <domain>/<service> [--lines=100]
```

`service add` **blocks immediately** if the runtime for the chosen template
is not installed (there is no "set it up now?" prompt); check `wor doctor`
to see what is missing. A `php` service automatically gets its own dedicated
php-fpm pool when the machine detects exactly one PHP-FPM version
(`--php-version=` selects one when several are found; `--no-php-pool` falls
back to the old shared endpoint -- see full details in `docs/services.md`
and `DESIGN.md` section 8).
`node`/`go`/`python` services are started automatically right after
creation (as if `wor service start` were run immediately afterwards). Pass
`--no-start` to skip this step (e.g. to configure env/secrets first and
start manually). If auto-start fails (e.g. a temporary runtime problem), it
only warns and does not make `service add` itself fail -- the
config/ecosystem/unit were already created successfully before that point,
so you can immediately run `wor service start <domain>/<service>` yourself.

`service remove` blocks if any host still references this service, unless
`--cascade` is given (which also deletes the related host configs).

`service status` no longer just calls `pm2 status` -- it gathers **every**
service (including disabled ones) from all domains and displays them
grouped by actual process provider (`PM2 (node)`, `SYSTEMD (go/python)`,
`PHP-FPM (php)`, `STATIC (no process)`). Each row shows status
(online/pid/uptime/cpu%/memory) from `pm2 jlist`/`systemctl show`, queried
once per group for all services in that group rather than repeatedly per
service.

Row icons (agreed 2026-07-06 after the "status all green but the site was
down" incident): blue ✓ = enabled, red ✗ = disabled -- the icon conveys
**config state only**. Deliberately no green dot, because green gets read
as "the site is fine," which this command never checks. Process state is
the last column; the state word for a service that is enabled but whose
process is not running (errored/stopped/not started) is rendered in red.
Disabled rows show a dimmed "disabled" state and are not queried. The end
of the report always includes a hint pointing to `wor health` for
end-to-end health.

`service start`/`stop`/`restart`/`logs` error immediately if the specified
domain/service does not exist (no more silently falling back to "a static
service with nothing to do," as an old bug used to).

## wor run

```
wor run
```

The single command that checks and starts **every enabled service on the
machine**, along with the runtimes/web server they need. Deliberately named
`run` rather than `start`/`up` because it is a one-way command ("bring the
system to the desired state," like `terraform apply` or
`docker-compose up`); there is no matching `wor stop`/`wor down`.

Order of operations:
1. One-time checks before the loop: the active web server provider
   (started if not running) and the pm2 daemon (only if any service needs
   pm2) -- if `pm2 startup` has never been registered on this machine, it
   offers to register it right away (always showing the command it will run
   first, then requesting sudo via the same confirm-once pattern other
   privileged operations use), closing the gap where pm2-backed services
   never came back after a reboot.
2. Loop over each enabled service: check/start the runtime that service
   needs first (for php with a dedicated pool), then start the service
   itself if it is not running.

Failed services are skipped and do not abort the whole command. At the end
it prints a one-line summary of how many services succeeded and how many
failed.

## Host

```
wor host add <host> [--target=<domain>/<service>] [--server=nginx|apache]
    [--replace] [--domain-type=local|public] [--add-hosts|--no-hosts]
wor host remove <host> [--yes]
wor host list
wor host test
wor host reload
wor host logs <host> [access|error] [--lines=100]
```

`host list` shows a single table under the report header
`WOR Hosts <server> (<version>)` (no more ENABLED/DISABLED group headers).
Each row has blue ✓ = enabled / red ✗ = disabled (comparing
sites-available against sites-enabled; enabled rows sort first), the target
(`domain/service`), the port, and a plain-colored `ssl`/`-` marker --
deliberately not coloring whole rows green, because this list reports
config, not health (a cert existing in the system does not mean the cert
works -- `wor diagnose <host>` is the real check).

`host remove` deletes the host config, the entry in services.config.json,
the entry in `/etc/hosts`, and the recorded SSL state
(`$WOR_HOME/ssl/hosts/<host>/`) all in one command.

## Database

```
wor database add <domain>/<profile> [--label="Label"]
wor database remove <domain>/<profile>
wor database backup <domain>/<profile>[/database]
```

Backup only -- there is **no** restore/drop/migrate (deliberately, same as
the original wor-cli). `add` errors if the domain does not already exist
(it no longer auto-creates the domain like it used to). A duplicate profile
does not error, only warns. `remove` deletes both the config entry and that
profile's `.env` file (previously the `.env` file was forgotten).

## Source

```
wor source clone <domain> <git-url>
wor source clone <domain>/<service> <git-url>
wor source pull <domain> [--stash]
wor source pull <domain>/<service> [--stash]
wor source backup <domain> [--gitignore=enable|disable]
wor source backup <domain>/<service> [--gitignore=enable|disable]
```

`source clone`: if the target already has source, it always backs it up
(via `wor source backup`) and replaces it automatically -- no extra flag
needed (it used to require `--replace`, but this is now considered the
desired behavior, with the backup as the safety net). During replacement
the old source is always moved aside first, never deleted outright, until
the new source has actually been moved into place successfully.

If the old tree has a `.env`, a prominent warning is shown (the fresh clone
has no `.env`, and the backup zip may not either because it filters by
`.gitignore`), then a choice is offered: **keep both** (default -- the old
`.env` stays in use, the repo's copy is kept as `.env.new`), **overwrite**
(the old `.env` stays in use, the repo's copy is discarded), or **replace**
(use the repo's copy, discard the old one -- requires an extra
confirmation).

After the clone, if the target is a registered service, it asks whether to
deploy right away (delegating to `wor deploy --no-pull --force`), because a
fresh clone has no `node_modules` / build output / go binary -- the service
cannot run until dependencies are installed and it is built.

`source backup` compresses to `.zip` using Go's own `archive/zip` (no
external `zip` program required). By default it also filters files against
the `.gitignore` at the root of that source tree (in addition to the
existing exclude list in `backup.config.json`).
`--gitignore=enable|disable` overrides this behavior for a single run
without modifying config. The matcher deliberately reads only the single
`.gitignore` file at the root; nested `.gitignore` files in subfolders are
not supported the way real git does (a trade-off chosen to avoid writing a
full-blown matcher).

## Deploy / Rollback

```
wor deploy <host|domain/service> [--pull-only] [--no-pull] [--no-restart] [--force] [--stash]
wor rollback <domain>/<service> [--yes]
```

`deploy` = pull new code (if any) -> install dependencies if the manifest
files changed (`package.json`/`requirements.txt`) -> build if needed (node
checks for an `npm run build` script; go builds **every time** there is a
new commit, with no node-style heuristic) -> restart the service via the
correct process provider -> health-check after restart (PM2
`describe`/systemd `is-active`).

`--force` skips all "did the manifest change?" checks: it always forces
`npm ci` (or `npm install` if the repo has no lockfile), `npm run build`,
`pip install -r requirements.txt`, and go build -- this is the mechanism
`wor rollback` and `wor source clone` use (calling deploy with
`--no-pull --force`), because in both cases the commit has not moved from
deploy's point of view, yet the dependencies are exactly what is missing or
stale.

`rollback` hard-resets the source back to `origin/<branch>`, discarding all
uncommitted changes (always backing up via `wor source backup` first). It
accepts only `domain/service`, not a bare domain.

## SSL

```
wor ssl issue <host> [--provider=letsencrypt|self-signed|custom|none] [--preferred=<host>]
wor ssl renew <host>
wor ssl status <host>
wor ssl remove <host> [--yes]
wor ssl install <host> --cert=/path/fullchain.pem --key=/path/privkey.pem
```

`letsencrypt` (via certbot) is Unix-only (there is no trustworthy certbot
build for Windows). `self-signed` (via `openssl` when available) and
`custom` (bring your own certificate/key) work on every OS.

## Info

```
wor info <host|domain/service>
```

Shows a summary of the given host or domain/service in one command: type,
enabled, source path, bound hosts (with per-host SSL status), the real
process status via that service's provider (pm2 describe /
systemctl status / php-fpm pool socket+version+group depending on type), a
reachability check of whether the web server user (nginx/apache) can
traverse WOR_HOME and this service's docroot (Debian/Ubuntu and static/php
only -- node/go/python reverse-proxy services don't need file reads), a
Resources section (host CPU%/Mem + this service's cpu/mem -- details under
`wor health` below), and git status if the source is a git repo.

## Health

```
wor health
```

The "the site is down but I don't know which one yet" mode (e.g. after a
reboot): sweeps every enabled service -- checks the process/port layer per
service **and then fires one real HTTP request per service through the web
server** (first registered host, dialing 127.0.0.1 + Host header), because
permission/vhost/proxy problems never kill a process -- "pool accepting"
does not mean the site is reachable (lesson from a real incident,
2026-07-06).

Layout is one card per service (mockup from the owner, agreed 2026-07-07):
a machine summary header with `Host CPU` (% across all cores, sampling
`/proc/stat` twice) / `Host Memory` (`/proc/meminfo` as
MemTotal−MemAvailable) / `Disk Usage` (WOR_HOME's filesystem), followed by
a ● card per service: Status, Runtime (with php-fpm worker count for
dedicated pools), CPU (100% = one full core, top-style), Memory (RSS + %
of total machine RAM), Uptime (pm2 only), and an HTTP line
`✓/⚠/✗ <url> -> <code>` -- CPU/Memory/Uptime lines with no data (static
etc.) are hidden entirely rather than showing "-".

There are 3 status tiers: green ● = healthy, yellow ● = **Warning** (HTTP
404 "may be normal for APIs," or no host to probe -- shown visibly but the
exit code stays 0 so cron/monitoring doesn't false-alarm), red ● = FAILED
(broken process, or HTTP 4xx/5xx/refused/timeout). The report ends with a
Healthy/Warning/Failed tally plus a `wor diagnose <target>` suggestion per
failed service. Resource data source per provider: pm2 = monit from
`pm2 jlist`, systemd = CPUUsageNSec/MemoryCurrent delta (`GetInfoBatch`),
php pool = summing every worker in `/proc` (matching the title
"php-fpm: pool <name>"). The whole report waits for a single sample
(~200-250ms). The `/proc` reader is Linux-only (macOS only sees pm2's
values).

This used to be `wor diagnose --all` -- split out into its own command
(agreed 2026-07-06) because "diagnose" means analyzing a patient you have
already identified, while this command *finds* who is sick. The line
between it and `wor doctor`: doctor answers "is the machine/runtime
installed and ready?", health answers "are the services still serving?"
It shares every diagnose guarantee: read-only, no sudo prompts, every
probe has a timeout, exit code 0/1 works with cron/monitoring, and it does
not hold the WOR_HOME lock.

The full-circle story: `wor health` (who is broken) ->
`wor diagnose <target>` (why it broke + how to fix it) -> `wor run`
(bring it back).

## Diagnose

```
wor diagnose <host|domain/service>
```

Analyzes the root cause of a down/misbehaving service, strictly read-only
(**no auto-fix whatsoever** -- it only shows copy-pasteable fix commands;
the admin decides and runs them. See the full design in
`docs/diagnose.md`). It checks layer by layer, outside-in along the request
path: config (enabled/entry point/docroot/runtime) -> dns/hosts file ->
web server (running?, vhost present+enabled, unelevated config test) ->
SSL (cert files + expiry read via crypto/x509, no openssl dependency) ->
process (pm2/systemd/php-fpm by provider, with crash-loop detection and
the special "pm2 empty after reboot" case) -> port (distinguishing "nobody
listening" from "another process took the port") -> two-layer HTTP probe
(hitting the app directly, and through the web server at 127.0.0.1 with a
Host header so it can't wander off to a CDN/proxy) -> file reachability
(Debian/Ubuntu) -> disk -> logs (pm2/journalctl/nginx error log with known
patterns such as EADDRINUSE, Cannot find module, OOM). The report ends by
synthesizing every FAIL into **a single Root cause + Evidence
(grouped/deduplicated) + Fix** -- FAILs that are the same problem seen
from different layers (e.g. http 403 + file permission) are merged by a
kind+confidence system rather than left for the user to interpret. There
are at most 2 "Other possibilities" entries as a hedge against
mis-ranking, and if the source changed within the last hour it also
suggests `wor rollback`. The report header summarizes
Target/Host/Runtime/Server in the first lines.

Additions from real-world field lessons (2026-07-06): (1) the php pool
process layer also checks **socket permissions from the web server user's
point of view** -- a pool that answers wor but whose socket refuses
www-data connections (wrong listen.owner) used to PASS while actually
502ing; it is now a FAIL with a sed command to fix the pool config (2) log
evidence from the nginx/apache error log **discards lines older than 1
hour** (lines whose timestamps can't be fully parsed are kept, not
dropped), because the http probe just fired moments ago, so a problem that
still exists will always have fresh lines -- this stops stale lines from
before a config fix from hijacking the root cause (3) if a log references
a path containing `/domains/` that is not under the current WOR_HOME, it
concludes "a config from an old installation is still active" instead of a
generic permission issue.

For sweeping the whole machine for broken services, see `wor health`
(formerly `wor diagnose --all`, split out into its own command).

Behavioral guarantees (all shared with `wor health`): absolutely no sudo
prompts (checks that would need root report "not verified" instead), every
probe has a short timeout, and the exit code is 0 when no problem is found
/ 1 when one is, so it works with cron/monitoring. It also does not hold
the WOR_HOME lock (same as version/help/logs and path/shell-init) -- a
diagnostic tool must not be blocked during an incident.

## Path / Goto (folder navigation)

```
wor path [.|./<path>|<domain>[/<service>]]
wor shell-init
wor goto [.|./<path>|<domain>[/<service>]]   (shell function)
```

- `wor path <domain>` / `wor path <domain>/<service>` -- resolves to a
  directory under `WOR_HOME/domains` and prints **the bare path on a single
  line** to stdout (no `[OK]` prefix) so that
  `cd "$(wor path myapp/backend)"` works directly. All errors go to
  stderr + exit 1.
- `wor path .` -- WOR_HOME itself / `wor path ./<path>` -- `WOR_HOME/<path>`
  (any subtree, e.g. `./logs`, `./backups/myapp`). The `./` form accepts
  multi-level paths and therefore bypasses the slug rules -- traversal is
  blocked instead via `filepath.Clean`, rejecting anything that still
  starts with `..` or becomes an absolute path.
- Validation is only "the directory actually exists on disk" (os.Stat); it
  does not check services.config.json -- folders not yet registered as a
  service can still be navigated into.
- **With no argument** (both `wor path` and `wor goto`) -- shows a numbered
  menu: the first entry is always `WOR_HOME (<actual path>)`, followed by
  every domain and domain/service sorted by name. The menu/prompt goes to
  stderr, the selection is read as a number from stdin, and only the chosen
  path is printed to stdout -- this 3-stream contract lets the menu work
  even inside a shell function's command substitution. Pressing Enter on an
  empty line = cancel (exit 1 -- it must not be 0, or the shell would
  `cd ""`).
- `wor shell-init` -- prints a shell function to install in an rc file:
  `eval "$(wor shell-init)"` in `~/.bashrc`/`~/.zshrc` gives you
  `wor goto <target>` that **actually cd's** (a process cannot change its
  parent shell's cwd -- same approach as zoxide/nvm, hence a shell function
  wrapping `cd "$(command wor path ...)"`). `install.sh` offers to add this
  line automatically based on the operator's login shell.
- Both are read-only and do not hold the WOR_HOME lock (`path` is called on
  every `goto`; `shell-init` is eval'd on every new shell -- they must not
  queue behind a running deploy). `shell-init` is also exempt from the
  workspace-init gate: if it printed ERROR while the workspace was not yet
  initialized, that message would be eval'd as a shell command in every new
  terminal.

## Environment variables

`wor` always shows the values currently in effect at the end of
`wor help`/`wor <no command>`: `WOR_ENV`, `WOR_HOME`, and the config file
in use. These are set via `wor setup` or edited directly in the config
file/`host.env`.
