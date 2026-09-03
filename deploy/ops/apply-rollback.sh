#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DATA_DIR="${SUB2API_DATA_DIR:-/var/lib/docker/volumes/deploy_sub2api_data/_data}"
STATE_HELPER="${SUB2API_RELEASE_STATE_HELPER:-$SCRIPT_DIR/release-state.sh}"
COMMON_HELPER="${SUB2API_RELEASE_COMMON_HELPER:-$SCRIPT_DIR/release-common.sh}"
LEDGER_HELPER="${SUB2API_RELEASE_LEDGER_HELPER:-$SCRIPT_DIR/release-ledger.sh}"
REPO="${SUB2API_REPO:-/root/sub2api}"
BRANCH="${SUB2API_BRANCH:-custom-release}"
ENV_FILE="${SUB2API_ENV_FILE:-$REPO/deploy/.env}"
COMPOSE_BASE="${SUB2API_COMPOSE_BASE:-$REPO/deploy/docker-compose.yml}"
COMPOSE_CUSTOM="${SUB2API_COMPOSE_CUSTOM:-$REPO/deploy/docker-compose.custom.yml}"
MAIN_REPOSITORY="${SUB2API_MAIN_REPOSITORY:-ghcr.io/listencodes/sub2api-custom}"
EXTENSIONS_REPOSITORY="${SUB2API_EXTENSIONS_REPOSITORY:-ghcr.io/listencodes/sub2api-extensions}"
INTERNAL_HEALTH_URL="${SUB2API_INTERNAL_HEALTH_URL:-http://127.0.0.1:8081/health}"
PUBLIC_HEALTH_URL="${SUB2API_PUBLIC_HEALTH_URL:-https://sub.ailisten.top/health}"
ADMIN_HEALTH_URL="${SUB2API_ADMIN_HEALTH_URL:-http://127.0.0.1:8081/admin}"
EXTENSION_ROUTE_URL="${SUB2API_EXTENSION_ROUTE_URL:-http://127.0.0.1:8081/admin/extensions/account-monitor}"
HOMEPAGE_HEALTH_URL="${SUB2API_HOMEPAGE_HEALTH_URL:-http://127.0.0.1:8081/api/v1/extensions-self/homepage/}"
LOG="${SUB2API_SYNC_PUBLISH_LOG:-/var/log/sub2api-release.log}"
HEALTH_WAIT_TIMEOUT_SECONDS="${SUB2API_HEALTH_WAIT_TIMEOUT_SECONDS:-180}"
HEALTH_WAIT_INTERVAL_SECONDS="${SUB2API_HEALTH_WAIT_INTERVAL_SECONDS:-2}"

source "$STATE_HELPER"
source "$COMMON_HELPER"
source "$LEDGER_HELPER"

JOB_ID="${SUB2API_JOB_ID:-${2:-}}"
[[ "${1:-}" == --job-id && "$JOB_ID" == rollback-* ]] || { printf 'usage: apply-rollback.sh --job-id <rollback-job-id>\n' >&2; exit 2; }
release_valid_job_id "$JOB_ID" || { printf 'invalid rollback job id\n' >&2; exit 2; }
JOB_FILE="$(release_job_path "$JOB_ID")"
[[ -r "$JOB_FILE" ]] || { printf 'rollback operation is missing\n' >&2; exit 1; }
[[ "$(jq -r '.operation_kind // empty' "$JOB_FILE")" == rollback && "$(jq -r '.action // empty' "$JOB_FILE")" == apply ]] \
  || { release_job_fail "$JOB_ID" LEGACY_SINGLE_PHASE_UNSUPPORTED 'Legacy or non-rollback apply operation rejected'; exit 1; }

mkdir -p "$DATA_DIR" "$(dirname "$LOG")"
touch "$LOG"

fail_before_mutation() {
  local message="$1" code="${2:-ROLLBACK_APPLY_VALIDATION_FAILED}" status="${3:-drifted}" metadata
  metadata="$(jq -n --arg code "$code" '{error_code:$code,published:false,production_changed:false,rollback:{attempted:false,succeeded:false,message:""}}')"
  ledger_settle_pre_mutation_failure "$JOB_ID" "$status" "$message" "$metadata" || true
  printf '%s\n' "$message" >&2
  exit 1
}

