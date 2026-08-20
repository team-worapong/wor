<?php
// download/version.php -- names the release the installer should fetch.
//
// This exists because `latest.tar.gz` is a stable URL whose bytes change
// every release, which is the one thing a CDN handles worst: the site
// sits behind Cloudflare, `.gz` is in Cloudflare's default cacheable
// extension list, and the edge went on serving the previous build to
// everyone who ran the install one-liner.
//
// Splitting it in two fixes that at the root instead of fighting it with
// cache headers on a 29 MB file. This response is tiny and uncacheable;
// the archive it names -- v1.0.1-b58.tar.gz -- is immutable, so the edge
// can hold that one forever and should.
//
//   GET version.php               -> "v1.0.1-b58\n"   (text/plain)
//   GET version.php?format=json   -> {"version","build","tag","tarball",...}
//
// Plain text is the default so the installer stays a bare `curl` with no
// jq dependency, matching how download/hcp/installer-hcp.sh already
// reads its own latest.txt.

require __DIR__ . '/../lib/releases.php';

// no-store rather than a short max-age: this response is the one thing
// that must never be stale, it is cheap to produce, and a stale answer
// here silently pins every new install to an old release -- the exact
// failure this endpoint was added to end.
header('Cache-Control: no-store, no-cache, must-revalidate, max-age=0');
header('Pragma: no-cache');

$format = ($_GET['format'] ?? 'text') === 'json' ? 'json' : 'text';

// 503 rather than 404 in both failure cases below: the endpoint is
// exactly where it should be, there is simply no release it can honestly
// name right now. That is a state worth retrying, and it is what a
// caller sees mid-publish.
function refuse(string $format, string $why): never
{
    http_response_code(503);
    if ($format === 'json') {
        header('Content-Type: application/json; charset=utf-8');
        echo json_encode(['error' => $why], JSON_PRETTY_PRINT), "\n";
    } else {
        header('Content-Type: text/plain; charset=utf-8');
        echo "error: $why\n";
    }
    exit;
}

$tag = publishedReleaseTag();
if ($tag === null) {
    refuse($format, 'no release has been published yet');
}

// The check that makes a stated answer safe. Pulling a bad release out
// of releases/ by hand is a normal thing to do in a hurry, and without
// this the constant would go on naming it -- sending every new install
// to a 404 partway through a download, which reads like the site is
// broken rather than like the release was withdrawn. Failing here
// instead is loud, and the fix is to re-run scripts/release.sh.
//
// Deliberately not "fall back to the previous release": quietly
// installing a version nobody asked for is worse than refusing.
$tarball = "releases/{$tag}.tar.gz";
if (!is_file(__DIR__ . '/' . $tarball)) {
    refuse($format, "published release $tag is not on disk");
}

if ($format === 'json') {
    header('Content-Type: application/json; charset=utf-8');
    echo json_encode([
        'tag'     => $tag,
        'version' => defined('WOR_RELEASE_VERSION') ? WOR_RELEASE_VERSION : null,
        'build'   => defined('WOR_RELEASE_BUILD') ? WOR_RELEASE_BUILD : null,
        'tarball' => $tarball,
        'zip'     => "releases/{$tag}.zip",
        'sha256'  => "releases/{$tag}.sha256",
    ], JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES), "\n";
    exit;
}

header('Content-Type: text/plain; charset=utf-8');
echo $tag, "\n";
