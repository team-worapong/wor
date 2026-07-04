#!/usr/bin/env sh
set -eu

VERSION_FILE="VERSION"

if [ -f "$VERSION_FILE" ]; then
    VERSION="$(tr -d '\r\n' < "$VERSION_FILE")"
else
    VERSION="dev"
fi

TIMESTAMP="$(date +"%Y%m%d-%H%M%S")"

OUTPUT_DIR="backups"
OUTPUT_FILE="${OUTPUT_DIR}/wor_v${VERSION}_${TIMESTAMP}.zip"

echo "==> Running checks"
./scripts/check.sh

echo
echo "==> Packaging"

mkdir -p "$OUTPUT_DIR"

rm -f "$OUTPUT_FILE"

git ls-files --cached --others --exclude-standard -z \
    | xargs -0 zip -q "$OUTPUT_FILE"

echo
echo "✓ Package created"
echo "  ${OUTPUT_FILE}"
