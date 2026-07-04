# Design notes: wor-cli (bash) -> wor (Go)

This document records the deliberate differences from the original
shell CLI and why each one was made. Everything not listed here should
behave the same way (same directory conventions, same PM2 naming
`wor_<domain>_<service>`, same `wor__<host>.conf` / `000_wor_default.conf`
host file naming, same template variables).

## 1. Config files are JSON, not hand-rolled JS

The shell CLI stored `services.config.js` / `databases.config.js` /
`backup.config.js` as `module.exports = {...}` files, read and written
by shelling out to `node -e '...'`. That made Node.js a hard dependency
for *managing configuration*, even for a pure static site with no
Node.js service at all -- and it doesn't have an equivalent that works
well on Windows without assuming Node is on PATH before wor itself can
even list domains.

The Go rewrite stores the same information as
`services.config.json` / `databases.config.json` / `backup.config.json`,
read and written with `encoding/json`. Same shape, same fields
(`domain`, `services[].name/type/hosts/port/...`), just a different file
extension and no code-generation step. If you have existing `*.config.js`
files from wor-cli v1, they need a one-time conversion to `.json`
(strip the `module.exports = ` prefix and trailing `;`) -- there's no
automatic migration in this rewrite yet.

`wor.config.js` (the generated PM2 ecosystem file) similarly becomes
`wor.config.json`. PM2 natively supports `pm2 start ecosystem.json`, so
this is a drop-in change with no loss of functionality.

## 2. No shelled-out `gzip`/`zip`/`tail`/`ss`/`lsof`/`netstat`

The shell version composed dozens of small Unix utilities. Each one is
a place the Windows port would break. The Go version replaces them with
standard-library equivalents:

- Database backup compression: `compress/gzip` instead of piping
  through the `gzip` binary.
