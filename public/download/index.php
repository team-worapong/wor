<?php
// WOR Runtime Manager — downloads page
// Lists everything in ./releases/ plus installer.sh, grouped by version.

$releasesDir = __DIR__ . '/releases';

function humanSize(int $bytes): string {
    $units = ['B', 'KB', 'MB', 'GB'];
    $i = 0;
    $v = (float)$bytes;
    while ($v >= 1024 && $i < count($units) - 1) { $v /= 1024; $i++; }
    return ($i === 0 ? (string)$bytes : number_format($v, 1)) . ' ' . $units[$i];
}

function fileKind(string $name): array {
    // [icon, badge label, badge class]
    if (str_ends_with($name, '.tar.gz') || str_ends_with($name, '.tgz')) return ['bi-file-zip', 'tar.gz', 'text-bg-primary'];
    if (str_ends_with($name, '.zip'))  return ['bi-file-zip', 'zip', 'text-bg-info'];
    if (str_ends_with($name, '.sh'))   return ['bi-terminal', 'script', 'text-bg-success'];
    return ['bi-file-earmark', 'file', 'text-bg-secondary'];
}

// Sortable key for version tags: v1.0.0-b32 -> [1,0,0,32]; final releases outrank betas.
function versionKey(string $tag): array {
    if (!preg_match('/^v(\d+)\.(\d+)\.(\d+)(?:-?b(\d+))?/i', $tag, $m)) return [0, 0, 0, 0];
    return [(int)$m[1], (int)$m[2], (int)$m[3], isset($m[4]) && $m[4] !== '' ? (int)$m[4] : PHP_INT_MAX];
}

$latest = null;     // ['file','size','mtime'] for latest.tar.gz
$versions = [];     // tag => list of ['file','size','mtime','ext']
$others = [];       // anything not matching the version pattern