operation_status="$(jq -r '.status // empty' "$JOB_FILE")"
if release_terminal_status "$operation_status" && [[ "$operation_status" != success ]]; then
  ledger_recover_pre_mutation_terminal "$JOB_ID" || true
  printf 'rollback operation is already terminal: %s\n' "$operation_status" >&2
  exit 1
fi

manifest="$(release_manifest_path "$JOB_ID")"
manifest_check=0
release_manifest_valid "$JOB_ID" || manifest_check=$?
[[ "$manifest_check" -ne 2 ]] || fail_before_mutation 'Prepared rollback expired; prepare again' PREPARED_EXPIRED expired
[[ "$manifest_check" -eq 0 ]] || fail_before_mutation 'Prepared rollback manifest is invalid' PREPARED_MANIFEST_INVALID failed

BASE_RELEASE_ID="$(jq -r '.base_release_id' "$manifest")"
TARGET_RELEASE_ID="$(jq -r '.target_release_id' "$manifest")"
BASE_CUSTOM_HIGH_WATER="$(jq -r '.base_custom_high_water' "$manifest")"
SOURCE_COMMIT="$(jq -r '.source_commit' "$manifest")"
TARGET_COMMIT="$(jq -r '.target_commit' "$manifest")"
TARGET_OFFICIAL_VERSION="$(jq -r '.target_official_version' "$manifest")"
MAIN_DIGEST="$(jq -r '.main_digest' "$manifest")"
EXTENSIONS_DIGEST="$(jq -r '.extensions_digest' "$manifest")"
CURRENT_MAIN_DIGEST="$(jq -r '.current_main_digest' "$manifest")"
CURRENT_EXTENSIONS_DIGEST="$(jq -r '.current_extensions_digest' "$manifest")"
BACKUP_DIR="$(jq -r '.backup_dir' "$manifest")"
TARGET_DIR="$BACKUP_DIR/target"
BASELINE_MISSING_GROUP_REQUESTS="$(jq -r '.baseline_missing_group_requests // empty' "$manifest")"
if [[ -n "$BASELINE_MISSING_GROUP_REQUESTS" ]]; then
  [[ "$BASELINE_MISSING_GROUP_REQUESTS" =~ ^[0-9]+$ ]] || fail_before_mutation 'prepared data-quality baseline is invalid' PREPARED_MANIFEST_INVALID failed
fi
STATE_PATH="$(ledger_state_path)"
BASE_RECORD_PATH="$(ledger_release_path "$BASE_RELEASE_ID")"
TARGET_RECORD_PATH="$(ledger_release_path "$TARGET_RELEASE_ID")"

wait_container_healthy() {
  local container="$1" deadline status
  deadline=$((SECONDS + HEALTH_WAIT_TIMEOUT_SECONDS))
  while (( SECONDS < deadline )); do
    status="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' "$container" 2>/dev/null || true)"
    case "$status" in healthy) return 0 ;; unhealthy|exited|dead) return 1 ;; esac
    sleep "$HEALTH_WAIT_INTERVAL_SECONDS"
  done
  return 1
}

check_data_quality() {
  local rendered="$1" enabled secret timestamp nonce signature quality missing data_as_of
  DATA_QUALITY_DIAGNOSTICS='{}'
  enabled="$(jq -r '.services["extensions-self"].environment.ACCOUNT_MONITOR_ENABLED // "false" | ascii_downcase' "$rendered")"
  [[ "$enabled" == false ]] && return 0
  [[ "$enabled" == true ]] || return 1
  secret="$(jq -r '.services["extensions-self"].environment.RISK_CONTROL_INTERNAL_SECRET // empty' "$rendered")"
  [[ -n "$secret" ]] || return 1
  timestamp="$(date +%s)"; nonce="rollback-$JOB_ID-$timestamp"
  signature="$(MONITOR_SECRET="$secret" MONITOR_TIMESTAMP="$timestamp" MONITOR_NONCE="$nonce" python3 -c '
import hashlib, hmac, os
message = (os.environ["MONITOR_TIMESTAMP"] + "\n" + os.environ["MONITOR_NONCE"] + "\n").encode()
print(hmac.new(os.environ["MONITOR_SECRET"].encode(), message, hashlib.sha256).hexdigest())
')" || return 1
  quality="$(docker exec extensions-self wget -qO- -T 10 --header="X-Risk-Timestamp: $timestamp" --header="X-Risk-Nonce: $nonce" --header="X-Risk-Signature: $signature" --header='X-Risk-Actor-ID: 1' http://extensions-self:8090/api/v1/admin/account-monitor/data-quality)" || return 1
  if ! jq -e '.source_connected == true and (.missing_group_requests | type == "number" and floor == . and . >= 0) and (.data_as_of | type == "string" and length > 0)' <<< "$quality" >/dev/null; then
    DATA_QUALITY_DIAGNOSTICS="$(jq -c '{source_connected,missing_group_requests,data_as_of}' <<< "$quality" 2>/dev/null || printf '{}')"
    return 1
  fi
  missing="$(jq -r '.missing_group_requests' <<< "$quality")"
  data_as_of="$(jq -r '.data_as_of' <<< "$quality")"
  if [[ -n "$BASELINE_MISSING_GROUP_REQUESTS" ]] && (( missing > BASELINE_MISSING_GROUP_REQUESTS )); then
    DATA_QUALITY_DIAGNOSTICS="$(jq -cn --argjson actual "$missing" --argjson baseline "$BASELINE_MISSING_GROUP_REQUESTS" --arg as_of "$data_as_of" \
      '{source_connected:true,missing_group_requests:$actual,baseline_missing_group_requests:$baseline,data_as_of:$as_of}')"
    return 1
  fi
  return 0
}

