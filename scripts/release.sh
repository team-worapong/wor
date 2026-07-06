#!/usr/bin/env bash
# scripts/release.sh
#
# Builds the full release matrix and packages it into a single
# distributable zip: bin/wor-<os>-<arch>[.exe] for every target plus
# install.sh, so a user only has to:
#
#   unzip wor-runtime-manager-<version>.zip
#   cd wor-runtime-manager
#   sudo ./install.sh
#
# This script does not build/package anything itself beyond calling
# scripts/build.sh --release (which does gofmt/vet/test + the 5-target
# cross-compile, see that script for details) and zipping its output
# together with scripts/install.sh.
#
# Usage:
#   ./scripts/release.sh
#
# Output: dist/release/wor-runtime-manager-<version>.zip, where
# <version> comes from internal/version/version.go (single source of
# truth for the version string -- see that package's doc comment).
# Raw per-target binaries (scripts/build.sh's own output) live under
# dist/bin/ -- kept separate from dist/release/ so packaged zips never
# collide with the loose binaries they're built from.
#
# Can be run from any directory; it resolves and cd's into the repo
# root first.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$ROOT_DIR"

if ! command -v zip >/dev/null 2>&1; then
  echo "ERROR: zip is not installed or not on PATH." >&2
  exit 1
fi

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
ZIP_PATH="$ROOT_DIR/dist/release/$ZIP_NAME"

# The folder name *inside* the zip is deliberately version-less
# (wor-runtime-manager/, not wor-runtime-manager-1.0.0/) even though
# the zip filename itself carries the version -- so "cd
# wor-runtime-manager" in install instructions never has to change
# between releases, only the zip filename the user downloads does.
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

echo "==> Compressing"
echo "    Output : $ZIP_PATH"
mkdir -p "$ROOT_DIR/dist/release"
rm -f "$ZIP_PATH"
(cd "$STAGE_DIR" && zip -rq "$ZIP_PATH" "$PKG_NAME")

echo
echo "[OK] Release package ready: dist/release/$ZIP_NAME"
