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

# read_version reads the release version string from
# internal/version/version.go, the single source of truth for it (see
# that package's doc comment). scripts/release.sh reads the same file
# with the same expression to name its archives; this copy exists so a
# plain `scripts/build.sh windows` produces a correctly stamped binary
# without having to go through release.sh.
read_version() {
  local version_file="$ROOT_DIR/internal/version/version.go"
  local version
  version="$(sed -nE 's/^const Number = "(.*)"$/\1/p' "$version_file")"
  if [ -z "$version" ]; then
    echo "ERROR: could not read version from $version_file" >&2
    exit 1
  fi
  echo "$version"
}

# make_windows_resource compiles winres/winres.json into
# cmd/wor/rsrc_windows_<arch>.syso, which `go build` links in
# automatically -- and only for that GOOS/GOARCH, because of the
# filename suffix, so the linux/macos targets are untouched.
#
# This is not cosmetic polish. Go emits no Win32 VERSIONINFO resource of
# its own, so without this step wor.exe reports an empty ProductName and
# ProductVersion, and SignPath (which enforces both as a signing
# precondition -- see .signpath/artifact-configurations/default.xml)
# refuses to sign it. The version stamped here therefore has to stay
# equal to internal/version/version.go, which is why it is read from
# there rather than passed in.
#
# A missing go-winres is a hard error rather than a warning, matching how
# wor itself blocks `service add` when a template's runtime is absent: a
# build that quietly produced an unsignable binary would only fail much
# later, in CI, after a human already approved the signing request.
make_windows_resource() {
  local goarch_value="$1"
  local version

  if ! command -v go-winres >/dev/null 2>&1; then
    echo "ERROR: go-winres is not installed or not on PATH." >&2
    echo "It is required to build the windows target, because Go does not emit a" >&2
    echo "VERSIONINFO resource by itself and releases must carry one to be signed." >&2
    echo "Install it with:" >&2
    echo "    go install github.com/tc-hib/go-winres@latest" >&2
    echo "(then make sure \$(go env GOPATH)/bin is on your PATH)" >&2
    exit 1
  fi

  version="$(read_version)"

  echo "==> Generating Windows resource"
  echo "    Version: $version"
  go-winres make \
    --in "$ROOT_DIR/winres/winres.json" \
    --arch "$goarch_value" \
    --product-version "$version" \
    --file-version "$version.0" \
    --out "$ROOT_DIR/cmd/wor/rsrc"
}

# read_build reports the running commit count (git rev-list --count HEAD),
# the same number scripts/release.sh names a tag's "-b<n>" suffix after.
# Stamped into the binary below so `wor version` and `wor upgrade` can
# tell two releases of the same version number apart -- 1.0.1 covered
# builds 51 through 58, and Go embeds the commit SHA automatically but
# not the commit count.
#
# Derived here rather than passed in by release.sh so a plain
# `./scripts/build.sh linux amd64` stamps the same number a release
# would at that commit. Empty outside a git work tree, which is not an
# error: the binary just reports its build as unknown.
read_build() {
  if ! command -v git >/dev/null 2>&1; then
    echo ""
    return 0
  fi
  git -C "$ROOT_DIR" rev-list --count HEAD 2>/dev/null || echo ""
}

# build_one GOOS_VALUE OS_LABEL GOARCH_VALUE
build_one() {
  local goos_value="$1" os_label="$2" goarch_value="$3"
  local bin_name="wor-${os_label}-${goarch_value}"
  if [ "$goos_value" = "windows" ]; then
    bin_name="${bin_name}.exe"
  fi
  local out_path="$ROOT_DIR/dist/bin/$bin_name"

  if [ "$goos_value" = "windows" ]; then
    make_windows_resource "$goarch_value"
  fi

  echo "==> Building"
  echo "    Target : $goos_value/$goarch_value"
  echo "    Output : $out_path"

  mkdir -p "$(dirname "$out_path")"

  local build_number
  build_number="$(read_build)"
  if [ -n "$build_number" ]; then
    echo "    Build  : $build_number"
  fi

  #echo "==> go build ./cmd/wor"
  GOOS="$goos_value" GOARCH="$goarch_value" go build \
    -ldflags "-X wor/internal/version.Build=$build_number" \
    -o "$out_path" ./cmd/wor

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

    # Wipe dist/bin/ before rebuilding the release matrix. build_one's
    # go build -o always fully overwrites its own named target file, so
    # this isn't fixing stale *contents* -- it's a guard against
    # binaries orphaned by a future change to the fixed 5-target matrix
    # below (a renamed/dropped target would otherwise leave its old
    # dist/bin/wor-<os>-<arch> sitting there forever with nothing to
    # remove it). Scoped to --release only: a single-target build
    # (./scripts/build.sh <goos> <goarch>) must not wipe other targets'
    # binaries a developer may still want lying around from a previous
    # run.
    rm -rf "$ROOT_DIR/dist/bin"

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
