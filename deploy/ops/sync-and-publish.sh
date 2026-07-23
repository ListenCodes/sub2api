#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DATA_DIR="${SUB2API_DATA_DIR:-/var/lib/docker/volumes/deploy_sub2api_data/_data}"
STATE_HELPER="${SUB2API_RELEASE_STATE_HELPER:-$SCRIPT_DIR/release-state.sh}"
PREPARE_SCRIPT="${SUB2API_PREPARE_SCRIPT:-$SCRIPT_DIR/prepare-release.sh}"
APPLY_SCRIPT="${SUB2API_APPLY_SCRIPT:-$SCRIPT_DIR/apply-release.sh}"
PREPARE_ROLLBACK_SCRIPT="${SUB2API_PREPARE_ROLLBACK_SCRIPT:-$SCRIPT_DIR/prepare-rollback.sh}"
APPLY_ROLLBACK_SCRIPT="${SUB2API_APPLY_ROLLBACK_SCRIPT:-$SCRIPT_DIR/apply-rollback.sh}"
LOCK_FILE="${SUB2API_SYNC_PUBLISH_LOCK:-/var/lock/sub2api-release.lock}"
LOG="${SUB2API_SYNC_PUBLISH_LOG:-/var/log/sub2api-release.log}"
TRIGGER_FILE="$DATA_DIR/release-trigger"
TRIGGER_CLAIM=""

source "$STATE_HELPER"
mkdir -p "$DATA_DIR" "$(dirname "$LOCK_FILE")" "$(dirname "$LOG")"
touch "$LOG" "$LOCK_FILE"

log() { printf '[%s] %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$1" | tee -a "$LOG"; }

claim_job() {
  if [[ -n "${SUB2API_JOB_ID:-}" ]]; then
    JOB_ID="$SUB2API_JOB_ID"
    ACTION="$(jq -r '.action // empty' "$(release_job_path "$JOB_ID")" 2>/dev/null || true)"
    return
  fi
  [[ -s "$TRIGGER_FILE" ]] || return 1
  TRIGGER_CLAIM="$TRIGGER_FILE.processing.$$"
  mv "$TRIGGER_FILE" "$TRIGGER_CLAIM"
  read -r first second _ < "$TRIGGER_CLAIM"
  if [[ "$first" == prepare || "$first" == apply ]]; then
    ACTION="$first"
    JOB_ID="$second"
  else
    ACTION="legacy"
    JOB_ID="$first"
  fi
}

cleanup() { [[ -z "$TRIGGER_CLAIM" ]] || rm -f "$TRIGGER_CLAIM"; }
trap cleanup EXIT

exec 9>"$LOCK_FILE"
if ! flock -n 9; then
  log 'Another release pipeline is running; leaving the durable trigger pending'
  exit 0
fi
claim_job || { log 'No durable release trigger is pending'; exit 0; }
release_valid_job_id "$JOB_ID" || { log 'Invalid release job id'; exit 1; }
JOB_FILE="$(release_job_path "$JOB_ID")"
if [[ ! -r "$JOB_FILE" ]]; then
  if [[ -r "$(release_legacy_job_path "$JOB_ID")" ]]; then
    log "LEGACY_SINGLE_PHASE_UNSUPPORTED: legacy release job $JOB_ID cannot be resumed"
  else
    log 'Release job file is missing'
  fi
  exit 1
fi
JOB_ACTION="$(jq -r '.action // empty' "$JOB_FILE")"
OPERATION_KIND="$(jq -r '.operation_kind // empty' "$JOB_FILE")"
[[ "$OPERATION_KIND" == update || "$OPERATION_KIND" == rollback ]] || {
  release_job_update "$JOB_ID" failed 'Release operation kind is invalid' '{"error_code":"INVALID_OPERATION_KIND","production_changed":false}' || true
  exit 1
}
[[ "$JOB_ID" == "$OPERATION_KIND-"* ]] || {
  release_job_update "$JOB_ID" failed 'Release operation kind does not match its id' '{"error_code":"OPERATION_KIND_MISMATCH","production_changed":false}' || true
  exit 1
}
if [[ "$ACTION" == legacy || -z "$JOB_ACTION" ]]; then
  release_job_update "$JOB_ID" failed 'Legacy single-phase release job rejected; prepare a new update' '{"error_code":"LEGACY_SINGLE_PHASE_UNSUPPORTED","production_changed":false}' || true
  log "Rejected legacy release job $JOB_ID"
  exit 1
fi
[[ "$ACTION" == "$JOB_ACTION" ]] || {
  release_job_update "$JOB_ID" failed 'Release action does not match its durable job' '{"error_code":"ACTION_MISMATCH","production_changed":false}' || true
  exit 1
}
export SUB2API_JOB_ID="$JOB_ID"
status="$(jq -r '.status // empty' "$JOB_FILE")"
if release_terminal_status "$status"; then
  release_reconcile_active_operation "$JOB_ID" "$status" || true
  log "Release operation $JOB_ID is already terminal: $status"
  exit 0
fi

case "$OPERATION_KIND:$ACTION" in
  update:prepare) EXECUTOR="$PREPARE_SCRIPT" ;;
  update:apply) EXECUTOR="$APPLY_SCRIPT" ;;
  rollback:prepare) EXECUTOR="$PREPARE_ROLLBACK_SCRIPT" ;;
  rollback:apply) EXECUTOR="$APPLY_ROLLBACK_SCRIPT" ;;
  *)
    release_job_update "$JOB_ID" failed 'Unknown release operation/action pair' '{"error_code":"INVALID_RELEASE_ACTION","production_changed":false}' || true
    exit 1
    ;;
esac

if [[ "$ACTION" == apply ]]; then
    [[ "$status" == apply_queued || "$status" == prepared ]] || {
      release_job_update "$JOB_ID" failed 'Apply requires a prepared job' '{"error_code":"APPLY_REQUIRES_PREPARED","production_changed":false}' || true
      exit 1
    }
fi
[[ -x "$EXECUTOR" ]] || { release_job_update "$JOB_ID" failed 'Release executor is missing' '{"error_code":"RELEASE_EXECUTOR_MISSING"}'; exit 1; }
log "Starting $OPERATION_KIND $ACTION phase for administrator job $JOB_ID"
exec "$EXECUTOR" --job-id "$JOB_ID"
