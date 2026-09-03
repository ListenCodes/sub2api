#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DATA_DIR="${SUB2API_DATA_DIR:-/var/lib/docker/volumes/deploy_sub2api_data/_data}"
STATE_HELPER="${SUB2API_RELEASE_STATE_HELPER:-$SCRIPT_DIR/release-state.sh}"
COMMON_HELPER="${SUB2API_RELEASE_COMMON_HELPER:-$SCRIPT_DIR/release-common.sh}"
LEDGER_HELPER="${SUB2API_RELEASE_LEDGER_HELPER:-$SCRIPT_DIR/release-ledger.sh}"
REPO="${SUB2API_REPO:-/root/sub2api}"
ENV_FILE="${SUB2API_ENV_FILE:-$REPO/deploy/.env}"
COMPOSE_BASE="${SUB2API_COMPOSE_BASE:-$REPO/deploy/docker-compose.yml}"
COMPOSE_CUSTOM="${SUB2API_COMPOSE_CUSTOM:-$REPO/deploy/docker-compose.custom.yml}"
BACKUP_ROOT="${SUB2API_BACKUP_ROOT:-/var/lib/sub2api-release/backups}"
export SUB2API_RELEASE_BACKUP_ROOT="${SUB2API_RELEASE_BACKUP_ROOT:-$BACKUP_ROOT}"
PREPARED_ROOT="${SUB2API_PREPARED_ROOT:-/var/lib/sub2api-release/prepared}"
MAIN_REPOSITORY="${SUB2API_MAIN_REPOSITORY:-ghcr.io/listencodes/sub2api-custom}"
EXTENSIONS_REPOSITORY="${SUB2API_EXTENSIONS_REPOSITORY:-ghcr.io/listencodes/sub2api-extensions}"
NGINX_VHOST="${SUB2API_NGINX_VHOST:-/etc/nginx/sites-available/sub.ailisten.top}"
ORIGIN_CERT="${SUB2API_ORIGIN_CERT:-/etc/nginx/ssl/ailisten.top.crt}"
ORIGIN_KEY="${SUB2API_ORIGIN_KEY:-/etc/nginx/ssl/ailisten.top.key}"
LOG="${SUB2API_SYNC_PUBLISH_LOG:-/var/log/sub2api-release.log}"

source "$STATE_HELPER"
source "$COMMON_HELPER"
source "$LEDGER_HELPER"

JOB_ID="${SUB2API_JOB_ID:-${2:-}}"
[[ "${1:-}" == --job-id && -n "$JOB_ID" ]] || { printf 'usage: prepare-rollback.sh --job-id <job-id>\n' >&2; exit 2; }
[[ "$JOB_ID" == rollback-* ]] && release_valid_job_id "$JOB_ID" || { printf 'invalid rollback job id\n' >&2; exit 2; }
JOB_FILE="$(release_job_path "$JOB_ID")"
[[ -r "$JOB_FILE" ]] || { printf 'rollback operation is missing\n' >&2; exit 1; }
[[ "$(jq -r '.operation_kind // empty' "$JOB_FILE")" == rollback && "$(jq -r '.action // empty' "$JOB_FILE")" == prepare ]] \
  || { release_job_fail "$JOB_ID" LEGACY_SINGLE_PHASE_UNSUPPORTED 'Legacy or non-rollback prepare operation rejected'; exit 1; }

fail_prepare() {
  local message="$1" code="${2:-ROLLBACK_PREPARE_FAILED}" metadata
  metadata="$(jq -n --arg code "$code" '{error_code:$code,published:false,production_changed:false,rollback:{attempted:false,succeeded:false,message:""}}')"
  ledger_settle_pre_mutation_failure "$JOB_ID" failed "$message" "$metadata" || true
  printf '%s\n' "$message" >&2
  exit 1
}
trap 'fail_prepare "unexpected rollback prepare failure at line $LINENO" UNEXPECTED_ROLLBACK_PREPARE_ERROR' ERR

