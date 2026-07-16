#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="${SUB2API_REPO:-/root/sub2api}"
BRANCH="${SUB2API_BRANCH:-custom-release}"
UPSTREAM_REMOTE="${SUB2API_UPSTREAM_REMOTE:-upstream}"
ORIGIN_REMOTE="${SUB2API_ORIGIN_REMOTE:-origin}"
RESOLVER="${SUB2API_RELEASE_RESOLVER:-$SCRIPT_DIR/resolve-stable-release.sh}"
BASELINE_FILE="${SUB2API_STABLE_BASELINE_FILE:-$REPO/deploy/stable-release-baseline.json}"
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
RELEASE_TAG=""
RELEASE_COMMIT=""
RELEASE_TAG_OBJECT_SHA=""
RELEASE_PUBLISHED_AT=""
CONFLICT_FILES_JSON='[]'
CONFLICT_BASE=""
CONFLICT_UPSTREAM=""
CONFLICT_RELEASE=""
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
    --arg release_tag "$RELEASE_TAG" \
    --arg release_commit "$RELEASE_COMMIT" \
    --arg release_published_at "$RELEASE_PUBLISHED_AT" \
    --arg conflict_base "$CONFLICT_BASE" \
    --arg conflict_upstream "$CONFLICT_UPSTREAM" \
    --arg conflict_release "$CONFLICT_RELEASE" \
    --arg conflict_log "$CONFLICT_LOG" \
    --arg resolution_hint "$RESOLUTION_HINT" \
    --argjson conflict_files "$CONFLICT_FILES_JSON" \
    --argjson finished_at "$finished_at" \
    '{job_id:$job_id,status:$status,message:$message,ts:$ts,started_at:$started_at,finished_at:$finished_at,integration_branch:$integration_branch,base_commit:$base_commit,release_tag:$release_tag,release_commit:$release_commit,release_published_at:$release_published_at,conflict_files:$conflict_files,conflict_base:$conflict_base,conflict_upstream:$conflict_upstream,conflict_release:$conflict_release,conflict_log:$conflict_log,resolution_hint:$resolution_hint,need_restart:false,published:false,published_commit:""}' \
    > "$STATUS_FILE.tmp.$$"
  mv -f "$STATUS_FILE.tmp.$$" "$STATUS_FILE"
}

cleanup() {
  if [[ -n "$WORKTREE" && -d "$WORKTREE" ]]; then
    git -C "$REPO" worktree remove "$WORKTREE" >/dev/null 2>&1 || true
  fi
}

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

on_error() {
  local code="$?"
  local line="${1:-unknown}"
  trap - ERR
  set +e
  result "FAILED: unexpected stable Release sync error at line $line" "$code"
}

parse_release_output() {
  local output="$1"
  local line key value
  local seen_tag=0 seen_published=0 seen_sha=0
  while IFS= read -r line; do
    [[ -n "$line" ]] || continue
    [[ "$line" == *=* ]] || return 1
    key="${line%%=*}"
    value="${line#*=}"
    [[ -n "$value" ]] || return 1
    case "$key" in
      release_tag)
        ((seen_tag == 0)) || return 1
        RELEASE_TAG="$value"
        seen_tag=1
        ;;
      release_published_at)
        ((seen_published == 0)) || return 1
        RELEASE_PUBLISHED_AT="$value"
        seen_published=1
        ;;
      release_tag_object_sha)
        ((seen_sha == 0)) || return 1
        RELEASE_TAG_OBJECT_SHA="$value"
        seen_sha=1
        ;;
      *)
        return 1
        ;;
    esac
  done <<< "$output"
  [[ "$seen_tag" -eq 1 && "$seen_published" -eq 1 && "$seen_sha" -eq 1 ]]
}

trap cleanup EXIT
trap 'on_error "$LINENO"' ERR
mkdir -p "$DATA_DIR" "$WORKTREE_ROOT" "$(dirname "$LOG")" "$(dirname "$LOCK_FILE")"
touch "$LOG" "$LOCK_FILE"
[[ "$BRANCH" == custom-release ]] || result "FAILED: publication branch $BRANCH is not approved; expected custom-release" 1

exec 9>"$LOCK_FILE"
if ! flock -n 9; then
  result 'FAILED: another stable Release sync is already running' 1
fi

