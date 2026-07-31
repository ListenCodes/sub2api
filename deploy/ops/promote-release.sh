#!/usr/bin/env bash
set -Eeuo pipefail

REPO="${SUB2API_REPO:-/root/sub2api}"
BRANCH="${SUB2API_BRANCH:-custom-release}"
ORIGIN_REMOTE="${SUB2API_ORIGIN_REMOTE:-origin}"
ORIGIN_REF="$ORIGIN_REMOTE/$BRANCH"
BASELINE_RELATIVE="${SUB2API_STABLE_BASELINE_RELATIVE:-deploy/stable-release-baseline.json}"
LOG="${SUB2API_PROMOTE_LOG:-/var/log/sub2api-release.log}"

fail() {
  printf 'Release promotion failed: %s\n' "$1" >&2
  exit 1
}

[[ "$BRANCH" == custom-release ]] || fail 'only custom-release may be promoted'
[[ "$#" -eq 3 ]] || fail 'usage: promote-release.sh <base-commit> <target-commit> <integration-branch>'
BASE_COMMIT="$1"
TARGET_COMMIT="$2"
INTEGRATION_BRANCH="$3"
[[ "$BASE_COMMIT" =~ ^[0-9a-f]{40}$ ]] || fail 'base commit must be a full SHA'
[[ "$TARGET_COMMIT" =~ ^[0-9a-f]{40}$ ]] || fail 'target commit must be a full SHA'
[[ "$INTEGRATION_BRANCH" == integration/release-* ]] || fail 'integration branch name is not approved'

cd "$REPO"
[[ "$(git branch --show-current)" == "$BRANCH" ]] || fail "VPS source branch is not $BRANCH"
[[ -z "$(git status --porcelain --untracked-files=all)" ]] || fail 'VPS source worktree is dirty'
git fetch "$ORIGIN_REMOTE" "$BRANCH" "$INTEGRATION_BRANCH" >> "$LOG" 2>&1 || fail 'fetch approved and integration refs failed'

origin_head="$(git rev-parse "$ORIGIN_REF")"
integration_ref="$ORIGIN_REMOTE/$INTEGRATION_BRANCH"
integration_head="$(git rev-parse "$integration_ref")"
local_head="$(git rev-parse HEAD)"
[[ "$origin_head" == "$BASE_COMMIT" ]] || fail "$ORIGIN_REF changed after candidate creation"
[[ "$integration_head" == "$TARGET_COMMIT" ]] || fail 'integration ref does not match the tested target'
git merge-base --is-ancestor "$BASE_COMMIT" "$TARGET_COMMIT" || fail 'tested target is not based on the approved branch'

baseline_json="$(git show "$TARGET_COMMIT:$BASELINE_RELATIVE" 2>/dev/null)" \
  || fail 'tested target does not contain stable Release baseline metadata'
jq -e '
  .repository == "Wei-Shaw/sub2api"
  and (.tag | type == "string" and test("^v[0-9]+\\.[0-9]+\\.[0-9]+$"))
  and (.tag_object_sha | type == "string" and test("^[0-9a-f]{40}$"))
  and (.commit_sha | type == "string" and test("^[0-9a-f]{40}$"))
  and (.published_at | type == "string" and length > 0)
' <<< "$baseline_json" >/dev/null || fail 'tested target stable Release baseline is invalid'
stable_tag="$(jq -r '.tag' <<< "$baseline_json")"
stable_commit="$(jq -r '.commit_sha' <<< "$baseline_json")"
git cat-file -e "$stable_commit^{commit}" >/dev/null 2>&1 \
  || fail 'canonical stable Release merge second parent is unavailable'

mapfile -t candidate_merges < <(git rev-list --first-parent --merges "$BASE_COMMIT..$TARGET_COMMIT")
[[ "${#candidate_merges[@]}" -eq 1 ]] \
  || fail 'tested target does not contain exactly one canonical stable Release merge'
candidate_merge="${candidate_merges[0]}"
candidate_subject="$(git show -s --format=%s "$candidate_merge")"
read -r merge_identity merge_parent_one merge_parent_two merge_parent_extra \
  <<< "$(git rev-list --parents -n 1 "$candidate_merge")"
[[ "$merge_identity" == "$candidate_merge" && -z "$merge_parent_extra" ]] \
  || fail 'canonical stable Release merge must have exactly two parents'
[[ "$candidate_subject" == "merge: integrate stable Release $stable_tag" ]] \
  || fail 'canonical stable Release merge subject does not match the baseline tag'
[[ "$merge_parent_one" == "$BASE_COMMIT" ]] \
  || fail 'canonical stable Release merge first parent is not the approved base'
[[ "$merge_parent_two" == "$stable_commit" ]] \
  || fail 'canonical stable Release merge second parent does not match the baseline commit'

if [[ "$local_head" != "$TARGET_COMMIT" ]] \
  && ! git merge-base --is-ancestor "$local_head" "$BASE_COMMIT"; then
  fail 'local source is neither an ancestor of the approved base nor the tested target'
fi

integration_source="$(git rev-parse --symbolic-full-name "$integration_ref")"
[[ "$integration_source" == refs/remotes/* ]] || fail 'integration source is not a remote-tracking ref'
git push "$ORIGIN_REMOTE" "$integration_source:refs/heads/$BRANCH" >> "$LOG" 2>&1 || fail 'push custom-release failed'
git fetch "$ORIGIN_REMOTE" "$BRANCH" >> "$LOG" 2>&1 || fail 'post-push verification fetch failed'
[[ "$(git rev-parse "$ORIGIN_REF")" == "$TARGET_COMMIT" ]] || fail 'remote custom-release did not reach the tested target'
printf 'promoted_commit=%s\n' "$TARGET_COMMIT"
