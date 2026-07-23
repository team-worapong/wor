<?php
// WOR Host — documentation page
$latestVersion = 'v1.0.0-b32';

// Sortable key: v1.0.0-b32 -> [1,0,0,32]; final releases outrank betas.
function versionKey(string $tag): array {
    if (!preg_match('/^v(\d+)\.(\d+)\.(\d+)(?:-?b(\d+))?/i', $tag, $m))
        return [0, 0, 0, 0];
    return [(int) $m[1], (int) $m[2], (int) $m[3], isset($m[4]) && $m[4] !== '' ? (int) $m[4] : PHP_INT_MAX];
}

$releasesDir = __DIR__ . '/../download/releases';
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

/**
 * Minimal markdown renderer for docs/commands.md — supports exactly what
 * that file uses: #/## headings, ``` fences, "- " and "1. " lists with
 * indented continuation lines, `code`, **bold**, and paragraphs.
 * Returns [html, toc] where toc is [[slug, title], ...] from ## headings.
 * The single # H1 is skipped (the page renders its own section heading).
 */
function mdRender(string $md): array {
    $inline = function (string $s): string {
        $s = htmlspecialchars($s, ENT_QUOTES);
        $s = preg_replace('/`([^`]+)`/', '<code>$1</code>', $s);
        $s = preg_replace('/\*\*([^*]+)\*\*/', '<strong>$1</strong>', $s);
        return $s;
    };
    $slugify = fn(string $t): string => trim(preg_replace('/[^a-z0-9]+/', '-', strtolower($t)), '-');

    $html = '';
    $toc = [];
    $inFence = false;
    $fence = [];
    $para = [];
    $list = null; // ['tag' => 'ul'|'ol', 'items' => [...]]

    $flushPara = function () use (&$html, &$para, $inline) {
        if ($para) {
            $html .= '<p>' . $inline(implode(' ', $para)) . "</p>\n";
            $para = [];
        }
    };
    $flushList = function () use (&$html, &$list, $inline) {
        if ($list) {
            $html .= '<' . $list['tag'] . '>';
            foreach ($list['items'] as $item) {
                $html .= '<li>' . $inline($item) . '</li>';
            }
            $html .= '</' . $list['tag'] . ">\n";
            $list = null;
        }
    };

    foreach (explode("\n", $md) as $line) {
        if (preg_match('/^```/', $line)) {
            if ($inFence) {
                $html .= '<div class="code-block mb-4"><pre>'
                    . htmlspecialchars(rtrim(implode("\n", $fence)), ENT_QUOTES)
                    . "</pre></div>\n";
                $fence = [];
            } else {
                $flushPara();
                $flushList();
            }
            $inFence = !$inFence;
            continue;
        }
        if ($inFence) {
            $fence[] = $line;
            continue;
        }

        if (trim($line) === '') {
            $flushPara();
            $flushList();
            continue;
        }
        if (preg_match('/^##\s+(.*)$/', $line, $m)) {
            $flushPara();
            $flushList();
            $slug = 'cmd-' . $slugify($m[1]);
            $toc[] = [$slug, $m[1]];
            $html .= '<h4 class="fw-bold mt-5 mb-3 nav-anchor" id="' . $slug . '">' . $inline($m[1]) . "</h4>\n";
            continue;
        }
        if (preg_match('/^#\s+/', $line)) {
            $flushPara();
            $flushList();
            continue; // H1 skipped -- page has its own heading
        }
        if (preg_match('/^- (.*)$/', $line, $m)) {
            $flushPara();
            if (!$list || $list['tag'] !== 'ul') {
                $flushList();
                $list = ['tag' => 'ul', 'items' => []];
            }
            $list['items'][] = $m[1];
            continue;
        }
        if (preg_match('/^\d+\. (.*)$/', $line, $m)) {
            $flushPara();
            if (!$list || $list['tag'] !== 'ol') {
                $flushList();
                $list = ['tag' => 'ol', 'items' => []];
            }
            $list['items'][] = $m[1];
            continue;
        }
        if ($list && preg_match('/^\s+\S/', $line)) {
            // continuation of the previous list item
            $list['items'][count($list['items']) - 1] .= ' ' . trim($line);
            continue;
        }
        $para[] = trim($line);
    }
    $flushPara();
    $flushList();
    return [$html, $toc];
}

