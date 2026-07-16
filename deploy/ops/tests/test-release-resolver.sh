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

run_resolver() {
  SUB2API_RELEASE_JSON_FILE="$1" \
    SUB2API_RELEASE_REF_JSON_FILE="$2" \
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
  if run_resolver "$FIXTURE_DIR/$release_fixture" "$FIXTURE_DIR/$ref_fixture" >/dev/null 2>&1; then
    printf 'expected resolver failure for %s and %s\n' "$release_fixture" "$ref_fixture" >&2
    exit 1
  fi
}

stable_output="$(run_resolver "$FIXTURE_DIR/stable.json" "$FIXTURE_DIR/ref.json")"
assert_line 'release_tag=v0.1.157'
assert_line 'release_published_at=2026-07-16T09:12:30Z'
assert_line 'release_tag_object_sha=a44e63f9fab426ec181bafcf4e4c1a002bbcb8e0'

assert_fails draft.json ref.json
assert_fails prerelease.json ref.json
assert_fails malformed-tag.json ref.json
assert_fails stable.json missing-ref.json
assert_fails stable.json mismatched-ref.json

printf 'release resolver tests: PASS\n'
