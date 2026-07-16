#!/usr/bin/env bash
set -Eeuo pipefail

RELEASE_API="${SUB2API_RELEASE_API:-https://api.github.com/repos/Wei-Shaw/sub2api/releases/latest}"
USER_AGENT="sub2api-stable-release-resolver/1"

fail() {
	printf 'stable release resolution failed\n' >&2
	exit 1
}

read_fixture_or_curl() {
	local fixture="$1"
	local url="$2"
	if [[ -n "$fixture" ]]; then
		[[ -r "$fixture" ]] || fail
		cat -- "$fixture"
		return
	fi
	curl -fsSL --retry 2 --connect-timeout 10 --max-time 30 \
		-H 'Accept: application/vnd.github+json' \
		-A "$USER_AGENT" \
		-- "$url" || fail
}

release_json="$(read_fixture_or_curl "${SUB2API_RELEASE_JSON_FILE:-}" "$RELEASE_API")" || fail
tag="$(jq -er 'select(.draft == false and .prerelease == false) | .tag_name' <<<"$release_json" 2>/dev/null)" || fail
[[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail

published_at="$(jq -er '.published_at' <<<"$release_json" 2>/dev/null)" || fail
ref_json="$(read_fixture_or_curl "${SUB2API_RELEASE_REF_JSON_FILE:-}" "https://api.github.com/repos/Wei-Shaw/sub2api/git/ref/tags/$tag")" || fail
ref_name="$(jq -er '.ref' <<<"$ref_json" 2>/dev/null)" || fail
[[ "$ref_name" == "refs/tags/$tag" ]] || fail
tag_object_sha="$(jq -er '.object.sha' <<<"$ref_json" 2>/dev/null)" || fail
[[ "$tag_object_sha" =~ ^[0-9a-fA-F]{40}$ ]] || fail
tag_object_type="$(jq -er '.object.type' <<<"$ref_json" 2>/dev/null)" || fail
[[ "$tag_object_type" == tag ]] || fail
tag_json="$(read_fixture_or_curl "${SUB2API_RELEASE_TAG_JSON_FILE:-}" "https://api.github.com/repos/Wei-Shaw/sub2api/git/tags/$tag_object_sha")" || fail
tag_name="$(jq -er '.tag' <<<"$tag_json" 2>/dev/null)" || fail
[[ "$tag_name" == "$tag" ]] || fail
peeled_object_type="$(jq -er '.object.type' <<<"$tag_json" 2>/dev/null)" || fail
[[ "$peeled_object_type" == commit ]] || fail
release_commit="$(jq -er '.object.sha' <<<"$tag_json" 2>/dev/null)" || fail
[[ "$release_commit" =~ ^[0-9a-fA-F]{40}$ ]] || fail

printf 'release_tag=%s\nrelease_published_at=%s\nrelease_tag_object_sha=%s\nrelease_tag_object_type=%s\nrelease_commit=%s\n' \
	"$tag" "$published_at" "$tag_object_sha" "$tag_object_type" "$release_commit"
