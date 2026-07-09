# wor diagnose -- Design Doc (draft, not yet implemented)

Status: implemented (2026-07-06) in `internal/cliapp/diagnose.go`
(+ small additions in internal/pm2 and internal/systemd)

Second revision (2026-07-06, after testing on a real machine): fixed 3
false positives (relative DocumentRoot, `nginx -t` hitting [emerg] due to
permissions, stat'ing cert files in root-only /etc/letsencrypt) and
switched the verdict to a ranked model -- see the "Verdict" section below,
which has been updated accordingly.

## Goal

When a service goes down, the admin must learn the **cause** and the
**fix command** within a single command run. The goal is to reduce MTTR
(time back online), not to print a pretty status report -- `wor info`
already does that.

Division of labor with existing commands:

- `wor doctor` -- whole-machine health (are runtimes complete and installed
  correctly?)
- `wor info` -- show one service's status (makes no judgments)
- `wor health` -- sweep the whole machine for broken services (end-to-end)
- `wor diagnose` -- find the root cause of a problematic service + suggest
  fixes
- `wor run` -- the recovery command (ensure everything enabled is up)

Workflow when the server is down: health finds what's broken -> diagnose
explains why -> run brings it back.

## Command form

    wor diagnose <host|domain/service>
    wor health

- Same target forms as `wor info`: a single host resolved via
  `Store.ResolveHost`, or a direct `domain/service`
- `wor health` (formerly `wor diagnose --all`; split into its own command
  2026-07-06 -- "diagnose" means analyzing a patient you have already
  identified, while this *finds* who is sick) runs an abbreviated sweep
  over every enabled service: the process/port layer per service + **one
  real HTTP request per service through the web server** (first registered
  host, shown as a sub-line under each service), then summarizes only the
  problematic ones -- for the "the site is down but I don't know which one
  yet" case, e.g. after a reboot.
  The http part is essential, not a bonus: permission/vhost/proxy problems
  never kill a process (real incident 2026-07-06: pool "accepting" while
  nginx returned 403/502). Interpretation: 2xx/3xx = ok, **404 = ok with a
  note** (many APIs have no `/` page -- false FAILs make people stop
  trusting the command), 4xx/5xx/refused/timeout = FAIL.
  The line versus `wor doctor`: doctor = is the machine/runtime ready,
  health = are the services still serving.
- Entirely read-only, **no auto-fix** (consistent with the Safety Rules in
  AGENTS.md) -- only shows copy-pasteable fix commands.
- Exit code: 0 = no problem found, 1 = problem found (usable with
  cron/monitoring)
- Every probe has a timeout (HTTP 5 seconds) -- the whole command must
  finish in ~10-15 seconds, because during a fire the diagnostic tool must
  not be the slow thing itself.

## Diagnostic principle: follow the request path

Check layer by layer from the outside in. **The first failure along the
chain is the most likely root cause.** Layers that cannot be checked
because an earlier layer failed are marked SKIP.

### Layer 1: Config

- service exists in services.config.json, enabled, which template
- entry point actually exists on disk (node/go/python) and is executable
  (go)
- Common FAILs: incomplete deploy, binary not yet built

### Layer 2: DNS / hosts

- does the host resolve (`net.LookupHost`) and does it actually point at
  this machine (compared against the machine's IP / 127.0.0.1 for local
  domains)
- local domain: is the registered entry present in /etc/hosts

### Layer 3: Web server

- is the nginx/apache master running (process + `systemctl is-active` on
  Linux)
- this host's vhost config exists and is enabled
- does `nginx -t` / `apachectl configtest` pass (one broken config blocks
  reload for the whole machine)
- SSL: cert/key files exist, **expiry date** (report days remaining, FAIL
  if already expired, WARN if < 14 days)

### Layer 4: Process (split by the same providers as `wor info`)

pm2 (node on every OS, go/python on macOS):

- is the pm2 daemon itself running -- if the daemon is empty/dead and the
  machine's uptime is low, give the direct verdict "the machine just
  rebooted and pm2 has no boot persistence -- run `wor run`" (a known
  weakness of the system)
- process status: errored / stopped / online
- restart count + uptime: many restarts within 10 minutes = crash loop,
  uptime of a few seconds = just died repeatedly

systemd (go/python on Linux):

