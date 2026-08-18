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
#   ./scripts/release.sh [output-name] [--skip-build] [--no-publish]
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
# Publishing (unless --no-publish): the three files above are then copied
# into public/download/releases/, which is what the download site serves.
# Nothing there is ever deleted -- every published version stays
# downloadable, so the copy only ever adds files or overwrites the ones
# belonging to the tag being released. Two extra things happen so the
# site actually reflects the new release:
#
#   - latest.tar.gz is refreshed from this release's tar.gz. It is a real
#     copy rather than a symlink because it has to survive being moved
#     around by whatever copies public/ to the web host, and not every
#     such tool follows symlinks.
#   - the $latestVersion fallback in public/index.php is rewritten to this
#     release's tag. That page already derives the tag by scanning
#     public/download/releases/ at request time; the literal is only used
#     when that directory is empty or absent, and keeping it current means
#     that fallback is never a stale version number.
#
# public/download/index.php needs no such step -- it builds the whole
# download listing from whatever is in public/download/releases/ at
# request time.
#
# public/download/releases/ is gitignored (see .gitignore), so publishing
# does not commit archives into the repo. Only the public/index.php edit
# shows up in `git status`, which is intentional: it is a one-line record
# of which release the site was last pointed at.
#
# Can be run from any directory; it resolves and cd's into the repo
# root first.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$ROOT_DIR"

USAGE="usage: ./scripts/release.sh [output-name] [--skip-build] [--no-publish]"

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

while [ "$#" -gt 0 ]; do
  case "$1" in
    --skip-build)
      SKIP_BUILD=1
      ;;
    --no-publish)
      PUBLISH=0
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
# download site, and the sed replacement that rewrites $latestVersion in
# public/index.php (where '&' and '\' have their own meanings). Release
# tags only ever use these characters -- v1.0.0-b31 and the like -- so
# nothing that was previously valid is being turned away.
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
LANDING_PAGE="$ROOT_DIR/public/index.php"

# Checked before anything is copied so a repo whose public/ tree has been
# moved or renamed fails with one clear message, instead of half-publishing
# and then failing on the rewrite below.
if [ ! -f "$LANDING_PAGE" ]; then
  echo "ERROR: expected the landing page at $LANDING_PAGE, but it is not there." >&2
  echo "(release.sh publishes into public/ -- pass --no-publish if this checkout has no download site.)" >&2
  exit 1
fi

# The $latestVersion literal is a single-quoted PHP assignment on its own
# line near the top of public/index.php. Match it exactly and bail out if
# it is not found, rather than silently publishing archives while leaving
# the page advertising an older tag.
LATEST_VERSION_RE="^\\\$latestVersion = '[^']*';\$"
if ! grep -qE "$LATEST_VERSION_RE" "$LANDING_PAGE"; then
  echo "ERROR: could not find the \$latestVersion fallback line in $LANDING_PAGE." >&2
  echo "(expected a line of exactly: \$latestVersion = '<tag>';  -- has the file been restructured?)" >&2
  exit 1
fi

echo
echo "==> Publishing to public/download/releases"
mkdir -p "$PUBLIC_RELEASES_DIR"
cp "$ZIP_PATH"   "$PUBLIC_RELEASES_DIR/$ZIP_NAME"
cp "$TARGZ_PATH" "$PUBLIC_RELEASES_DIR/$TARGZ_NAME"
cp "$SHA_PATH"   "$PUBLIC_RELEASES_DIR/$SHA_NAME"
echo "    $ZIP_NAME"
echo "    $TARGZ_NAME"
echo "    $SHA_NAME"

# latest.tar.gz is the file public/download/installer.sh fetches when it is
# run without a version argument -- i.e. the `curl ... | bash` one-liner
# the download page recommends -- and it is what that page's "latest"
# card links to. Copy to a temporary name in the same directory and then
# rename it into place: a rename within one filesystem is atomic, so a
# visitor downloading latest.tar.gz while a release is being published
# gets either the whole previous file or the whole new one, never a
# half-written archive.
cp "$TARGZ_PATH" "$PUBLIC_RELEASES_DIR/latest.tar.gz.tmp"
mv -f "$PUBLIC_RELEASES_DIR/latest.tar.gz.tmp" "$PUBLIC_RELEASES_DIR/latest.tar.gz"
echo "    latest.tar.gz (copy of $TARGZ_NAME)"

# sed -i takes an argument on BSD/macOS and does not on GNU/Linux, so
# there is no spelling of it that works on both. Write to a temporary
# file and rename instead, which also means an interrupted run cannot
# leave a truncated index.php behind.
sed -E "s/$LATEST_VERSION_RE/\$latestVersion = '$OUTPUT_NAME';/" \
  "$LANDING_PAGE" > "$LANDING_PAGE.tmp"
mv -f "$LANDING_PAGE.tmp" "$LANDING_PAGE"

echo
echo "==> Updated public/index.php"
echo "    \$latestVersion fallback -> $OUTPUT_NAME"
echo
echo "[OK] Download site updated. public/download/releases/ is gitignored,"
echo "     so only the public/index.php edit shows up in 'git status'."