mkdir -p "$DATA_DIR" "$(dirname "$LOG")"
release_ensure_backup_root || fail_prepare 'root-owned rollback backup directory is unsafe' BACKUP_PATH_UNSAFE
touch "$LOG"
RENDERED=''
MANIFEST_SOURCE=''
cleanup() {
  [[ -z "$RENDERED" ]] || rm -f "$RENDERED"
  [[ -z "$MANIFEST_SOURCE" ]] || rm -f "$MANIFEST_SOURCE"
}
trap cleanup EXIT

capture_data_quality_baseline() {
  local rendered="$1" enabled secret timestamp nonce signature quality
  BASELINE_MISSING_GROUP_REQUESTS=0
  BASELINE_DATA_AS_OF=''
  [[ -r "$rendered" ]] || return 1
  enabled="$(jq -r '.services["extensions-self"].environment.ACCOUNT_MONITOR_ENABLED // "false" | ascii_downcase' "$rendered")"
  [[ "$enabled" == false ]] && return 0
  [[ "$enabled" == true ]] || return 1
  secret="$(jq -r '.services["extensions-self"].environment.RISK_CONTROL_INTERNAL_SECRET // empty' "$rendered")"
  [[ -n "$secret" ]] || return 1
  timestamp="$(date +%s)"; nonce="prepare-$JOB_ID-$timestamp"
  signature="$(MONITOR_SECRET="$secret" MONITOR_TIMESTAMP="$timestamp" MONITOR_NONCE="$nonce" python3 -c '
import hashlib, hmac, os
message = (os.environ["MONITOR_TIMESTAMP"] + "\n" + os.environ["MONITOR_NONCE"] + "\n").encode()
print(hmac.new(os.environ["MONITOR_SECRET"].encode(), message, hashlib.sha256).hexdigest())
')" || return 1
  quality="$(docker exec extensions-self wget -qO- -T 10 \
    --header="X-Risk-Timestamp: $timestamp" --header="X-Risk-Nonce: $nonce" \
    --header="X-Risk-Signature: $signature" --header='X-Risk-Actor-ID: 1' \
    http://extensions-self:8090/api/v1/admin/account-monitor/data-quality)" || return 1
  jq -e '.source_connected == true and (.missing_group_requests | type == "number" and floor == . and . >= 0) and (.data_as_of | type == "string" and length > 0)' <<< "$quality" >/dev/null || return 1
  BASELINE_MISSING_GROUP_REQUESTS="$(jq -r '.missing_group_requests' <<< "$quality")"
  BASELINE_DATA_AS_OF="$(jq -r '.data_as_of' <<< "$quality")"
}

STATE_PATH="$(ledger_state_path)"
ledger_validate_state "$STATE_PATH" || fail_prepare 'release ledger state is invalid' LEDGER_INCONSISTENT
BASE_RELEASE_ID="$(jq -r '.current_release_id' "$STATE_PATH")"
BASE_CUSTOM_HIGH_WATER="$(jq -r '.custom_version_high_water' "$STATE_PATH")"
[[ "$(jq -r '.active_operation_id // empty' "$STATE_PATH")" == "$JOB_ID" ]] || fail_prepare 'release ledger does not own rollback preparation' LEDGER_OPERATION_DRIFT
[[ "$(jq -r '.base_release_id // empty' "$JOB_FILE")" == "$BASE_RELEASE_ID" ]] || fail_prepare 'rollback base release drifted' CURRENT_RELEASE_DRIFT
TARGET_RELEASE_ID="$(jq -r '.target_release_id // empty' "$JOB_FILE")"
[[ "$TARGET_RELEASE_ID" =~ ^release-[A-Za-z0-9-]+$ && "$TARGET_RELEASE_ID" != "$BASE_RELEASE_ID" ]] || fail_prepare 'rollback target is invalid or current' ROLLBACK_TARGET_INVALID

