#!/usr/bin/env bash

# Usage:
#   curl -fsSL https://wor.worapong.com/download/installer.sh | bash
#
# Specific version:
#   curl -fsSL https://wor.worapong.com/download/installer.sh | \
#       bash -s -- v1.0.0
#
# Beta / RC:
#   curl -fsSL https://wor.worapong.com/download/installer.sh | \
#       bash -s -- v1.0.0b5
#
# Custom package URL (Developer only):
#   curl -fsSL https://wor.worapong.com/download/installer.sh | \
#       bash -s -- --url https://example.com/test-build.tar.gz

set -euo pipefail

BASE_URL="https://wor.worapong.com/download/releases"

usage() {
    cat <<EOF
WOR Runtime Manager Installer

Usage:
  bash installer.sh
      Install latest release.

  bash installer.sh <version>
      Install a specific release.

  bash installer.sh --url <package-url>
      Install from a custom package URL.

Examples:
  bash installer.sh
  bash installer.sh v1.0.0
  bash installer.sh v1.0.0b5
  bash installer.sh --url https://example.com/test-build.tar.gz
EOF
}

VERSION="latest"
DOWNLOAD_URL=""

case "${1:-}" in
    "")
        ;;
    --url)
        [[ $# -ge 2 ]] || {
            echo "Error: Missing URL."
            exit 1
        }
        DOWNLOAD_URL="$2"
        ;;
    -h|--help)
        usage
        exit 0
        ;;
    *)
        VERSION="$1"
        ;;
esac

if [[ -z "$DOWNLOAD_URL" ]]; then
    DOWNLOAD_URL="${BASE_URL}/${VERSION}.tar.gz"
fi

TMP_DIR="$(mktemp -d)"

cleanup() {
    rm -rf "$TMP_DIR"
}

trap cleanup EXIT

cd "$TMP_DIR"

echo "Downloading package..."
echo "  $DOWNLOAD_URL"

curl -fsSL "$DOWNLOAD_URL" -o package.tar.gz

echo "Extracting package..."

tar -xzf package.tar.gz

INSTALL_SCRIPT="$(find . -type f -name install.sh | head -n1)"

if [[ -z "$INSTALL_SCRIPT" ]]; then
    echo "Error: install.sh not found in package."
    exit 1
fi

chmod +x "$INSTALL_SCRIPT"

cd "$(dirname "$INSTALL_SCRIPT")"

echo "Starting installer..."

if [[ $EUID -eq 0 ]]; then
    exec ./install.sh
else
    exec sudo ./install.sh
fi