#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="${SUB2API_REPO:-/root/sub2api}"
BRANCH="${SUB2API_BRANCH:-custom-release}"
UPSTREAM_REMOTE="${SUB2API_UPSTREAM_REMOTE:-upstream}"
ORIGIN_REMOTE="${SUB2API_ORIGIN_REMOTE:-origin}"
RESOLVER="${SUB2API_RELEASE_RESOLVER:-$SCRIPT_DIR/resolve-stable-release.sh}"
STATE_HELPER="${SUB2API_RELEASE_STATE_HELPER:-$SCRIPT_DIR/release-state.sh}"
BASELINE_FILE="${SUB2API_STABLE_BASELINE_FILE:-$REPO/deploy/stable-release-baseline.json}"
DATA_DIR="${SUB2API_DATA_DIR:-/var/lib/docker/volumes/deploy_sub2api_data/_data}"
LOG="${SUB2API_SYNC_LOG:-/var/log/sub2api-release.log}"
WORKTREE_ROOT="${SUB2API_SYNC_WORKTREE_ROOT:-/var/tmp/sub2api-release}"
CONFLICT_DIR="${SUB2API_SYNC_CONFLICT_DIR:-$DATA_DIR/sync-conflicts}"
WORKTREE=""

source "$STATE_HELPER"

fail_job() {
  local message="$1"
  local error_code="${2:-RELEASE_PREPARATION_FAILED}"
  trap - ERR
  release_job_update "$JOB_ID" failed "$message" "$(jq -n --arg error_code "$error_code" '{error_code:$error_code,production_changed:false}')" || true
  printf 'Stable Release preparation failed: %s\n' "$message" >&2
  exit 1
}

on_error() {
  local code="$?"
  local line="${1:-unknown}"
  fail_job "unexpected preparation error at line $line (exit=$code)" UNEXPECTED_PREPARATION_ERROR
}

cleanup() {
  if [[ -n "$WORKTREE" && -d "$WORKTREE" ]]; then
    git -C "$REPO" worktree remove "$WORKTREE" >/dev/null 2>&1 || true
  fi
}

parse_release_output() {
  local output="$1"
  local line key value
  local seen_tag=0 seen_published=0 seen_tag_sha=0 seen_type=0 seen_commit=0
  while IFS= read -r line; do
    [[ -n "$line" && "$line" == *=* ]] || return 1
    key="${line%%=*}"
    value="${line#*=}"
    [[ -n "$value" ]] || return 1
    case "$key" in
      release_tag) ((seen_tag == 0)) || return 1; RELEASE_TAG="$value"; seen_tag=1 ;;
      release_published_at) ((seen_published == 0)) || return 1; RELEASE_PUBLISHED_AT="$value"; seen_published=1 ;;
      release_tag_object_sha) ((seen_tag_sha == 0)) || return 1; RELEASE_TAG_OBJECT_SHA="$value"; seen_tag_sha=1 ;;
      release_tag_object_type) ((seen_type == 0)) || return 1; RELEASE_TAG_OBJECT_TYPE="$value"; seen_type=1 ;;
      release_commit) ((seen_commit == 0)) || return 1; RELEASE_COMMIT="$value"; seen_commit=1 ;;
      *) return 1 ;;
    esac
  done <<< "$output"
  [[ "$seen_tag" -eq 1 && "$seen_published" -eq 1 && "$seen_tag_sha" -eq 1 && "$seen_type" -eq 1 && "$seen_commit" -eq 1 ]]
}

[[ "$BRANCH" == custom-release ]] || { printf 'Only custom-release may be prepared\n' >&2; exit 1; }
[[ "${1:-}" == --job-id && -n "${2:-}" && "$#" -eq 2 ]] || { printf 'usage: sync-upstream.sh --job-id <job-id>\n' >&2; exit 1; }
JOB_ID="$2"
release_valid_job_id "$JOB_ID" || { printf 'Invalid release job id\n' >&2; exit 1; }
job_file="$(release_job_path "$JOB_ID")"
[[ -r "$job_file" ]] || { printf 'Release job file is missing\n' >&2; exit 1; }

