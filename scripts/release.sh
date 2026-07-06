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
#   ./scripts/release.sh
#
# Output: dist/release/wor-runtime-manager-<version>.{zip,tar.gz},
# where <version> comes from internal/version/version.go (single
# source of truth for the version string -- see that package's doc
# comment). Raw per-target binaries (scripts/build.sh's own output)
# live under dist/bin/ -- kept separate from dist/release/ so packaged
# archives never collide with the loose binaries they're built from.
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

VERSION_FILE="$ROOT_DIR/internal/version/version.go"
VERSION="$(sed -nE 's/^const Number = "(.*)"$/\1/p' "$VERSION_FILE")"
if [ -z "$VERSION" ]; then
  echo "ERROR: could not read version from $VERSION_FILE" >&2
  exit 1
fi

echo "==> Building release matrix (version $VERSION)"
"$SCRIPT_DIR/build.sh" --release

PKG_NAME="wor-runtime-manager"
ZIP_NAME="${PKG_NAME}-${VERSION}.zip"
TARGZ_NAME="${PKG_NAME}-${VERSION}.tar.gz"
ZIP_PATH="$ROOT_DIR/dist/release/$ZIP_NAME"
TARGZ_PATH="$ROOT_DIR/dist/release/$TARGZ_NAME"

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

# Wipe the whole dist/release/ directory rather than just rm -f'ing this
# version's own zip/tar.gz path. Staging above already happens in a
# brand-new mktemp dir every run, so the *contents* of this run's
# archives are never stale -- but leftover archives from older versions
# (or a prior run of this script) sitting alongside the new ones in
# dist/release/ is exactly the kind of thing that gets grabbed by
# mistake ("why does the tar.gz I just downloaded still have the old
# install.sh" is almost always someone opening an old file, not this
# script producing one). Clearing the directory first means whatever's
# in dist/release/ after this script finishes is only ever this run's
# output.
rm -rf "$ROOT_DIR/dist/release"
mkdir -p "$ROOT_DIR/dist/release"

echo "==> Compressing (zip)"
echo "    Output : $ZIP_PATH"
(cd "$STAGE_DIR" && zip -rq "$ZIP_PATH" "$PKG_NAME")

echo "==> Compressing (tar.gz)"
echo "    Output : $TARGZ_PATH"
# tar preserves the executable bits chmod'd above natively on both GNU
# tar (Linux) and BSD tar (macOS) -- no extra flags needed for that.
# COPYFILE_DISABLE=1 is a macOS-only bsdtar/cp setting that stops it
# from embedding Apple extended attributes (e.g. the
# "com.apple.provenance" xattr Ventura+ attaches) as PAX extended
# headers in the archive -- harmless on this machine either way, but
# without it, GNU tar on the Linux/Debian install target prints a
# "tar: Ignoring unknown extended header keyword ..." warning per file
# when extracting. Purely cosmetic (extraction still succeeds either
# way), but confusing enough during a fresh install that it's worth
# not shipping. No-op on Linux, so this env var is safe to always set
# regardless of which OS actually builds the release.
(cd "$STAGE_DIR" && COPYFILE_DISABLE=1 tar -czf "$TARGZ_PATH" "$PKG_NAME")

echo
echo "[OK] Release packages ready:"
echo "    dist/release/$ZIP_NAME"
echo "    dist/release/$TARGZ_NAME"
