#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DATA_DIR="${SUB2API_DATA_DIR:-/var/lib/docker/volumes/deploy_sub2api_data/_data}"
STATE_HELPER="${SUB2API_RELEASE_STATE_HELPER:-$SCRIPT_DIR/release-state.sh}"
PREPARE_SCRIPT="${SUB2API_PREPARE_SCRIPT:-$SCRIPT_DIR/prepare-release.sh}"
APPLY_SCRIPT="${SUB2API_APPLY_SCRIPT:-$SCRIPT_DIR/apply-release.sh}"
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
[[ -r "$JOB_FILE" ]] || { log 'Release job file is missing'; exit 1; }
JOB_ACTION="$(jq -r '.action // empty' "$JOB_FILE")"
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

case "$ACTION" in
  prepare)
    [[ -x "$PREPARE_SCRIPT" ]] || { release_job_update "$JOB_ID" failed 'Prepare executor is missing' '{"error_code":"PREPARE_EXECUTOR_MISSING"}'; exit 1; }
    log "Starting prepare phase for administrator job $JOB_ID"
    exec "$PREPARE_SCRIPT" --job-id "$JOB_ID"
    ;;
  apply)
    [[ -x "$APPLY_SCRIPT" ]] || { release_job_update "$JOB_ID" failed 'Apply executor is missing' '{"error_code":"APPLY_EXECUTOR_MISSING"}'; exit 1; }
    status="$(jq -r '.status // empty' "$JOB_FILE")"
    [[ "$status" == apply_queued || "$status" == prepared ]] || {
      [[ "$status" == success ]] && exit 0
      release_job_update "$JOB_ID" failed 'Apply requires a prepared job' '{"error_code":"APPLY_REQUIRES_PREPARED","production_changed":false}' || true
      exit 1
    }
    log "Starting apply phase for administrator job $JOB_ID"
    exec "$APPLY_SCRIPT" --job-id "$JOB_ID"
    ;;
  *)
    release_job_update "$JOB_ID" failed 'Unknown release action' '{"error_code":"INVALID_RELEASE_ACTION","production_changed":false}' || true
    exit 1
    ;;
esac
