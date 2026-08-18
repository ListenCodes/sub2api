#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="${SUB2API_REPO:-$(cd "$SCRIPT_DIR/../.." && pwd)}"
RESOLVER="${SUB2API_RELEASE_RESOLVER:-$REPO/deploy/ops/resolve-stable-release.sh}"
COMMON_HELPER="${SUB2API_RELEASE_COMMON_HELPER:-$REPO/deploy/ops/release-common.sh}"
UPSTREAM_URL="${SUB2API_UPSTREAM_URL:-https://github.com/Wei-Shaw/sub2api.git}"
BASELINE_RELATIVE="${SUB2API_STABLE_BASELINE_RELATIVE:-deploy/stable-release-baseline.json}"
OUTPUT_FILE="${GITHUB_OUTPUT:-}"

export SUB2API_REPO="$REPO"
source "$COMMON_HELPER"

fail() {
  printf 'upstream Stable preflight failed: %s\n' "$1" >&2
  exit "${2:-1}"
}

write_output() {
  [[ -n "$OUTPUT_FILE" ]] || return 0
  printf '%s=%s\n' "$1" "$2" >> "$OUTPUT_FILE"
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
  [[ "$seen_tag" -eq 1 && "$seen_published" -eq 1 && "$seen_tag_sha" -eq 1 \
    && "$seen_type" -eq 1 && "$seen_commit" -eq 1 ]]
}

cd "$REPO"
[[ -z "$(git status --porcelain --untracked-files=all)" ]] \
  || fail 'checkout must be clean before staging the candidate'
case "$BASELINE_RELATIVE" in
  /*|../*|*/../*|*/..) fail 'baseline path must stay inside the repository' ;;
esac

BASE_COMMIT="$(git rev-parse HEAD)" || fail 'cannot resolve the custom-release base'
baseline_json="$(git show "$BASE_COMMIT:$BASELINE_RELATIVE" 2>/dev/null)" \
  || fail 'approved base does not contain Stable baseline metadata'
release_stable_baseline_valid "$baseline_json" || fail 'baseline identity is invalid'
baseline_commit="$(jq -r '.commit_sha' <<< "$baseline_json")"

release_output="$($RESOLVER)" || fail 'stable Release resolution failed'
RELEASE_TAG=''
RELEASE_PUBLISHED_AT=''
RELEASE_TAG_OBJECT_SHA=''
RELEASE_TAG_OBJECT_TYPE=''
RELEASE_COMMIT=''
parse_release_output "$release_output" || fail 'stable Release resolver output was invalid'
[[ "$RELEASE_TAG_OBJECT_TYPE" == tag ]] || fail 'latest Release tag is not annotated'
[[ "$RELEASE_TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ \
  && "$RELEASE_TAG_OBJECT_SHA" =~ ^[0-9a-f]{40}$ \
  && "$RELEASE_COMMIT" =~ ^[0-9a-f]{40}$ \
  && -n "$RELEASE_PUBLISHED_AT" ]] \
  || fail 'stable Release identity fields are invalid'

git fetch --force --no-tags "$UPSTREAM_URL" \
  "refs/tags/$RELEASE_TAG:refs/tags/$RELEASE_TAG" >/dev/null \
  || fail "fetch exact Release tag $RELEASE_TAG failed"
[[ "$(git rev-parse "$RELEASE_TAG^{tag}" 2>/dev/null || true)" == "$RELEASE_TAG_OBJECT_SHA" ]] \
  || fail 'fetched tag object does not match the GitHub Release identity'
[[ "$(git rev-parse "$RELEASE_TAG^{commit}" 2>/dev/null || true)" == "$RELEASE_COMMIT" ]] \
  || fail 'fetched commit does not match the annotated Release tag'
git cat-file -e "$baseline_commit^{commit}" >/dev/null 2>&1 \
  || fail 'baseline commit is unavailable'
git merge-base --is-ancestor "$baseline_commit" "$BASE_COMMIT" \
  || fail 'approved base does not contain its recorded Stable baseline commit'
git merge-base --is-ancestor "$baseline_commit" "$RELEASE_COMMIT" \
  || fail 'latest Release is not descended from the recorded Stable baseline'

if git merge-base --is-ancestor "$RELEASE_COMMIT" "$BASE_COMMIT"; then
  release_stable_baseline_matches "$baseline_json" "$RELEASE_TAG" \
    "$RELEASE_TAG_OBJECT_SHA" "$RELEASE_COMMIT" "$RELEASE_PUBLISHED_AT" \
    || fail 'integrated Release baseline does not match the latest Release identity'
  write_output candidate_prepared false
  write_output stable_tag "$RELEASE_TAG"
  write_output target_commit "$BASE_COMMIT"
  printf 'Stable Release %s is already integrated at %s\n' "$RELEASE_TAG" "$BASE_COMMIT"
  exit 0
fi

git config --local user.name 'Sub2API Upstream Preflight'
git config --local user.email 'actions@users.noreply.github.com'
MERGE_SUBJECT="$(release_stable_merge_subject "$RELEASE_TAG")"
if ! release_merge_stable_candidate "$REPO" "$RELEASE_COMMIT" "$RELEASE_TAG" >/dev/null; then
  printf 'conflicting files:\n' >&2
  git diff --name-only --diff-filter=U >&2 || true
  git merge --abort >/dev/null 2>&1 || true
  fail "Stable Release $RELEASE_TAG conflicts with custom-release" 2
fi

MERGE_COMMIT="$(git rev-parse HEAD)"
release_validate_canonical_stable_merge "$REPO" "$MERGE_COMMIT" "$BASE_COMMIT" "$RELEASE_COMMIT" "$RELEASE_TAG" \
  || fail "candidate merge identity is invalid ($RELEASE_STABLE_MERGE_ERROR)"

mkdir -p "$(dirname "$BASELINE_RELATIVE")"
jq -n \
  --arg repository 'Wei-Shaw/sub2api' \
  --arg tag "$RELEASE_TAG" \
  --arg tag_object_sha "$RELEASE_TAG_OBJECT_SHA" \
  --arg commit_sha "$RELEASE_COMMIT" \
  --arg published_at "$RELEASE_PUBLISHED_AT" \
  '{repository:$repository,tag:$tag,tag_object_sha:$tag_object_sha,commit_sha:$commit_sha,published_at:$published_at}' \
  > "$BASELINE_RELATIVE.tmp"
mv -f "$BASELINE_RELATIVE.tmp" "$BASELINE_RELATIVE"
git add "$BASELINE_RELATIVE"
git commit -m "chore: record stable Release $RELEASE_TAG" >/dev/null
TARGET_COMMIT="$(git rev-parse HEAD)"

write_output candidate_prepared true
write_output stable_tag "$RELEASE_TAG"
write_output base_commit "$BASE_COMMIT"
write_output merge_commit "$MERGE_COMMIT"
write_output target_commit "$TARGET_COMMIT"
printf 'Prepared read-only compatibility candidate %s for %s\n' "$TARGET_COMMIT" "$RELEASE_TAG"
