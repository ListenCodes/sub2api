#!/usr/bin/env bash
set -Eeuo pipefail

REPO="${SUB2API_REPO:-/root/sub2api}"
BRANCH="${SUB2API_BRANCH:-custom}"
UPSTREAM_REMOTE="${SUB2API_UPSTREAM_REMOTE:-upstream}"
ORIGIN_REMOTE="${SUB2API_ORIGIN_REMOTE:-origin}"
DATA_DIR="${SUB2API_DATA_DIR:-/var/lib/docker/volumes/deploy_sub2api_data/_data}"
LOG="${SUB2API_SYNC_LOG:-/var/log/sub2api-sync.log}"
LOCK_FILE="${SUB2API_SYNC_LOCK:-/var/lock/sub2api-sync.lock}"
WORKTREE_ROOT="${SUB2API_SYNC_WORKTREE_ROOT:-/var/tmp/sub2api-sync}"
CONFLICT_DIR="${SUB2API_SYNC_CONFLICT_DIR:-$DATA_DIR/sync-conflicts}"
STATUS_FILE="$DATA_DIR/sync-status"
RESULT_FILE="$DATA_DIR/sync-result"
JOB_ID_FILE="$DATA_DIR/sync-job-id"
DEFER_RESULT="${SUB2API_SYNC_DEFER_RESULT:-0}"
SCHEDULED_RUN="${1:-}"

mkdir -p "$DATA_DIR" "$WORKTREE_ROOT" "$(dirname "$LOG")" "$(dirname "$LOCK_FILE")"
touch "$LOG" "$LOCK_FILE"

log() {
  printf '[%s] %s\n' "$(date '+%Y-%m-%d %H:%M:%S')" "$1" | tee -a "$LOG"
}

if [[ "$SCHEDULED_RUN" == "--scheduled" ]]; then
  JOB_ID="scheduled-$(date -u +%Y%m%d-%H%M%S)-$$"
  STARTED_AT="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
else
  JOB_ID="$(cat "$JOB_ID_FILE" 2>/dev/null || true)"
  if [[ -z "$JOB_ID" ]]; then
    JOB_ID="sync-$(date -u +%Y%m%d-%H%M%S)-$$"
    printf '%s\n' "$JOB_ID" > "$JOB_ID_FILE"
  fi
  STARTED_AT="$(jq -r '.started_at // empty' "$STATUS_FILE" 2>/dev/null || true)"
  [[ -n "$STARTED_AT" ]] || STARTED_AT="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
fi
INTEGRATION_BRANCH=""
BASE_COMMIT=""
WORKTREE=""
CONFLICT_FILES_JSON='[]'
CONFLICT_BASE=""
CONFLICT_UPSTREAM=""
CONFLICT_LOG=""
RESOLUTION_HINT=""

write_status() {
  local state="$1"
  local message="$2"
  local finished_at="null"
  if [[ "$state" != "running" ]]; then
    finished_at="\"$(date -u '+%Y-%m-%dT%H:%M:%SZ')\""
  fi
  jq -n \
    --arg job_id "$JOB_ID" \
    --arg status "$state" \
    --arg message "$message" \
    --arg ts "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" \
    --arg started_at "$STARTED_AT" \
    --arg integration_branch "$INTEGRATION_BRANCH" \
    --arg base_commit "$BASE_COMMIT" \
    --arg conflict_base "$CONFLICT_BASE" \
    --arg conflict_upstream "$CONFLICT_UPSTREAM" \
    --arg conflict_log "$CONFLICT_LOG" \
    --arg resolution_hint "$RESOLUTION_HINT" \
    --argjson conflict_files "$CONFLICT_FILES_JSON" \
    --argjson finished_at "$finished_at" \
    '{job_id:$job_id,status:$status,message:$message,ts:$ts,started_at:$started_at,finished_at:$finished_at,integration_branch:$integration_branch,base_commit:$base_commit,conflict_files:$conflict_files,conflict_base:$conflict_base,conflict_upstream:$conflict_upstream,conflict_log:$conflict_log,resolution_hint:$resolution_hint,need_restart:false,published:false,published_commit:""}' \
    > "$STATUS_FILE.tmp.$$"
  mv -f "$STATUS_FILE.tmp.$$" "$STATUS_FILE"
}

