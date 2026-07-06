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

# prompt_line PROMPT DEFAULT
# Prints a reply on stdout, echoing PROMPT and reading a line of input
# from /dev/tty rather than plain stdin (fd 0). This matters because
# install.sh is designed to also run via the documented
# `curl -fsSL .../installer.sh | bash` flow: installer.sh downloads and
# extracts the release, then does `exec ./install.sh`. By that point
# bash has already consumed the entire piped stream as installer.sh's
# own script text, so fd 0 is at EOF -- any plain `read` here would
# get empty input immediately and (under `set -e`) abort the script.
# /dev/tty talks to the real controlling terminal directly, bypassing
# that. If no terminal is attached at all (e.g. a fully headless/CI
# invocation with stdin/stdout both redirected away from a tty),
# /dev/tty won't be readable either -- in that case this falls back to
# DEFAULT rather than hanging or letting `set -e` kill the script.
prompt_line() {
  local prompt="$1" default_reply="$2" reply
  if [ -r /dev/tty ]; then
    read -r -p "$prompt" reply < /dev/tty || reply="$default_reply"
  else
    echo "${prompt}(no terminal attached, using default: '${default_reply}')" >&2
    reply="$default_reply"
  fi
  printf '%s' "$reply"
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

# ---- detect + optionally remove a pre-existing wor installation --------

# Not apt/dnf-specific, so this runs once here rather than inside
# install_debian()/install_rhel() -- whichever family branch eventually
# runs after this shares the same detection/removal step.
#
# Two things are checked independently:
#   - the operator's ~/.wor/config (the shell-script wor-cli this may be
#     replacing used the same path convention)
#   - /opt/wor, the default production WOR_HOME on Linux
# $SUDO_USER is used only to know *whose* home to look in -- this script
# itself always runs as root, but the config file that matters belongs
# to whoever actually runs `wor` day to day.
OPERATOR_HOME=""
if [ -n "${SUDO_USER:-}" ] && [ "$SUDO_USER" != "root" ]; then
  OPERATOR_HOME="$(getent passwd "$SUDO_USER" 2>/dev/null | cut -d: -f6 || true)"
fi

OLD_CONFIG=""
if [ -n "$OPERATOR_HOME" ] && [ -f "$OPERATOR_HOME/.wor/config" ]; then
  OLD_CONFIG="$OPERATOR_HOME/.wor/config"
fi

OLD_WORHOME=""
if [ -d /opt/wor ]; then
  OLD_WORHOME="/opt/wor"
fi

# Distinguish "this machine still has the old shell-script wor-cli"
# (the RESET/REMOVE questions below exist for exactly that one-time
# migration) from "this machine already has *this* Go-based wor
# installed, and install.sh is just being re-run to update it to a
# newer release". Without this check, an existing config + WOR_HOME
# look identical in both cases, so every routine update re-run would
# re-ask the same migration questions forever. A compiled Go binary
# starts with the 4-byte ELF magic number; the old shell-script
# wor-cli is a plain text file starting with a #! shebang line --
# reading the first 4 bytes is enough to tell them apart without
# depending on the external `file` command, which isn't guaranteed
# present on a minimal image.
EXISTING_WOR_BIN=""
if [ -x "${INSTALL_DIR}/wor" ]; then
  EXISTING_WOR_BIN="${INSTALL_DIR}/wor"
elif command -v wor >/dev/null 2>&1; then
  EXISTING_WOR_BIN="$(command -v wor)"
fi

IS_GO_BUILD=0
if [ -n "$EXISTING_WOR_BIN" ]; then
  case "$(head -c4 "$EXISTING_WOR_BIN" 2>/dev/null)" in
    $'\x7fELF') IS_GO_BUILD=1 ;;
  esac
fi

