#!/usr/bin/env bash
set -Eeuo pipefail

REPO="${SUB2API_REPO:-/root/sub2api}"
SYNC_SCRIPT="${SUB2API_SYNC_SCRIPT:-/opt/sub2api-custom/sync-upstream.sh}"
PUBLISH_SCRIPT="${SUB2API_PUBLISH_SCRIPT:-/opt/sub2api-custom/publish-custom.sh}"
DATA_DIR="${SUB2API_DATA_DIR:-/var/lib/docker/volumes/deploy_sub2api_data/_data}"
STATUS_FILE="$DATA_DIR/sync-status"
RESULT_FILE="$DATA_DIR/sync-result"
JOB_ID_FILE="$DATA_DIR/sync-job-id"
PENDING_FILE="$DATA_DIR/sync-pending-publish"
LOCK_FILE="${SUB2API_SYNC_PUBLISH_LOCK:-/var/lock/sub2api-sync-publish.lock}"
LOG="${SUB2API_SYNC_PUBLISH_LOG:-/var/log/sub2api-sync-publish.log}"

mkdir -p "$DATA_DIR" "$(dirname "$LOCK_FILE")" "$(dirname "$LOG")"
touch "$LOG"
rm -f "$RESULT_FILE"

log() {
  printf '[%s] %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$1" | tee -a "$LOG"
}

prepare_scheduled_status() {
  [[ "${1:-}" == "--scheduled" ]] || return 0

  local job_id="scheduled-$(date -u +%Y%m%d-%H%M%S)-$$"
  local now="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
  printf '%s\n' "$job_id" > "$JOB_ID_FILE"
  jq -n \
    --arg job_id "$job_id" \
    --arg now "$now" \
    '{job_id:$job_id,status:"running",message:"scheduled sync starting",ts:$now,started_at:$now,finished_at:null,integration_branch:"",base_commit:"",need_restart:false,published:false,published_commit:""}' \
    > "$STATUS_FILE.tmp.$$"
  mv -f "$STATUS_FILE.tmp.$$" "$STATUS_FILE"
}

write_result() {
  printf '%s\n' "$1" > "$RESULT_FILE"
}

update_status() {
  local state="$1"
  local message="$2"
  local published="$3"
  local published_commit="$4"
  local publish_status="$5"

  if [[ ! -f "$STATUS_FILE" ]]; then
    return 0
  fi

  jq \
    --arg status "$state" \
    --arg message "$message" \
    --arg published_commit "$published_commit" \
    --arg publish_status "$publish_status" \
    --arg ts "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" \
    --argjson published "$published" \
    '.status=$status | .message=$message | .ts=$ts | .finished_at=$ts | .published=$published | .published_commit=$published_commit | .publish_status=$publish_status' \
    "$STATUS_FILE" > "$STATUS_FILE.tmp.$$"
  mv -f "$STATUS_FILE.tmp.$$" "$STATUS_FILE"
}

publish_commit() {
  local approved_commit="$1"
  local success_message="$2"

  printf '%s\n' "$approved_commit" > "$PENDING_FILE"
  log "Publishing approved commit $approved_commit"
  if ! "$PUBLISH_SCRIPT" --commit "$approved_commit" >> "$LOG" 2>&1; then
    fail_run "FAILED: automatic publish failed for commit $approved_commit; inspect $LOG"
  fi

  rm -f "$PENDING_FILE"
  update_status success "$success_message" true "$approved_commit" published
  write_result "$success_message"
  log "$success_message"
}

