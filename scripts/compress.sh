#!/usr/bin/env bash
# scripts/compress.sh
#
# Packages a zip snapshot of the repo (git-tracked files, plus untracked
# files not excluded by .gitignore) into backups/wor_<timestamp>.zip.
# This is a working-tree file snapshot, not a full git-history backup --
# .git/ itself is not included.
#
# Usage:
#   ./scripts/compress.sh
#
# Can be run from any directory; it resolves and cd's into the repo
# root first.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$ROOT_DIR"

TIMESTAMP="$(date +"%Y%m%d-%H%M%S")"

OUTPUT_DIR="$ROOT_DIR/backups"
OUTPUT_FILE="${OUTPUT_DIR}/wor_${TIMESTAMP}.zip"

echo "==> Packaging"
echo "    Root   : $ROOT_DIR"

echo "==> Checking"
go fmt ./...
go vet ./...

echo "==> Running tests"
go test ./...

mkdir -p "$OUTPUT_DIR"
rm -f "$OUTPUT_FILE"

echo "==> Compressing"
echo "    Output : $OUTPUT_FILE"
git ls-files --cached --others --exclude-standard -z \
    | xargs -0 zip -q "$OUTPUT_FILE"

echo "✓ Done!"