run_complete_health() {
  local rendered="$1" container
  docker compose --project-name deploy -f "$COMPOSE_BASE" -f "$COMPOSE_CUSTOM" --env-file "$ENV_FILE" ps --status running >/dev/null || return 1
  for container in extensions-self sub2api sub2api-postgres sub2api-redis risk-control-postgres; do wait_container_healthy "$container" || return 1; done
  if [[ "${SUB2API_SKIP_EXTERNAL_HEALTH_CHECKS:-0}" != 1 ]]; then
    curl -fsS --max-time 15 "$INTERNAL_HEALTH_URL" >/dev/null || return 1
    docker exec extensions-self wget -qO- -T 5 http://extensions-self:8090/healthz >/dev/null || return 1
    curl -fsS --max-time 15 "$HOMEPAGE_HEALTH_URL" >/dev/null || return 1
    curl -fsS --max-time 15 "$PUBLIC_HEALTH_URL" >/dev/null || return 1
    curl -fsS --max-time 15 "$ADMIN_HEALTH_URL" >/dev/null || return 1
    curl -fsS --max-time 15 "$EXTENSION_ROUTE_URL" >/dev/null || return 1
    check_data_quality "$rendered" || return 1
  fi
}

ledger_validate_state "$STATE_PATH" || fail_before_mutation 'release ledger state is invalid' LEDGER_INCONSISTENT failed
ledger_validate_release "$BASE_RECORD_PATH" || fail_before_mutation 'rollback base release record is invalid' LEDGER_INCONSISTENT failed
ledger_validate_release_artifacts "$TARGET_RECORD_PATH" || fail_before_mutation 'rollback target record or artifacts are invalid' TARGET_RECORD_DRIFT failed
BASE_RECORD="$(cat "$BASE_RECORD_PATH")"; TARGET_RECORD="$(cat "$TARGET_RECORD_PATH")"
BASE_PROJECTION="$(ledger_projection_for_release "$BASE_RECORD")" || fail_before_mutation 'base projection cannot be reconstructed' LEDGER_INCONSISTENT failed

[[ "$(jq -r '.current_release_id' "$STATE_PATH")" == "$BASE_RELEASE_ID" ]] || fail_before_mutation 'current release changed since rollback preparation' CURRENT_RELEASE_DRIFT
[[ "$(jq -r '.custom_version_high_water' "$STATE_PATH")" -eq "$BASE_CUSTOM_HIGH_WATER" ]] || fail_before_mutation 'custom version high-water changed since rollback preparation' CUSTOM_HIGH_WATER_DRIFT
[[ "$(jq -r '.active_operation_id // empty' "$STATE_PATH")" == "$JOB_ID" ]] || fail_before_mutation 'ledger no longer owns rollback operation' LEDGER_OPERATION_DRIFT
[[ "$(jq -cS . "$PRODUCTION_RELEASE_STATE_FILE")" == "$(jq -cS . <<< "$BASE_PROJECTION")" ]] || fail_before_mutation 'compatibility projection drifted' LEDGER_PROJECTION_DRIFT
jq -e -n --argjson base "$BASE_RECORD" --argjson target "$TARGET_RECORD" --argjson manifest "$(cat "$manifest")" '
  $base.release_id == $manifest.base_release_id and $base.custom_commit == $manifest.source_commit
  and $base.main_digest == $manifest.current_main_digest and $base.extensions_digest == $manifest.current_extensions_digest
  and $target.release_id == $manifest.target_release_id and $target.custom_commit == $manifest.target_commit
  and $target.official_version == $manifest.target_official_version and $target.custom_version == $manifest.target_custom_version
  and $target.main_digest == $manifest.main_digest and $target.extensions_digest == $manifest.extensions_digest
