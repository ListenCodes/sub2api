#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

if ! chmod 0700 "$TMP_DIR" 2>/dev/null || [[ "$(stat -c '%a' "$TMP_DIR")" != 700 ]]; then
  printf 'release-manifest-boundary=SKIP (filesystem does not enforce POSIX modes)\n'
  exit 0
fi

fail() {
  printf 'release manifest boundary test failed: %s\n' "$1" >&2
  exit 1
}

export SUB2API_DATA_DIR="$TMP_DIR/data"
export SUB2API_PREPARED_ROOT="$TMP_DIR/prepared"
export SUB2API_RELEASE_BACKUP_ROOT="$TMP_DIR/backups"
source "$ROOT_DIR/deploy/ops/release-state.sh"
source "$ROOT_DIR/deploy/ops/release-common.sh"

job_id=update-manifest-boundary
release_ensure_backup_root || fail 'secure backup root was rejected'
[[ "$(stat -c '%a' "$SUB2API_RELEASE_BACKUP_ROOT")" == 700 ]] \
  || fail 'backup root is not mode 0700'
manifest_dir="$(release_prepare_manifest_dir "$job_id")" \
  || fail 'secure manifest directory was rejected'
[[ "$(stat -c '%a' "$SUB2API_PREPARED_ROOT")" == 700 ]] \
  || fail 'prepared root is not mode 0700'
[[ "$(stat -c '%a' "$manifest_dir")" == 700 ]] \
  || fail 'manifest directory is not mode 0700'

source_file="$TMP_DIR/source.json"
printf '{"fixture":true}\n' > "$source_file"
chmod 0600 "$source_file"
release_install_manifest_files "$manifest_dir" "$source_file" \
  || fail 'secure manifest files were rejected'
[[ "$(stat -c '%a' "$manifest_dir/manifest.json")" == 600 ]] \
  || fail 'manifest is not mode 0600'
[[ "$(stat -c '%a' "$manifest_dir/manifest.sha256")" == 600 ]] \
  || fail 'manifest checksum is not mode 0600'
(
  cd "$manifest_dir"
  sha256sum -c manifest.sha256 >/dev/null
) \
  || fail 'manifest checksum does not match the installed manifest'

chmod 0755 "$SUB2API_PREPARED_ROOT"
if release_ensure_prepared_root; then
  fail 'prepared root with broad permissions was accepted'
fi
chmod 0700 "$SUB2API_PREPARED_ROOT"

mkdir -p "$TMP_DIR/insecure-parent"
chmod 0777 "$TMP_DIR/insecure-parent"
PREPARED_ROOT="$TMP_DIR/insecure-parent/prepared"
if release_ensure_prepared_root; then
  fail 'prepared root beneath a writable parent was accepted'
fi
PREPARED_ROOT="$SUB2API_PREPARED_ROOT"

if ln -s "$TMP_DIR" "$TMP_DIR/link" 2>/dev/null; then
  PREPARED_ROOT="$TMP_DIR/link/prepared"
  if release_ensure_prepared_root; then
    fail 'prepared root beneath a symlink was accepted'
  fi
  PREPARED_ROOT="$SUB2API_PREPARED_ROOT"

  rm -f "$manifest_dir/manifest.json"
  ln -s "$TMP_DIR/symlink-target" "$manifest_dir/manifest.json"
  if release_install_manifest_files "$manifest_dir" "$source_file"; then
    fail 'symlink manifest target was accepted'
  fi
  [[ ! -e "$TMP_DIR/symlink-target" ]] \
    || fail 'symlink manifest target was overwritten'
fi

printf 'release-manifest-boundary=PASS\n'