- `systemctl show <unit>` reading `ActiveState`, `SubState`, `Result`
  (exit-code / oom-kill / signal / start-limit-hit), `NRestarts`,
  `ExecMainStatus`
- `Result=oom-kill` gives a memory verdict immediately
- `start-limit-hit` = systemd-side crash loop

php-fpm (php with a dedicated pool):

- is that version's master process running, does the pool file exist,
  does the socket actually exist + is it accessible to the web user
- does `php-fpmX.Y -t` pass (using sudo on Linux, as already fixed)
- legacy pools (empty PHPVersion): only check that the endpoint in config
  responds

static: skip this layer (no process)

### Layer 5: Port / Socket

- is any process listening on `svc.Port` (reading /proc/net/tcp on Linux,
  or `lsof -i` as a fallback) and **does the PID match this service's
  process**
- a port stolen by another process = the classic root cause (EADDRINUSE)
  -- show the thief's name/PID directly

### Layer 6: Two-layer HTTP probe (the decider)

1. Hit `http://127.0.0.1:<port>` directly (bypassing the web server)
2. Hit via the real host (per SSL state)

Interpretation:

| Direct | Via host | Conclusion |
|-----|-----------|------|
| OK | OK | service is fine (the problem may be off-machine, e.g. real DNS/firewall) |
| OK | FAIL | problem at the web server / vhost / SSL |
| FAIL | - | the app itself is dead -> see layer 4 + logs |

Status codes via host: 502 = upstream dead, 504 = app hung/slow,
403 = permission (ties into layer 7), 404 = wrong docroot/route

php/static have no port -- probe via host only.

### Layer 7: Filesystem / Permissions

- the same reachability check as `wor info` / `wor doctor` Security (can
  the web user traverse to the docroot) -- reuses that code wholesale
- disk full: if usage of the filesystem holding WOR_HOME is >= 95%, WARN
  loudly (a silent cause of logs/db failing to write)

### Layer 8: Logs -- fetched for you, no hunting required

Log sources per provider: pm2 error log, `journalctl -u <unit> -n 30`, the
pool's php-fpm error log, nginx/apache error log (filtered by host where
possible). Take the last 20-30 lines.

Match known patterns and translate them into a cause + fix:

| pattern | cause | suggestion |
|---------|--------|-------|
| `EADDRINUSE` | port collision | find the port thief / change port |
| `MODULE_NOT_FOUND` / `Cannot find module` | npm install not run | `npm install` in the service dir |
| `permission denied` | file/socket permissions | setfacl/chmod command per context |
| `Out of memory` / oom-kill | not enough RAM | check memory / reduce workers |
| `ENOENT` | missing file/path | check entry point, .env |
| `SSL_ERROR` / `certificate` | cert problem | `wor ssl ...` |

This table is kept as a plain Go slice; adding patterns later is easy
(no plugin system needed -- Simplicity First).

## Verdict: the most important part of the output

Core principle (confirmed by the Project Owner): **"one main problem
(Root Cause) + evidence + fix"** -- the user must never have to interpret
a pile of FAIL entries themselves, because this synthesis is exactly the
value that makes wor diagnose different from running
systemctl/pm2/nginx -t/curl separately.

Mechanism: every FAIL produces a "cause" with a kind (problem family:
proc, perm, port, tls, ...) + a static 3-level confidence
(high/medium/low).

- causes with the same kind from **different layers** merge into one
  (e.g. http 403 + files-blocked = the same permission problem), with the
  higher confidence winning both the confidence and the wording -- this is
  the agreed cross-layer boost for the files+http pair
- a matching log pattern **confirms** an existing cause (pushing
  confidence to high) or **adds the reason** to the process-side cause
  ("app crashes on start -- Cannot find module") instead of appearing as a
  separate entry
- a lone pattern with no layer confirming it = confidence low

Ranked by confidence first, tie-broken by layer order along the request
path.

Final layout (agreed 2026-07-06): **Checks (printed live while checking)
-> Summary -> Evidence -> Suggested fix** -- Summary comes after Checks
deliberately, because check rows print live line by line to show progress
(when everything is down, the probes together can take ~10-15 s, and a
blank screen is worse), and the verdict sits at the very bottom next to
the prompt, which is where the eye lands first.

1. **Summary** -- `Status: FAILED (N fail, M warn)` + **the single Root
   cause** + **Cascade**: the other FAILs *proven* to be the same problem
   (only from kind merging; never guess a causal link -- a wrong claim is
   worse than no claim)