cleanup() {
  if [[ -n "$WORKTREE" && -d "$WORKTREE" ]]; then
    git -C "$REPO" worktree remove "$WORKTREE" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

result() {
  local message="$1"
  local code="${2:-0}"
  if [[ "$code" -eq 0 ]]; then
    write_status success "$message"
  else
    write_status failed "$message"
  fi
  if [[ "$DEFER_RESULT" != 1 ]]; then
    printf '%s\n' "$message" > "$RESULT_FILE"
  fi
  log "RESULT: $message"
  exit "$code"
}

exec 9>"$LOCK_FILE"
if ! flock -n 9; then
  result 'FAILED: another upstream sync is already running' 1
fi

cd "$REPO"
if [[ "$(git branch --show-current)" != "$BRANCH" ]]; then
  result "FAILED: VPS source branch is not $BRANCH" 1
fi
if [[ -n "$(git status --porcelain --untracked-files=all)" ]]; then
  result 'FAILED: VPS source worktree is dirty; no changes were stashed' 1
fi

write_status running 'fetching upstream and approved fork'
log "Sync check started for $BRANCH"
git fetch "$UPSTREAM_REMOTE" main >> "$LOG" 2>&1 || result 'FAILED: fetch upstream/main failed' 1
git fetch "$ORIGIN_REMOTE" "$BRANCH" >> "$LOG" 2>&1 || result 'FAILED: fetch origin/custom failed' 1

local_head="$(git rev-parse HEAD)"
origin_head="$(git rev-parse "$ORIGIN_REMOTE/$BRANCH")"
upstream_head="$(git rev-parse "$UPSTREAM_REMOTE/main")"
BASE_COMMIT="$origin_head"
if [[ "$local_head" != "$origin_head" ]]; then
  result "FAILED: VPS custom $local_head differs from origin/custom $origin_head" 1
fi
if git merge-base --is-ancestor "$upstream_head" "$local_head"; then
  result "SUCCESS: origin/custom already contains upstream/main at ${upstream_head:0:12}" 0
fi

INTEGRATION_BRANCH="integration/upstream-$(date -u '+%Y%m%d-%H%M%S')-$RANDOM"
WORKTREE="$WORKTREE_ROOT/$JOB_ID"
mkdir -p "$WORKTREE"
rmdir "$WORKTREE" 2>/dev/null || true
git worktree add --detach "$WORKTREE" "$BRANCH" >> "$LOG" 2>&1 || result 'FAILED: create temporary integration worktree failed' 1
git -C "$WORKTREE" switch -c "$INTEGRATION_BRANCH" >> "$LOG" 2>&1 || result 'FAILED: create integration branch failed' 1

write_status running "checking merge into $INTEGRATION_BRANCH"
if ! git -C "$WORKTREE" merge --no-ff --no-edit "$UPSTREAM_REMOTE/main" >> "$LOG" 2>&1; then
  conflict_snapshot_dir="$CONFLICT_DIR/$JOB_ID"
  mkdir -p "$conflict_snapshot_dir"
  CONFLICT_FILES_JSON="$(git -C "$WORKTREE" diff --name-only --diff-filter=U | jq -R -s 'split("\n") | map(select(length > 0))')"
  conflict_files="$(jq -r 'join(", ")' <<< "$CONFLICT_FILES_JSON")"
  CONFLICT_BASE="$BASE_COMMIT"
  CONFLICT_UPSTREAM="$upstream_head"
  CONFLICT_LOG="$conflict_snapshot_dir/metadata.json"
  RESOLUTION_HINT='Resolve the listed files in a local custom worktree, merge upstream/main, run tests, push origin/custom, then retry.'
  git -C "$WORKTREE" status --short > "$conflict_snapshot_dir/status.txt" || true
  git -C "$WORKTREE" diff --cc > "$conflict_snapshot_dir/conflict.diff" || true
  git -C "$WORKTREE" ls-files -u > "$conflict_snapshot_dir/unmerged-stages.txt" || true
  jq -n \
    --arg job_id "$JOB_ID" \
    --arg integration_branch "$INTEGRATION_BRANCH" \
    --arg base_commit "$CONFLICT_BASE" \
    --arg upstream_commit "$CONFLICT_UPSTREAM" \
    --arg log "$CONFLICT_LOG" \
    --arg artifact_path "$conflict_snapshot_dir/metadata.json" \
    --arg resolution_hint "$RESOLUTION_HINT" \
    --argjson files "$CONFLICT_FILES_JSON" \
    '{job_id:$job_id,integration_branch:$integration_branch,base_commit:$base_commit,upstream_commit:$upstream_commit,conflict_files:$files,conflict_log:$log,artifact_path:$artifact_path,resolution_hint:$resolution_hint}' \
    > "$conflict_snapshot_dir/metadata.json"
  git -C "$WORKTREE" merge --abort >> "$LOG" 2>&1 || true
  if [[ -n "$conflict_files" ]]; then
    result "FAILED: upstream merge conflict; files: $conflict_files" 1
  fi
  result 'FAILED: upstream merge failed; inspect sync log' 1
fi

git -C "$WORKTREE" push "$ORIGIN_REMOTE" "HEAD:$INTEGRATION_BRANCH" >> "$LOG" 2>&1 || result "FAILED: push $INTEGRATION_BRANCH to origin failed" 1
result "SUCCESS: created origin/$INTEGRATION_BRANCH; review locally before merging custom" 0
