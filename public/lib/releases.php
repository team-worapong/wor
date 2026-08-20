<?php
// Shared release logic for the download site.
//
// Defines functions only and emits no output, so including it before
// headers are sent is safe.

// The file scripts/release.sh regenerates on every publish. It is
// tracked in git, unlike public/download/releases/ itself, so a checkout
// carries a record of which release the site is pointing at.
const RELEASE_TAG_FILE = __DIR__ . '/release-tag.php';

// publishedReleaseTag returns the tag scripts/release.sh last published,
// or null if the site has never published one.
//
// Read from a generated constant rather than inferred by scanning the
// releases directory. release.sh already knows the exact version and
// build it just packaged, so having PHP work it back out from filenames
// was solving a problem nobody had -- and it meant the answer depended
// on a sorting rule ("does v1.0.2 outrank v1.0.2-b70?") that had to be
// got right at request time. The publisher states the answer instead.
//
// The one thing a stated answer cannot notice is the file being removed
// afterwards -- pulling a bad release out of releases/ by hand would
// leave this naming something that is no longer there. That is why
// callers check the archive exists before handing the tag out; see
// download/version.php.
function publishedReleaseTag(): ?string
{
    if (is_file(RELEASE_TAG_FILE)) {
        require_once RELEASE_TAG_FILE;
    }
    return defined('WOR_RELEASE_TAG') && WOR_RELEASE_TAG !== '' ? WOR_RELEASE_TAG : null;
}

// versionKey turns a tag into a sortable key: v1.0.0-b32 -> [1,0,0,32].
//
// Only the download listing needs this now, to order the releases it
// shows. A tag with no build suffix sorts *above* every build of the
// same version (PHP_INT_MAX), so a final v1.0.1 is listed ahead of
// v1.0.1-b58: the build suffix marks a pre-release of the version it
// names, not a later revision of it.
function versionKey(string $tag): array
{
    if (!preg_match('/^v(\d+)\.(\d+)\.(\d+)(?:-?b(\d+))?/i', $tag, $m)) {
        return [0, 0, 0, 0];
    }
    return [
        (int) $m[1],
        (int) $m[2],
        (int) $m[3],
        isset($m[4]) && $m[4] !== '' ? (int) $m[4] : PHP_INT_MAX,
    ];
}
