<?php
require __DIR__ . '/../lib/releases.php';
// WOR Host — release notes
//
// One entry per released VERSION, newest first, written by hand: this is
// the "what changed and why you care" page, not a changelog generated
// from commits.
//
// The version each entry describes is stated in the entry itself and is
// never derived from publishedReleaseTag(). The published tag moves on
// every `scripts/release.sh` run, so deriving the heading from it would
// eventually put the next version's number above this version's notes --
// the page would look updated without anyone having written a word. The
// live tag is shown once, separately, as the fact that it is: which
// build the download page is currently serving.

$published = publishedReleaseTag();

$releases = [
    [
        'version' => 'v1.0.2',
        'date'    => '2026-08-21',
        'summary' => 'Per-service PHP configuration, a self-upgrade command, '
                   . 'and a deploy that stops reloading php-fpm when nothing changed.',
        'items'   => [
            [
                'title' => 'Per-service PHP settings',
                'body'  => 'A php service with its own pool can now configure itself, in two '
                         . 'files inside its own tree: <code>.wor/php.ini</code> for PHP ini '
                         . 'settings (<code>memory_limit</code>, upload sizes, timezone) and '
                         . '<code>.wor/php-fpm.ini</code> for the pool\'s process manager '
                         . '(<code>pm</code>, <code>pm.max_children</code> and friends). No more '
                         . 'editing the host\'s php.ini and hoping no other site minded.',
                'note'  => 'WOR reads these files; PHP does not. A PHP-FPM pool cannot include a '
                         . 'php.ini of its own, so WOR parses them and renders directives into '
                         . 'the service\'s pool config — which <code>php-fpm -t</code> validates '
                         . 'before anything reloads. A value php-fpm rejects rolls the pool back '
                         . 'to exactly what it was, so one bad setting cannot take down the other '
                         . 'services sharing that master. Only an allowlist of keys is accepted, '
                         . 'and a key outside it is an error rather than a skipped line.',
            ],
            [
                'title' => '<code>wor service reload</code>',
                'body'  => 'Applies those files on their own: re-renders the pool, validates it, '
                         . 'reloads php-fpm and prints what is now in force — without reinstalling '
                         . 'dependencies, rebuilding or restarting the service.',
                'note'  => 'Configuration only. Restarting a process is still '
                         . '<code>wor service restart</code> and re-rendering a vhost is still '
                         . '<code>wor host reload</code>. It is also the only way to apply these '
                         . 'files to a service whose source is not a git repository, which '
                         . '<code>wor deploy</code> requires.',
            ],
            [
                'title' => 'Deploy no longer reloads php-fpm for nothing',
                'body'  => '<code>wor deploy</code> re-renders a pooled php service\'s config on '
                         . 'every deploy, so an edit to either settings file ships with the code '
                         . 'that needs it. But it now skips the write and the reload entirely when '
                         . 'the result would be identical to what is already on disk.',
                'note'  => 'Reloading the shared php-fpm master cycles the workers of every other '
                         . 'service under it. A deploy that only changed code has no business '
                         . 'doing that.',
            ],
            [
                'title' => 'Settings that were never applied are now visible',
                'body'  => '<code>wor info</code> lists what each service asks for. '
                         . '<code>wor diagnose</code> and <code>wor health</code> warn when the '
                         . 'running pool no longer matches those files — edited and never applied '
                         . '— or when a file no longer parses, which would fail the next deploy.',
                'note'  => 'All three are read-only and never ask for sudo: a pool file they '
                         . 'cannot read is reported as unchecked, never as wrong.',
            ],
            [
                'title' => '<code>wor upgrade</code>',
                'body'  => 'Compares the running binary against the release this site publishes, '
                         . 'shows you both, and installs the newer one once you confirm. '
                         . '<code>--yes</code> skips the confirmation.',
                'note'  => 'Installation is handed to the <code>install.sh</code> inside the '
                         . 'downloaded archive rather than reimplemented, so there is only one '
                         . 'tested install path. Not available on Windows.',
            ],
            [
                'title' => '<code>wor service chown</code>',
                'body'  => 'Hands one service\'s files back to the operator account, or to a user '
                         . 'you name — for a tree left owned by root, by a CI rsync, or by an '
                         . 'admin who is not the operator.',
                'note'  => 'It changes the owner only, never the group, and re-grants the php-fpm '
                         . 'pool afterwards. Rewriting the group would revoke the pool\'s access '
                         . 'to the very files it has to read.',
            ],
            [
                'title' => '<code>wor source clone</code> asks about <code>.wor</code>',
                'body'  => 'A clone replaces the service\'s whole tree, and the backup it takes '
                         . 'first honours <code>.gitignore</code> — so a gitignored '
                         . '<code>.wor</code> could be lost with no copy anywhere. It now asks '
                         . 'first: keep the current one (the default) or take the repository\'s.',
                'note'  => 'Committing <code>.wor</code> to your repository is the tidy way to '
                         . 'version this configuration alongside the code.',
            ],
        ],
        'upgrade' => 'Nothing to migrate. A service with no settings files renders exactly the '
                   . 'pool config it had before, so upgrading does not rewrite or reload anything '
                   . 'on its own — verified against a live pool on a production host.',
    ],
];
?>
<!doctype html>
<html lang="en" data-bs-theme="auto">
<head>
<!-- Google tag (gtag.js) -->
<script async src="https://www.googletagmanager.com/gtag/js?id=G-VPT5GEM34V"></script>
<script>
  window.dataLayer = window.dataLayer || [];
  function gtag(){dataLayer.push(arguments);}
  gtag('js', new Date());
  gtag('config', 'G-VPT5GEM34V');