retry_pending_publish() {
  local pending_commit
  pending_commit="$(tr -d '[:space:]' < "$PENDING_FILE")"
  [[ -n "$pending_commit" ]] || { rm -f "$PENDING_FILE"; return 1; }

  cd "$REPO"
  [[ "$(git branch --show-current)" == custom ]] || fail_run 'FAILED: VPS source branch is not custom'
  [[ -z "$(git status --porcelain --untracked-files=all)" ]] || fail_run 'FAILED: VPS source worktree is dirty; pending publish retained'
  git fetch origin custom >> "$LOG" 2>&1 || fail_run 'FAILED: fetch origin/custom for pending publish'
  origin_custom="$(git rev-parse origin/custom)"
  [[ "$pending_commit" == "$origin_custom" ]] || fail_run "FAILED: pending publish commit $pending_commit is not origin/custom $origin_custom"
  [[ "$(git rev-parse HEAD)" == "$origin_custom" ]] || fail_run 'FAILED: local custom is not aligned with origin/custom for pending publish'

  update_status running "retrying pending publish $pending_commit" false "" retrying
  publish_commit "$pending_commit" "PUBLISH OK: commit=$pending_commit pending-retry=true"
  return 0
}

fail_run() {
  local message="$1"
  update_status failed "$message" false "" failed
  write_result "$message"
  log "$message"
  exit 1
}

exec 9>"$LOCK_FILE"
if ! flock -n 9; then
  fail_run 'FAILED: another sync-and-publish run is already running'
fi

prepare_scheduled_status "$@"

[[ -x "$SYNC_SCRIPT" ]] || fail_run "FAILED: sync script not executable: $SYNC_SCRIPT"
[[ -x "$PUBLISH_SCRIPT" ]] || fail_run "FAILED: publish script not executable: $PUBLISH_SCRIPT"

if [[ -s "$PENDING_FILE" ]]; then
  retry_pending_publish
  exit 0
fi

log 'Starting conflict-gated upstream sync and publish'
set +e
SUB2API_SYNC_DEFER_RESULT=1 "$SYNC_SCRIPT" "$@"
sync_exit=$?
set -e

if [[ "$sync_exit" -ne 0 ]]; then
  message="$(jq -r '.message // "FAILED: upstream sync failed"' "$STATUS_FILE" 2>/dev/null || printf '%s' 'FAILED: upstream sync failed')"
  write_result "$message"
  log "$message"
  exit "$sync_exit"
fi

integration_branch="$(jq -r '.integration_branch // empty' "$STATUS_FILE")"
base_commit="$(jq -r '.base_commit // empty' "$STATUS_FILE")"
sync_message="$(jq -r '.message // "upstream sync completed"' "$STATUS_FILE")"

if [[ -z "$integration_branch" ]]; then
  write_result "$sync_message"
  log "$sync_message"
  exit 0
fi

cd "$REPO"
[[ "$(git branch --show-current)" == custom ]] || fail_run 'FAILED: VPS source branch is not custom'
[[ -z "$(git status --porcelain --untracked-files=all)" ]] || fail_run 'FAILED: VPS source worktree is dirty; no promotion performed'

git fetch origin custom "$integration_branch" >> "$LOG" 2>&1 || fail_run 'FAILED: fetch integration branch for promotion'
origin_custom="$(git rev-parse origin/custom)"
integration_ref="origin/$integration_branch"
integration_head="$(git rev-parse "$integration_ref")"

[[ -n "$base_commit" && "$base_commit" == "$origin_custom" ]] || fail_run "FAILED: origin/custom changed since integration base (base=$base_commit current=$origin_custom)"
git merge-base --is-ancestor "$origin_custom" "$integration_head" || fail_run 'FAILED: integration branch is not based on current origin/custom'
[[ "$(git rev-parse HEAD)" == "$origin_custom" ]] || fail_run 'FAILED: local custom is not aligned with origin/custom'

log "Promoting clean integration $integration_branch"
git merge --ff-only "$integration_ref" >> "$LOG" 2>&1 || fail_run 'FAILED: integration promotion was not fast-forwardable'
git push origin custom >> "$LOG" 2>&1 || fail_run 'FAILED: push promoted origin/custom failed'
approved_commit="$(git rev-parse HEAD)"
publish_commit "$approved_commit" "PUBLISH OK: commit=$approved_commit integration=$integration_branch"
