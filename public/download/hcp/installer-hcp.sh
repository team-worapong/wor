#!/usr/bin/env bash
set -euo pipefail

download_base="${WOR_HCP_DOWNLOAD_BASE:-https://wor.worapong.com/download/hcp}"
version=""
target=""
dry_run=0

for arg in "$@"; do
  case "$arg" in
    --target=*) target="${arg#*=}" ;;
    --dry-run) dry_run=1 ;;
    -h|--help) echo "usage: installer-hcp.sh [version] [--target=domain/service] [--dry-run]"; exit 0 ;;
    -*) echo "unknown option: $arg" >&2; exit 1 ;;
    *) if [ -n "$version" ]; then echo "only one version may be specified" >&2; exit 1; fi; version="$arg" ;;
  esac
done

command -v wor >/dev/null 2>&1 || { echo "wor is required" >&2; exit 1; }
command -v curl >/dev/null 2>&1 || { echo "curl is required" >&2; exit 1; }
command -v tar >/dev/null 2>&1 || { echo "tar is required" >&2; exit 1; }

wor_home="$(wor path .)"
wor_home="$(cd "$wor_home" && pwd -P)"

if [ -n "$target" ]; then
  case "$target" in */*) ;; *) echo "target must be domain/service" >&2; exit 1;; esac
  service_root="$(wor path "$target")"
  service_root="$(cd "$service_root" && pwd -P)"
else
  service_root="$(pwd -P)"
  relative="${service_root#"$wor_home"/domains/}"
  if [ "$relative" = "$service_root" ]; then echo "run this installer from a WOR service directory, or pass --target=domain/service" >&2; exit 1; fi
  domain="${relative%%/*}"
  service="${relative#*/}"
  if [ -z "$domain" ] || [ -z "$service" ] || [ "$service" != "${service%%/*}" ]; then echo "current directory must be exactly WOR_HOME/domains/<domain>/<service>" >&2; exit 1; fi
  target="$domain/$service"
fi

domain="${target%%/*}"
service="${target#*/}"
case "$domain/$service" in *[!A-Za-z0-9._/-]*|*/*/*) echo "invalid target: $target" >&2; exit 1;; esac
config="$wor_home/domains/$domain/services.config.json"
[ -f "$config" ] || { echo "services config not found: $config" >&2; exit 1; }

read_service_field() {
  local field="$1"
  awk -v service="$service" -v field="$field" '
    $0 ~ "\\\"name\\\"[[:space:]]*:[[:space:]]*\\\"" service "\\\"" { found=1 }
    found && $0 ~ "\\\"" field "\\\"[[:space:]]*:" {
      line=$0; sub("^[^:]*:[[:space:]]*", "", line); gsub(/[\",]/, "", line); gsub(/^[[:space:]]+|[[:space:]]+$/, "", line); print line; exit
    }
    found && $0 ~ "^[[:space:]]*\\\"name\\\"[[:space:]]*:" && $0 !~ "\\\"" service "\\\"" { exit }
  ' "$config"
}

service_type="$(read_service_field type)"
entry="$(read_service_field entryPoint)"
[ "$service_type" = "go" ] || { echo "$target is type '$service_type', expected 'go'" >&2; exit 1; }
[ -n "$entry" ] || { echo "entryPoint not found for $target" >&2; exit 1; }
case "$entry" in /*|*..*) echo "unsafe entryPoint: $entry" >&2; exit 1;; esac

wor info "$target" >/dev/null || { echo "wor could not verify target $target" >&2; exit 1; }

if [ -z "$version" ]; then version="$(curl -fsSL "$download_base/latest.txt")"; fi
version="${version#v}"
os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$os" in linux|darwin) ;; *) echo "unsupported OS: $os" >&2; exit 1;; esac
machine="$(uname -m)"
case "$machine" in x86_64|amd64) arch=amd64;; arm64|aarch64) arch=arm64;; *) echo "unsupported architecture: $machine" >&2; exit 1;; esac

archive="wor-hcp-v${version}-${os}-${arch}.tar.gz"
release_url="$download_base/releases"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

echo "Installing WOR HCP v$version into $target ($entry)"
if [ "$dry_run" -eq 1 ]; then
  echo "Dry run: would download $release_url/$archive"
  echo "Dry run: would install into $service_root and restart $target"
  exit 0
fi

curl -fsSL "$release_url/$archive" -o "$tmp_dir/$archive"
curl -fsSL "$download_base/checksums.txt" -o "$tmp_dir/checksums.txt"
expected="$(awk -v file="$archive" '$2 == file || $2 == "./" file { print $1; exit }' "$tmp_dir/checksums.txt")"
[ -n "$expected" ] || { echo "checksum not found for $archive" >&2; exit 1; }
if command -v sha256sum >/dev/null 2>&1; then actual="$(sha256sum "$tmp_dir/$archive" | awk '{print $1}')"; else actual="$(shasum -a 256 "$tmp_dir/$archive" | awk '{print $1}')"; fi
[ "$expected" = "$actual" ] || { echo "checksum verification failed" >&2; exit 1; }
tar -xzf "$tmp_dir/$archive" -C "$tmp_dir"

stamp="$(date -u +%Y%m%dT%H%M%SZ)"
backup="$service_root/.wor-hcp-backups/$stamp"
mkdir -p "$backup" "$(dirname "$service_root/$entry")"
[ ! -e "$service_root/$entry" ] || cp "$service_root/$entry" "$backup/app"
[ ! -d "$service_root/public" ] || cp -R "$service_root/public" "$backup/public"

restart_service() {
  if [ -r /dev/tty ]; then
    wor service restart "$target" </dev/tty
  else
    wor service restart "$target"
  fi
}

rollback() {
  echo "Install failed; restoring previous files" >&2
  [ ! -f "$backup/app" ] || cp "$backup/app" "$service_root/$entry"
  if [ -d "$backup/public" ]; then rm -rf "$service_root/public"; cp -R "$backup/public" "$service_root/public"; fi
  restart_service >/dev/null 2>&1 || true
}
trap 'status=$?; if [ $status -ne 0 ]; then rollback; fi; rm -rf "$tmp_dir"; exit $status' EXIT

cp "$tmp_dir/wor-hcp/app" "$service_root/$entry.new"
chmod 0755 "$service_root/$entry.new"
mv "$service_root/$entry.new" "$service_root/$entry"
rm -rf "$service_root/public.new"
cp -R "$tmp_dir/wor-hcp/public" "$service_root/public.new"
rm -rf "$service_root/public"
mv "$service_root/public.new" "$service_root/public"
restart_service
echo "WOR HCP v$version installed successfully for $target"