if [ -n "$OLD_CONFIG" ] || [ -n "$OLD_WORHOME" ]; then
  if [ "$IS_GO_BUILD" -eq 1 ]; then
    echo "==> Existing wor (Go build) found at $EXISTING_WOR_BIN -- treating this as an"
    echo "    update, not a migration from the old shell-script wor-cli. Skipping the"
    echo "    config/WOR_HOME reset questions."
    echo
  else
    echo "==> Found an existing wor installation:"
    [ -n "$OLD_CONFIG" ] && echo "    Config file : $OLD_CONFIG"
    [ -n "$OLD_WORHOME" ] && echo "    WOR_HOME    : $OLD_WORHOME"
    echo
    echo "Remove the old config so wor starts fresh? (domains/backups/logs/ssl"
    echo "under WOR_HOME are NOT touched by this step -- that's asked separately"
    echo "below.)"
    confirm_reset="$(prompt_line "Type RESET to confirm, or press Enter to keep it: " "")"
    if [ "$confirm_reset" = "RESET" ]; then
      if [ -n "$OLD_CONFIG" ]; then
        rm -f "$OLD_CONFIG"
        echo "[OK] Removed $OLD_CONFIG"
      fi
      if [ -n "$OLD_WORHOME" ]; then
        echo
        echo "Also completely remove WOR_HOME ($OLD_WORHOME) -- including any"
        echo "deployed sites, source/database backups, and SSL certs under it?"
        echo "This cannot be undone."
        confirm_remove="$(prompt_line "Type \"REMOVE $OLD_WORHOME\" to confirm, or press Enter to keep it: " "")"
        if [ "$confirm_remove" = "REMOVE $OLD_WORHOME" ]; then
          rm -rf "$OLD_WORHOME"
          echo "[OK] Removed $OLD_WORHOME"
        else
          echo "Keeping $OLD_WORHOME as-is."
        fi
      fi
    else
      echo "Keeping existing config as-is."
    fi
    echo
  fi
fi

# ---- family-specific install ------------------------------------------

# enable_and_start_unit UNIT
# systemctl enable --now is already safe to re-run on its own (enabling
# an already-enabled unit, or starting an already-running one, is a
# no-op either way) -- but it still prints systemd's own
# "Synchronizing state of <unit> with SysV service script..." /
# "Executing: ... enable ..." boilerplate every single time, which
# looks like real work happened on every re-run of this installer even
# when nothing changed. Checking is-enabled/is-active first just
# avoids that noise; it does not change what actually gets
# enabled/started.
enable_and_start_unit() {
  local unit="$1"
  if systemctl is-enabled --quiet "$unit" 2>/dev/null && systemctl is-active --quiet "$unit" 2>/dev/null; then
    echo "==> $unit already enabled and running -- skipping"
    return 0
  fi
  echo "==> Enabling + starting $unit"
  systemctl enable --now "$unit" || true
}

