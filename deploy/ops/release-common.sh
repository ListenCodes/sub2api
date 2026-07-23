#!/usr/bin/env bash
set -Eeuo pipefail

RELEASE_COMMON_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SUB2API_DATA_DIR="${SUB2API_DATA_DIR:-/var/lib/docker/volumes/deploy_sub2api_data/_data}"
RELEASE_LEDGER_ROOT="${SUB2API_RELEASE_LEDGER_ROOT:-$SUB2API_DATA_DIR/release-ledger}"
RELEASE_JOBS_DIR="${SUB2API_RELEASE_OPERATIONS_DIR:-$RELEASE_LEDGER_ROOT/operations}"
PREPARED_ROOT="${SUB2API_PREPARED_ROOT:-$SUB2API_DATA_DIR/release-prepared}"
PRODUCTION_RELEASE_STATE_FILE="${SUB2API_RELEASE_STATE_FILE:-$SUB2API_DATA_DIR/release-state.json}"
REPO="${SUB2API_REPO:-/root/sub2api}"
BRANCH="${SUB2API_BRANCH:-custom-release}"
ENV_FILE="${SUB2API_ENV_FILE:-$REPO/deploy/.env}"
COMPOSE_BASE="${SUB2API_COMPOSE_BASE:-$REPO/deploy/docker-compose.yml}"
COMPOSE_CUSTOM="${SUB2API_COMPOSE_CUSTOM:-$REPO/deploy/docker-compose.custom.yml}"
MAIN_REPOSITORY="${SUB2API_MAIN_REPOSITORY:-ghcr.io/listencodes/sub2api-custom}"
EXTENSIONS_REPOSITORY="${SUB2API_EXTENSIONS_REPOSITORY:-ghcr.io/listencodes/sub2api-extensions}"

release_manifest_path() {
  local job_id="$1"
  printf '%s/%s/manifest.json\n' "$PREPARED_ROOT" "$job_id"
}

release_manifest_sha_path() {
  local job_id="$1"
  printf '%s/%s/manifest.sha256\n' "$PREPARED_ROOT" "$job_id"
}

release_manifest_valid() {
  local job_id="$1" manifest expected actual expires
  manifest="$(release_manifest_path "$job_id")"
  expected="$(cat "$(release_manifest_sha_path "$job_id")" 2>/dev/null | awk '{print $1}' || true)"
  actual="$(sha256sum "$manifest" 2>/dev/null | awk '{print $1}' || true)"
  [[ -n "$expected" && "$expected" == "$actual" ]] || return 1
  jq -e 'type == "object"
    and (.production_commit|test("^[0-9a-f]{40}$"))
    and (.target_commit|test("^[0-9a-f]{40}$"))
    and (.stable_tag|test("^v[0-9]+\\.[0-9]+\\.[0-9]+$"))
    and (.stable_commit|test("^[0-9a-f]{40}$"))
    and (.main_digest|test("^sha256:[0-9a-f]{64}$"))
    and (.extensions_digest|test("^sha256:[0-9a-f]{64}$"))
    and (.compose_hash|test("^[0-9a-f]{64}$"))
    and (.env_hash|test("^[0-9a-f]{64}$"))
    and (.backup_dir|type=="string" and length>0)' "$manifest" >/dev/null
  expires="$(jq -r '.expires_at // empty' "$manifest")"
  [[ -n "$expires" ]] || return 1
  backup_dir="$(jq -r '.backup_dir' "$manifest")"
  case "$backup_dir" in
    "$SUB2API_DATA_DIR/release-backups/"*) ;;
    *) return 1 ;;
  esac
  [[ -r "$backup_dir/SHA256SUMS" ]] || return 1
  [[ "$(date -u +%s)" -lt "$(date -u -d "$expires" +%s)" ]] || return 2
}

release_job_fail() {
  local job_id="$1" code="$2" message="$3"
  source "${SUB2API_RELEASE_STATE_HELPER:-$RELEASE_COMMON_DIR/release-state.sh}"
  release_job_update "$job_id" failed "$message" "$(jq -n --arg code "$code" '{error_code:$code,production_changed:false}')" || true
}

release_json_hash() {
  jq -cS . "$1" | sha256sum | awk '{print $1}'
}
