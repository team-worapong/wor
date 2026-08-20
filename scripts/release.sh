#!/usr/bin/env bash
# scripts/release.sh
#
# Builds the full release matrix and packages it into two identical
# distributable archives (.zip and .tar.gz -- same contents, just two
# container formats so users can grab whichever is more convenient):
# bin/wor-<os>-<arch>[.exe] for every target plus install.sh, so a user
# only has to:
#
#   unzip v<version>-b<build>.zip          # or:
#   tar -xzf v<version>-b<build>.tar.gz
#   cd wor-host
#   sudo ./install.sh
#
# Both formats are produced because .zip is the more universally
# double-click-friendly format (especially on Windows), while .tar.gz
# is the more "native" format on Linux/macOS -- it needs no extra tool
# (tar ships with every Unix by default, unlike unzip, which isn't
# always preinstalled on minimal Debian images) and preserves the
# staged files' executable bits more reliably across platforms.
#
# This script does not build/package anything itself beyond calling
# scripts/build.sh --release (which does gofmt/vet/test + the 5-target
# cross-compile, see that script for details) and archiving its output
# together with scripts/install.sh.
#
# Usage:
#   ./scripts/release.sh [output-name] [--skip-build] [--no-publish] [--allow-dirty]
#
# --skip-build packages whatever is already in dist/bin instead of
# rebuilding it. It exists so a step can be inserted between building and
# packaging -- specifically Authenticode-signing the Windows binary, which
# happens off this machine (see .github/workflows/release.yml). Without
# it, release.sh would rebuild the matrix and silently overwrite the
# signed wor-windows-amd64.exe with an unsigned one, producing an archive
# that looks fine and is rejected by Smart App Control on the user's
# machine. The "expected build output missing" check below still applies,
# so passing --skip-build with an empty dist/bin fails loudly rather than
# packaging nothing.
#
# --no-publish skips the "Publishing" step at the end, which copies the
# finished archives and their checksum file into
# public/download/releases/ and points the download site at them (see
# "Publishing" below). It exists for .github/workflows/release.yml: a CI
# runner's public/ tree is thrown away when the job ends, so copying tens
# of megabytes into it and rewriting a page nobody will serve is pure
# waste there. The download site is published from a working copy on a
# real machine, which is the case where the default (publish) is what you
# want.
#
# --allow-dirty packages a work tree that still has uncommitted changes.
# Refused by default: the build number is a commit count, so a dirty tree
# means the tag names a commit that does not contain what is being
# packaged -- edit the version, forget to commit, and v1.0.2-b62 ships
# built from the commit that still said 1.0.1. Use it for throwaway
# builds, never for a release anybody else will install.
#
# [output-name] is optional and overrides the archive *filenames* only
# (not the folder name inside them -- see PKG_DIR below): e.g.
# `./scripts/release.sh v1-b2` produces dist/releases/v1-b2.zip and
# dist/releases/v1-b2.tar.gz instead of the default
# v<version>-b<build>.{zip,tar.gz}. This matters because
# scripts/installer.sh's documented `curl ... | bash -s -- <version>`
# flow downloads from a fixed URL template of exactly
# "<base-url>/<version>.tar.gz" -- the file
# uploaded there just has to be named to match the tag requested. The
# default computed here (v<version>-b<build>) already follows that tag
# scheme (README.md shows v1.0.0-b31), so the override is only for the
# rare case you need a filename that differs from it.
#
# Output: dist/releases/<output-name-or-default>.{zip,tar.gz,sha256},
# where the default is v<version>-b<build> -- <version> comes from
# internal/version/version.go (single source of truth for the version
# string -- see that package's doc comment) and <build> is the running
# commit count (git rev-list --count HEAD). Raw per-target binaries
# (scripts/build.sh's own output) live under dist/bin/ -- kept separate
# from dist/releases/ so packaged archives never collide with the loose
# binaries they're built from.
#
# Publishing (unless --no-publish): the three files above are copied into
# public/download/releases/, which is what the download site serves.
# Nothing there is ever deleted -- every published version stays
# downloadable, so the copy only ever adds files or overwrites the ones
# belonging to the tag being released. Every file goes in through a
# temporary name and is renamed into place, so a 29 MB archive is never
# visible half-written.
#
# Then two things make the new release live:
#
#   - any leftover latest.tar.gz is deleted. That URL is retired: it was a
#     stable name whose bytes changed every release, which behind
#     Cloudflare (where .gz is cached by default) meant the edge served
#     the previous build. Nothing points at it any more -- README.md and
#     the download page both go through installer.sh.
#   - public/lib/release-tag.php is regenerated with this release's tag,
#     version and build. **This is written last and it is what makes the
#     release live**: public/download/version.php serves whichever tag is
#     named there, so until this file changes the site still points at the
#     previous release. The installer asks version.php first and then
#     downloads the versioned archive it names -- a URL that never repeats
#     and so is safe for the edge to cache forever.
#
# public/download/index.php needs no such step -- it builds the whole
# download listing from whatever is in public/download/releases/ at
# request time.
#
# public/download/releases/ is gitignored (see .gitignore), so publishing
# does not commit archives into the repo. Only the release-tag.php change
# shows up in `git status`, which is intentional: it is the record of
# which release the site was last pointed at.
#
# Can be run from any directory; it resolves and cd's into the repo
# root first.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$ROOT_DIR"

