#!/usr/bin/env bash
# scripts/install.sh
#
# Server-side installer for a wor release package. Meant to be shipped
# alongside a prebuilt bin/ directory (see README.md's GOOS/GOARCH
# matrix / scripts/build.sh --release), e.g.:
#
#   bin/wor-linux-amd64
#   bin/wor-linux-arm64
#   bin/wor-macos-amd64
#   bin/wor-macos-arm64
#   bin/wor-windows-amd64.exe
#   install.sh
#
# It does NOT build wor -- it installs the OS packages every wor
# service template (static/node/go/python/php) needs on a fresh
# server, then copies the matching prebuilt binary for this machine's
# OS/arch into place.
#
# Usage:
#   sudo ./install.sh [options]
#
# The Linux distro family is auto-detected from /etc/os-release -- no
# flag needed. Only Debian/Ubuntu (apt) is actually implemented right
# now; RHEL/CentOS-family (dnf/yum) is recognized and routed to its own
# install_rhel() function so it's a small, contained job to fill in
# later, but it currently just errors out rather than guessing at
# package names it hasn't been verified against (see install_rhel()
# below for what still needs doing).
#
# Options:
#   --host-provider=NAME     nginx or apache (default: nginx)
#   --with-mysql             also install mariadb-server (mysql-compatible)
#   --with-postgres          also install postgresql
#   --with-redis             also install redis-server
#   --skip-ssl               don't install certbot
#   --install-dir=PATH       where to place the wor binary (default: /usr/local/bin)
#   -h, --help
#
# Database engines and certbot are the only optional pieces -- they're
# not required by any service template, only by `wor database ...`/
# `wor ssl issue --provider=letsencrypt` if and when you use those.
#
# Can be run from any directory; it resolves its own location first so
# `bin/` is found relative to this script, not relative to the caller's
# cwd.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

usage() {
  cat <<'EOF'
Usage:
  sudo ./install.sh [options]

The Linux distro is auto-detected from /etc/os-release -- no flag
needed. Only Debian/Ubuntu (apt) is implemented right now.

Options:
  --host-provider=NAME     nginx or apache (default: nginx)
  --with-mysql             also install mariadb-server
  --with-postgres          also install postgresql
  --with-redis             also install redis-server
  --skip-ssl               don't install certbot
  --install-dir=PATH       where to place the wor binary (default: /usr/local/bin)
  -h, --help
EOF
}

# ---- defaults ---------------------------------------------------------

HOST_PROVIDER="nginx"
WITH_MYSQL=0
WITH_POSTGRES=0
WITH_REDIS=0
SKIP_SSL=0
INSTALL_DIR="/usr/local/bin"

# ---- arg parsing --------------------------------------------------------

for arg in "$@"; do
  case "$arg" in
    --host-provider=*)
      HOST_PROVIDER="${arg#*=}"
      ;;
    --with-mysql)
      WITH_MYSQL=1
      ;;
    --with-postgres)
      WITH_POSTGRES=1
      ;;
    --with-redis)
      WITH_REDIS=1
      ;;
    --skip-ssl)
      SKIP_SSL=1
      ;;
    --install-dir=*)
      INSTALL_DIR="${arg#*=}"
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "ERROR: unknown option: $arg" >&2
      usage
      exit 1
      ;;
  esac
done

case "$HOST_PROVIDER" in
  nginx|apache) ;;
  *)
    echo "ERROR: --host-provider must be nginx or apache, got: $HOST_PROVIDER" >&2
    exit 1
    ;;
esac

# ---- OS family auto-detection ------------------------------------------

# ID/ID_LIKE/PRETTY_NAME come from sourcing /etc/os-release below.
ID=""
ID_LIKE=""
PRETTY_NAME=""
if [ -r /etc/os-release ]; then
  # shellcheck disable=SC1091
  . /etc/os-release
fi

# id_like_has FAMILY checks both ID and the (possibly multi-value)
# ID_LIKE field, since derivatives set ID to their own name and rely on
# ID_LIKE to say what they're compatible with (e.g. Ubuntu is
# ID=ubuntu, ID_LIKE=debian; Rocky Linux is ID=rocky, ID_LIKE="rhel
# centos fedora").
id_like_has() {
  local family="$1"
  [ "${ID:-}" = "$family" ] && return 0
  for like in ${ID_LIKE:-}; do
    [ "$like" = "$family" ] && return 0
  done
  return 1
}

