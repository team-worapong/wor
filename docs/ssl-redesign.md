# SSL redesign -- Design Doc (draft, not yet implemented)

Status: proposed (2026-08-18), agreed with the Project Owner in
discussion; all open questions resolved and both open risks checked
against the real machine the same day (see "Resolved" and "Verified on
the reported machine" near the end). No code has been written yet. This document exists to be
reviewed and approved first, following the project convention of
settling a design before touching code for anything that affects
architecture (the same way `docs/diagnose.md` started).

Once implemented, the contents of this file should be folded into
`DESIGN.md` as new numbered sections and this file deleted.

## Why: three problems, one area

All three were found by reading the current code and by a real
incident on the owner's machine. They are grouped into one proposal
because they touch the same call path (`internal/cliapp/ssl_cmd.go` ->
`buildWriteParams` -> `hostprovider.WriteConfig` -> `Reload`), and
fixing them separately would mean rewriting that path three times.

### Problem 1: nginx never redirects HTTP -> HTTPS, apache always does

`apacheHTTPRedirectBlock()` (`internal/hostprovider/apache.go`) emits a
real redirect into the `*:80` VirtualHost whenever `SSLEnabled` is
true:

    RewriteEngine On
    RewriteRule ^/(.*)$ https://<host>/$1 [R=301,L]

nginx has nothing equivalent. `nginxRedirectBlock()`
(`internal/hostprovider/nginx.go`) only canonicalizes the hostname
(alias -> preferred) and deliberately keeps the original scheme:

    if ($host = "www.example.com") {
        return 301 $scheme://example.com$request_uri;
    }

It is also emitted only when the host has aliases at all
(`if preferred == "" || len(aliases) == 0 { return "" }`).

So the same service behaves differently depending on which web server
is configured, and neither behavior is something the operator chose.
Apache's is not configurable at all.

A complication specific to nginx: the generated vhost is a **single
`server` block listening on both ports** -- `webserver/nginx/http.conf`
has `listen 80;` followed by `{{NGINX_HTTPS_CONFIG}}`, which expands to
`listen 443 ssl;` plus the certificate directives. A naive
`return 301 https://...` in that block would redirect HTTPS requests to
themselves forever. Apache does not have this problem because it
already renders two separate VirtualHosts (`http.conf` +
`{{APACHE_HTTPS_VHOST}}`).

### Problem 2: Let's Encrypt certificates are read from /etc/letsencrypt

`wor ssl issue --provider=letsencrypt` records certbot's own paths into
the host's state file and points the vhost straight at them:

    /etc/letsencrypt/live/<host>/fullchain.pem
    /etc/letsencrypt/live/<host>/privkey.pem

Both are symlinks into `/etc/letsencrypt/archive/<host>/`, where the
real key file is root-only. Whether the web server can read them
therefore depends entirely on which user its **master** process runs
as (the master, not the workers, is what opens certificate files at
config load time):

| Environment                  | master runs as   | can read /etc/letsencrypt |
|------------------------------|------------------|---------------------------|
| Linux, systemd-managed nginx | root             | yes                       |
| macOS, Homebrew nginx        | the login user   | **no**                    |
| non-root master / container  | non-root         | **no**                    |

The macOS case is the same structural problem already documented in
`DESIGN.md` section 8 for php-fpm pools: a service started through
`brew services` is an unprivileged process and simply cannot reach
root-owned paths.

This is the reported incident: the web server could not read
`fullchain.pem`/`privkey.pem`, so the configuration became invalid and
the reload took the whole server down -- not just that one host.

Note that the other two providers do **not** have this problem.
`IssueSelfSigned()` and `InstallCustom()` (`internal/ssl/`) both write
their output into `HostDir(sslRoot, host)` -- i.e. inside WOR_HOME --
and `chmod` the key to `0600`. Let's Encrypt is the only provider that
points outside wor's own tree. It is the odd one out, not the rule.

### Problem 3: the SSL write path reloads without testing

`rewriteHostConfigWithSSL()` and `rewriteHostConfigWithSSLFiles()`
(`internal/cliapp/ssl_cmd.go`) both do:

    if err := provider.WriteConfig(params); err != nil { return err }
    return provider.Reload()

There is no `provider.Test()` between the two, and no rollback of the
written file if the reload fails. The `host add` path already does this
correctly (`internal/cliapp/host.go`, `provider.Test()` before
`provider.Reload()`), and the php-fpm pool code is stricter still --
`DESIGN.md` section 8 states it "validates the resulting config with
`php-fpm -t` *before* touching anything live... If validation fails,
the pool file is rolled back, never leaving a broken config behind to
trip up the next real reload."

The SSL path is the one place that skips the pattern. This is what
turned Problem 2 from "one host has no certificate" into "the web
server is down", because a single invalid vhost blocks the reload for
every site on the machine (`docs/diagnose.md` already relies on this
fact when it checks `nginx -t` as a whole-machine failure).

## Guiding principle: store decisions, derive facts

This proposal stores one new value and deliberately refuses to store
another. The rule that separates them, which should be recorded in
`DESIGN.md` because it looks contradictory otherwise:

- A **decision the user made** must be **stored**, never recomputed.
  Recomputing it means the behavior the user chose can change by
  itself later when an unrelated input changes.
- A **fact about the machine** must be **derived at the moment of
  use**, never stored. Storing it means it goes stale and becomes
  wrong, silently.

`forceHttps` (below) is a decision: stored. The unix owner to give
certificate files to is a fact: derived.

## Decision 1: HTTP -> HTTPS redirect becomes an explicit per-host setting

### What is stored

One new field on the existing per-host state file
(`$WOR_HOME/ssl/hosts/<host>/ssl.json`, `internal/ssl/ssl.go`):

    type State struct {
        Enabled    bool   `json:"enabled"`
        Provider   string `json:"provider"`
        CertFile   string `json:"certFile"`
        KeyFile    string `json:"keyFile"`
        AutoRenew  string `json:"autoRenew"`
        ForceHTTPS bool   `json:"forceHttps"`   // new
    }

It holds the **resolved** answer, not a rule to re-evaluate. Both
providers read the same field, so nginx and apache finally agree.

Implementation note: `WriteState()` currently takes the fields as
positional arguments (`provider, cert, key, autoRenew`). Adding a fifth
makes the call sites unreadable. This is the right moment to change it
to take a `State` value.

### How the default is computed (once, at the point of asking)

The default shown to the user when a certificate is issued:

| condition                              | default |
|----------------------------------------|---------|
| provider `letsencrypt`                 | on      |
| provider `custom`                      | on      |
| provider `self-signed`                 | off     |
| hostname is a local name               | off (overrides the above) |

The reasoning:

- `letsencrypt`/`custom` mean a certificate a browser will actually
  trust, so a redirect is safe and is what the operator wanted by
  issuing it.
- `self-signed` means every visitor gets a warning. Forcing the
  redirect makes that warning unavoidable instead of optional, which
  breaks local development for no benefit.
- A local hostname is off regardless, for the same reason.

`WOR_ENV` was **considered and rejected** as an input. It is a
machine-wide value that `internal/config` can *infer* on its own
(`inferEnvironmentFromWorHome`, `defaultEnvForOS`) when the user never
set it, and deriving security-relevant behavior from an inferred value
is unsafe in an asymmetric way: a production host left at
`WOR_ENV=development` would silently stop redirecting, with nothing
reporting it. The provider field carries the same intent more directly
and is always explicit.

Detecting "is this a local hostname" should key off the hostname
itself (no dot, or a `.local`/`.test`/`.localhost` suffix; possibly
helped by `internal/domainmodel/psl.go`) rather than
`Service.DomainType`. `DomainType` is stored **per service**
(`internal/domainmodel/types.go`) while `Hosts` is a list, so a service
bound to both `app.example.com` and `app.local` has one `domainType`
covering both -- it must be wrong for one of them. `DomainType` may be
used as a secondary hint, never as the sole input.

### Command surface

    wor ssl issue <host> [--redirect|--no-redirect]
    wor ssl redirect <host> on|off
    wor ssl status <host>            (now also prints the redirect state)
    wor ssl remove <host>            (clears it along with the rest of the state)

`ssl issue` prompts interactively with the computed default
pre-selected, so pressing Enter accepts it; the two flags make it
scriptable without a prompt. `ssl redirect` is the "add or remove it
later" path -- it rewrites the vhost and reloads, nothing else.

Because the flag lives with the certificate state, it cannot outlive
the certificate: `ssl remove` deletes the state file, so there is no
way to end up with a redirect pointing at a certificate that is no
longer installed (which would be a guaranteed outage).

### Rendering

- **apache**: `apacheHTTPRedirectBlock()` gains the flag as an input
  instead of keying on `SSLEnabled` alone.
- **nginx**: the template is **split into two `server` blocks**, matching
  apache's shape (decided 2026-08-18):

      server {
          listen 80;
          location /.well-known/acme-challenge/ { root <WOR_HOME>/ssl/acme; }
          ... redirect or serve ...
      }
      server { listen 443 ssl;  ... serve ... }

The ACME `location` comes first in the `:80` block and is never
redirected, so certificate issuance and renewal keep working with the
redirect on (see "Issuance moves to `--webroot`" under Decision 2).

  This was chosen over the smaller alternative -- keeping one block that
  listens on both ports and guarding with
  `if ($scheme = http) { return 301 ...; }` -- because the two-block form
  is nginx's own documented shape, avoids `if` inside a `server` block
  (long-standing source of surprising behavior in nginx), and makes the
  two providers structurally comparable instead of merely
  behaviorally equal.

  Consequences to handle:
  - `webserver/nginx/http.conf` and `nginxProvider.writeConfig()` change
    more than option 1 would have. `{{NGINX_HTTPS_CONFIG}}` stops being
    an inline fragment and becomes the second block.
  - The per-service custom include (`.wor/nginx/*.conf`, `DESIGN.md`
    section 17) goes in the **`:443` block only** when the redirect is
    on -- the `:80` block does nothing but redirect, so a snippet there
    could never run. When the redirect is off, both blocks serve, and
    the include must be emitted in both.
  - That is a **silent behavior change** for anyone who already has a
    snippet and expects it on port 80. It affects no one today (the
    include and the redirect are both new/off by default), but it must
    be stated in `docs/services.md` alongside the existing include
    rules.
  - When a host has no certificate at all, only the `:80` block is
    emitted -- unchanged from today's behavior.

Both providers must keep `/.well-known/acme-challenge/` outside the
redirect. Since issuance moves to `--webroot` (Decision 2), that path is
a real `location`/`Alias` wor emits rather than an exception someone has
to remember to add.

## Decision 2: wor owns a copy of every certificate

Approach chosen after comparing it against granting the web server user
ACL access to `/etc/letsencrypt` directly. The ACL approach was
rejected because it cannot work at all on macOS (Homebrew's nginx runs
unprivileged), and because it means reaching into a directory another
tool owns.

### The model

After certbot succeeds, wor copies the certificate and key into its own
per-host directory and points the vhost at the copy:

    $WOR_HOME/ssl/hosts/<host>/fullchain.pem
    $WOR_HOME/ssl/hosts/<host>/privkey.pem

This is exactly what `IssueSelfSigned()` and `InstallCustom()` already
do. The result is a single ownership model for all three providers
instead of two, and wor controls the permissions rather than inheriting
whatever certbot chose.

### Permissions

`0600`, owned by the operator user. This is sufficient on both
platforms and needs no ACL, no group, and no chown of anything else:

- Linux: the master process runs as root and can read any file.
- macOS: the master process runs as the login user, which *is* the
  operator, and therefore owns the file.

`privkey.pem` must never be made group- or world-readable. Fixing an
outage by exposing a private key to every process on the machine is not
a fix. If a future environment genuinely needs a non-root master under
a different user than the operator, that is a separate design with a
dedicated group -- not a `chmod` widening.

### `wor ssl sync <host>`

A new subcommand that refreshes the copy from the provider's source of
truth. One command, three jobs:

1. It is what the certbot deploy hook calls after each renewal.
2. It is how an operator migrates a host that predates this change
   (see Migration below).
3. It is a manual repair path when the copy and the source have drifted
   for any reason.

Behavior:

- Reads the source paths for the host's provider. For `letsencrypt`
  that is `/etc/letsencrypt/live/<host>/`; for `self-signed` and
  `custom` the copy *is* the source, so sync is a no-op that only
  re-checks permissions.
- **Idempotent**: compares the certificate already in place against the
  source (serial or content) and does nothing -- including no reload --
  when they match. A renewal hook that fires on an unchanged
  certificate must not churn the web server.
- On a real change: copy -> chown to the derived owner -> `chmod 0600`
  -> `provider.Test()` -> `provider.Reload()`, rolling the previous
  files back if the test fails (see Decision 3).
- **Never prompts.** certbot runs hooks non-interactively, so a prompt
  would hang the renewal. Two invocation contexts must both work:
  - from the hook, running as root: `/etc/letsencrypt` is directly
    readable, no elevation needed;
  - manually, running as the operator: reading
    `/etc/letsencrypt/archive/.../privkey.pem` requires elevation, so
    it goes through `osutil.SudoCommand` and its existing confirm-once
    gate. This is the one interactive path, and it is not the hook.

### Deriving the owner

The hook runs from certbot's renewal timer as plain root, with no
`SUDO_USER` in the environment. wor must still work out which user to
give the files to.

**The owner is derived from the ownership of WOR_HOME itself**
(`Stat`, not `Lstat`, since WOR_HOME may be a symlink). WOR_HOME is by
definition the operator's workspace, so its owner is the answer, and it
is correct at the moment it is read.

Baking a numeric uid into the hook command line at issue time was
considered and rejected: a uid recorded months earlier goes stale if
the machine is rebuilt or the account changes, and a stale uid means
chowning a private key to whichever unrelated account now holds that
number. That is a worse failure than the one being fixed. This is the
"derive facts, store decisions" rule from above.

**Required guard**: if WOR_HOME is itself owned by root, `ssl sync`
must **stop and report**, not proceed -- otherwise it would chown the
certificate to root and silently recreate the original problem. This is
not a hypothetical case: `osutil.ClaimOwnership()` already exists
specifically because WOR_HOME has been found root-owned in the field
(its doc comment cites a prior shell-`wor-cli` install as the real
cause). `wor doctor` should grow the same check so the condition is
reported before it breaks a renewal at 03:00.

### Issuance moves from `--nginx`/`--apache` to `--webroot`

Decided 2026-08-18, after the two-block nginx template (Decision 1) made
the conflict visible.

Today `IssueLetsEncrypt()` picks certbot's web-server plugin:

    switch hostProviderName {
    case "nginx":  pluginFlag = "--nginx"
    case "apache": pluginFlag = "--apache"
    }

Those plugins work by **editing the vhost file** -- certbot's own dry-run
output says so ("the temporary nginx configuration changes made by
Certbot"). That is a direct conflict with how wor works: the vhost is
fully regenerated from templates on every write, which is the exact
reasoning `DESIGN.md` section 17 gives for putting user customization
in a separate include instead of in the vhost. Two tools editing one
generated file is a latent bug that currently survives only because wor
happens to rewrite the same SSL directives certbot added.

The two-block split makes it worse in a concrete way: both blocks carry
the same `server_name`, so which one certbot chooses to insert its
challenge `location` into is not something this project should be
depending on. If it picks the `:443` block, an HTTP-01 challenge
arriving on port 80 goes unanswered.

**`--webroot` removes the whole class of problem**: certbot writes a
file into a directory and never touches the vhost. wor stays the sole
owner of every file it generates.

    $WOR_HOME/ssl/acme/.well-known/acme-challenge/

Served by a `location` emitted into every host's `:80` block:

    location /.well-known/acme-challenge/ { root <WOR_HOME>/ssl/acme; }

Consequences, all of them improvements:

- **One code path for both web servers.** The plugin switch disappears,
  and `hostProviderName` drops out of `IssueLetsEncrypt()`'s signature
  entirely.
- **wor performs the reload**, instead of certbot doing it invisibly.
  That routes issuance through the same test-then-reload-then-rollback
  helper as everything else (Decision 3).
- **The ACME path needs no special case against the redirect.** The
  `location` sits above the `return 301` in the same block, so the
  challenge is answered over plain HTTP by construction rather than by
  a carve-out someone has to remember.
- Emitted **unconditionally**, whether or not the host has a
  certificate yet -- a host with no certificate is precisely the host
  about to request one. Same reasoning as the section 17 include:
  always present, no hidden "did you opt in at create time" state.

Three things it introduces that must be designed, not assumed:

1. **certbot runs as root and writes into WOR_HOME.** The challenge
   file is transient and certbot removes it, but a failed run can leave
   a root-owned file behind, and certbot will `mkdir` missing
   directories itself -- reintroducing the root-owned-artifact problem
   section 4 exists to prevent. Mitigation: **wor pre-creates the
   `.well-known/acme-challenge/` tree as the operator** (mode `0755`)
   so certbot only ever writes files inside a directory wor owns, and
   `wor clean` sweeps leftovers.
2. **On Linux the web server user must be able to reach it.** The nginx
   worker (`www-data`) reads the challenge file, so the acme directory
   is subject to the very same traversal requirement `wor doctor`'s
   Security section and `internal/cliapp/permcheck_unix.go` already
   check for service document roots. It must be added to that existing
   check rather than treated as a new problem. macOS is unaffected --
   the worker is the login user.
3. **Existing certificates must be re-issued to switch authenticator.**
   See Migration.

Unchanged: HTTP-01 still requires the CA to reach port 80 from the
internet. `--webroot` is not a workaround for a closed port -- that
would be DNS-01, which is out of scope.

### The deploy hook

Registered at issue time, so certbot re-runs it after every successful
renewal:

    certbot certonly --webroot -w <WOR_HOME>/ssl/acme -d <host> ... \
        --deploy-hook "/usr/local/bin/wor ssl sync <host>"

- The path must be **absolute**, resolved via `os.Executable()` when
  the hook is registered. The hook is stored in
  `/etc/letsencrypt/renewal/<host>.conf` and runs unattended months
  later; `wor` will not be on root's `PATH` in every context. If the
  binary is later moved or replaced, the hook breaks silently -- the
  expiry check below is the net that catches it.
- `install.sh` places the binary at `/usr/local/bin/wor`, which is the
  expected value on Linux. On macOS the binary is wherever the operator
  put it, which is exactly why the path is resolved rather than
  hardcoded.

### macOS is the platform this was reported on

Confirmed 2026-08-18: the incident happened on macOS with nginx under
`brew services`, i.e. an unprivileged master that cannot read
`/etc/letsencrypt`. That settles the migration scope -- Linux hosts
with a root master are working today and only benefit from the
consistency, while **macOS hosts are the broken ones** and are what
`ssl sync` is for.

Two follow-on findings from checking that machine (see "Verified on the
reported machine" below): certbot's config directory is `/etc/letsencrypt`
there exactly as on Linux, so the hardcoded `LetsEncryptCertDir()` is
correct -- but **nothing on the machine schedules renewal at all**.

That last point makes the carve-out below more important, not less: with
no timer, renewal is a hand-typed `sudo certbot renew`, which sets
`SUDO_USER` and hits precisely the refusal the carve-out removes.

### The `IsSudoElevated` carve-out

`App.Run()` (`internal/cliapp/app.go`) currently refuses outright when
`osutil.IsSudoElevated()` reports root **and** `SUDO_USER` is set
(`DESIGN.md` section 4). That check must gain a narrow exemption, or
`sudo certbot renew` typed by hand would set `SUDO_USER`, and the hook
would be rejected and fail. (A renewal from a systemd timer sets no
`SUDO_USER` and already passes, which makes this failure intermittent
and very hard to attribute -- worth fixing precisely because of that.)

Shape of the change:

- Add a third classifier function alongside the two that already exist
  in `app.go`, `commandNeedsLock(cmd, rest)` and
  `requiresInitializedWorkspace(cmd)`. Per-command exemption lists are
  an established pattern in this file, so this is not a new concept --
  and each has a comment explaining every entry, which the new one must
  match.
- The exemption covers `ssl sync` **only**.
- The sudo check currently runs at the very top of `Run()`, before
  `args` is even split. It has to move to just after
  `cmd, rest := args[0], args[1:]` so it can see which subcommand was
  requested. Nothing else about the check changes.
- The justification to record: section 4's stated purpose is preventing
  root-owned artifacts (git clones, `npm install` output, PM2 dumps)
  from being left in WOR_HOME. `ssl sync` writes exactly two files and
  explicitly chowns both to the derived operator, so it upholds that
  purpose rather than evading it. The exemption must stay this narrow;
  it is not a general "root mode" for wor.

## Decision 3: test before reload, roll back on failure

Apply the php-fpm pool pattern (`DESIGN.md` section 8) to every vhost
write, not just `host add`:

1. Keep the existing file contents in memory (or as a sibling backup).
2. Write the new vhost.
3. `provider.Test()`.
4. On pass: `provider.Reload()`. On fail: restore the previous
   contents, do **not** reload, and report the test output plus a fix
   suggestion.

Extended to the SSL paths in `ssl_cmd.go`
(`rewriteHostConfigWithSSL`, `rewriteHostConfigWithSSLFiles`) and to
`ssl sync`. Best done as one shared helper so there is a single place
where "write a vhost safely" is defined.

The rule this encodes: **one host failing to come up is acceptable; the
web server going down for every site is not.** That matches the
existing division of responsibility, where `wor diagnose` never changes
anything and action commands change only what was asked for.

A `wor run` preflight follows from the same rule: before reloading, if
a host has SSL on record, confirm its certificate files are present and
readable. `wor run` currently has no SSL awareness at all
(`internal/cliapp/run.go` only calls `provider.IsRunning()` /
`provider.Start()`).

## Detecting drift when the hook does not fire

Copying the certificate introduces one risk the previous design did not
have: if the hook silently fails, the copy goes stale and the site
serves an **expired** certificate. That is worse than a loud failure,
because nothing reports it until a visitor complains.

Three layers (decided 2026-08-18):

- **Prevention**: the deploy hook (above).
- **Detection of the outcome**: `wor health` reports certificate expiry
  as its existing yellow **Warning** tier -- visible, but exit code
  stays 0 so cron does not false-alarm, exactly as the 404 case already
  works. Nothing new is needed to read the date: `ssl.Status()` already
  has `certExpiration()`, and `cmdHealth`/`cmdDiagnose`
  (`internal/cliapp/diagnose.go`) already parse expiry with
  `crypto/x509` via `certNotAfter()`.
- **Detection of the cause**: every `ssl sync` run records its result
  next to the host's other state, e.g.
  `$WOR_HOME/ssl/hosts/<host>/sync.json`:

      { "at": "...", "ok": false, "source": "...", "error": "..." }

  Read back by `wor ssl status` (one line), by `wor health` (to say
  *why* the certificate is stale rather than only that it is), and by
  `wor diagnose` (as evidence feeding the root-cause synthesis).
  Deleted along with the rest of the host's state on `wor ssl remove`.

The third layer exists because wor has no log of its own, so a hook
that fails at 03:00 would otherwise leave a trace only in the renewal
job's own output, which nobody reads. A per-host result file is the
smallest thing that answers "what happened and when" at the point where
someone is already looking.

Adding a general wor log file (`$WOR_HOME/logs/wor.log`) was considered
and **deliberately deferred**: it is a feature in its own right
(rotation, levels, which commands write) and does not belong inside an
SSL change. If it is built later, `sync.json` is a candidate to fold
into it.

Detection matters more than prevention here, because it catches every
way the hook can fail, including ones not anticipated -- which, on
macOS, includes the hook never being invoked at all (see below).

## Migration

Following the project's established no-forced-migration pattern
(`DESIGN.md` section 8), nothing self-heals and nothing is rewritten
behind the user's back:

- **Existing Let's Encrypt hosts** keep pointing at `/etc/letsencrypt`
  until `wor ssl sync <host>` is run for them. On Linux with a
  root-owned master they are working today and will keep working; on
  macOS -- the confirmed platform of the reported incident -- they are
  the broken ones, and sync is the fix.
- `ssl sync` also registers the deploy hook if the host's renewal
  config does not have one yet, so migrating a host and making it stay
  fixed are the same command.
- **Switching authenticator needs a re-issue, not a sync.** Existing
  renewal configs record `authenticator = nginx`; `ssl sync` only
  refreshes the copy and cannot change that. The clean path is
  `wor ssl issue <host>` again with the new `--webroot` flow, which
  makes certbot rewrite the renewal config itself. Hand-editing
  `/etc/letsencrypt/renewal/<host>.conf` works too but is not what wor
  should be doing on the operator's behalf. On the reported machine
  this affects two hosts (`team.ddns.net`, `team-pma.ddns.net`), and
  cannot be done until port 80 is reachable again.
- **`forceHttps`** is absent from every existing `ssl.json`. Absent
  must mean **off**, so no existing site starts redirecting because of
  an upgrade. Operators opt in per host via `wor ssl redirect <host> on`.
  This is a visible behavior change for apache hosts, which redirect
  unconditionally today -- it must be called out in the release notes,
  not just in this document.
- **Existing vhosts do not regenerate themselves**, the same caveat as
  the PATH_INFO change in `DESIGN.md` section 16. `wor ssl redirect`
  and `wor ssl sync` both rewrite the host they touch.

Explicitly avoided: the situation section 8 records with regret, where
pools created before a fix "do not self-heal and must be removed+added
again manually... There is no lightweight 'repair an existing pool in
place' command yet." `ssl sync` is that lightweight repair command,
present from the start.

## Impacted files

| File | Change |
|------|--------|
| `internal/ssl/ssl.go` | `ForceHTTPS` field; `WriteState` takes a `State`; read/write `sync.json` |
| `internal/ssl/letsencrypt.go` | switch to `--webroot` (drops the `hostProviderName` parameter and the plugin switch); copy cert into the host dir; register the deploy hook; expose the source paths |
| `internal/cliapp/ssl_cmd.go` | `sync` + `redirect` actions; redirect prompt on `issue`; test-then-reload; `status` output |
| `internal/cliapp/host.go` | `buildWriteParams` passes `ForceHTTPS` through |
| `internal/cliapp/app.go` | move the sudo check after arg split; add the `ssl sync` exemption classifier |
| `internal/cliapp/run.go` | certificate-readability preflight before reload |
| `internal/cliapp/doctor.go` | warn when WOR_HOME is root-owned; warn when LE certificates exist with no renewal schedule, and offer to install one |
| `internal/config/config.go` | the acme webroot path alongside the existing `SSL` root |
| `internal/cliapp/diagnose.go` | certificate expiry as a Warning-tier line in `cmdHealth` (there is no separate `health.go`; both commands live here) |
| `internal/cliapp/usage.go` | the new commands and flags (the stated source of truth, per `docs/commands.md`) |
| `internal/hostprovider/provider.go` | `ForceHTTPS` on `WriteParams`; **remove** the unused `SSLChainFile` |
| `internal/hostprovider/nginx.go` | render two `server` blocks; custom include placement |
| `internal/hostprovider/apache.go` | redirect keys off the flag, not `SSLEnabled`; **remove** `apacheSSLChainFileLine()` |
| `internal/templates/assets/webserver/nginx/http.conf` | split into `:80` + `:443` blocks; ACME `location` in `:80` |
| `internal/templates/assets/webserver/apache/http.conf` | ACME `Alias`/`Directory` ahead of the redirect |
| `internal/templates/assets/webserver/apache/https.conf` | **remove** `{{APACHE_SSL_CHAIN_FILE}}` |
| `internal/cliapp/permcheck_unix.go` | include the acme webroot in the existing traversal check |
| `internal/osutil/fsops_unix.go` | `FileOwner(path) (uid, gid int, err error)` |
| `internal/osutil/fsops_windows.go` | matching stub -- the pair already exists for the other fs helpers |
| `docs/commands.md`, `docs/services.md`, `DESIGN.md` | documentation |

Scope note: Let's Encrypt is Unix-only (`DESIGN.md` section 5), so the
whole certificate-copy mechanism is Unix-scoped. Windows keeps
`self-signed`/`custom`, which already live in WOR_HOME and are
unaffected.

## Resolved (2026-08-18)

The four questions this document opened with were decided by the
Project Owner:

1. **Platform of the incident**: macOS, nginx under `brew services`.
   Folded into the deploy-hook section and Migration.
2. **Surfacing a failed hook**: a per-host `sync.json` result file, read
   back by `ssl status` / `health` / `diagnose`. A general wor log file
   was deferred as a separate feature.
3. **nginx redirect shape**: two `server` blocks, matching apache.
4. **`SSLChainFile`**: removed, along with `apacheSSLChainFileLine()`
   and `{{APACHE_SSL_CHAIN_FILE}}`. It is a directive deprecated since
   Apache 2.4.8 and unnecessary when serving `fullchain.pem`.

5. **Issuance authenticator**: `--webroot`, replacing the
   `--nginx`/`--apache` plugins, so certbot never edits a file wor
   generates.

## Verified on the reported machine (2026-08-18)

Checked on the macOS machine where the incident happened. These were
the two risks this document opened as unverifiable by reading code --
the same class `DESIGN.md` section 9 warns about, where "features that
depend on external tools' behavior (pm2, Homebrew, launchd) cannot be
verified by reading code alone".

**Nothing schedules renewal. Confirmed, and it is a real gap.**

    $ sudo crontab -l
    crontab: no crontab for root
    $ launchctl list | grep -i certbot
    (no output)

Homebrew's certbot ships no launchd job, and none was added by hand. The
two certificates on that machine will simply expire; the deploy hook
would never fire on its own. This inverts the priority set out in
"Detecting drift": on this platform **detection is the primary
mechanism and the hook is the secondary one**, not the other way round.

Proposed response, following the precedent `DESIGN.md` section 9
already set for the identical situation with `pm2 startup` -- an
external tool's persistence never registered, so the thing silently
never came back:

- `wor doctor` reports a ⚠ when Let's Encrypt certificates exist but no
  renewal schedule can be found. Warning only; it must not fail the
  exit code, matching how the Security section already behaves.
- Offer to install the schedule, using the same shape as the pm2
  startup flow: show the full command first, then run it through
  `osutil.SudoCommand`'s confirm-once gate. Never install silently.

**certbot's nginx plugin does work on Homebrew. Resolved.**

`certbot renew --dry-run` reached the challenge stage with
`authenticator: nginx` and failed only on `Timeout during connect`
from the CA -- a reachability problem on that network (dynamic DNS was
switched off), not a configuration-discovery problem. Had the plugin
been unable to find `/opt/homebrew/etc/nginx`, it would have failed far
earlier and differently. No `--nginx-server-root` is needed.

This risk is closed rather than fixed, because Decision 2 drops the
plugin for `--webroot` anyway. It is recorded because it also confirms
two things the design does rely on: certbot's config directory on macOS
is `/etc/letsencrypt`, exactly as on Linux (so the hardcoded
`LetsEncryptCertDir()` is right), and `/etc/letsencrypt/renewal/*.conf`
parse cleanly (`0 parse failure(s)`), so rewriting them to add a
deploy hook is realistic.

Still unverified, and worth a `--dry-run` before shipping: that
`--webroot` succeeds end to end on that machine once port 80 is
reachable again.

## Out of scope (deliberately cut)

- **HSTS.** A redirect is reversible; an HSTS header is cached by
  browsers and is not. It deserves its own opt-in design, not a free
  rider on this one.
- **Automatic migration of existing hosts.** Consistent with sections 8
  and 16; `ssl sync` is offered instead.
- **DNS-01 and wildcard certificates.** HTTP-01 via `--webroot` still
  needs port 80 reachable from the internet; DNS-01 is the answer when
  it is not, but it needs provider credentials and a plugin per DNS
  host, which is a feature of its own.
- **Making the web server master run as root on macOS.** Considered and
  rejected for php-fpm pools in section 8 for the same reasons; there
  is no reason to revisit it here now that the copy approach removes
  the need.
- **A general "run wor as root" mode.** The carve-out is one subcommand
  wide and must stay that way.
- **A wor log file.** Deferred to its own design; `sync.json` covers
  the only case this change creates.
