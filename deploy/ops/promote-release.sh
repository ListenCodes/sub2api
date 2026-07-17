#!/usr/bin/env bash
set -Eeuo pipefail

REPO="${SUB2API_REPO:-/root/sub2api}"
BRANCH="${SUB2API_BRANCH:-custom-release}"
ORIGIN_REMOTE="${SUB2API_ORIGIN_REMOTE:-origin}"
ORIGIN_REF="$ORIGIN_REMOTE/$BRANCH"
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