' >/dev/null || fail_before_mutation 'rollback manifest contradicts release records' TARGET_RECORD_DRIFT failed
ledger_validate_backup_contract "$BACKUP_DIR" || fail_before_mutation 'fresh rollback backup is invalid' BACKUP_DRIFT failed
[[ "$(sha256sum "$BACKUP_DIR/SHA256SUMS" | awk '{print $1}')" == "$(jq -r '.backup_manifest_sha256' "$manifest")" ]] || fail_before_mutation 'fresh rollback backup manifest drifted' BACKUP_DRIFT failed

RENDERED="$(mktemp "$DATA_DIR/.rollback-current-$JOB_ID.XXXXXX")"
release_validate_snapshot_against_record "$COMPOSE_BASE" "$COMPOSE_CUSTOM" "$ENV_FILE" "$RENDERED" "$BASE_RECORD" "$LOG" || { rm -f "$RENDERED"; fail_before_mutation 'current runtime snapshot drifted' CURRENT_SNAPSHOT_DRIFT; }
rm -f "$RENDERED"
RENDERED="$(mktemp "$DATA_DIR/.rollback-target-$JOB_ID.XXXXXX")"
release_validate_snapshot_against_record "$TARGET_DIR/docker-compose.yml" "$TARGET_DIR/docker-compose.custom.yml" "$TARGET_DIR/.env" "$RENDERED" "$TARGET_RECORD" "$LOG" || { rm -f "$RENDERED"; fail_before_mutation 'prepared rollback target drifted' TARGET_ARTIFACT_DRIFT; }
rm -f "$RENDERED"
[[ "$(sha256sum "$TARGET_DIR/SHA256SUMS" | awk '{print $1}')" == "$(jq -r '.target_artifact_manifest_sha256' "$manifest")" ]] || fail_before_mutation 'prepared target manifest drifted' TARGET_ARTIFACT_DRIFT

release_verify_local_image_identity "$MAIN_REPOSITORY" "$MAIN_DIGEST" "$TARGET_COMMIT" "${TARGET_OFFICIAL_VERSION#v}" || fail_before_mutation 'historical main image is unavailable locally' PREPARED_IMAGE_DRIFT
release_verify_local_image_identity "$EXTENSIONS_REPOSITORY" "$EXTENSIONS_DIGEST" "$TARGET_COMMIT" "${TARGET_OFFICIAL_VERSION#v}" || fail_before_mutation 'historical extensions image is unavailable locally' PREPARED_IMAGE_DRIFT
BASE_OFFICIAL_VERSION="$(jq -r '.official_version' <<< "$BASE_RECORD")"
release_verify_local_image_identity "$MAIN_REPOSITORY" "$CURRENT_MAIN_DIGEST" "$SOURCE_COMMIT" "${BASE_OFFICIAL_VERSION#v}" || fail_before_mutation 'base main image is unavailable locally' PREPARED_IMAGE_DRIFT
release_verify_local_image_identity "$EXTENSIONS_REPOSITORY" "$CURRENT_EXTENSIONS_DIGEST" "$SOURCE_COMMIT" "${BASE_OFFICIAL_VERSION#v}" || fail_before_mutation 'base extensions image is unavailable locally' PREPARED_IMAGE_DRIFT
release_source_snapshot || fail_before_mutation 'production source is dirty or unreadable' SOURCE_WORKTREE_DIRTY
[[ "$SOURCE_HEAD" == "$SOURCE_COMMIT" ]] || fail_before_mutation 'production source changed since preparation' SOURCE_HEAD_DRIFT
release_running_container_matches_image sub2api "$MAIN_REPOSITORY@$CURRENT_MAIN_DIGEST" || fail_before_mutation 'running main image drifted' RUNNING_IMAGE_DRIFT
release_running_container_matches_image extensions-self "$EXTENSIONS_REPOSITORY@$CURRENT_EXTENSIONS_DIGEST" || fail_before_mutation 'running extensions image drifted' RUNNING_IMAGE_DRIFT

