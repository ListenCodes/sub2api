#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DATA_DIR="${SUB2API_DATA_DIR:-/var/lib/docker/volumes/deploy_sub2api_data/_data}"
STATE_HELPER="${SUB2API_RELEASE_STATE_HELPER:-$SCRIPT_DIR/release-state.sh}"
SYNC_SCRIPT="${SUB2API_SYNC_SCRIPT:-$SCRIPT_DIR/sync-upstream.sh}"
WAIT_ACTIONS_SCRIPT="${SUB2API_WAIT_ACTIONS_SCRIPT:-$SCRIPT_DIR/wait-for-actions.sh}"
VERIFY_IMAGES_SCRIPT="${SUB2API_VERIFY_IMAGES_SCRIPT:-$SCRIPT_DIR/verify-release-images.sh}"
PROMOTE_SCRIPT="${SUB2API_PROMOTE_SCRIPT:-$SCRIPT_DIR/promote-release.sh}"
PUBLISH_SCRIPT="${SUB2API_PUBLISH_SCRIPT:-$SCRIPT_DIR/publish-custom.sh}"
LOCK_FILE="${SUB2API_SYNC_PUBLISH_LOCK:-/var/lock/sub2api-release.lock}"
LOG="${SUB2API_SYNC_PUBLISH_LOG:-/var/log/sub2api-release.log}"
TRIGGER_FILE="$DATA_DIR/release-trigger"
TRIGGER_CLAIM=""

source "$STATE_HELPER"

mkdir -p "$DATA_DIR" "$(dirname "$LOCK_FILE")" "$(dirname "$LOG")"
touch "$LOG" "$LOCK_FILE"

log() {
  printf '[%s] %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$1" | tee -a "$LOG"
}

claim_job() {
  if [[ -n "${SUB2API_JOB_ID:-}" ]]; then
    JOB_ID="$SUB2API_JOB_ID"
    return
  fi
  [[ -s "$TRIGGER_FILE" ]] || return 1
  TRIGGER_CLAIM="$TRIGGER_FILE.processing.$$"
  mv "$TRIGGER_FILE" "$TRIGGER_CLAIM"
  JOB_ID="$(tr -d '[:space:]' < "$TRIGGER_CLAIM")"
}

fail_run() {
  local message="$1"
  local error_code="${2:-RELEASE_PIPELINE_FAILED}"
  trap - ERR
  if release_job_path "$JOB_ID" >/dev/null 2>&1; then
    release_job_update "$JOB_ID" failed "$message" "$(jq -n --arg error_code "$error_code" '{error_code:$error_code}')" || true
  fi
  log "FAILED: $message"
  exit 1
}

on_error() {
  local code="$?"
  local line="${1:-unknown}"
  fail_run "unexpected release pipeline error at line $line (exit=$code)" UNEXPECTED_PIPELINE_ERROR
}

parse_actions_output() {
  local output="$1"
  [[ "$output" == workflow_url=* && "$output" != *$'\n'* ]] || return 1
  WORKFLOW_URL="${output#workflow_url=}"
  [[ -n "$WORKFLOW_URL" ]]
}

parse_images_output() {
  local output="$1"
  local line key value
  local seen_main=0 seen_extensions=0
  MAIN_DIGEST=""
  EXTENSIONS_DIGEST=""
  while IFS= read -r line; do
    [[ "$line" == *=* ]] || return 1
    key="${line%%=*}"
    value="${line#*=}"
    case "$key" in
      main_digest)
        ((seen_main == 0)) || return 1
        MAIN_DIGEST="$value"
        seen_main=1
        ;;
      extensions_digest)
        ((seen_extensions == 0)) || return 1
        EXTENSIONS_DIGEST="$value"
        seen_extensions=1
        ;;
      *) return 1 ;;
    esac
  done <<< "$output"
  [[ "$seen_main" -eq 1 && "$seen_extensions" -eq 1 ]]
  [[ "$MAIN_DIGEST" =~ ^sha256:[0-9a-f]{64}$ ]]
  [[ "$EXTENSIONS_DIGEST" =~ ^sha256:[0-9a-f]{64}$ ]]
}

cleanup() {
  [[ -z "$TRIGGER_CLAIM" ]] || rm -f "$TRIGGER_CLAIM"
}

trap cleanup EXIT
claim_job || { log 'No durable release trigger is pending'; exit 0; }
release_valid_job_id "$JOB_ID" || { log 'Invalid release job id'; exit 1; }
export SUB2API_JOB_ID="$JOB_ID"

trap 'on_error "$LINENO"' ERR
exec 9>"$LOCK_FILE"
flock -n 9 || fail_run 'another release pipeline is already running' RELEASE_LOCKED

for script in "$SYNC_SCRIPT" "$WAIT_ACTIONS_SCRIPT" "$VERIFY_IMAGES_SCRIPT" "$PROMOTE_SCRIPT" "$PUBLISH_SCRIPT"; do
  [[ -x "$script" ]] || fail_run "required release script is not executable: $script" RELEASE_SCRIPT_MISSING
done