if (is_dir($releasesDir)) {
    foreach (scandir($releasesDir) as $f) {
        $path = $releasesDir . '/' . $f;
        if ($f[0] === '.' || !is_file($path)) continue;
        $entry = ['file' => $f, 'size' => filesize($path), 'mtime' => filemtime($path)];
        if ($f === 'latest.tar.gz') {
            $latest = $entry;
        } elseif (preg_match('/^(v\d+\.\d+\.\d+(?:-?[A-Za-z0-9.]+)?)\.(tar\.gz|zip|tgz)$/', $f, $m)) {
            $versions[$m[1]][] = $entry + ['ext' => $m[2]];
        } else {
            $others[] = $entry;
        }
    }
    uksort($versions, fn($a, $b) => versionKey($b) <=> versionKey($a)); // newest first
}
$latestTag = array_key_first($versions);
?>
<!doctype html>
<html lang="en" data-bs-theme="auto">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Downloads — WOR Runtime Manager</title>
<meta name="description" content="Download WOR Runtime Manager releases: installer script, latest build, and all versioned archives.">
<link href="https://cdn.jsdelivr.net/npm/bootstrap@5.3.8/dist/css/bootstrap.min.css" rel="stylesheet" integrity="sha384-sRIl4kxILFvY47J16cr9ZwB07vP4J8+LH7qKQnuqkuIAvNWLzeN8tE5YBujZqJLB" crossorigin="anonymous">
<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/bootstrap-icons@1.13.1/font/bootstrap-icons.min.css">
<style>
  :root { --wor-accent: #6366f1; --wor-accent-2: #22d3ee; }
  body { display: flex; flex-direction: column; min-height: 100vh; }
  main { flex: 1; }
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
  .btn-copy {
    position: absolute; top: .5rem; right: .5rem; color: #94a3b8;
    border: 1px solid rgba(255,255,255,.15); background: rgba(255,255,255,.05);
  }
  .btn-copy:hover { color: #fff; border-color: rgba(255,255,255,.4); }
  .release-row:hover { background: var(--bs-tertiary-bg); }
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
        <li class="nav-item"><a class="nav-link" href="/#features">Features</a></li>
        <li class="nav-item"><a class="nav-link" href="/#install">Install</a></li>
        <li class="nav-item"><a class="nav-link" href="/#quickstart">Quick start</a></li>
        <li class="nav-item"><a class="nav-link active" href="/download/"><i class="bi bi-download me-1"></i>Downloads</a></li>
        <li class="nav-item ms-md-2">
          <button class="btn btn-outline-secondary btn-sm" id="themeToggle" title="Toggle theme" aria-label="Toggle theme">
            <i class="bi bi-circle-half"></i>
          </button>
        </li>
      </ul>
    </div>
  </div>
</nav>

<main class="py-5">
  <div class="container">
    <div class="row justify-content-center">
      <div class="col-lg-9">

        <h1 class="fw-bold mb-1"><i class="bi bi-box-seam me-2 text-gradient"></i>Downloads</h1>
        <p class="text-body-secondary mb-4">All release archives contain the same layout — the folder inside is always <code>wor-runtime-manager/</code>. Run <code>sudo ./install.sh</code> after extracting.</p>

        <!-- Installer -->
        <div class="card border-0 shadow-sm mb-4">
          <div class="card-body">
            <div class="d-flex flex-wrap justify-content-between align-items-center gap-2 mb-2">
              <h5 class="mb-0"><i class="bi bi-terminal me-2 text-success"></i>Recommended: installer script</h5>
              <a class="btn btn-sm btn-outline-secondary" href="installer.sh" download><i class="bi bi-download me-1"></i>installer.sh</a>
            </div>
            <p class="text-body-secondary small mb-2">Installs the latest release, or pass a version tag from the list below.</p>
            <div class="code-block">
              <pre><span class="prompt">$</span> curl -fsSL https://wor.worapong.com/download/installer.sh | bash</pre>
              <button class="btn btn-sm btn-copy" data-copy="curl -fsSL https://wor.worapong.com/download/installer.sh | bash" aria-label="Copy"><i class="bi bi-clipboard"></i></button>
            </div>
          </div>
        </div>

        <?php if ($latest !== null): ?>
        <!-- latest.tar.gz -->
        <div class="card border-0 shadow-sm mb-4">
          <div class="card-body d-flex flex-wrap justify-content-between align-items-center gap-2">
            <div>
              <h5 class="mb-1">
                <i class="bi bi-lightning-charge me-2 text-warning"></i>latest.tar.gz
                <?php if ($latestTag !== null): ?><span class="badge text-bg-warning ms-1"><?= htmlspecialchars($latestTag) ?></span><?php endif; ?>
              </h5>
              <span class="text-body-secondary small">
                <?= humanSize($latest['size']) ?> · updated <?= date('j M Y H:i', $latest['mtime']) ?> — always points at the newest build
              </span>
            </div>
            <a class="btn btn-primary" href="releases/latest.tar.gz" download><i class="bi bi-download me-1"></i>Download</a>
          </div>
        </div>
        <?php endif; ?>

        <!-- Versioned releases -->
        <h4 class="fw-bold mt-5 mb-3"><i class="bi bi-tags me-2"></i>All releases</h4>

        <?php if (empty($versions) && empty($others)): ?>
          <div class="alert alert-secondary"><i class="bi bi-inbox me-2"></i>No release files found yet.</div>
        <?php endif; ?>

        <?php $first = true; foreach ($versions as $tag => $files): ?>
        <div class="card border-0 shadow-sm mb-3">
          <div class="card-header bg-transparent d-flex flex-wrap align-items-center gap-2 py-3">
            <h5 class="mb-0"><i class="bi bi-tag me-2 text-primary"></i><?= htmlspecialchars($tag) ?></h5>
            <?php if ($first): ?><span class="badge text-bg-success">Latest</span><?php endif; ?>
            <?php if (preg_match('/b\d+/i', $tag)): ?><span class="badge text-bg-secondary">beta</span><?php endif; ?>
            <span class="text-body-secondary small ms-auto"><?= date('j M Y', max(array_column($files, 'mtime'))) ?></span>
          </div>
          <div class="list-group list-group-flush">
            <?php foreach ($files as $f): [$icon, $label, $badge] = fileKind($f['file']); ?>
            <div class="list-group-item release-row d-flex flex-wrap align-items-center gap-2 py-3">
              <i class="bi <?= $icon ?> fs-4 text-body-secondary"></i>
              <div class="me-auto">
                <div class="fw-medium font-monospace"><?= htmlspecialchars($f['file']) ?></div>
                <span class="text-body-secondary small"><?= humanSize($f['size']) ?> · <?= date('j M Y H:i', $f['mtime']) ?></span>
              </div>
              <span class="badge <?= $badge ?>"><?= $label ?></span>
              <a class="btn btn-sm btn-outline-primary" href="releases/<?= rawurlencode($f['file']) ?>" download>
                <i class="bi bi-download me-1"></i>Download
              </a>
            </div>
            <?php endforeach; ?>
          </div>
        </div>
        <?php $first = false; endforeach; ?>

        <?php if (!empty($others)): ?>
        <h5 class="fw-bold mt-4 mb-3">Other files</h5>
        <div class="card border-0 shadow-sm mb-3">
          <div class="list-group list-group-flush">
            <?php foreach ($others as $f): [$icon, $label, $badge] = fileKind($f['file']); ?>
            <div class="list-group-item release-row d-flex flex-wrap align-items-center gap-2 py-3">
              <i class="bi <?= $icon ?> fs-4 text-body-secondary"></i>
              <div class="me-auto">
                <div class="fw-medium font-monospace"><?= htmlspecialchars($f['file']) ?></div>
                <span class="text-body-secondary small"><?= humanSize($f['size']) ?> · <?= date('j M Y H:i', $f['mtime']) ?></span>
              </div>
              <span class="badge <?= $badge ?>"><?= $label ?></span>
              <a class="btn btn-sm btn-outline-primary" href="releases/<?= rawurlencode($f['file']) ?>" download>
                <i class="bi bi-download me-1"></i>Download
              </a>
            </div>
            <?php endforeach; ?>
          </div>
        </div>
        <?php endif; ?>

        <div class="alert alert-info d-flex gap-2 mt-4" role="alert">
          <i class="bi bi-info-circle-fill"></i>
          <div>Install a specific version with the installer:
            <code>curl -fsSL https://wor.worapong.com/download/installer.sh | bash -s -- <?= htmlspecialchars($latestTag ?? 'v1.0.0') ?></code>
          </div>
        </div>

      </div>
    </div>
  </div>
</main>

<footer class="border-top py-4 bg-body-tertiary">
  <div class="container d-flex flex-column flex-md-row justify-content-between align-items-center gap-2">
    <span class="text-body-secondary small"><i class="bi bi-hdd-stack me-1"></i>WOR Runtime Manager &copy; <?= date('Y') ?></span>
    <span class="text-body-secondary small"><a href="/" class="link-secondary">Home</a></span>
  </div>
</footer>

<script src="https://cdn.jsdelivr.net/npm/bootstrap@5.3.8/dist/js/bootstrap.bundle.min.js" integrity="sha384-FKyoEForCGlyvwx9Hj09JcYn3nv7wiPVlz7YYwJrWVcXK/BmnVDxM+D2scQbITxI" crossorigin="anonymous"></script>
<script>
(() => {
  'use strict';
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
  document.querySelectorAll('.btn-copy').forEach((b) => {
    b.addEventListener('click', async () => {
      try {
        await navigator.clipboard.writeText(b.dataset.copy);
        const i = b.querySelector('i');
        i.className = 'bi bi-check-lg';
        setTimeout(() => { i.className = 'bi bi-clipboard'; }, 1500);
      } catch (e) {}
    });
  });
})();
</script>
</body>
</html>
