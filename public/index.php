<?php
// WOR Host — landing page
$latestVersion = 'v1.0.0-b32';

// Sortable key: v1.0.0-b32 -> [1,0,0,32]; final releases outrank betas.
function versionKey(string $tag): array {
    if (!preg_match('/^v(\d+)\.(\d+)\.(\d+)(?:-?b(\d+))?/i', $tag, $m))
        return [0, 0, 0, 0];
    return [(int) $m[1], (int) $m[2], (int) $m[3], isset($m[4]) && $m[4] !== '' ? (int) $m[4] : PHP_INT_MAX];
}

$releasesDir = __DIR__ . '/download/releases';
if (is_dir($releasesDir)) {
    $best = null;
    foreach (scandir($releasesDir) as $f) {
        if (preg_match('/^(v\d+\.\d+\.\d+(?:-?[A-Za-z0-9.]+)?)\.(?:tar\.gz|zip|tgz)$/', $f, $m)) {
            if ($best === null || (versionKey($m[1]) <=> versionKey($best)) > 0) {
                $best = $m[1];
            }
        }
    }
    if ($best !== null)
        $latestVersion = $best;
}
?>
<!doctype html>
<html lang="en" data-bs-theme="auto">
    <head>
        <!-- Google tag (gtag.js) -->
        <script async src="https://www.googletagmanager.com/gtag/js?id=G-VPT5GEM34V"></script>
        <script>
            window.dataLayer = window.dataLayer || [];
            function gtag() {
                dataLayer.push(arguments);
            }
            gtag('js', new Date());
            gtag('config', 'G-VPT5GEM34V');
        </script>
        <meta charset="utf-8">
        <meta name="viewport" content="width=device-width, initial-scale=1">
        <title>WOR Host — One workflow for web applications</title>
        <meta name="description" content="WOR is a cross-platform host manager for web applications: Node.js/PHP/Go/Python services, static sites, nginx/apache hosts, SSL, and database backups — One workflow for web applications.">
        <link href="https://cdn.jsdelivr.net/npm/bootstrap@5.3.8/dist/css/bootstrap.min.css" rel="stylesheet" integrity="sha384-sRIl4kxILFvY47J16cr9ZwB07vP4J8+LH7qKQnuqkuIAvNWLzeN8tE5YBujZqJLB" crossorigin="anonymous">
        <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/bootstrap-icons@1.13.1/font/bootstrap-icons.min.css">
        <style>
            :root {
                --wor-accent: #6366f1;
                --wor-accent-2: #22d3ee;
            }
            body {
                display: flex;
                flex-direction: column;
                min-height: 100vh;
            }
            main {
                flex: 1;
            }
            .hero {
                background:
                    radial-gradient(1100px 500px at 85% -10%, rgba(99,102,241,.28), transparent 60%),
                    radial-gradient(900px 420px at 0% 110%, rgba(34,211,238,.18), transparent 60%);
            }
            .text-gradient {
                background: linear-gradient(90deg, var(--wor-accent), var(--wor-accent-2));
                -webkit-background-clip: text;
                background-clip: text;
                color: transparent;
            }
            .code-block {
                position: relative;
                border-radius: .75rem;
                overflow: hidden;
                background: #0f172a;
                border: 1px solid rgba(255,255,255,.08);
            }
            .code-block pre {
                margin: 0;
                padding: 1rem 3.25rem 1rem 1.25rem;
                color: #e2e8f0;
                font-size: .875rem;
                overflow-x: auto;
                white-space: pre;
            }
            .code-block .prompt {
                color: #22d3ee;
                user-select: none;
            }
            .code-block .cmt {
                color: #64748b;
            }
            .btn-copy {
                position: absolute;
                top: .5rem;
                right: .5rem;
                color: #94a3b8;
                border: 1px solid rgba(255,255,255,.15);
                background: rgba(255,255,255,.05);
            }
            .btn-copy:hover {
                color: #fff;
                border-color: rgba(255,255,255,.4);
            }
            .feature-icon {
                width: 3rem;
                height: 3rem;
                border-radius: .85rem;
                font-size: 1.35rem;
                display: inline-flex;
                align-items: center;
                justify-content: center;
                background: linear-gradient(135deg, rgba(99,102,241,.15), rgba(34,211,238,.15));
                color: var(--wor-accent);
            }
            [data-bs-theme="dark"] .feature-icon {
                color: #a5b4fc;
            }
            .card-hover {
                transition: transform .15s ease, box-shadow .15s ease;
            }
            .card-hover:hover {
                transform: translateY(-3px);
                box-shadow: 0 .5rem 1.5rem rgba(0,0,0,.12);
            }
            .step-num {
                width: 2rem;
                height: 2rem;
                border-radius: 50%;
                flex: 0 0 auto;
                display: inline-flex;
                align-items: center;
                justify-content: center;
                background: var(--wor-accent);
                color: #fff;
                font-weight: 600;
                font-size: .9rem;
            }
            .nav-anchor {
                scroll-margin-top: 80px;
            }
            /* Let flex items shrink below content width so code blocks scroll instead of overflowing the viewport */
            .step-num + .flex-grow-1 {
                min-width: 0;
            }
            /* Terminal mockup colors -- mirror the CLI's own ANSI palette
               (see internal/cliapp/statusview.go): pink group headers, cyan
               config marks, green/yellow/red health, dim secondary text. */
            .tm-h    {
                color: #f472b6;
            }  /* ansiPink   -- group headers */
            .tm-cfg  {
                color: #22d3ee;
            }  /* ansiBlue (cyan) -- config ✓, never health */
            .tm-ok   {
                color: #4ade80;
            }  /* ansiGreen  */
            .tm-warn {
                color: #facc15;
            }  /* ansiYellow */
            .tm-err  {
                color: #f87171;
            }  /* ansiRed    */
            .tm-dim  {
                color: #64748b;
            }  /* ansiDim    */
            /* Donate section: animated build demo + fuel gauge */
            #fuelDemo {
                min-height: 30rem; /* reserve space for all lines so the page doesn't jump while "building" */
            }
            .refuel-link {
                color: #facc15;
                text-decoration: underline dotted;
                text-underline-offset: .25em;
            }
            .refuel-link:hover {
                color: #fde047;
                text-decoration-style: solid;
            }
            .fuel-cursor {
                animation: fuel-blink 1s steps(1) infinite;
            }
            @keyframes fuel-blink {
                50% { opacity: 0; }
            }
            .fuel-bar {
                display: inline-block;
                width: 180px;
                height: 14px;
                vertical-align: middle;
                background: repeating-conic-gradient(#475569 0% 25%, transparent 0% 50%) 0 0 / 8px 8px;
            }
            .fuel-fill {
                display: block;
                height: 100%;
                background: #e2e8f0;
                animation: fuel-fill 2.4s ease-in-out infinite alternate;
            }
            @keyframes fuel-fill {
                from { width: 25%; }
                to   { width: 85%; }
            }
        </style>
    </head>
    <body>

        <nav class="navbar navbar-expand-md sticky-top border-bottom bg-body-tertiary">
            <div class="container">
                <a class="navbar-brand fw-bold" href="/"><i class="bi bi-hdd-stack me-2 text-gradient"></i>WOR</a>
                <button class="navbar-toggler" type="button" data-bs-toggle="collapse" data-bs-target="#nav" aria-controls="nav" aria-expanded="false" aria-label="Toggle navigation">
                    <span class="navbar-toggler-icon"></span>
                </button>
                <div class="collapse navbar-collapse" id="nav">
                    <ul class="navbar-nav ms-auto align-items-md-center gap-md-1">
                        <li class="nav-item"><a class="nav-link" href="#features">Features</a></li>
                        <li class="nav-item"><a class="nav-link" href="#why">Why WOR?</a></li>
                        <li class="nav-item"><a class="nav-link" href="#demo">Demo</a></li>
                        <li class="nav-item"><a class="nav-link" href="/docs/"><i class="bi bi-book me-1"></i>Docs</a></li>
                        <li class="nav-item"><a class="nav-link" href="/download/"><i class="bi bi-download me-1"></i>Downloads</a></li>
                        <li class="nav-item ms-md-2">
                            <button class="btn btn-outline-secondary btn-sm" id="themeToggle" title="Toggle theme" aria-label="Toggle theme">
                                <i class="bi bi-circle-half"></i>
                            </button>
                        </li>
                    </ul>
                </div>
            </div>
        </nav>

        <main>
            <!-- Hero -->
            <section class="hero py-5">
                <div class="container py-md-5">
                    <div class="row justify-content-center text-center">
                        <div class="col-lg-9">
                            <span class="badge rounded-pill text-bg-primary bg-opacity-75 mb-3">
                                <i class="bi bi-tag me-1"></i>Latest release: <?= htmlspecialchars($latestVersion) ?>
                            </span>
                            <h1 class="display-4 fw-bold mb-3">
                                Manage web applications with <div class="text-gradient">WOR Host</div>
                            </h1>
                            <p class="lead text-body-secondary mb-4">
                                One consistent workflow for services, hosts, SSL certificates, and backups across Node.js, PHP, Go, Python, and static web applications on Linux, macOS, and Windows.
                            </p>
                            <div class="d-flex flex-wrap gap-2 justify-content-center mb-4">
                                <a href="https://github.com/team-worapong/wor" target="_blank" rel="noopener" class="text-decoration-none">
                                    <img src="https://img.shields.io/github/stars/team-worapong/wor?style=for-the-badge&logo=github" alt="GitHub Stars">
                                </a>
                                <a href="https://github.com/team-worapong/wor/network/members" target="_blank" rel="noopener" class="text-decoration-none">
                                    <img src="https://img.shields.io/github/forks/team-worapong/wor?style=for-the-badge&logo=github" alt="GitHub Forks">
                                </a>
                                <a href="https://github.com/team-worapong/wor/blob/main/LICENSE" target="_blank" rel="noopener" class="text-decoration-none">
                                    <img src="https://img.shields.io/github/license/team-worapong/wor?style=for-the-badge" alt="License">
                                </a>
                            </div>
                            <div class="d-flex flex-column flex-sm-row gap-2 justify-content-center mb-4">
                                <a href="#install" class="btn btn-primary btn-lg"><i class="bi bi-lightning-charge me-1"></i>Install now</a>
                                <a href="/download/" class="btn btn-outline-secondary btn-lg"><i class="bi bi-box-seam me-1"></i>Browse releases</a>
                            </div>
                            <div class="code-block text-start mx-auto" style="max-width: 640px;">
                                <pre><span class="prompt">$</span> curl -fsSL https://wor.worapong.com/download/installer.sh | bash</pre>
                                <button class="btn btn-sm btn-copy" data-copy="curl -fsSL https://wor.worapong.com/download/installer.sh | bash" aria-label="Copy"><i class="bi bi-clipboard"></i></button>
                            </div>
                        </div>
                    </div>
                </div>
            </section>

            <!-- Features -->
            <section id="features" class="py-5 nav-anchor">
                <div class="container">
                    <div class="text-center mb-5">
                        <h2 class="fw-bold">Everything you need to run web applications</h2>
                        <p class="text-body-secondary">Built for developers who manage their own servers.</p>
                    </div>
                    <div class="row g-4">
                        <div class="col-md-6 col-lg-4">
                            <div class="card h-100 card-hover border-0 shadow-sm">
                                <div class="card-body">
                                    <div class="feature-icon mb-3"><i class="bi bi-boxes"></i></div>
                                    <h5 class="card-title">Built-in templates</h5>
                                    <p class="card-text text-body-secondary">static, node, go, python and php — scaffolded with a starter source tree and wired to the right process manager (PM2, systemd or PHP-FPM) automatically. Dedicated PHP-FPM pools on Linux.</p>
                                </div>
                            </div>
                        </div>
                        <div class="col-md-6 col-lg-4">
                            <div class="card h-100 card-hover border-0 shadow-sm">
                                <div class="card-body">
                                    <div class="feature-icon mb-3"><i class="bi bi-globe2"></i></div>
                                    <h5 class="card-title">Host management</h5>
                                    <p class="card-text text-body-secondary">Generates and enables Nginx and Apache vhosts, tests configuration before reload, and can manage your local hosts file for development domains.</p>
                                </div>
                            </div>
                        </div>
                        <div class="col-md-6 col-lg-4">
                            <div class="card h-100 card-hover border-0 shadow-sm">
                                <div class="card-body">
                                    <div class="feature-icon mb-3"><i class="bi bi-rocket-takeoff"></i></div>
                                    <h5 class="card-title">Deploy &amp; rollback</h5>
                                    <p class="card-text text-body-secondary">Pull from git, install dependencies, rebuild and restart with one command — and roll back to the previous release when something goes wrong.</p>
                                </div>
                            </div>
                        </div>
                        <div class="col-md-6 col-lg-4">
                            <div class="card h-100 card-hover border-0 shadow-sm">
                                <div class="card-body">
                                    <div class="feature-icon mb-3"><i class="bi bi-shield-lock"></i></div>
                                    <h5 class="card-title">SSL certificates</h5>
                                    <p class="card-text text-body-secondary">Issue, renew and install certificates per host — Let's Encrypt, self-signed, or bring your own cert and key.</p>
                                </div>
                            </div>
                        </div>
                        <div class="col-md-6 col-lg-4">
                            <div class="card h-100 card-hover border-0 shadow-sm">
                                <div class="card-body">
                                    <div class="feature-icon mb-3"><i class="bi bi-database-down"></i></div>
                                    <h5 class="card-title">Database backups</h5>
                                    <p class="card-text text-body-secondary">MySQL/MariaDB, PostgreSQL, SQL Server and SQLite backup profiles with Go-native gzip compression — no extra tooling needed.</p>
                                </div>
                            </div>
                        </div>
                        <div class="col-md-6 col-lg-4">
                            <div class="card h-100 card-hover border-0 shadow-sm">
                                <div class="card-body">
                                    <div class="feature-icon mb-3"><i class="bi bi-heart-pulse"></i></div>
                                    <h5 class="card-title">Health &amp; diagnostics</h5>
                                    <p class="card-text text-body-secondary"><code>wor health</code> sweeps the whole fleet, <code>wor diagnose</code> does read-only root-cause analysis on a broken service, and <code>wor doctor</code> checks the machine itself.</p>
                                </div>
                            </div>
                        </div>
                    </div>
                </div>
            </section>

            <!-- Why WOR -->
            <section id="why" class="py-5 bg-body-tertiary nav-anchor">
                <div class="container">
                    <div class="row justify-content-center">
                        <div class="col-lg-8">
                            <div class="text-center mb-4">
                                <h2 class="fw-bold">Why WOR?</h2>
                                <p class="text-body-secondary mb-0">Same servers, same tools underneath — one consistent workflow on top.</p>
                            </div>
                            <div class="table-responsive">
                                <table class="table table-hover align-middle mb-2">
                                    <thead>
                                        <tr><th>Traditional workflow</th><th>WOR workflow</th></tr>
                                    </thead>
                                    <tbody>
                                        <tr><td>pm2 / systemctl / php-fpm commands per service</td><td><code>wor service</code></td></tr>
                                        <tr><td>editing nginx/apache vhosts by hand</td><td><code>wor host</code></td></tr>
                                        <tr><td>certbot + renewal cron</td><td><code>wor ssl</code></td></tr>
                                        <tr><td>git pull + install deps + rebuild + restart</td><td><code>wor deploy</code></td></tr>
                                        <tr><td>mysqldump + gzip + retention scripts</td><td><code>wor database backup</code></td></tr>
                                        <tr><td>curl + grep in a loop</td><td><code>wor health</code></td></tr>
                                        <tr><td>log hunting across five files</td><td><code>wor diagnose</code></td></tr>
                                    </tbody>
                                </table>
                            </div>
                            <p class="text-body-secondary small mb-0">WOR builds on the tools you already trust. It doesn’t replace them—it gives them one consistent workflow.</p>
                        </div>
                    </div>
                </div>
            </section>

            <!-- Install -->
            <section id="install" class="py-5 nav-anchor">
                <div class="container">
                    <div class="row justify-content-center">
                        <div class="col-lg-9 text-center">
                            <h2 class="fw-bold mb-1">Install in under a minute</h2>
                            <p class="text-body-secondary mb-4">One static Go binary, no runtime dependencies. The one-liner supports Debian/Ubuntu.</p>
                            <div class="code-block text-start mx-auto mb-4" style="max-width: 640px;">
                                <pre><span class="prompt">$</span> curl -fsSL https://wor.worapong.com/download/installer.sh | bash</pre>
                                <button class="btn btn-sm btn-copy" data-copy="curl -fsSL https://wor.worapong.com/download/installer.sh | bash" aria-label="Copy"><i class="bi bi-clipboard"></i></button>
                            </div>
                            <p class="text-body-secondary mb-0">
                                Installing on macOS or Windows, or want the step-by-step guide?
                                <a href="/docs/">Read the docs</a> — install, quick start, and the full command reference.
                            </p>
                        </div>
                    </div>
                </div>
            </section>

            <!-- See it in action -->
            <section id="demo" class="py-5 bg-body-tertiary nav-anchor">
                <div class="container">
                    <div class="row justify-content-center">
                        <div class="col-lg-9">
                            <h2 class="fw-bold mb-1">See it in action</h2>
                            <p class="text-body-secondary mb-4">Three commands, one story: <code>wor service status</code> (what's configured &amp; running) &rarr; <code>wor health</code> (who's broken) &rarr; <code>wor diagnose</code> (why, and how to fix it).</p>

                            <ul class="nav nav-pills mb-3" id="demoTabs" role="tablist">
                                <li class="nav-item" role="presentation">
                                    <button class="nav-link active font-monospace" data-bs-toggle="pill" data-bs-target="#demo-status" type="button" role="tab" aria-selected="true">wor service status</button>
                                </li>
                                <li class="nav-item" role="presentation">
                                    <button class="nav-link font-monospace" data-bs-toggle="pill" data-bs-target="#demo-health" type="button" role="tab" aria-selected="false">wor health</button>
                                </li>
                                <li class="nav-item" role="presentation">
                                    <button class="nav-link font-monospace" data-bs-toggle="pill" data-bs-target="#demo-diagnose" type="button" role="tab" aria-selected="false">wor diagnose</button>
                                </li>
                            </ul>

                            <div class="tab-content">

                                <div class="tab-pane fade show active" id="demo-status" role="tabpanel">
                                    <p class="text-body-secondary small mb-2">Per-provider process view. The <span class="text-info">blue ✓</span> means <em>enabled in config</em> — deliberately not a green health dot; live process state is the last column, and the footer says what this command doesn't check.</p>
                                    <div class="code-block">
                                        <pre><span class="prompt">$</span> wor service status

<span class="tm-h">PM2 (node)</span>
  <span class="tm-cfg">✓</span>   com-example/api     :3001   pid 41230  2d 4h   online
      <span class="tm-dim">wor_com-example_api          cpu 0.4%    mem 86mb</span>
  <span class="tm-err">✗</span>   com-example/queue   :3005                      <span class="tm-dim">disabled</span>

<span class="tm-h">SYSTEMD (go/python)</span>
  <span class="tm-cfg">✓</span>   com-example/worker   :3102   pid 40988   active
      <span class="tm-dim">wor_com-example_worker       cpu 1.2%    mem 24mb</span>

<span class="tm-h">PHP-FPM (php)</span>
  <span class="tm-cfg">✓</span>   com-shop/webapp   n/a      running (php 8.4 pool)

<span class="tm-h">STATIC (no process)</span>
  <span class="tm-cfg">✓</span>   com-example/landing   n/a      served by web server

<span class="tm-dim">(process status only -- for end-to-end health: wor health)</span></pre>
                                    </div>
                                </div><!-- /demo-status -->

                                <div class="tab-pane fade" id="demo-health" role="tabpanel">
                                    <p class="text-body-secondary small mb-2">Fleet-wide sweep: host resources up top, one card per enabled service — process check, live cpu/mem, and one <em>real HTTP request</em> through the web server. Notice <code>com-shop/webapp</code>: the pool is up, yet requests fail — process status alone would never catch this. Exit code drives cron.</p>
                                    <div class="code-block">
                                        <pre><span class="prompt">$</span> wor health

WOR Health (4 enabled services)
--------------------------------------------
Host CPU    : 9% (4 cores)
Host Memory : 2.1G / 7.8G (27%)
Disk Usage  : 17.6G / 77.9G (23%)
--------------------------------------------

<span class="tm-ok">●</span> com-example/api
    Status : Online
    Runtime: pm2 (node)
    CPU    : 0.4%
    Memory : 86.3M (1.1%)
    Uptime : 2d 4h
    <span class="tm-ok">✓</span> https://api.example.com -> 200

<span class="tm-warn">●</span> com-example/worker
    Status : Online
    Runtime: systemd (go)
    CPU    : 1.2%
    Memory : 24.6M (0.3%)
    <span class="tm-dim">ℹ</span> no host registered -- HTTP not probed

<span class="tm-err">●</span> com-shop/webapp
    Status : Online
    Runtime: php-fpm 8.4 (dedicated pool, 3 workers)
    CPU    : 0.2%
    Memory : 118.4M (1.5%)
    <span class="tm-err">✗</span> https://shop.example.com -> 502

<span class="tm-ok">●</span> com-example/landing
    Status : Online (served by web server)
    Runtime: static
    <span class="tm-ok">✓</span> https://www.example.com -> 200

--------------------------------------------
Healthy : 2
Warning : 1
Failed  : 1
    wor diagnose com-shop/webapp   # root cause + fix
To bring everything enabled back up: wor run</pre>
                                    </div>
                                </div><!-- /demo-health -->

                                <div class="tab-pane fade" id="demo-diagnose" role="tabpanel">
                                    <p class="text-body-secondary small mb-2">Read-only root-cause analysis of the failure <code>wor health</code> found: layered checks print live, then the summary names <em>one</em> root cause with evidence and a numbered runbook. Diagnose recommends — it never changes anything itself.</p>
                                    <div class="code-block">
                                        <pre><span class="prompt">$</span> wor diagnose shop.example.com

WOR Diagnose
------------
Target : com-shop/webapp
Host   : shop.example.com  [ssl: letsencrypt]
Runtime: php 8.4 (dedicated php-fpm pool)
Server : nginx (nginx/1.24.0)

Checks
------------------------------------------------
<span class="tm-ok">[PASS]</span> config    enabled (php), document root found
<span class="tm-ok">[PASS]</span> dns       shop.example.com -> 203.0.113.10 (this machine)
<span class="tm-ok">[PASS]</span> nginx     running, vhost ok, config test ok
<span class="tm-ok">[PASS]</span> ssl       letsencrypt cert valid (58d left)
<span class="tm-err">[FAIL]</span> process   pool is up but its socket denies the web server user (www-data)
<span class="tm-err">[FAIL]</span> http-host via shop.example.com :443 -> 502
<span class="tm-ok">[PASS]</span> files     reachable by web server user (www-data)
<span class="tm-ok">[PASS]</span> disk      23% used
<span class="tm-warn">[WARN]</span> logs      1 known error pattern(s) in nginx error log -- see evidence below

Summary
------------------------------------------------
Status: FAILED (2 fail, 1 warn)

Root cause:
  www-data cannot connect to this pool's socket -- listen.owner/listen.group in the pool config don't include the web server user, so every request through nginx gets 502

Evidence
------------------------------------------------
  php-fpm socket:
    /run/php/wor_com-shop_webapp.sock is deploy:deploy 0660 -- www-data cannot connect
  nginx error log:
    2026/07/09 14:02:17 [crit] 8412#8412: *3 connect() to unix:/run/php/wor_com-shop_webapp.sock failed (13: Permission denied) (x14)

Suggested fix (run yourself -- wor diagnose never changes anything)
------------------------------------------------
1. Fix the root cause -- www-data cannot connect to this pool's socket -- listen.owner/listen.group in the pool config don't include the web server user, so every request through nginx gets 502:
     sudo sed -i 's/^listen.owner = .*/listen.owner = www-data/; s/^listen.group = .*/listen.group = www-data/' /etc/php/8.4/fpm/pool.d/wor_com-shop_webapp.conf && sudo systemctl restart php8.4-fpm
     check ownership/permissions under /opt/wor/domains/com-shop/webapp; wor doctor's Security section shows the exact fix
2. If it persists -- the web server is up but cannot reach the app behind it (502):
     wor service restart com-shop/webapp
3. Verify:
     wor diagnose com-shop/webapp</pre>
                                    </div>
                                </div><!-- /demo-diagnose -->

                            </div><!-- /tab-content -->
                        </div>
                    </div>
                </div>
            </section>

            <!-- CTA -->
            <section class="py-5 text-center">
                <div class="container">
                    <h2 class="fw-bold mb-3">Ready to try it?</h2>
                    <p class="text-body-secondary mb-4">Install in under a minute, or grab a release archive.</p>
                    <div class="d-flex flex-column flex-sm-row gap-2 justify-content-center mb-5">
                        <a href="#install" class="btn btn-primary btn-lg"><i class="bi bi-terminal me-1"></i>Install</a>
                        <a href="/download/" class="btn btn-outline-secondary btn-lg"><i class="bi bi-download me-1"></i>Download <?= htmlspecialchars($latestVersion) ?></a>
                    </div>
                    <div class="row justify-content-center">
                        <div class="col-lg-6">
                            <hr class="mb-4">
                            <div class="code-block text-start mb-4">
                                <pre id="fuelDemo" aria-label="Build demo animation"></pre>
                            </div>
                            <hr class="mb-4">
                            <p class="fs-5 mb-1">If WOR saves you time,<br>consider buying me a beer and some Texas BBQ.</p>
                            <p class="text-body-secondary small mb-4">Thanks for supporting independent open source.</p>
                            <a class="btn btn-primary btn-lg" href="https://paypal.me/TeamWorapong" target="_blank" rel="noopener">
                                🍺 Buy me a beer &amp; BBQ
                            </a>
                        </div>
                    </div>
                </div>
            </section>
        </main>

        <footer class="border-top py-4 bg-body-tertiary">
            <div class="container d-flex flex-column flex-md-row justify-content-between align-items-center gap-2">
                <span class="text-body-secondary small">
                    <i class="bi bi-hdd-stack me-1"></i>WOR Host &copy; <?= date('Y') ?>
                </span>
                <span class="text-body-secondary small">
                    <a href="/download/" class="link-secondary me-3">Downloads</a>
                    <a href="/docs/" class="link-secondary me-3">Docs</a>
                    <a href="https://paypal.me/TeamWorapong" target="_blank" rel="noopener" class="link-secondary"><i class="bi bi-heart-fill me-1"></i>Donate</a>
                </span>
            </div>
        </footer>

        <script src="https://cdn.jsdelivr.net/npm/bootstrap@5.3.8/dist/js/bootstrap.bundle.min.js" integrity="sha384-FKyoEForCGlyvwx9Hj09JcYn3nv7wiPVlz7YYwJrWVcXK/BmnVDxM+D2scQbITxI" crossorigin="anonymous"></script>
        <script>
            (() => {
                'use strict';
                // Theme: auto -> light -> dark cycle, persisted
                const root = document.documentElement;
                const btn = document.getElementById('themeToggle');
                const icons = {auto: 'bi-circle-half', light: 'bi-sun-fill', dark: 'bi-moon-stars-fill'};
                const media = window.matchMedia('(prefers-color-scheme: dark)');
                const stored = () => localStorage.getItem('wor-theme') || 'auto';
                const apply = (t) => {
                    root.setAttribute('data-bs-theme', t === 'auto' ? (media.matches ? 'dark' : 'light') : t);
                    btn.querySelector('i').className = 'bi ' + icons[t];
                };
                apply(stored());
                media.addEventListener('change', () => {
                    if (stored() === 'auto')
                        apply('auto');
                });
                btn.addEventListener('click', () => {
                    const order = ['auto', 'light', 'dark'];
                    const next = order[(order.indexOf(stored()) + 1) % order.length];
                    localStorage.setItem('wor-theme', next);
                    apply(next);
                });

                // Donate section: replay of ./scripts/build.sh --release,
                // ending with the (very real) fuel warning.
                const demo = document.getElementById('fuelDemo');
                if (demo) {
                    // [html, pauseAfterMs] -- html is trusted, hand-written below.
                    const lines = [
                        ['<span class="tm-dim">==&gt;</span> Checking', 600],
                        ['<span class="tm-dim">==&gt;</span> Running tests', 900],
                        ['<span class="tm-ok">ok</span>   wor/internal/cliapp        1.32s', 150],
                        ['<span class="tm-ok">ok</span>   wor/internal/hostprovider  0.27s', 150],
                        ['<span class="tm-ok">ok</span>   wor/internal/phpfpm        0.42s', 400],
                        ['<span class="tm-dim">==&gt;</span> Building', 400],
                        ['    Target : linux/amd64', 500],
                        ['<span class="tm-ok">[OK]</span> Build complete: ./dist/bin/wor-linux-amd64', 250],
                        ['    Target : macos/arm64', 500],
                        ['<span class="tm-ok">[OK]</span> Build complete: ./dist/bin/wor-macos-arm64', 250],
                        ['    Target : windows/amd64', 500],
                        ['<span class="tm-ok">[OK]</span> Build complete: ./dist/bin/wor-windows-amd64.exe', 800],
                        ['', 100],
                        ['Checking maintainer dependencies: <span class="fuel-bar"><span class="fuel-fill"></span></span>', 1100],
                        ['<span class="tm-warn">[WARN]</span> Missing optional packages:', 450],
                        ['  🍺 beer       &gt;= 1.0', 300],
                        ['  🍖 texas-bbq  &gt;= 18oz', 900],
                        ['', 100],
                        ['<span class="tm-dim">[ ENTER ] Skip optional packages</span>', 350],
                        ['<a class="refuel-link" href="https://paypal.me/TeamWorapong" target="_blank" rel="noopener"><span class="tm-warn">[ INSTALL 🍺+🍖 ]</span> Install optional packages</a>', 0],
                    ];
                    const cmd = './scripts/build.sh --release';
                    const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
                    const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;

                    const renderAll = () => {
                        demo.innerHTML = '<span class="prompt">$</span> ' + cmd + '\n'
                            + lines.map(([h]) => h).join('\n');
                    };

                    const play = async () => {
                        demo.innerHTML = '<span class="prompt">$</span> <span id="fuelCmd"></span><span class="fuel-cursor">▌</span>';
                        const cmdEl = document.getElementById('fuelCmd');
                        for (const ch of cmd) {
                            cmdEl.textContent += ch;
                            await sleep(35);
                        }
                        await sleep(400);
                        demo.querySelector('.fuel-cursor').remove();
                        for (const [html, pause] of lines) {
                            demo.innerHTML += '\n' + html;
                            await sleep(pause);
                        }
                        // ends here on purpose: the last line is a clickable
                        // menu, so no replay loop that would yank it away.
                    };

                    if (reduced) {
                        renderAll();
                    } else {
                        const io = new IntersectionObserver((entries) => {
                            if (entries.some((e) => e.isIntersecting)) {
                                io.disconnect();
                                play();
                            }
                        }, {threshold: 0.3});
                        io.observe(demo);
                    }
                }

                // Copy buttons
                document.querySelectorAll('.btn-copy').forEach((b) => {
                    b.addEventListener('click', async () => {
                        try {
                            await navigator.clipboard.writeText(b.dataset.copy);
                            const i = b.querySelector('i');
                            i.className = 'bi bi-check-lg';
                            setTimeout(() => {
                                i.className = 'bi bi-clipboard';
                            }, 1500);
                        } catch (e) { /* clipboard unavailable */
                        }
                    });
                });
            })();
        </script>
    </body>
</html>
