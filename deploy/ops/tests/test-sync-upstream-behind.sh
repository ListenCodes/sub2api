#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

SEED="$TMP_DIR/seed"
REMOTE="$TMP_DIR/origin.git"
REPO="$TMP_DIR/production"
DATA_DIR="$TMP_DIR/data"
mkdir -p "$SEED" "$DATA_DIR"

git init -q -b custom-release "$SEED"
git -C "$SEED" config user.name 'Release Fixture'
git -C "$SEED" config user.email 'release-fixture@example.com'
mkdir -p "$SEED/deploy"
printf 'release\n' > "$SEED/release.txt"
git -C "$SEED" add release.txt
git -C "$SEED" commit -q -m 'stable release'
release_commit="$(git -C "$SEED" rev-parse HEAD)"
git -C "$SEED" tag -a v0.1.158 -m v0.1.158 "$release_commit"
tag_object="$(git -C "$SEED" rev-parse 'v0.1.158^{tag}')"
cat > "$SEED/deploy/stable-release-baseline.json" <<JSON
{
  "repository": "Wei-Shaw/sub2api",
  "tag": "v0.1.158",
  "tag_object_sha": "$tag_object",
  "commit_sha": "$release_commit",
  "published_at": "2026-07-16T12:37:06Z"
}
JSON
git -C "$SEED" add deploy/stable-release-baseline.json
git -C "$SEED" commit -q -m 'record stable baseline'
local_commit="$(git -C "$SEED" rev-parse HEAD)"
printf 'approved custom change\n' > "$SEED/custom.txt"
git -C "$SEED" add custom.txt
git -C "$SEED" commit -q -m 'approved custom change'
origin_commit="$(git -C "$SEED" rev-parse HEAD)"

git clone -q --bare "$SEED" "$REMOTE"
git --git-dir="$REMOTE" symbolic-ref HEAD refs/heads/custom-release
git clone -q "$REMOTE" "$REPO"
git -C "$REPO" config user.name 'Release Fixture'
git -C "$REPO" config user.email 'release-fixture@example.com'
git -C "$REPO" switch -q custom-release
git -C "$REPO" reset --keep "$local_commit"
git -C "$REPO" remote add upstream "$REMOTE"

cat > "$TMP_DIR/resolver.sh" <<SH
#!/usr/bin/env bash
printf '%s\n' \
  'release_tag=v0.1.158' \
  'release_published_at=2026-07-16T12:37:06Z' \
  'release_tag_object_sha=$tag_object' \
  'release_tag_object_type=tag' \
  'release_commit=$release_commit'
SH
chmod +x "$TMP_DIR/resolver.sh"

export SUB2API_DATA_DIR="$DATA_DIR"
export SUB2API_RELEASE_JOBS_DIR="$DATA_DIR/release-jobs"
export SUB2API_CURRENT_RELEASE_JOB_FILE="$DATA_DIR/release-current-job-id"
source "$ROOT_DIR/deploy/ops/release-state.sh"
release_job_init update-behind

SUB2API_REPO="$REPO" \
SUB2API_RELEASE_RESOLVER="$TMP_DIR/resolver.sh" \
SUB2API_RELEASE_STATE_HELPER="$ROOT_DIR/deploy/ops/release-state.sh" \
SUB2API_SYNC_WORKTREE_ROOT="$TMP_DIR/worktrees" \
SUB2API_SYNC_CONFLICT_DIR="$TMP_DIR/conflicts" \
SUB2API_SYNC_LOG="$TMP_DIR/release.log" \
  "$ROOT_DIR/deploy/ops/sync-upstream.sh" --job-id update-behind

job_file="$DATA_DIR/release-jobs/update-behind.json"
[[ "$(jq -r '.status' "$job_file")" == waiting_actions ]]
[[ "$(jq -r '.base_commit' "$job_file")" == "$origin_commit" ]]
[[ "$(jq -r '.target_commit' "$job_file")" == "$origin_commit" ]]
[[ "$(jq -r '.integration_branch' "$job_file")" == '' ]]
[[ "$(git -C "$REPO" rev-parse HEAD)" == "$local_commit" ]]

printf 'sync-upstream behind-source fixture: PASS\n'