BASE_RECORD_PATH="$(ledger_release_path "$BASE_RELEASE_ID")"
TARGET_RECORD_PATH="$(ledger_release_path "$TARGET_RELEASE_ID")"
ledger_validate_release "$BASE_RECORD_PATH" || fail_prepare 'current release record is invalid' LEDGER_INCONSISTENT
BASE_RECORD="$(cat "$BASE_RECORD_PATH")"
jq -e --argjson record "$BASE_RECORD" '
  .release_id == $record.release_id and .production_commit == $record.custom_commit
  and .stable_release_tag == $record.official_version and .stable_release_commit == $record.official_commit
  and .main_digest == $record.main_digest and .extensions_digest == $record.extensions_digest
  and .official_version == $record.official_version and .custom_version == $record.custom_version
  and .custom_version_sequence == $record.custom_version_sequence
' "$PRODUCTION_RELEASE_STATE_FILE" >/dev/null || fail_prepare 'compatibility projection drifted from current release' LEDGER_PROJECTION_DRIFT

eligible=false
while IFS= read -r release_id; do [[ "$release_id" != "$TARGET_RELEASE_ID" ]] || eligible=true; done < <(ledger_list_rollback_release_ids 3)
[[ "$eligible" == true ]] || fail_prepare 'rollback target is not one of the latest three complete snapshots' ROLLBACK_TARGET_INELIGIBLE
ledger_validate_release_artifacts "$TARGET_RECORD_PATH" || fail_prepare 'historical release artifacts are incomplete or corrupt' ROLLBACK_ARTIFACT_DRIFT
TARGET_RECORD="$(cat "$TARGET_RECORD_PATH")"
TARGET_ARTIFACT_DIR="$(jq -r '.backup_dir' <<< "$TARGET_RECORD")/target"
TARGET_COMMIT="$(jq -r '.custom_commit' <<< "$TARGET_RECORD")"
MAIN_DIGEST="$(jq -r '.main_digest' <<< "$TARGET_RECORD")"
EXTENSIONS_DIGEST="$(jq -r '.extensions_digest' <<< "$TARGET_RECORD")"
TARGET_OFFICIAL_VERSION="$(jq -r '.official_version' <<< "$TARGET_RECORD")"
TARGET_CUSTOM_VERSION="$(jq -r '.custom_version' <<< "$TARGET_RECORD")"

release_job_update "$JOB_ID" verifying_snapshot 'Verifying the selected complete release snapshot' \
  "$(jq -n --arg base "$BASE_RELEASE_ID" --arg target "$TARGET_RELEASE_ID" --argjson high "$BASE_CUSTOM_HIGH_WATER" '{base_release_id:$base,target_release_id:$target,base_custom_high_water:$high}')"
git -C "$REPO" cat-file -e "$TARGET_COMMIT^{commit}" >/dev/null 2>&1 || fail_prepare 'historical source commit is unavailable locally' TARGET_SOURCE_MISSING

ensure_target_image() {
  local repository="$1" digest="$2" canonical
  canonical="$repository@$digest"
  if ! docker image inspect "$canonical" >/dev/null 2>&1; then
    release_job_update "$JOB_ID" downloading_images "Pulling missing historical digest $canonical" '{}'
    docker pull "$canonical" >> "$LOG" 2>&1 || return 1
  fi
  release_verify_local_image_identity "$repository" "$digest" "$TARGET_COMMIT" "${TARGET_OFFICIAL_VERSION#v}"
}
ensure_target_image "$MAIN_REPOSITORY" "$MAIN_DIGEST" || fail_prepare 'historical main image is missing or invalid' TARGET_IMAGE_INVALID
ensure_target_image "$EXTENSIONS_REPOSITORY" "$EXTENSIONS_DIGEST" || fail_prepare 'historical extensions image is missing or invalid' TARGET_IMAGE_INVALID

RENDERED="$(mktemp "$DATA_DIR/.rollback-target-$JOB_ID.XXXXXX")"
release_validate_snapshot_against_record "$TARGET_ARTIFACT_DIR/docker-compose.yml" "$TARGET_ARTIFACT_DIR/docker-compose.custom.yml" \
  "$TARGET_ARTIFACT_DIR/.env" "$RENDERED" "$TARGET_RECORD" "$LOG" || fail_prepare 'historical Compose/environment contract is invalid' TARGET_COMPOSE_INVALID