job_file="$(release_job_path "$JOB_ID")" || fail_run 'release job id is invalid' INVALID_JOB_ID
[[ -r "$job_file" ]] || fail_run 'release job file is missing' RELEASE_JOB_NOT_FOUND

log "Starting administrator release job $JOB_ID"
if ! "$SYNC_SCRIPT" --job-id "$JOB_ID" >> "$LOG" 2>&1; then
  current_status="$(jq -r '.status // empty' "$job_file" 2>/dev/null || true)"
  if [[ "$current_status" == conflict || "$current_status" == failed ]]; then
    log "Release preparation stopped with status $current_status"
    exit 1
  fi
  fail_run 'stable Release preparation failed' RELEASE_PREPARATION_FAILED
fi

BASE_COMMIT="$(jq -r '.base_commit // empty' "$job_file")"
TARGET_COMMIT="$(jq -r '.target_commit // empty' "$job_file")"
RELEASE_TAG="$(jq -r '.release_tag // empty' "$job_file")"
RELEASE_COMMIT="$(jq -r '.release_commit // empty' "$job_file")"
INTEGRATION_BRANCH="$(jq -r '.integration_branch // empty' "$job_file")"
[[ "$BASE_COMMIT" =~ ^[0-9a-f]{40}$ ]] || fail_run 'Release preparation returned an invalid base commit' INVALID_BASE_COMMIT
[[ "$TARGET_COMMIT" =~ ^[0-9a-f]{40}$ ]] || fail_run 'Release preparation returned an invalid target commit' INVALID_TARGET_COMMIT
[[ "$RELEASE_TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail_run 'Release preparation returned an invalid stable tag' INVALID_RELEASE_TAG
[[ "$RELEASE_COMMIT" =~ ^[0-9a-f]{40}$ ]] || fail_run 'Release preparation returned an invalid stable commit' INVALID_RELEASE_COMMIT

production_commit="$(jq -r '.production_commit // empty' "$PRODUCTION_RELEASE_STATE_FILE" 2>/dev/null || true)"
if [[ "$TARGET_COMMIT" == "$production_commit" ]]; then
  message="Already current: commit=$TARGET_COMMIT release=$RELEASE_TAG"
  release_job_update "$JOB_ID" success "$message" '{"published":false,"production_changed":false}'
  log "$message"
  exit 0
fi

release_job_update "$JOB_ID" waiting_actions "Waiting for Actions on $TARGET_COMMIT" '{}'
actions_output="$($WAIT_ACTIONS_SCRIPT "$TARGET_COMMIT")" || fail_run 'required GitHub Actions checks failed' ACTIONS_FAILED
parse_actions_output "$actions_output" || fail_run 'Actions waiter returned invalid evidence' ACTIONS_EVIDENCE_INVALID
release_job_update "$JOB_ID" waiting_images "Waiting for paired GHCR images for $TARGET_COMMIT" "$(jq -n --arg workflow_url "$WORKFLOW_URL" '{workflow_url:$workflow_url}')"

release_version="${RELEASE_TAG#v}"
images_output="$($VERIFY_IMAGES_SCRIPT "$TARGET_COMMIT" "$release_version")" || fail_run 'paired GHCR image verification failed' IMAGES_FAILED
parse_images_output "$images_output" || fail_run 'image verifier returned invalid digest evidence' IMAGE_EVIDENCE_INVALID
digest_metadata="$(jq -n --arg main "$MAIN_DIGEST" --arg extensions "$EXTENSIONS_DIGEST" '{main_digest:$main,extensions_digest:$extensions}')"
release_job_update "$JOB_ID" waiting_images 'Paired GHCR images verified' "$digest_metadata"

if [[ -n "$INTEGRATION_BRANCH" ]]; then
  release_job_update "$JOB_ID" promoting_release "Promoting $INTEGRATION_BRANCH" '{}'
  "$PROMOTE_SCRIPT" "$BASE_COMMIT" "$TARGET_COMMIT" "$INTEGRATION_BRANCH" >> "$LOG" 2>&1 \
    || fail_run 'origin/custom-release changed or candidate promotion failed' PROMOTION_FAILED
fi

log "Publishing commit $TARGET_COMMIT by immutable digest"
if ! "$PUBLISH_SCRIPT" \
  --commit "$TARGET_COMMIT" \
  --main-digest "$MAIN_DIGEST" \
  --extensions-digest "$EXTENSIONS_DIGEST" \
  >> "$LOG" 2>&1; then
  current_status="$(jq -r '.status // empty' "$job_file" 2>/dev/null || true)"
  if [[ "$current_status" == failed || "$current_status" == conflict ]]; then
    log "Publication stopped with persisted status $current_status; inspect the release job evidence"
    exit 1
  fi
  fail_run 'digest publication failed without terminal publisher evidence' PUBLICATION_FAILED
fi

if [[ "$(jq -r '.status // empty' "$job_file")" != success ]]; then
  release_job_update "$JOB_ID" success "Published commit $TARGET_COMMIT" "$(jq -n --arg commit "$TARGET_COMMIT" '{published:true,published_commit:$commit,production_changed:true}')"
fi
log "Release job $JOB_ID completed"
