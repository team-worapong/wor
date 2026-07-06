<?php
// WOR Runtime Manager — landing page
$latestVersion = 'v1.0.0-b32';

// Sortable key: v1.0.0-b32 -> [1,0,0,32]; final releases outrank betas.
function versionKey(string $tag): array {
    if (!preg_match('/^v(\d+)\.(\d+)\.(\d+)(?:-?b(\d+))?/i', $tag, $m)) return [0, 0, 0, 0];
    return [(int)$m[1], (int)$m[2], (int)$m[3], isset($m[4]) && $m[4] !== '' ? (int)$m[4] : PHP_INT_MAX];
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
    if ($best !== null) $latestVersion = $best;
}
?>
<!doctype html>
<html lang="en" data-bs-theme="auto">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>WOR Runtime Manager — Manage web app runtimes under one convention</title>
<meta name="description" content="WOR is a cross-platform runtime manager for web applications: Node.js/PHP/Go/Python services, static sites, nginx/apache hosts, SSL, and database backups — one CLI, one filesystem convention.">
<link href="https://cdn.jsdelivr.net/npm/bootstrap@5.3.8/dist/css/bootstrap.min.css" rel="stylesheet" integrity="sha384-sRIl4kxILFvY47J16cr9ZwB07vP4J8+LH7qKQnuqkuIAvNWLzeN8tE5YBujZqJLB" crossorigin="anonymous">
<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/bootstrap-icons@1.13.1/font/bootstrap-icons.min.css">
<style>
  :root { --wor-accent: #6366f1; --wor-accent-2: #22d3ee; }
  body { display: flex; flex-direction: column; min-height: 100vh; }
  main { flex: 1; }
  .hero {
    background:
      radial-gradient(1100px 500px at 85% -10%, rgba(99,102,241,.28), transparent 60%),
      radial-gradient(900px 420px at 0% 110%, rgba(34,211,238,.18), transparent 60%);
  }
  .text-gradient {
    background: linear-gradient(90deg, var(--wor-accent), var(--wor-accent-2));
    -webkit-background-clip: text; background-clip: text; color: transparent;
  }
  .code-block {
    position: relative; border-radius: .75rem; overflow: hidden;
    background: #0f172a; border: 1px solid rgba(255,255,255,.08);
  }
  .code-block pre {
    margin: 0; padding: 1rem 3.25rem 1rem 1.25rem; color: #e2e8f0;
    font-size: .875rem; overflow-x: auto; white-space: pre;
  }
  .code-block .prompt { color: #22d3ee; user-select: none; }
  .code-block .cmt { color: #64748b; }
  .btn-copy {
    position: absolute; top: .5rem; right: .5rem; color: #94a3b8;
    border: 1px solid rgba(255,255,255,.15); background: rgba(255,255,255,.05);
  }
  .btn-copy:hover { color: #fff; border-color: rgba(255,255,255,.4); }
  .feature-icon {
    width: 3rem; height: 3rem; border-radius: .85rem; font-size: 1.35rem;
    display: inline-flex; align-items: center; justify-content: center;
    background: linear-gradient(135deg, rgba(99,102,241,.15), rgba(34,211,238,.15));
    color: var(--wor-accent);
  }
  [data-bs-theme="dark"] .feature-icon { color: #a5b4fc; }
  .card-hover { transition: transform .15s ease, box-shadow .15s ease; }
  .card-hover:hover { transform: translateY(-3px); box-shadow: 0 .5rem 1.5rem rgba(0,0,0,.12); }
  .step-num {
    width: 2rem; height: 2rem; border-radius: 50%; flex: 0 0 auto;
    display: inline-flex; align-items: center; justify-content: center;
    background: var(--wor-accent); color: #fff; font-weight: 600; font-size: .9rem;
  }
  .nav-anchor { scroll-margin-top: 80px; }
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
        <li class="nav-item"><a class="nav-link" href="#install">Install</a></li>
        <li class="nav-item"><a class="nav-link" href="#quickstart">Quick start</a></li>
        <li class="nav-item"><a class="nav-link" href="#commands">Commands</a></li>
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
            One CLI for your <span class="text-gradient">web app runtimes</span>
          </h1>
          <p class="lead text-body-secondary mb-4">
            WOR Runtime Manager keeps Node.js, PHP, Go, Python and static services, nginx/apache
            hosts, SSL certificates and database backups under a single filesystem convention —
            on Linux, macOS and Windows.
          </p>
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
        <h2 class="fw-bold">Everything a small server fleet needs</h2>
        <p class="text-body-secondary">Infrastructure &amp; operations, without the YAML.</p>
      </div>
      <div class="row g-4">
        <div class="col-md-6 col-lg-4">
          <div class="card h-100 card-hover border-0 shadow-sm">
            <div class="card-body">
              <div class="feature-icon mb-3"><i class="bi bi-boxes"></i></div>
              <h5 class="card-title">Five service templates</h5>
              <p class="card-text text-body-secondary">static, node, go, python and php — scaffolded with a starter source tree and wired to the right process manager (PM2, systemd or PHP-FPM) automatically.</p>
            </div>
          </div>
        </div>
        <div class="col-md-6 col-lg-4">
          <div class="card h-100 card-hover border-0 shadow-sm">
            <div class="card-body">
              <div class="feature-icon mb-3"><i class="bi bi-globe2"></i></div>
              <h5 class="card-title">Host management</h5>
              <p class="card-text text-body-secondary">Generates and enables nginx or apache vhosts, tests configuration before reload, and can manage your local hosts file for development domains.</p>
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
              <div class="feature-icon mb-3"><i class="bi bi-rocket-takeoff"></i></div>
              <h5 class="card-title">Deploy &amp; rollback</h5>
              <p class="card-text text-body-secondary">Pull from git, install dependencies, rebuild and restart with one command — and roll back to the previous release when something goes wrong.</p>
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

  <!-- Install -->
  <section id="install" class="py-5 bg-body-tertiary nav-anchor">
    <div class="container">
      <div class="row justify-content-center">
        <div class="col-lg-9">
          <h2 class="fw-bold mb-1">Install on a server</h2>
          <p class="text-body-secondary mb-4">Debian/Ubuntu are supported by the installer today. A single static Go binary — no runtime dependencies.</p>

          <h5 class="mt-4"><i class="bi bi-1-circle me-2 text-primary"></i>One-liner (latest release)</h5>
          <div class="code-block mb-4">
            <pre><span class="prompt">$</span> curl -fsSL https://wor.worapong.com/download/installer.sh | bash</pre>
            <button class="btn btn-sm btn-copy" data-copy="curl -fsSL https://wor.worapong.com/download/installer.sh | bash" aria-label="Copy"><i class="bi bi-clipboard"></i></button>
          </div>

          <h5><i class="bi bi-2-circle me-2 text-primary"></i>Specific version</h5>
          <p class="text-body-secondary small mb-2">Any release tag listed on the <a href="/download/">download page</a>, including beta builds.</p>
          <div class="code-block mb-4">
            <pre><span class="prompt">$</span> curl -fsSL https://wor.worapong.com/download/installer.sh | bash -s -- <?= htmlspecialchars($latestVersion) ?></pre>
            <button class="btn btn-sm btn-copy" data-copy="curl -fsSL https://wor.worapong.com/download/installer.sh | bash -s -- <?= htmlspecialchars($latestVersion) ?>" aria-label="Copy"><i class="bi bi-clipboard"></i></button>
          </div>

          <h5><i class="bi bi-3-circle me-2 text-primary"></i>Manual install</h5>
          <p class="text-body-secondary small mb-2">Both <code>.tar.gz</code> and <code>.zip</code> contain the same files; the folder inside is always <code>wor-runtime-manager/</code>.</p>
          <div class="code-block mb-4">
            <pre><span class="prompt">$</span> curl -fsSL https://wor.worapong.com/download/releases/<?= htmlspecialchars($latestVersion) ?>.tar.gz -o wor.tar.gz
<span class="prompt">$</span> tar -xzf wor.tar.gz
<span class="prompt">$</span> cd wor-runtime-manager
<span class="prompt">$</span> sudo ./install.sh</pre>
            <button class="btn btn-sm btn-copy" data-copy="curl -fsSL https://wor.worapong.com/download/releases/<?= htmlspecialchars($latestVersion) ?>.tar.gz -o wor.tar.gz
tar -xzf wor.tar.gz
cd wor-runtime-manager
sudo ./install.sh" aria-label="Copy"><i class="bi bi-clipboard"></i></button>
          </div>

          <div class="alert alert-info d-flex gap-2" role="alert">
            <i class="bi bi-info-circle-fill"></i>
            <div><code>install.sh</code> detects your distro, reports which runtime packages are already installed, and asks before installing only the missing ones — it never upgrades or removes anything already present.</div>
          </div>
        </div>
      </div>
    </div>
  </section>

  <!-- Quick start -->
  <section id="quickstart" class="py-5 nav-anchor">
    <div class="container">
      <div class="row justify-content-center">
        <div class="col-lg-9">
          <h2 class="fw-bold mb-4">Quick start</h2>

          <div class="d-flex gap-3 mb-4">
            <span class="step-num">1</span>
            <div class="flex-grow-1">
              <h5 class="mb-1">Verify &amp; set up</h5>
              <p class="text-body-secondary mb-2">As your non-root operator user, check the install, let <code>doctor</code> inspect the machine, then initialize the WOR home directory.</p>
              <div class="code-block">
                <pre><span class="prompt">$</span> wor version
<span class="prompt">$</span> wor doctor
<span class="prompt">$</span> wor setup</pre>
                <button class="btn btn-sm btn-copy" data-copy="wor version
wor doctor
wor setup" aria-label="Copy"><i class="bi bi-clipboard"></i></button>
              </div>
            </div>
          </div>

          <div class="d-flex gap-3 mb-4">
            <span class="step-num">2</span>
            <div class="flex-grow-1">
              <h5 class="mb-1">Create your first host</h5>
              <p class="text-body-secondary mb-2">An interactive wizard walks you through service type, domain and hosts entry.</p>
              <div class="code-block">
                <pre><span class="prompt">$</span> wor create myapp.example.com</pre>
                <button class="btn btn-sm btn-copy" data-copy="wor create myapp.example.com" aria-label="Copy"><i class="bi bi-clipboard"></i></button>
              </div>
            </div>
          </div>

          <div class="d-flex gap-3 mb-4">
            <span class="step-num">3</span>
            <div class="flex-grow-1">
              <h5 class="mb-1">Clone your code &amp; deploy</h5>
              <p class="text-body-secondary mb-2">Point a service at a git repository, then deploy — pull, install dependencies, rebuild, restart.</p>
              <div class="code-block">
                <pre><span class="prompt">$</span> wor source clone myapp https://github.com/you/myapp.git
<span class="prompt">$</span> wor deploy myapp.example.com</pre>
                <button class="btn btn-sm btn-copy" data-copy="wor source clone myapp https://github.com/you/myapp.git
wor deploy myapp.example.com" aria-label="Copy"><i class="bi bi-clipboard"></i></button>
              </div>
            </div>
          </div>

          <div class="d-flex gap-3 mb-4">
            <span class="step-num">4</span>
            <div class="flex-grow-1">
              <h5 class="mb-1">Secure &amp; monitor</h5>
              <p class="text-body-secondary mb-2">Issue a certificate and keep an eye on the fleet.</p>
              <div class="code-block">
                <pre><span class="prompt">$</span> wor ssl issue myapp.example.com --provider=letsencrypt
<span class="prompt">$</span> wor health</pre>
                <button class="btn btn-sm btn-copy" data-copy="wor ssl issue myapp.example.com --provider=letsencrypt
wor health" aria-label="Copy"><i class="bi bi-clipboard"></i></button>
              </div>
            </div>
          </div>

          <h5 class="mt-5 mb-2"><i class="bi bi-stars me-2 text-primary"></i>Service templates</h5>
          <div class="table-responsive">
            <table class="table table-hover align-middle">
              <thead>
                <tr><th>Template</th><th>Runtime</th><th>Process provider</th><th>Default entry point</th></tr>
              </thead>
              <tbody>
                <tr><td><code>static</code></td><td>none</td><td>web server serves <code>public/</code></td><td>—</td></tr>
                <tr><td><code>node</code></td><td>Node.js</td><td>PM2 (every OS)</td><td><code>app.js</code></td></tr>
                <tr><td><code>go</code></td><td>Go</td><td>systemd (Linux) / PM2 (else)</td><td><code>app</code> (compiled binary)</td></tr>
                <tr><td><code>python</code></td><td>Python</td><td>systemd (Linux) / PM2 (else)</td><td><code>app.py</code></td></tr>
                <tr><td><code>php</code></td><td>PHP-FPM</td><td>PHP-FPM (per-service pool)</td><td><code>public/index.php</code></td></tr>
              </tbody>
            </table>
          </div>
        </div>
      </div>
    </div>
  </section>

  <!-- Commands -->
  <section id="commands" class="py-5 bg-body-tertiary nav-anchor">
    <div class="container">
      <div class="row justify-content-center">
        <div class="col-lg-9">
          <h2 class="fw-bold mb-4">Command overview</h2>
          <div class="row g-3">
            <?php
            $groups = [
                ['bi-gear',          'Setup',      ['wor setup', 'wor doctor', 'wor env', 'wor clean', 'wor reset']],
                ['bi-magic',         'Create',     ['wor create [host]', 'wor domain add|remove', 'wor service add|remove']],
                ['bi-play-circle',   'Run',        ['wor run', 'wor service start|stop|restart', 'wor service status|logs']],
                ['bi-globe',         'Hosts',      ['wor host add|remove|list', 'wor host test|reload|logs']],
                ['bi-git',           'Source',     ['wor source clone|pull|backup', 'wor deploy', 'wor rollback']],
                ['bi-shield-check',  'SSL',        ['wor ssl issue|renew|status', 'wor ssl install|remove']],
                ['bi-database',      'Databases',  ['wor database add|remove', 'wor database backup']],
                ['bi-activity',      'Observe',    ['wor info', 'wor health', 'wor diagnose']],
                ['bi-folder-symlink','Navigate',   ['wor path', 'wor goto (via shell-init)']],
            ];
            foreach ($groups as [$icon, $title, $cmds]): ?>
            <div class="col-sm-6 col-lg-4">
              <div class="card h-100 border-0 shadow-sm">
                <div class="card-body py-3">
                  <h6 class="mb-2"><i class="bi <?= $icon ?> me-2 text-primary"></i><?= $title ?></h6>
                  <?php foreach ($cmds as $c): ?>
                    <div><code class="small"><?= htmlspecialchars($c) ?></code></div>
                  <?php endforeach; ?>
                </div>
              </div>
            </div>
            <?php endforeach; ?>
          </div>
          <p class="text-body-secondary small mt-3 mb-0">Run <code>wor &lt;command&gt; --help</code> for full flags, or see <code>docs/commands.md</code> in the release archive.</p>
        </div>
      </div>
    </div>
  </section>

  <!-- CTA -->
  <section class="py-5 text-center">
    <div class="container">
      <h2 class="fw-bold mb-3">Ready to try it?</h2>
      <p class="text-body-secondary mb-4">Install in under a minute, or grab a release archive.</p>
      <div class="d-flex flex-column flex-sm-row gap-2 justify-content-center">
        <a href="#install" class="btn btn-primary btn-lg"><i class="bi bi-terminal me-1"></i>Install</a>
        <a href="/download/" class="btn btn-outline-secondary btn-lg"><i class="bi bi-download me-1"></i>Download <?= htmlspecialchars($latestVersion) ?></a>
      </div>
    </div>
  </section>
</main>

<footer class="border-top py-4 bg-body-tertiary">
  <div class="container d-flex flex-column flex-md-row justify-content-between align-items-center gap-2">
    <span class="text-body-secondary small">
      <i class="bi bi-hdd-stack me-1"></i>WOR Runtime Manager &copy; <?= date('Y') ?>
    </span>
    <span class="text-body-secondary small">
      <a href="/download/" class="link-secondary me-3">Downloads</a>
      <a href="#quickstart" class="link-secondary">Quick start</a>
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
  const icons = { auto: 'bi-circle-half', light: 'bi-sun-fill', dark: 'bi-moon-stars-fill' };
  const media = window.matchMedia('(prefers-color-scheme: dark)');
  const stored = () => localStorage.getItem('wor-theme') || 'auto';
  const apply = (t) => {
    root.setAttribute('data-bs-theme', t === 'auto' ? (media.matches ? 'dark' : 'light') : t);
    btn.querySelector('i').className = 'bi ' + icons[t];
  };
  apply(stored());
  media.addEventListener('change', () => { if (stored() === 'auto') apply('auto'); });
  btn.addEventListener('click', () => {
    const order = ['auto', 'light', 'dark'];
    const next = order[(order.indexOf(stored()) + 1) % order.length];
    localStorage.setItem('wor-theme', next);
    apply(next);
  });

  // Copy buttons
  document.querySelectorAll('.btn-copy').forEach((b) => {
    b.addEventListener('click', async () => {
      try {
        await navigator.clipboard.writeText(b.dataset.copy);
        const i = b.querySelector('i');
        i.className = 'bi bi-check-lg';
        setTimeout(() => { i.className = 'bi bi-clipboard'; }, 1500);
      } catch (e) { /* clipboard unavailable */ }
    });
  });
})();
</script>
</body>
</html>
