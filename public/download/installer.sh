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
VERSION_URL="https://wor.worapong.com/download/version.php"

usage() {
    cat <<EOF
WOR Host Installer

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

# Resolve "latest" to the tag it currently means, then download that
# exact archive.
#
# The old path fetched releases/latest.tar.gz directly. That is a stable
# URL whose contents change every release, and the site is behind
# Cloudflare, so the edge kept handing out the previous build. Asking
# version.php first costs one small uncacheable request and means the 29
# MB file this downloads is a versioned, immutable URL that any cache is
# welcome to keep.
if [[ -z "$DOWNLOAD_URL" && "$VERSION" == "latest" ]]; then
    echo "Resolving latest release..."
    VERSION="$(curl -fsSL "$VERSION_URL" | tr -d '[:space:]')" || {
        echo "Error: could not reach $VERSION_URL to find the latest release."
        echo "Pass a version explicitly, e.g. bash installer.sh v1.0.1-b58"
        exit 1
    }
    if [[ -z "$VERSION" ]]; then
        echo "Error: $VERSION_URL did not name a release."
        exit 1
    fi
    echo "  latest is $VERSION"
fi

CHECKSUM_URL=""
if [[ -z "$DOWNLOAD_URL" ]]; then
    DOWNLOAD_URL="${BASE_URL}/${VERSION}.tar.gz"
    CHECKSUM_URL="${BASE_URL}/${VERSION}.sha256"
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

# Verify against the published checksum when there is one to verify
# against. release.sh has always written a <tag>.sha256 next to each
# archive and nothing ever read it; a 29 MB download that silently
# truncates is exactly what it is for.
#
# Skipped, with a warning, for --url (a custom build has no published
# checksum) and for anything else that has no .sha256 beside it. Not
# fatal in that case: refusing to install because a *checksum* is
# missing would break every older release that predates one.
if [[ -n "$CHECKSUM_URL" ]]; then
    if command -v sha256sum >/dev/null 2>&1; then
        sha256_check() { sha256sum -c --status "$1"; }
    elif command -v shasum >/dev/null 2>&1; then
        sha256_check() { shasum -a 256 -c --status "$1"; }
    else
        sha256_check() { return 2; }
    fi

    if curl -fsSL "$CHECKSUM_URL" -o published.sha256 2>/dev/null; then
        # The published file names the release archive (v1.0.1-b58.tar.gz),
        # not the local package.tar.gz, so give the local copy that name
        # before checking rather than rewriting the checksum file.
        expected_name="${VERSION}.tar.gz"
        cp package.tar.gz "$expected_name"
        # It also lists the .zip, which was never downloaded here.
        grep -F "  $expected_name" published.sha256 > expected.sha256 || true

        if [[ ! -s expected.sha256 ]]; then
            echo "Warning: published checksum does not cover $expected_name; skipping verification."
        elif sha256_check expected.sha256; then
            echo "Checksum OK."
        else
            rc=$?
            if [[ $rc -eq 2 ]]; then
                echo "Warning: neither sha256sum nor shasum is available; skipping verification."
            else
                echo "Error: checksum mismatch for $expected_name."
                echo "The download is corrupt or has been tampered with. Not installing."
                exit 1
            fi
        fi
        rm -f "$expected_name"
    else
        echo "Warning: no published checksum at $CHECKSUM_URL; skipping verification."
    fi
fi

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