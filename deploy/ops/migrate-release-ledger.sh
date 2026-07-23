#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DATA_DIR="${SUB2API_DATA_DIR:-/var/lib/docker/volumes/deploy_sub2api_data/_data}"
REPO="${SUB2API_REPO:-/root/sub2api}"
ENV_FILE="${SUB2API_ENV_FILE:-$REPO/deploy/.env}"
COMPOSE_BASE="${SUB2API_COMPOSE_BASE:-$REPO/deploy/docker-compose.yml}"
COMPOSE_CUSTOM="${SUB2API_COMPOSE_CUSTOM:-$REPO/deploy/docker-compose.custom.yml}"
LEDGER_HELPER="${SUB2API_RELEASE_LEDGER_HELPER:-$SCRIPT_DIR/release-ledger.sh}"

source "$LEDGER_HELPER"

EXPECTED_COMMIT=''
OFFICIAL_VERSION=''
CUSTOM_VERSION=''
while [[ $# -gt 0 ]]; do
  case "$1" in
    --expected-production-commit) EXPECTED_COMMIT="${2:-}"; shift 2 ;;
    --official-version) OFFICIAL_VERSION="${2:-}"; shift 2 ;;
    --custom-version) CUSTOM_VERSION="${2:-}"; shift 2 ;;
    *) printf 'unknown migration argument: %s\n' "$1" >&2; exit 2 ;;
  esac
done

BASELINE_COMMIT=aa2d24106cab0a03785330d8e0ff4e02b0474a0e
BASELINE_STABLE_COMMIT=d0bdd7e771636a8d315f542cafd39484f39bd60c
[[ "$EXPECTED_COMMIT" == "$BASELINE_COMMIT" && "$OFFICIAL_VERSION" == v0.1.163 && "$CUSTOM_VERSION" == v1.0.0 ]] || {
  printf 'migration identity must be %s / v0.1.163 / v1.0.0\n' "$BASELINE_COMMIT" >&2
  exit 2
}

STATE_PATH="$(ledger_state_path)"
mkdir -p "$(dirname "$RELEASE_LEDGER_LOCK_FILE")"
exec 8>"$RELEASE_LEDGER_LOCK_FILE"
flock -x 8

[[ -r "$PRODUCTION_RELEASE_STATE_FILE" && -r "$COMPOSE_BASE" && -r "$COMPOSE_CUSTOM" && -r "$ENV_FILE" ]] || {
  printf 'baseline production evidence is missing\n' >&2
  exit 1
}

legacy_current="$DATA_DIR/release-current-job-id"
if [[ -s "$legacy_current" ]]; then
  legacy_id="$(tr -d '[:space:]' < "$legacy_current")"
  legacy_path="$DATA_DIR/release-jobs/$legacy_id.json"
  [[ -r "$legacy_path" ]] || { printf 'legacy release operation pointer is inconsistent\n' >&2; exit 1; }
  legacy_status="$(jq -er '.status | strings | select(length > 0)' "$legacy_path" 2>/dev/null || true)"
  case "$legacy_status" in success|failed|conflict|expired|drifted|failed_rolled_back|rollback_failed|'') ;;
    *) printf 'active release operation blocks migration\n' >&2; exit 1 ;;
  esac
  [[ -n "$legacy_status" ]] || { printf 'legacy release operation pointer is inconsistent\n' >&2; exit 1; }
fi

production_commit="$(jq -r '.production_commit // empty' "$PRODUCTION_RELEASE_STATE_FILE")"
stable_tag="$(jq -r '.stable_release_tag // empty' "$PRODUCTION_RELEASE_STATE_FILE")"
stable_commit="$(jq -r '.stable_release_commit // empty' "$PRODUCTION_RELEASE_STATE_FILE")"
main_digest="$(jq -r '.main_digest // empty' "$PRODUCTION_RELEASE_STATE_FILE")"
extensions_digest="$(jq -r '.extensions_digest // empty' "$PRODUCTION_RELEASE_STATE_FILE")"
published_at="$(jq -r '.published_at // empty' "$PRODUCTION_RELEASE_STATE_FILE")"
backup_dir="$(jq -r '.backup_dir // empty' "$PRODUCTION_RELEASE_STATE_FILE")"

[[ "$production_commit" == "$EXPECTED_COMMIT" ]] || { printf 'production commit mismatch\n' >&2; exit 1; }
[[ "$(git -C "$REPO" rev-parse HEAD 2>/dev/null || true)" == "$EXPECTED_COMMIT" ]] || { printf 'source HEAD mismatch\n' >&2; exit 1; }
if ! source_status="$(git -C "$REPO" status --porcelain --untracked-files=all 2>/dev/null)"; then
  printf 'could not verify production source cleanliness\n' >&2
  exit 1
fi
[[ -z "$source_status" ]] || { printf 'production source is dirty\n' >&2; exit 1; }
[[ "$stable_tag" == "$OFFICIAL_VERSION" && "$stable_commit" == "$BASELINE_STABLE_COMMIT" ]] || { printf 'Stable identity mismatch\n' >&2; exit 1; }
[[ "$main_digest" =~ ^sha256:[0-9a-f]{64}$ && "$extensions_digest" =~ ^sha256:[0-9a-f]{64}$ ]] || { printf 'digest metadata is invalid\n' >&2; exit 1; }

