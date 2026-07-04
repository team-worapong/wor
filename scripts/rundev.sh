#!/usr/bin/env bash
# scripts/rundev.sh
#
# Dev build helper for wor. Runs `go vet` (static check) then `go build`
# for whatever OS/arch the script is currently running on, and writes the
# binary to dist/dev/wor (or dist/dev/wor.exe on Windows).
#
# Usage:
#   ./scripts/rundev.sh
#
# This is a dev-only convenience script; it does not cross-compile for
# other platforms (see README.md for the GOOS/GOARCH release matrix).

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$ROOT_DIR"

if ! command -v go >/dev/null 2>&1; then
  echo "ERROR: go is not installed or not on PATH." >&2
  exit 1
fi

GOOS="$(go env GOOS)"
GOARCH="$(go env GOARCH)"

BIN_NAME="wor"
if [[ "$GOOS" == "windows" ]]; then
  BIN_NAME="wor.exe"
fi

OUT_DIR="$ROOT_DIR/dist/dev"
OUT_PATH="$OUT_DIR/$BIN_NAME"

echo "==> wor dev build"
echo "    OS/Arch : $GOOS/$GOARCH"
echo "    Output  : $OUT_PATH"
echo

echo "==> go vet ./..."
go vet ./...

mkdir -p "$OUT_DIR"

echo "==> go build ./cmd/wor"
go build -o "$OUT_PATH" ./cmd/wor

echo
echo "[OK] Build complete: $OUT_PATH"