OS_FAMILY="unknown"
if id_like_has debian; then
  OS_FAMILY="debian"
elif id_like_has rhel || id_like_has fedora || id_like_has centos; then
  OS_FAMILY="rhel"
fi

if [ "$OS_FAMILY" = "unknown" ]; then
  echo "ERROR: auto-detected OS is '${PRETTY_NAME:-${ID:-unknown}}', which this" >&2
  echo "installer doesn't recognize as Debian/Ubuntu or RHEL/CentOS family." >&2
  echo "Install the runtimes manually instead (see docs/services.md for the full" >&2
  echo "list wor needs: git, go, node+npm+pm2, python3+pip, php-fpm, nginx/apache)" >&2
  echo "and then just place the matching bin/wor-<os>-<arch> yourself." >&2
  exit 1
fi

# ---- root check ---------------------------------------------------------

if [ "$(id -u)" -ne 0 ]; then
  echo "ERROR: this script installs system packages and must be run as root." >&2
  echo "Re-run as: sudo ./install.sh ..." >&2
  exit 1
fi

echo "==> Detected: ${PRETTY_NAME:-${ID:-unknown}} (family: $OS_FAMILY)"

# ---- family-specific install ------------------------------------------

install_debian() {
  if ! command -v apt-get >/dev/null 2>&1; then
    echo "ERROR: apt-get not found even though /etc/os-release looks Debian-family." >&2
    exit 1
  fi

  echo "==> apt-get update"
  apt-get update -qq

  local packages=(
    git
    golang-go
    nodejs
    npm
    python3
    python3-pip
    php-fpm
    php-cli
  )

  # webserver_service is the real apt package / systemd unit name --
  # HOST_PROVIDER's "apache" value (matching wor's own --host-provider
  # naming) isn't the actual package name on Debian, which is "apache2".
  local webserver_service
  case "$HOST_PROVIDER" in
    nginx) packages+=(nginx); webserver_service="nginx" ;;
    apache) packages+=(apache2); webserver_service="apache2" ;;
  esac

  if [ "$SKIP_SSL" -eq 0 ]; then
    packages+=(certbot)
    case "$HOST_PROVIDER" in
      nginx) packages+=(python3-certbot-nginx) ;;
      apache) packages+=(python3-certbot-apache) ;;
    esac
  fi

  if [ "$WITH_MYSQL" -eq 1 ]; then
    packages+=(mariadb-server mariadb-client)
  fi
  if [ "$WITH_POSTGRES" -eq 1 ]; then
    packages+=(postgresql postgresql-client)
  fi
  if [ "$WITH_REDIS" -eq 1 ]; then
    packages+=(redis-server)
  fi

  echo "==> Installing: ${packages[*]}"
  DEBIAN_FRONTEND=noninteractive apt-get install -y "${packages[@]}"

  echo "==> Installing PM2 (npm -g)"
  npm install -g pm2

  # Debian 12+ marks the system Python as "externally managed" (PEP
  # 668), which makes a bare `pip install` fail everywhere, including
  # wor's own `wor deploy` step for python services
  # (internal/cliapp/deploy.go runs `python3 -m pip install -r
  # requirements.txt` directly against the system interpreter -- it
  # does not create a venv per service). Writing this system-wide
  # pip.conf is the simplest fix that works no matter which unix user
  # ends up running `wor deploy` later, since a per-user `pip config
  # set` would only cover whichever user runs this installer (root),
  # not the actual wor operator.
  echo "==> Allowing system-wide pip installs (PEP 668) for wor deploy's python step"
  cat > /etc/pip.conf <<'EOF'
[global]
break-system-packages = true
EOF

  # "php*-fpm" is quoted here specifically so the *shell* never expands
  # it as a filesystem glob against the current directory -- it's
  # passed through to systemctl literally, which does its own glob
  # matching against actual unit names (e.g. php8.2-fpm.service).
  local svc unit
  for svc in "$webserver_service" "php*-fpm"; do
    for unit in $(systemctl list-unit-files --type=service --no-legend "${svc}.service" 2>/dev/null | awk '{print $1}'); do
      echo "==> Enabling + starting $unit"
      systemctl enable --now "$unit" || true
    done
  done

  if [ "$WITH_MYSQL" -eq 1 ]; then
    systemctl enable --now mariadb || true
  fi
  if [ "$WITH_POSTGRES" -eq 1 ]; then
    systemctl enable --now postgresql || true
  fi
  if [ "$WITH_REDIS" -eq 1 ]; then
    systemctl enable --now redis-server || true
  fi
}