running_main="$(docker inspect --format '{{.Config.Image}}' sub2api 2>/dev/null || true)"
running_extensions="$(docker inspect --format '{{.Config.Image}}' extensions-self 2>/dev/null || true)"
[[ "$running_main" == *@"$main_digest" && "$running_extensions" == *@"$extensions_digest" ]] || { printf 'running digest pair mismatch\n' >&2; exit 1; }

backup_root_real="$(cd "$RELEASE_BACKUP_ROOT" 2>/dev/null && pwd -P)" || { printf 'backup root is missing\n' >&2; exit 1; }
backup_dir_real="$(cd "$backup_dir" 2>/dev/null && pwd -P)" || { printf 'backup directory is missing\n' >&2; exit 1; }
case "$backup_dir_real/" in "$backup_root_real/"*) ;; *) printf 'backup directory escapes configured root\n' >&2; exit 1 ;; esac
ledger_validate_backup_contract "$backup_dir_real" || { printf 'bootstrap backup contract is incomplete or invalid\n' >&2; exit 1; }

base_hash="$(sha256sum "$COMPOSE_BASE" | awk '{print $1}')"
custom_hash="$(sha256sum "$COMPOSE_CUSTOM" | awk '{print $1}')"
env_hash="$(sha256sum "$ENV_FILE" | awk '{print $1}')"
[[ "$base_hash" == "$(sha256sum "$backup_dir/target/docker-compose.yml" | awk '{print $1}')" ]] || { printf 'base Compose mismatch\n' >&2; exit 1; }
[[ "$custom_hash" == "$(sha256sum "$backup_dir/target/docker-compose.custom.yml" | awk '{print $1}')" ]] || { printf 'custom Compose mismatch\n' >&2; exit 1; }
[[ "$env_hash" == "$(sha256sum "$backup_dir/target/.env" | awk '{print $1}')" ]] || { printf 'environment mismatch\n' >&2; exit 1; }

rendered_tmp="$(mktemp "$DATA_DIR/.bootstrap-compose.XXXXXX")"
trap 'rm -f "$rendered_tmp"' EXIT
docker compose --project-name deploy -f "$COMPOSE_BASE" -f "$COMPOSE_CUSTOM" --env-file "$ENV_FILE" config --format json > "$rendered_tmp"
rendered_hash="$(sha256sum "$rendered_tmp" | awk '{print $1}')"
[[ "$rendered_hash" == "$(sha256sum "$backup_dir/target/rendered-compose.json" | awk '{print $1}')" ]] || { printf 'rendered Compose mismatch\n' >&2; exit 1; }
backup_manifest_hash="$(sha256sum "$backup_dir/SHA256SUMS" | awk '{print $1}')"

timestamp="$(date -u -d "$published_at" '+%Y%m%dT%H%M%SZ' 2>/dev/null || true)"
[[ -n "$timestamp" ]] || { printf 'published timestamp is invalid\n' >&2; exit 1; }
release_id="release-bootstrap-$timestamp-${EXPECTED_COMMIT:0:9}"
record="$(jq -n \
  --arg release_id "$release_id" --arg official_version "$OFFICIAL_VERSION" --arg official_commit "$stable_commit" \
  --arg custom_version "$CUSTOM_VERSION" --arg custom_commit "$production_commit" \
  --arg main_digest "$main_digest" --arg extensions_digest "$extensions_digest" \
  --arg base_hash "$base_hash" --arg custom_hash "$custom_hash" --arg rendered_hash "$rendered_hash" --arg env_hash "$env_hash" \
  --arg backup_dir "$backup_dir_real" --arg backup_manifest_hash "$backup_manifest_hash" --arg published_at "$published_at" \
  '{schema_version:1,release_id:$release_id,official_version:$official_version,official_commit:$official_commit,custom_version:$custom_version,custom_version_sequence:0,custom_commit:$custom_commit,main_digest:$main_digest,extensions_digest:$extensions_digest,base_compose_sha256:$base_hash,custom_compose_sha256:$custom_hash,rendered_compose_sha256:$rendered_hash,env_sha256:$env_hash,backup_dir:$backup_dir,backup_manifest_sha256:$backup_manifest_hash,published_at:$published_at,source_kind:"bootstrap",operation_id:"migration-bootstrap"}')"
projection="$(ledger_projection_for_release "$record")"

if [[ -e "$STATE_PATH" ]]; then
  _ledger_recover_or_refuse_unlocked "$record" "$projection" 0 || {
    printf 'LEDGER_INCONSISTENT: existing ledger does not match bootstrap identity\n' >&2
    exit 1
  }
  exit 0
fi

ledger_create_release "$record"
[[ "${SUB2API_LEDGER_MIGRATION_FAILPOINT:-}" != after_release_record ]] || exit 97

ledger_write_projection "$projection"
[[ "${SUB2API_LEDGER_MIGRATION_FAILPOINT:-}" != after_projection ]] || exit 98

now="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
state="$(jq -n --arg release_id "$release_id" --arg now "$now" '{schema_version:1,current_release_id:$release_id,custom_version_high_water:0,active_operation_id:null,updated_at:$now}')"
ledger_atomic_write "$STATE_PATH" "$state"
printf 'release ledger bootstrap complete: %s\n' "$release_id"
