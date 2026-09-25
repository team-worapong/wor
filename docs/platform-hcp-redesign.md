# Supported platforms and a bundled control panel -- Design Doc (proposed)

Status: proposed (2026-09-25), agreed with the Project Owner in
discussion. No code has been written yet. This document exists to be
reviewed and approved first, following the project convention of
settling a design before touching code for anything that affects
architecture (the same way `docs/diagnose.md` and `docs/ssl-redesign.md`
started).

Once implemented, the contents of this file should be folded into
`DESIGN.md` as new numbered sections and this file deleted.

## Why

The owner works from Windows, macOS, iPadOS and Android, travels often,
and tried a home Mac mini as a server. That failed on power cuts,
unstable internet, and remote desktop between operating systems (the
Thai/English keyboard switch in particular). The working pattern that
came out of the discussion:

- services that others depend on run on a **Linux VM in the cloud**,
  never on the device in hand;
- code is edited over a text protocol (VS Code Remote-SSH, code-server
  in a browser), which keeps input methods local and survives bad
  connections far better than remote desktop;
- a host is managed from **any device through a browser**, which is the
  only thing all four devices have in common.

Decisions taken explicitly by the owner:

1. **Windows is dropped.** It never worked for real: DESIGN.md section
   3's paths are guesses, PM2 was never tested, there is no php-fpm, and
   Smart App Control blocks the unsigned binary before it starts.
2. **Linux is the fully supported host; macOS stays, as a development
   host.** It keeps working as it does today, including the panel.
3. **No desktop GUI.** It cannot run on iPadOS or Android. The browser
   panel covers every device.
4. **WOR HCP moves into this repository** and becomes the standard way
   to manage a wor host from a browser, on Linux and macOS. It stays
   **Go on the backend and wfw-js on the frontend**.

### Considered and rejected

- **Linux-only, removing macOS too.** Proposed during the discussion and
  withdrawn. The macOS code works and is in daily use; it is spread over
  roughly 80 branches in the busiest packages (`hostprovider`, `phpfpm`,
  `dbbackup`), so removing it is a large change with regression risk and
  no gain anyone would notice. The problem that prompted it -- a Mac
  slowing down with many services running -- comes from running
  services on the machine you also work on, and the dev VM solves that
  without deleting anything. macOS also lets the owner build and test
  wor natively.
- **A Node.js rewrite of HCP** (with wfw-node). The panel is the tool
  used when services are broken, so it must not depend on a runtime it
  manages: when Node or PM2 break, a Node panel breaks with them. It
  would also force Node 24 onto hosts that run only PHP or static sites,
  and tie wor to `@mooda/wfw-node`, a private package owned by another
  project.
- **A desktop app** (Wails/Electron/Tauri). No iPadOS or Android.

## Decision 1: support policy

| Platform | wor CLI | HCP |
|---|---|---|
| **Linux, Debian 12 and newer** (amd64, arm64) | full: development and production | yes, systemd |
| **macOS** (amd64, arm64) | development host, as today | yes, LaunchAgent (Decision 3) |
| **Windows** | removed | -- |

### Linux

Debian 12 and newer, with systemd. Ubuntu is no longer promised:
today's `install.sh` accepts it and it may keep working, but it is not
tested and not a reason to keep a code path. RHEL-family stays
unsupported. `install.sh` checks `/etc/os-release` and refuses outside
that range; `wor doctor` warns.

### macOS: a development host

Everything that works on macOS today keeps working, and the existing
macOS branches stay. What changes is the promise:

- macOS is supported for **development**. A Mac serving production
  traffic is not a supported setup: Homebrew services run as
  LaunchAgents, which start only when the user logs in, so after a power
  cut nothing comes back until someone signs in. Making that work
  (LaunchDaemons, boot without login, FileVault) was scoped in the
  discussion and deliberately not taken on.
- `wor doctor` on macOS warns when `WOR_ENV` is `production` (Decision
  6), saying exactly that.
- **No new macOS-only features**, with one deliberate exception: the
  panel's LaunchAgent (Decision 3). New work is designed for Linux
  first; macOS gets it when it comes at no extra cost, and otherwise the
  command says it is Linux-only.