rm -f "$RENDERED"; RENDERED=''

STAMP="$(date -u '+%Y%m%dT%H%M%S%NZ')"
BACKUP_DIR="$(mktemp -d "$BACKUP_ROOT/$JOB_ID-$STAMP.XXXXXX")"
mkdir -p "$BACKUP_DIR/target"
cp -p "$TARGET_ARTIFACT_DIR/docker-compose.yml" "$BACKUP_DIR/target/docker-compose.yml"
cp -p "$TARGET_ARTIFACT_DIR/docker-compose.custom.yml" "$BACKUP_DIR/target/docker-compose.custom.yml"
cp -p "$TARGET_ARTIFACT_DIR/.env" "$BACKUP_DIR/target/.env"
release_job_update "$JOB_ID" rendering_compose 'Rendering the selected historical Compose pair' '{}'
release_render_explicit_compose "$BACKUP_DIR/target/docker-compose.yml" "$BACKUP_DIR/target/docker-compose.custom.yml" \
  "$BACKUP_DIR/target/.env" "$BACKUP_DIR/target/rendered-compose.json" "$LOG" || fail_prepare 'historical Compose rendering failed' TARGET_COMPOSE_INVALID
release_validate_rendered_compose "$BACKUP_DIR/target/rendered-compose.json" "$MAIN_REPOSITORY@$MAIN_DIGEST" "$EXTENSIONS_REPOSITORY@$EXTENSIONS_DIGEST" \
  || fail_prepare 'historical Compose contract failed' TARGET_COMPOSE_INVALID
[[ "$(sha256sum "$BACKUP_DIR/target/rendered-compose.json" | awk '{print $1}')" == "$(jq -r '.rendered_compose_sha256' <<< "$TARGET_RECORD")" ]] \
  || fail_prepare 'historical rendered Compose drifted' TARGET_COMPOSE_INVALID

release_job_update "$JOB_ID" backing_up 'Backing up the current complete release before rollback' "$(jq -n --arg dir "$BACKUP_DIR" '{backup_dir:$dir}')"
release_create_complete_backup "$BACKUP_DIR" "$JOB_ID" "$LOG" || fail_prepare 'fresh current backup validation failed' BACKUP_CONTRACT_FAILED
RENDERED="$(mktemp "$DATA_DIR/.rollback-current-$JOB_ID.XXXXXX")"
release_validate_snapshot_against_record "$BACKUP_DIR/docker-compose.yml" "$BACKUP_DIR/docker-compose.custom.yml" "$BACKUP_DIR/.env" "$RENDERED" "$BASE_RECORD" "$LOG" \
  || fail_prepare 'fresh current snapshot drifted from the ledger' CURRENT_SNAPSHOT_DRIFT
capture_data_quality_baseline "$RENDERED" || fail_prepare 'production data-quality baseline is unavailable or invalid' DATA_QUALITY_BASELINE_FAILED
rm -f "$RENDERED"; RENDERED=''

prepared_at="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
expires_at="$(date -u -d "$prepared_at +60 minutes" '+%Y-%m-%dT%H:%M:%SZ')"
MANIFEST_DIR="$(release_prepare_manifest_dir "$JOB_ID")" \
  || fail_prepare 'root-owned prepared manifest directory is unsafe' PREPARED_PATH_UNSAFE
MANIFEST_SOURCE="$(mktemp "$MANIFEST_DIR/.rollback-manifest-source.XXXXXX")" \
  || fail_prepare 'rollback manifest temporary file could not be created' PREPARED_MANIFEST_INVALID
chmod 0600 "$MANIFEST_SOURCE" \
  || fail_prepare 'rollback manifest temporary file permissions could not be secured' PREPARED_MANIFEST_INVALID
