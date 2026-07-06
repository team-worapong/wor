#Usage: curl -fsSL https://wor.worapong.com/download/installer.sh | bash
#!/usr/bin/env bash
set -euo pipefail

TMP_DIR="$(mktemp -d)"

cleanup() {
    rm -rf "$TMP_DIR"
}

trap cleanup EXIT

if ! command -v curl >/dev/null 2>&1; then
    echo "Error: curl is required but not installed."
    exit 1
fi

if ! command -v tar >/dev/null 2>&1; then
    echo "Error: tar is required but not installed."
    exit 1
fi

cd "$TMP_DIR"

# The query string is a cache-buster, not a real parameter -- latest.tar.gz
# gets overwritten in place on every release, and a plain repeated URL is
# exactly the kind of request a CDN/reverse-proxy cache will happily keep
# serving a stale copy of. Appending a value that changes every run (the
# current unix timestamp) makes most caches treat each request as a
# distinct, uncached URL, so this always reaches the origin server for a
# fresh copy. curl's -o flag still names the local file "latest.tar.gz"
# regardless of what's in the query string.
curl -fsSLo latest.tar.gz "https://wor.worapong.com/download/release/latest.tar.gz?_=$(date +%s)"

tar -xzf latest.tar.gz

if [[ ! -d wor-runtime-manager ]]; then
    echo "Error: Invalid release package."
    exit 1
fi

cd wor-runtime-manager

chmod +x install.sh

if [[ $EUID -eq 0 ]]; then
    exec ./install.sh
else
    exec sudo ./install.sh
fi
