#!/bin/sh

set -eu

repository_root="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"
test_root="$(mktemp -d "${TMPDIR:-/tmp}/denverr-installer-test.XXXXXX")"
trap 'rm -rf "$test_root"' EXIT HUP INT TERM

mock_bin="${test_root}/bin"
payload_dir="${test_root}/payload"
release_root="${test_root}/releases"
release_dir="${release_root}/download/v9.8.7"
install_dir="${test_root}/installed"
mkdir -p "$mock_bin" "$payload_dir" "$release_dir"

cat > "${mock_bin}/uname" <<'EOF'
#!/bin/sh
case "$1" in
  -s) printf 'Darwin\n' ;;
  -m) printf 'arm64\n' ;;
  *) exit 1 ;;
esac
EOF
chmod +x "${mock_bin}/uname"

cat > "${payload_dir}/denverr" <<'EOF'
#!/bin/sh
printf 'fixture-denverr\n'
EOF
chmod +x "${payload_dir}/denverr"

archive_name="denverr_9.8.7_darwin_arm64.tar.gz"
archive_path="${release_dir}/${archive_name}"
tar -czf "$archive_path" -C "$payload_dir" denverr

if command -v sha256sum >/dev/null 2>&1; then
  archive_checksum="$(sha256sum "$archive_path" | awk '{ print $1 }')"
else
  archive_checksum="$(shasum -a 256 "$archive_path" | awk '{ print $1 }')"
fi
printf '%s  %s\n' "$archive_checksum" "$archive_name" > "${release_dir}/checksums.txt"

PATH="${mock_bin}:${PATH}" \
  DENVERR_RELEASE_ROOT="file://${release_root}" \
  DENVERR_VERSION="9.8.7" \
  DENVERR_INSTALL_DIR="$install_dir" \
  sh "${repository_root}/install.sh" > "${test_root}/install.out"

[ -x "${install_dir}/denverr" ] || { printf 'installer did not create an executable\n' >&2; exit 1; }
[ "$("${install_dir}/denverr")" = "fixture-denverr" ] || { printf 'installed executable is incorrect\n' >&2; exit 1; }

printf 'corrupt' >> "$archive_path"
if PATH="${mock_bin}:${PATH}" \
  DENVERR_RELEASE_ROOT="file://${release_root}" \
  DENVERR_VERSION="v9.8.7" \
  DENVERR_INSTALL_DIR="$install_dir" \
  sh "${repository_root}/install.sh" > /dev/null 2> "${test_root}/failure.err"; then
  printf 'installer accepted an archive with the wrong checksum\n' >&2
  exit 1
fi

[ "$("${install_dir}/denverr")" = "fixture-denverr" ] || { printf 'failed install replaced the existing executable\n' >&2; exit 1; }
grep -q 'checksum verification failed' "${test_root}/failure.err"

printf 'installer tests passed\n'
