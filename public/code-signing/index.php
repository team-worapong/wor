<?php
// WOR Host — code signing policy
//
// Published because SignPath Foundation's terms require the project's home
// page to carry a section headed (or linked as) "Code signing policy",
// including their exact attribution and privacy wording. Do not reword the
// attribution line or the privacy sentence -- both are prescribed verbatim.
// See docs/code-signing.md for the rest of the signing setup.
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
<title>Code signing policy — WOR Host</title>
<meta name="description" content="How official WOR Host binaries are built, signed and verified.">
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
  main h2 { margin-top: 2.5rem; }
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
      <div class="col-lg-8">

        <h1 class="mb-3">Code signing policy</h1>
        <p class="lead text-body-secondary">
          How official WOR Host binaries are built, signed and verified.
        </p>

        <p>
          Free code signing provided by <a href="https://about.signpath.io" target="_blank" rel="noopener">SignPath.io</a>,
          certificate by <a href="https://signpath.org" target="_blank" rel="noopener">SignPath Foundation</a>.
        </p>

        <div class="alert alert-info d-flex gap-2" role="alert">
          <i class="bi bi-info-circle-fill mt-1"></i>
          <div>
            The certificate is issued to <strong>SignPath Foundation</strong>, so that is the
            publisher Windows shows for <code>wor.exe</code>. It does not mean the file came
            from anywhere other than this project.
          </div>
        </div>

        <h2 class="h4">Team roles</h2>
        <ul>
          <li><strong>Committers and reviewers:</strong> Worapong Sriwichian</li>
          <li><strong>Approvers:</strong> Worapong Sriwichian</li>
        </ul>
        <p>
          Only an approver can authorise a release for signing, and every release is
          approved manually. All team members use multi-factor authentication on both
          their SignPath and GitHub accounts.
        </p>

        <h2 class="h4">How official releases are built</h2>
        <p>
          Official WOR Host releases are built by a GitHub Actions workflow in the
          project repository, on GitHub-hosted runners, from the tagged source commit.
          The Windows binary is submitted for signing directly from that workflow, and
          SignPath independently verifies with GitHub that the artifact came from that
          build before signing it. Binaries built anywhere else are never signed.
        </p>
        <p>
          Releases are published at
          <a href="/download/">wor.worapong.com/download</a> and on
          <a href="https://github.com/team-worapong/wor" target="_blank" rel="noopener">GitHub</a>.
        </p>

        <h2 class="h4">Third-party components</h2>
        <p>
          WOR Host is written in Go and depends on the Go standard library only. It has
          no third-party runtime dependencies, so no third-party code is included in the
          signed binary. Build-time tooling is limited to the Go toolchain and
          <a href="https://github.com/tc-hib/go-winres" target="_blank" rel="noopener">go-winres</a>,
          which embeds the Windows version resource.
        </p>
        <p>
          At runtime WOR Host invokes external programs that are already installed on the
          host — nginx or Apache, PM2, systemd, PHP-FPM, git, certbot and database client
          tools. These are neither bundled nor distributed with WOR Host and are not
          covered by this signature.
        </p>

        <h2 class="h4">Privacy</h2>
        <p>
          This program will not transfer any information to other networked systems
          unless specifically requested by the user or the person installing or
          operating it.
        </p>

        <h2 class="h4">Uninstalling</h2>
        <p>
          <code>wor reset</code> removes everything WOR Host created — its PM2 processes,
          systemd units, generated host configs, hosts-file entries, and the workspace
          folders. Afterwards, delete the binary itself
          (<code>/usr/local/bin/wor</code> on Linux and macOS, or wherever
          <code>wor.exe</code> was placed on Windows) and the
          <code>~/.wor</code> configuration directory.
        </p>

        <h2 class="h4">Verifying a download</h2>
        <p>
          On Windows you can confirm the signature yourself:
        </p>
        <pre class="bg-body-tertiary border rounded p-3"><code>Get-AuthenticodeSignature .\wor.exe | Format-List Status, SignerCertificate</code></pre>
        <p class="text-body-secondary small">
          A valid result reports <code>Status: Valid</code> with SignPath Foundation as the signer.
        </p>

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