rollback_started=false
failure_message=''; failure_code=''
DATA_QUALITY_DIAGNOSTICS='{}'
restore_base_runtime() {
  local rendered
  release_restore_source_snapshot "$SOURCE_HEAD" "$SOURCE_REF" >> "$LOG" 2>&1 || return 1
  release_install_snapshot_artifacts "$BACKUP_DIR" || return 1
  release_verify_local_image_identity "$MAIN_REPOSITORY" "$CURRENT_MAIN_DIGEST" "$SOURCE_COMMIT" "$(jq -r '.official_version' <<< "$BASE_RECORD" | sed 's/^v//')" || return 1
  release_verify_local_image_identity "$EXTENSIONS_REPOSITORY" "$CURRENT_EXTENSIONS_DIGEST" "$SOURCE_COMMIT" "$(jq -r '.official_version' <<< "$BASE_RECORD" | sed 's/^v//')" || return 1
  rendered="$(mktemp "$DATA_DIR/.rollback-restore-$JOB_ID.XXXXXX")" || return 1
  release_validate_snapshot_against_record "$COMPOSE_BASE" "$COMPOSE_CUSTOM" "$ENV_FILE" "$rendered" "$BASE_RECORD" "$LOG" || { rm -f "$rendered"; return 1; }
  SUB2API_IMAGE="$MAIN_REPOSITORY@$CURRENT_MAIN_DIGEST" EXTENSIONS_SELF_IMAGE="$EXTENSIONS_REPOSITORY@$CURRENT_EXTENSIONS_DIGEST" docker compose --project-name deploy -f "$COMPOSE_BASE" -f "$COMPOSE_CUSTOM" --env-file "$ENV_FILE" up -d --pull never --no-deps --force-recreate extensions-self >> "$LOG" 2>&1 || { rm -f "$rendered"; return 1; }
  wait_container_healthy extensions-self || { rm -f "$rendered"; return 1; }
  SUB2API_IMAGE="$MAIN_REPOSITORY@$CURRENT_MAIN_DIGEST" EXTENSIONS_SELF_IMAGE="$EXTENSIONS_REPOSITORY@$CURRENT_EXTENSIONS_DIGEST" docker compose --project-name deploy -f "$COMPOSE_BASE" -f "$COMPOSE_CUSTOM" --env-file "$ENV_FILE" up -d --pull never --no-deps --force-recreate sub2api >> "$LOG" 2>&1 || { rm -f "$rendered"; return 1; }
  wait_container_healthy sub2api && run_complete_health "$rendered"; result=$?; rm -f "$rendered"; return "$result"
}

rollback_on_error() {
  local code="${1:-$?}" restored=true
  trap - ERR
  if [[ "$rollback_started" == true ]]; then
    release_job_update "$JOB_ID" rolling_back "${failure_message:-rollback failed}; restoring pre-rollback release" "$(jq -n --arg cause "${failure_code:-ROLLBACK_APPLY_FAILED}" '{cause_error_code:$cause,production_changed:true,rollback:{attempted:true,succeeded:false,message:"automatic restoration started"}}')" || true
    restore_base_runtime || restored=false
    [[ "$restored" != true ]] || ledger_restore_failed_rollback "$BASE_RELEASE_ID" "$BASE_CUSTOM_HIGH_WATER" "$JOB_ID" "$BASE_PROJECTION" || restored=false
    if [[ "$restored" == true ]]; then
      release_job_update "$JOB_ID" failed_rolled_back "${failure_message:-rollback failed}; pre-rollback release restored" "$(jq -n --arg cause "${failure_code:-ROLLBACK_APPLY_FAILED}" --arg artifact "$BACKUP_DIR" '{error_code:"ROLLBACK_FAILED_RESTORED",cause_error_code:$cause,artifact_path:$artifact,published:false,production_changed:false,rollback:{attempted:true,succeeded:true,message:"automatic restoration completed"}}')" || true
    else
      release_job_update "$JOB_ID" rollback_failed "${failure_message:-rollback failed}; automatic restoration failed" "$(jq -n --arg cause "${failure_code:-ROLLBACK_APPLY_FAILED}" --arg artifact "$BACKUP_DIR" --argjson diagnostics "${DATA_QUALITY_DIAGNOSTICS:-{}}" '{error_code:"ROLLBACK_RESTORATION_FAILED",cause_error_code:$cause,artifact_path:$artifact,published:false,production_changed:true,health_diagnostics:(if ($diagnostics|length)>0 then {data_quality:$diagnostics} else {} end),rollback:{attempted:true,succeeded:false,message:"automatic restoration failed; inspect fresh backup"}}')" || true
    fi
  fi
  exit "$code"
}
abort_apply() { failure_message="$1"; failure_code="$2"; rollback_on_error 1; }
trap 'failure_message="unexpected rollback apply error at line $LINENO"; failure_code=UNEXPECTED_ROLLBACK_APPLY_ERROR; rollback_on_error 1' ERR

