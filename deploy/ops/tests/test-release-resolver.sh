#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
RESOLVER="$ROOT_DIR/deploy/ops/resolve-stable-release.sh"
FIXTURE_DIR="$(mktemp -d)"
trap 'rm -rf "$FIXTURE_DIR"' EXIT

cat >"$FIXTURE_DIR/stable.json" <<'JSON'
{"draft":false,"prerelease":false,"tag_name":"v0.1.157","published_at":"2026-07-16T09:12:30Z"}
JSON
cat >"$FIXTURE_DIR/draft.json" <<'JSON'
{"draft":true,"prerelease":false,"tag_name":"v0.1.157","published_at":"2026-07-16T09:12:30Z"}
JSON
cat >"$FIXTURE_DIR/prerelease.json" <<'JSON'
{"draft":false,"prerelease":true,"tag_name":"v0.1.157","published_at":"2026-07-16T09:12:30Z"}
JSON
cat >"$FIXTURE_DIR/malformed-tag.json" <<'JSON'
{"draft":false,"prerelease":false,"tag_name":"release-157","published_at":"2026-07-16T09:12:30Z"}
JSON
cat >"$FIXTURE_DIR/ref.json" <<'JSON'
{"ref":"refs/tags/v0.1.157","object":{"type":"tag","sha":"a44e63f9fab426ec181bafcf4e4c1a002bbcb8e0"}}
JSON
cat >"$FIXTURE_DIR/missing-ref.json" <<'JSON'
{"ref":"refs/tags/v0.1.157","object":{"type":"tag"}}
JSON
cat >"$FIXTURE_DIR/mismatched-ref.json" <<'JSON'
{"ref":"refs/tags/v0.1.156","object":{"type":"tag","sha":"a44e63f9fab426ec181bafcf4e4c1a002bbcb8e0"}}
JSON
cat >"$FIXTURE_DIR/lightweight-ref.json" <<'JSON'
{"ref":"refs/tags/v0.1.157","object":{"type":"commit","sha":"a2779cd5f30d6d3904a9d59088aed09507678dfe"}}
JSON
cat >"$FIXTURE_DIR/tag-object.json" <<'JSON'
{"tag":"v0.1.157","object":{"type":"commit","sha":"a2779cd5f30d6d3904a9d59088aed09507678dfe"}}
JSON
cat >"$FIXTURE_DIR/tag-object-wrong-tag.json" <<'JSON'
{"tag":"v0.1.156","object":{"type":"commit","sha":"a2779cd5f30d6d3904a9d59088aed09507678dfe"}}
JSON
cat >"$FIXTURE_DIR/tag-object-non-commit.json" <<'JSON'
{"tag":"v0.1.157","object":{"type":"tree","sha":"a2779cd5f30d6d3904a9d59088aed09507678dfe"}}
JSON

run_resolver() {
  SUB2API_RELEASE_JSON_FILE="$1" \
    SUB2API_RELEASE_REF_JSON_FILE="$2" \
    SUB2API_RELEASE_TAG_JSON_FILE="${3:-$FIXTURE_DIR/tag-object.json}" \
    bash "$RESOLVER"
}

assert_line() {
  local expected="$1"
  if ! grep -Fxq "$expected" <<<"$stable_output"; then
    printf 'missing output line: %s\nactual output:\n%s\n' "$expected" "$stable_output" >&2
    exit 1
  fi
}

assert_fails() {
  local release_fixture="$1"
  local ref_fixture="$2"
  local tag_fixture="${3:-tag-object.json}"
  if run_resolver "$FIXTURE_DIR/$release_fixture" "$FIXTURE_DIR/$ref_fixture" "$FIXTURE_DIR/$tag_fixture" >/dev/null 2>&1; then
    printf 'expected resolver failure for %s and %s\n' "$release_fixture" "$ref_fixture" >&2
    exit 1
  fi
}

stable_output="$(run_resolver "$FIXTURE_DIR/stable.json" "$FIXTURE_DIR/ref.json")"
assert_line 'release_tag=v0.1.157'
assert_line 'release_published_at=2026-07-16T09:12:30Z'
assert_line 'release_tag_object_sha=a44e63f9fab426ec181bafcf4e4c1a002bbcb8e0'
assert_line 'release_tag_object_type=tag'
assert_line 'release_commit=a2779cd5f30d6d3904a9d59088aed09507678dfe'

assert_fails draft.json ref.json
assert_fails prerelease.json ref.json
assert_fails malformed-tag.json ref.json
assert_fails stable.json missing-ref.json
assert_fails stable.json mismatched-ref.json
assert_fails stable.json lightweight-ref.json
assert_fails stable.json ref.json tag-object-wrong-tag.json
assert_fails stable.json ref.json tag-object-non-commit.json

printf 'release resolver tests: PASS\n'