### Windows: what is removed

| Area | Removed |
|---|---|
| Code | the six `*_windows.go` files (`cliapp/diskusage`, `cliapp/permcheck`, `osutil/fsops`, `osutil/osutil`, `phpfpm/access`, `worlock/lockfile`) and the `IsWindows()` branches |
| Signing | `winres/`, `.signpath/`, `docs/code-signing.md`, `public/code-signing/`, the SignPath steps in `.github/workflows/release.yml`, `make_windows_resource` in `scripts/build.sh`, `release.sh --skip-build` (it existed only so signing could sit between build and package) |
| Build matrix | `windows/amd64`; releases carry `linux/{amd64,arm64}` and `darwin/{amd64,arm64}` |
| Installer | the Windows message in `install.sh` becomes "Windows is not supported; v1.0.2-b74 was the last Windows build" |

To stop anyone building a broken Windows binary from source, one file
remains: `cmd/wor/unsupported_os.go` with `//go:build windows`,
referring to an undefined identifier named
`wor_does_not_build_for_windows__use_WSL2`. The build fails with a
message that says what to do, instead of a pile of `syscall` errors or
"build constraints exclude all Go files".

### Developing wor

The owner develops on macOS and Windows.

- **macOS**: `go build`, `go vet` and `go test` run natively, as today.
- **Windows**: inside WSL2 (Debian). `GOOS=linux go vet ./...` also
  works from native Windows for a quick check, but tests need to execute
  the code and wor no longer compiles for Windows.
- **CI** runs the tests on a Linux runner for every push.

### Migration

- **`v1.0.2-b74` is the last Windows build.** It stays on the download
  site permanently, and the release notes and download page say so. No
  Windows machine can be stranded by an upgrade: `wor upgrade` has never
  been available on Windows.
- **macOS machines keep receiving releases** as before.
- **Macs used as production hosts** (on the owner's machine:
  `team.ddns.net`, `team-pma.ddns.net`) are not broken by this change,
  but are outside the promise; `wor doctor` says so. Moving them to a
  Linux VM is recommended and done by hand: `wor source backup`,
  `wor database backup`, recreate the services on the VM, reissue the
  certificates there, then switch DNS. A reissue is needed anyway --
  those renewal configs still record `authenticator = nginx`.

### Private repository

Closing the repository is compatible with everything above: releases
are served from `wor.worapong.com/download`, not GitHub. The one plan
that depended on a public repository, SignPath Foundation code signing,
leaves with Windows. Links to `github.com/team-worapong/wor` in
README.md, TRADEMARK.md and the public site must be removed or pointed
at the website, or they will 404 for everyone outside the team.

### DESIGN.md sections affected

Rewritten as history, not deleted -- the reasoning still explains the
code that remains: section 3 (the Windows paths), 4 (the Windows
elevation half), 5 (Let's Encrypt was "Unix-only"; now there is no
other kind), 18 (code signing: dropped with Windows). Sections 6 and 8
are unchanged: the PM2 fallback for go/python and the pool-user
exception still apply on macOS. Known gaps that mention Windows are
closed.

## Decision 2: HCP lives in this repository

### Layout: one module, two binaries

    cmd/wor/          the CLI (unchanged)
    cmd/wor-hcp/      the panel
    internal/hcp/     app, auth, store, adminui (embedded wfw-js UI)

One `go.mod`. The `go` directive moves from 1.21 to 1.24, because HCP
uses the method/wildcard patterns of `net/http.ServeMux` (Go 1.22+).

The code is **copied, not merged with history**. The wor-hcp repository
is archived read-only, so its history stays readable there; the first
commit here names the wor-hcp commit it was copied from (`2a3146c` at
the time of writing), which is the only link anyone needs to follow a
line back.

**Dependency rule**: third-party modules are allowed only under
`internal/hcp/...` and `cmd/wor-hcp`. The `wor` binary stays standard
library only, as it is today -- Go links only what a binary imports, so
the rule costs nothing at build time. It is enforced by a test, not by
review: `go list -deps ./cmd/wor` must contain no module outside the
standard library and `wor/...`. A rule nobody checks is a rule that
breaks the first time someone is in a hurry.