2. **Evidence** -- grouped by source, duplicate lines collapsed as "(xN)"
   (compared with numbers stripped: timestamps/pids/connection ids differ
   on every repeated log line), truncated at ~160 characters, at most 3
   lines/source
3. **Suggested fix** -- a numbered runbook: step 1 fixes the root cause,
   subsequent steps are the remaining independent causes by rank
   ("If it persists -- ..."), closing with a Verify step (re-running
   `wor diagnose <target>`) -- the rule is always fix first, verify after
4. **Also worth checking** -- loose suggestions (cert nearing expiry,
   process flapping, rollback hint)

Special cases worth detecting directly because they are common and fast to
recover:

- machine just rebooted + pm2 empty -> `wor run`
- service died shortly after the latest deploy (comparing the process
  death time against the latest git commit / deploy) -> suggest
  `wor rollback <target>`
- cert expired -> point at the ssl renew command
- every layer passes -> say plainly "the machine looks fine; the problem
  is probably external (real DNS, firewall, CDN)" rather than staying
  silent

## Example output

    $ wor diagnose api.example.com

    WOR Diagnose
    ------------
    Target : example.com/api
    Host   : api.example.com  [ssl: letsencrypt]
    Runtime: node v20.11.1
    Server : nginx (nginx/1.24.0)

    Checks
    ------------------------------------------------
    [PASS] config    enabled (node, port 3100), entry app.js found
    [PASS] dns       api.example.com -> 127.0.0.1 (this machine)
    [PASS] nginx     running, vhost ok, config test ok
    [PASS] ssl       letsencrypt cert valid (45d left)
    [FAIL] process   pm2 status: errored (15 restarts)
    [SKIP] port      (process not running)
    [FAIL] http-app  direct 127.0.0.1:3100 -> connection refused
    [FAIL] http-host via api.example.com :443 -> 502
    [WARN] logs      1 known error pattern(s) in pm2 error log -- see evidence below

    Summary
    ------------------------------------------------
    Status: FAILED (3 fail, 1 warn)

    Root cause:
      app crashes on start (pm2 gave up restarting it) -- Node.js dependency missing (Cannot find module)

    Cascade (same problem, seen from other layers):
      - http-app: direct 127.0.0.1:3100 -> connection refused
      - http-host: via api.example.com :443 -> 502

    Evidence
    ------------------------------------------------
      pm2 error log:
        Error: Cannot find module 'express'

    Suggested fix (run yourself -- wor diagnose never changes anything)
    ------------------------------------------------
    1. Fix the root cause -- app crashes on start (...) -- Node.js dependency missing:
         wor service logs example.com/api
         wor service restart example.com/api
         cd <WOR_HOME>/domains/example.com/api && npm install && wor service restart example.com/api
    2. Verify:
         wor diagnose example.com/api

    exit status 1

Note how three FAIL layers (process, http-app, http-host) are synthesized
into a single Root cause: http-app/http-host share the kind "proc" and so
merge into the process layer's cause (then appear in Cascade with their
originating layer noted), and the log pattern adds the reason
"Cannot find module" into the root cause's wording.

## Implementation approach (impact summary)

- new file `internal/cliapp/diagnose.go` + subcommand in `app.go`/
  `usage.go` -- existing workflows untouched
- reuses nearly everything existing: target resolution from `info.go`,
  process status from the `pm2`/`systemd`/`phpfpm` packages, reachability
  from `doctor.go`, SSL state from `ssl.LoadState`
- the only truly new pieces: HTTP probe (plain net/http), port-listener
  lookup, cert expiry parsing (crypto/x509), the log pattern table -- all
  using the Go standard library (per the Dependency Policy)
- internal structure: each check is a func returning
  `{status, label, detail, fix}`, run in layer order, results collected
  into a slice, then rendered + verdict summarized at the end -- adding a
  new check = adding one func
- cross platform: Linux gets every layer, macOS skips systemd/reachability
  under the same conditions as info/doctor, Windows checks what it can
  (config/process/port/http) and says clearly what was skipped

## Out of scope (deliberately cut)

- auto-fix / a hands-on `--fix` -- too risky while production is down
- watch mode / continuous monitoring -- use cron + exit codes instead
- `--json` -- add later if a monitoring system needs it; doesn't block v1
- performance analysis (slow but not down) -- a different problem from
  "down"