cd "$REPO"
repo_root="$(realpath -m "$REPO")"
baseline_path="$(realpath -m "$BASELINE_FILE")"
case "$baseline_path" in
  "$repo_root"/*)
    BASELINE_FILE="$baseline_path"
    BASELINE_RELATIVE="${BASELINE_FILE#"$repo_root"/}"
    ;;
  *)
    result 'FAILED: baseline metadata path must stay inside the repository' 1
    ;;
esac
if [[ "$(git branch --show-current)" != "$BRANCH" ]]; then
  result "FAILED: VPS source branch is not $BRANCH" 1
fi
if [[ -n "$(git status --porcelain --untracked-files=all)" ]]; then
  result 'FAILED: VPS source worktree is dirty; no changes were stashed' 1
fi

write_status running 'resolving the latest stable Release'
release_output="$("$RESOLVER" 2>>"$LOG")" || result 'FAILED: stable Release resolution failed' 1
parse_release_output "$release_output" || result 'FAILED: stable Release resolver output was invalid' 1

write_status running "fetching approved branch and Release tag $RELEASE_TAG"
git fetch "$ORIGIN_REMOTE" "$BRANCH" >> "$LOG" 2>&1 || result "FAILED: fetch $ORIGIN_REMOTE/$BRANCH failed" 1
git fetch "$UPSTREAM_REMOTE" "refs/tags/$RELEASE_TAG:refs/tags/$RELEASE_TAG" >> "$LOG" 2>&1 || result "FAILED: fetch Release tag $RELEASE_TAG failed" 1

local_head="$(git rev-parse HEAD)"
origin_head="$(git rev-parse "$ORIGIN_REMOTE/$BRANCH")"
BASE_COMMIT="$origin_head"
if [[ "$local_head" != "$origin_head" ]]; then
  result "FAILED: VPS $BRANCH $local_head differs from $ORIGIN_REMOTE/$BRANCH $origin_head" 1
fi

local_tag_object_sha="$(git rev-parse "$RELEASE_TAG^{tag}" 2>/dev/null || true)"
if [[ -z "$local_tag_object_sha" || "$local_tag_object_sha" != "$RELEASE_TAG_OBJECT_SHA" ]]; then
  result "FAILED: Release tag object verification failed for $RELEASE_TAG" 1
fi
RELEASE_COMMIT="$(git rev-list -n 1 "$RELEASE_TAG" 2>/dev/null || true)"
[[ "$RELEASE_COMMIT" =~ ^[0-9a-fA-F]{40}$ ]] || result "FAILED: could not peel Release tag $RELEASE_TAG" 1

[[ -r "$BASELINE_FILE" ]] || result 'FAILED: stable Release baseline metadata is missing' 1
baseline_commit="$(jq -er '.commit_sha' "$BASELINE_FILE" 2>/dev/null || true)"
[[ "$baseline_commit" =~ ^[0-9a-fA-F]{40}$ ]] || result 'FAILED: stable Release baseline commit is invalid' 1
git cat-file -e "$baseline_commit^{commit}" >/dev/null 2>&1 || result 'FAILED: stable Release baseline commit is unavailable' 1
if ! git merge-base --is-ancestor "$baseline_commit" "$RELEASE_COMMIT"; then
  result "FAILED: Release $RELEASE_TAG is not based on the recorded stable baseline" 1
fi

baseline_matches_release=0
if [[ "$(jq -r '.tag // empty' "$BASELINE_FILE")" == "$RELEASE_TAG" \
  && "$(jq -r '.commit_sha // empty' "$BASELINE_FILE")" == "$RELEASE_COMMIT" \
  && "$(jq -r '.tag_object_sha // empty' "$BASELINE_FILE")" == "$RELEASE_TAG_OBJECT_SHA" \
  && "$(jq -r '.published_at // empty' "$BASELINE_FILE")" == "$RELEASE_PUBLISHED_AT" ]]; then
  baseline_matches_release=1
fi
if git merge-base --is-ancestor "$RELEASE_COMMIT" "$local_head" && [[ "$baseline_matches_release" -eq 1 ]]; then
  result "SUCCESS: $BRANCH already contains stable Release $RELEASE_TAG at ${RELEASE_COMMIT:0:12}" 0
fi

INTEGRATION_BRANCH="integration/release-$RELEASE_TAG-$(date -u '+%Y%m%d-%H%M%S')-$RANDOM"
WORKTREE="$WORKTREE_ROOT/$JOB_ID"
mkdir -p "$WORKTREE"
rmdir "$WORKTREE" 2>/dev/null || true
git worktree add --detach "$WORKTREE" "$BRANCH" >> "$LOG" 2>&1 || result 'FAILED: create temporary Release integration worktree failed' 1
git -C "$WORKTREE" switch -c "$INTEGRATION_BRANCH" >> "$LOG" 2>&1 || result 'FAILED: create Release integration branch failed' 1

write_status running "checking Release $RELEASE_TAG merge into $INTEGRATION_BRANCH"
if ! git -C "$WORKTREE" merge --no-ff --no-edit "$RELEASE_COMMIT" >> "$LOG" 2>&1; then
  conflict_snapshot_dir="$CONFLICT_DIR/$JOB_ID"
  mkdir -p "$conflict_snapshot_dir"
  CONFLICT_FILES_JSON="$(git -C "$WORKTREE" diff --name-only --diff-filter=U | jq -R -s 'split("\n") | map(select(length > 0))')"
  conflict_files="$(jq -r 'join(", ")' <<< "$CONFLICT_FILES_JSON")"
  CONFLICT_BASE="$BASE_COMMIT"
  CONFLICT_RELEASE="$RELEASE_TAG@$RELEASE_COMMIT"
  CONFLICT_LOG="$conflict_snapshot_dir/metadata.json"
  RESOLUTION_HINT="Resolve the listed files in a local $BRANCH worktree against Release $RELEASE_TAG, run tests, push $ORIGIN_REMOTE/$BRANCH, then retry."
  git -C "$WORKTREE" status --short > "$conflict_snapshot_dir/status.txt" || true
  git -C "$WORKTREE" diff --cc > "$conflict_snapshot_dir/conflict.diff" || true
  git -C "$WORKTREE" ls-files -u > "$conflict_snapshot_dir/unmerged-stages.txt" || true
  jq -n \
    --arg job_id "$JOB_ID" \
    --arg integration_branch "$INTEGRATION_BRANCH" \
    --arg base_commit "$CONFLICT_BASE" \
    --arg release_tag "$RELEASE_TAG" \
    --arg release_commit "$RELEASE_COMMIT" \
    --arg release_published_at "$RELEASE_PUBLISHED_AT" \
    --arg conflict_release "$CONFLICT_RELEASE" \
    --arg log "$CONFLICT_LOG" \
    --arg artifact_path "$conflict_snapshot_dir/metadata.json" \
    --arg resolution_hint "$RESOLUTION_HINT" \
    --argjson files "$CONFLICT_FILES_JSON" \
    '{job_id:$job_id,integration_branch:$integration_branch,base_commit:$base_commit,release_tag:$release_tag,release_commit:$release_commit,release_published_at:$release_published_at,conflict_release:$conflict_release,conflict_files:$files,conflict_log:$log,artifact_path:$artifact_path,resolution_hint:$resolution_hint}' \
    > "$conflict_snapshot_dir/metadata.json"
  git -C "$WORKTREE" merge --abort >> "$LOG" 2>&1 || true
  if [[ -n "$conflict_files" ]]; then
    result "FAILED: stable Release merge conflict; files: $conflict_files" 1
  fi
  result 'FAILED: stable Release merge failed; inspect sync log' 1
fi

jq -n \
  --arg repository 'Wei-Shaw/sub2api' \
  --arg tag "$RELEASE_TAG" \
  --arg tag_object_sha "$RELEASE_TAG_OBJECT_SHA" \
  --arg commit_sha "$RELEASE_COMMIT" \
  --arg published_at "$RELEASE_PUBLISHED_AT" \
  '{repository:$repository,tag:$tag,tag_object_sha:$tag_object_sha,commit_sha:$commit_sha,published_at:$published_at}' \
  > "$WORKTREE/$BASELINE_RELATIVE.tmp"
mv -f "$WORKTREE/$BASELINE_RELATIVE.tmp" "$WORKTREE/$BASELINE_RELATIVE"
git -C "$WORKTREE" add "$BASELINE_RELATIVE"
if ! git -C "$WORKTREE" diff --cached --quiet; then
  git -C "$WORKTREE" commit -m "chore: record stable Release $RELEASE_TAG" >> "$LOG" 2>&1 || result 'FAILED: record stable Release baseline metadata failed' 1
fi

git -C "$WORKTREE" push "$ORIGIN_REMOTE" "HEAD:$INTEGRATION_BRANCH" >> "$LOG" 2>&1 || result "FAILED: push $INTEGRATION_BRANCH to $ORIGIN_REMOTE failed" 1
result "SUCCESS: created $ORIGIN_REMOTE/$INTEGRATION_BRANCH for stable Release $RELEASE_TAG; review locally before promoting $BRANCH" 0