trap cleanup EXIT
trap 'on_error "$LINENO"' ERR
mkdir -p "$DATA_DIR" "$WORKTREE_ROOT" "$CONFLICT_DIR" "$(dirname "$LOG")"
touch "$LOG"

cd "$REPO"
repo_root="$(realpath -m "$REPO")"
baseline_path="$(realpath -m "$BASELINE_FILE")"
case "$baseline_path" in
  "$repo_root"/*)
    BASELINE_FILE="$baseline_path"
    BASELINE_RELATIVE="${BASELINE_FILE#"$repo_root"/}"
    ;;
  *) fail_job 'baseline metadata path must stay inside the repository' INVALID_BASELINE_PATH ;;
esac
[[ "$(git branch --show-current)" == "$BRANCH" ]] || fail_job "VPS source branch is not $BRANCH" WRONG_SOURCE_BRANCH
[[ -z "$(git status --porcelain --untracked-files=all)" ]] || fail_job 'VPS source worktree is dirty' DIRTY_SOURCE

release_job_update "$JOB_ID" resolving_target 'Checking the latest stable Release' '{}'
release_output="$($RESOLVER 2>>"$LOG")" || fail_job 'stable Release resolution failed' RELEASE_RESOLUTION_FAILED
RELEASE_TAG=''
RELEASE_PUBLISHED_AT=''
RELEASE_TAG_OBJECT_SHA=''
RELEASE_TAG_OBJECT_TYPE=''
RELEASE_COMMIT=''
parse_release_output "$release_output" || fail_job 'stable Release resolver output was invalid' RELEASE_IDENTITY_INVALID
[[ "$RELEASE_TAG_OBJECT_TYPE" == tag ]] || fail_job 'latest Release does not use an annotated tag' RELEASE_TAG_NOT_ANNOTATED

release_job_update "$JOB_ID" resolving_target "Validating annotated tag $RELEASE_TAG" "$(jq -n \
  --arg tag "$RELEASE_TAG" \
  --arg commit "$RELEASE_COMMIT" \
  --arg published "$RELEASE_PUBLISHED_AT" \
  '{release_tag:$tag,release_commit:$commit,release_published_at:$published}')"

git fetch "$ORIGIN_REMOTE" "$BRANCH" >> "$LOG" 2>&1 || fail_job "fetch $ORIGIN_REMOTE/$BRANCH failed" ORIGIN_FETCH_FAILED
git fetch "$UPSTREAM_REMOTE" "refs/tags/$RELEASE_TAG:refs/tags/$RELEASE_TAG" >> "$LOG" 2>&1 || fail_job "fetch exact Release tag $RELEASE_TAG failed" RELEASE_TAG_FETCH_FAILED

local_head="$(git rev-parse HEAD)"
origin_head="$(git rev-parse "$ORIGIN_REMOTE/$BRANCH")"
git merge-base --is-ancestor "$local_head" "$origin_head" \
  || fail_job "local $BRANCH is not an ancestor of $ORIGIN_REMOTE/$BRANCH" SOURCE_BASE_MISMATCH
BASE_COMMIT="$origin_head"

local_tag_object_sha="$(git rev-parse "$RELEASE_TAG^{tag}" 2>/dev/null || true)"
[[ "$local_tag_object_sha" == "$RELEASE_TAG_OBJECT_SHA" ]] || fail_job 'local tag object does not match GitHub API' RELEASE_TAG_OBJECT_MISMATCH
local_release_commit="$(git rev-parse "$RELEASE_TAG^{commit}" 2>/dev/null || true)"
[[ "$local_release_commit" == "$RELEASE_COMMIT" ]] || fail_job 'local peeled commit does not match GitHub tag API' RELEASE_COMMIT_MISMATCH

[[ -r "$BASELINE_FILE" ]] || fail_job 'stable Release baseline metadata is missing' BASELINE_MISSING
baseline_tag="$(jq -er '.tag' "$BASELINE_FILE" 2>/dev/null || true)"
baseline_tag_object="$(jq -er '.tag_object_sha' "$BASELINE_FILE" 2>/dev/null || true)"
baseline_commit="$(jq -er '.commit_sha' "$BASELINE_FILE" 2>/dev/null || true)"
baseline_published="$(jq -er '.published_at' "$BASELINE_FILE" 2>/dev/null || true)"
[[ "$baseline_tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail_job 'baseline tag is invalid' BASELINE_INVALID
[[ "$baseline_tag_object" =~ ^[0-9a-f]{40}$ && "$baseline_commit" =~ ^[0-9a-f]{40}$ ]] || fail_job 'baseline object identity is invalid' BASELINE_INVALID
[[ -n "$baseline_published" ]] || fail_job 'baseline publication time is missing' BASELINE_INVALID
git cat-file -e "$baseline_commit^{commit}" >/dev/null 2>&1 || fail_job 'baseline commit is unavailable' BASELINE_COMMIT_MISSING
git merge-base --is-ancestor "$baseline_commit" "$RELEASE_COMMIT" || fail_job 'latest Release is not descended from the stable baseline' RELEASE_ANCESTRY_INVALID

baseline_matches=0
if [[ "$baseline_tag" == "$RELEASE_TAG" && "$baseline_tag_object" == "$RELEASE_TAG_OBJECT_SHA" \
  && "$baseline_commit" == "$RELEASE_COMMIT" && "$baseline_published" == "$RELEASE_PUBLISHED_AT" ]]; then
  baseline_matches=1
fi

if git merge-base --is-ancestor "$RELEASE_COMMIT" "$BASE_COMMIT"; then
  target_baseline_json="$(git show "$BASE_COMMIT:$BASELINE_RELATIVE" 2>/dev/null)" \
    || fail_job 'integrated target does not contain stable Release baseline metadata' BASELINE_MERGE_IDENTITY_MISMATCH
  jq -e \
    --arg tag "$RELEASE_TAG" \
    --arg tag_object "$RELEASE_TAG_OBJECT_SHA" \
    --arg commit "$RELEASE_COMMIT" \
    --arg published "$RELEASE_PUBLISHED_AT" '
      .repository == "Wei-Shaw/sub2api"
      and .tag == $tag
      and .tag_object_sha == $tag_object
      and .commit_sha == $commit
      and .published_at == $published
    ' <<< "$target_baseline_json" >/dev/null \
    || fail_job 'integrated target baseline does not match the resolved Release identity' BASELINE_MERGE_IDENTITY_MISMATCH

  if [[ "$baseline_matches" -ne 1 ]]; then
    mapfile -t integrated_merges < <(git rev-list --first-parent --merges "$local_head..$BASE_COMMIT")
    [[ "${#integrated_merges[@]}" -eq 1 ]] \
      || fail_job 'integrated target does not contain exactly one canonical stable Release merge' BASELINE_MERGE_IDENTITY_MISMATCH
    integrated_merge="${integrated_merges[0]}"
    integrated_subject="$(git show -s --format=%s "$integrated_merge")"
    read -r integrated_identity integrated_parent_one integrated_parent_two integrated_parent_extra \
      <<< "$(git rev-list --parents -n 1 "$integrated_merge")"
    [[ "$integrated_identity" == "$integrated_merge" && -z "$integrated_parent_extra" ]] \
      || fail_job 'integrated stable Release merge must have exactly two parents' BASELINE_MERGE_IDENTITY_MISMATCH
    [[ "$integrated_subject" == "merge: integrate stable Release $RELEASE_TAG" ]] \
      || fail_job 'integrated stable Release merge subject does not match the baseline tag' BASELINE_MERGE_IDENTITY_MISMATCH
    git merge-base --is-ancestor "$local_head" "$integrated_parent_one" \
      || fail_job 'integrated stable Release merge is not based on the deployed source' BASELINE_MERGE_IDENTITY_MISMATCH
    ! git merge-base --is-ancestor "$RELEASE_COMMIT" "$integrated_parent_one" \
      || fail_job 'integrated stable Release ancestry predates the canonical merge' BASELINE_MERGE_IDENTITY_MISMATCH
    [[ "$integrated_parent_two" == "$RELEASE_COMMIT" ]] \
      || fail_job 'integrated stable Release merge second parent does not match the resolved Release' BASELINE_MERGE_IDENTITY_MISMATCH
  fi

  metadata="$(jq -n \
    --arg base "$BASE_COMMIT" \
    --arg target "$BASE_COMMIT" \
    --arg tag "$RELEASE_TAG" \
    --arg commit "$RELEASE_COMMIT" \
    --arg published "$RELEASE_PUBLISHED_AT" \
    '{base_commit:$base,target_commit:$target,integration_branch:"",release_tag:$tag,release_commit:$commit,release_published_at:$published}')"
  release_job_update "$JOB_ID" resolving_target "Stable Release $RELEASE_TAG is already integrated" "$metadata"
  exit 0
fi

INTEGRATION_BRANCH="integration/release-$RELEASE_TAG-$(date -u '+%Y%m%d-%H%M%S')-$RANDOM"
WORKTREE="$WORKTREE_ROOT/$JOB_ID"
[[ ! -e "$WORKTREE" ]] || fail_job 'candidate worktree path already exists' WORKTREE_PATH_EXISTS
git worktree add --detach "$WORKTREE" "$ORIGIN_REMOTE/$BRANCH" >> "$LOG" 2>&1 || fail_job 'create candidate worktree failed' WORKTREE_CREATE_FAILED
git -C "$WORKTREE" switch -c "$INTEGRATION_BRANCH" >> "$LOG" 2>&1 || fail_job 'create candidate branch failed' INTEGRATION_BRANCH_FAILED
release_job_update "$JOB_ID" resolving_target "Merging $RELEASE_TAG into $INTEGRATION_BRANCH" "$(jq -n --arg branch "$INTEGRATION_BRANCH" --arg base "$BASE_COMMIT" '{integration_branch:$branch,base_commit:$base}')"

MERGE_SUBJECT="merge: integrate stable Release $RELEASE_TAG"
if ! git -C "$WORKTREE" merge --no-ff -m "$MERGE_SUBJECT" "$RELEASE_COMMIT" >> "$LOG" 2>&1; then
  conflict_snapshot_dir="$CONFLICT_DIR/$JOB_ID"
  mkdir -p "$conflict_snapshot_dir"
  conflict_files_json="$(git -C "$WORKTREE" diff --name-only --diff-filter=U | jq -R -s 'split("\n") | map(select(length > 0))')"
  conflict_release="$RELEASE_TAG@$RELEASE_COMMIT"
  resolution_hint="Resolve the listed files in a local $BRANCH worktree against $RELEASE_TAG, run tests, push $ORIGIN_REMOTE/$BRANCH, then retry."
  git -C "$WORKTREE" status --short > "$conflict_snapshot_dir/status.txt" || true
  git -C "$WORKTREE" diff --cc > "$conflict_snapshot_dir/conflict.diff" || true
  git -C "$WORKTREE" ls-files -u > "$conflict_snapshot_dir/unmerged-stages.txt" || true
  artifact_path="$conflict_snapshot_dir/metadata.json"
  conflict_metadata="$(jq -n \
    --arg job_id "$JOB_ID" \
    --arg integration_branch "$INTEGRATION_BRANCH" \
    --arg base_commit "$BASE_COMMIT" \
    --arg release_tag "$RELEASE_TAG" \
    --arg release_commit "$RELEASE_COMMIT" \
    --arg release_published_at "$RELEASE_PUBLISHED_AT" \
    --arg conflict_release "$conflict_release" \
    --arg artifact_path "$artifact_path" \
    --arg resolution_hint "$resolution_hint" \
    --argjson conflict_files "$conflict_files_json" \
    '{job_id:$job_id,integration_branch:$integration_branch,base_commit:$base_commit,conflict_base:$base_commit,release_tag:$release_tag,release_commit:$release_commit,release_published_at:$release_published_at,conflict_release:$conflict_release,conflict_files:$conflict_files,conflict_log:$artifact_path,artifact_path:$artifact_path,resolution_hint:$resolution_hint,production_changed:false,error_code:"RELEASE_CONFLICT"}')"
  printf '%s\n' "$conflict_metadata" > "$artifact_path.tmp"
  mv -f "$artifact_path.tmp" "$artifact_path"
  git -C "$WORKTREE" merge --abort >> "$LOG" 2>&1 || true
  release_job_update "$JOB_ID" conflict 'Stable Release merge conflict; production was not changed' "$conflict_metadata"
  exit 2
fi

MERGE_COMMIT="$(git -C "$WORKTREE" rev-parse HEAD)"
merge_subject="$(git -C "$WORKTREE" show -s --format=%s "$MERGE_COMMIT")"
read -r merge_identity merge_parent_one merge_parent_two merge_parent_extra \
  <<< "$(git -C "$WORKTREE" rev-list --parents -n 1 "$MERGE_COMMIT")"
[[ "$merge_identity" == "$MERGE_COMMIT" && -z "$merge_parent_extra" ]] \
  || fail_job 'stable Release integration is not an exact two-parent merge' RELEASE_MERGE_SHAPE_INVALID
[[ "$merge_subject" == "$MERGE_SUBJECT" ]] \
  || fail_job 'stable Release integration subject is not canonical' RELEASE_MERGE_SUBJECT_INVALID
[[ "$merge_parent_one" == "$BASE_COMMIT" ]] \
  || fail_job 'stable Release integration first parent is not the approved custom-release base' RELEASE_MERGE_FIRST_PARENT_INVALID
[[ "$merge_parent_two" == "$RELEASE_COMMIT" ]] \
  || fail_job 'stable Release integration second parent is not the resolved Release commit' RELEASE_MERGE_SECOND_PARENT_INVALID

jq -n \
  --arg repository 'Wei-Shaw/sub2api' \
  --arg tag "$RELEASE_TAG" \
  --arg tag_object_sha "$RELEASE_TAG_OBJECT_SHA" \
  --arg commit_sha "$RELEASE_COMMIT" \
  --arg published_at "$RELEASE_PUBLISHED_AT" \
  '{repository:$repository,tag:$tag,tag_object_sha:$tag_object_sha,commit_sha:$commit_sha,published_at:$published_at}' \
  > "$WORKTREE/$BASELINE_RELATIVE.tmp"
mv -f "$WORKTREE/$BASELINE_RELATIVE.tmp" "$WORKTREE/$BASELINE_RELATIVE"
jq -e \
  --arg tag "$RELEASE_TAG" \
  --arg tag_object "$RELEASE_TAG_OBJECT_SHA" \
  --arg commit "$RELEASE_COMMIT" \
  --arg published "$RELEASE_PUBLISHED_AT" '
    .repository == "Wei-Shaw/sub2api"
    and .tag == $tag
    and .tag_object_sha == $tag_object
    and .commit_sha == $commit
    and .published_at == $published
  ' "$WORKTREE/$BASELINE_RELATIVE" >/dev/null \
  || fail_job 'stable Release baseline does not match the canonical merge identity' BASELINE_IDENTITY_INVALID
git -C "$WORKTREE" add "$BASELINE_RELATIVE"
if ! git -C "$WORKTREE" diff --cached --quiet; then
  git -C "$WORKTREE" commit -m "chore: record stable Release $RELEASE_TAG" >> "$LOG" 2>&1 || fail_job 'commit stable baseline failed' BASELINE_COMMIT_FAILED
fi

TARGET_COMMIT="$(git -C "$WORKTREE" rev-parse HEAD)"
git -C "$WORKTREE" push "$ORIGIN_REMOTE" "HEAD:$INTEGRATION_BRANCH" >> "$LOG" 2>&1 || fail_job 'push integration candidate failed' INTEGRATION_PUSH_FAILED
metadata="$(jq -n \
  --arg branch "$INTEGRATION_BRANCH" \
  --arg base "$BASE_COMMIT" \
  --arg target "$TARGET_COMMIT" \
  --arg tag "$RELEASE_TAG" \
  --arg commit "$RELEASE_COMMIT" \
  --arg published "$RELEASE_PUBLISHED_AT" \
  '{integration_branch:$branch,base_commit:$base,target_commit:$target,release_tag:$tag,release_commit:$commit,release_published_at:$published}')"
release_job_update "$JOB_ID" resolving_target "Candidate $TARGET_COMMIT is waiting for Actions" "$metadata"
printf 'candidate_commit=%s\n' "$TARGET_COMMIT"