# install_rhel is a placeholder, not a real implementation -- wor has
# never been run or tested on RHEL/CentOS/Fedora/Rocky/Alma. It's
# wired into OS detection/dispatch already so adding real support later
# is just filling in this one function, not restructuring the script.
# Known differences from install_debian() that whoever picks this up
# will need to handle (not yet verified, don't assume these are
# complete or correct):
#   - package manager: dnf (or yum on older EL) instead of apt-get
#   - apache's package/service/unit name is "httpd", not "apache2"
#   - PHP: RHEL/CentOS base repos often ship an old PHP; most real
#     deployments add the Remi repo and use its versioned
#     `phpXY-php-fpm` module streams instead of a plain "php-fpm"
#     package -- this changes both the package name AND
#     phpfpm.DetectVersions()'s assumptions in the Go code (it expects
#     Debian's /etc/php/<version>/fpm layout, which RHEL does not use)
#   - node/npm: EL's base repos are usually too old; typically needs
#     NodeSource's repo added first, same as on Debian but more so
#   - SELinux is enabled by default on RHEL-family and will block
#     things Debian never has to think about (php-fpm socket
#     permissions, nginx proxying, etc.) -- likely needs explicit
#     `semanage`/`setsebool` calls this script doesn't do at all yet
install_rhel() {
  echo "ERROR: RHEL/CentOS/Fedora-family support is not implemented yet." >&2
  echo "wor has not been built or tested against this distro family. Install" >&2
  echo "the runtimes manually (git, go, node+npm+pm2, python3+pip, php-fpm," >&2
  echo "nginx/httpd) via dnf/yum, then place the matching bin/wor-linux-<arch>" >&2
  echo "yourself. See the comment above install_rhel() in this script for the" >&2
  echo "known differences from the Debian install path." >&2
  exit 1
}

case "$OS_FAMILY" in
  debian) install_debian ;;
  rhel) install_rhel ;;
esac

# ---- install the matching wor binary (distro-agnostic from here) -------

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo "amd64" ;;
    aarch64|arm64) echo "arm64" ;;
    *) return 1 ;;
  esac
}

if ! ARCH="$(detect_arch)"; then
  echo "ERROR: unsupported CPU architecture: $(uname -m)" >&2
  exit 1
fi
BIN_SRC="$SCRIPT_DIR/bin/wor-linux-${ARCH}"

if [ ! -f "$BIN_SRC" ]; then
  echo "ERROR: no matching binary for linux/${ARCH} at $BIN_SRC" >&2
  echo "Available binaries:" >&2
  ls -1 "$SCRIPT_DIR/bin" >&2 || true
  exit 1
fi

echo "==> Installing $BIN_SRC -> ${INSTALL_DIR}/wor"
mkdir -p "$INSTALL_DIR"
install -m 0755 "$BIN_SRC" "${INSTALL_DIR}/wor"

if ! command -v wor >/dev/null 2>&1; then
  echo "WARNING: ${INSTALL_DIR} does not appear to be on PATH for interactive shells." >&2
  echo "Add it (e.g. in /etc/profile.d/) or always invoke ${INSTALL_DIR}/wor directly." >&2
fi

echo
echo "[OK] Install complete. wor is ready."
"${INSTALL_DIR}/wor" version || true
echo
# Deliberately just a suggestion, not auto-run: wor refuses to run at
# all when invoked as root via sudo (osutil.IsSudoElevated() in
# internal/cliapp/app.go: "do not run wor via sudo"), and this script
# itself has to run as root for apt/systemctl. Trying to guess the
# right non-root user to drop to (via $SUDO_USER) and run these for
# them adds a fair amount of edge-case handling for little benefit --
# simpler and more predictable to just tell the user to run them
# themselves, as whichever normal user will actually operate wor.
echo "Next, as the (non-root) user who will operate wor:"
echo "  wor doctor   # confirm every runtime above was detected correctly"
echo "  wor setup    # configure WOR_HOME, host provider, SSL, etc."
