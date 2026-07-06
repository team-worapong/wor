#!/usr/bin/env bash
# scripts/build.sh
#
# Release build helper for wor. Writes to dist/bin/wor-<os>-<arch>[.exe],
# matching the naming convention documented in README.md's GOOS/GOARCH
# matrix. (Raw build output lives under dist/bin/ specifically so
# scripts/release.sh can put its packaged zips under dist/release/
# without the two colliding in the same directory.)
#
# Usage:
#   ./scripts/build.sh                  # build for this machine's OS/arch
#   ./scripts/build.sh <goos> <goarch>  # cross-compile for one target
#   ./scripts/build.sh --release        # build the full release matrix
#
# <goos> accepts "linux", "macos" (or "darwin"), or "windows". If <goos>
# is given without <goarch>, arch defaults to this machine's arch (and
# vice versa).
#
# Examples:
#   ./scripts/build.sh
#   ./scripts/build.sh linux arm64
#   ./scripts/build.sh windows
#   ./scripts/build.sh --release
#
# Can be run from any directory; it resolves and cd's into the repo
# root first.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$ROOT_DIR"

usage() {
  cat <<'EOF'
Usage:
  scripts/build.sh                  build for this machine's OS/arch
  scripts/build.sh <goos> <goarch>  cross-compile for one target
  scripts/build.sh --release        build the full release matrix

<goos>: linux | macos (or darwin) | windows
<goarch>: amd64 | arm64 | ... (any GOARCH your Go toolchain supports)
EOF
}

if ! command -v go >/dev/null 2>&1; then
  echo "ERROR: go is not installed or not on PATH." >&2
  exit 1
fi

HOST_GOOS="$(go env GOOS)"
HOST_GOARCH="$(go env GOARCH)"

# normalize_os maps an --os value (or README's "macos" label, or Go's
# actual GOOS value "darwin") to (GOOS_VALUE, OS_LABEL).
normalize_os() {
  case "$1" in
    mac|macos|darwin) echo "darwin macos" ;;
    linux) echo "linux linux" ;;
    windows|win) echo "windows windows" ;;
    *) return 1 ;;
  esac
}

#checking
checking() {
  echo "==> Checking"
  gofmt -l .
  go vet ./...
  
  echo "==> Running tests"
  go test ./...
}

# build_one GOOS_VALUE OS_LABEL GOARCH_VALUE
build_one() {
  local goos_value="$1" os_label="$2" goarch_value="$3"
  local bin_name="wor-${os_label}-${goarch_value}"
  if [ "$goos_value" = "windows" ]; then
    bin_name="${bin_name}.exe"
  fi
  local out_path="$ROOT_DIR/dist/bin/$bin_name"

  echo "==> Building"
  echo "    Target : $goos_value/$goarch_value"
  echo "    Output : $out_path"

  mkdir -p "$(dirname "$out_path")"

  #echo "==> go build ./cmd/wor"
  GOOS="$goos_value" GOARCH="$goarch_value" go build -o "$out_path" ./cmd/wor

  echo "[OK] Build complete: ./dist/bin/$bin_name"
}

case "${1:-}" in
  -h|--help)
    usage
    exit 0
    ;;
  --release)
    if [ "$#" -gt 1 ]; then
      echo "ERROR: --release does not take extra arguments" >&2
      usage
      exit 1
    fi

    checking

    # Matches the GOOS/GOARCH matrix documented in README.md.
    build_one linux linux amd64
    build_one linux linux arm64
    build_one darwin macos arm64
    build_one darwin macos amd64
    build_one windows windows amd64

    echo
    echo "[OK] Release build complete: $ROOT_DIR/dist/bin"
    exit 0
    ;;
  --*)
    echo "ERROR: unknown option: $1" >&2
    usage
    exit 1
    ;;
esac

if [ "$#" -gt 2 ]; then
  echo "ERROR: too many arguments" >&2
  usage
  exit 1
fi

GOOS_ARG="${1:-$HOST_GOOS}"
GOARCH_VALUE="${2:-$HOST_GOARCH}"

if ! read -r GOOS_VALUE OS_LABEL < <(normalize_os "$GOOS_ARG"); then
  echo "ERROR: unsupported goos: $GOOS_ARG (expected linux, macos, or windows)" >&2
  exit 1
fi

checking

build_one "$GOOS_VALUE" "$OS_LABEL" "$GOARCH_VALUE"