</script>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Release notes — WOR Host</title>
<meta name="description" content="What changed in each WOR Host release: new commands, behaviour changes, and what you need to do when upgrading.">
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
  .release-item { border-left: 3px solid var(--bs-border-color); padding-left: 1rem; }
  .release-item:hover { border-left-color: var(--wor-accent); }
  .release-item h3 { font-size: 1.05rem; }
  .release-note {
    font-size: .9rem;
    color: var(--bs-secondary-color);
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
        <li class="nav-item"><a class="nav-link" href="/#features">Features</a></li>
        <li class="nav-item"><a class="nav-link" href="/#why">Why WOR?</a></li>
        <li class="nav-item"><a class="nav-link" href="/#demo">Demo</a></li>
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

<main class="py-5">
  <div class="container">
    <div class="row justify-content-center">
      <div class="col-lg-9">

        <h1 class="fw-bold mb-1"><i class="bi bi-megaphone me-2 text-gradient"></i>Release notes</h1>
        <p class="text-body-secondary mb-4">What changed in each release, and what it means for a machine you already run. For the full command reference see the <a href="/docs/#commands">docs</a>.</p>

        <?php if ($published !== null): ?>
        <p class="mb-5">
          <span class="badge text-bg-secondary">Currently published</span>
          <code class="ms-2"><?= htmlspecialchars($published) ?></code>
          <a class="ms-2 small" href="/download/">Download</a>
        </p>
        <?php endif; ?>

        <?php foreach ($releases as $rel): ?>
        <section class="mb-5" id="<?= htmlspecialchars($rel['version']) ?>">
          <div class="d-flex flex-wrap align-items-baseline gap-2 mb-2">
            <h2 class="fw-bold mb-0"><?= htmlspecialchars($rel['version']) ?></h2>
            <span class="text-body-secondary small"><?= htmlspecialchars($rel['date']) ?></span>
          </div>
          <p class="text-body-secondary"><?= $rel['summary'] ?></p>

          <?php foreach ($rel['items'] as $item): ?>
          <div class="release-item mb-4">
            <h3 class="fw-semibold mb-1"><?= $item['title'] ?></h3>
            <p class="mb-1"><?= $item['body'] ?></p>
            <?php if (!empty($item['note'])): ?>
            <p class="release-note mb-0"><?= $item['note'] ?></p>
            <?php endif; ?>
          </div>
          <?php endforeach; ?>

          <?php if (!empty($rel['upgrade'])): ?>
          <div class="alert alert-secondary d-flex gap-2" role="alert">
            <i class="bi bi-arrow-up-circle-fill flex-shrink-0"></i>
            <div><strong>Upgrading:</strong> <?= $rel['upgrade'] ?></div>
          </div>
          <?php endif; ?>
        </section>
        <?php endforeach; ?>

      </div>
    </div>
  </div>
</main>

<footer class="border-top py-4 bg-body-tertiary">
  <div class="container d-flex flex-column flex-md-row justify-content-between align-items-center gap-2">
    <span class="text-body-secondary small"><i class="bi bi-hdd-stack me-1"></i>WOR Host &copy; <?= date('Y') ?></span>
    <span class="text-body-secondary small">
      <a href="/" class="link-secondary me-3">Home</a>
      <a href="/docs/" class="link-secondary me-3">Docs</a>
      <a href="/download/" class="link-secondary me-3">Downloads</a>
      <a href="/code-signing/" class="link-secondary me-3">Code signing policy</a>
      <a href="https://paypal.me/TeamWorapong" target="_blank" rel="noopener" class="link-secondary"><i class="bi bi-heart-fill me-1"></i>Donate</a>
    </span>
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
})();
</script>
</body>
</html>