USAGE="usage: ./scripts/release.sh [output-name] [--skip-build] [--no-publish] [--allow-dirty]"

for tool in zip tar; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "ERROR: $tool is not installed or not on PATH." >&2
    exit 1
  fi
done

# The checksum file is written with whichever of the two standard tools is
# present: sha256sum(1) is the GNU/coreutils name and is what Linux has,
# macOS ships shasum(1) instead. Both print "<hash>  <filename>" lines, so
# the file this produces is identical either way and verifies with either
# tool on either OS.
if command -v sha256sum >/dev/null 2>&1; then
  sha256_of() { sha256sum "$@"; }
elif command -v shasum >/dev/null 2>&1; then
  sha256_of() { shasum -a 256 "$@"; }
else
  echo "ERROR: neither sha256sum nor shasum is installed or on PATH (needed for the release checksums)." >&2
  exit 1
fi

OUTPUT_NAME=""
SKIP_BUILD=0
PUBLISH=1
ALLOW_DIRTY=0

while [ "$#" -gt 0 ]; do
  case "$1" in
    --skip-build)
      SKIP_BUILD=1
      ;;
    --no-publish)
      PUBLISH=0
      ;;
    --allow-dirty)
      ALLOW_DIRTY=1
      ;;
    --*)
      echo "ERROR: unknown option: $1" >&2
      echo "($USAGE)" >&2
      exit 1
      ;;
    *)
      if [ -n "$OUTPUT_NAME" ]; then
        echo "ERROR: too many arguments ($USAGE)" >&2
        exit 1
      fi
      OUTPUT_NAME="$1"
      ;;
  esac
  shift
done

# The character set is restricted rather than just rejecting '/' because
# output-name ends up in three places where a stray metacharacter would do
# something other than name a file: an archive filename, a URL path on the
# download site, and a single-quoted PHP string literal in the generated
# public/lib/release-tag.php (where a quote or a backslash would end or
# escape the literal). Release tags only ever use these characters --
# v1.0.0-b31 and the like -- so nothing previously valid is turned away.
case "$OUTPUT_NAME" in
  "") ;;
  *[!A-Za-z0-9._-]*)
    echo "ERROR: output-name may only contain letters, digits, '.', '_' and '-': $OUTPUT_NAME" >&2
    exit 1
    ;;
esac

VERSION_FILE="$ROOT_DIR/internal/version/version.go"
VERSION="$(sed -nE 's/^const Number = "(.*)"$/\1/p' "$VERSION_FILE")"
if [ -z "$VERSION" ]; then
  echo "ERROR: could not read version from $VERSION_FILE" >&2
  exit 1
fi