// Command reference source: docs/commands.md at the repo root, one level
// above the web root. Falls back to a GitHub link when not deployed.
$commandsHtml = null;
$commandsToc = [];
$commandsPath = dirname(__DIR__, 2) . '/docs/commands.md';
if (is_readable($commandsPath)) {
    [$commandsHtml, $commandsToc] = mdRender(file_get_contents($commandsPath));
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
        <title>Documentation — WOR Host</title>
        <meta name="description" content="WOR Host documentation: installation for Linux, macOS and Windows, quick start, service templates, and the full command reference.">
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
            .docs-sidebar {
                position: sticky;
                top: 80px;
                max-height: calc(100vh - 96px);
                overflow-y: auto;
                font-size: .9rem;
            }
            .docs-sidebar .nav-link {
                padding: .15rem .5rem;
                color: var(--bs-secondary-color);
            }
            .docs-sidebar .nav-link:hover {
                color: var(--bs-body-color);
            }
            .docs-sidebar .sidebar-heading {
                font-size: .75rem;
                text-transform: uppercase;
                letter-spacing: .05em;
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
                        <li class="nav-item"><a class="nav-link active" href="/docs/"><i class="bi bi-book me-1"></i>Docs</a></li>
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
                <div class="row g-5">

                    <!-- Sidebar -->
                    <aside class="col-lg-3 d-none d-lg-block">
                        <nav class="docs-sidebar">
                            <div class="sidebar-heading fw-bold mb-2">Getting started</div>
                            <ul class="nav flex-column mb-4">
                                <li class="nav-item"><a class="nav-link" href="#install">Install</a></li>
                                <li class="nav-item"><a class="nav-link" href="#quickstart">Quick start</a></li>
                                <li class="nav-item"><a class="nav-link" href="#templates">Service templates</a></li>
                            </ul>
                            <?php if ($commandsToc): ?>
                            <div class="sidebar-heading fw-bold mb-2">Command reference</div>
                            <ul class="nav flex-column mb-4">
                                <?php foreach ($commandsToc as [$slug, $title]): ?>
                                <li class="nav-item"><a class="nav-link" href="#<?= $slug ?>"><?= htmlspecialchars($title) ?></a></li>
                                <?php endforeach; ?>
                            </ul>
                            <?php endif; ?>
                            <div class="sidebar-heading fw-bold mb-2">More</div>
                            <ul class="nav flex-column">
                                <li class="nav-item"><a class="nav-link" href="https://github.com/team-worapong/wor" target="_blank" rel="noopener"><i class="bi bi-github me-1"></i>Source on GitHub</a></li>
                                <li class="nav-item"><a class="nav-link" href="/download/"><i class="bi bi-download me-1"></i>Downloads</a></li>
                            </ul>
                        </nav>
                    </aside>

                    <!-- Content -->
                    <div class="col-lg-9">
                        <h1 class="fw-bold mb-1"><i class="bi bi-book me-2 text-gradient"></i>Documentation</h1>
                        <p class="text-body-secondary mb-5">Everything you need to install WOR and run your first services. For the story of <em>why</em>, see the <a href="/">home page</a>.</p>

                        <!-- Install -->
                        <section id="install" class="nav-anchor mb-5">
                            <h2 class="fw-bold mb-1">Install</h2>
                            <p class="text-body-secondary mb-4">A single static Go binary — no runtime dependencies. The installer script supports Debian/Ubuntu; on macOS and Windows, install the bundled binary manually.</p>

                            <ul class="nav nav-tabs mb-4" id="installTabs" role="tablist">
                                <li class="nav-item" role="presentation">
                                    <button class="nav-link active" data-bs-toggle="tab" data-bs-target="#tab-linux" type="button" role="tab" aria-selected="true"><i class="bi bi-ubuntu me-1"></i>Linux</button>
                                </li>
                                <li class="nav-item" role="presentation">
                                    <button class="nav-link" data-bs-toggle="tab" data-bs-target="#tab-macos" type="button" role="tab" aria-selected="false"><i class="bi bi-apple me-1"></i>macOS</button>
                                </li>
                                <li class="nav-item" role="presentation">
                                    <button class="nav-link" data-bs-toggle="tab" data-bs-target="#tab-windows" type="button" role="tab" aria-selected="false"><i class="bi bi-windows me-1"></i>Windows</button>
                                </li>
                            </ul>

                            <div class="tab-content">
                                <div class="tab-pane fade show active" id="tab-linux" role="tabpanel">

                                    <h5 class="mt-2"><i class="bi bi-1-circle me-2 text-primary"></i>One-liner (latest release)</h5>
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
                                    <p class="text-body-secondary small mb-2">Both <code>.tar.gz</code> and <code>.zip</code> contain the same files; the folder inside is always <code>wor-host/</code>.</p>
                                    <div class="code-block mb-4">
                                        <pre><span class="prompt">$</span> curl -fsSL https://wor.worapong.com/download/releases/<?= htmlspecialchars($latestVersion) ?>.tar.gz -o wor.tar.gz
<span class="prompt">$</span> tar -xzf wor.tar.gz
<span class="prompt">$</span> cd wor-host
<span class="prompt">$</span> sudo ./install.sh</pre>
                                        <button class="btn btn-sm btn-copy" data-copy="curl -fsSL https://wor.worapong.com/download/releases/<?= htmlspecialchars($latestVersion) ?>.tar.gz -o wor.tar.gz
tar -xzf wor.tar.gz
cd wor-host
sudo ./install.sh" aria-label="Copy"><i class="bi bi-clipboard"></i></button>
                                    </div>

                                    <div class="alert alert-info d-flex gap-2" role="alert">
                                        <i class="bi bi-info-circle-fill"></i>
                                        <div><code>install.sh</code> detects your distro (Debian/Ubuntu only for now), reports which runtime packages are already installed, and asks before installing only the missing ones — it never upgrades or removes anything already present.</div>
                                    </div>

                                </div><!-- /tab-linux -->

                                <div class="tab-pane fade" id="tab-macos" role="tabpanel">

                                    <h5 class="mt-2"><i class="bi bi-1-circle me-2 text-primary"></i>Download &amp; install the binary</h5>
                                    <p class="text-body-secondary small mb-2">The archive bundles binaries for every platform. Apple Silicon uses <code>wor-macos-arm64</code>; Intel Macs use <code>wor-macos-amd64</code>.</p>
                                    <div class="code-block mb-4">
                                        <pre><span class="prompt">$</span> curl -fsSL https://wor.worapong.com/download/releases/latest.tar.gz -o wor.tar.gz
<span class="prompt">$</span> tar -xzf wor.tar.gz &amp;&amp; cd wor-host
<span class="prompt">$</span> sudo cp bin/wor-macos-arm64 /usr/local/bin/wor   <span class="cmt"># Intel: bin/wor-macos-amd64</span>
<span class="prompt">$</span> sudo chmod +x /usr/local/bin/wor</pre>
                                        <button class="btn btn-sm btn-copy" data-copy="curl -fsSL https://wor.worapong.com/download/releases/latest.tar.gz -o wor.tar.gz
tar -xzf wor.tar.gz && cd wor-host
sudo cp bin/wor-macos-arm64 /usr/local/bin/wor
sudo chmod +x /usr/local/bin/wor" aria-label="Copy"><i class="bi bi-clipboard"></i></button>
                                    </div>

                                    <h5><i class="bi bi-2-circle me-2 text-primary"></i>Install the runtimes you need</h5>
                                    <p class="text-body-secondary small mb-2">There is no automated package install on macOS — use Homebrew for whatever your services require. <code>wor doctor</code> reports exactly what's missing.</p>
                                    <div class="code-block mb-4">
                                        <pre><span class="prompt">$</span> brew install nginx node php go python   <span class="cmt"># pick what you need</span>
<span class="prompt">$</span> npm install -g pm2                       <span class="cmt"># process manager for services</span></pre>
                                        <button class="btn btn-sm btn-copy" data-copy="brew install nginx node php go python
npm install -g pm2" aria-label="Copy"><i class="bi bi-clipboard"></i></button>
                                    </div>

                                    <h5><i class="bi bi-3-circle me-2 text-primary"></i>Verify &amp; set up</h5>
                                    <div class="code-block mb-4">
                                        <pre><span class="prompt">$</span> wor version
<span class="prompt">$</span> wor doctor
<span class="prompt">$</span> wor setup</pre>
                                        <button class="btn btn-sm btn-copy" data-copy="wor version
wor doctor
wor setup" aria-label="Copy"><i class="bi bi-clipboard"></i></button>
                                    </div>

                                    <div class="alert alert-info d-flex gap-2" role="alert">
                                        <i class="bi bi-info-circle-fill"></i>
                                        <div>On macOS, <code>go</code> and <code>python</code> services run under PM2 instead of systemd, and PHP-FPM pools all run as your login user (no per-service privilege separation). Everything else works the same as on Linux.</div>
                                    </div>

                                </div><!-- /tab-macos -->

                                <div class="tab-pane fade" id="tab-windows" role="tabpanel">

                                    <h5 class="mt-2"><i class="bi bi-1-circle me-2 text-primary"></i>Download &amp; extract</h5>
                                    <p class="text-body-secondary small mb-2">Grab the <code>.zip</code> from the <a href="/download/">download page</a>, or in PowerShell:</p>
                                    <div class="code-block mb-4">
                                        <pre><span class="prompt">PS&gt;</span> Invoke-WebRequest https://wor.worapong.com/download/releases/<?= htmlspecialchars($latestVersion) ?>.zip -OutFile wor.zip
<span class="prompt">PS&gt;</span> Expand-Archive wor.zip -DestinationPath .
<span class="prompt">PS&gt;</span> cd wor-host</pre>
                                        <button class="btn btn-sm btn-copy" data-copy="Invoke-WebRequest https://wor.worapong.com/download/releases/<?= htmlspecialchars($latestVersion) ?>.zip -OutFile wor.zip
Expand-Archive wor.zip -DestinationPath .
cd wor-host" aria-label="Copy"><i class="bi bi-clipboard"></i></button>
                                    </div>

                                    <h5><i class="bi bi-2-circle me-2 text-primary"></i>Put <code>wor.exe</code> on your PATH</h5>
                                    <div class="code-block mb-4">
                                        <pre><span class="prompt">PS&gt;</span> New-Item -ItemType Directory -Force "$env:LOCALAPPDATA\wor\bin" | Out-Null
<span class="prompt">PS&gt;</span> Copy-Item bin\wor-windows-amd64.exe "$env:LOCALAPPDATA\wor\bin\wor.exe"
<span class="prompt">PS&gt;</span> [Environment]::SetEnvironmentVariable("Path", "$env:Path;$env:LOCALAPPDATA\wor\bin", "User")</pre>
                                        <button class="btn btn-sm btn-copy" data-copy="New-Item -ItemType Directory -Force &quot;$env:LOCALAPPDATA\wor\bin&quot; | Out-Null
Copy-Item bin\wor-windows-amd64.exe &quot;$env:LOCALAPPDATA\wor\bin\wor.exe&quot;
[Environment]::SetEnvironmentVariable(&quot;Path&quot;, &quot;$env:Path;$env:LOCALAPPDATA\wor\bin&quot;, &quot;User&quot;)" aria-label="Copy"><i class="bi bi-clipboard"></i></button>
                                    </div>

                                    <h5><i class="bi bi-3-circle me-2 text-primary"></i>Verify &amp; set up</h5>
                                    <p class="text-body-secondary small mb-2">Open a new terminal so the PATH change takes effect. Managing hosts entries requires an elevated (Administrator) terminal.</p>
                                    <div class="code-block mb-4">
                                        <pre><span class="prompt">PS&gt;</span> wor version
<span class="prompt">PS&gt;</span> wor doctor
<span class="prompt">PS&gt;</span> wor setup</pre>
                                        <button class="btn btn-sm btn-copy" data-copy="wor version
wor doctor
wor setup" aria-label="Copy"><i class="bi bi-clipboard"></i></button>
                                    </div>

                                    <div class="alert alert-info d-flex gap-2" role="alert">
                                        <i class="bi bi-info-circle-fill"></i>
                                        <div>On Windows, services run under PM2 (install Node.js + <code>npm install -g pm2</code>). PHP services share a single <code>PHP_FPM_ENDPOINT</code> — there are no per-service PHP-FPM pools, since PHP-FPM has no official Windows build. <code>wor doctor</code> reports any missing runtimes.</div>
                                    </div>

                                </div><!-- /tab-windows -->
                            </div><!-- /tab-content -->
                        </section>

                        <!-- Quick start -->
                        <section id="quickstart" class="nav-anchor mb-5">
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
                        </section>

                        <!-- Service templates -->
                        <section id="templates" class="nav-anchor mb-5">
                            <h2 class="fw-bold mb-3"><i class="bi bi-stars me-2 text-primary"></i>Service templates</h2>
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
                                        <tr><td><code>php</code></td><td>PHP-FPM</td><td>Dedicated PHP-FPM pool per service (Linux)</td><td><code>public/index.php</code></td></tr>
                                    </tbody>
                                </table>
                            </div>
                        </section>

                        <!-- Command reference -->
                        <section id="commands" class="nav-anchor">
                            <h2 class="fw-bold mb-1">Command reference</h2>
                            <?php if ($commandsHtml !== null): ?>
                            <p class="text-body-secondary">Generated from <code>docs/commands.md</code> — the same document bundled in every release archive. Run <code>wor &lt;command&gt; --help</code> for full flags.</p>
                            <?= $commandsHtml ?>
                            <?php else: ?>
                            <div class="alert alert-secondary d-flex gap-2" role="alert">
                                <i class="bi bi-info-circle-fill"></i>
                                <div>The command reference isn't available on this server. Read <a href="https://github.com/team-worapong/wor/blob/main/docs/commands.md" target="_blank" rel="noopener">docs/commands.md on GitHub</a>, or find it in every release archive.</div>
                            </div>
                            <?php endif; ?>
                        </section>

                    </div>
                </div>
            </div>
        </main>

        <footer class="border-top py-4 bg-body-tertiary">
            <div class="container d-flex flex-column flex-md-row justify-content-between align-items-center gap-2">
                <span class="text-body-secondary small">
                    <i class="bi bi-hdd-stack me-1"></i>WOR Host &copy; <?= date('Y') ?>
                </span>
                <span class="text-body-secondary small">
                    <a href="/" class="link-secondary me-3">Home</a>
                    <a href="/download/" class="link-secondary me-3">Downloads</a>
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