jq -n --arg operation rollback --arg base "$BASE_RELEASE_ID" --arg target "$TARGET_RELEASE_ID" --argjson high "$BASE_CUSTOM_HIGH_WATER" \
  --arg source "$(jq -r '.custom_commit' <<< "$BASE_RECORD")" --arg target_commit "$TARGET_COMMIT" \
  --arg official "$TARGET_OFFICIAL_VERSION" --arg custom "$TARGET_CUSTOM_VERSION" --arg main "$MAIN_DIGEST" --arg ext "$EXTENSIONS_DIGEST" \
  --arg current_main "$(jq -r '.main_digest' <<< "$BASE_RECORD")" --arg current_ext "$(jq -r '.extensions_digest' <<< "$BASE_RECORD")" \
  --arg current_base "$(sha256sum "$BACKUP_DIR/docker-compose.yml" | awk '{print $1}')" --arg current_custom "$(sha256sum "$BACKUP_DIR/docker-compose.custom.yml" | awk '{print $1}')" \
  --arg target_base "$(sha256sum "$BACKUP_DIR/target/docker-compose.yml" | awk '{print $1}')" --arg target_custom "$(sha256sum "$BACKUP_DIR/target/docker-compose.custom.yml" | awk '{print $1}')" \
  --arg target_rendered "$(sha256sum "$BACKUP_DIR/target/rendered-compose.json" | awk '{print $1}')" --arg target_env "$(sha256sum "$BACKUP_DIR/target/.env" | awk '{print $1}')" \
  --arg target_manifest "$(sha256sum "$BACKUP_DIR/target/SHA256SUMS" | awk '{print $1}')" --arg backup "$BACKUP_DIR" \
  --arg backup_manifest "$(sha256sum "$BACKUP_DIR/SHA256SUMS" | awk '{print $1}')" --arg prepared "$prepared_at" --arg expires "$expires_at" \
  --arg baseline_data_as_of "$BASELINE_DATA_AS_OF" --argjson baseline_missing "$BASELINE_MISSING_GROUP_REQUESTS" '
  {schema_version:1,operation_kind:$operation,base_release_id:$base,target_release_id:$target,base_custom_high_water:$high,
  source_commit:$source,target_commit:$target_commit,target_official_version:$official,target_custom_version:$custom,
  main_digest:$main,extensions_digest:$ext,current_main_digest:$current_main,current_extensions_digest:$current_ext,
  current_base_compose_sha256:$current_base,current_custom_compose_sha256:$current_custom,target_base_compose_sha256:$target_base,
  target_custom_compose_sha256:$target_custom,target_rendered_compose_sha256:$target_rendered,target_env_sha256:$target_env,
  target_artifact_manifest_sha256:$target_manifest,backup_dir:$backup,backup_manifest_sha256:$backup_manifest,
  baseline_missing_group_requests:$baseline_missing,baseline_data_as_of:$baseline_data_as_of,
  prepared_at:$prepared,expires_at:$expires,images_verified:true,compose_contract:"deploy-explicit-pair-v1",backup_contract:"complete-paired-snapshot-v1"}' \
  > "$MANIFEST_SOURCE"
release_install_manifest_files "$MANIFEST_DIR" "$MANIFEST_SOURCE" \
  || fail_prepare 'rollback manifest files could not be installed safely' PREPARED_PATH_UNSAFE
rm -f "$MANIFEST_SOURCE"
MANIFEST_SOURCE=''
release_manifest_valid "$JOB_ID" || fail_prepare 'rollback prepared manifest failed validation' PREPARED_MANIFEST_INVALID
manifest_sha="$(awk '{print $1}' "$MANIFEST_DIR/manifest.sha256")"
metadata="$(jq --arg manifest "$MANIFEST_DIR/manifest.json" --arg sha "$manifest_sha" '. + {prepared_manifest:$manifest,prepared_manifest_sha256:$sha,published:false,production_changed:false}' "$MANIFEST_DIR/manifest.json")"
release_job_update "$JOB_ID" validating_backup 'Fresh current backup and rollback manifest validated' "$metadata"
release_job_update "$JOB_ID" prepared 'Rollback prepared; administrator confirmation is required' "$metadata"