# BUILD is the running commit count on the current branch
# (git rev-list --count HEAD): a monotonic build number that increments
# by one per commit, matching the v<version>-b<build> naming documented
# in README.md and served from the download page (e.g. v1.0.0-b31 was
# HEAD at 31 commits). It is derived from git at package time rather
# than stored in a file so it can never drift out of sync with the
# commit it was actually built from; a release must therefore be
# packaged from inside the git work tree.
if ! command -v git >/dev/null 2>&1; then
  echo "ERROR: git is not installed or not on PATH (needed for the build number)." >&2
  exit 1
fi
BUILD="$(git -C "$ROOT_DIR" rev-list --count HEAD 2>/dev/null)" || BUILD=""
if [ -z "$BUILD" ]; then
  echo "ERROR: could not determine build number ('git rev-list --count HEAD' failed -- is $ROOT_DIR a git work tree with at least one commit?)." >&2
  exit 1
fi

# Refuse to package a tree with uncommitted changes.
#
# BUILD above is a commit count, so it only moves when you commit --
# editing internal/version/version.go and releasing without committing
# produces a tag like v1.0.2-b62 whose b62 names a commit that still
# says 1.0.1. The tag would then point at the wrong source, which is the
# one thing deriving it from git was meant to prevent. Go marks such a
# binary "-dirty" (see cliapp.formatCommit), but only in `wor version`,
# long after the archive has been published under a name nobody can
# tell is wrong.
#
# dist/ and public/download/releases/ are gitignored, so this does not
# fire on build output. It does fire on public/lib/release-tag.php after
# a previous release regenerated it -- commit that too; it is the record
# of what the site is serving.
if [ "$ALLOW_DIRTY" -eq 0 ]; then
  DIRTY="$(git -C "$ROOT_DIR" status --porcelain 2>/dev/null)"
  if [ -n "$DIRTY" ]; then
    echo "ERROR: the work tree has uncommitted changes, so build $BUILD does not describe what would be packaged." >&2
    echo "$DIRTY" | sed 's/^/    /' >&2
    echo "Commit them and re-run, or pass --allow-dirty for a throwaway build." >&2
    exit 1
  fi
fi

if [ "$SKIP_BUILD" -eq 1 ]; then
  echo "==> Skipping build (--skip-build): packaging the existing dist/bin"
  echo "    (version $VERSION, build $BUILD)"
else
  echo "==> Building release matrix (version $VERSION, build $BUILD)"
  "$SCRIPT_DIR/build.sh" --release
fi

PKG_NAME="wor-host"
if [ -z "$OUTPUT_NAME" ]; then
  OUTPUT_NAME="v${VERSION}-b${BUILD}"
fi
RELEASES_DIR="$ROOT_DIR/dist/releases"
ZIP_NAME="${OUTPUT_NAME}.zip"
TARGZ_NAME="${OUTPUT_NAME}.tar.gz"
SHA_NAME="${OUTPUT_NAME}.sha256"
ZIP_PATH="$RELEASES_DIR/$ZIP_NAME"
TARGZ_PATH="$RELEASES_DIR/$TARGZ_NAME"
SHA_PATH="$RELEASES_DIR/$SHA_NAME"

# The folder name *inside* both archives is deliberately version-less
# (wor-host/, not wor-host-1.0.0/) even though
# the archive filenames themselves carry the version and build -- so "cd
# wor-host" in install instructions never has to change
# between releases, only the filename the user downloads does.
STAGE_DIR="$(mktemp -d)"
trap 'rm -rf "$STAGE_DIR"' EXIT
PKG_DIR="$STAGE_DIR/$PKG_NAME"
mkdir -p "$PKG_DIR/bin"

echo "==> Staging package contents"
BINARIES=(
  wor-linux-amd64
  wor-linux-arm64
  wor-macos-amd64
  wor-macos-arm64
  wor-windows-amd64.exe
)
for bin in "${BINARIES[@]}"; do
  src="$ROOT_DIR/dist/bin/$bin"
  if [ ! -f "$src" ]; then
    echo "ERROR: expected build output missing: $src" >&2
    echo "(scripts/build.sh --release should have produced this -- did it change?)" >&2
    exit 1
  fi
  cp "$src" "$PKG_DIR/bin/$bin"