release_job_update "$JOB_ID" validating_manifest 'Prepared rollback manifest and ledger revalidated' '{}'
rollback_started=true
release_checkout_exact_commit "$TARGET_COMMIT" >> "$LOG" 2>&1 || abort_apply 'historical source checkout failed' TARGET_CHECKOUT_FAILED
release_install_snapshot_artifacts "$TARGET_DIR" || abort_apply 'historical Compose/environment installation failed' TARGET_INSTALL_FAILED
release_job_update "$JOB_ID" switching_extensions "Switching extensions to historical digest $EXTENSIONS_DIGEST" '{}'
SUB2API_IMAGE="$MAIN_REPOSITORY@$MAIN_DIGEST" EXTENSIONS_SELF_IMAGE="$EXTENSIONS_REPOSITORY@$EXTENSIONS_DIGEST" docker compose --project-name deploy -f "$COMPOSE_BASE" -f "$COMPOSE_CUSTOM" --env-file "$ENV_FILE" up -d --pull never --no-deps --force-recreate extensions-self >> "$LOG" 2>&1 || abort_apply 'historical extensions switch failed' EXTENSIONS_SWITCH_FAILED
wait_container_healthy extensions-self || abort_apply 'historical extensions health failed' EXTENSIONS_HEALTH_FAILED
release_job_update "$JOB_ID" switching_main "Switching main application to historical digest $MAIN_DIGEST" '{}'
SUB2API_IMAGE="$MAIN_REPOSITORY@$MAIN_DIGEST" EXTENSIONS_SELF_IMAGE="$EXTENSIONS_REPOSITORY@$EXTENSIONS_DIGEST" docker compose --project-name deploy -f "$COMPOSE_BASE" -f "$COMPOSE_CUSTOM" --env-file "$ENV_FILE" up -d --pull never --no-deps --force-recreate sub2api >> "$LOG" 2>&1 || abort_apply 'historical main switch failed' MAIN_SWITCH_FAILED
wait_container_healthy sub2api || abort_apply 'historical main health failed' MAIN_HEALTH_FAILED
release_job_update "$JOB_ID" health_checking 'Checking complete health after snapshot rollback' '{}'
run_complete_health "$TARGET_DIR/rendered-compose.json" || abort_apply 'historical release failed complete health' HEALTH_CHECK_FAILED
release_running_container_matches_image extensions-self "$EXTENSIONS_REPOSITORY@$EXTENSIONS_DIGEST" || abort_apply 'historical extensions runtime identity drifted' RUNTIME_IMAGE_DRIFT
release_running_container_matches_image sub2api "$MAIN_REPOSITORY@$MAIN_DIGEST" || abort_apply 'historical main runtime identity drifted' RUNTIME_IMAGE_DRIFT
release_attach_source_branch "$TARGET_COMMIT" "$BRANCH" >> "$LOG" 2>&1 || abort_apply 'historical source could not be attached to the production branch' SOURCE_BRANCH_ATTACH_FAILED
if ! ledger_commit_rollback "$TARGET_RELEASE_ID" "$JOB_ID"; then
  # A metadata failpoint can leave the target projection durable while state is
  # still on the base release. Retry the idempotent commit before restoring
  # runtime; the ledger lock and operation preconditions make this safe.
  if ! ledger_commit_rollback "$TARGET_RELEASE_ID" "$JOB_ID"; then
    TARGET_PROJECTION="$(ledger_projection_for_release "$TARGET_RECORD")" || abort_apply 'target projection construction failed' LEDGER_COMMIT_FAILED
    ledger_recover_or_refuse "$TARGET_RECORD" "$TARGET_PROJECTION" "$BASE_CUSTOM_HIGH_WATER" "$JOB_ID" || abort_apply 'rollback ledger commit failed' LEDGER_COMMIT_FAILED
  fi
fi
trap - ERR
