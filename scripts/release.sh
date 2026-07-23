#!/usr/bin/env bash
# scripts/release.sh
#
# Builds the full release matrix and packages it into two identical
# distributable archives (.zip and .tar.gz -- same contents, just two
# container formats so users can grab whichever is more convenient):
# bin/wor-<os>-<arch>[.exe] for every target plus install.sh, so a user
# only has to:
#
#   unzip wor-runtime-manager-<version>.zip          # or:
#   tar -xzf wor-runtime-manager-<version>.tar.gz
#   cd wor-runtime-manager
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
#   ./scripts/release.sh [output-name]
#
# [output-name] is optional and overrides the archive *filenames* only
# (not the folder name inside them -- see PKG_DIR below): e.g.
# `./scripts/release.sh v1-b2` produces dist/releases/v1-b2.zip and
# dist/releases/v1-b2.tar.gz instead of the default
# wor-runtime-manager-<version>.{zip,tar.gz}. This matters because
# scripts/installer.sh's documented `curl ... | bash -s -- <version>`
# flow downloads from a fixed URL template of exactly
# "<base-url>/<version>.tar.gz" -- there is no
# "wor-runtime-manager-" prefix on the server side, so whatever gets
# uploaded there has to be named to match the version tag being
# requested (e.g. a v1.0.0 release must be uploaded as v1.0.0.tar.gz),
# not this script's own default naming.
#
# Output: dist/releases/<output-name-or-default>.{zip,tar.gz}, where the
# default is wor-runtime-manager-<version> and <version> comes from
# internal/version/version.go (single source of truth for the version
# string -- see that package's doc comment). Raw per-target binaries
# (scripts/build.sh's own output) live under dist/bin/ -- kept separate
# from dist/releases/ so packaged archives never collide with the loose
# binaries they're built from.
#
# Can be run from any directory; it resolves and cd's into the repo
# root first.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$ROOT_DIR"

for tool in zip tar; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "ERROR: $tool is not installed or not on PATH." >&2
    exit 1
  fi
done

if [ "$#" -gt 1 ]; then
  echo "ERROR: too many arguments (usage: ./scripts/release.sh [output-name])" >&2
  exit 1
fi

OUTPUT_NAME="${1:-}"
case "$OUTPUT_NAME" in
  */*)
    echo "ERROR: output-name must not contain '/': $OUTPUT_NAME" >&2
    exit 1
    ;;
esac

VERSION_FILE="$ROOT_DIR/internal/version/version.go"
VERSION="$(sed -nE 's/^const Number = "(.*)"$/\1/p' "$VERSION_FILE")"
if [ -z "$VERSION" ]; then
  echo "ERROR: could not read version from $VERSION_FILE" >&2
  exit 1
fi

echo "==> Building release matrix (version $VERSION)"
"$SCRIPT_DIR/build.sh" --release

PKG_NAME="wor-runtime-manager"
if [ -z "$OUTPUT_NAME" ]; then
  OUTPUT_NAME="${PKG_NAME}-${VERSION}"
fi
ZIP_NAME="${OUTPUT_NAME}.zip"
TARGZ_NAME="${OUTPUT_NAME}.tar.gz"
ZIP_PATH="$ROOT_DIR/dist/releases/$ZIP_NAME"
TARGZ_PATH="$ROOT_DIR/dist/releases/$TARGZ_NAME"

# The folder name *inside* both archives is deliberately version-less
# (wor-runtime-manager/, not wor-runtime-manager-1.0.0/) even though
# the archive filenames themselves carry the version -- so "cd
# wor-runtime-manager" in install instructions never has to change
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
rm -rf "$ROOT_DIR/dist/releases"
mkdir -p "$ROOT_DIR/dist/releases"

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

echo
echo "[OK] Release packages ready:"
echo "    dist/releases/$ZIP_NAME"
echo "    dist/releases/$TARGZ_NAME"