done
chmod +x "$PKG_DIR"/bin/wor-linux-* "$PKG_DIR"/bin/wor-macos-*

cp "$SCRIPT_DIR/install.sh" "$PKG_DIR/install.sh"
chmod +x "$PKG_DIR/install.sh"

# Wipe the whole dist/releases/ directory rather than just rm -f'ing this
# version's own zip/tar.gz path. Staging above already happens in a
# brand-new mktemp dir every run, so the *contents* of this run's
# archives are never stale -- but leftover archives from older versions
# (or a prior run of this script) sitting alongside the new ones in
# dist/releases/ is exactly the kind of thing that gets grabbed by
# mistake ("why does the tar.gz I just downloaded still have the old
# install.sh" is almost always someone opening an old file, not this
# script producing one). Clearing the directory first means whatever's
# in dist/releases/ after this script finishes is only ever this run's
# output.
rm -rf "$RELEASES_DIR"
mkdir -p "$RELEASES_DIR"

echo "==> Compressing (zip)"
echo "    Output : $ZIP_PATH"
(cd "$STAGE_DIR" && zip -rq "$ZIP_PATH" "$PKG_NAME")

echo "==> Compressing (tar.gz)"
echo "    Output : $TARGZ_PATH"
# tar preserves the executable bits chmod'd above natively on both GNU
# tar (Linux) and BSD tar (macOS) -- no extra flags needed for that.
#
# Two separate mechanisms can leak macOS-only metadata into the
# archive, and both need to be disabled to actually stop the
# "tar: Ignoring unknown extended header keyword
# 'LIBARCHIVE.xattr.com.apple.provenance'" warning GNU tar prints per
# file on the Linux/Debian install target when extracting:
#   - COPYFILE_DISABLE=1 stops cp(1)/bsdtar's copyfile()-based AppleDouble
#     sidecar behavior (the classic "._<name>" resource-fork files).
#   - --no-xattrs stops bsdtar (macOS's own tar) from writing a file's
#     actual extended attributes -- e.g. the "com.apple.provenance"
#     xattr Ventura+ attaches to files that were downloaded/quarantined
#     -- into the archive as LIBARCHIVE.xattr.* PAX headers in the
#     first place. COPYFILE_DISABLE alone does NOT suppress this: a
#     release built with only COPYFILE_DISABLE=1 (v1.0.0-b4) still
#     produced these warnings on a real Debian 13 VM extracting it,
#     confirming the xattr itself was still being written.
# Both are no-ops on Linux (GNU tar already defaults to not writing
# xattrs, and --no-xattrs is a recognized flag there too, confirmed
# against GNU tar 1.34 -- it does not error out), so it's safe to
# always pass them regardless of which OS actually builds the release.
(cd "$STAGE_DIR" && COPYFILE_DISABLE=1 tar --no-xattrs -czf "$TARGZ_PATH" "$PKG_NAME")

echo "==> Writing checksums"
echo "    Output : $SHA_PATH"
# Hashed from inside dist/releases so the file records bare filenames
# rather than absolute build-machine paths. That is what makes it usable
# on the other end: a user who downloaded both files into one directory
# can run `sha256sum -c <tag>.sha256` (or `shasum -a 256 -c <tag>.sha256`
# on macOS) there and have the names resolve.
(cd "$RELEASES_DIR" && sha256_of "$ZIP_NAME" "$TARGZ_NAME" > "$SHA_NAME")

echo
echo "[OK] Release packages ready:"
echo "    dist/releases/$ZIP_NAME"
echo "    dist/releases/$TARGZ_NAME"
echo "    dist/releases/$SHA_NAME"

if [ "$PUBLISH" -eq 0 ]; then
  echo
  echo "==> Skipping publish (--no-publish): public/ left untouched"
  exit 0
fi