install_debian() {
  if ! command -v apt-get >/dev/null 2>&1; then
    echo "ERROR: apt-get not found even though /etc/os-release looks Debian-family." >&2
    exit 1
  fi

  # Deliberately just the OS-default package for each runtime -- no
  # version pinning, no PPAs/NodeSource/Remi-style third-party repos.
  # Whatever version Debian's own repos currently consider current is
  # what gets installed; anyone who needs a different/newer version is
  # expected to set that up manually afterward, not have this script
  # decide for them.
  #
  # Notably this means no separate "npm" package: on Debian, "nodejs"
  # already bundles its own npm, and the standalone "npm" binary
  # package is a legacy, separately-built npm assembled from dozens of
  # individually packaged node-* libraries -- it actively Conflicts
  # with the bundled one and (as of trixie) has its own broken
  # dependency chain besides. Installing "nodejs" alone is enough for
  # both `node` and `npm` to work.
  local packages=(
    git
    golang-go
    nodejs
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

  # Report installed-vs-missing before touching apt at all (dpkg-query
  # reads the local package database only, no network/apt-get-update
  # needed for this part) -- this is also what makes it possible to
  # only ever apt-get install the *missing* subset below, never the
  # whole list. That distinction matters: apt-get install on an
  # already-installed package is normally a no-op, but if a newer
  # version exists in the repo it will happily upgrade it -- which
  # would be silently changing something the user (or the old
  # shell-script wor-cli, or a manual install) already set up. Only
  # ever touching packages that aren't installed at all is a stronger
  # guarantee than "never remove": it also means never modifying
  # anything already present.
  echo "==> Checking installed runtimes"
  local missing=() pkg version
  for pkg in "${packages[@]}"; do
    version="$(dpkg-query -W -f='${Version}' "$pkg" 2>/dev/null || true)"
    if [ -n "$version" ]; then
      printf "    %-24s: installed (%s)\n" "$pkg" "$version"
    else
      printf "    %-24s: not installed\n" "$pkg"
      missing+=("$pkg")
    fi
  done
  echo

  if [ "${#missing[@]}" -eq 0 ]; then
    echo "All recommended packages are already installed -- nothing to do."
  else
    echo "wor would install: ${missing[*]}"
    echo "wor will never remove, downgrade, or upgrade anything already on this system --"
    echo "only the packages listed above (currently missing) would be touched."
    echo
    # Default reply ("") falls into the ""|y|Y|yes... branch below, i.e.
    # a headless run with no tty attached at all installs the missing
    # packages automatically -- matching this script's old unconditional
    # behavior before this confirmation existed, so unattended
    # `curl | bash` automation keeps working unchanged.
    confirm_install="$(prompt_line "Install the packages above now? [Y/n] " "")"
    case "$confirm_install" in
      ""|y|Y|yes|Yes|YES)
        echo "==> apt-get update"
        apt-get update -qq
        echo "==> Installing: ${missing[*]}"
        DEBIAN_FRONTEND=noninteractive apt-get install -y "${missing[@]}"
        ;;
      *)
        echo "Skipping package installation. 'wor doctor' will report the packages"
        echo "above as missing until you install them yourself."
        ;;
    esac
  fi

  if command -v pm2 >/dev/null 2>&1; then
    echo "==> PM2 already installed ($(pm2 --version 2>/dev/null || echo "unknown version")) -- skipping"
  elif command -v npm >/dev/null 2>&1; then
    echo "==> Installing PM2 (npm -g)"
    npm install -g pm2
  else
    echo "WARNING: npm not found -- skipping PM2 install (needed for node services)." >&2
    echo "Install Node.js, then run: npm install -g pm2" >&2
  fi

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
  # PIP_CONF_MARKER identifies a pip.conf this installer itself wrote,
  # so a re-run can tell "safe to regenerate" apart from "a real
  # /etc/pip.conf the operator set up by hand (custom index-url,
  # proxy, etc.)". Without this distinction, blindly overwriting the
  # file on every run (the old behavior) would silently destroy any
  # such manual configuration. pip.conf is plain INI (Python
  # configparser), which errors on a duplicate `[global]` section, so
  # this deliberately does not try to merge into an existing
  # non-wor-managed file -- that would need a real INI parser, more
  # than this one setting is worth. If break-system-packages is
  # already set (by us or by someone else) there's nothing to do
  # either way.
  local PIP_CONF="/etc/pip.conf"
  local PIP_CONF_MARKER="# Managed by wor installer -- allows system-wide pip installs (PEP 668) for wor deploy's python step"
  if [ -f "$PIP_CONF" ] && grep -qF "break-system-packages" "$PIP_CONF" 2>/dev/null; then
    echo "==> System-wide pip installs already allowed ($PIP_CONF) -- skipping"
  elif [ -f "$PIP_CONF" ] && ! grep -qF "$PIP_CONF_MARKER" "$PIP_CONF" 2>/dev/null; then
    echo "==> $PIP_CONF already exists and wasn't created by this installer -- leaving it as-is."
    echo "    wor deploy's python step needs this to work around PEP 668; add it yourself if needed:"
    echo "      [global]"
    echo "      break-system-packages = true"
  else
    echo "==> Allowing system-wide pip installs (PEP 668) for wor deploy's python step"
    cat > "$PIP_CONF" <<EOF
$PIP_CONF_MARKER
[global]
break-system-packages = true
EOF
  fi

  # "php*-fpm" is quoted here specifically so the *shell* never expands
  # it as a filesystem glob against the current directory -- it's
  # passed through to systemctl literally, which does its own glob
  # matching against actual unit names (e.g. php8.2-fpm.service).
  local svc unit
  for svc in "$webserver_service" "php*-fpm"; do
    for unit in $(systemctl list-unit-files --type=service --no-legend "${svc}.service" 2>/dev/null | awk '{print $1}'); do
      enable_and_start_unit "$unit"
    done
  done

  if [ "$WITH_MYSQL" -eq 1 ]; then
    enable_and_start_unit mariadb
  fi
  if [ "$WITH_POSTGRES" -eq 1 ]; then
    enable_and_start_unit postgresql
  fi
  if [ "$WITH_REDIS" -eq 1 ]; then
    enable_and_start_unit redis-server
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

# ---- offer wor shell-init in the operator's shell rc file --------------