- Source backups: `archive/zip` instead of shelling out to `zip`.
- Port availability checks (`wor service add`'s auto-port picker):
  attempting `net.Listen("tcp", ...)` instead of parsing `ss`/`lsof`/
  `netstat` output.
- `wor host logs`: a small built-in tail-and-follow loop instead of
  `tail -f` (which doesn't exist on Windows).

## 3. Cross-platform host provider paths

nginx/apache sites-available/sites-enabled/log directory conventions
differ by OS:

- Linux: `/etc/nginx/sites-available` + `/etc/nginx/sites-enabled`
  (classic Debian-style split), `/etc/apache2/sites-available` or
  `/etc/httpd/conf.d`.
- macOS (Homebrew): a single flat directory (`servers/` for nginx,
  `servers/` under `httpd` for apache) -- there is no separate
  "enabled" directory, so enabling a host is a no-op once the file is
  written.
- Windows: no ecosystem-wide convention exists. This rewrite defaults
  to `C:\nginx\conf\sites-available` / `C:\Apache24\conf\sites-available`
  with the enabled directory equal to the available directory (same
  flat-directory model as macOS, sidestepping the fact that creating
  symlinks on Windows requires either Administrator rights or Developer
  Mode). These are reasonable defaults, not universal ones -- override
  them via `host.env` (`NGINX_SITES_AVAILABLE=`, etc.) to match your
  actual nginx/Apache Windows install.

All of this lives behind one interface (`internal/hostprovider`), so
adding a more accurate Windows-specific default later doesn't touch any
command code.

## 4. Privilege elevation

Unix keeps the original model: if not root, wrap privileged operations
(`mkdir`, writing into `/etc/nginx/...`, `tee`, `rm`, `ln`, `systemctl
reload`, `certbot`) with `sudo` -- but with two additions over the
shell version:

- **`wor` refuses to run as `sudo wor ...`.** `osutil.IsSudoElevated()`
  checks for root *plus* a `SUDO_USER` environment variable (which
  `sudo` sets on the child process, but a direct root login never
  does). `App.Run()` checks this first, before dispatching to any
  subcommand, and exits immediately with an error if true. This is
  deliberately narrower than "reject if root": a server with no user
  account other than root (logging in and running `wor` directly, no
  `sudo` involved) is unaffected, since `SUDO_USER` is never set in
  that case. PM2 already refused to run under sudo (see
  `internal/pm2`); this closes the same gap for every other
  subcommand, so users can't accidentally end up with root-owned git
  clones, npm installs, or PM2 dumps by prefixing the whole CLI with
  `sudo`.
- **`osutil.SudoCommand` asks for confirmation the first time (per
  process) it actually needs to prepend `sudo`** -- not before, and not
  again for the rest of that command. `cliapp.New()` wires this to an
  interactive `[Y/n]` prompt (`osutil.SetElevationPrompt`); declining
  makes every subsequent privileged call in that run fail immediately
  rather than re-prompting. Environments where the relevant paths are
  already user-writable (e.g. Homebrew's nginx directories on macOS)
  never trigger the prompt at all, since the unprivileged attempt
  succeeds first and `SudoCommand` is never reached.

Windows has no equivalent of transparently re-running a command with
elevation from inside an already-running process, and this rewrite
does not build a UAC re-launch flow, so an elevated (Administrator)
console remains the only way to run privileged commands on Windows --
`IsSudoElevated()` always returns `false` there on purpose, so Windows
users are not blocked the way `sudo wor` is on Unix. `osutil.IsElevated()`
detects an elevated console via the standard `net session` probe
(succeeds only for Administrators); when a privileged write fails, the
error tells the user to re-open their terminal as Administrator, rather
than silently trying (and failing) to auto-elevate via a UAC prompt.

## 5. SSL: Let's Encrypt is Unix-only

Certbot has no official, reliable Windows story. `wor ssl issue
--provider=letsencrypt` returns a clear error on Windows pointing at
`self-signed` or `custom` instead of attempting something fragile.
Self-signed (via `openssl`, if installed) and custom
(bring-your-own cert/key) work on every OS.

## 6. Service templates: go/python + systemd (new vs. v1)

wor-cli v1 had no `go` or `python` templates. Added them (see
`docs/services.md`) along with a fifth-template cleanup: the four
hybrid templates (`static-node`, `node-web`, `node-php`, `php-node`)
were removed, leaving exactly five: `static`, `node`, `go`, `python`,
`php`. A service is one runtime kind, not a mix -- the hybrid variants
existed for cases that are better served by running a separate static
and process-backed service under the same domain.

Process supervision now has two providers:

- **node** always uses PM2 (unchanged from v1), on every OS.
- **go** and **python** use **systemd** on Linux -- already present on
  virtually every distro, and simpler to reason about than adding a
  second PM2-managed language runtime -- and fall back to **PM2** on
  macOS and Windows, where systemd doesn't exist. `domainmodel.ProcessProviderFor`
  is the single place this OS-dependent choice is made; `internal/systemd`
  mirrors `internal/pm2`'s shape (unit generation, start/stop/restart/status/logs,
  `wor_<domain>_<service>` naming) so the two providers are close to
  interchangeable from the CLI's point of view.
- **php** and **static** have no process to supervise (php-fpm is
  assumed already running as its own system service; wor never manages
  its lifecycle).

Every systemctl/journalctl call goes through the same confirm-once
sudo escalation gate described in section 4.

`go` additionally needs a build step that node and python don't:
`wor service add --service-type=go` and `wor create` run `go build`
right after scaffolding, and `wor deploy` reruns it on every deploy
where `git pull` brought in a new commit -- unconditionally, not based
on a file-diff heuristic like node's package.json-changed check,
since a plain `.go` source edit with no dependency change still needs
recompiling.

`wor create` also changed shape in this redesign: it now accepts *no*
`--` flags at all (only an optional positional host), reinforcing its
existing "interactive only" intent. The one flag it dropped real
functionality for, `--domain=` (overriding the auto-derived domain id),
became an interactive confirm/override prompt instead of disappearing.
Automation continues to go through `wor domain/service/host add`,
which gained `--service-type=` (renamed from `--template=`, to match
the existing `--domain-type=` naming and the internal `Service.Type`
field) and a new `--entry=` flag for overriding a service's entry point
file/binary name.

`wor create`/`wor service add` hard-block service creation with a clear
"runtime not found" error if a template's runtime isn't installed --
there is deliberately no interactive "configure it now?" prompt for
this, unlike some other wizards in this CLI; `wor doctor` is the
single place that reports what's missing and how to fix it.

## 7. What's intentionally out of scope (same as v1)

- No restore/drop/migrate for databases -- backup only.
- `wor create` remains interactive-only; automation goes through
  `wor domain/service/host add`.
- Templates are immutable after service creation.

## Known gaps to verify on a real machine

- This code has not been compiled (`go build ./...`) because the
  authoring sandbox had no Go toolchain and no network access to
  install one. It should be close, but treat the first build as a
  normal "fix the typos" pass, not evidence of a deeper problem.
- Windows nginx/apache default paths (section 4) are best-effort
  conventions, not verified against a real Windows nginx/Apache
  install -- expect to override them via `host.env` at least once.
- PM2 on Windows: PM2 itself is an npm package and should work, but it
  hasn't been exercised as part of this rewrite.
- The single-dash short flags some commands accepted in the shell
  version (`-y` alongside `--yes`) are not parsed by the Go flag parser
  (`internal/cliapp/args.go`); only the long form works. Easy to add if
  you rely on the short form in scripts.
