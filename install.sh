#!/bin/sh

set -eu

repository_url="https://github.com/Usefused/developa"
release_root="${DENVERR_RELEASE_ROOT:-${repository_url}/releases}"
requested_version="${DENVERR_VERSION:-}"

fail() {
  printf 'denverr installer: %s\n' "$1" >&2
  exit 1
}

usage() {
  cat <<'EOF'
Install the Denverr native binary.

Usage: install.sh [--version VERSION] [--install-dir DIRECTORY]

Environment:
  DENVERR_VERSION       Release to install, with or without the leading v.
  DENVERR_INSTALL_DIR   Destination directory (default: ~/.local/bin).
EOF
}

install_dir="${DENVERR_INSTALL_DIR:-}"
while [ "$#" -gt 0 ]; do
  case "$1" in
    --version)
      [ "$#" -ge 2 ] || fail "--version requires a value"
      requested_version="$2"
      shift 2
      ;;
    --install-dir)
      [ "$#" -ge 2 ] || fail "--install-dir requires a value"
      install_dir="$2"
      shift 2
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      fail "unknown argument: $1"
      ;;
  esac
done

if [ -z "$install_dir" ]; then
  [ -n "${HOME:-}" ] || fail "HOME is unset; pass --install-dir"
  install_dir="${HOME}/.local/bin"
fi

for command_name in curl tar awk mktemp mkdir install mv rm uname; do
  command -v "$command_name" >/dev/null 2>&1 || fail "required command not found: ${command_name}"
done

case "$(uname -s)" in
  Darwin) operating_system="darwin" ;;
  Linux) operating_system="linux" ;;
  *) fail "supported operating systems are macOS and Linux" ;;
esac

case "$(uname -m)" in
  x86_64|amd64) architecture="amd64" ;;
  arm64|aarch64) architecture="arm64" ;;
  *) fail "supported architectures are amd64 and arm64" ;;
esac

normalize_version() {
  normalized_version="${1#v}"
  case "$normalized_version" in
    ''|*[!0-9.]*|.*|*.|*..*) fail "invalid release version: $1" ;;
  esac

  old_ifs="$IFS"
  IFS=.
  set -- $normalized_version
  IFS="$old_ifs"
  [ "$#" -eq 3 ] || fail "release version must use major.minor.patch"
  printf '%s\n' "$normalized_version"
}

if [ -z "$requested_version" ]; then
  latest_url="$(curl --proto '=https' --proto-redir '=https' -fsSL -o /dev/null -w '%{url_effective}' "${release_root}/latest")"
  requested_version="${latest_url##*/}"
fi

version="$(normalize_version "$requested_version")"
tag="v${version}"
archive_name="denverr_${version}_${operating_system}_${architecture}.tar.gz"
download_root="${release_root}/download/${tag}"

temporary_dir="$(mktemp -d "${TMPDIR:-/tmp}/denverr-install.XXXXXX")"
target_temporary=""
cleanup() {
  rm -rf "$temporary_dir"
  if [ -n "$target_temporary" ]; then
    rm -f "$target_temporary"
  fi
}
trap cleanup EXIT HUP INT TERM

archive_path="${temporary_dir}/${archive_name}"
checksum_path="${temporary_dir}/checksums.txt"
curl --proto '=https,file' --proto-redir '=https,file' -fsSL --retry 3 --output "$checksum_path" "${download_root}/checksums.txt"
curl --proto '=https,file' --proto-redir '=https,file' -fsSL --retry 3 --output "$archive_path" "${download_root}/${archive_name}"

expected_checksum="$(awk -v archive="$archive_name" '$2 == archive { print $1; exit }' "$checksum_path")"
[ -n "$expected_checksum" ] || fail "release checksum is missing for ${archive_name}"

if command -v sha256sum >/dev/null 2>&1; then
  actual_checksum="$(sha256sum "$archive_path" | awk '{ print $1 }')"
elif command -v shasum >/dev/null 2>&1; then
  actual_checksum="$(shasum -a 256 "$archive_path" | awk '{ print $1 }')"
else
  fail "sha256sum or shasum is required to verify the release"
fi

[ "$actual_checksum" = "$expected_checksum" ] || fail "checksum verification failed for ${archive_name}"

# Extract only the executable so unexpected archive entries can never be installed.
tar -xzf "$archive_path" -C "$temporary_dir" denverr
mkdir -p "$install_dir"
target_temporary="${install_dir}/.denverr-install.$$"
install -m 0755 "${temporary_dir}/denverr" "$target_temporary"
mv -f "$target_temporary" "${install_dir}/denverr"
target_temporary=""

printf 'Installed Denverr %s to %s/denverr\n' "$tag" "$install_dir"
case ":${PATH}:" in
  *":${install_dir}:"*) ;;
  *) printf 'Add %s to PATH before running denverr.\n' "$install_dir" ;;
esac