PUBLIC_RELEASES_DIR="$ROOT_DIR/public/download/releases"
RELEASE_TAG_FILE="$ROOT_DIR/public/lib/release-tag.php"

# Checked before anything is copied so a repo whose public/ tree has been
# moved or renamed fails with one clear message, instead of half-publishing
# and then failing on the write below.
if [ ! -d "$ROOT_DIR/public/lib" ]; then
  echo "ERROR: expected the download site at $ROOT_DIR/public, but public/lib is not there." >&2
  echo "(release.sh publishes into public/ -- pass --no-publish if this checkout has no download site.)" >&2
  exit 1
fi

echo
echo "==> Publishing to public/download/releases"
mkdir -p "$PUBLIC_RELEASES_DIR"

# Every file goes in through a temporary name and is then renamed into
# place. A rename within one filesystem is atomic, so nothing in this
# directory is ever visible half-written -- which matters now that
# download/version.php decides what "latest" means by scanning here. A
# plain `cp` of a 29 MB tarball is a window several seconds wide in
# which a visitor can be handed a truncated archive.
publish() {
  local src="$1" name="$2"
  cp "$src" "$PUBLIC_RELEASES_DIR/$name.tmp"
  mv -f "$PUBLIC_RELEASES_DIR/$name.tmp" "$PUBLIC_RELEASES_DIR/$name"
  echo "    $name"
}

publish "$ZIP_PATH"   "$ZIP_NAME"
publish "$TARGZ_PATH" "$TARGZ_NAME"
# Last, deliberately. version.php treats a tag as published only once its
# .sha256 is present, so writing this file is what makes the release
# visible as "latest" -- and by then its archives are already complete.
publish "$SHA_PATH"   "$SHA_NAME"

# latest.tar.gz is retired, and removed here if an older release left one
# behind. It was the URL that made this whole split necessary: a stable
# name whose bytes change every release, served through Cloudflare where
# `.gz` is cached by default, so the edge went on handing out the
# previous build.
#
# Deleted rather than left in place, which is the one exception to "this
# directory is append-only" above. Left alone it would sit there frozen
# at whichever release last wrote it, and anyone still using that URL
# would keep installing that build forever without a hint that anything
# was wrong. A 404 says "this URL is gone, go look at the download page"
# on the first try.
if [ -f "$PUBLIC_RELEASES_DIR/latest.tar.gz" ]; then
  rm -f "$PUBLIC_RELEASES_DIR/latest.tar.gz"
  echo "    removed latest.tar.gz (retired; installs resolve via version.php)"
fi

# Written last, after every archive is in place, and this is the step
# that makes the release live: download/version.php serves whatever tag
# is named here, so until this file changes the site still points at the
# previous release. Publishing therefore has a single commit point
# rather than a window in which the site advertises a release whose
# 29 MB archive is still being copied.
#
# Generated rather than hand-edited because release.sh already knows the
# exact version and build it just packaged -- there is nothing for the
# site to work out. It replaces an older sed that patched a $latestVersion
# literal inside public/index.php, which meant one fragile regex against
# a 38 KB page every release.
cat > "$RELEASE_TAG_FILE.tmp" <<EOF
<?php
// Generated by scripts/release.sh on each publish -- do not edit by hand.
//
// The release currently published to public/download/releases/. Read via
// publishedReleaseTag() in public/lib/releases.php; served by
// public/download/version.php, which checks the archive is really on
// disk before handing this out.
const WOR_RELEASE_TAG     = '$OUTPUT_NAME';
const WOR_RELEASE_VERSION = '$VERSION';
const WOR_RELEASE_BUILD   = $BUILD;
EOF
mv -f "$RELEASE_TAG_FILE.tmp" "$RELEASE_TAG_FILE"

echo
echo "==> Updated public/lib/release-tag.php"
echo "    WOR_RELEASE_TAG -> $OUTPUT_NAME (version $VERSION, build $BUILD)"
echo
echo "[OK] Download site updated. public/download/releases/ is gitignored,"
echo "     so only the release-tag.php change shows up in 'git status'."