The panel's current dependencies stay: sqlite, go-qrcode, x/crypto,
x/term, and gopsutil -- the last because `wor health` still has no
per-service CPU or memory on macOS, which is why HCP reads host metrics
itself (`readSystemMetrics`).

### HCP keeps calling `wor` as a separate process

Tempting after the merge, and rejected: importing `internal/cliapp`
into the panel. wor is a short-lived CLI and its internals are built on
that assumption:

- the elevation gate (`osutil.elevationConfirmed` /
  `elevationDeclined`) is process-global and means "asked once for this
  command". In a long-running server, one "yes" would last until the
  panel restarts;
- `worlock` is one lock per process per command;
- two code paths for one action (the CLI's and the panel's) are exactly
  what the `--json` contract of DESIGN.md section 23 exists to avoid.

So the boundary stays `exec wor ... --json`. Two refinements:

- HCP runs **`wor` by absolute path** -- the `wor` installed beside
  `wor-hcp` -- not by looking it up on `PATH`. A service manager's
  `PATH` is not a login shell's (see the LaunchAgent below), and the
  panel must never run a different `wor` than the one it shipped with.
- The minimum-version gate (`supportedWORRelease`, "v1.0.2-b74 or
  newer") becomes a **same-version check**: wor and HCP now ship in one
  release, so `wor version --json` must report the version HCP was
  built with, and a mismatch (a half-finished upgrade) is shown as an
  error on every page. The `"schema"` check stays; it costs nothing and
  still protects against a hand-copied binary.

### What goes away from the standalone HCP

- `installer-hcp.sh`, `wor-hcp upgrade`, `wor-hcp restart`, and the
  `wor-hcp.worapong.com` download site (redirected to
  `wor.worapong.com`). `wor upgrade` and `install.sh` install both
  binaries and restart the panel.
- The hard-coded `/Users/teems/...` default in `sync-wfw-js.sh`; it
  becomes a required `WFW_JS_DIST_DIR`.

### Licensing

wor-hcp has no LICENSE file today; wfw-js is Apache 2.0. Moving the
code here puts it under this repository's Apache 2.0, which is the
owner's decision to make and compatible with wfw-js.

## Decision 3: the panel is a built-in service, `_wor/hcp`

### Why not a normal `go` service

The standalone HCP installs into any Go service. As the standard panel
that breaks down:

- `wor service add --service-type=go` refuses without a Go toolchain
  and runs `go build`. HCP is a prebuilt binary; a host that serves only
  PHP must not need Go to get its panel.
- On macOS a `go` service runs under **PM2**, so the panel would need
  Node and PM2 -- and would go down with them, the failure it exists to
  help with.
- A user-owned service can be removed, renamed or redeployed like any
  other, taking the panel with it.

### Shape (both platforms)

- **Reserved target `_wor/hcp`.** `domainmodel`'s slug rule
  (`^[a-z0-9-]+$`) already rejects a leading underscore, so no user can
  create or collide with it; wor's own code path is allowed to. Because
  it is a registered domain/service with a registered host, the host
  and certificate machinery works unchanged: `wor host`, `wor ssl`, the
  HTTP probes of `wor health` and `wor diagnose`.
- **New internal service type `hcp`** -- not offered by
  `--service-type=` or `wor create`. No build step. The process runs
  the installed `wor-hcp` binary, so upgrading wor upgrades the panel.
- **Process names follow the existing convention**
  (`wor_<domain>_<service>` -> `wor__wor_hcp`), so `wor reset` removes
  the panel with everything else and `wor clean` recognises it as
  registered.
- **Listens on 127.0.0.1 only**, behind the generated vhost like any
  proxied service.
- **Runs as the operator account.** PM2 keeps one process list per unix
  user; a panel running as someone else starts node services under the
  wrong daemon (`reconcilePM2Ownership` exists because of exactly this
  mismatch). On macOS the operator is the login user. On Linux, if
  `wor setup` step 6 left the operator empty, the panel runs as the user
  running setup, and that is recorded.
- **Data** (users, sessions, TOTP secrets, `secret.key`) lives in
  `$WOR_HOME/domains/_wor/hcp/data/`, mode `0700`.

### Process provider per platform

`domainmodel.ProcessProviderFor` already picks a provider per OS; the
`hcp` type adds one row to it.

**Linux: systemd.** A unit written like the go/python units, with:

- `User=` the operator;
- `Environment=HOME=... WOR_HOME=...` set explicitly. systemd gives a
  service no `HOME`; the wor it spawns would then fail to find
  `~/.wor/config`, and PM2 needs `HOME` for its own state. HCP already
  documents this problem for its data directory.

**macOS: a LaunchAgent.** `~/Library/LaunchAgents/wor__wor_hcp.plist`
in the operator's home, loaded into the user's GUI domain:

- installed with `launchctl bootstrap gui/<uid> <plist>`, restarted with
  `launchctl kickstart -k gui/<uid>/wor__wor_hcp`, stopped and removed
  with `launchctl bootout`; status read from
  `launchctl print gui/<uid>/wor__wor_hcp`. None of these needs sudo.
- `RunAtLoad` and `KeepAlive` on, so it starts at login and is
  restarted if it exits.
- **`EnvironmentVariables` sets `PATH`, `HOME` and `WOR_HOME`
  explicitly.** launchd gives an agent a minimal `PATH`
  (`/usr/bin:/bin:/usr/sbin:/sbin`), which contains neither
  `/usr/local/bin` nor `/opt/homebrew/bin` -- so without it, every
  `nginx`, `php-fpm` and `pm2` that wor looks up would be "not
  installed" when run from the panel, and nowhere else. The `PATH`
  written is the one from the `wor setup` run that installed the agent.
- It starts **at login, not at boot.** That matches the support policy:
  a Mac is a development host, and the Homebrew services the panel
  manages start at login too, so a panel that came up earlier would
  have nothing running to manage.

This is a LaunchAgent, not a LaunchDaemon. The LaunchDaemon route
(start at boot without a login, needed for a Mac serving production)
was scoped and rejected with the rest of "macOS as a production host".

Every command that reports on a service's process learns the new
provider: `wor service status`, `wor info`, `wor health`,
`wor diagnose`, and `wor reset` / `wor clean` for removal. On Linux it
is just another systemd unit to them.

### `wor setup`: step 7, optional, default yes

After step 6 (the operator account), on both platforms:

    Install WOR HCP (web control panel)? [Y/n]

On yes it asks what `wor create` asks, reusing the same prompts: host
name, domain type (local/public), hosts-file entry, SSL provider,
HTTPS redirect. On macOS the suggested host is a local name (for
example `hcp.localhost`) with a self-signed certificate. Then it creates
the first administrator immediately (the existing `user create` wizard)
-- a panel with no account is a login page nobody can pass. It ends by
printing the URL; TOTP enrollment happens at first sign-in, as today.

Guards specific to the panel, because it can restart services and later
deploy code:

- a **public** domain requires a certificate; `none` is refused and the
  redirect is forced on;
- it offers to **restrict access to the Tailscale network**
  (`allow 100.64.0.0/10; deny all;`), written as a per-service snippet
  in `_wor/hcp/.wor/nginx/` -- the custom include of DESIGN.md
  section 17, with no new mechanism.

Declining installs nothing. The same step is available later as a
command.

### Commands

    wor hcp install [--import=<domain>/<service>]
    wor hcp remove [--yes]
    wor hcp user create|list|remove|reset-password|reset-authenticator

- `install` is setup's step 7 on its own. `--import` copies the data
  directory from a standalone HCP installation (users, TOTP enrolments,
  `secret.key`), so existing administrators keep their accounts. This
  covers the owner's current macOS installation as well as Linux ones.
  The old service is left in place for the operator to remove.
- `wor hcp user` runs `wor-hcp user ...` against the right data
  directory, so nobody has to `cd` into `_wor/hcp` first.
- `wor service remove _wor/hcp` and `wor domain remove _wor` are
  refused with a pointer to `wor hcp remove`.

## Decision 4: prompts never answer themselves

### The problem

`App.prompt()` (`internal/cliapp/prompts.go`) returns `""` on EOF, and
an empty answer is the default. For `confirmYesDefaultYes` (7 call
sites), `confirmYN` (3) and `promptDefault` (14) the default is an
answer. So any `wor` run with no stdin -- which is how HCP runs it, and
how cron runs it -- **accepts every default-yes question unasked**,
including:

- the elevation gate ("wor needs to run ... with sudo? [Y/n]"), and
- `wor domain remove`'s Backups -> Logs -> **Web Data** sequence, whose
  last answer deletes the domain's files.

This is a live risk in wor today, independent of the panel, which is
why it ships first.

### The rule

1. **EOF is cancel, everywhere.** `prompt()` distinguishes "the user
   pressed Enter" from "there is no user" and the latter aborts the
   command with an error naming the question. Piped answers
   (`printf 'y\n' | wor ...`) keep working: they are lines, not EOF.
2. **`--non-interactive` (or `WOR_NONINTERACTIVE=1`)** makes the first
   prompt fail immediately, without reading stdin, with an error naming
   the question. HCP always passes it. The flag is global: `Run` removes
   it before dispatch, so no subcommand's own parsing sees it.
3. **`sudo` gets `-n` in non-interactive mode**, and wor's own
   confirm-once elevation question is not asked, so a password prompt
   fails at once instead of hanging a panel request until its timeout.

No TTY detection is involved: the trigger is the end of input, not the
kind of file stdin is, which is what keeps piped answers working.

**Implemented 2026-09-25** (`internal/cliapp/prompts.go`), with three
refinements found while doing it:

- **The abort is a panic recovered in `App.Run`**, not an error return.
  A question can be asked from any depth and the ~50 call sites return
  a plain answer; stopping at the question is what Ctrl-C does at that
  moment, and it is safe because wor persists state last (a command
  stopped at a question has not recorded the change it was asking
  about).
- **The elevation question declines instead of aborting.** It is asked
  from deep inside privileged operations whose own error paths restore
  what they changed (the vhost snapshot, the pool rollback); a panic
  would jump past them.
- **Optional follow-ups asked after the work is done use a separate
  helper, `offer`, that skips instead of aborting** -- the deploy offers
  after `source clone` and `rollback`, the php-fpm restart offer (which
  sits between a pool write and its bookkeeping), and "create your first
  website?" at the end of `setup`. Aborting there would report finished
  work as failed, or stop halfway.

The error names the question but not the flag that answers it; a
per-question flag hint would mean annotating every call site, and is
left for when the panel screens need specific ones.

Every command the panel calls must be fully answerable by flags. For
the v1 scope below that already holds (`service start|stop|restart`,
`host test|reload`); later screens add flags where a prompt has none.

## Decision 5: elevation for the panel

### What wor elevates today

`SudoCommand`/`RunPrivileged` wrap: `systemctl` (5 sites), `chown`,
`useradd`/`usermod`/`userdel`, `find`, `certbot`, `sh`, `rm`, `mkdir`,
`cat`, `journalctl`, `<php-fpm> -t`, the web server's own commands via
`hostprovider.runSudo`, and file writes through `WriteFilePrivileged`.
Several of those (`sh`, `rm`, `chown`, `find`, `cat`, writes) are
general-purpose: granting them without a password *is* granting root.
A blanket `NOPASSWD` rule for the panel user is therefore rejected.

### Tiers

- **Tier A -- no elevation.** Every read-only report, and
  start/stop/restart of **node** services (PM2 runs as the operator).
- **Tier B -- narrow, delegated (Linux).** start/stop/restart of `wor_*`
  systemd units, the web server's config test and reload, and
  `php-fpm -t` + reload for pooled php. wor writes
  `/etc/sudoers.d/wor-hcp` containing **exact command lines, one per
  unit** -- never a `wor_*` wildcard, because a sudoers `*` also matches
  spaces and therefore extra arguments. The file is regenerated whenever
  `service add`/`service remove` changes the unit list, validated with
  `visudo -c` before it is moved into place (the same
  validate-then-apply shape as sections 8 and 19), and only written if
  the operator opted in during the HCP step.
- **Tier C -- not delegated.** `service add` (useradd, chown, pool
  files), `ssl issue` (certbot), `host add` (writes under
  `/etc/nginx`), `setup`. The panel does not offer these in v1; where a
  screen would need one, it shows the exact command to run over SSH.

polkit rules for `org.freedesktop.systemd1.manage-units` were
considered for Tier B: they would let `systemctl` run without `sudo`
at all. Rejected because they cannot cover the web server's config test
or `php-fpm -t`, so sudoers would be needed anyway -- and two
delegation mechanisms are two places to audit, with the second one
buying nothing the first cannot do.

### macOS

Homebrew runs nginx, php-fpm and PM2 as the login user, which is also
the user the panel runs as. The expectation is that everything in the
v1 scope is Tier A there and needs no delegation at all; this must be
verified (see below), not assumed. Anything that does turn out to need
sudo on macOS is treated as Tier C: the panel shows the command. No
sudoers file is written on macOS.

A future design can shrink Tier C on Linux -- for example a `wor` group
that owns `sites-available` so vhost writes need no elevation -- but
that changes ownership of files outside WOR_HOME and deserves its own
document.

## Decision 6: `WOR_ENV` is the machine's role, and stops being guessed

The machine's role -- development or production -- is expressed with
the existing `WOR_ENV` (`development` | `production`) rather than a new
key, so there are not two settings that mean nearly the same thing.

That only works if `WOR_ENV` stops being inferred. Today
`internal/config` fills it in when nobody set it
(`inferEnvironmentFromWorHome`, `defaultEnvForOS`), which is exactly
why DESIGN.md section 20 refused to base the HTTPS redirect on it: a
production host left looking like `development` would quietly behave
like one. A role is a **decision**, so it must be stored and never
derived (the rule of DESIGN.md section 21). The change:

- `wor setup` step 1 always writes `WOR_ENV` explicitly, into
  `host.env` beside the operator account -- it describes the machine,
  not the admin running the command.
- Anything that *acts* on the role reads only an explicit value. An
  unset value resolves to **`production`**, the conservative side,
  and `wor doctor` warns that it was never chosen. The two inference
  functions are removed.
- The existing display of the environment (`wor env`, `wor doctor`,
  the help footer) shows "unset (treated as production)" instead of a
  guess.
- On macOS, `production` produces the `wor doctor` warning of
  Decision 1.
- Section 20's decision does not change: the redirect remains a
  per-host setting. `WOR_ENV` becoming explicit removes the *reason* it
  was rejected there, but a per-host choice should still not be driven
  by a machine-wide one.

Effects in v1, deliberately few:

- new php services get `pm = ondemand` on `development` (no idle
  workers; the first request after idling is slower) and today's
  `pm = dynamic` defaults on `production`. `.wor/php-fpm.ini` still
  overrides either.

Existing hosts that relied on inference keep working: the value they
were inferred to has no behavioural effect today, and the only new
effects (the php default, the macOS warning) apply to services created
after the change or are warnings only.

## Panel scope for v1

| Screen | Backing command | Tier |
|---|---|---|
| Dashboard (exists) | `wor health --json` | A |
| System (exists) | `wor version --json`, `wor doctor --json` | A |
| Services: list, start/stop/restart | `wor service status --json`, `wor service start|stop|restart` | A (node, all of macOS), B (Linux systemd) |
| Service detail | `wor info --json` | A |
| Hosts: list, test, reload | `wor host list --json`, `wor host test|reload` | A (list; macOS), B (Linux test/reload) |

CLI prerequisites: `--json` for `service status`, `host list` and
`info`. These are additions to schema 1 (new commands, no existing
field changes), so the schema number does not move. The allowlist in
`supportsJSON` grows by exactly these three, per DESIGN.md section 23's
"one command at a time, when a real screen needs it".

Panel-side requirements:

- **One action at a time.** Mutations go through a single queue in the
  panel. wor's lock is non-blocking, so a second concurrent action
  would fail anyway; a lock held by something else (an SSH session
  mid-deploy) is shown as "busy", not as an error. Note that
  `wor doctor` takes the lock (it can rewrite the default vhost), so the
  System page can be busy too.
- **Live output** for actions over Server-Sent Events -- standard
  library, no websocket dependency.
- **Audit log**: who ran which command, when, exit code, stored in the
  existing SQLite database and shown in the panel.
- **Login throttling** (today there is only a 250 ms delay on failure)
  and an **`Origin` check** on every POST, in addition to the existing
  SameSite=Strict cookies.
- **Web app manifest**, so the panel installs to the home screen on
  iPadOS and Android.

Out of v1: create, deploy, SSL issuance, settings.

## Rollout order

Each step ships on its own and leaves the system working.

1. **Decision 4** (prompts). Independent, fixes a live risk.
2. **Decision 1** (Windows removed, support policy stated, Debian 12+
   check). `v1.0.2-b74` already serves as the last Windows release.
3. **Decision 2** (HCP moves in): behaviour-neutral copy, dependency
   test, absolute `wor` path, same-version check.
4. **Decision 3 on Linux** (`_wor/hcp` under systemd, setup step 7,
   `wor hcp` commands, import).
5. **Decision 3 on macOS** (the LaunchAgent provider and its support in
   status/info/health/diagnose/reset/clean).
6. **The three `--json` additions and the v1 panel screens** (Tier A).
7. **Decision 5** Tier B delegation (Linux).
8. **Decision 6** (`WOR_ENV` explicit).

## Needs verification on a real machine

Per DESIGN.md section 9: behaviour of external tools cannot be settled
by reading code.

Linux (Debian 12 and 13):

- `sudo -n` with exact-line sudoers rules, invoked from a systemd
  service with no TTY (`Defaults requiretty` must be absent).
- The web server's config test under `sudo -n` -- run unprivileged it
  fails on the error log, which is the false positive `docs/diagnose.md`
  already records.
- PM2 driven from the panel's unit: `HOME` set, correct process list,
  `pm2 save` persisting across a reboot.
- A reboot with nobody logged in: every service and the panel come
  back.

macOS:

- The LaunchAgent's `PATH`: from the panel, `wor doctor --json` finds
  the same nginx, php-fpm, PM2 and certbot as it does in a terminal.
- `launchctl kickstart -k` restarts the panel cleanly, and `KeepAlive`
  brings it back after a crash; after a log-out and log-in it starts
  again.
- The v1 actions -- node start/stop/restart, `wor host test`,
  `wor host reload` -- complete from the panel with no sudo prompt
  (Decision 5's macOS expectation).
- `wor hcp install --import` against the owner's current standalone
  installation: sign in afterwards with the same TOTP.

Both:

- `wor hcp install --import` from a Linux standalone installation too.

## Out of scope

- Windows, in any form.
- macOS as a production host: LaunchDaemons, starting at boot without a
  login, FileVault handling.
- Managing several hosts from one panel.
- Panel screens for create, deploy, SSL issuance and settings (each
  needs Decision 4's flags and some needs Tier C; separate designs).
- Running the panel as root.
- A general wor log file (still deferred, as in DESIGN.md section 21).

## Resolved (2026-09-25)

Decided by the Project Owner:

1. **Platforms**: Linux fully supported; macOS kept as a development
   host, including the panel; Windows removed. A Linux-only variant was
   proposed and withdrawn as more change than it was worth.
2. **Panel on macOS**: a LaunchAgent, not PM2.
3. **Panel stack**: Go backend, wfw-js frontend.
4. **Panel installation**: optional in `wor setup`, default yes.
5. **wor-hcp's git history**: copy the code only; archive the old
   repository.
6. **Machine role**: `WOR_ENV`, made explicit.
7. **Supported Linux**: Debian 12 and newer; Ubuntu not promised.
8. **Developing wor**: native on macOS; WSL2 on Windows; tests in CI on
   Linux.
9. **Last Windows release**: `v1.0.2-b74`.