# `wor goto <domain>[/<service>]` only works once the operator's rc
# file evals `wor shell-init` (a process can't cd its parent shell, so
# the cd has to happen in a shell function -- see internal/cliapp/
# path.go). This installer runs as root, but the rc file that matters
# belongs to $SUDO_USER -- the same operator-vs-root distinction the
# old-install detection above already makes. Which rc file is decided
# by that user's *login shell* (getent passwd field 7), not by distro:
# this script only ever runs on Linux, where bash is the usual default
# but zsh operators are common enough to matter.
#
# The grep guard makes re-runs (release updates) idempotent: any
# existing mention of `wor shell-init` -- whether added here or by the
# operator themselves -- means there's nothing to do.
SHELL_RC=""
OPERATOR_SHELL=""
SHELLINIT_ADDED=0
if [ -n "$OPERATOR_HOME" ]; then
  OPERATOR_SHELL="$(getent passwd "$SUDO_USER" 2>/dev/null | cut -d: -f7 || true)"
  case "$OPERATOR_SHELL" in
    */zsh) SHELL_RC="$OPERATOR_HOME/.zshrc" ;;
    *)     SHELL_RC="$OPERATOR_HOME/.bashrc" ;;
  esac
fi

if [ -z "$SHELL_RC" ]; then
  # Direct root login (no SUDO_USER): there's no way to know whose rc
  # file to touch, and wor itself refuses to run as root anyway.
  echo "==> Skipping shell integration (couldn't determine the operator user)."
  echo "    As the user who will operate wor, add this line to ~/.bashrc or ~/.zshrc:"
  echo "      eval \"\$(wor shell-init)\""
elif grep -qF "wor shell-init" "$SHELL_RC" 2>/dev/null; then
  echo "==> wor shell integration already present in $SHELL_RC -- skipping"
else
  echo
  echo "wor can add one line to $SHELL_RC:"
  echo "  eval \"\$(wor shell-init)\""
  echo "which enables:  wor goto <domain>[/<service>]  -> cd into that folder."
  # Default reply ("") = yes, matching the package-install prompt above:
  # headless/no-tty runs add the line automatically. That's safe here
  # because the addition is one clearly-commented line, and the grep
  # guard above keeps repeat runs from stacking copies.
  confirm_shellinit="$(prompt_line "Add it now? [Y/n] " "")"
  case "$confirm_shellinit" in
    ""|y|Y|yes|Yes|YES)
      SHELLINIT_ADDED=1
      SHELL_RC_EXISTED=1
      [ -f "$SHELL_RC" ] || SHELL_RC_EXISTED=0
      {
        echo ""
        echo "# wor shell integration (wor goto) -- added by wor install.sh"
        echo "eval \"\$(wor shell-init)\""
      } >> "$SHELL_RC"
      # >> as root on a nonexistent rc file would leave it root-owned
      # (and thus unwritable by the operator forever after); hand a
      # newly created one to its actual owner. An rc file that already
      # existed keeps whatever owner/perms it had.
      if [ "$SHELL_RC_EXISTED" -eq 0 ]; then
        chown "$SUDO_USER" "$SHELL_RC" || true
      fi
      echo "[OK] Added. Takes effect in new shells (or run: source $SHELL_RC)"
      ;;
    *)
      echo "Skipped. To add it later:"
      echo "  echo 'eval \"\$(wor shell-init)\"' >> $SHELL_RC"
      ;;
  esac
fi

echo
echo "[OK] Install complete. wor is ready."
echo
# Deliberately just a suggestion, not auto-run: wor refuses to run at
# all when invoked as root via sudo -- osutil.IsSudoElevated() in
# internal/cliapp/app.go blocks *every* subcommand under that
# condition, including harmless read-only ones like `wor version`, and
# this script itself has to run as root for apt/systemctl. An earlier
# version of this script ran `wor version` directly right here as a
# sanity check -- that's exactly the case IsSudoElevated blocks, so it
# always printed "do not run wor via sudo" instead of the version info.
# Trying to guess the right non-root user to drop to (via $SUDO_USER)
# and run these for them adds a fair amount of edge-case handling for
# little benefit -- simpler and more predictable to just tell the user
# to run them themselves, as whichever normal user will actually
# operate wor.
echo "Next, as the (non-root) user who will operate wor, run these in order:"
if [ "$SHELLINIT_ADDED" -eq 1 ]; then
  # The eval line was appended to an rc file the operator's *current*
  # shell has already read -- it only takes effect in shells started
  # from now on, so surface the one-time source here in the final
  # checklist (the [OK] line above scrolls away behind the apt output).
  echo "  0. source $SHELL_RC   # or open a new terminal, to enable \"wor goto\" now"
fi
echo "  1. wor version   # confirm the binary installed correctly"
echo "  2. wor doctor    # confirm every runtime above was detected correctly"
echo "  3. wor setup     # configure WOR_HOME, host provider, SSL, etc."
